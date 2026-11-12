package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"deskpilot/internal/developer"
	"deskpilot/internal/tools"
)

func mvpObject(t *testing.T, v any) map[string]any {
	t.Helper()
	var out map[string]any
	if e := json.Unmarshal([]byte(encode(v)), &out); e != nil {
		t.Fatal(e)
	}
	return out
}

func mvpWorkspace(t *testing.T, s *Service, dir string) Workspace {
	t.Helper()
	root := filepath.Join(dir, "project")
	if e := os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	s.SetWorkspacePicker(func(context.Context) (string, error) { return root, nil })
	return mustCall(t, s, "PickWorkspace", struct{}{}).(Workspace)
}

func TestMVPExpiredAndCoherentlyTamperedProposalHaveNoEffect(t *testing.T) {
	for _, mode := range []string{"expired", "tampered", "stale"} {
		t.Run(mode, func(t *testing.T) {
			s, dir, _ := fixture(t)
			w := mvpWorkspace(t, s, dir)
			path := filepath.Join(w.Root, "old.txt")
			if e := os.WriteFile(path, []byte("before"), 0600); e != nil {
				t.Fatal(e)
			}
			a := mustCall(t, s, "PrepareFileChange", map[string]any{"workspaceId": w.ID, "path": "old.txt", "content": "approved"}).(ProductAction)
			switch mode {
			case "expired":
				a.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
				a.Hash = productHash(a)
				s.mvp.immutable[a.ID] = encode(a)
				if _, e := s.db.Exec("UPDATE product_actions SET expires_at=?,hash=? WHERE id=?", a.ExpiresAt, a.Hash, a.ID); e != nil {
					t.Fatal(e)
				}
			case "tampered":
				a.Proposal.File.AfterContent = "forged"
				a.Hash = productHash(a)
				if _, e := s.db.Exec("UPDATE product_actions SET proposal_json=?,hash=? WHERE id=?", encode(a.Proposal), a.Hash, a.ID); e != nil {
					t.Fatal(e)
				}
			case "stale":
				if e := os.WriteFile(path, []byte("new external bytes"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			r := s.Call("ApproveProductAction", encode(map[string]string{"id": a.ID, "hash": a.Hash}))
			if r.Error == nil && r.Data.(ProductAction).Status == "SUCCEEDED" {
				t.Fatal("unsafe action succeeded")
			}
			b, e := os.ReadFile(path)
			if e != nil || string(b) == "approved" || string(b) == "forged" {
				t.Fatalf("unauthorized write %q %v", b, e)
			}
		})
	}
}

func TestMVPConsumptionAndIntentRollbackTogether(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	a := mustCall(t, s, "PrepareFileChange", map[string]string{"workspaceId": w.ID, "path": "blocked.txt", "content": "must not exist"}).(ProductAction)
	if _, e := s.db.Exec("CREATE TRIGGER fail_mvp_intent BEFORE INSERT ON product_action_intents BEGIN SELECT RAISE(FAIL,'intent blocked'); END"); e != nil {
		t.Fatal(e)
	}
	if r := s.Call("ApproveProductAction", encode(map[string]string{"id": a.ID, "hash": a.Hash})); r.Error == nil {
		t.Fatal("intent persistence failed but action admitted")
	}
	assertAbsent(t, filepath.Join(w.Root, "blocked.txt"))
	var status string
	var consumed int
	if e := s.db.QueryRow("SELECT status,consumed FROM product_actions WHERE id=?", a.ID).Scan(&status, &consumed); e != nil || status != "PENDING" || consumed != 0 {
		t.Fatalf("partial consume %s %d %v", status, consumed, e)
	}
}

func TestMVPStopAllInvalidatesPendingAndPausesTask(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	stamp := now()
	task := AgentTask{ID: newID(), WorkspaceID: w.ID, Goal: "review", Status: "COMPLETED", Steps: []AgentStep{}, CreatedAt: stamp, UpdatedAt: stamp}
	if e := s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, task) }); e != nil {
		t.Fatal(e)
	}
	a := mustCall(t, s, "PrepareFileChange", map[string]string{"workspaceId": w.ID, "taskId": task.ID, "path": "stop.txt", "content": "never"}).(ProductAction)
	mustCall(t, s, "StopAll", struct{}{})
	task = mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
	if task.Status != "PAUSED" {
		t.Fatalf("StopAll left task %s", task.Status)
	}
	if r := s.Call("ApproveProductAction", encode(map[string]string{"id": a.ID, "hash": a.Hash})); r.Error == nil {
		t.Fatal("stop allowed replay")
	}
	assertAbsent(t, filepath.Join(w.Root, "stop.txt"))
}

