package app

import (
	"asimi/internal/command"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Removing exact action/version evidence, recall filters, or review gates must break these tests.
func evolutionSkillFixture(t *testing.T, s *Service, project, slug string) cognitionSkill {
	t.Helper()
	in := cognitionSkillInput{Scope: "project", WorkspaceID: project, Slug: slug, Name: "Focused tests", Description: "Run focused tests", Instructions: "Inspect the current source and verify the approved check.", Triggers: []string{"focused tests"}, AllowedTools: []string{"go.test"}, Execution: []cognitionSkillStep{{Tool: "go.test", Target: "changed-package"}}}
	if err := s.transaction(func(tx *sql.Tx) error { return saveCognitionSkill(tx, in, "reviewed:fixture") }); err != nil {
		t.Fatal(err)
	}
	all, err := s.cognitionSkills(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range all {
		if skill.Slug == slug {
			return skill
		}
	}
	t.Fatal("missing skill")
	return cognitionSkill{}
}
func evolutionTaskFixture(t *testing.T, s *Service, project string, skill cognitionSkill, status string, executed bool) AgentTask {
	t.Helper()
	dispatch := &agentSkillDispatch{ID: skill.ID, Namespace: skill.Namespace, Version: skill.Version, Checksum: skill.Checksum, Tool: "go.test"}
	task := AgentTask{ID: newID(), WorkspaceID: project, Goal: "focused tests password: secret-must-not-leak", Status: status, CreatedAt: now(), UpdatedAt: now(), Steps: []AgentStep{}, Agent: &agent02Run{Rounds: []agent02Round{{Version: 1, Status: "COMPLETED", DurationMS: 37}, {Version: 2, Status: "COMPLETED", Skill: dispatch, DurationMS: 0}}}}
	action := ProductAction{ID: newID(), TaskID: task.ID, Kind: "command", Status: "PENDING", Hash: "fixture", CreatedAt: now(), UpdatedAt: now(), ExpiresAt: now(), Proposal: ProductProposal{WorkspaceID: project, Command: &command.Proposal{ID: "go.test"}, Skill: dispatch}}
	if executed {
		task.Agent.Rounds[1].ActionID = action.ID
		task.Steps = []AgentStep{{ID: newID(), Kind: "command", Status: status, ActionID: action.ID}}
	}
	err := s.transaction(func(tx *sql.Tx) error {
		if err := saveAgentTask(tx, task); err != nil {
			return err
		}
		if err := recordCognitionUsage(tx, task.ID, []cognitionSkill{skill}); err != nil {
			return err
		}
		if !executed {
			return nil
		}
		if err := savePreparedProduct(tx, action, task.ID); err != nil {
			return err
		}
		result, statusCode := "{\"exitCode\":0,\"durationMs\":42}", "SUCCEEDED"
		if status == "FAILED" {
			result = "{\"exitCode\":1,\"durationMs\":23}"
			statusCode = "FAILED"
		}
		_, err := tx.Exec("UPDATE product_actions SET status=?,consumed=1,result_json=?,error_code=? WHERE id=?", statusCode, result, "COMMAND_FAILED password: secret-must-not-leak", action.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}
func evolutionJSON(t *testing.T, s *Service, method string, in any) map[string]any {
	t.Helper()
	reply := s.Call(method, encode(in))
	if reply.Error != nil {
		t.Fatalf("%s: %+v", method, reply.Error)
	}
	raw, _ := json.Marshal(reply.Data)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestEvolutionExactExecutionMetricsAndPendingImprovement(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	mustCall(t, s, "PauseCognitionLearning", struct{}{})
	skill := evolutionSkillFixture(t, s, w.ID, "focused-check")
	evolutionTaskFixture(t, s, w.ID, skill, "COMPLETED", false)
	failed := evolutionTaskFixture(t, s, w.ID, skill, "FAILED", true)
	data := evolutionJSON(t, s, "GetEvolutionInsights", map[string]string{"workspaceId": w.ID})
	raw := encode(data)
	if strings.Contains(raw, "secret-must-not-leak") {
		t.Fatal("failure experience leaked input")
	}
	entries := data["skills"].([]any)
	var found map[string]any
	for _, entry := range entries {
		v := entry.(map[string]any)
		if v["id"] == skill.ID {
			found = v
		}
	}
	if found == nil || found["contextUses"] != float64(2) || found["executions"] != float64(1) || found["failures"] != float64(1) || found["successes"] != float64(0) || found["totalDurationMs"] != float64(23) {
		t.Fatalf("wrong actual evidence: %s", raw)
	}
	candidate := evolutionJSON(t, s, "ProposeSkillImprovement", map[string]string{"workspaceId": w.ID, "skillId": skill.ID, "taskId": failed.ID})
	if candidate["status"] != "pending" {
		t.Fatal(candidate)
	}
	skills, _ := s.cognitionSkills(w.ID)
	for _, current := range skills {
		if current.ID == skill.ID && current.Version != 1 {
			t.Fatal("candidate silently activated")
		}
	}
	mustCall(t, s, "ReviewCognitionCandidate", map[string]any{"id": candidate["id"], "workspaceId": w.ID, "approve": true})
	versions, err := s.cognitionSkillVersions(skill.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("review did not create version: %+v %v", versions, err)
	}
}
func TestEvolutionOutcomeEvidenceChangesNextTaskSelection(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	mustCall(t, s, "PauseCognitionLearning", struct{}{})
	reliable := evolutionSkillFixture(t, s, w.ID, "reliable-check")
	noisy := evolutionSkillFixture(t, s, w.ID, "noisy-check")
	for i := 0; i < 5; i++ {
		evolutionTaskFixture(t, s, w.ID, reliable, "COMPLETED", true)
		evolutionTaskFixture(t, s, w.ID, noisy, "FAILED", true)
	}
	next, err := s.cognitionContext(w.ID, "focused tests", 6000, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Skills) < 2 || next.Skills[0].ID != reliable.ID {
		t.Fatalf("next task ignored verified execution history: %+v", next.Skills)
	}
	for _, match := range next.Matches {
		if match.ID == reliable.ID && match.Reason != "outcome-informed" {
			t.Fatalf("missing explanation: %+v", match)
		}
	}
}
func TestEvolutionMaintenancePreservesExplicitMemoryAndStopsExpiredRecall(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	mustCall(t, s, "SaveCognitionMemory", map[string]string{"kind": "user", "key": "language", "text": "Prefer focused tests in Go"})
	err := s.transaction(func(tx *sql.Tx) error {
		return saveCognitionMemory(tx, w.ID, "episode", "old", "focused tests from old run", "task:old", false)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE memory_records SET updated_at='2020-01-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	memories, _ := s.cognitionMemories(w.ID)
	var explicit, old string
	for _, m := range memories {
		if m.Kind == "user" {
			explicit = m.ID
		} else {
			old = m.ID
		}
	}
	dry := evolutionJSON(t, s, "RunMemoryMaintenance", map[string]any{"workspaceId": w.ID, "apply": false, "archiveAfterDays": 30})
	if len(dry["archiveIds"].([]any)) != 1 || len(dry["preservedIds"].([]any)) != 1 {
		t.Fatal(dry)
	}
	evolutionJSON(t, s, "RunMemoryMaintenance", map[string]any{"workspaceId": w.ID, "apply": true, "archiveAfterDays": 30, "expirations": []map[string]string{{"id": explicit, "expiresAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}}})
	context, err := s.cognitionContext(w.ID, "focused tests", 6000, false)
	if err != nil || len(context.Memories) != 0 {
		t.Fatalf("expired/archived memory recalled: %+v %v", context, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM memory_records WHERE id IN (?,?)", explicit, old).Scan(&count); err != nil || count != 2 {
		t.Fatalf("maintenance deleted records: %d %v", count, err)
	}
}
func TestEvolutionCompositionPinsReviewedDependenciesAndRuntimeRevocation(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	skill := evolutionSkillFixture(t, s, w.ID, "base-check")
	input := map[string]any{"workspaceId": w.ID, "scope": "project", "slug": "composed-check", "name": "Focused tests composition", "description": "Reviewed focused check workflow", "triggers": []string{"focused tests"}, "dependencies": []map[string]any{{"id": skill.ID, "version": skill.Version, "checksum": skill.Checksum}}}
	composed := evolutionJSON(t, s, "ComposeCognitionSkills", input)
	if composed["enabled"] != true || len(composed["execution"].([]any)) != 1 {
		t.Fatalf("composition lost executable Go scheduling: %+v", composed)
	}
	var dispatch agentSkillDispatch
	dispatch.ID = composed["id"].(string)
	dispatch.Version = int(composed["version"].(float64))
	dispatch.Checksum = composed["checksum"].(string)
	dispatch.Tool = "go.test"
	if err := validateAgentSkillDispatch(s.db, w.ID, &dispatch); err != nil {
		t.Fatal(err)
	}
	mustCall(t, s, "ChangeCognitionSkill", map[string]string{"scope": "project", "workspaceId": w.ID, "slug": skill.Slug, "operation": "disable"})
	if err := validateAgentSkillDispatch(s.db, w.ID, &dispatch); errorCode(err) != "COGNITION_REVIEW_STALE" {
		t.Fatalf("revoked dependency executable: %v", err)
	}
	context, err := s.cognitionContext(w.ID, "focused tests", 7000, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range context.Skills {
		if entry.ID == dispatch.ID {
			t.Fatal("revoked composition selected")
		}
	}
	input["scope"] = "user"
	input["workspaceId"] = ""
	input["slug"] = "leaked-composition"
	if reply := s.Call("ComposeCognitionSkills", encode(input)); reply.Error == nil {
		t.Fatal("project dependency escaped to personal scope")
	}
}
func TestEvolutionConflictedPreferencesNotRecalled(t *testing.T) {
	s, _, _ := fixture(t)
	if err := captureCognitionPreference(s.db, "first", "I prefer focused tests"); err != nil {
		t.Fatal(err)
	}
	if err := captureCognitionPreference(s.db, "second", "I prefer relevant tests"); err != nil {
		t.Fatal(err)
	}
	context, err := s.cognitionContext("", "focused tests", 4000, false)
	if err != nil || len(context.Memories) != 0 {
		t.Fatalf("conflicted memory recalled: %+v %v", context, err)
	}
}

func TestEvolutionVersionMetricsDoNotCountContextOnlyCompletions(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	mustCall(t, s, "PauseCognitionLearning", struct{}{})
	skill := evolutionSkillFixture(t, s, w.ID, "versioned-check")
	evolutionTaskFixture(t, s, w.ID, skill, "COMPLETED", true)
	evolutionTaskFixture(t, s, w.ID, skill, "COMPLETED", false)
	skills, _ := s.cognitionSkills(w.ID)
	for _, entry := range skills {
		if entry.ID == skill.ID && entry.CompletedUses != 1 {
			t.Fatalf("UI mislabeled context inclusion as execution: %+v", entry)
		}
	}
	updated := skill.cognitionSkillInput
	updated.Instructions = "Inspect the current failure, then verify the approved command result."
	if err := s.transaction(func(tx *sql.Tx) error { return saveCognitionSkill(tx, updated, "reviewed:fixture") }); err != nil {
		t.Fatal(err)
	}
	skills, _ = s.cognitionSkills(w.ID)
	for _, entry := range skills {
		if entry.ID == skill.ID && (entry.Version != 2 || entry.CompletedUses != 0) {
			t.Fatalf("v1 outcomes attributed to v2: %+v", entry)
		}
	}
	data := evolutionJSON(t, s, "GetEvolutionInsights", map[string]string{"workspaceId": w.ID})
	var oldCount, newCount float64
	for _, v := range data["skills"].([]any) {
		m := v.(map[string]any)
		if m["id"] == skill.ID {
			if m["version"] == float64(1) {
				oldCount = m["executions"].(float64)
			} else {
				newCount = m["executions"].(float64)
			}
		}
	}
	if oldCount != 1 || newCount != 0 {
		t.Fatal(data)
	}
}
func TestEvolutionPreRequestFailureNotCountedAsModelRequest(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	mustCall(t, s, "PauseCognitionLearning", struct{}{})
	skill := evolutionSkillFixture(t, s, w.ID, "request-check")
	task := evolutionTaskFixture(t, s, w.ID, skill, "FAILED", true)
	task.Agent.Rounds = append(task.Agent.Rounds, agent02Round{Version: 3, Status: "FAILED", Error: "CREDENTIAL_UNAVAILABLE"})
	if err := s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, task) }); err != nil {
		t.Fatal(err)
	}
	data := evolutionJSON(t, s, "GetEvolutionInsights", map[string]string{"workspaceId": w.ID})
	for _, v := range data["skills"].([]any) {
		m := v.(map[string]any)
		if m["id"] == skill.ID && m["modelRequests"] != float64(1) {
			t.Fatalf("credential failure became network usage: %+v", m)
		}
	}
}

func TestEvolutionImprovementCannotReturnUnrelatedTaskCandidate(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	mustCall(t, s, "PauseCognitionLearning", struct{}{})
	skill := evolutionSkillFixture(t, s, w.ID, "improve-check")
	task := evolutionTaskFixture(t, s, w.ID, skill, "FAILED", true)
	unrelated := skill.cognitionSkillInput
	unrelated.Slug = "unrelated-candidate"
	unrelated.SourceExperience = task.ID
	if err := s.transaction(func(tx *sql.Tx) error { return persistCognitionCandidate(tx, task, unrelated) }); err != nil {
		t.Fatal(err)
	}
	reply := s.Call("ProposeSkillImprovement", encode(map[string]string{"workspaceId": w.ID, "skillId": skill.ID, "taskId": task.ID}))
	if reply.Error == nil {
		t.Fatalf("returned an unrelated existing candidate as this skill improvement: %+v", reply.Data)
	}
}

func TestEvolutionComposedGoCheckActuallyRunsAfterApproval(t *testing.T) {
	var calls atomic.Int32
	s, w, task := agent02TestRun(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Messages []struct {
				Role    string
				Content json.RawMessage
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var content string
		for _, m := range request.Messages {
			if m.Role == "user" {
				if json.Unmarshal(m.Content, &content) != nil {
					var blocks []struct{ Text string }
					if json.Unmarshal(m.Content, &blocks) == nil && len(blocks) > 0 {
						content = blocks[0].Text
					}
				}
			}
		}
		var context struct {
			ProjectContext struct {
				Files []struct{ Path, SHA256, Content string }
			}
		}
		if json.Unmarshal([]byte(content), &context) != nil || len(context.ProjectContext.Files) == 0 {
			w.WriteHeader(400)
			return
		}
		file := context.ProjectContext.Files[0]
		fixed := strings.ReplaceAll(file.Content, "a-b", "a+b")
		plan := agent02Plan{Objective: "Fix Add", Steps: []agent02PlanStep{{Type: "PROPOSE_FILE_CHANGE", Path: file.Path, BaseSHA256: file.SHA256, ProposedContent: &fixed, Summary: "Correct addition"}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": encode(plan)}, "finish_reason": "stop"}}})
	})
	for path, value := range map[string]string{"go.mod": "module fixture\n\ngo 1.26\n", "calc_test.go": "package calc\nimport \"testing\"\nfunc TestAdd(t *testing.T){if Add(2,3)!=5{t.Fatal(\"incorrect addition\")}}\n"} {
		if err := os.WriteFile(filepath.Join(w.Root, path), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	base := evolutionSkillFixture(t, s, w.ID, "base-approved-check")
	composed := evolutionJSON(t, s, "ComposeCognitionSkills", map[string]any{"scope": "project", "workspaceId": w.ID, "slug": "composed-approved-check", "name": "Focused tests composition", "description": "Apply reviewed check", "dependencies": []map[string]any{{"id": base.ID, "version": 1, "checksum": base.Checksum}}})
	composedID := composed["id"].(string)
	mustCall(t, s, "SetAgentCognitionChoice", map[string]string{"id": task.ID, "skillId": base.ID, "mode": "exclude"})
	mustCall(t, s, "SetAgentCognitionChoice", map[string]string{"id": task.ID, "skillId": composedID, "mode": "include"})
	wait := func(status string) AgentTask {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			current := mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
			if current.Status == status {
				return current
			}
			if current.Status == "FAILED" {
				t.Fatalf("task failed: %+v", current)
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timeout waiting %s", status)
		return AgentTask{}
	}
	for round := 1; round <= 2; round++ {
		preview := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": task.ID}).(agent02Preview)
		if round == 2 && (preview.LocalSkill == nil || preview.LocalSkill.ID != composedID) {
			t.Fatalf("composed skill not scheduled: %+v", preview.LocalSkill)
		}
		mustCall(t, s, "PlanAgentRun", map[string]string{"id": task.ID, "contextHash": preview.Hash})
		waiting := wait("WAITING_APPROVAL")
		action, err := loadProductAction(s.db, waiting.Steps[waiting.CurrentStep].ActionID)
		if err != nil {
			t.Fatal(err)
		}
		if round == 2 && (action.Proposal.Skill == nil || action.Proposal.Skill.ID != composedID || action.Kind != "command") {
			t.Fatalf("composition did not make approved command: %+v", action)
		}
		approved := mustCall(t, s, "ApproveProductAction", map[string]any{"id": action.ID, "hash": action.Hash, "acceptNetwork": true}).(ProductAction)
		if approved.Status != "SUCCEEDED" {
			t.Fatalf("approved check failed: %+v", approved)
		}
		wait("NEEDS_REVIEW")
	}
	if calls.Load() != 1 {
		t.Fatalf("composed check unexpectedly required another model request: %d", calls.Load())
	}
	data := evolutionJSON(t, s, "GetEvolutionInsights", map[string]string{"workspaceId": w.ID})
	for _, v := range data["skills"].([]any) {
		m := v.(map[string]any)
		if m["id"] == composedID {
			if m["executions"] != float64(1) || m["successes"] != float64(1) || m["modelRequests"] != float64(1) {
				t.Fatalf("real command outcome missing: %+v", m)
			}
			return
		}
	}
	t.Fatal("composition outcome missing")
}
func TestEvolutionCompositionRejectsUnreviewedChangedAndEscalatedDependencies(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	base := evolutionSkillFixture(t, s, w.ID, "guarded-base")
	manifest := cognitionSkillInput{Scope: "project", WorkspaceID: w.ID, Slug: "guarded-composition", Name: "Guarded composition", Description: "Reviewed workflow", Instructions: "Use reviewed workflow", AllowedTools: []string{"go.test", "workspace.analyze"}, Dependencies: []cognitionSkillDependency{{ID: base.ID, Version: 1, Checksum: base.Checksum}}}
	if err := s.transaction(func(tx *sql.Tx) error { return saveCognitionSkill(tx, manifest, "reviewed:composition") }); errorCode(err) != "COGNITION_REVIEW_STALE" {
		t.Fatalf("tool escalation accepted: %v", err)
	}
	manifest.AllowedTools = []string{"go.test"}
	manifest.Dependencies = append(manifest.Dependencies, manifest.Dependencies[0])
	if err := validateCognitionDependencies(s.db, manifest); errorCode(err) != "COGNITION_REVIEW_STALE" {
		t.Fatalf("duplicate dependency accepted: %v", err)
	}
	manifest.Dependencies = manifest.Dependencies[:1]
	changed := base.cognitionSkillInput
	changed.Instructions = "Inspect changed source before checking."
	if err := s.transaction(func(tx *sql.Tx) error { return saveCognitionSkill(tx, changed, "reviewed:fixture") }); err != nil {
		t.Fatal(err)
	}
	if err := validateCognitionDependencies(s.db, manifest); errorCode(err) != "COGNITION_REVIEW_STALE" {
		t.Fatalf("changed dependency accepted: %v", err)
	}
	manifest.Dependencies[0].Version = 2
	manifest.Dependencies[0].Checksum = cognitionChecksum(encode(changed))
	if _, err := s.db.Exec("UPDATE skill_packages SET source='imported' WHERE id=?", base.ID); err != nil {
		t.Fatal(err)
	}
	if err := validateCognitionDependencies(s.db, manifest); errorCode(err) != "COGNITION_REVIEW_STALE" {
		t.Fatalf("unreviewed import accepted: %v", err)
	}
}
func TestEvolutionSmallOutcomeSamplesDoNotChangeRankingAndProfilesStaySeparate(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	mustCall(t, s, "PauseCognitionLearning", struct{}{})
	skill := evolutionSkillFixture(t, s, w.ID, "small-sample")
	for i := 0; i < 4; i++ {
		evolutionTaskFixture(t, s, w.ID, skill, "FAILED", true)
	}
	data := evolutionJSON(t, s, "GetEvolutionInsights", map[string]string{"workspaceId": w.ID})
	for _, v := range data["skills"].([]any) {
		m := v.(map[string]any)
		if m["id"] == skill.ID && m["selectionAdjustment"] != float64(0) {
			t.Fatalf("tiny samples biased selection: %+v", m)
		}
	}
	next, err := s.cognitionContext(w.ID, "focused tests", 6000, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range next.Skills {
		found = found || entry.ID == skill.ID
	}
	if !found {
		t.Fatal("small samples dropped skill")
	}
	other, _, _ := fixture(t)
	separate := evolutionJSON(t, other, "GetEvolutionInsights", map[string]string{})
	if len(separate["skills"].([]any)) != 0 || len(separate["failures"].([]any)) != 0 {
		t.Fatalf("profile database leaked: %+v", separate)
	}
}

func TestEvolutionFailedLaunchWithoutCommandResultRemainsFailureEvidence(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	mustCall(t, s, "PauseCognitionLearning", struct{}{})
	skill := evolutionSkillFixture(t, s, w.ID, "failed-launch-check")
	task := evolutionTaskFixture(t, s, w.ID, skill, "FAILED", true)
	if _, err := s.db.Exec("UPDATE product_actions SET result_json=NULL WHERE task_id=?", task.ID); err != nil {
		t.Fatal(err)
	}
	data := evolutionJSON(t, s, "GetEvolutionInsights", map[string]string{"workspaceId": w.ID})
	for _, v := range data["skills"].([]any) {
		m := v.(map[string]any)
		if m["id"] == skill.ID {
			if m["failures"] != float64(1) || m["totalDurationMs"] != float64(0) {
				t.Fatalf("failed launch lost: %+v", m)
			}
			return
		}
	}
	t.Fatal(data)
}

func TestEvolutionMaintenanceDryRunAndForeignControlsCannotMutateRecords(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	other := mvpWorkspace(t, s, t.TempDir())
	if err := s.transaction(func(tx *sql.Tx) error {
		if err := saveCognitionMemory(tx, w.ID, "episode", "old", "focused tests history", "task:old", false); err != nil {
			return err
		}
		return saveCognitionMemory(tx, other.ID, "episode", "foreign", "private foreign history", "task:foreign", false)
	}); err != nil {
		t.Fatal(err)
	}
	var own, foreign string
	if err := s.db.QueryRow("SELECT id FROM memory_records WHERE workspace_id=?", w.ID).Scan(&own); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT id FROM memory_records WHERE workspace_id=?", other.ID).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	before, _ := cognitionRevision(s.db)
	evolutionJSON(t, s, "RunMemoryMaintenance", map[string]any{"workspaceId": w.ID, "apply": false, "expirations": []map[string]string{{"id": own, "expiresAt": "2020-01-01T00:00:00Z"}}})
	after, _ := cognitionRevision(s.db)
	if before != after {
		t.Fatal("dry run modified governance state")
	}
	reply := s.Call("RunMemoryMaintenance", encode(map[string]any{"workspaceId": w.ID, "apply": true, "expirations": []map[string]string{{"id": foreign, "expiresAt": "2020-01-01T00:00:00Z"}}}))
	if reply.Error == nil {
		t.Fatal("foreign memory was controlled")
	}
	evolutionJSON(t, s, "RunMemoryMaintenance", map[string]any{"workspaceId": w.ID, "apply": true, "expirations": []map[string]string{{"id": own, "expiresAt": "2020-01-01T00:00:00Z"}}})
	context, err := s.cognitionContext(w.ID, "focused tests", 4000, false)
	if err != nil || len(context.Memories) != 0 {
		t.Fatalf("expired record recalled: %+v %v", context, err)
	}
	evolutionJSON(t, s, "RunMemoryMaintenance", map[string]any{"workspaceId": w.ID, "apply": true, "expirations": []map[string]string{{"id": own, "expiresAt": ""}}})
	context, err = s.cognitionContext(w.ID, "focused tests", 4000, false)
	if err != nil || len(context.Memories) != 1 {
		t.Fatalf("explicit expiration clear did not restore recall: %+v %v", context, err)
	}
}
func TestEvolutionCompositionBoundedGraphAndProjectIsolation(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	other := mvpWorkspace(t, s, t.TempDir())
	base := evolutionSkillFixture(t, s, w.ID, "dag-base")
	parent := base
	for i := 0; i < 8; i++ {
		composed := evolutionJSON(t, s, "ComposeCognitionSkills", map[string]any{"scope": "project", "workspaceId": w.ID, "slug": fmt.Sprintf("dag-level-%d", i), "name": "Reviewed checks", "description": "Bounded reviewed workflow", "dependencies": []map[string]any{{"id": parent.ID, "version": parent.Version, "checksum": parent.Checksum}}})
		raw, _ := json.Marshal(composed)
		if err := json.Unmarshal(raw, &parent); err != nil {
			t.Fatal(err)
		}
	}
	reply := s.Call("ComposeCognitionSkills", encode(map[string]any{"scope": "project", "workspaceId": w.ID, "slug": "dag-too-deep", "name": "Reviewed checks", "description": "Over depth limit", "dependencies": []map[string]any{{"id": parent.ID, "version": parent.Version, "checksum": parent.Checksum}}}))
	if reply.Error == nil {
		t.Fatal("unbounded graph accepted")
	}
	reply = s.Call("ComposeCognitionSkills", encode(map[string]any{"scope": "project", "workspaceId": other.ID, "slug": "dag-foreign", "name": "Reviewed checks", "description": "Foreign dependency", "dependencies": []map[string]any{{"id": base.ID, "version": base.Version, "checksum": base.Checksum}}}))
	if reply.Error == nil {
		t.Fatal("foreign project dependency accepted")
	}
}
