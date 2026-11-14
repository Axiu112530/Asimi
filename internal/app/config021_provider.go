package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"asimi/internal/ai"
	"asimi/internal/credential"
	"asimi/internal/domain"
)

const (
	presetDeepSeekID    = "deepseek"
	presetDeepSeekModel = "deepseek-flash"
	presetDeepSeekURL   = "https://api.deepseek.com"
	presetTestTimeout   = 25 * time.Second
)

type presetPreset struct {
	ID      string     `json:"id"`
	Profile ai.Profile `json:"profile"`
}

type presetProviderState struct {
	Preset              presetPreset   `json:"preset"`
	Options             []presetOption `json:"options"`
	ConfiguredModel     *ai.Model      `json:"configuredModel,omitempty"`
	ProviderID          string         `json:"providerId"`
	HasCredential       bool           `json:"hasCredential"`
	VerifiedAt          string         `json:"verifiedAt,omitempty"`
	DefaultProviderID   string         `json:"defaultProviderId"`
	DefaultProviderName string         `json:"defaultProviderName"`
	DefaultModelID      string         `json:"defaultModelId"`
	DefaultUsable       bool           `json:"defaultUsable"`
}

func presetDeepSeek() presetPreset {
	contextLimit := int64(1048576)
	cached := 0.006
	model := ai.Model{
		ID: presetDeepSeekModel, DisplayName: presetDeepSeekModel,
		Capabilities: ai.Capabilities{
			Text: ai.Supported, Vision: ai.Supported, Streaming: ai.Supported,
			ToolCalling: ai.Unsupported, StructuredOutput: ai.Supported, ComputerUse: ai.Unsupported,
			Temperature: ai.Unknown, ContextLimit: &contextLimit,
			ImageDetails: []string{"auto", "low", "high", "original"},
		},
		// Conservative peak prices. https://api-docs.deepseek.com/quick_start/pricing/
		Pricing: &ai.Pricing{
			InputPerMillion: 0.30, OutputPerMillion: 1.20, CachedInputPerMillion: &cached,
			Currency: "USD", EffectiveAt: "2026-11-11",
		},
	}
	return presetPreset{
		ID: presetDeepSeekID,
		Profile: ai.Profile{
			Name: "DeepSeek", Protocol: ai.ChatCompletions, BaseURL: presetDeepSeekURL,
			// Official model identifiers and modalities: https://api-docs.deepseek.com/api/list-models/
			Enabled: true, Models: []ai.Model{model, presetModel("deepseek-v4-pro", false)},
		},
	}
}

type presetProviderSnapshot struct {
	state    presetProviderState
	profile  ai.Profile
	revision string
}

