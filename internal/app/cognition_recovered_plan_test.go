package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCognitionRecoveredInvalidPlanRequiresManualSkill(t *testing.T) {
	const initial = "package calc\nfunc Add(a,b int) int {return a-b}\n"
	const fixed = "package calc\nfunc Add(a,b int) int {return a+b}\n"
	requests := 0
	s, project, task := agent02TestRun(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		patch := fixed
		step := agent02PlanStep{Type: "PROPOSE_FILE_CHANGE", Path: "calc.go", BaseSHA256: hashControlText(initial), ProposedContent: &patch}
		plan := agent02Plan{Objective: "Fix Add", Steps: []agent02PlanStep{step}}
		switch requests {
		case 1:
			plan.Steps = append(plan.Steps, agent02PlanStep{Type: "PROPOSE_COMMAND", CommandID: "go.test"})
		case 3:
			plan.Steps = []agent02PlanStep{{Type: "PROPOSE_COMMAND", CommandID: "go.test"}}
		case 4:
			plan.Steps = []agent02PlanStep{{Type: "FINALIZE", Summary: "Verified patch and passing tests."}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "recovered-plan-fixture", "choices": []any{map[string]any{"message": map[string]string{"content": encode(plan)}, "finish_reason": "stop"}}})
	})
	for name, source := range map[string]string{"go.mod": "module fixture\n\ngo 1.26\n", "calc_test.go": "package calc\nimport \"testing\"\nfunc TestAdd(t *testing.T){if Add(2,3)!=5{t.Fatal(\"wrong sum\")}}\n"} {
		if err := os.WriteFile(filepath.Join(project.Root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true, "automaticLearning": true, "projectLearning": true, "automaticSkillCreation": true})
	for round := 1; round <= 4; round++ {
		task = harnessRunRound(t, s, task)
		if round == 1 {
			if task.Status != "NEEDS_REVIEW" || task.Summary != "AGENT_PLAN_INVALID" {
				t.Fatalf("unsafe combined proposal was not rejected: %s %s", task.Status, task.Summary)
			}
			continue
		}
		if round == 4 {
			break
		}
		if task.Status != "WAITING_APPROVAL" {
			t.Fatalf("safe retry lacks approval: %s %s", task.Status, task.Summary)
		}
		action, err := loadProductAction(s.db, task.Agent.Rounds[len(task.Agent.Rounds)-1].ActionID)
		if err != nil {
			t.Fatal(err)
		}
		approved := mustCall(t, s, "ApproveProductAction", map[string]any{"id": action.ID, "hash": action.Hash, "acceptNetwork": true}).(ProductAction)
		if approved.Status != "SUCCEEDED" {
			t.Fatal(approved.Status, approved.Error)
		}
	}
	if task.Status != "COMPLETED" || task.Agent.Verification == nil || !task.Agent.Verification.Passed {
		t.Fatalf("retry did not recover a verified result: %s", task.Status)
	}
	until := time.Now().Add(4 * time.Second)
	var state cognitionState
	for time.Now().Before(until) {
		state = mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": project.ID}).(cognitionState)
		if state.Completed > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if state.Completed != 1 || len(state.Candidates) != 0 {
		t.Fatal("failed planner attempt was promoted to trusted automatic skill")
	}
	recorded := false
	for _, memory := range state.Memories {
		if memory.Key == task.ID {
			recorded = !memory.Verified && strings.Contains(memory.Text, "AGENT_PLAN_INVALID")
		}
	}
	if !recorded {
		t.Fatal("recovered task erased its observed planner failure")
	}
	// An explicit manually authored instruction remains available for this task.
	mustCall(t, s, "SaveCognitionSkill", cognitionSkillInput{Scope: "project", WorkspaceID: project.ID, Slug: "manual-recovered-workflow", Name: "Reviewed recovery workflow", Description: "Inspect the current task and test the approved repair.", Instructions: "Review the patch and request approval for the registered Go tests.", SourceExperience: task.ID, AllowedTools: []string{"go.test"}})
	skills, err := s.cognitionSkills(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range skills {
		if skill.Slug == "manual-recovered-workflow" && skill.Source == "manual" && skill.Enabled && len(skill.Execution) == 0 {
			return
		}
	}
	t.Fatal("explicit manual instruction skill was unavailable")
}