func TestMVPReadOnlyGoalPersistsSeparateTaskAndUsefulEvidence(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	if e := os.WriteFile(filepath.Join(w.Root, "go.mod"), []byte("module example.test/project\n\ngo 1.26\n"), 0600); e != nil {
		t.Fatal(e)
	}
	task := mustCall(t, s, "CreateAgentTask", map[string]string{"workspaceId": w.ID, "goal": "Explain project dependencies"}).(AgentTask)
	deadline := time.Now().Add(30 * time.Second)
	for task.Status == "RUNNING" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		task = mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
	}
	if task.Status != "COMPLETED" || len(task.Steps) != 3 {
		t.Fatalf("task %#v", task)
	}
	var report developer.Report
	if e := json.Unmarshal(task.Steps[0].Result, &report); e != nil || len(report.Entries) == 0 {
		t.Fatalf("no real project evidence: %s %v", task.Steps[0].Result, e)
	}
	var n int
	if e := s.db.QueryRow("SELECT COUNT(*) FROM task_sessions").Scan(&n); e != nil || n != 0 {
		t.Fatalf("agent task leaked into ordinary chat %d %v", n, e)
	}
}

func TestMVPShutdownSignalsWithoutWaitingForProductStateLock(t *testing.T) {
	s, _, _ := fixture(t)
	s.mvp.mu.Lock()
	done := make(chan struct{})
	go func() { s.BeginShutdown(); close(done) }()
	blocked := false
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		blocked = true
	}
	s.mvp.mu.Unlock()
	if blocked {
		<-done
		t.Fatal("shutdown waited on product state/database lock")
	}
}

