package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"asimi/internal/ai"
	"asimi/internal/configuration"
	"asimi/internal/credential"
)

func TestPresetConfigurationProvenanceFollowsVerifiedModelAndCatalog(t *testing.T) {
	for _, test := range []struct {
		name         string
		modelID      string
		override     bool
		capabilities configuration.Source
	}{
		{name: "second catalog model", modelID: "glm-4.6v", capabilities: configuration.Preset},
		{name: "custom model", modelID: "my-private-model", capabilities: configuration.UserDefined},
		{name: "advanced override", modelID: "glm-4.6v", override: true, capabilities: configuration.UserDefined},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := aiFixture(t, nil)
			preset, _ := providerPreset("glm")
			accept := func(context.Context, ai.Profile, string, []byte) error { return nil }
			connected, err := s.connectPresetProvider(preset, []byte("fixture-provenance-key"), accept, test.modelID)
			if err != nil {
				t.Fatal(err)
			}
			if test.override {
				profile, err := s.aiProfileLocked(connected.ProviderID)
				if err != nil {
					t.Fatal(err)
				}
				for i := range profile.Models {
					if profile.Models[i].ID == test.modelID {
						profile.Models[i].Capabilities.Vision = ai.Unsupported
					}
				}
				mustCall(t, s, "SaveAIProfile", map[string]any{"profile": profile})
				if _, err := s.connectPresetProvider(preset, nil, accept, test.modelID); err != nil {
					t.Fatal(err)
				}
			}
			resolved := mustCall(t, s, "GetConfiguration", struct{}{}).(configuration.Resolved)
			if resolved.Sources["provider.selection"] != configuration.Preset || resolved.Sources["model.capabilities"] != test.capabilities {
				t.Fatalf("verified model provenance was not resolved from its provider catalog: %+v", resolved.Sources)
			}
			mustCall(t, s, "DeleteCredential", map[string]string{"providerId": connected.ProviderID})
			removed := mustCall(t, s, "GetConfiguration", struct{}{}).(configuration.Resolved)
			if removed.Sources["provider.selection"] == configuration.Preset || removed.Sources["model.capabilities"] == configuration.Preset {
				t.Fatal("removed credential still has verified preset provenance")
			}
		})
	}
}

func TestPresetConfigurationProvenanceDoesNotLeakAcrossDeepSeekModels(t *testing.T) {
	for _, metadata := range []string{"current", "legacy-preset", "legacy-community"} {
		t.Run(metadata, func(t *testing.T) {
			s := aiFixture(t, nil)
			verifiedID, otherID := "deepseek-v4-pro", "deepseek-flash"
			if metadata != "current" {
				verifiedID, otherID = otherID, verifiedID
			}
			accept := func(context.Context, ai.Profile, string, []byte) error { return nil }
			connected, err := s.connectPresetProvider(presetDeepSeek(), []byte("fixture-provenance-key"), accept, verifiedID)
			if err != nil {
				t.Fatal(err)
			}
			if metadata != "current" {
				if _, err := s.db.Exec("UPDATE ai_profiles SET json=json_remove(json,'$.presetVerifiedModelId') WHERE id=?", connected.ProviderID); err != nil {
					t.Fatal(err)
				}
			}
			if metadata == "legacy-community" {
				if _, err := s.db.Exec("UPDATE ai_profiles SET json=json_set(json_remove(json,'$.presetVerifiedAt'),'$.communityVerifiedAt',?) WHERE id=?", connected.VerifiedAt, connected.ProviderID); err != nil {
					t.Fatal(err)
				}
			}
			verified := mustCall(t, s, "GetConfiguration", struct{}{}).(configuration.Resolved)
			if verified.Sources["provider.selection"] != configuration.Preset || verified.Sources["model.capabilities"] != configuration.Preset {
				t.Fatalf("verified catalog model lost provenance: %+v", verified.Sources)
			}
			selection := verified.Selection
			selection.DefaultModelID = otherID
			mustCall(t, s, "SaveAISettings", selection)
			other := mustCall(t, s, "GetConfiguration", struct{}{}).(configuration.Resolved)
			if other.Sources["provider.selection"] != configuration.UserDefined || other.Sources["model.capabilities"] != configuration.UserDefined {
				t.Fatalf("verification provenance leaked to an untested model: %+v", other.Sources)
			}
			selection.DefaultModelID = verifiedID
			mustCall(t, s, "SaveAISettings", selection)
			restored := mustCall(t, s, "GetConfiguration", struct{}{}).(configuration.Resolved)
			if restored.Sources["provider.selection"] != configuration.Preset || restored.Sources["model.capabilities"] != configuration.Preset {
				t.Fatal("switching back lost verification provenance")
			}
		})
	}
}

