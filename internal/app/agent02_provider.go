package app

import (
	"context"
	"math"
	"os"
	"strings"

	"deskpilot/internal/ai"
	"deskpilot/internal/credential"
)

const agent02LiveProvider = "deepseek-live-acceptance"

// This entry point is used only by the explicit isolated acceptance launcher.
// The profile and key are never registered in SQLite or exposed to the WebView.
func (s *Service) EnableAgent02LiveAcceptance() error {
	value, present := os.LookupEnv("DEEPSEEK_API_KEY")
	if !present || strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") {
		return codedError("CREDENTIAL_UNAVAILABLE")
	}
	if err := os.Unsetenv("DEEPSEEK_API_KEY"); err != nil {
		return codedError("CREDENTIAL_UNAVAILABLE")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agent02Secret = []byte(value)
	return nil
}

func (s *Service) agent02Provider(providerID, modelID string, objective ...string) (ai.Profile, ai.Model, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, err := s.settingsLocked()
	if err != nil {
		return ai.Profile{}, ai.Model{}, "", err
	}
	goal := ""
	if len(objective) > 0 {
		goal = objective[0]
	}
	language := responseLanguage(goal, settings)
	if providerID == agent02LiveProvider {
		if len(s.agent02Secret) == 0 || modelID != "deepseek-flash" {
			return ai.Profile{}, ai.Model{}, "", codedError("PROVIDER_UNAVAILABLE")
		}
		limit := int64(1048576)
		cached := 0.006
		model := ai.Model{ID: modelID, DisplayName: "DeepSeek Flash", Capabilities: ai.Capabilities{Text: ai.Supported, StructuredOutput: ai.Supported, ContextLimit: &limit}, Pricing: &ai.Pricing{InputPerMillion: 0.30, OutputPerMillion: 1.20, CachedInputPerMillion: &cached, Currency: "USD", EffectiveAt: "2026-11-11"}}
		return ai.Profile{ID: providerID, Name: "DeepSeek live acceptance", BaseURL: "https://api.deepseek.com/v1", Protocol: ai.ChatCompletions, Enabled: true, Models: []ai.Model{model}}, model, language, nil
	}
	profile, err := s.aiProfileLocked(providerID)
	if err != nil {
		return ai.Profile{}, ai.Model{}, "", err
	}
	if !profile.Enabled {
		return profile, ai.Model{}, "", codedError("PROVIDER_UNAVAILABLE")
	}
	for _, model := range profile.Models {
		if model.ID == modelID && model.Capabilities.Text == ai.Supported {
			if !validAgent02Pricing(model.Pricing) {
				return profile, model, language, codedError("AGENT_PRICING_REQUIRED")
			}
			return profile, model, language, nil
		}
	}
	return profile, ai.Model{}, "", codedError("MODEL_NOT_FOUND")
}

func validAgent02Pricing(p *ai.Pricing) bool {
	return p != nil && p.Currency == "USD" && !math.IsInf(p.InputPerMillion, 0) && !math.IsInf(p.OutputPerMillion, 0) && p.InputPerMillion >= 0 && p.OutputPerMillion >= 0 && (p.CachedInputPerMillion == nil || (!math.IsInf(*p.CachedInputPerMillion, 0) && *p.CachedInputPerMillion >= 0))
}

func (s *Service) requestAgent02Plan(ctx context.Context, preview agent02Preview) (ai.Response, error) {
	adapter, err := ai.NewClient(preview.profile)
	if err != nil {
		return ai.Response{}, err
	}
	defer adapter.Close()
	secret, err := s.agent02Credential(ctx, preview.profile)
	if err != nil {
		return ai.Response{}, err
	}
	defer credential.Wipe(secret)
	in := ai.Request{RequestID: newID(), ConversationID: preview.TaskID, ProviderID: preview.profile.ID, ModelID: preview.model.ID, SystemInstruction: agent02Instruction + " Write user-facing summaries in " + preview.language + " unless the user requests another language.", Messages: []ai.Message{{Role: "user", Content: []ai.Content{{Type: "text", Text: preview.Content}}}}, MaxOutputTokens: preview.limits.OutputTokens, ResponseLanguage: preview.language, JSONOutput: preview.model.Capabilities.StructuredOutput == ai.Supported}
	return adapter.Generate(ctx, in, secret)
}

func (s *Service) agent02Credential(ctx context.Context, reviewed ai.Profile) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if reviewed.ID == agent02LiveProvider {
		if len(s.agent02Secret) == 0 {
			return nil, codedError("CREDENTIAL_UNAVAILABLE")
		}
		return append([]byte(nil), s.agent02Secret...), nil
	}
	current, err := s.aiProfileLocked(reviewed.ID)
	if err != nil || current.CredentialID != reviewed.CredentialID || encode(current) != encode(reviewed) {
		return nil, codedError("TASK_CONTEXT_CHANGED")
	}
	if !current.HasCredential {
		return nil, nil
	}
	secret, err := (credential.Vault{DB: s.db}).GetCredential(current.ID)
	if err != nil {
		return nil, codedError("CREDENTIAL_DECRYPT_FAILED")
	}
	return secret, nil
}
