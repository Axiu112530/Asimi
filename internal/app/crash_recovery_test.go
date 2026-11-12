package app

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"asimi/internal/domain"
	"asimi/internal/provider"
	"asimi/internal/tools"
)

const crashProfile = "ASIMI_CRASH_FIXTURE"
const crashBoundary = "ASIMI_CRASH_BOUNDARY"
const crashContent = "Phase 2A crash fixture\n实际内容"

func crashFixture(t *testing.T, dir string) (*Service, func()) {
	t.Helper()
	registry, err := tools.New(filepath.Join(dir, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(dir, registry, nil)
	if err != nil {
		registry.Close()
		t.Fatal(err)
	}
	return service, func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
		if err := registry.Close(); err != nil {
			t.Error(err)
		}
	}
}

// The child deliberately exits with OS cleanup only. No application defer,
// graceful shutdown, or test cleanup runs across the selected boundary.
func TestCrashFixtureProcess(t *testing.T) {
	dir := os.Getenv(crashProfile)
	if dir == "" {
		return
	}
	s, closeFixture := crashFixture(t, dir)
	defer closeFixture()
	point := os.Getenv(crashBoundary)
	var taskID string
	s.checkpoint = func(reached string) {
		if point == "cancelled-after-commit" && reached == "file-committed" {
			mustCall(t, s, "CancelTask", map[string]string{"id": taskID})
			os.Exit(77)
		}
		if reached == point {
			os.Exit(77)
		}
	}
	task := sendFile(t, s, "result.txt", crashContent)
	taskID = task.ID
	mustCall(t, s, "ApproveAction", map[string]string{"id": task.Approval.ID})
	awaitTerminal(t, s, task.ID)
	t.Fatalf("fault injection point %q did not exit", point)
}

func TestCrashAfterCancellationPreservesCommittedFile(t *testing.T) {
	dir := t.TempDir()
	crashAt(t, dir, "cancelled-after-commit")
	s, closeFixture := crashFixture(t, dir)
	defer closeFixture()
	task := onlyRecoveredTask(t, s)
	if task.Status != "CANCELLED" || task.Verification == nil || !task.Verification.Created || !task.Verification.Passed {
		t.Fatalf("recovery lost cancellation or committed evidence: %+v", task)
	}
}

func crashAt(t *testing.T, dir, point string) {
	t.Helper()
	runInterruptedFixture(t, "^TestCrashFixtureProcess$", crashProfile+"="+dir, crashBoundary+"="+point)
}

func onlyRecoveredTask(t *testing.T, s *Service) domain.Task {
	t.Helper()
	tasks := mustCall(t, s, "ListTasks", struct{}{}).([]domain.Task)
	if len(tasks) != 1 {
		t.Fatalf("wanted one original task, got %d", len(tasks))
	}
	return tasks[0]
}

func TestCrashRecoveryBoundaries(t *testing.T) {
	points := []struct {
		point       string
		committed   bool
		hadApproval bool
		consumed    bool
	}{
		{"task-created", false, false, false},
		{"approval-persisted", false, true, false},
		{"approval-consumed", false, true, true},
		{"temp-written", false, true, true},
		{"file-committed", true, true, true},
		{"file-verified", true, true, true},
		{"result-persisted", true, true, true},
	}
	for _, fixture := range points {
		t.Run(fixture.point, func(t *testing.T) {
			dir := t.TempDir()
			crashAt(t, dir, fixture.point)
			path := filepath.Join(dir, "workspace", "result.txt")
			// Check the effect independently before recovery runs.
			if fixture.committed {
				bytes, err := os.ReadFile(path)
				if err != nil || string(bytes) != crashContent {
					t.Fatalf("committed fixture missing or changed: %v", err)
				}
			} else {
				assertAbsent(t, path)
			}
			if fixture.point == "temp-written" {
				pattern := filepath.Join(dir, "workspace", ".asimi-*.tmp")
				if files, err := filepath.Glob(pattern); err != nil || len(files) != 1 {
					t.Fatalf("temporary-write injection did not leave its staged file: %v %v", files, err)
				}
			}
			auditCount := 0
			for restart := 0; restart < 2; restart++ {
				s, closeFixture := crashFixture(t, dir)
				defer closeFixture()
				task := onlyRecoveredTask(t, s)
				if fixture.committed {
					if task.Status != "COMPLETED" || task.Verification == nil || !task.Verification.Passed || !task.Verification.Created {
						t.Fatalf("committed identity was not reconciled: %+v", task)
					}
				} else {
					if task.Status != "PAUSED" {
						t.Fatalf("interrupted operation status = %s", task.Status)
					}
					assertAbsent(t, path)
				}
				if (task.Approval != nil) != fixture.hadApproval {
					t.Fatal("approval history changed")
				}
				if task.Approval != nil {
					want := "INVALIDATED"
					if fixture.consumed {
						want = "CONSUMED"
					}
					if task.Approval.Status != want {
						t.Fatalf("approval = %s, want %s", task.Approval.Status, want)
					}
					if r := s.Call("ApproveAction", encode(map[string]string{"id": task.Approval.ID})); r.Error == nil {
						t.Fatal("interrupted approval replayed")
					}
				}
				executions := mustCall(t, s, "GetToolExecutions", struct{}{}).([]domain.ToolExecution)
				wantExecutions := 0
				if fixture.consumed {
					wantExecutions = 1
				}
				if len(executions) != wantExecutions {
					t.Fatalf("original executions = %d, want %d", len(executions), wantExecutions)
				}
				count := len(mustCall(t, s, "GetAuditEvents", struct{}{}).([]domain.AuditEvent))
				if restart > 0 && count != auditCount {
					t.Fatalf("restart duplicated audit rows: %d -> %d", auditCount, count)
				}
				auditCount = count
				closeFixture()
			}
		})
	}
}

func TestCrashRecoveryKeepsAmbiguousResults(t *testing.T) {
	for _, mode := range []string{"same-name-same-hash", "same-identity-changed-content", "missing-target", "legacy-v1"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			crashAt(t, dir, "file-committed")
			path := filepath.Join(dir, "workspace", "result.txt")
			switch mode {
			case "same-name-same-hash", "missing-target":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if mode == "same-name-same-hash" {
					if err := os.WriteFile(path, []byte(crashContent), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "same-identity-changed-content":
				if err := os.WriteFile(path, []byte("Changed in isolated fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			case "legacy-v1":
				db, err := sql.Open("sqlite", filepath.Join(dir, "asimi.db"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`DROP TABLE computer_task_evidence; DROP TABLE product_action_journal; DROP TABLE product_action_intents; DROP TABLE product_actions; DROP TABLE agent_task_steps; DROP TABLE agent_tasks; DROP TABLE workspaces; DROP TABLE skill_settings; DROP TABLE onboarding_state; DROP TABLE computer_action_events; DROP TABLE computer_approvals; DROP TABLE computer_actions; DROP TABLE ai_attachment_cleanup; DROP TABLE ai_message_attachments; DROP TABLE ai_attachments; DROP TABLE ai_usage; DROP TABLE ai_requests; DROP TABLE ai_message_metadata; DROP TABLE ai_conversations; DROP TABLE ai_settings; DROP TABLE ai_credentials; DROP TABLE ai_models; DROP TABLE ai_profiles; DROP TABLE file_intents; PRAGMA user_version=1`); err != nil {
					t.Fatal(err)
				}
				db.Close()
			}
			s, closeFixture := crashFixture(t, dir)
			defer closeFixture()
			task := onlyRecoveredTask(t, s)
			if task.Status != "RECOVERY_REQUIRED" || task.Approval == nil || task.Approval.Status != "CONSUMED" ||
				(task.Verification != nil && task.Verification.Passed) {
				t.Fatalf("uncertainty was erased: %+v", task)
			}
			mustCall(t, s, "StopAll", struct{}{})
			if got := onlyRecoveredTask(t, s); got.Status != "RECOVERY_REQUIRED" {
				t.Fatal("StopAll erased the recovery requirement")
			}
			if mode == "missing-target" {
				assertAbsent(t, path)
			} else {
				bytes, err := os.ReadFile(path)
				if err != nil || len(bytes) == 0 || (strings.Contains(mode, "changed-content") && string(bytes) != "Changed in isolated fixture") {
					t.Fatalf("recovery modified existing file: %v", err)
				}
			}
		})
	}
}

type shutdownExecutor struct {
	executor
	point   string
	entered chan struct{}
}

func (r shutdownExecutor) Execute(ctx context.Context, op domain.Operation, consume func(context.Context, domain.Operation) error, hooks ...tools.ExecutionHooks) (domain.Verification, error) {
	waitForShutdown := func() {
		close(r.entered)
		<-ctx.Done()
	}
	switch r.point {
	case "after-approval":
		original := consume
		consume = func(ctx context.Context, op domain.Operation) error {
			waitForShutdown()
			return original(ctx, op)
		}
	case "before-commit":
		record := hooks[0].Record
		hooks[0].Record = func(witness domain.FileWitness) error {
			if err := record(witness); err != nil {
				return err
			}
			waitForShutdown()
			return nil
		}
	case "after-commit":
		hooks[0].Committed = waitForShutdown
	}
	return r.executor.Execute(ctx, op, consume, hooks...)
}

func TestShutdownDrainsActiveExecutionBeforeClosingSQLite(t *testing.T) {
	for _, point := range []string{"after-approval", "before-commit", "after-commit"} {
		t.Run(point, func(t *testing.T) {
			dir := t.TempDir()
			s, closeFixture := crashFixture(t, dir)
			defer closeFixture()
			task := sendFile(t, s, "shutdown.txt", crashContent)
			r := shutdownExecutor{executor: s.registry, point: point, entered: make(chan struct{})}
			s.registry = r
			mustCall(t, s, "ApproveAction", map[string]string{"id": task.Approval.ID})
			select {
			case <-r.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not reach shutdown boundary")
			}
			s.BeginShutdown()
			s.BeginShutdown()
			if result := s.Call("SendMessage", `{"content":"Create file \"late.txt\" with content \"late\""}`); result.Error == nil {
				t.Fatal("shutdown accepted new work")
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			closeFixture()
			reopened, closeReopened := crashFixture(t, dir)
			defer closeReopened()
			task = onlyRecoveredTask(t, reopened)
			committed := point == "after-commit"
			if task.Status != "CANCELLED" || task.Verification == nil || task.Verification.Created != committed {
				t.Fatalf("shutdown lost actual outcome: %+v", task)
			}
			x := mustCall(t, reopened, "GetToolExecutions", struct{}{}).([]domain.ToolExecution)
			if len(x) != 1 || x[0].Status != "CANCELLED" || x[0].Result == nil || x[0].Result.Created != committed {
				t.Fatalf("execution record was not drained: %+v", x)
			}
			path := filepath.Join(dir, "workspace", "shutdown.txt")
			if committed {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != crashContent {
					t.Fatalf("committed file was not preserved: %v", err)
				}
			} else {
				assertAbsent(t, path)
			}
			files, err := filepath.Glob(filepath.Join(dir, "workspace", ".asimi-*.tmp"))
			if err != nil || len(files) != 0 {
				t.Fatalf("graceful cancel left staged files: %v %v", files, err)
			}
			auditCount := len(mustCall(t, reopened, "GetAuditEvents", struct{}{}).([]domain.AuditEvent))
			for i := 0; i < 2; i++ {
				mustCall(t, reopened, "CancelTask", map[string]string{"id": task.ID})
			}
			if len(mustCall(t, reopened, "GetAuditEvents", struct{}{}).([]domain.AuditEvent)) != auditCount {
				t.Fatal("repeated cancellation wrote new audit records")
			}
		})
	}
}

func TestFailedFinalCommitKeepsRecoveryEvidence(t *testing.T) {
	dir := t.TempDir()
	registry, err := tools.New(filepath.Join(dir, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	s, err := New(dir, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.db.Exec(`CREATE TABLE fault_result (
		id TEXT REFERENCES conversations(id) DEFERRABLE INITIALLY DEFERRED
	);
	CREATE TRIGGER inject_final_commit_failure
	BEFORE UPDATE ON task_sessions WHEN NEW.status='COMPLETED'
	BEGIN INSERT INTO fault_result VALUES('missing'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	task := sendFile(t, s, "final.txt", crashContent)
	mustCall(t, s, "ApproveAction", map[string]string{"id": task.Approval.ID})
	task = awaitTerminal(t, s, task.ID)
	if task.Status != "RECOVERY_REQUIRED" || task.ErrorCode != "STORAGE_CONSTRAINT" {
		t.Fatalf("failed commit presented as durable completion: %+v", task)
	}
	data, err := os.ReadFile(filepath.Join(dir, "workspace", "final.txt"))
	if err != nil || string(data) != crashContent {
		t.Fatalf("file commit was not preserved: %v", err)
	}
	var status string
	var settled int
	err = s.db.QueryRow(`SELECT t.status,i.settled FROM task_sessions t JOIN file_intents i ON i.task_id=t.id WHERE t.id=?`, task.ID).Scan(&status, &settled)
	if err != nil || status != "RUNNING" || settled != 0 {
		t.Fatalf("partial final transaction persisted: status=%s settled=%d err=%v", status, settled, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER inject_final_commit_failure"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err == nil {
		t.Fatal("shutdown hid worker persistence failure")
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, closeReopened := crashFixture(t, dir)
	defer closeReopened()
	task = onlyRecoveredTask(t, reopened)
	if task.Status != "COMPLETED" || task.Verification == nil || !task.Verification.Passed {
		t.Fatalf("restart did not reconcile the original file: %+v", task)
	}
}

func TestFailedIntentInsertDoesNotConsumeApproval(t *testing.T) {
	dir := t.TempDir()
	s, closeFixture := crashFixture(t, dir)
	defer closeFixture()
	_, err := s.db.Exec(`CREATE TRIGGER inject_intent_failure BEFORE INSERT ON file_intents
		BEGIN SELECT RAISE(ABORT,'fixture intent failure'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	task := sendFile(t, s, "blocked.txt", crashContent)
	mustCall(t, s, "ApproveAction", map[string]string{"id": task.Approval.ID})
	task = awaitTerminal(t, s, task.ID)
	if task.Status != "FAILED" || task.ErrorCode != "STORAGE_CONSTRAINT" ||
		task.Approval == nil || task.Approval.Status != "INVALIDATED" {
		t.Fatalf("failed consume was misreported: %+v", task)
	}
	for _, event := range mustCall(t, s, "GetAuditEvents", struct{}{}).([]domain.AuditEvent) {
		if event.TaskID == task.ID && event.Code == "APPROVAL_CONSUMED" {
			t.Fatal("rolled-back approval was recorded as consumed")
		}
	}
	assertAbsent(t, filepath.Join(dir, "workspace", "blocked.txt"))
}

type delayedPlanner struct {
	provider.Provider
	started chan struct{}
}

func (p delayedPlanner) Generate(ctx context.Context, req provider.Request) (provider.Result, error) {
	close(p.started)
	<-ctx.Done()
	return p.Provider.Generate(ctx, req)
}

func TestCancelDuringPlanningDoesNotNeedPlannerResponse(t *testing.T) {
	dir := t.TempDir()
	s, closeFixture := crashFixture(t, dir)
	defer closeFixture()
	planner := delayedPlanner{Provider: s.provider, started: make(chan struct{})}
	s.provider = planner
	finished := make(chan domain.Reply, 1)
	go func() {
		finished <- s.Call("SendMessage", encode(map[string]string{"content": `Create file "not-planned.txt" with content "never"`}))
	}()
	select {
	case <-planner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("task did not reach planning")
	}
	task := onlyRecoveredTask(t, s)
	if task.Status != "PLANNING" {
		t.Fatalf("wanted PLANNING, got %s", task.Status)
	}
	mustCall(t, s, "CancelTask", map[string]string{"id": task.ID})
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel signal did not release planner")
	}
	task = onlyRecoveredTask(t, s)
	if task.Status != "CANCELLED" || task.Approval != nil {
		t.Fatalf("cancelled planning produced an approval: %+v", task)
	}
	assertAbsent(t, filepath.Join(dir, "workspace", "not-planned.txt"))
}
