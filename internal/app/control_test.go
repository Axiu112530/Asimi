package app

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"deskpilot/internal/computer"
	"deskpilot/internal/screen"
)

type controlFixtureDriver struct {
	mu      sync.Mutex
	window  computer.Window
	element computer.Element
	calls   int
	verify  bool
	block   chan struct{}
}

func TestControlPolicyRejectsUnsafeTargetsAndInputs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*controlFixtureDriver)
	}{
		{"foreign process", func(d *controlFixtureDriver) { d.element.ProcessID++ }},
		{"disabled", func(d *controlFixtureDriver) { d.element.Enabled = false }},
		{"offscreen", func(d *controlFixtureDriver) { d.element.Offscreen = true }},
		{"password", func(d *controlFixtureDriver) { d.element.Password = true }},
		{"missing pattern", func(d *controlFixtureDriver) { d.element.Patterns = nil }},
		{"unapproved selector", func(d *controlFixtureDriver) { d.element.AutomationID = "OtherInput" }},
		{"zero bounds", func(d *controlFixtureDriver) { d.element.Bounds.Width = 0 }},
		{"missing identity", func(d *controlFixtureDriver) { d.element.RuntimeID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := aiFixture(t, nil)
			d := newControlFixtureDriver()
			tc.change(d)
			s.desktopControl.driver = d
			o := observeControlFixture(t, s)
			screenError(t, s, "ProposeControlAction", map[string]string{"type": string(computer.TypeText), "observationId": o.ID, "elementId": o.Elements[0].ID, "text": "Hello"}, "CONTROL_ACTION_DENIED")
			if d.calls != 0 {
				t.Fatal("unsafe dispatch")
			}
		})
	}
	for _, text := range []string{"", "line\nnext", "\ttext", "powershell hidden", "API Key abc", "secret text", strings.Repeat("x", 513), "curl example.com", "abc1234567890123456789012345", "open;run"} {
		if computer.ValidateText(text) {
			t.Fatalf("unsafe input accepted %q", text)
		}
	}
	for _, text := range []string{"Hello DeskPilot", "你好，测试。", "A simple line 123!"} {
		if !computer.ValidateText(text) {
			t.Fatalf("plain line rejected %q", text)
		}
	}
}

func TestControlDatabaseFailureNeverDispatchesOrClaimsSuccess(t *testing.T) {
	for _, boundary := range []string{"EXECUTING", "COMPLETED"} {
		t.Run(boundary, func(t *testing.T) {
			s := aiFixture(t, nil)
			d := newControlFixtureDriver()
			s.desktopControl.driver = d
			p := proposeFixtureText(t, s)
			_, e := s.db.Exec("CREATE TRIGGER control_fault BEFORE UPDATE ON computer_actions WHEN NEW.status='" + boundary + "' BEGIN SELECT RAISE(ABORT,'injected'); END")
			if e != nil {
				t.Fatal(e)
			}
			r := s.Call("ApproveControlAction", encode(controlApproval(p)))
			if boundary == "EXECUTING" {
				if d.calls != 0 {
					t.Fatal("effect before durable intent")
				}
				if r.Error == nil && r.Data.(computer.Record).Status == "COMPLETED" {
					t.Fatal("false success")
				}
			} else {
				if d.calls != 1 || r.Error == nil {
					t.Fatalf("effect or error lost %+v", r)
				}
				var status string
				if e = s.db.QueryRow("SELECT status FROM computer_actions WHERE id=?", p.ID).Scan(&status); e != nil || status != "VERIFYING" {
					t.Fatalf("durable uncertainty missing %s %v", status, e)
				}
			}
			if _, e = s.db.Exec("DROP TRIGGER control_fault"); e != nil {
				t.Fatal(e)
			}
			if e = recoverComputer(s.db); e != nil {
				t.Fatal(e)
			}
			if boundary == "COMPLETED" {
				var status string
				s.db.QueryRow("SELECT status FROM computer_actions WHERE id=?", p.ID).Scan(&status)
				if status != "RECOVERY_REQUIRED" {
					t.Fatal(status)
				}
			}
		})
	}
}

