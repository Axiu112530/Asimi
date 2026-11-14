package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCognitionV11VerifiedWorkflowReviewAndReuse(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("Go toolchain must be on PATH for command approval")
	}
	type observed struct {
		Content string
		Round   int
	}
	started := time.Now()
	var baselineMS, reusedMS int64
	var baselineRequests, reusedRequests int
	requests := make(chan observed, 12)
	s, project, first := agent02TestRun(t, func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			Messages []struct {
				Role    string
				Content json.RawMessage
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			w.WriteHeader(400)
			return
		}
		var content string
		for _, m := range envelope.Messages {
			if m.Role == "user" {
				if err := json.Unmarshal(m.Content, &content); err != nil {
					var blocks []struct{ Text string }
					if json.Unmarshal(m.Content, &blocks) != nil || len(blocks) != 1 {
						w.WriteHeader(400)
						return
					}
					content = blocks[0].Text
				}
			}
		}
		var review struct {
			Round          int
			ProjectContext struct {
				Files []struct{ Path, SHA256, Content string }
			}
		}
		if json.Unmarshal([]byte(content), &review) != nil || len(review.ProjectContext.Files) == 0 {
			w.WriteHeader(400)
			return
		}
		requests <- observed{content, review.Round}
		plan := agent02Plan{Objective: "Fix Add and verify it", Steps: []agent02PlanStep{}}
		file := review.ProjectContext.Files[0]
		if file.Path == "more.go" && review.Round == 2 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		switch review.Round {
		case 1:
			if file.Path == "more.go" && !strings.Contains(content, "project:workflow-go-test") {
				w.WriteHeader(422)
				return
			}
			if strings.Contains(content, "project:workflow-go-test") && !strings.Contains(content, "After approval, run the registered go.test") {
				w.WriteHeader(422)
				return
			}
			fixed := strings.ReplaceAll(file.Content, "a-b", "a+b")
			plan.Steps = append(plan.Steps, agent02PlanStep{Type: "PROPOSE_FILE_CHANGE", Path: file.Path, BaseSHA256: file.SHA256, ProposedContent: &fixed, Summary: "Correct addition"})
		case 2:
			plan.Steps = append(plan.Steps, agent02PlanStep{Type: "PROPOSE_COMMAND", CommandID: "go.test", Summary: "Check actual tests"})
		default:
			plan.Steps = append(plan.Steps, agent02PlanStep{Type: "FINALIZE", Summary: "Corrected addition and verified the project tests."})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "cognition-loopback", "choices": []any{map[string]any{"message": map[string]string{"content": encode(plan)}, "finish_reason": "stop"}}})
	})
	for path, value := range map[string]string{
		"go.mod":       "module fixture\n\ngo 1.26\n",
		"calc_test.go": "package calc\nimport \"testing\"\nfunc TestAdd(t *testing.T){if Add(2,3)!=5{t.Fatal(\"addition is incorrect\")}}\n",
	} {
		if err := os.WriteFile(filepath.Join(project.Root, path), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true, "automaticLearning": true, "projectLearning": true, "automaticSkillCreation": true})
	awaitState := func(id, status string) AgentTask {
		t.Helper()
		until := time.Now().Add(15 * time.Second)
		for time.Now().Before(until) {
			task := mustCall(t, s, "GetAgentTask", map[string]string{"id": id}).(AgentTask)
			if task.Status == status {
				return task
			}
			if task.Status == "FAILED" || task.Status == "CANCELLED" {
				t.Fatalf("task %s: %s", task.Status, task.Summary)
			}
			time.Sleep(15 * time.Millisecond)
		}
		t.Fatalf("timeout waiting for %s", status)
		return AgentTask{}
	}
	run := func(task AgentTask, wantSkill bool) {
		t.Helper()
		id := map[string]string{"id": task.ID}
		start := time.Now()
		modelRequests := 0
		for round := 1; round <= 3; round++ {
			preview := mustCall(t, s, "PreviewAgentRun", id).(agent02Preview)
			if strings.Contains(preview.Content, "project:workflow-go-test") != wantSkill {
				t.Fatal("unexpected skill in preview")
			}
			mustCall(t, s, "PlanAgentRun", map[string]string{"id": task.ID, "contextHash": preview.Hash})
			if wantSkill && round == 2 {
				if !strings.Contains(preview.Content, `"localSkill"`) {
					t.Fatal("approved skill did not schedule a local step")
				}
			} else {
				modelRequests++
				select {
				case sent := <-requests:
					if sent.Round != round || sent.Content != preview.Content {
						t.Fatal("provider received a different context")
					}
				case <-time.After(8 * time.Second):
					t.Fatal("provider received no request")
				}
			}
			if round == 3 {
				awaitState(task.ID, "COMPLETED")
				break
			}
			current := awaitState(task.ID, "WAITING_APPROVAL")
			aid := current.Agent.Rounds[len(current.Agent.Rounds)-1].ActionID
			action, err := loadProductAction(s.db, aid)
			if err != nil {
				t.Fatal(err)
			}
			if wantSkill && round == 2 {
				if action.Proposal.Skill == nil || action.Proposal.Skill.Version != 1 || action.Proposal.Skill.After == "" || action.Proposal.Command == nil || action.Proposal.Command.ID != "go.test" ||
					len(action.Proposal.Command.Args) != 2 || action.Proposal.Command.Args[1] != "." ||
					!strings.Contains(encode(current.Agent.Rounds[1]), "workflow-go-test") {
					t.Fatalf("skill did not schedule the changed package's test: %s", encode(current.Agent.Rounds[1]))
				}
				select {
				case <-requests:
					t.Fatal("local workflow unexpectedly called the provider")
				default:
				}
			}
			if wantSkill && round == 2 {
				withoutConsent := s.Call("ApproveProductAction", encode(map[string]any{"id": aid, "hash": action.Hash}))
				if withoutConsent.Error == nil || withoutConsent.Error.Code != "COMMAND_NETWORK_CONSENT" {
					t.Fatal("skill command bypassed network consent")
				}
				var consumed bool
				if err := s.db.QueryRow("SELECT consumed FROM product_actions WHERE id=?", aid).Scan(&consumed); err != nil || consumed {
					t.Fatalf("skill command consumed before approval: %v %v", consumed, err)
				}
			}
			approved := mustCall(t, s, "ApproveProductAction", map[string]any{"id": aid, "hash": action.Hash, "acceptNetwork": true}).(ProductAction)
			if approved.Status != "SUCCEEDED" {
				t.Fatal(approved.Status, approved.Error)
			}
			awaitState(task.ID, "NEEDS_REVIEW")
		}
		elapsed := time.Since(start).Milliseconds()
		final := awaitState(task.ID, "COMPLETED")
		failedSteps := 0
		for _, step := range final.Steps {
			if step.Status == "FAILED" || step.Status == "RECOVERY_REQUIRED" {
				failedSteps++
			}
		}
		if failedSteps != 0 {
			t.Fatalf("completed run contains %d failed steps", failedSteps)
		}
		if wantSkill {
			reusedMS, reusedRequests = elapsed, modelRequests
		} else {
			baselineMS, baselineRequests = elapsed, modelRequests
		}
	}
	var startPages, pageBytes int64
	if err := s.db.QueryRow("PRAGMA page_count").Scan(&startPages); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("PRAGMA page_size").Scan(&pageBytes); err != nil {
		t.Fatal(err)
	}
	run(first, false)
	state := func() cognitionState {
		return mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": project.ID}).(cognitionState)
	}
	until := time.Now().Add(6 * time.Second)
	var candidate cognitionCandidate
	for time.Now().Before(until) {
		data := state()
		if len(data.Candidates) > 0 {
			candidate = data.Candidates[0]
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if candidate.Status != "pending" || candidate.TaskID != first.ID || len(candidate.Manifest.Workflow) < 2 {
		t.Fatal(candidate)
	}
	for _, skill := range state().Skills {
		if skill.Slug == candidate.Manifest.Slug {
			t.Fatal("pending skill was activated")
		}
	}
	mustCall(t, s, "ReviewCognitionCandidate", map[string]any{"id": candidate.ID, "workspaceId": project.ID, "approve": true})
	awaitSkill := state()
	var id string
	for _, skill := range awaitSkill.Skills {
		if skill.Slug == candidate.Manifest.Slug {
			id = skill.ID
		}
	}
	if id == "" {
		t.Fatal("reviewed skill missing")
	}
	other := mvpWorkspace(t, s, t.TempDir())
	foreign := mustCall(t, s, "CreateAgentRun", agent02CreateInput{WorkspaceID: other.ID, Goal: first.Goal,
		ProviderID: first.Agent.ProviderID, ModelID: first.Agent.ModelID}).(AgentTask)
	foreignPreview := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": foreign.ID}).(agent02Preview)
	if strings.Contains(foreignPreview.Content, "project:workflow-go-test") || foreignPreview.LocalSkill != nil {
		t.Fatal("project skill crossed into a second workspace")
	}
	for name, source := range map[string]string{"calc.go": "package calc\n", "more.go": "package calc\nfunc Add(a,b int) int {return a-b}\n"} {
		if err := os.WriteFile(filepath.Join(project.Root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	next := mustCall(t, s, "CreateAgentRun", agent02CreateInput{WorkspaceID: project.ID, Goal: first.Goal, ProviderID: first.Agent.ProviderID, ModelID: first.Agent.ModelID, Paths: []string{"more.go"}}).(AgentTask)
	mustCall(t, s, "SetAgentCognitionChoice", map[string]string{"id": next.ID, "skillId": id, "mode": "exclude"})
	excluded := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": next.ID}).(agent02Preview)
	if strings.Contains(excluded.Content, "project:workflow-go-test") {
		t.Fatal("excluded skill was injected")
	}
	mustCall(t, s, "SetAgentCognitionChoice", map[string]string{"id": next.ID, "skillId": id, "mode": "auto"})
	run(next, true)
	if baselineRequests != 3 || reusedRequests != 2 {
		t.Fatalf("model requests baseline=%d reuse=%d", baselineRequests, reusedRequests)
	}
	t.Logf("baseline requests=%d elapsed_ms=%d; reuse requests=%d elapsed_ms=%d; both tasks verified; failed_steps=0/0", baselineRequests, baselineMS, reusedRequests, reusedMS)
	var usage int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM skill_usage WHERE task_id=? AND package_id=?", next.ID, id).Scan(&usage); err != nil || usage != 1 {
		t.Fatalf("usage=%d err=%v", usage, err)
	}
	until = time.Now().Add(6 * time.Second)
	var improvement cognitionCandidate
	for time.Now().Before(until) {
		for _, entry := range state().Candidates {
			if entry.TaskID == next.ID {
				improvement = entry
				break
			}
		}
		if improvement.ID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if improvement.Status != "pending" || improvement.BaseVersion != 1 || improvement.Previous == nil ||
		!strings.Contains(encode(improvement.Previous.Workflow), "calc.go") ||
		!strings.Contains(encode(improvement.Manifest.Workflow), "more.go") {
		t.Fatal(improvement)
	}
	mustCall(t, s, "ReviewCognitionCandidate", map[string]any{"id": improvement.ID, "workspaceId": project.ID, "approve": true})
	skillKey := map[string]any{"scope": "project", "workspaceId": project.ID, "slug": candidate.Manifest.Slug}
	versions := mustCall(t, s, "ListCognitionSkillVersions", skillKey).([]cognitionSkillVersion)
	if len(versions) != 2 || versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatal(versions)
	}
	skillKey["operation"], skillKey["version"] = "restore", 1
	mustCall(t, s, "ChangeCognitionSkill", skillKey)
	for _, skill := range state().Skills {
		if skill.ID == id && skill.Version != 1 {
			t.Fatal("rollback did not restore version one")
		}
	}
	var endPages int64
	if err := s.db.QueryRow("PRAGMA page_count").Scan(&endPages); err != nil {
		t.Fatal(err)
	}
	t.Logf("loopback requests=%d elapsed_ms=%d database_growth_bytes=%d", baselineRequests+reusedRequests, time.Since(started).Milliseconds(), (endPages-startPages)*pageBytes)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", ".")
	cmd.Dir = project.Root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("independent check: %v %s", err, out)
	}
}
