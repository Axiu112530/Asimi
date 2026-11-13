package app

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"asimi/internal/tools"
)

func TestCognitionPreferencePersistsAcrossRestart(t *testing.T) {
	requests := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- string(body)
		jsonFixture(w)
	}))
	defer server.Close()
	s, dir, _ := fixture(t)
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": aiTestProfile("cognition-loopback", server.URL)})
	mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true, "automaticLearning": true})
	chat := func(service *Service, text string) string {
		ids := aiSend(t, service, ChatInput{ProviderID: "cognition-loopback", ModelID: "fixture", Content: text, ConfirmSend: true})
		if usage := awaitAI(t, service, ids["requestId"]); usage.Status != "COMPLETED" {
			t.Fatal(usage.Status)
		}
		return <-requests
	}
	chat(s, "Remember workflow = Use Go and incremental changes")
	chat(s, "Correction: workflow = Use Go and focused patches")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(dir, s.registry.(*tools.Registry), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	body := chat(restarted, "How should we begin a new implementation?")
	if !strings.Contains(body, "Use Go and focused patches") || strings.Contains(body, "Use Go and incremental changes") {
		t.Fatal(body)
	}
}

func TestCognitionSkillPackageRoundTrip(t *testing.T) {
	s, dir, _ := fixture(t)
	workspace := mvpWorkspace(t, s, dir)
	input := cognitionSkillInput{Scope: "project", WorkspaceID: workspace.ID, Slug: "go-check", Name: "Go check",
		Description: "Verify the Go project", Instructions: "Inspect the exit status.", Triggers: []string{"Go check"}, AllowedTools: []string{"go.test"}}
	mustCall(t, s, "SaveCognitionSkill", input)
	state := mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": workspace.ID}).(cognitionState)
	for _, item := range state.Skills {
		if item.Scope != "project" {
			continue
		}
		path := filepath.Join(dir, "skills", item.ID+"-v1", "SKILL.md")
		deadline := time.Now().Add(5 * time.Second)
		for {
			raw, err := os.ReadFile(path)
			if err == nil {
				if !strings.Contains(string(raw), input.Instructions) {
					t.Fatal(string(raw))
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal(err)
			}
			time.Sleep(25 * time.Millisecond)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "manifest.json")); err != nil {
			deadline := time.Now().Add(time.Second)
			for err != nil && time.Now().Before(deadline) {
				time.Sleep(15 * time.Millisecond)
				_, err = os.Stat(filepath.Join(filepath.Dir(path), "manifest.json"))
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		bundle := mustCall(t, s, "ExportCognitionSkill", map[string]string{"scope": "project", "workspaceId": workspace.ID, "slug": input.Slug}).(cognitionSkillPackage)
		if bundle.Manifest.WorkspaceID != "" {
			t.Fatal("workspace ID leaked into portable bundle")
		}
		mustCall(t, s, "ImportCognitionSkill", map[string]any{"scope": "user", "package": bundle})
		bundle.Manifest.Instructions = "Changed after export."
		if reply := s.Call("ImportCognitionSkill", encode(map[string]any{"scope": "user", "package": bundle})); reply.Error == nil || reply.Error.Code != "COGNITION_INVALID_SKILL" {
			t.Fatal(reply)
		}
		local := mustCall(t, s, "GetCognitionState", struct{}{}).(cognitionState)
		for _, skill := range local.Skills {
			if skill.Namespace == "user:go-check" && skill.Source == "imported" {
				return
			}
		}
		t.Fatal("imported package not found")
	}
	t.Fatal("project package not found")
}

func TestCognitionRecordsVerifiedLocalFileAction(t *testing.T) {
	s, dir, _ := fixture(t)
	workspace := mvpWorkspace(t, s, dir)
	mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true, "automaticLearning": true, "projectLearning": true})
	stamp := now()
	task := AgentTask{ID: newID(), WorkspaceID: workspace.ID, Goal: "Document verified local changes", Status: "WAITING_APPROVAL", CreatedAt: stamp, UpdatedAt: stamp}
	prepared := mustCall(t, s, "PrepareFileChange", map[string]any{"workspaceId": workspace.ID, "path": "lesson.txt", "content": "Verified file change\n"}).(ProductAction)
	task.Steps = []AgentStep{{ID: newID(), Kind: "file", Title: "Write lesson", Status: "WAITING_APPROVAL", ActionID: prepared.ID}}
	if err := s.transaction(func(tx *sql.Tx) error {
		if err := saveAgentTask(tx, task); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE product_actions SET task_id=? WHERE id=?", task.ID, prepared.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	prepared.TaskID = task.ID
	// Rebind the prepared fixture to the task as the normal Agent planning path does.
	prepared.Hash = productHash(prepared)
	if _, err := s.db.Exec("UPDATE product_actions SET hash=? WHERE id=?", prepared.Hash, prepared.ID); err != nil {
		t.Fatal(err)
	}
	s.mvp.mu.Lock()
	s.mvp.immutable[prepared.ID] = encode(prepared)
	s.mvp.mu.Unlock()
	result := mustCall(t, s, "ApproveProductAction", map[string]string{"id": prepared.ID, "hash": prepared.Hash}).(ProductAction)
	if result.Status != "SUCCEEDED" {
		t.Fatal(result.Status, result.Error)
	}
	actual, err := os.ReadFile(filepath.Join(workspace.Root, "lesson.txt"))
	if err != nil || string(actual) != "Verified file change\n" {
		t.Fatalf("file result %q %v", actual, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info := mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": workspace.ID}).(cognitionState)
		for _, memory := range info.Memories {
			if memory.Kind == "episode" && memory.Verified && strings.Contains(memory.Text, "independently verified") {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("verified action did not enter project experience")
}