func TestControlStopRevokesApprovalEvenWhenCancellationWriteFails(t *testing.T) {
	s := aiFixture(t, nil)
	d := newControlFixtureDriver()
	s.desktopControl.driver = d
	p := proposeFixtureText(t, s)
	if _, e := s.db.Exec("CREATE TRIGGER cancel_fault BEFORE UPDATE ON computer_actions WHEN NEW.status='CANCELLED' BEGIN SELECT RAISE(ABORT,'injected'); END"); e != nil {
		t.Fatal(e)
	}
	screenError(t, s, "StopComputerControl", struct{}{}, "STORAGE_CONSTRAINT")
	mustCall(t, s, "ResumeComputerControl", struct{}{})
	screenError(t, s, "ApproveControlAction", controlApproval(p), "CONTROL_APPROVAL_USED")
	if d.calls != 0 {
		t.Fatal("stopped authorization was restored")
	}
	if _, e := s.db.Exec("DROP TRIGGER cancel_fault"); e != nil {
		t.Fatal(e)
	}
}

func TestControlStopAllStillCancelsOtherWorkersOnJournalFailure(t *testing.T) {
	s := aiFixture(t, nil)
	s.desktopControl.driver = newControlFixtureDriver()
	proposeFixtureText(t, s)
	fileCtx, fileCancel := context.WithCancel(context.Background())
	chatCtx, chatCancel := context.WithCancel(context.Background())
	s.controls["worker"] = control{ctx: fileCtx, cancel: fileCancel}
	s.chatControls["chat"] = chatControl{cancel: chatCancel}
	if _, e := s.db.Exec("CREATE TRIGGER stop_fault BEFORE UPDATE ON computer_actions WHEN NEW.status='CANCELLED' BEGIN SELECT RAISE(ABORT,'injected'); END"); e != nil {
		t.Fatal(e)
	}
	screenError(t, s, "StopAll", struct{}{}, "STORAGE_CONSTRAINT")
	if fileCtx.Err() == nil || chatCtx.Err() == nil {
		t.Fatal("journal failure prevented global cancellation")
	}
	if _, e := s.db.Exec("DROP TRIGGER stop_fault"); e != nil {
		t.Fatal(e)
	}
}