func TestPresetProviderSelectsModelBeforeConnection(t *testing.T) {
	s := aiFixture(t, nil)
	state := mustCall(t, s, "GetPresetProvider", map[string]string{"presetId": "glm", "modelId": "glm-4.6v"}).(presetProviderState)
	if state.ConfiguredModel == nil || state.ConfiguredModel.ID != "glm-4.6v" || state.ConfiguredModel.Capabilities.Vision != ai.Supported {
		t.Fatalf("selected catalog model was not returned: %+v", state)
	}
	custom := mustCall(t, s, "GetPresetProvider", map[string]string{"presetId": "glm", "modelId": "account-custom-model"}).(presetProviderState)
	if custom.ConfiguredModel == nil || custom.ConfiguredModel.ID != "account-custom-model" || custom.ConfiguredModel.Capabilities.Vision != ai.Unknown || custom.ConfiguredModel.Pricing != nil {
		t.Fatalf("custom model should have conservative capabilities and no invented price: %+v", custom)
	}
	if custom.ConfiguredModel.Capabilities.Streaming != ai.Supported {
		t.Fatal("custom model should support the ordinary chat form's streaming requests")
	}
	profiles := mustCall(t, s, "ListAIProfiles", struct{}{}).([]ai.Profile)
	if len(profiles) != 0 {
		t.Fatal("reading model choices wrote a provider")
	}
}

func TestPresetProviderRejectsInvalidModelIDs(t *testing.T) {
	s := aiFixture(t, nil)
	for _, id := range []string{" ", "bad\nmodel", "bad\tmodel", "bad\x7fmodel", "bad\u0085model", strings.Repeat("m", 257)} {
		for _, method := range []string{"GetPresetProvider", "ConnectPresetProvider"} {
			screenError(t, s, method, map[string]string{"presetId": "glm", "modelId": id}, "INVALID_REQUEST")
		}
	}
}

func TestPresetProviderModelChangesReuseProfileAndPreserveOverrides(t *testing.T) {
	s := aiFixture(t, nil)
	preset, _ := providerPreset("glm")
	profile := preset.Profile
	profile.ID, profile.Name, profile.TimeoutSeconds = "advanced-glm", "My GLM", 42
	// An advanced profile need not contain the preset's first catalog model.
	custom := presetModel("previous-custom-model", true)
	custom.DisplayName = "My model"
	custom.Pricing = &ai.Pricing{InputPerMillion: 4, OutputPerMillion: 8, Currency: "CNY"}
	profile.Models = []ai.Model{custom}
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": profile})
	mustCall(t, s, "StoreCredential", map[string]string{"providerId": profile.ID, "secret": "fixture-glm-key"})
	probe := func(_ context.Context, actual ai.Profile, modelID string, key []byte) error {
		if actual.ID != "advanced-glm" || actual.BaseURL != "https://open.bigmodel.cn/api/paas/v4" ||
			actual.TimeoutSeconds != 42 || string(key) != "fixture-glm-key" || modelID != "glm-4.6v" {
			t.Fatalf("model selection changed provider details or reused the wrong key: %+v, %s", actual, modelID)
		}
		return nil
	}
	connected, err := s.connectPresetProvider(preset, nil, probe, "glm-4.6v")
	if err != nil || connected.ProviderID != "advanced-glm" || connected.DefaultModelID != "glm-4.6v" || connected.ConfiguredModel.Capabilities.Vision != ai.Supported {
		t.Fatalf("could not connect another catalog model: %+v, %v", connected, err)
	}
	stored := mustCall(t, s, "ListAIProfiles", struct{}{}).([]ai.Profile)
	if len(stored) != 1 || len(stored[0].Models) != 2 || stored[0].Name != "My GLM" {
		t.Fatalf("model connection duplicated a provider or lost an existing model: %+v", stored)
	}
	var savedCustom *ai.Model
	for _, model := range stored[0].Models {
		if model.ID == "previous-custom-model" {
			savedCustom = &model
		}
	}
	if savedCustom == nil || encode(savedCustom) != encode(custom) {
		t.Fatalf("advanced model overrides changed: %+v", savedCustom)
	}
	state := mustCall(t, s, "GetPresetProvider", map[string]string{"presetId": "glm"}).(presetProviderState)
	if state.ProviderID != "advanced-glm" || state.ConfiguredModel.ID != "glm-4.6v" || state.VerifiedAt == "" || len(state.Preset.Profile.Models) != 3 {
		t.Fatalf("default model and combined choices were not returned: %+v", state)
	}
	other := mustCall(t, s, "GetPresetProvider", map[string]string{"presetId": "glm", "modelId": "previous-custom-model"}).(presetProviderState)
	if other.VerifiedAt != "" || encode(other.ConfiguredModel) != encode(custom) {
		t.Fatalf("verification or overrides leaked across models: %+v", other)
	}
}

