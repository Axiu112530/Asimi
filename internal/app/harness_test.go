package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func harnessTestPlan(t *testing.T, goal string, steps ...agent02PlanStep) (*Service, Workspace, AgentTask) {
	t.Helper()
	s, workspace, fixture := agent02TestRun(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		plan := agent02Plan{Objective: goal, Steps: steps}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "harness-loopback", "choices": []any{map[string]any{"message": map[string]string{"content": encode(plan)}, "finish_reason": "stop"}}})
	})
	task := mustCall(t, s, "CreateAgentRun", agent02CreateInput{WorkspaceID: workspace.ID, Goal: goal, ProviderID: fixture.Agent.ProviderID, ModelID: fixture.Agent.ModelID, Paths: []string{"calc.go"}}).(AgentTask)
	return s, workspace, task
}

func harnessRunRound(t *testing.T, s *Service, task AgentTask) AgentTask {
	t.Helper()
	review := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": task.ID}).(agent02Preview)
	mustCall(t, s, "PlanAgentRun", map[string]string{"id": task.ID, "contextHash": review.Hash})
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		current := mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
		if current.Status != "RUNNING" && !s.activeMVPControl("task:"+task.ID) {
			return current
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("planning round did not finish")
	return AgentTask{}
}

func TestHarnessRejectsFinalizeWithoutRequiredEvidence(t *testing.T) {
	for _, goal := range []string{"Fix Add", "请修复 calc.go 并运行测试", "Run go test ./calc", "Analyze this project then send an email"} {
		t.Run(goal, func(t *testing.T) {
			s, _, task := harnessTestPlan(t, goal,
				agent02PlanStep{Type: "VERIFY_RESULT", Summary: "All checks passed"},
				agent02PlanStep{Type: "FINALIZE", Summary: "All done"})
			task = harnessRunRound(t, s, task)
			if task.Status == "COMPLETED" || task.Summary == "All done" {
				t.Fatalf("model summary became task outcome: %s %s", task.Status, task.Summary)
			}
		})
	}
}

func TestHarnessClarificationBindsRevisedOutcome(t *testing.T) {
	s, _, task := harnessTestPlan(t, "Help with this",
		agent02PlanStep{Type: "FINALIZE", Summary: "Add subtracts its second argument"})
	id := map[string]string{"id": task.ID}
	before := mustCall(t, s, "PreviewAgentRun", id).(agent02Preview)
	mustCall(t, s, "SetAgentCognitionCorrection", map[string]string{"id": task.ID, "correction": "Explain Add in calc.go"})
	after := mustCall(t, s, "PreviewAgentRun", id).(agent02Preview)
	if before.Hash == after.Hash || !strings.Contains(after.Content, `"acceptance":{"version":1,"kind":"read","paths":["calc.go"]}`) {
		t.Fatal("review does not bind the clarified read goal")
	}
	task = harnessRunRound(t, s, task)
	if task.Status != "COMPLETED" {
		t.Fatalf("clarified read goal did not complete: %s %s", task.Status, task.Summary)
	}
}

func TestHarnessObservationAndVerificationHaveEvidence(t *testing.T) {
	s, _, task := harnessTestPlan(t, "Explain Add in calc.go",
		agent02PlanStep{Type: "READ_WORKSPACE"},
		agent02PlanStep{Type: "ANALYZE_CODE"},
		agent02PlanStep{Type: "VERIFY_RESULT"},
		agent02PlanStep{Type: "FINALIZE", Summary: "Add subtracts its second argument"})
	task = harnessRunRound(t, s, task)
	if task.Status != "COMPLETED" || len(task.Steps) != 5 {
		t.Fatalf("read-only analysis did not complete: %+v", task)
	}
	for _, step := range task.Steps[1:4] {
		if step.Status != "COMPLETED" || len(step.Result) == 0 || string(step.Result) == "null" {
			t.Errorf("%s: no observed result: %+v", step.Title, step)
		}
	}
}
