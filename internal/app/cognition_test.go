package app

import (
	"asimi/internal/command"
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCognitionSettingsAndPrivateMemory(t *testing.T) {
	s, root, _ := fixture(t)
	first := mvpWorkspace(t, s, root)
	second := mvpWorkspace(t, s, t.TempDir())
	mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true, "automaticLearning": true, "projectLearning": true})
	mustCall(t, s, "SaveCognitionMemory", map[string]string{"kind": "user", "key": "language", "text": "Prefer Go and incremental changes"})
	mustCall(t, s, "SaveCognitionMemory", map[string]string{"kind": "project", "workspaceId": first.ID, "key": "testing", "text": "Run focused Go tests"})
	state := func(project string) []byte {
		t.Helper()
		value := mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": project})
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	verifyCognitionText(t, state(first.ID), "Run focused Go tests", true)
	verifyCognitionText(t, state(second.ID), "Run focused Go tests", false)
	verifyCognitionText(t, state(second.ID), "Prefer Go and incremental changes", true)
	reply := s.Call("SaveCognitionMemory", `{"kind":"user","key":"password","text":"password: plain-secret"}`)
	if reply.Error == nil {
		t.Fatal("credential was accepted as ordinary memory")
	}
}

func TestCognitionSkillsVersionsAndTools(t *testing.T) {
	s, root, _ := fixture(t)
	first := mvpWorkspace(t, s, root)
	second := mvpWorkspace(t, s, t.TempDir())
	req := map[string]any{"scope": "project", "workspaceId": first.ID, "slug": "go-tests", "name": "Go tests", "description": "Go tests", "instructions": "Confirm the actual test result.", "triggers": []string{"Go tests"}, "allowedTools": []string{"workspace.analyze"}}
	mustCall(t, s, "SaveCognitionSkill", req)
	req["instructions"] = "Inspect the failure before making changes."
	mustCall(t, s, "SaveCognitionSkill", req)
	req["allowedTools"] = []string{"unregistered.tool"}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if reply := s.Call("SaveCognitionSkill", string(raw)); reply.Error == nil {
		t.Fatal("unregistered tool was accepted")
	}
	mustCall(t, s, "ChangeCognitionSkill", map[string]any{"scope": "project", "workspaceId": first.ID, "slug": "go-tests", "operation": "restore", "version": 1})
	for _, tc := range []struct {
		project string
		want    bool
	}{{first.ID, true}, {second.ID, false}} {
		value := mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": tc.project})
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		verifyCognitionText(t, raw, "Confirm the actual test result.", tc.want)
	}
}

func TestCognitionLearningAndReviewedReuse(t *testing.T) {
	s, workspace, first := agent02TestRun(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("preview must not call the provider")
	})
	mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true, "automaticLearning": true, "automaticSkillCreation": true, "projectLearning": true})
	mustCall(t, s, "PauseCognitionLearning", struct{}{})
	for i := 0; i < 3; i++ {
		task := first
		task.ID = newID()
		task.Goal = "Verify Go tests"
		task.Status = "COMPLETED"
		a := ProductAction{ID: newID(), TaskID: task.ID, Kind: "command", Status: "SUCCEEDED", Hash: "fixture", CreatedAt: now(), UpdatedAt: now(), ExpiresAt: now(), Proposal: ProductProposal{WorkspaceID: workspace.ID, Command: &command.Proposal{ID: "go.test"}}}
		task.Steps = []AgentStep{{ID: newID(), ActionID: a.ID, Kind: "command", Status: "COMPLETED"}}
		if err := s.transaction(func(tx *sql.Tx) error {
			if err := saveAgentTask(tx, task); err != nil {
				return err
			}
			if err := savePreparedProduct(tx, a, task.ID); err != nil {
				return err
			}
			_, err := tx.Exec("UPDATE product_actions SET status='SUCCEEDED',consumed=1,result_json=? WHERE id=?", `{"exitCode":0,"stdout":"PASS"}`, a.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	state := mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": workspace.ID})
	raw, _ := json.Marshal(state)
	verifyCognitionText(t, raw, `"paused":3`, true)
	mustCall(t, s, "ResumeCognitionLearning", struct{}{})
	deadline := time.Now().Add(5 * time.Second)
	for {
		state := mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": workspace.ID})
		raw, _ = json.Marshal(state)
		if strings.Contains(string(raw), `"completed":3`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("learning did not drain: %s", raw)
		}
		time.Sleep(25 * time.Millisecond)
	}
	verifyCognitionText(t, raw, "user:learned-", false)
	verifyCognitionText(t, raw, "PASS", false)
	next := mustCall(t, s, "CreateAgentRun", agent02CreateInput{WorkspaceID: workspace.ID, Goal: "Verify Go tests", ProviderID: first.Agent.ProviderID, ModelID: first.Agent.ModelID, Paths: first.Agent.Paths}).(AgentTask)
	preview := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": next.ID}).(agent02Preview)
	if !strings.Contains(preview.Content, "go.test") || strings.Contains(preview.Content, "user:learned-") {
		t.Fatal(preview.Content)
	}
}

func verifyCognitionText(t *testing.T, raw []byte, text string, want bool) {
	t.Helper()
	if contains := bytes.Contains(raw, []byte(text)); contains != want {
		t.Fatalf("presence of %q = %v, want %v: %s", text, contains, want, raw)
	}
}
