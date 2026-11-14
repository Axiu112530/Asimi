package app

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"asimi/internal/computer"
	"asimi/internal/domain"
)

func controlEscapeStatus(t *testing.T, s *Service) map[string]any {
	t.Helper()
	return mustCall(t, s, "GetControlStatus", struct{}{}).(map[string]any)
}

func approveControlAsync(s *Service, p computer.Proposal) <-chan domain.Reply {
	done := make(chan domain.Reply, 1)
	go func() { done <- s.Call("ApproveControlAction", encode(controlApproval(p))) }()
	return done
}

func awaitControlSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func awaitControlReply(t *testing.T, done <-chan domain.Reply) computer.Record {
	t.Helper()
	select {
	case reply := <-done:
		if reply.Error != nil {
			t.Fatalf("control failed: %+v", reply.Error)
		}
		row, ok := reply.Data.(computer.Record)
		if !ok {
			t.Fatalf("unexpected control reply: %+v", reply)
		}
		return row
	case <-time.After(3 * time.Second):
		t.Fatal("Escape did not cancel the active driver")
		return computer.Record{}
	}
}

func TestControlEscapeStatusHasHonestPlatformAndActivity(t *testing.T) {
	s := aiFixture(t, nil)
	s.desktopControl.driver = newControlFixtureDriver()
	status := controlEscapeStatus(t, s)
	if status["working"] != false || status["escapeAvailable"] != false {
		t.Fatalf("idle control reports active Escape: %+v", status)
	}
	if status["nativeEscape"] != (runtime.GOOS == "windows") {
		t.Fatalf("native Escape capability does not match this platform: %+v", status)
	}
}

func TestControlEscapeCancelsApprovedDriverAndStopsSamplingWhenIdle(t *testing.T) {
	s := aiFixture(t, nil)
	d := newControlFixtureDriver()
	d.block = make(chan struct{})
	s.desktopControl.driver = d
	var pressed atomic.Bool
	var reads atomic.Int64
	s.desktopControl.escapePressed = func() bool {
		reads.Add(1)
		return pressed.Load()
	}
	p := proposeFixtureText(t, s)
	if reads.Load() != 0 {
		t.Fatal("Escape sampled before explicit approval")
	}
	done := approveControlAsync(s, p)
	defer s.desktopControl.signalStop()
	awaitControlSignal(t, d.block, "driver entry")
	status := controlEscapeStatus(t, s)
	if status["working"] != true || status["escapeAvailable"] != status["nativeEscape"] {
		t.Fatalf("active driver missing from status: %+v", status)
	}
	pressed.Store(true)
	row := awaitControlReply(t, done)
	if row.Status != "RECOVERY_REQUIRED" || row.Code != "CONTROL_UNKNOWN_RESULT" || row.Verified {
		t.Fatalf("interrupted effect falsely claimed certainty: %+v", row)
	}
	status = controlEscapeStatus(t, s)
	if status["stopped"] != true || status["working"] != false || status["escapeAvailable"] != false {
		t.Fatalf("Escape did not finish the control lifetime: %+v", status)
	}
	mustCall(t, s, "ResumeComputerControl", struct{}{})
	finishedReads := reads.Load()
	time.Sleep(100 * time.Millisecond)
	if reads.Load() != finishedReads || controlEscapeStatus(t, s)["stopped"] != false {
		t.Fatal("idle Escape sampled or stopped control after explicit resume")
	}
}

func TestControlHeldEscapePreventsAnyApprovedEffect(t *testing.T) {
	s := aiFixture(t, nil)
	d := newControlFixtureDriver()
	s.desktopControl.driver = d
	s.desktopControl.escapePressed = func() bool { return true }
	p := proposeFixtureText(t, s)
	row := mustCall(t, s, "ApproveControlAction", controlApproval(p)).(computer.Record)
	if row.Status != "CANCELLED" || row.Code != "CONTROL_STOPPED" || row.Verified || d.calls != 0 {
		t.Fatalf("held Escape allowed a desktop effect: calls=%d row=%+v", d.calls, row)
	}
}

type escapeInspectDriver struct {
	*controlFixtureDriver
	block      atomic.Bool
	inspecting chan struct{}
}

