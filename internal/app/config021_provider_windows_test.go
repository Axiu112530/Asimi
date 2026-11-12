package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"asimi/internal/ai"
	"asimi/internal/credential"
)

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
