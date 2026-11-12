package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"deskpilot/internal/domain"
	"deskpilot/internal/tools"
)

func fixture(t *testing.T) (*Service, string, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	r, e := tools.New(filepath.Join(dir, "workspace"))
	if e != nil {
		t.Fatal(e)
	}
	events := &atomic.Int32{}
	s, e := New(dir, r, func(name string, _ any) {
		if name == "deskpilot:changed" {
			events.Add(1)
		}
	})
	if e != nil {
		r.Close()
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := s.Close(); e != nil {
			t.Error(e)
		}
		r.Close()
	})
	return s, dir, events
}
func mustCall(t *testing.T, s *Service, method string, payload any) any {
	t.Helper()
	r := s.Call(method, encode(payload))
	if r.Error != nil {
		t.Fatalf("%s: %#v", method, r.Error)
	}
	return r.Data
}
func sendFile(t *testing.T, s *Service, name, content string) domain.Task {
	t.Helper()
	ids := mustCall(t, s, "SendMessage", map[string]string{"content": encode(map[string]string{"fileName": name, "content": content})}).(map[string]string)
	return mustCall(t, s, "GetTask", map[string]string{"id": ids["taskId"]}).(domain.Task)
}
func awaitTerminal(t *testing.T, s *Service, id string) domain.Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task := mustCall(t, s, "GetTask", map[string]string{"id": id}).(domain.Task)
		if terminal(task.Status) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return domain.Task{}
}
func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatalf("unexpected file %s: %v", path, e)
	}
}

func TestApprovalGateRejectCancelAndExactBinding(t *testing.T) {
	s, dir, _ := fixture(t)
	for _, mode := range []string{"reject", "cancel", "tamper"} {
		t.Run(mode, func(t *testing.T) {
			task := sendFile(t, s, mode+".txt", "Hello")
			if task.Status != "WAITING_APPROVAL" || task.Approval == nil {
				t.Fatalf("task %#v", task)
			}
			if e := s.consume(context.Background(), task.Approval.Operation); e == nil {
				t.Fatal("pending approval was consumed")
			}
			switch mode {
			case "reject":
				mustCall(t, s, "RejectAction", map[string]string{"id": task.Approval.ID})
			case "cancel":
				mustCall(t, s, "CancelTask", map[string]string{"id": task.ID})
			case "tamper":
				op := task.Approval.Operation
				op.Content = "forged"
				if e := s.consume(context.Background(), op); e == nil {
					t.Fatal("changed operation was accepted")
				}
				mustCall(t, s, "RejectAction", map[string]string{"id": task.Approval.ID})
			}
			if r := s.Call("ApproveAction", encode(map[string]string{"id": task.Approval.ID})); r.Error == nil {
				t.Fatal("invalid approval accepted")
			}
			finalTask := mustCall(t, s, "GetTask", map[string]string{"id": task.ID}).(domain.Task)
			for _, step := range finalTask.Steps {
				if step.Status == "PENDING" || step.Status == "RUNNING" {
					t.Fatalf("terminal task leaves queued step: %#v", finalTask)
				}
			}
			assertAbsent(t, filepath.Join(dir, "workspace", mode+".txt"))
		})
	}
}

func TestCreateVerifiedDurableAndNoOverwrite(t *testing.T) {
	s, dir, events := fixture(t)
	task := sendFile(t, s, "notes.txt", "你好, world\n")
	mustCall(t, s, "ApproveAction", map[string]string{"id": task.Approval.ID})
	task = awaitTerminal(t, s, task.ID)
	if task.Status != "COMPLETED" || task.Verification == nil || !task.Verification.Passed || !task.Verification.Created {
		t.Fatalf("task %#v", task)
	}
	b, e := os.ReadFile(filepath.Join(dir, "workspace", "notes.txt"))
	if e != nil || string(b) != "你好, world\n" {
		t.Fatalf("bytes %q %v", b, e)
	}
	if task.Approval.Status != "CONSUMED" {
		t.Fatal(task.Approval.Status)
	}
	if r := s.Call("ApproveAction", encode(map[string]string{"id": task.Approval.ID})); r.Error == nil {
		t.Fatal("replay accepted")
	}
	second := sendFile(t, s, "notes.txt", "replacement")
	if second.Status == "WAITING_APPROVAL" {
		mustCall(t, s, "ApproveAction", map[string]string{"id": second.Approval.ID})
		second = awaitTerminal(t, s, second.ID)
	}
	if second.Status != "FAILED" || second.ErrorCode != "FILE_EXISTS" {
		t.Fatalf("overwrite task %#v", second)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "workspace", "notes.txt"))
	if string(b) != "你好, world\n" {
		t.Fatal("file overwritten")
	}
	if events.Load() < 5 {
		t.Fatal("durable event notifications missing")
	}
	if executions := mustCall(t, s, "GetToolExecutions", struct{}{}).([]domain.ToolExecution); len(executions) != 2 || executions[0].Status != "FAILED" || executions[1].Status != "COMPLETED" {
		t.Fatal(executions)
	}
}

