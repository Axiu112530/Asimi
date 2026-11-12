package configuration

import (
	"testing"

	"asimi/internal/ai"
)

func TestResolvePreservesExplicitValuesAndLabelsDefaults(t *testing.T) {
	resolved, err := Resolve(Preferences{Selection: ai.Selection{DefaultProviderID: "p", DefaultModelID: "m", MaxOutputTokens: 384}, Agent: &AgentLimits{Requests: 3}})
	if err != nil || resolved.Selection.MaxOutputTokens != 384 || resolved.Selection.DefaultModelID != "m" || resolved.Selection.MaxContextBytes != 65536 {
		t.Fatalf("unexpected resolution %+v %v", resolved, err)
	}
	if resolved.Agent.Requests != 3 || resolved.Agent.OutputTokens != 1536 || resolved.Sources["chat.maxOutputTokens"] != UserDefined || resolved.Sources["chat.maxContextBytes"] != Default {
		t.Fatalf("incorrect source or Agent settings: %+v", resolved)
	}
}

func TestAgentOverrideCannotExpandPolicy(t *testing.T) {
	for _, override := range []AgentLimits{{Requests: 6}, {BudgetUSD: .11}, {TimeoutSeconds: 61}, {ContextBytes: -1}} {
		if _, err := Resolve(Preferences{Agent: &override}); err == nil {
			t.Fatalf("unsafe override accepted: %+v", override)
		}
	}
}