func (s *Service) presetProviderLocked(preset presetPreset, selectedIDs ...string) (presetProviderSnapshot, error) {
	out := presetProviderSnapshot{state: presetProviderState{Preset: preset}}
	modelID := ""
	if len(selectedIDs) > 0 {
		modelID = selectedIDs[0]
	}
	if err := validatePresetModelID(modelID); err != nil {
		return out, err
	}
	for _, item := range providerPresets() {
		out.state.Options = append(out.state.Options, presetOption{ID: item.ID, Name: item.Profile.Name})
	}
	settings, err := s.aiSettingsLocked()
	if err != nil {
		return out, err
	}
	out.state.DefaultProviderID = settings.DefaultProviderID
	out.state.DefaultModelID = settings.DefaultModelID

	profiles, err := s.aiProfilesLocked()
	if err != nil {
		return out, err
	}
	for _, p := range profiles {
		if p.ID == settings.DefaultProviderID {
			out.state.DefaultProviderName = p.Name
			for _, model := range p.Models {
				if model.ID == settings.DefaultModelID {
					out.state.DefaultUsable = p.Enabled && model.Capabilities.Text == ai.Supported &&
						(p.HasCredential || !requiresPresetCredential(p))
					break
				}
			}
		}
		if !p.Enabled || !matchesPresetEndpoint(p, preset.Profile) {
			continue
		}
		if out.profile.ID == "" || p.ID == settings.DefaultProviderID ||
			out.profile.ID != settings.DefaultProviderID && !out.profile.HasCredential && p.HasCredential {
			out.profile = p
		}
	}
	if modelID == "" {
		modelID = preset.Profile.Models[0].ID
		if out.profile.ID != "" && out.profile.ID == settings.DefaultProviderID && settings.DefaultModelID != "" {
			modelID = settings.DefaultModelID
		}
	}
	out.state.Preset.Profile.Models = mergePresetModels(preset.Profile.Models, out.profile.Models)
	for i := range out.state.Preset.Profile.Models {
		if out.state.Preset.Profile.Models[i].ID == modelID {
			model := out.state.Preset.Profile.Models[i]
			out.state.ConfiguredModel = &model
			break
		}
	}
	if out.state.ConfiguredModel == nil {
		model := presetModel(modelID, false)
		model.Capabilities.Vision = ai.Unknown
		out.state.ConfiguredModel = &model
		out.state.Preset.Profile.Models = append(out.state.Preset.Profile.Models, model)
	}
	if out.profile.ID == "" {
		return out, nil
	}
	out.state.ProviderID = out.profile.ID
	out.state.HasCredential = out.profile.HasCredential
	var raw string
	err = s.db.QueryRow("SELECT json,updated_at FROM ai_profiles WHERE id=? AND deleted=0", out.profile.ID).Scan(&raw, &out.revision)
	if err != nil {
		return out, err
	}
	var metadata struct {
		VerifiedAt       string `json:"presetVerifiedAt"`
		VerifiedModelID  string `json:"presetVerifiedModelId"`
		LegacyVerifiedAt string `json:"communityVerifiedAt"`
	}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return out, err
	}
	// Old preset and community connections always probed the first catalog model.
	verifiedModelID := metadata.VerifiedModelID
	if verifiedModelID == "" {
		verifiedModelID = preset.Profile.Models[0].ID
	}
	if out.profile.HasCredential && modelID == verifiedModelID {
		out.state.VerifiedAt = metadata.VerifiedAt
		if out.state.VerifiedAt == "" && preset.ID == presetDeepSeekID {
			out.state.VerifiedAt = metadata.LegacyVerifiedAt
		}
	}
	return out, nil
}

func validatePresetModelID(modelID string) error {
	if modelID == "" {
		return nil
	}
	if strings.TrimSpace(modelID) == "" || len(modelID) > 256 || strings.IndexFunc(modelID, unicode.IsControl) >= 0 {
		return codedError("INVALID_REQUEST")
	}
	return nil
}

// Saved models replace catalog metadata by ID so advanced overrides remain authoritative.
func mergePresetModels(catalog, saved []ai.Model) []ai.Model {
	models := append([]ai.Model(nil), catalog...)
	indexes := make(map[string]int, len(models))
	for i, model := range models {
		indexes[model.ID] = i
	}
	for _, model := range saved {
		if i, ok := indexes[model.ID]; ok {
			models[i] = model
		} else {
			indexes[model.ID] = len(models)
			models = append(models, model)
		}
	}
	return models
}

func matchesPresetEndpoint(profile, preset ai.Profile) bool {
	if profile.Protocol != preset.Protocol {
		return false
	}
	base := strings.TrimRight(profile.BaseURL, "/")
	if base == preset.BaseURL || base == preset.BaseURL+"/chat/completions" {
		return true
	}
	return preset.BaseURL == presetDeepSeekURL &&
		(base == presetDeepSeekURL+"/v1" || base == presetDeepSeekURL+"/v1/chat/completions")
}

func requiresPresetCredential(profile ai.Profile) bool {
	for _, preset := range providerPresets() {
		if matchesPresetEndpoint(profile, preset.Profile) {
			return true
		}
	}
	return profile.BaseURL == presetDeepSeekURL || strings.HasPrefix(profile.BaseURL, presetDeepSeekURL+"/")
}

