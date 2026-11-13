package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestCognitionV11ArchiveChangesReviewedContext(t *testing.T) {
	s, project, task := agent02TestRun(t, func(http.ResponseWriter, *http.Request) { t.Error("unreviewed provider call") })
	mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true})
	mustCall(t, s, "SaveCognitionMemory", map[string]string{"workspaceId": project.ID, "kind": "project", "key": "Add", "text": "Fix Add using the project arithmetic convention"})
	args := map[string]string{"id": task.ID}
	before := mustCall(t, s, "PreviewAgentRun", args).(agent02Preview)
	if !strings.Contains(before.Content, "project arithmetic convention") {
		t.Fatal(before.Content)
	}
	state := mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": project.ID}).(cognitionState)
	mustCall(t, s, "ArchiveCognitionMemory", map[string]any{"workspaceId": project.ID, "id": state.Memories[0].ID, "archived": true})
	mustCall(t, s, "SetAgentCognitionCorrection", map[string]string{"id": task.ID, "correction": "Preserve the existing Go function signature."})
	after := mustCall(t, s, "PreviewAgentRun", args).(agent02Preview)
	if !strings.Contains(after.Content, "Preserve the existing Go function signature.") {
		t.Fatal(after.Content)
	}
	if before.Hash == after.Hash || strings.Contains(after.Content, "project arithmetic convention") {
		t.Fatal(after.Content)
	}
	stale := s.Call("PlanAgentRun", encode(map[string]string{"id": task.ID, "contextHash": before.Hash}))
	if stale.Error == nil || stale.Error.Code != "TASK_CONTEXT_CHANGED" {
		t.Fatal(stale)
	}
}