func TestControlConcurrentApprovalAndV4Rollback(t *testing.T) {
	s := aiFixture(t, nil)
	d := newControlFixtureDriver()
	s.desktopControl.driver = d
	p := proposeFixtureText(t, s)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); s.Call("ApproveControlAction", encode(controlApproval(p))) }()
	}
	wg.Wait()
	if d.calls != 1 {
		t.Fatalf("approval dispatched %d times", d.calls)
	}
	dir := t.TempDir()
	db, e := sql.Open("sqlite", filepath.Join(dir, "deskpilot.db"))
	if e != nil {
		t.Fatal(e)
	}
	for _, schema := range []string{schemaV1, schemaV2, schemaV3} {
		if e = applyMigration(db, schema); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec("CREATE TABLE computer_approvals(dummy TEXT)"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if opened, err := openStore(dir); err == nil {
		opened.Close()
		t.Fatal("conflicting v4 migration succeeded")
	}
	db, e = sql.Open("sqlite", filepath.Join(dir, "deskpilot.db"))
	if e != nil {
		t.Fatal(e)
	}
	var version, count int
	db.QueryRow("PRAGMA user_version").Scan(&version)
	db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='computer_actions'").Scan(&count)
	if version != 3 || count != 0 {
		t.Fatalf("partial v4 migration %d %d", version, count)
	}
	db.Exec("DROP TABLE computer_approvals")
	db.Close()
	for range 2 {
		db, e = openStore(dir)
		if e != nil {
			t.Fatal(e)
		}
		db.QueryRow("PRAGMA user_version").Scan(&version)
		db.Close()
		if version != 6 {
			t.Fatal(version)
		}
	}
}

func TestControlCrashFixture(t *testing.T) {
	boundary := os.Getenv("DESKPILOT_CONTROL_CRASH")
	if boundary == "" {
		t.Skip("subprocess fixture")
	}
	db, e := openStore(os.Getenv("DESKPILOT_CONTROL_PROFILE"))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Service{db: db, lifetime: ctx, stop: cancel}
	s.desktopControl.driver = newControlFixtureDriver()
	s.checkpoint = func(name string) {
		if name == boundary {
			os.Exit(83)
		}
	}
	p := proposeFixtureText(t, s)
	mustCall(t, s, "ApproveControlAction", controlApproval(p))
	t.Fatal("crash boundary was not reached")
}

func TestControlProcessCrashDoesNotReplay(t *testing.T) {
	for _, boundary := range []string{"control.intent_persisted", "control.effect_returned", "control.verification_returned", "control.final_committed"} {
		t.Run(boundary, func(t *testing.T) {
			dir := t.TempDir()
			exe, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			cmd := exec.Command(exe, "-test.run=^TestControlCrashFixture$")
			cmd.Env = append(os.Environ(), "DESKPILOT_CONTROL_CRASH="+boundary, "DESKPILOT_CONTROL_PROFILE="+dir)
			output, e := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(e, &exit) || exit.ExitCode() != 83 {
				t.Fatalf("boundary missed %v %s", e, output)
			}
			db, e := openStore(dir)
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			if e = recoverComputer(db); e != nil {
				t.Fatal(e)
			}
			var status string
			var consumed int
			if e = db.QueryRow("SELECT status FROM computer_actions").Scan(&status); e != nil {
				t.Fatal(e)
			}
			want := "RECOVERY_REQUIRED"
			if boundary == "control.final_committed" {
				want = "COMPLETED"
			}
			if status != want {
				t.Fatalf("recovered %s want %s", status, want)
			}
			if e = db.QueryRow("SELECT count(*) FROM computer_approvals WHERE status='CONSUMED'").Scan(&consumed); e != nil || consumed != 1 {
				t.Fatalf("original consumption lost %d %v", consumed, e)
			}
			if e = recoverComputer(db); e != nil {
				t.Fatal(e)
			}
			db.QueryRow("SELECT status FROM computer_actions").Scan(&status)
			if status != want {
				t.Fatal("recovery was not idempotent")
			}
		})
	}
}

func newControlFixtureDriver() *controlFixtureDriver {
	bounds := screen.Rect{X: 20, Y: 50, Width: 180, Height: 60}
	return &controlFixtureDriver{
		window: computer.Window{ID: "fixture", App: computer.TestHost, HWND: "167", PID: 123,
			CreationTime: "fixture-time", ImagePath: "fixture.exe", SessionID: 1, Integrity: 0x2000,
			ClassName: "Fixture", Title: "Dedicated test window", Bounds: screen.Rect{Width: 500, Height: 400}},
		element: computer.Element{ID: "editor", AutomationID: "TestInput", Name: "Test input", ControlType: "Edit",
			Enabled: true, Focused: true, ProcessID: 123, Bounds: bounds, Patterns: []string{"Value"},
			ParentPath: "root", RuntimeID: "fixture-editor", Blank: true},
		verify: true,
	}
}

func (d *controlFixtureDriver) Enumerate(context.Context) ([]computer.Window, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return []computer.Window{d.window}, nil
}
func (d *controlFixtureDriver) Inspect(_ context.Context, _ computer.Window) (computer.Observation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return computer.Observation{Window: d.window, Elements: []computer.Element{d.element}}, nil
}
func (d *controlFixtureDriver) Execute(ctx context.Context, _ computer.Proposal) (computer.Outcome, error) {
	d.mu.Lock()
	d.calls++
	block := d.block
	d.mu.Unlock()
	if block != nil {
		close(block)
		<-ctx.Done()
		return computer.Outcome{Effect: "UNKNOWN"}, ctx.Err()
	}
	return computer.Outcome{Effect: "ISSUED"}, nil
}
func (d *controlFixtureDriver) Verify(context.Context, computer.Proposal, computer.Outcome) (computer.Verification, error) {
	return computer.Verification{Verified: d.verify}, nil
}
func (*controlFixtureDriver) Close() error { return nil }

func observeControlFixture(t *testing.T, s *Service) computer.Observation {
	t.Helper()
	windows := mustCall(t, s, "ListControlWindows", struct{}{}).([]computer.Window)
	return mustCall(t, s, "InspectControlWindow", map[string]string{"windowId": windows[0].ID}).(computer.Observation)
}
func proposeFixtureText(t *testing.T, s *Service) computer.Proposal {
	t.Helper()
	tree := observeControlFixture(t, s)
	return mustCall(t, s, "ProposeControlAction", map[string]string{
		"type": string(computer.TypeText), "observationId": tree.ID,
		"elementId": tree.Elements[0].ID, "text": "Hello DeskPilot",
	}).(computer.Proposal)
}
func controlApproval(p computer.Proposal) map[string]string {
	return map[string]string{"actionId": p.ID, "actionHash": p.Hash}
}

func TestControlApprovalConsumesExactlyOnceAndRequiresVerification(t *testing.T) {
	s := aiFixture(t, nil)
	driver := newControlFixtureDriver()
	s.desktopControl.driver = driver
	p := proposeFixtureText(t, s)
	if p.Status != "AWAITING_APPROVAL" || p.Hash == "" || p.Text != "Hello DeskPilot" || driver.calls != 0 {
		t.Fatal("invalid proposal or premature dispatch")
	}
	bad := controlApproval(p)
	bad["actionHash"] = "wrong"
	screenError(t, s, "ApproveControlAction", bad, "CONTROL_APPROVAL_MISMATCH")
	approved := mustCall(t, s, "ApproveControlAction", controlApproval(p)).(computer.Record)
	if approved.Status != "COMPLETED" || !approved.Verified || driver.calls != 1 {
		t.Fatalf("unexpected result %+v", approved)
	}
	screenError(t, s, "ApproveControlAction", controlApproval(p), "CONTROL_APPROVAL_USED")
	driver.verify = false
	next := proposeFixtureText(t, s)
	unverified := mustCall(t, s, "ApproveControlAction", controlApproval(next)).(computer.Record)
	if unverified.Status != "UNKNOWN_RESULT" || unverified.Verified || driver.calls != 2 {
		t.Fatalf("unverified action completed: %+v", unverified)
	}
	rows := mustCall(t, s, "ListControlJournal", struct{}{}).([]computer.Record)
	if len(rows) < 2 || strings.Contains(encode(rows), "Hello DeskPilot") {
		t.Fatal("missing history or literal input leaked into journal")
	}
}

func TestControlStaleExpiryRejectAndStopDoNotExecute(t *testing.T) {
	s := aiFixture(t, nil)
	driver := newControlFixtureDriver()
	s.desktopControl.driver = driver
	clock := time.Now()
	s.desktopControl.clock = func() time.Time { return clock }
	p := proposeFixtureText(t, s)
	driver.window.CreationTime = "replacement process"
	stale := mustCall(t, s, "ApproveControlAction", controlApproval(p)).(computer.Record)
	if stale.Status != "STALE_TARGET" || driver.calls != 0 {
		t.Fatalf("stale dispatch: %+v", stale)
	}
	p = proposeFixtureText(t, s)
	clock = clock.Add(time.Minute)
	screenError(t, s, "ApproveControlAction", controlApproval(p), "CONTROL_EXPIRED")
	p = proposeFixtureText(t, s)
	rejected := mustCall(t, s, "RejectControlAction", controlApproval(p)).(computer.Record)
	if rejected.Status != "REJECTED" {
		t.Fatalf("rejection not saved: %+v", rejected)
	}
	p = proposeFixtureText(t, s)
	mustCall(t, s, "StopComputerControl", struct{}{})
	mustCall(t, s, "StopComputerControl", struct{}{})
	screenError(t, s, "ApproveControlAction", controlApproval(p), "CONTROL_STOPPED")
	screenError(t, s, "ProposeControlAction", map[string]string{"type": string(computer.LaunchHost)}, "CONTROL_STOPPED")
	if driver.calls != 0 {
		t.Fatal("action executed without live approval")
	}
}

func TestControlStopDuringEffectRecordsUnknownAndRecoveryDoesNotReplay(t *testing.T) {
	s := aiFixture(t, nil)
	driver := newControlFixtureDriver()
	driver.block = make(chan struct{})
	s.desktopControl.driver = driver
	p := proposeFixtureText(t, s)
	result := make(chan computer.Record, 1)
	go func() {
		reply := s.Call("ApproveControlAction", encode(controlApproval(p)))
		row, _ := reply.Data.(computer.Record)
		result <- row
	}()
	select {
	case <-driver.block:
	case <-time.After(3 * time.Second):
		t.Fatal("action did not reach driver")
	}
	mustCall(t, s, "StopComputerControl", struct{}{})
	select {
	case row := <-result:
		if row.Status != "RECOVERY_REQUIRED" || row.Verified {
			t.Fatalf("stop misreported: %+v", row)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not cancel driver")
	}
	db, e := openStore(filepath.Join(t.TempDir(), ""))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	const insert = `INSERT INTO computer_actions(id,kind,status,action_hash,target_hash,created_at,updated_at,expires_at)
VALUES(?, 'uia.invoke', ?, 'action', 'target', 'now', 'now', 'now')`
	if _, e = db.Exec(insert, "running", "EXECUTING"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(insert, "pending", "AWAITING_APPROVAL"); e != nil {
		t.Fatal(e)
	}
	if e = recoverComputer(db); e != nil {
		t.Fatal(e)
	}
	for id, want := range map[string]string{"running": "RECOVERY_REQUIRED", "pending": "CANCELLED"} {
		var got string
		if e = db.QueryRow("SELECT status FROM computer_actions WHERE id=?", id).Scan(&got); e != nil || got != want {
			t.Fatalf("%s: %s %v", id, got, e)
		}
	}
}