func TestUnsupportedRequestsDoNotReportCompletion(t *testing.T) {
	s, _, _ := fixture(t)
	for _, input := range []string{"Take a screenshot", "Run PowerShell"} {
		ids := mustCall(t, s, "SendMessage", map[string]string{"content": input}).(map[string]string)
		task := mustCall(t, s, "GetTask", map[string]string{"id": ids["taskId"]}).(domain.Task)
		if task.Status != "FAILED" || task.ErrorCode != "UNSUPPORTED_REQUEST" || task.Summary == "" || task.Approval != nil {
			t.Fatalf("unsupported request reported an invalid outcome: %#v", task)
		}
		if task.Steps[1].Status != "SKIPPED" || task.Steps[2].Status != "SKIPPED" {
			t.Fatalf("unsupported request scheduled tool steps: %#v", task.Steps)
		}
	}
	if executions := mustCall(t, s, "GetToolExecutions", struct{}{}).([]domain.ToolExecution); len(executions) != 0 {
		t.Fatalf("unsupported request executed a tool: %#v", executions)
	}
}

func TestGuidedJSONUsesHumanTitleAndPreservesExactRequest(t *testing.T) {
	s, _, _ := fixture(t)
	settings := domain.Settings{AppLanguage: "zh-CN", AgentLanguage: "zh-CN", Theme: "light", Startup: "welcome"}
	mustCall(t, s, "UpdateSettings", settings)
	input := `{"fileName":"notes.txt","content":"exact content"}`
	ids := mustCall(t, s, "SendMessage", map[string]string{"content": input}).(map[string]string)
	task := mustCall(t, s, "GetTask", map[string]string{"id": ids["taskId"]}).(domain.Task)
	conversation := mustCall(t, s, "GetConversation", map[string]string{"id": ids["conversationId"]}).(domain.Conversation)
	if task.Title != "创建 notes.txt" || conversation.Title != task.Title || task.Description != input || conversation.Messages[0].Content != input || task.Approval.Operation.Content != "exact content" {
		t.Fatalf("guided title altered request data: %#v %#v", task, conversation)
	}
}

func TestMessageTimestampTiesPreserveInsertionOrder(t *testing.T) {
	s, _, _ := fixture(t)
	stamp := now()
	if _, e := s.db.Exec("INSERT INTO conversations(id,title,created_at,updated_at) VALUES('timestamp-tie','Title',?,?)", stamp, stamp); e != nil {
		t.Fatal(e)
	}
	for _, m := range []domain.Message{
		{ID: "z-first", ConversationID: "timestamp-tie", Role: "user", Content: "first", CreatedAt: stamp},
		{ID: "a-second", ConversationID: "timestamp-tie", Role: "assistant", Content: "second", CreatedAt: stamp},
	} {
		if _, e := s.db.Exec("INSERT INTO messages(id,conversation_id,role,content,created_at,updated_at) VALUES(?,?,?,?,?,?)", m.ID, m.ConversationID, m.Role, m.Content, m.CreatedAt, m.CreatedAt); e != nil {
			t.Fatal(e)
		}
	}
	c := mustCall(t, s, "GetConversation", map[string]string{"id": "timestamp-tie"}).(domain.Conversation)
	if len(c.Messages) != 2 || c.Messages[0].Content != "first" || c.Messages[1].Content != "second" {
		t.Fatal("equal timestamps reordered messages", c.Messages)
	}
}

