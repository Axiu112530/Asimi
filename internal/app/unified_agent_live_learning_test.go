package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLiveDevelopmentLearnsAndReusesReviewedSkill(t *testing.T) {
	if os.Getenv("ASIMI_LIVE_LEARNING_ACCEPTANCE") != "1" {
		t.Skip("explicit real model learning acceptance opt-in")
	}
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Fatal("authorized model credential unavailable")
	}
	s, dir, _ := fixture(t)
	s.agent02Secret = []byte(key)
	project := mvpWorkspace(t, s, dir)
	for name, source := range map[string]string{
		"go.mod":       "module fixture\n\ngo 1.26\n",
		"calc.go":      "package calc\nfunc Add(a,b int) int{return a-b}\n",
		"calc_test.go": "package calc\nimport \"testing\"\nfunc TestAdd(t *testing.T){if got:=Add(2,3);got!=5{t.Fatalf(\"got %d want 5\",got)}}\n",
	} {
		if err := os.WriteFile(filepath.Join(project.Root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true, "automaticLearning": true, "projectLearning": true, "automaticSkillCreation": true})
	settle := func(id string) AgentTask {
		until := time.Now().Add(75 * time.Second)
		for time.Now().Before(until) {
			current := mustCall(t, s, "GetAgentTask", map[string]string{"id": id}).(AgentTask)
			if current.Status != "RUNNING" {
				return current
			}
			time.Sleep(75 * time.Millisecond)
		}
		t.Fatal("real model task did not settle")
		return AgentTask{}
	}
	runTask := func(file string, wantSkill bool) (AgentTask, int, int) {
		goal := "Fix the incorrect Add function in " + file + " and run the relevant Go tests. Keep the public API unchanged."
		run := mustCall(t, s, "SubmitAgentGoal", map[string]any{"kind": "development", "goal": goal, "workspaceId": project.ID, "providerId": agent02LiveProvider, "modelId": "deepseek-flash", "paths": []string{file, "calc_test.go"}}).(unifiedRun)
		task := mustCall(t, s, "GetAgentTask", map[string]string{"id": run.TaskID}).(AgentTask)
		local := 0
		for round := 0; round < 5; round++ {
			preview := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": task.ID}).(agent02Preview)
			if preview.LocalSkill != nil {
				local++
			}
			mustCall(t, s, "PlanAgentRun", map[string]string{"id": task.ID, "contextHash": preview.Hash})
			task = settle(task.ID)
			if task.Status == "COMPLETED" {
				break
			}
			if task.Status == "FAILED" || task.Status == "CANCELLED" {
				t.Fatalf("real task status=%s code=%s", task.Status, task.Summary)
			}
			if task.Status == "WAITING_APPROVAL" {
				aid := task.Agent.Rounds[len(task.Agent.Rounds)-1].ActionID
				action, err := loadProductAction(s.db, aid)
				if err != nil {
					t.Fatal(err)
				}
				approved := mustCall(t, s, "ApproveProductAction", map[string]any{"id": aid, "hash": action.Hash, "acceptNetwork": true}).(ProductAction)
				if approved.Status != "SUCCEEDED" {
					t.Fatalf("isolated action failed %s", approved.Error)
				}
				task = settle(task.ID)
			}
		}
		if task.Status != "COMPLETED" || task.Agent.Verification == nil || !task.Agent.Verification.Passed {
			t.Fatalf("real repair lacks completion evidence: %s", task.Status)
		}
		models := 0
		for _, round := range task.Agent.Rounds {
			if round.Skill == nil && round.Status == "COMPLETED" {
				models++
			}
		}
		if wantSkill && local == 0 {
			t.Fatal("reviewed skill did not actually schedule a local execution")
		}
		t.Logf("real task complete; model requests=%d; local skill dispatches=%d", models, local)
		return task, models, local
	}
	first, firstRequests, _ := runTask("calc.go", false)
	var candidate cognitionCandidate
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) {
		state := mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": project.ID}).(cognitionState)
		for _, entry := range state.Candidates {
			if entry.TaskID == first.ID && entry.Status == "pending" {
				candidate = entry
			}
		}
		if candidate.ID != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if candidate.ID == "" {
		state := mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": project.ID}).(cognitionState)
		diagnostic := map[string]any{"first": first, "learning": state.Learning, "memories": state.Memories}
		if directory := os.Getenv("ASIMI_LIVE_EVIDENCE_DIR"); directory != "" {
			body, _ := json.MarshalIndent(diagnostic, "", "  ")
			_ = os.WriteFile(filepath.Join(directory, "live-learning-diagnostic.json"), body, 0600)
		}
		t.Logf("learning status: %s", encode(state.Learning))
		t.Logf("recorded actions: %s", encode(first.Steps))
		t.Fatal("real repair produced no reviewable skill candidate")
	}
	mustCall(t, s, "ReviewCognitionCandidate", map[string]any{"id": candidate.ID, "workspaceId": project.ID, "approve": true})
	// The second task has a distinct source file while retaining the same tests.
	if err := os.WriteFile(filepath.Join(project.Root, "calc.go"), []byte("package calc\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.Root, "more.go"), []byte("package calc\nfunc Add(a,b int) int{return a-b}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	second, secondRequests, local := runTask("more.go", true)
	insights := mustCall(t, s, "GetEvolutionInsights", map[string]string{"workspaceId": project.ID})
	report := map[string]any{"passed": true, "provider": "DeepSeek", "model": "deepseek-flash", "scope": "two isolated repairs -> real test evidence -> learned candidate -> explicit review -> distinct next-task local skill execution", "firstTaskId": first.ID, "secondTaskId": second.ID, "reviewedCandidateId": candidate.ID, "firstModelRequests": firstRequests, "secondModelRequests": secondRequests, "localSkillDispatches": local, "firstVerification": first.Agent.Verification, "secondVerification": second.Agent.Verification, "evolution": insights, "roundsFirst": first.Agent.Rounds, "roundsSecond": second.Agent.Rounds}
	if directory := os.Getenv("ASIMI_LIVE_EVIDENCE_DIR"); directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		body, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(filepath.Join(directory, "live-development-learning.json"), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
