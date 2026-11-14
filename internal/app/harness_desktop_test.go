package app

import (
	"asimi/internal/domain"
	"asimi/internal/tools"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"asimi/internal/computer"
)

func TestHarnessDesktopTaskPendingTerminalNotifiesTaskView(t *testing.T) {
	for _, mode := range []string{"expire", "reject", "stop", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, dir, _ := fixture(t)
			w := mvpWorkspace(t, s, dir)
			s.desktopControl.driver = newControlFixtureDriver()
			o := observeControlFixture(t, s)
			task, p, err := s.createDesktopTask(desktopTaskInput{WorkspaceID: w.ID, Type: computer.SetValue,
				ObservationID: o.ID, ElementID: o.Elements[0].ID, Text: "Pending fixture"})
			if err != nil {
				t.Fatal(err)
			}
			events := make(chan string, 16)
			s.emit = func(name string, value any) {
				if name == "asimi:changed" {
					if ids, ok := value.(map[string]string); ok {
						select {
						case events <- ids["taskId"]:
						default:
						}
					}
				}
			}
			switch mode {
			case "expire":
				s.desktopControl.mu.Lock()
				s.desktopControl.clock = func() time.Time { return time.Now().Add(time.Minute) }
				err = s.expireControlPending()
				s.desktopControl.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
			case "reject":
				mustCall(t, s, "RejectControlAction", controlApproval(p))
			case "stop":
				mustCall(t, s, "StopComputerControl", struct{}{})
			case "cancel":
				mustCall(t, s, "CancelAgentTask", map[string]string{"id": task.ID})
			}
			select {
			case id := <-events:
				if id != task.ID {
					t.Fatalf("notified wrong task: %s", id)
				}
			case <-time.After(150 * time.Millisecond):
				t.Fatal("task view received no terminal-state notification")
			}
		})
	}
}

func TestHarnessDesktopTaskInterruptsIssuedAction(t *testing.T) {
	for _, method := range []string{"PauseAgentTask", "CancelAgentTask", "StopAll"} {
		t.Run(method, func(t *testing.T) {
			s, dir, _ := fixture(t)
			w := mvpWorkspace(t, s, dir)
			driver := newControlFixtureDriver()
			driver.block = make(chan struct{})
			s.desktopControl.driver = driver
			o := observeControlFixture(t, s)
			task, p, err := s.createDesktopTask(desktopTaskInput{WorkspaceID: w.ID, Type: computer.SetValue,
				ObservationID: o.ID, ElementID: o.Elements[0].ID, Text: "Interrupted input"})
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan domain.Reply, 1)
			go func() { result <- s.Call("ApproveControlAction", encode(controlApproval(p))) }()
			select {
			case <-driver.block:
			case <-time.After(3 * time.Second):
				t.Fatal("action never reached driver")
			}
			if method == "StopAll" {
				mustCall(t, s, method, struct{}{})
			} else {
				mustCall(t, s, method, map[string]string{"id": task.ID})
			}
			select {
			case reply := <-result:
				row, ok := reply.Data.(computer.Record)
				if !ok || row.Status != "RECOVERY_REQUIRED" || row.Verified {
					t.Fatalf("interrupted effect lost uncertainty: %+v", reply)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("task stop did not cancel desktop effect")
			}
			current := mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
			want := "PAUSED"
			if method == "CancelAgentTask" {
				want = "CANCELLED"
			}
			if current.Status != want || current.Steps[1].Status != "RECOVERY_REQUIRED" || driver.calls != 1 {
				t.Fatalf("interrupted task status=%s step=%s calls=%d", current.Status, current.Steps[1].Status, driver.calls)
			}
			if reply := s.Call("ApproveControlAction", encode(controlApproval(p))); reply.Error == nil {
				t.Fatal("interrupted approval became executable again")
			}
			var consumed int
			if err := s.db.QueryRow("SELECT count(*) FROM computer_approvals WHERE action_id=? AND status='CONSUMED'", p.ID).Scan(&consumed); err != nil || consumed != 1 {
				t.Fatalf("issued action consumption=%d, error=%v", consumed, err)
			}
		})
	}
}