func (s *Service) callPresetProvider(method, payload string) domain.Reply {
	var request struct {
		PresetID string `json:"presetId"`
		ModelID  string `json:"modelId,omitempty"`
		Secret   string `json:"secret,omitempty"`
	}
	if err := decode(payload, &request); err != nil {
		return result(nil, err)
	}
	preset, ok := providerPreset(request.PresetID)
	if !ok || (method == "ConnectPresetProvider" && request.PresetID == "") {
		return fail("PROVIDER_UNAVAILABLE")
	}
	if err := validatePresetModelID(request.ModelID); err != nil {
		return result(nil, err)
	}
	if method == "GetPresetProvider" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.lifetime.Err() != nil {
			return fail("SERVICE_CLOSED")
		}
		snapshot, err := s.presetProviderLocked(preset, request.ModelID)
		return result(snapshot.state, err)
	}
	key := []byte(request.Secret)
	request.Secret = ""
	defer credential.Wipe(key)
	state, err := s.connectPresetProvider(preset, key, probePresetProvider, request.ModelID)
	return result(state, err)
}

// ConnectPresetAcceptance runs the production preset flow for native acceptance.
// The acceptance runner owns profile isolation and wipes its input.
func (s *Service) ConnectPresetAcceptance(key []byte) error {
	if !s.beginCall() {
		return codedError("SERVICE_CLOSED")
	}
	defer s.wg.Done()
	_, err := s.connectPresetProvider(presetDeepSeek(), key, probePresetProvider)
	return err
}

type presetProbe func(context.Context, ai.Profile, string, []byte) error

