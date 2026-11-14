package app

import (
	"strings"
	"testing"
)

func TestCognitionNaturalPreferenceAndConflictReview(t *testing.T) {
	s, _, _ := fixture(t)
	mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true, "automaticLearning": true})
	if score := cognitionScore(cognitionTerms("Keep this patch focused"), "先分析再做最小范围修改"); score == 0 {
		t.Fatal("Chinese preference did not match the English paraphrase")
	}
	workspace := mvpWorkspace(t, s, t.TempDir())
	mustCall(t, s, "SaveCognitionMemory", map[string]string{"kind": "project", "workspaceId": workspace.ID,
		"key": "local-workflow", "text": "先分析再做最小范围修改"})
	matched, err := s.cognitionContext(workspace.ID, "Keep this patch focused", 4200, false)
	if err != nil || len(matched.Memories) != 1 || matched.Memories[0].Kind != "project" {
		t.Fatalf("the English paraphrase did not retrieve the Chinese project memory: %+v %v", matched, err)
	}
	unrelated, err := s.cognitionContext(workspace.ID, "Change the report title", 4200, false)
	if err != nil || len(unrelated.Memories) != 0 {
		t.Fatalf("unrelated project memory entered context: %+v %v", unrelated, err)
	}
	first := "我喜欢先分析再做最小范围修改"
	second := "我更喜欢小步修复，每次只改一处"
	if err := captureCognitionPreference(s.db, "preference-first", first); err != nil {
		t.Fatal(err)
	}
	knowledge, err := s.cognitionContext("", "Keep this patch focused", 4200, false)
	if err != nil || len(knowledge.Memories) != 1 || !strings.Contains(knowledge.Memories[0].Text, first) {
		t.Fatalf("natural preference not retrieved: %+v %v", knowledge, err)
	}
	if err := captureCognitionPreference(s.db, "preference-second", second); err != nil {
		t.Fatal(err)
	}
	changes, err := s.cognitionMemoryChanges("")
	if err != nil || len(changes) != 1 || changes[0].Current != second {
		t.Fatalf("missing proposal: %+v %v", changes, err)
	}
	memories, err := s.cognitionMemories("")
	if err != nil || len(memories) != 1 || memories[0].Text != first {
		t.Fatalf("conflict applied before approval: %+v %v", memories, err)
	}
	mustCall(t, s, "ResolveCognitionMemoryChange", map[string]any{"id": changes[0].ID, "decision": "accept", "pending": true})
	memories, err = s.cognitionMemories("")
	if err != nil || len(memories) != 1 || memories[0].Text != second {
		t.Fatalf("accepted preference not applied: %+v %v", memories, err)
	}
}
