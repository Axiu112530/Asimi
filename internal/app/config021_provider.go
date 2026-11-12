package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"deskpilot/internal/ai"
	"deskpilot/internal/credential"
	"deskpilot/internal/domain"
)

const (
	communityDeepSeekID    = "deepseek"
	communityDeepSeekModel = "deepseek-flash"
	communityDeepSeekURL   = "https://api.deepseek.com"
	communityTestTimeout   = 25 * time.Second
)

type communityPreset struct {
	ID      string     `json:"id"`
	Profile ai.Profile `json:"profile"`
}

type communityProviderState struct {
	Preset              communityPreset `json:"preset"`
	ConfiguredModel     *ai.Model       `json:"configuredModel,omitempty"`
	HasCredential       bool            `json:"hasCredential"`
	VerifiedAt          string          `json:"verifiedAt,omitempty"`
	DefaultProviderID   string          `json:"defaultProviderId"`
	DefaultProviderName string          `json:"defaultProviderName"`
	DefaultModelID      string          `json:"defaultModelId"`
	DefaultUsable       bool            `json:"defaultUsable"`
}

func communityDeepSeek() communityPreset {
	contextLimit := int64(1048576)
	cached := 0.006
	model := ai.Model{
		ID: communityDeepSeekModel, DisplayName: communityDeepSeekModel,
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
	return communityPreset{
		ID: communityDeepSeekID,
		Profile: ai.Profile{
			Name: "DeepSeek", Protocol: ai.ChatCompletions, BaseURL: communityDeepSeekURL,
			Enabled: true, Models: []ai.Model{model},
		},
	}
}

type communityProviderSnapshot struct {
	state    communityProviderState
	profile  ai.Profile
	revision string
}

func (s *Service) communityProviderLocked() (communityProviderSnapshot, error) {
	preset := communityDeepSeek()
	out := communityProviderSnapshot{state: communityProviderState{Preset: preset}}
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
						(p.HasCredential || !isOfficialDeepSeek(p))
					break
				}
			}
		}
		if !p.Enabled || !matchesCommunityEndpoint(p, preset.Profile) {
			continue
		}
		for _, model := range p.Models {
			if model.ID == communityDeepSeekModel && model.Capabilities.Text == ai.Supported &&
				(out.profile.ID == "" || p.ID == settings.DefaultProviderID ||
					out.profile.ID != settings.DefaultProviderID && !out.profile.HasCredential && p.HasCredential) {
				out.profile = p
				out.state.ConfiguredModel = &model
				break
			}
		}
	}
	if out.profile.ID == "" {
		return out, nil
	}
	out.state.HasCredential = out.profile.HasCredential
	var raw string
	err = s.db.QueryRow("SELECT json,updated_at FROM ai_profiles WHERE id=? AND deleted=0", out.profile.ID).Scan(&raw, &out.revision)
	if err != nil {
		return out, err
	}
	var metadata struct {
		VerifiedAt string `json:"communityVerifiedAt"`
	}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return out, err
	}
	if out.profile.HasCredential {
		out.state.VerifiedAt = metadata.VerifiedAt
	}
	return out, nil
}

func matchesCommunityEndpoint(profile, preset ai.Profile) bool {
	if profile.Protocol != preset.Protocol {
		return false
	}
	switch strings.TrimRight(profile.BaseURL, "/") {
	case preset.BaseURL, preset.BaseURL + "/v1", preset.BaseURL + "/chat/completions", preset.BaseURL + "/v1/chat/completions":
		return true
	default:
		return false
	}
}

func isOfficialDeepSeek(profile ai.Profile) bool {
	return profile.BaseURL == communityDeepSeekURL || strings.HasPrefix(profile.BaseURL, communityDeepSeekURL+"/")
}