func TestPresetProviderCustomModelProbeIsAtomic(t *testing.T) {
	s := aiFixture(t, nil)
	preset, _ := providerPreset("qwen")
	accept := func(context.Context, ai.Profile, string, []byte) error { return nil }
	initial, err := s.connectPresetProvider(preset, []byte("fixture-saved-key"), accept)
	if err != nil {
		t.Fatal(err)
	}
	deny := func(_ context.Context, profile ai.Profile, modelID string, key []byte) error {
		if profile.ID != initial.ProviderID || modelID != "failed-custom" || string(key) != "fixture-wrong-key" {
			t.Fatal("failed probe did not receive the selected provider, model, and replacement key")
		}
		return ai.Error("MODEL_NOT_FOUND")
	}
	if _, err := s.connectPresetProvider(preset, []byte("fixture-wrong-key"), deny, "failed-custom"); err != ai.Error("MODEL_NOT_FOUND") {
		t.Fatalf("unexpected model probe failure: %v", err)
	}
	unchanged := mustCall(t, s, "GetPresetProvider", map[string]string{"presetId": "qwen"}).(presetProviderState)
	if unchanged.DefaultModelID != "qwen-plus" || unchanged.VerifiedAt != initial.VerifiedAt {
		t.Fatalf("failed model probe changed the default or verification: %+v", unchanged)
	}
	stored, err := s.aiProfileLocked(initial.ProviderID)
	if err != nil || len(stored.Models) != 2 {
		t.Fatalf("failed probe wrote a new model: %+v, %v", stored, err)
	}
	probe := func(_ context.Context, profile ai.Profile, modelID string, key []byte) error {
		if modelID != "my-account-model" || profile.ID != initial.ProviderID || string(key) != "fixture-saved-key" {
			t.Fatal("custom model probe lost the saved profile or key")
		}
		return nil
	}
	connected, err := s.connectPresetProvider(preset, nil, probe, "my-account-model")
	if err != nil || connected.ProviderID != initial.ProviderID || connected.DefaultModelID != "my-account-model" ||
		connected.ConfiguredModel.Capabilities.Vision != ai.Unknown || connected.ConfiguredModel.Pricing != nil {
		t.Fatalf("custom model was not safely connected: %+v, %v", connected, err)
	}
	restored := mustCall(t, s, "GetPresetProvider", map[string]string{"presetId": "qwen"}).(presetProviderState)
	if restored.ConfiguredModel.ID != "my-account-model" || restored.VerifiedAt == "" || len(restored.Preset.Profile.Models) != 3 {
		t.Fatalf("custom model did not persist: %+v", restored)
	}
	first := mustCall(t, s, "GetPresetProvider", map[string]string{"presetId": "qwen", "modelId": "qwen-plus"}).(presetProviderState)
	if first.VerifiedAt != "" {
		t.Fatal("custom model verification was reported for a different selected model")
	}
}

func TestPresetProviderLegacyVerificationOnlyAppliesToFirstModel(t *testing.T) {
	s := aiFixture(t, nil)
	accept := func(context.Context, ai.Profile, string, []byte) error { return nil }
	connected, err := s.connectPresetProvider(presetDeepSeek(), []byte("fixture-legacy-key"), accept)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE ai_profiles SET json=json_remove(json,'$.presetVerifiedModelId') WHERE id=?", connected.ProviderID); err != nil {
		t.Fatal(err)
	}
	first := mustCall(t, s, "GetPresetProvider", map[string]string{"presetId": "deepseek", "modelId": "deepseek-flash"}).(presetProviderState)
	second := mustCall(t, s, "GetPresetProvider", map[string]string{"presetId": "deepseek", "modelId": "deepseek-v4-pro"}).(presetProviderState)
	if first.VerifiedAt == "" || second.VerifiedAt != "" || second.ConfiguredModel.Capabilities.Vision != ai.Unsupported {
		t.Fatalf("legacy verification was not scoped to the model it actually probed: first=%+v, second=%+v", first, second)
	}
}

