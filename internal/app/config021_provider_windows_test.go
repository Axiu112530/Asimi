package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"deskpilot/internal/ai"
	"deskpilot/internal/credential"
)

func TestCommunityProviderConnectPreservesExistingConfiguration(t *testing.T) {
	s := aiFixture(t, nil)
	preset := communityDeepSeek()
	custom := aiTestProfile("existing", "https://example.com/v1")
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": custom})
	old := mustCall(t, s, "GetAISettings", struct{}{}).(ai.Selection)
	old.DefaultProviderID, old.DefaultModelID = custom.ID, custom.Models[0].ID
	old.MaxOutputTokens = 73
	mustCall(t, s, "SaveAISettings", old)
	initial := mustCall(t, s, "GetCommunityProvider", struct{}{}).(communityProviderState)
	if initial.DefaultModelID != old.DefaultModelID || initial.HasCredential || initial.VerifiedAt != "" {
		t.Fatalf("unexpected initial state: %+v", initial)
	}
	if !initial.DefaultUsable {
		t.Fatal("keyless custom providers should remain available")
	}
	const originalKey = "fixture-original-credential"
	accept := func(_ context.Context, p ai.Profile, key []byte) error {
		if p.BaseURL != preset.Profile.BaseURL || p.Protocol != ai.ChatCompletions || string(key) != originalKey {
			t.Fatal("connection sent to an unexpected endpoint or with the wrong key")
		}
		return nil
	}
	connected, err := s.connectCommunityProvider([]byte(originalKey), accept)
	if err != nil || !connected.HasCredential || connected.VerifiedAt == "" || connected.DefaultModelID != preset.Profile.Models[0].ID {
		t.Fatalf("unexpected connection state: %+v, %v", connected, err)
	}
	reloaded := mustCall(t, s, "GetCommunityProvider", struct{}{}).(communityProviderState)
	if reloaded.VerifiedAt != connected.VerifiedAt {
		t.Fatal("verification did not persist")
	}
	deny := func(context.Context, ai.Profile, []byte) error { return ai.Error("INVALID_API_KEY") }
	if _, err = s.connectCommunityProvider([]byte("fixture-invalid-credential"), deny); err != ai.Error("INVALID_API_KEY") {
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
	if _, err := s.connectCommunityProvider(nil, accept); err != nil {
		t.Fatalf("stored key could not be reused: %v", err)
	}
	profile, err := s.aiProfileLocked(connected.DefaultProviderID)
	if err != nil {
		t.Fatal(err)
	}
	profile.Models[0].Capabilities.Vision = ai.Unsupported
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": profile})
	edited := mustCall(t, s, "GetCommunityProvider", struct{}{}).(communityProviderState)
	if !edited.HasCredential || edited.VerifiedAt != "" || edited.ConfiguredModel.Capabilities.Vision != ai.Unsupported {
		t.Fatal("advanced edits did not invalidate verification or preserve capability overrides")
	}
	if retested, err := s.connectCommunityProvider(nil, accept); err != nil || retested.ConfiguredModel.Capabilities.Vision != ai.Unsupported {
		t.Fatal("retest did not preserve advanced model capabilities")
	}
	mustCall(t, s, "DeleteCredential", map[string]string{"providerId": connected.DefaultProviderID})
	removed := mustCall(t, s, "GetCommunityProvider", struct{}{}).(communityProviderState)
	if removed.HasCredential || removed.VerifiedAt != "" {
		t.Fatal("removed key is still shown as verified")
	}
}

func TestCommunityProviderRejectsChangesMadeDuringProbe(t *testing.T) {
	s := aiFixture(t, nil)
	old := aiTestProfile("existing", "http://127.0.0.1:9")
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": old})
	probe := func(context.Context, ai.Profile, []byte) error {
		settings := mustCall(t, s, "GetAISettings", struct{}{}).(ai.Selection)
		settings.DefaultProviderID, settings.DefaultModelID = old.ID, old.Models[0].ID
		mustCall(t, s, "SaveAISettings", settings)
		return nil
	}
	if _, err := s.connectCommunityProvider([]byte("temporary-key"), probe); err != codedError("COMMUNITY_PROVIDER_CHANGED") {
		t.Fatalf("concurrent provider change was not detected: %v", err)
	}
	profiles := mustCall(t, s, "ListAIProfiles", struct{}{}).([]ai.Profile)
	if len(profiles) != 1 || profiles[0].ID != old.ID {
		t.Fatalf("a preset profile was written after detecting stale settings: %+v", profiles)
	}
}

func TestCommunityProviderProbeLoopback(t *testing.T) {
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
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != communityDeepSeekModel ||
			body.MaxTokens != 128 || len(body.Messages) != 1 || body.Messages[0].Role != "user" {
			t.Errorf("unexpected probe payload shape: %+v, %v", body, err)
		}
		jsonFixture(w)
	}))
	defer server.Close()
	p := communityDeepSeek().Profile
	p.ID, p.BaseURL = "loopback-test", server.URL
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := probeCommunityProvider(ctx, p, []byte("loopback-test")); err != nil {
		t.Fatal(err)
	}
}