func (s *Service) callCommunityProvider(method, payload string) domain.Reply {
	if method == "GetCommunityProvider" {
		if err := decode(payload, &struct{}{}); err != nil {
			return result(nil, err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.lifetime.Err() != nil {
			return fail("SERVICE_CLOSED")
		}
		snapshot, err := s.communityProviderLocked()
		return result(snapshot.state, err)
	}
	var request struct {
		PresetID string `json:"presetId"`
		Secret   string `json:"secret,omitempty"`
	}
	if err := decode(payload, &request); err != nil {
		return result(nil, err)
	}
	if request.PresetID != communityDeepSeekID {
		return fail("PROVIDER_UNAVAILABLE")
	}
	key := []byte(request.Secret)
	request.Secret = ""
	defer credential.Wipe(key)
	state, err := s.connectCommunityProvider(key, probeCommunityProvider)
	return result(state, err)
}

// ConnectCommunityAcceptance runs the production preset flow for native acceptance.
// The acceptance runner owns profile isolation and wipes its input.
func (s *Service) ConnectCommunityAcceptance(key []byte) error {
	if !s.beginCall() {
		return codedError("SERVICE_CLOSED")
	}
	defer s.wg.Done()
	_, err := s.connectCommunityProvider(key, probeCommunityProvider)
	return err
}

type communityProbe func(context.Context, ai.Profile, []byte) error

func (s *Service) connectCommunityProvider(key []byte, probe communityProbe) (communityProviderState, error) {
	var empty communityProviderState
	if len(key) > 8192 {
		return empty, codedError("INVALID_API_KEY")
	}
	for _, char := range key {
		if char < 0x21 || char > 0x7e {
			return empty, codedError("INVALID_API_KEY")
		}
	}
	replacingKey := len(key) > 0
	snapshot, storedKey, err := s.communityProviderAttempt(replacingKey)
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

	ctx, cancel := context.WithTimeout(s.lifetime, communityTestTimeout)
	defer cancel()
	if err := probe(ctx, profile, key); err != nil {
		return empty, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifetime.Err() != nil {
		return empty, codedError("SERVICE_CLOSED")
	}
	current, err := s.communityProviderLocked()
	if err != nil {
		return empty, err
	}
	if current.profile.ID != snapshot.profile.ID || current.revision != snapshot.revision ||
		current.state.DefaultProviderID != snapshot.state.DefaultProviderID ||
		current.state.DefaultModelID != snapshot.state.DefaultModelID {
		return empty, codedError("COMMUNITY_PROVIDER_CHANGED")
	}
	if s.providerActiveLocked(profile.ID) {
		return empty, codedError("REQUEST_ACTIVE")
	}

	stamp := now()
	err = s.transaction(func(tx *sql.Tx) error {
		if err := persistCommunityProfile(tx, profile, current.profile.ID == "", key, replacingKey, stamp); err != nil {
			return err
		}
		if err := setResolvedDefaultModelLocked(tx, profile.ID, communityDeepSeekModel); err != nil {
			return err
		}
		return audit(tx, "", "COMMUNITY_PROVIDER_CONNECTED", map[string]string{
			"providerId": profile.ID, "modelId": communityDeepSeekModel,
		})
	})
	if err != nil {
		return empty, err
	}
	out := current.state
	if out.ConfiguredModel == nil {
		out.ConfiguredModel = &profile.Models[0]
	}
	out.HasCredential, out.DefaultUsable, out.VerifiedAt = true, true, stamp
	out.DefaultProviderID, out.DefaultProviderName, out.DefaultModelID = profile.ID, profile.Name, communityDeepSeekModel
	return out, nil
}

func (s *Service) communityProviderAttempt(replacingKey bool) (communityProviderSnapshot, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifetime.Err() != nil {
		return communityProviderSnapshot{}, nil, codedError("SERVICE_CLOSED")
	}
	snapshot, err := s.communityProviderLocked()
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

func persistCommunityProfile(tx *sql.Tx, profile ai.Profile, creating bool, key []byte, replacingKey bool, stamp string) error {
	if creating {
		models := profile.Models
		profile.Models = nil
		if _, err := tx.Exec("INSERT INTO ai_profiles(id,json,created_at,updated_at) VALUES(?,?,?,?)", profile.ID, encode(profile), stamp, stamp); err != nil {
			return err
		}
		for _, model := range models {
			if _, err := tx.Exec("INSERT INTO ai_models(provider_id,id,json) VALUES(?,?,?)", profile.ID, model.ID, encode(model)); err != nil {
				return err
			}
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
	_, err := tx.Exec("UPDATE ai_profiles SET json=json_set(json,'$.credentialId',?,'$.communityVerifiedAt',?),updated_at=? WHERE id=?",
		profile.CredentialID, stamp, stamp, profile.ID)
	return err
}

func probeCommunityProvider(ctx context.Context, profile ai.Profile, key []byte) error {
	client, err := ai.NewClient(profile)
	if err != nil {
		return err
	}
	defer client.Close()
	request := ai.Request{
		RequestID: newID(), ProviderID: profile.ID, ModelID: communityDeepSeekModel, MaxOutputTokens: 128,
		Messages: []ai.Message{{Role: "user", Content: []ai.Content{{Type: "text", Text: "Reply with OK to test this connection."}}}},
	}
	response, err := client.TestConnection(ctx, request, key)
	if response.HTTPStatus == http.StatusPaymentRequired {
		return codedError("COMMUNITY_PROVIDER_BALANCE")
	}
	if err != nil {
		return err
	}
	if response.Status != "COMPLETED" || strings.TrimSpace(response.Content) == "" {
		return codedError("INVALID_RESPONSE")
	}
	return nil
}