func TestPresetProviderConnectPreservesExistingConfiguration(t *testing.T) {
	s := aiFixture(t, nil)
	preset := presetDeepSeek()
	custom := aiTestProfile("existing", "https://example.com/v1")
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": custom})
	old := mustCall(t, s, "GetAISettings", struct{}{}).(ai.Selection)
	old.DefaultProviderID, old.DefaultModelID = custom.ID, custom.Models[0].ID
	old.MaxOutputTokens = 73
	mustCall(t, s, "SaveAISettings", old)
	initial := mustCall(t, s, "GetPresetProvider", struct{}{}).(presetProviderState)
	if initial.DefaultModelID != old.DefaultModelID || initial.HasCredential || initial.VerifiedAt != "" {
		t.Fatalf("unexpected initial state: %+v", initial)
	}
	if !initial.DefaultUsable {
		t.Fatal("keyless custom providers should remain available")
	}
	const originalKey = "fixture-original-credential"
	accept := func(_ context.Context, p ai.Profile, modelID string, key []byte) error {
		if modelID != presetDeepSeekModel || p.BaseURL != preset.Profile.BaseURL || p.Protocol != ai.ChatCompletions || string(key) != originalKey {
			t.Fatal("connection sent to an unexpected endpoint or with the wrong key")
		}
		return nil
	}
	connected, err := s.connectPresetProvider(presetDeepSeek(), []byte(originalKey), accept)
	if err != nil || !connected.HasCredential || connected.VerifiedAt == "" || connected.DefaultModelID != preset.Profile.Models[0].ID {
		t.Fatalf("unexpected connection state: %+v, %v", connected, err)
	}
	reloaded := mustCall(t, s, "GetPresetProvider", struct{}{}).(presetProviderState)
	if reloaded.VerifiedAt != connected.VerifiedAt {
		t.Fatal("verification did not persist")
	}
	deny := func(context.Context, ai.Profile, string, []byte) error { return ai.Error("INVALID_API_KEY") }
	if _, err = s.connectPresetProvider(presetDeepSeek(), []byte("fixture-invalid-credential"), deny); err != ai.Error("INVALID_API_KEY") {
		t.Fatalf("unexpected failed-connection result: %v", err)
	}
	secret, err := (credential.Vault{DB: s.db}).GetCredential(connected.DefaultProviderID)
	defer credential.Wipe(secret)
	if err != nil || string(secret) != originalKey {
		t.Fatal("failed connection replaced the stored key")
	}
	settings := mustCall(t, s, "GetAISettings", struct{}{}).(ai.Selection)
	if settings.DefaultProviderID != connected.DefaultProviderID || settings.MaxOutputTokens != old.MaxOutputTokens {
		t.Fatalf("existing settings were overwritten: %+v", settings)
	}
	if _, err := s.connectPresetProvider(presetDeepSeek(), nil, accept); err != nil {
		t.Fatalf("stored key could not be reused: %v", err)
	}
	profile, err := s.aiProfileLocked(connected.DefaultProviderID)
	if err != nil {
		t.Fatal(err)
	}
	profile.Models[0].Capabilities.Vision = ai.Unsupported
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": profile})
	edited := mustCall(t, s, "GetPresetProvider", struct{}{}).(presetProviderState)
	if !edited.HasCredential || edited.VerifiedAt != "" || edited.ConfiguredModel.Capabilities.Vision != ai.Unsupported {
		t.Fatal("advanced edits did not invalidate verification or preserve capability overrides")
	}
	if retested, err := s.connectPresetProvider(presetDeepSeek(), nil, accept); err != nil || retested.ConfiguredModel.Capabilities.Vision != ai.Unsupported {
		t.Fatal("retest did not preserve advanced model capabilities")
	}
	mustCall(t, s, "DeleteCredential", map[string]string{"providerId": connected.DefaultProviderID})
	removed := mustCall(t, s, "GetPresetProvider", struct{}{}).(presetProviderState)
	if removed.HasCredential || removed.VerifiedAt != "" {
		t.Fatal("removed key is still shown as verified")
	}
}

func TestPresetProviderRejectsChangesMadeDuringProbe(t *testing.T) {
	s := aiFixture(t, nil)
	old := aiTestProfile("existing", "http://127.0.0.1:9")
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": old})
	probe := func(context.Context, ai.Profile, string, []byte) error {
		settings := mustCall(t, s, "GetAISettings", struct{}{}).(ai.Selection)
		settings.DefaultProviderID, settings.DefaultModelID = old.ID, old.Models[0].ID
		mustCall(t, s, "SaveAISettings", settings)
		return nil
	}
	if _, err := s.connectPresetProvider(presetDeepSeek(), []byte("temporary-key"), probe); err != codedError("PRESET_PROVIDER_CHANGED") {
		t.Fatalf("concurrent provider change was not detected: %v", err)
	}
	profiles := mustCall(t, s, "ListAIProfiles", struct{}{}).([]ai.Profile)
	if len(profiles) != 1 || profiles[0].ID != old.ID {
		t.Fatalf("a preset profile was written after detecting stale settings: %+v", profiles)
	}
}

