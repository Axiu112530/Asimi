package app

import (
	"asimi/internal/communication"
	"encoding/json"
	"testing"
)

func TestUnifiedSecurityIncludesConnectorPermissionsAndApprovals(t *testing.T) {
	s, _, _ := fixture(t)
	base := mustCall(t, s, "GetSecurityStatus", struct{}{}).(map[string]any)
	if base["externalNetwork"] != false {
		t.Fatal("unconfigured network reported enabled")
	}
	c := mustCall(t, s, "SaveConnection", communication.SaveConnectionInput{Name: "Public fixture", Kind: "github", Origin: "https://api.github.com", Enabled: true, Scopes: []communication.Scope{{Resource: "octocat/Hello-World", Operations: []string{"repo.read"}}}}).(communication.Connection)
	action := mustCall(t, s, "PrepareConnectorAction", communication.PrepareInput{ConnectionID: c.ID, Operation: "repo.read", Resource: "octocat/Hello-World", Request: json.RawMessage("{}")}).(communication.Action)
	state := mustCall(t, s, "GetSecurityStatus", struct{}{}).(map[string]any)
	if state["pendingApprovals"] != 1 || state["connectorPending"] != 1 || state["externalNetwork"] != true {
		t.Fatalf("connector omitted from actual security state: %+v", state)
	}
	mustCall(t, s, "CancelConnectorAction", map[string]string{"id": action.ID})
	state = mustCall(t, s, "GetSecurityStatus", struct{}{}).(map[string]any)
	if state["pendingApprovals"] != 0 {
		t.Fatal("cancelled connector still counted as pending")
	}
	mustCall(t, s, "RevokeConnection", map[string]string{"id": c.ID})
	state = mustCall(t, s, "GetSecurityStatus", struct{}{}).(map[string]any)
	if state["externalNetwork"] != false {
		t.Fatal("revoked-only network reported enabled")
	}
}