func TestHarnessDesktopTaskCancelledRestartRefreshesActionEvidence(t *testing.T) {
	for _, status := range []string{"EXECUTING", "VERIFYING"} {
		t.Run(status, func(t *testing.T) {
			s, dir, _ := fixture(t)
			w := mvpWorkspace(t, s, dir)
			s.desktopControl.driver = newControlFixtureDriver()
			o := observeControlFixture(t, s)
			task, p, err := s.createDesktopTask(desktopTaskInput{WorkspaceID: w.ID, Type: computer.SetValue,
				ObservationID: o.ID, ElementID: o.Elements[0].ID, Text: "Crash fixture input"})
			if err != nil {
				t.Fatal(err)
			}
			// Persist the exact approval-consumed state before an effect returns.
			if err := saveControlApproval(s.db, p, now()); err != nil {
				t.Fatal(err)
			}
			if err := consumeControlApproval(s.db, p, now()); err != nil {
				t.Fatal(err)
			}
			if status == "VERIFYING" {
				if err := finishControl(s.db, computer.Record{ID: p.ID, Type: p.Type, Status: status, At: now()}, "EXECUTING"); err != nil {
					t.Fatal(err)
				}
			}
			s.desktopControl.mu.Lock()
			s.desktopControl.pending = nil
			s.desktopControl.mu.Unlock()
			mustCall(t, s, "CancelAgentTask", map[string]string{"id": task.ID})
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			restarted, err := New(dir, s.registry.(*tools.Registry), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			next := newControlFixtureDriver()
			restarted.desktopControl.driver = next
			current := mustCall(t, restarted, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
			if current.Status != "CANCELLED" || current.Steps[1].Status != "RECOVERY_REQUIRED" || next.calls != 0 {
				t.Fatalf("cancelled recovery status=%s step=%s effects=%d", current.Status, current.Steps[1].Status, next.calls)
			}
			screenError(t, restarted, "ApproveControlAction", controlApproval(p), "CONTROL_APPROVAL_USED")
		})
	}
}

func TestHarnessDesktopTaskRestartKeepsEvidenceAndNeverReplays(t *testing.T) {
	for _, completed := range []bool{false, true} {
		name := "pending"
		if completed {
			name = "completed"
		}
		t.Run(name, func(t *testing.T) {
			s, dir, _ := fixture(t)
			w := mvpWorkspace(t, s, dir)
			mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true, "automaticLearning": true, "projectLearning": true})
			driver := newControlFixtureDriver()
			s.desktopControl.driver = driver
			o := observeControlFixture(t, s)
			task, p, err := s.createDesktopTask(desktopTaskInput{WorkspaceID: w.ID, Type: computer.SetValue,
				ObservationID: o.ID, ElementID: o.Elements[0].ID, Text: "Private fixture text"})
			if err != nil {
				t.Fatal(err)
			}
			if completed {
				mustCall(t, s, "ApproveControlAction", controlApproval(p))
				until := time.Now().Add(3 * time.Second)
				learned := false
				for time.Now().Before(until) {
					state := mustCall(t, s, "GetCognitionState", map[string]string{"workspaceId": w.ID}).(cognitionState)
					for _, m := range state.Memories {
						if m.Kind == "episode" && m.Key == task.ID && m.Verified {
							learned = true
						}
					}
					if learned {
						break
					}
					time.Sleep(15 * time.Millisecond)
				}
				if !learned {
					t.Fatal("verified desktop evidence did not reach task learning")
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			restarted, err := New(dir, s.registry.(*tools.Registry), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			next := newControlFixtureDriver()
			restarted.desktopControl.driver = next
			current := mustCall(t, restarted, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
			want := "CANCELLED"
			if completed {
				want = "COMPLETED"
			}
			if current.Status != want || next.calls != 0 {
				t.Fatalf("restart state=%s, expected=%s, effects=%d", current.Status, want, next.calls)
			}
			screenError(t, restarted, "ApproveControlAction", controlApproval(p), "CONTROL_APPROVAL_USED")
			if strings.Contains(encode(current), "Private fixture text") {
				t.Fatal("task journal retained plaintext input")
			}
		})
	}
}

func TestHarnessDesktopTaskApprovalAllowsAsimiFocusChange(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	driver := newControlFixtureDriver()
	driver.window.Foreground = true
	s.desktopControl.driver = driver
	o := observeControlFixture(t, s)
	task, p, err := s.createDesktopTask(desktopTaskInput{WorkspaceID: w.ID, Type: computer.SetValue,
		ObservationID: o.ID, ElementID: o.Elements[0].ID, Text: "Fixture input"})
	if err != nil {
		t.Fatal(err)
	}
	// The user foregrounds Asimi to approve while the bound native window stays alive.
	driver.window.Foreground = false
	r := mustCall(t, s, "ApproveControlAction", controlApproval(p)).(computer.Record)
	current := mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
	if !r.Verified || current.Status != "COMPLETED" {
		t.Fatalf("approval focus change invalidated a live semantic target: %s %s", r.Status, current.Status)
	}
}

func TestHarnessDesktopTaskNeedsNoProjectConfiguration(t *testing.T) {
	s, _, _ := fixture(t)
	s.desktopControl.driver = newControlFixtureDriver()
	o := observeControlFixture(t, s)
	input := desktopTaskInput{Type: computer.SetValue, ObservationID: o.ID, ElementID: o.Elements[0].ID, Text: "Local desktop input"}
	task, p, err := s.createDesktopTask(input)
	if err != nil {
		t.Fatal(err)
	}
	if task.WorkspaceID == "" || task.Status != "WAITING_APPROVAL" {
		t.Fatalf("missing local task scope: %+v", task)
	}
	mustCall(t, s, "ApproveControlAction", controlApproval(p))
	current := mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
	if current.Status != "COMPLETED" {
		t.Fatal(current.Status)
	}
}

func TestHarnessDesktopTaskRevokesUnconsumedApproval(t *testing.T) {
	for _, mode := range []string{"cancel", "expired", "changed observation", "stop all"} {
		t.Run(mode, func(t *testing.T) {
			s, dir, _ := fixture(t)
			w := mvpWorkspace(t, s, dir)
			driver := newControlFixtureDriver()
			s.desktopControl.driver = driver
			clock := time.Now()
			s.desktopControl.clock = func() time.Time { return clock }
			observation := observeControlFixture(t, s)
			task, p, err := s.createDesktopTask(desktopTaskInput{WorkspaceID: w.ID, Type: computer.SetValue,
				ObservationID: observation.ID, ElementID: observation.Elements[0].ID, Text: "Fixture input"})
			if err != nil {
				t.Fatal(err)
			}
			want := "FAILED"
			switch mode {
			case "cancel":
				mustCall(t, s, "CancelAgentTask", map[string]string{"id": task.ID})
				want = "CANCELLED"
			case "stop all":
				mustCall(t, s, "StopAll", struct{}{})
				want = "CANCELLED"
			case "expired":
				clock = clock.Add(time.Minute)
			case "changed observation":
				observeControlFixture(t, s)
			}
			s.Call("ApproveControlAction", encode(controlApproval(p)))
			current := mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
			if current.Status != want || driver.calls != 0 {
				t.Fatalf("revoked action: task=%s expected=%s effects=%d", current.Status, want, driver.calls)
			}
			var consumed int
			err = s.db.QueryRow("SELECT count(*) FROM computer_approvals WHERE action_id=? AND status='CONSUMED'", p.ID).Scan(&consumed)
			if err != nil || consumed != 0 {
				t.Fatalf("consumed approval %d: %v", consumed, err)
			}
		})
	}
}

func TestHarnessDesktopTaskRequiresFreshObservationAndVerifiedApproval(t *testing.T) {
	for _, verified := range []bool{true, false} {
		name := "verified"
		if !verified {
			name = "unknown"
		}
		t.Run(name, func(t *testing.T) {
			s, dir, _ := fixture(t)
			w := mvpWorkspace(t, s, dir)
			driver := newControlFixtureDriver()
			driver.verify = verified
			s.desktopControl.driver = driver
			observation := observeControlFixture(t, s)
			const literal = "Harness fixture 你好"
			input := map[string]string{"workspaceId": w.ID, "type": string(computer.SetValue),
				"observationId": observation.ID, "elementId": observation.Elements[0].ID, "text": literal}
			data := mustCall(t, s, "CreateDesktopTask", input)
			var task AgentTask
			if err := json.Unmarshal([]byte(encode(data)), &task); err != nil {
				t.Fatal(err)
			}
			rows := readComputerEvidence(t, s, task.ID)
			if task.Status != "WAITING_APPROVAL" || len(rows) != 1 || driver.calls != 0 {
				t.Fatalf("desktop task did not wait for approval: %s %v", task.Status, rows)
			}
			id := rows[0]["actionId"].(string)
			p := *s.desktopControl.pending
			if id != p.ID || p.ObservationID != observation.ID {
				t.Fatal("task did not bind the observed target")
			}
			mustCall(t, s, "ApproveControlAction", controlApproval(p))
			screenError(t, s, "ApproveControlAction", controlApproval(p), "CONTROL_APPROVAL_USED")
			current := mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
			want := "PAUSED"
			if verified {
				want = "COMPLETED"
			}
			if current.Status != want || driver.calls != 1 || strings.Contains(encode(current), literal) {
				t.Fatalf("desktop task status %s, expected %s; executions %d", current.Status, want, driver.calls)
			}
			screenError(t, s, "CreateDesktopTask", input, "CONTROL_STALE_TARGET")
		})
	}
}