func TestUnrelatedCommandDoesNotFillPendingFileDraft(t *testing.T) {
	s, _, _ := fixture(t)
	ids := mustCall(t, s, "SendMessage", map[string]string{"content": `Create file "draft.txt"`}).(map[string]string)
	ids = mustCall(t, s, "SendMessage", map[string]string{"conversationId": ids["conversationId"], "content": `Run PowerShell "Get-Process"`}).(map[string]string)
	task := mustCall(t, s, "GetTask", map[string]string{"id": ids["taskId"]}).(domain.Task)
	if task.Status != "FAILED" || task.ErrorCode != "UNSUPPORTED_REQUEST" || task.Approval != nil {
		t.Fatal(task)
	}
	if executions := mustCall(t, s, "GetToolExecutions", struct{}{}).([]domain.ToolExecution); len(executions) != 0 {
		t.Fatal(executions)
	}
}

func TestTraversalAndImmutableBusinessBinding(t *testing.T) {
	s, dir, _ := fixture(t)
	task := sendFile(t, s, "../outside.txt", "escape")
	if task.Status != "FAILED" || task.ErrorCode != "PATH_DENIED" {
		t.Fatalf("task %#v", task)
	}
	if task.Steps[1].Status != "SKIPPED" || task.Steps[2].Status != "SKIPPED" {
		t.Fatalf("failed plan leaves queued tool steps: %#v", task.Steps)
	}
	assertAbsent(t, filepath.Join(dir, "outside.txt"))
	for _, method := range []string{"Execute", "ReadFile", "UpdateTask", "RunCommand"} {
		if r := s.Call(method, "{}"); r.Error == nil || r.Error.Code != "METHOD_NOT_ALLOWED" {
			t.Fatalf("binding %s %#v", method, r)
		}
	}
	if r := s.Call("ApproveAction", `{"id":"x","operation":{"content":"forged"}}`); r.Error == nil || r.Error.Code != "INVALID_REQUEST" {
		t.Fatal(r)
	}
}

func TestExpiredApprovalCannotExecute(t *testing.T) {
	s, dir, _ := fixture(t)
	task := sendFile(t, s, "expired.txt", "never")
	s.mu.Lock()
	_, e := s.db.Exec("UPDATE approval_records SET expires_at=? WHERE id=?", time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano), task.Approval.ID)
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	r := s.Call("ApproveAction", encode(map[string]string{"id": task.Approval.ID}))
	if r.Error == nil || r.Error.Code != "APPROVAL_EXPIRED" {
		t.Fatal(r)
	}
	task = mustCall(t, s, "GetTask", map[string]string{"id": task.ID}).(domain.Task)
	if task.Status != "FAILED" || task.Approval.Status != "EXPIRED" {
		t.Fatal(task)
	}
	if task.Steps[2].Status != "SKIPPED" {
		t.Fatalf("expiry leaves verification queued: %#v", task.Steps)
	}
	assertAbsent(t, filepath.Join(dir, "workspace", "expired.txt"))
}

type alteredExecutor struct {
	executor
	after   func()
	corrupt bool
}