func TestBuiltInPresetsKeepCredentialsSeparate(t *testing.T) {
	s := aiFixture(t, nil)
	presets := providerPresets()
	if len(presets) != 8 {
		t.Fatalf("built-in preset count: %d", len(presets))
	}
	ids := map[string]bool{}
	for _, preset := range presets {
		p := preset.Profile
		p.ID = preset.ID
		if ids[p.ID] || ai.ValidateProfile(p) != nil {
			t.Fatalf("invalid or duplicate preset: %s", p.ID)
		}
		ids[p.ID] = true
		state := mustCall(t, s, "GetPresetProvider", map[string]string{"presetId": p.ID}).(presetProviderState)
		if state.Preset.ID != p.ID || len(state.Options) != len(presets) || state.HasCredential {
			t.Fatalf("incorrect initial state for %s", p.ID)
		}
	}
	screenError(t, s, "ConnectPresetProvider", map[string]string{"presetId": "unknown", "secret": "unused"}, "PROVIDER_UNAVAILABLE")

	for _, id := range []string{"deepseek", "qwen"} {
		preset, _ := providerPreset(id)
		expectedKey := "fixture-" + id
		probe := func(_ context.Context, profile ai.Profile, modelID string, secret []byte) error {
			if string(secret) != expectedKey || profile.BaseURL != preset.Profile.BaseURL ||
				modelID != preset.Profile.Models[0].ID {
				t.Fatalf("wrong credentials, endpoint, or model for %s", id)
			}
			return nil
		}
		state, err := s.connectPresetProvider(preset, []byte(expectedKey), probe)
		if err != nil || !state.HasCredential || state.DefaultModelID != preset.Profile.Models[0].ID {
			t.Fatalf("could not connect %s: %v", id, err)
		}
		if _, err := s.connectPresetProvider(preset, nil, probe); err != nil {
			t.Fatalf("could not reuse %s key: %v", id, err)
		}
	}
	deepseek := mustCall(t, s, "GetPresetProvider", struct{}{}).(presetProviderState)
	qwen := mustCall(t, s, "GetPresetProvider", map[string]string{"presetId": "qwen"}).(presetProviderState)
	if !deepseek.HasCredential || !qwen.HasCredential || deepseek.DefaultModelID != "qwen-plus" ||
		qwen.ConfiguredModel.Capabilities.Vision != ai.Unsupported {
		t.Fatal("connecting Qwen replaced or misreported preset state")
	}
	other, _ := providerPreset("glm")
	probe := func(context.Context, ai.Profile, string, []byte) error {
		t.Fatal("missing key should prevent the probe")
		return nil
	}
	if _, err := s.connectPresetProvider(other, nil, probe); err != codedError("CREDENTIAL_REQUIRED") {
		t.Fatalf("unexpected result for unconfigured GLM: %v", err)
	}
}

func TestPresetProviderProbeLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			t.Error("unexpected connection test route")
		}
		if r.Header.Get("Authorization") != "Bearer loopback-test" {
			t.Error("missing fixture authorization")
		}
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			Messages  []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != presetDeepSeekModel ||
			body.MaxTokens != 128 || len(body.Messages) != 1 || body.Messages[0].Role != "user" {
			t.Errorf("unexpected probe payload shape: %+v, %v", body, err)
		}
		jsonFixture(w)
	}))
	defer server.Close()
	p := presetDeepSeek().Profile
	p.ID, p.BaseURL = "loopback-test", server.URL
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := probePresetProvider(ctx, p, presetDeepSeekModel, []byte("loopback-test")); err != nil {
		t.Fatal(err)
	}
}

func TestPresetProviderReadsLegacyVerificationMetadata(t *testing.T) {
	s := aiFixture(t, nil)
	accept := func(context.Context, ai.Profile, string, []byte) error { return nil }
	connected, err := s.connectPresetProvider(presetDeepSeek(), []byte("fixture-legacy-metadata"), accept)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE ai_profiles SET json=json_set(json_remove(json,'$.presetVerifiedAt'),'$.communityVerifiedAt',?) WHERE id=?", connected.VerifiedAt, connected.DefaultProviderID); err != nil {
		t.Fatal(err)
	}
	restored := mustCall(t, s, "GetPresetProvider", struct{}{}).(presetProviderState)
	if !restored.HasCredential || restored.VerifiedAt != connected.VerifiedAt || !restored.DefaultUsable {
		t.Fatalf("legacy verification metadata lost: %+v", restored)
	}
}