func (d *escapeInspectDriver) Inspect(ctx context.Context, w computer.Window) (computer.Observation, error) {
	if !d.block.Load() {
		return d.controlFixtureDriver.Inspect(ctx, w)
	}
	close(d.inspecting)
	<-ctx.Done()
	return computer.Observation{}, ctx.Err()
}

func TestControlEscapeDuringRevalidationPreventsExecute(t *testing.T) {
	s := aiFixture(t, nil)
	d := &escapeInspectDriver{controlFixtureDriver: newControlFixtureDriver(), inspecting: make(chan struct{})}
	s.desktopControl.driver = d
	var pressed atomic.Bool
	s.desktopControl.escapePressed = pressed.Load
	p := proposeFixtureText(t, s)
	d.block.Store(true)
	done := approveControlAsync(s, p)
	defer s.desktopControl.signalStop()
	awaitControlSignal(t, d.inspecting, "revalidation")
	pressed.Store(true)
	row := awaitControlReply(t, done)
	if row.Status != "CANCELLED" || row.Code != "CONTROL_STOPPED" || d.calls != 0 {
		t.Fatalf("Escape during revalidation did not cancel before effect: calls=%d row=%+v", d.calls, row)
	}
}

type escapeReleaseDriver struct {
	*controlFixtureDriver
	entered  chan struct{}
	release  chan struct{}
	verified chan struct{}
}

func (d *escapeReleaseDriver) Execute(ctx context.Context, _ computer.Proposal) (computer.Outcome, error) {
	close(d.entered)
	select {
	case <-d.release:
		return computer.Outcome{Effect: "ISSUED"}, nil
	case <-ctx.Done():
		return computer.Outcome{Effect: "UNKNOWN"}, ctx.Err()
	}
}

func (d *escapeReleaseDriver) Verify(context.Context, computer.Proposal, computer.Outcome) (computer.Verification, error) {
	close(d.verified)
	return computer.Verification{Verified: true}, nil
}

func TestControlEscapeCleanupJoinsReaderBeforeAllowingNextAction(t *testing.T) {
	s := aiFixture(t, nil)
	d := &escapeReleaseDriver{controlFixtureDriver: newControlFixtureDriver(), entered: make(chan struct{}), release: make(chan struct{}), verified: make(chan struct{})}
	s.desktopControl.driver = d
	sampling := make(chan struct{})
	releaseReader := make(chan struct{})
	var reads atomic.Int64
	s.desktopControl.escapePressed = func() bool {
		if reads.Add(1) == 2 {
			close(sampling)
			<-releaseReader
			return true // Finishes after this action requests watcher cleanup.
		}
		return false
	}
	p := proposeFixtureText(t, s)
	done := approveControlAsync(s, p)
	defer s.desktopControl.signalStop()
	defer func() {
		select {
		case <-releaseReader:
		default:
			close(releaseReader)
		}
	}()
	awaitControlSignal(t, d.entered, "driver entry")
	awaitControlSignal(t, sampling, "in-flight Escape sample")
	close(d.release)
	awaitControlSignal(t, d.verified, "verification")
	select {
	case reply := <-done:
		t.Fatalf("action returned without joining its Escape watcher: %+v", reply)
	case <-time.After(100 * time.Millisecond):
	}
	if controlEscapeStatus(t, s)["working"] != true {
		t.Fatal("working cleared while Escape callback can still run")
	}
	screenError(t, s, "ResumeComputerControl", struct{}{}, "CONTROL_BUSY")
	close(releaseReader)
	row := awaitControlReply(t, done)
	if row.Status != "COMPLETED" || controlEscapeStatus(t, s)["stopped"] != false {
		t.Fatalf("sample finishing after cleanup poisoned control: %+v", row)
	}
	// A later, separately approved action is unaffected by the previous sample.
	s.desktopControl.driver = newControlFixtureDriver()
	next := proposeFixtureText(t, s)
	row = mustCall(t, s, "ApproveControlAction", controlApproval(next)).(computer.Record)
	if row.Status != "COMPLETED" {
		t.Fatalf("next action was poisoned: %+v", row)
	}
}