func (a alteredExecutor) Execute(ctx context.Context, op domain.Operation, consume func(context.Context, domain.Operation) error, hooks ...tools.ExecutionHooks) (domain.Verification, error) {
	v, e := a.executor.Execute(ctx, op, consume, hooks...)
	if a.after != nil {
		a.after()
	}
	if a.corrupt {
		v.ActualHash = strings.Repeat("0", 64)
	}
	return v, e
}
func TestUnverifiedNeverCompletesAndCommittedCancellationIsHonest(t *testing.T) {
	for _, mode := range []string{"corrupt", "cancel-committed"} {
		t.Run(mode, func(t *testing.T) {
			s, dir, _ := fixture(t)
			task := sendFile(t, s, mode+".txt", "preserve")
			wrapper := alteredExecutor{executor: s.registry, corrupt: mode == "corrupt"}
			if mode == "cancel-committed" {
				wrapper.after = func() { mustCall(t, s, "CancelTask", map[string]string{"id": task.ID}) }
			}
			s.mu.Lock()
			s.registry = wrapper
			s.mu.Unlock()
			mustCall(t, s, "ApproveAction", map[string]string{"id": task.Approval.ID})
			task = awaitTerminal(t, s, task.ID)
			// Cancel returns before the independent worker reports its committed result.
			deadline := time.Now().Add(time.Second)
			for task.Verification == nil && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
				task = mustCall(t, s, "GetTask", map[string]string{"id": task.ID}).(domain.Task)
			}
			if task.Verification == nil || !task.Verification.Created {
				t.Fatal("lost committed file evidence", task)
			}
			if mode == "corrupt" && (task.Status != "FAILED" || task.ErrorCode != "VERIFICATION_FAILED") {
				t.Fatal(task)
			}
			if mode == "cancel-committed" && (task.Status != "CANCELLED" || !strings.Contains(task.Summary, "committed")) {
				t.Fatal(task)
			}
			b, e := os.ReadFile(filepath.Join(dir, "workspace", mode+".txt"))
			if e != nil || string(b) != "preserve" {
				t.Fatalf("committed file changed %q %v", b, e)
			}
		})
	}
}

func TestClarificationAndSettingsPersist(t *testing.T) {
	s, dir, _ := fixture(t)
	settings := domain.Settings{AppLanguage: "zh-CN", AgentLanguage: "en-US", Theme: "dark", Startup: "last"}
	mustCall(t, s, "UpdateSettings", settings)
	ids := mustCall(t, s, "SendMessage", map[string]string{"content": `Create file "draft.txt"`}).(map[string]string)
	task := mustCall(t, s, "GetTask", map[string]string{"id": ids["taskId"]}).(domain.Task)
	if task.Status != "PAUSED" || task.ErrorCode != "CLARIFICATION_REQUIRED" {
		t.Fatal(task)
	}
	ids = mustCall(t, s, "SendMessage", map[string]string{"conversationId": ids["conversationId"], "content": `Content is ""`}).(map[string]string)
	task = mustCall(t, s, "GetTask", map[string]string{"id": ids["taskId"]}).(domain.Task)
	if task.Status != "WAITING_APPROVAL" || task.Approval.Operation.Content != "" || task.Approval.Operation.FileName != "draft.txt" {
		t.Fatal(task)
	}
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	r, e := tools.New(filepath.Join(dir, "workspace"))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	restored, e := New(dir, r, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	got := mustCall(t, restored, "GetSettings", struct{}{}).(domain.Settings)
	if got != settings {
		t.Fatal(got)
	}
	c := mustCall(t, restored, "GetConversation", map[string]string{"id": ids["conversationId"]}).(domain.Conversation)
	if len(c.Messages) < 4 || len(c.Tasks) != 2 {
		t.Fatal(c)
	}
	bytes, e := json.Marshal(c)
	if e != nil || !strings.Contains(string(bytes), "draft.txt") {
		t.Fatal("history missing", e)
	}
}

func TestInterruptedTaskPausesAndInvalidatesApproval(t *testing.T) {
	s, dir, _ := fixture(t)
	task := sendFile(t, s, "interrupted.txt", "never resume")
	// Simulate process termination without the normal StopAll lifecycle. The
	// already durable waiting task and pending approval must survive as history.
	s.closeOnce.Do(func() {
		s.BeginShutdown()
		s.wg.Wait()
		s.closeErr = s.db.Close()
		if e := s.releaseProfile(); s.closeErr == nil {
			s.closeErr = e
		}
	})
	r, e := tools.New(filepath.Join(dir, "workspace"))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	recovered, e := New(dir, r, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	got := mustCall(t, recovered, "GetTask", map[string]string{"id": task.ID}).(domain.Task)
	if got.Status != "PAUSED" || got.ErrorCode != "INTERRUPTED" || got.Approval.Status != "INVALIDATED" {
		t.Fatal(got)
	}
	if reply := recovered.Call("ApproveAction", encode(map[string]string{"id": task.Approval.ID})); reply.Error == nil {
		t.Fatal("interrupted approval resumed")
	}
	assertAbsent(t, filepath.Join(dir, "workspace", "interrupted.txt"))
}
