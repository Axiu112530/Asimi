package app

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"asimi/internal/configuration"
)

func agent02TestRun(t *testing.T, handler http.HandlerFunc) (*Service, Workspace, AgentTask) {
	t.Helper()
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	if err := os.WriteFile(filepath.Join(w.Root, "calc.go"), []byte("package calc\nfunc Add(a,b int) int {return a-b}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	profile := aiTestProfile("agent-run-fixture", server.URL+"/v1")
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": profile})
	task := mustCall(t, s, "CreateAgentRun", agent02CreateInput{WorkspaceID: w.ID, Goal: "Fix Add", ProviderID: profile.ID, ModelID: "fixture", Paths: []string{"calc.go"}}).(AgentTask)
	return s, w, task
}

func TestAgent02ChangedContextCannotSpendRequest(t *testing.T) {
	var calls atomic.Int32
	s, w, task := agent02TestRun(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	p := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": task.ID}).(agent02Preview)
	if err := os.WriteFile(filepath.Join(w.Root, "calc.go"), []byte("package calc\n// changed after review\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reply := s.Call("PlanAgentRun", encode(map[string]string{"id": task.ID, "contextHash": p.Hash}))
	if reply.Error == nil || reply.Error.Code != "TASK_CONTEXT_CHANGED" || calls.Load() != 0 {
		t.Fatalf("changed source admitted: %v; calls=%d", reply.Error, calls.Load())
	}
	current := mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
	if len(current.Agent.Rounds) != 0 || current.Agent.ReservedUSD != 0 {
		t.Fatal("unreviewed request reserved budget")
	}
}

func TestAgent02ReviewIncludesSnapshottedLowerLimits(t *testing.T) {
	requests := make(chan string, 1)
	s, workspace, previous := agent02TestRun(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		requests <- string(body)
		w.WriteHeader(http.StatusBadRequest)
	})
	mustCall(t, s, "SaveAgentPreferences", map[string]any{"limits": configuration.AgentLimits{Requests: 1, StepsPerPlan: 1}})
	task := mustCall(t, s, "CreateAgentRun", agent02CreateInput{
		WorkspaceID: workspace.ID, Goal: "Fix Add", ProviderID: previous.Agent.ProviderID, ModelID: previous.Agent.ModelID, Paths: previous.Agent.Paths,
	}).(AgentTask)
	mustCall(t, s, "RestoreConfigurationDefaults", map[string]bool{"confirm": true})
	preview := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": task.ID}).(agent02Preview)
	if !strings.Contains(preview.Content, `"maxSteps":1,"finalizeOnly":true`) || preview.RemainingRequests != 1 {
		t.Fatalf("review did not explain the effective policy: %s", preview.Content)
	}
	if len(requests) != 0 {
		t.Fatal("preview contacted the provider without approval")
	}
	mustCall(t, s, "PlanAgentRun", map[string]string{"id": task.ID, "contextHash": preview.Hash})
	select {
	case request := <-requests:
		if !strings.Contains(request, `\"maxSteps\":1,\"finalizeOnly\":true`) || !strings.Contains(request, "last available request") {
			t.Fatalf("provider did not receive reviewed limits: %s", request)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reviewed loopback request did not reach the provider")
	}
}

func TestAgent02CannotAdmitPlanDuringStop(t *testing.T) {
	var calls atomic.Int32
	s, _, task := agent02TestRun(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	p := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": task.ID}).(agent02Preview)
	end := s.beginMVPStop()
	defer end()
	r := s.Call("PlanAgentRun", encode(map[string]string{"id": task.ID, "contextHash": p.Hash}))
	if r.Error == nil || r.Error.Code != "ACTION_CANCELLED" || calls.Load() != 0 {
		t.Fatalf("request admitted while stopping: %+v", r.Error)
	}
}

func TestAgent02CredentialRejectsChangedEndpointSnapshot(t *testing.T) {
	s, _, task := agent02TestRun(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider request") })
	p := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": task.ID}).(agent02Preview)
	profile := p.profile
	profile.BaseURL = "https://example.invalid/v1"
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": profile})
	if _, err := s.agent02Credential(t.Context(), p.profile); errorCode(err) != "TASK_CONTEXT_CHANGED" {
		t.Fatalf("old endpoint admitted: %v", err)
	}
}

func TestAgent02RecoveryUsesConsumedActionState(t *testing.T) {
	s, w, task := agent02TestRun(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider request") })
	stamp := now()
	a := ProductAction{ID: newID(), TaskID: task.ID, Kind: "file", Status: "EXECUTING", Hash: "fixture-hash", CreatedAt: stamp, UpdatedAt: stamp, ExpiresAt: stamp, Proposal: ProductProposal{WorkspaceID: w.ID, Root: w.Root}}
	task.Status = "PAUSED"
	task.Steps = append(task.Steps, AgentStep{ID: newID(), Kind: "file", Title: "fixture", Status: "EXECUTING", ActionID: a.ID})
	if err := s.transaction(func(tx *sql.Tx) error {
		if err := saveAgentTask(tx, task); err != nil {
			return err
		}
		if err := savePreparedProduct(tx, a, task.ID); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE product_actions SET consumed=1 WHERE id=?", a.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"id": task.ID, "acknowledgeRecovery": true}
	if r := s.Call("ResumeAgentRun", encode(input)); r.Error == nil || r.Error.Code != "RECOVERY_REQUIRED" {
		t.Fatalf("resumed unresolved execution: %+v", r.Error)
	}
	if err := s.initializeMVP(); err != nil {
		t.Fatal(err)
	}
	recovered := mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
	if recovered.Steps[0].Status != "RECOVERY_REQUIRED" {
		t.Fatal("paused task did not reconcile with action journal")
	}
	input["acknowledgeRecovery"] = false
	if r := s.Call("ResumeAgentRun", encode(input)); r.Error == nil || r.Error.Code != "AGENT_ACKNOWLEDGE_RECOVERY" {
		t.Fatalf("resumed unacknowledged outcome: %+v", r.Error)
	}
	input["acknowledgeRecovery"] = true
	resumed := mustCall(t, s, "ResumeAgentRun", input).(AgentTask)
	if resumed.Status != "NEEDS_REVIEW" || resumed.Steps[0].Status != "RECOVERY_REQUIRED" {
		t.Fatal("resume erased the outcome or replayed an action")
	}
}

func TestAgent02PauseCancelsInFlightPlanAndKeepsSpentRound(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	s, _, task := agent02TestRun(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-time.After(5 * time.Second):
		}
	})
	p := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": task.ID}).(agent02Preview)
	mustCall(t, s, "PlanAgentRun", map[string]string{"id": task.ID, "contextHash": p.Hash})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("loopback planner did not start")
	}
	mustCall(t, s, "PauseAgentTask", map[string]string{"id": task.ID})
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("planner request did not cancel")
	}
	deadline := time.Now().Add(3 * time.Second)
	for s.activeMVPControl("task:"+task.ID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	resumed := mustCall(t, s, "ResumeAgentRun", map[string]string{"id": task.ID}).(AgentTask)
	if resumed.Status != "NEEDS_REVIEW" || len(resumed.Agent.Rounds) != 1 || resumed.Agent.ReservedUSD <= 0 {
		t.Fatalf("pause lost reservation: %+v", resumed.Agent)
	}
	if actions := mustCall(t, s, "GetMVPState", struct{}{}).(MVPState).Actions; len(actions) != 0 {
		t.Fatalf("interrupted plan produced actions: %d", len(actions))
	}
}

func TestV6MigrationRollbackAndUpgrade(t *testing.T) {
	dir := t.TempDir()
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TABLE computer_task_evidence; PRAGMA user_version=5; CREATE TABLE computer_task_evidence (conflict TEXT);`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if db, err = openStore(dir); err == nil {
		db.Close()
		t.Fatal("conflicting v6 migration was accepted")
	}
	db, err = sql.Open("sqlite", filepath.Join(dir, "asimi.db"))
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatalf("version after rollback = %d, %v", version, err)
	}
	if _, err = db.Exec("DROP TABLE computer_task_evidence"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 6 {
		t.Fatalf("version after migration = %d, %v", version, err)
	}
}
