package app

import (
	"net/http"
	"testing"
)

func TestHarnessSecurityCountsDesktopAndAgentApprovals(t *testing.T) {
	s, _, _ := agent02TestRun(t, func(http.ResponseWriter, *http.Request) {
		t.Error("unexpected model request")
	})
	s.desktopControl.driver = newControlFixtureDriver()
	proposeFixtureText(t, s)

	control := mustCall(t, s, "GetControlStatus", map[string]any{}).(map[string]any)
	security := mustCall(t, s, "GetSecurityStatus", map[string]any{}).(map[string]any)
	if security["desktopControl"] != control["available"] || security["pendingApprovals"] != 1 || security["activeTasks"] != 2 {
		t.Fatalf("Security and desktop/task states disagree: %+v", security)
	}
}