func TestMVPFailedOutcomePersistenceShowsRecoveryAndNeverReplays(t *testing.T) {
	dir := t.TempDir()
	registry, e := tools.New(filepath.Join(dir, "workspace"))
	if e != nil {
		t.Fatal(e)
	}
	defer registry.Close()
	s, e := New(dir, registry, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	w := mvpWorkspace(t, s, dir)
	a := mustCall(t, s, "PrepareFileChange", map[string]string{"workspaceId": w.ID, "path": "committed.txt", "content": "committed evidence"}).(ProductAction)
	if _, e = s.db.Exec("CREATE TRIGGER fail_mvp_result BEFORE UPDATE OF status ON product_actions WHEN NEW.status='SUCCEEDED' BEGIN SELECT RAISE(FAIL,'result blocked'); END"); e != nil {
		t.Fatal(e)
	}
	if r := s.Call("ApproveProductAction", encode(map[string]string{"id": a.ID, "hash": a.Hash})); r.Error == nil {
		t.Fatal("unsaved success exposed")
	}
	b, e := os.ReadFile(filepath.Join(w.Root, "committed.txt"))
	if e != nil || string(b) != "committed evidence" {
		t.Fatalf("committed evidence %q %v", b, e)
	}
	state := mustCall(t, s, "GetMVPState", struct{}{}).(MVPState)
	if state.Actions[0].Status != "RECOVERY_REQUIRED" {
		t.Fatalf("unsaved outcome appears %s", state.Actions[0].Status)
	}
	if r := s.Call("ApproveProductAction", encode(map[string]string{"id": a.ID, "hash": a.Hash})); r.Error == nil {
		t.Fatal("unknown action replayed")
	}
	var settled int
	if e = s.db.QueryRow("SELECT settled FROM product_action_intents WHERE action_id=?", a.ID).Scan(&settled); e != nil || settled != 0 {
		t.Fatalf("unknown intent erased %d %v", settled, e)
	}
	if e = s.Close(); e == nil {
		t.Fatal("shutdown hid persistence failure")
	}
	reopened, e := New(dir, registry, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	state = mustCall(t, reopened, "GetMVPState", struct{}{}).(MVPState)
	if state.Actions[0].Status != "RECOVERY_REQUIRED" {
		t.Fatal(state.Actions[0])
	}
	if r := reopened.Call("ApproveProductAction", encode(map[string]string{"id": a.ID, "hash": a.Hash})); r.Error == nil {
		t.Fatal("restart replayed unknown action")
	}
}

func TestMVPRestartExpiresUnusedPermitAndRecoversConsumedIntent(t *testing.T) {
	dir := t.TempDir()
	registry, e := tools.New(filepath.Join(dir, "workspace"))
	if e != nil {
		t.Fatal(e)
	}
	defer registry.Close()
	s, e := New(dir, registry, nil)
	if e != nil {
		t.Fatal(e)
	}
	w := mvpWorkspace(t, s, dir)
	pending := mustCall(t, s, "PrepareFileChange", map[string]string{"workspaceId": w.ID, "path": "pending.txt", "content": "never"}).(ProductAction)
	consumed := mustCall(t, s, "PrepareFileChange", map[string]string{"workspaceId": w.ID, "path": "consumed.txt", "content": "never"}).(ProductAction)
	if _, e = s.db.Exec("UPDATE product_actions SET status='EXECUTING',consumed=1 WHERE id=?", consumed.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("INSERT INTO product_action_intents(action_id,proposal_json,hash,created_at,updated_at) VALUES(?,?,?,?,?)", consumed.ID, encode(consumed.Proposal), consumed.Hash, now(), now()); e != nil {
		t.Fatal(e)
	}
	// Closing does not invent an outcome for a consumed intent without a live worker.
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := New(dir, registry, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	state := mustCall(t, reopened, "GetMVPState", struct{}{}).(MVPState)
	for _, a := range state.Actions {
		if a.ID == consumed.ID && a.Status != "RECOVERY_REQUIRED" {
			t.Fatal(a)
		}
		if a.ID == pending.ID && a.Status == "PENDING" {
			t.Fatal("old permit survived session")
		}
		if r := reopened.Call("ApproveProductAction", encode(map[string]string{"id": a.ID, "hash": a.Hash})); r.Error == nil {
			t.Fatal("restart replayed action")
		}
	}
	assertAbsent(t, filepath.Join(w.Root, "pending.txt"))
	assertAbsent(t, filepath.Join(w.Root, "consumed.txt"))
}

func TestMVPPauseResumeRereadsProjectAndCancellationPersists(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	task := mustCall(t, s, "CreateAgentTask", map[string]string{"workspaceId": w.ID, "goal": "Review current project"}).(AgentTask)
	task = mustCall(t, s, "PauseAgentTask", map[string]string{"id": task.ID}).(AgentTask)
	if task.Status != "PAUSED" {
		t.Fatal(task)
	}
	if e := os.WriteFile(filepath.Join(w.Root, "new.txt"), []byte("new fact"), 0600); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.activeMVPControl("task:"+task.ID) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	task = mustCall(t, s, "ResumeAgentTask", map[string]string{"id": task.ID}).(AgentTask)
	deadline = time.Now().Add(30 * time.Second)
	for task.Status == "RUNNING" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		task = mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
	}
	if task.Status != "COMPLETED" {
		t.Fatal(task)
	}
	var report developer.Report
	if e := json.Unmarshal(task.Steps[0].Result, &report); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, entry := range report.Entries {
		if entry.Path == "new.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("resume retained stale project facts")
	}
	cancelled := mustCall(t, s, "CreateAgentTask", map[string]string{"workspaceId": w.ID, "goal": "Inspect project"}).(AgentTask)
	mustCall(t, s, "CancelAgentTask", map[string]string{"id": cancelled.ID})
	cancelled = mustCall(t, s, "GetAgentTask", map[string]string{"id": cancelled.ID}).(AgentTask)
	if cancelled.Status != "CANCELLED" {
		t.Fatal(cancelled)
	}
	if r := s.Call("ResumeAgentTask", encode(map[string]string{"id": cancelled.ID})); r.Error == nil {
		t.Fatal("cancelled task resumed")
	}
}

func TestMVPCommandRegistryAndNetworkConsentFailClosed(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	for _, id := range []string{"powershell", "go.test"} {
		r := s.Call("PrepareCommand", encode(map[string]any{"workspaceId": w.ID, "commandId": id, "args": []string{}, "network": false}))
		if r.Error == nil {
			t.Fatalf("unapproved executable/network %s accepted", id)
		}
	}
	var n int
	if e := s.db.QueryRow("SELECT COUNT(*) FROM product_actions").Scan(&n); e != nil || n != 0 {
		t.Fatalf("denied commands generated permits %d %v", n, e)
	}
}

func TestMVPCommandRejectsWorkspaceReplacementAfterPreview(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	a := mustCall(t, s, "PrepareCommand", map[string]any{"workspaceId": w.ID, "commandId": "go.version", "args": []string{}, "network": false}).(ProductAction)
	if e := os.Rename(w.Root, w.Root+"-old"); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(w.Root, 0700); e != nil {
		t.Fatal(e)
	}
	a = mustCall(t, s, "ApproveProductAction", map[string]string{"id": a.ID, "hash": a.Hash}).(ProductAction)
	if a.Status != "FAILED" || a.Error != "STALE_WORKSPACE" {
		t.Fatalf("new directory admitted as approved workspace: %#v", a)
	}
}

// A webview cannot register a path supplied in JSON, including through the picker method.
func TestMVPBridgeRejectsArbitraryWorkspaceRoot(t *testing.T) {
	s, _, _ := fixture(t)
	if r := s.Call("GetMVPState", "{}"); r.Error != nil {
		t.Fatalf("MVP state unavailable: %v", r.Error)
	}
	for _, method := range []string{"RegisterWorkspace", "PickWorkspace"} {
		if r := s.Call(method, `{"root":"C:\\Windows"}`); r.Error == nil {
			t.Fatal("arbitrary root accepted")
		}
	}
}

func TestMVPActionExactHashSingleConsumptionAndIndependentRead(t *testing.T) {
	s, dir, _ := fixture(t)
	root := filepath.Join(dir, "project")
	if e := os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	s.SetWorkspacePicker(func(context.Context) (string, error) { return root, nil })
	w := mvpObject(t, mustCall(t, s, "PickWorkspace", struct{}{}))
	a := mvpObject(t, mustCall(t, s, "PrepareFileChange", map[string]any{"workspaceId": w["id"], "path": "hello.txt", "content": "exact approved bytes\n"}))
	if r := s.Call("ApproveProductAction", encode(map[string]any{"id": a["id"], "hash": "wrong"})); r.Error == nil {
		t.Fatal("wrong hash accepted")
	}
	var wg sync.WaitGroup
	successes := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := s.Call("ApproveProductAction", encode(map[string]any{"id": a["id"], "hash": a["hash"]}))
			successes <- r.Error == nil
		}()
	}
	wg.Wait()
	close(successes)
	n := 0
	for ok := range successes {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("effect admitted %d times", n)
	}
	b, e := os.ReadFile(filepath.Join(root, "hello.txt"))
	if e != nil || string(b) != "exact approved bytes\n" {
		t.Fatalf("read evidence %q %v", b, e)
	}
	state := mvpObject(t, mustCall(t, s, "GetMVPState", struct{}{}))
	actions := state["actions"].([]any)
	if actions[0].(map[string]any)["status"] != "SUCCEEDED" {
		t.Fatal(actions)
	}
}