func (s *Service) connectPresetProvider(preset presetPreset, key []byte, probe presetProbe, selectedIDs ...string) (presetProviderState, error) {
	var empty presetProviderState
	if len(key) > 8192 {
		return empty, codedError("INVALID_API_KEY")
	}
	for _, char := range key {
		if char < 0x21 || char > 0x7e {
			return empty, codedError("INVALID_API_KEY")
		}
	}
	replacingKey := len(key) > 0
	snapshot, storedKey, err := s.presetProviderAttempt(preset, replacingKey, selectedIDs...)
	if err != nil {
		return empty, err
	}
	defer credential.Wipe(storedKey)
	if !replacingKey {
		key = storedKey
	}
	profile := snapshot.profile
	if profile.ID == "" {
		profile = snapshot.state.Preset.Profile
		profile.ID = newID()
	}
	modelID := snapshot.state.ConfiguredModel.ID
	profile.Models = mergePresetModels(profile.Models, []ai.Model{*snapshot.state.ConfiguredModel})

	ctx, cancel := context.WithTimeout(s.lifetime, presetTestTimeout)
	defer cancel()
	if err := probe(ctx, profile, modelID, key); err != nil {
		return empty, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifetime.Err() != nil {
		return empty, codedError("SERVICE_CLOSED")
	}
	current, err := s.presetProviderLocked(preset, modelID)
	if err != nil {
		return empty, err
	}
	if current.profile.ID != snapshot.profile.ID || current.revision != snapshot.revision ||
		current.state.DefaultProviderID != snapshot.state.DefaultProviderID ||
		current.state.DefaultModelID != snapshot.state.DefaultModelID {
		return empty, codedError("PRESET_PROVIDER_CHANGED")
	}
	if s.providerActiveLocked(profile.ID) {
		return empty, codedError("REQUEST_ACTIVE")
	}

	stamp := now()
	err = s.transaction(func(tx *sql.Tx) error {
		if err := persistPresetProfile(tx, profile, current.profile.ID == "", key, replacingKey, modelID, stamp); err != nil {
			return err
		}
		if err := setResolvedDefaultModelLocked(tx, profile.ID, modelID); err != nil {
			return err
		}
		return audit(tx, "", "PRESET_PROVIDER_CONNECTED", map[string]string{
			"providerId": profile.ID, "modelId": modelID,
		})
	})
	if err != nil {
		return empty, err
	}
	out := current.state
	out.ProviderID = profile.ID
	out.HasCredential, out.DefaultUsable, out.VerifiedAt = true, out.ConfiguredModel.Capabilities.Text == ai.Supported, stamp
	out.DefaultProviderID, out.DefaultProviderName, out.DefaultModelID = profile.ID, profile.Name, modelID
	return out, nil
}

func (s *Service) presetProviderAttempt(preset presetPreset, replacingKey bool, selectedIDs ...string) (presetProviderSnapshot, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifetime.Err() != nil {
		return presetProviderSnapshot{}, nil, codedError("SERVICE_CLOSED")
	}
	snapshot, err := s.presetProviderLocked(preset, selectedIDs...)
	if err != nil {
		return snapshot, nil, err
	}
	if s.providerActiveLocked(snapshot.profile.ID) {
		return snapshot, nil, codedError("REQUEST_ACTIVE")
	}
	if replacingKey {
		return snapshot, nil, nil
	}
	if !snapshot.profile.HasCredential {
		return snapshot, nil, codedError("CREDENTIAL_REQUIRED")
	}
	key, err := (credential.Vault{DB: s.db}).GetCredential(snapshot.profile.ID)
	if err != nil {
		err = codedError(err.Error())
	}
	return snapshot, key, err
}

func persistPresetProfile(tx *sql.Tx, profile ai.Profile, creating bool, key []byte, replacingKey bool, modelID, stamp string) error {
	if creating {
		stored := profile
		stored.Models = nil
		if _, err := tx.Exec("INSERT INTO ai_profiles(id,json,created_at,updated_at) VALUES(?,?,?,?)", profile.ID, encode(stored), stamp, stamp); err != nil {
			return err
		}
	}
	for _, model := range profile.Models {
		if _, err := tx.Exec("INSERT INTO ai_models(provider_id,id,json) VALUES(?,?,?) ON CONFLICT(provider_id,id) DO NOTHING", profile.ID, model.ID, encode(model)); err != nil {
			return err
		}
	}
	if replacingKey {
		if profile.CredentialID == "" {
			profile.CredentialID = newID()
		}
		if err := (credential.Vault{DB: tx}).StoreCredential(profile.CredentialID, profile.ID, key); err != nil {
			if errors.Is(err, credential.ErrUnavailable) {
				return codedError("CREDENTIAL_UNAVAILABLE")
			}
			return err
		}
	}
	_, err := tx.Exec("UPDATE ai_profiles SET json=json_set(json,'$.credentialId',?,'$.presetVerifiedAt',?,'$.presetVerifiedModelId',?),updated_at=? WHERE id=?",
		profile.CredentialID, stamp, modelID, stamp, profile.ID)
	return err
}

func probePresetProvider(ctx context.Context, profile ai.Profile, modelID string, key []byte) error {
	client, err := ai.NewClient(profile)
	if err != nil {
		return err
	}
	defer client.Close()
	maxTokens := 128
	if modelID == "kimi-k3" {
		maxTokens = 1024
	}
	request := ai.Request{
		RequestID: newID(), ProviderID: profile.ID, ModelID: modelID, MaxOutputTokens: maxTokens,
		Messages: []ai.Message{{Role: "user", Content: []ai.Content{{Type: "text", Text: "Reply with OK to test this connection."}}}},
	}
	response, err := client.TestConnection(ctx, request, key)
	if response.HTTPStatus == http.StatusPaymentRequired {
		return codedError("PRESET_PROVIDER_BALANCE")
	}
	if err != nil {
		return err
	}
	if response.Status != "COMPLETED" || strings.TrimSpace(response.Content) == "" {
		return codedError("INVALID_RESPONSE")
	}
	return nil
}
