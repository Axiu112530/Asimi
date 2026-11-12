package app

import (
	"strings"
	"testing"
)

func TestTaskContextRequiresExactReviewAndHasBoundedContent(t *testing.T) {
	task := AgentTask{ID: "task", Goal: "explain project", Steps: []AgentStep{{ID: "s", Kind: "project", Title: "inspect", Status: "COMPLETED", Result: []byte(`{"files":["main.go"]}`)}}}
	preview, e := agentContext(task)
	if e != nil || !strings.Contains(preview.Content, "main.go") || preview.Hash == "" {
		t.Fatalf("%+v %v", preview, e)
	}
	task.Goal = "different"
	changed, e := agentContext(task)
	if e != nil || changed.Hash == preview.Hash {
		t.Fatal("changed context retained approval hash")
	}
	task.Goal = strings.Repeat("a", 60000)
	if _, e = agentContext(task); e == nil {
		t.Fatal("unbounded provider context accepted")
	}
}
