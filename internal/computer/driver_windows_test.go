package computer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyFixtureRejectsAmbiguousResults(t *testing.T) {
	action := Proposal{Type: Invoke, Expected: "TEST_SUBMITTED"}
	result := Element{AutomationID: "ResultLabel", Name: "Submitted", ControlType: "Text", Enabled: true, ProcessID: 7}
	observation := Observation{Window: Window{PID: 7}, Elements: []Element{result}}
	if !verifyFixture(action, observation) {
		t.Fatal("the unique result label should verify submission")
	}
	observation.Elements = append(observation.Elements, result)
	if verifyFixture(action, observation) {
		t.Fatal("duplicate result labels must not verify submission")
	}
	observation.Elements = observation.Elements[:1]
	observation.Truncated = true
	if verifyFixture(action, observation) {
		t.Fatal("a truncated tree cannot establish a unique result")
	}
}

func TestWindowSnapshotRejectsChangedProcessAndBounds(t *testing.T) {
	approved := Window{App: TestHost, HWND: "133", PID: 7, CreationTime: "321", ImagePath: "C:\\app\\DeskPilotControlHost.exe"}
	current := approved
	current.ObservedAt = "later"
	current.Foreground = true
	if !sameWindow(approved, current) {
		t.Fatal("time and foreground may change between observations")
	}
	current.CreationTime = "322"
	if sameWindow(approved, current) {
		t.Fatal("process reuse invalidates the approved snapshot")
	}
	current = approved
	current.Bounds.X++
	if sameWindow(approved, current) {
		t.Fatal("moved bounds invalidate the approved snapshot")
	}
}

func TestPinnedBinaryRejectsMismatchAndBlocksReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture-image")
	data := []byte("fixture-pinning-test")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	f, err := pinFile(path, hex.EncodeToString(hash[:]))
	if err != nil {
		t.Fatal("expected image could not be pinned")
	}
	if err := os.WriteFile(path, []byte("replacement"), 0600); err == nil {
		f.Close()
		t.Fatal("pinned image allowed a concurrent write")
	}
	if err := os.Rename(path, path+".moved"); err == nil {
		f.Close()
		t.Fatal("pinned image allowed a concurrent rename")
	}
	f.Close()
	hash[0] ^= 1
	if unexpected, err := pinFile(path, hex.EncodeToString(hash[:])); err == nil {
		unexpected.Close()
		t.Fatal("mismatched image was accepted")
	}
}

// This opt-in test starts only the fixed hash-pinned fixture. Compile this test
// into the release directory with the same helper hash ldflags as the app.
func TestNativeFixtureLifecycle(t *testing.T) {
	if os.Getenv("DESKPILOT_CONTROL_FIXTURE_TEST") != "1" {
		t.Skip("dedicated native fixture acceptance requires an explicit opt-in")
	}
	driver := NewDriver()
	defer driver.Close()
	if !driver.(*windowsDriver).Available() {
		t.Fatal("pinned helpers and an ordinary medium-integrity session are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	startedAt := time.Now()
	launch := Proposal{Type: LaunchHost, Expected: "HOST_VISIBLE"}
	outcome, err := driver.Execute(ctx, launch)
	if err != nil || outcome.Effect != "ISSUED" || outcome.Window == nil {
		t.Fatalf("dedicated fixture launch failed, effect=%s error=%v", outcome.Effect, err)
	}
	verified, err := driver.Verify(ctx, launch, outcome)
	if err != nil || !verified.Verified {
		t.Fatalf("dedicated fixture visibility could not be verified, error=%v", err)
	}
	t.Logf("dedicated launch and fresh visibility verified in %s", time.Since(startedAt).Round(time.Millisecond))
	if second, err := driver.Execute(ctx, launch); second.Effect != "NOT_STARTED" || err != Error("CONTROL_BUSY") {
		t.Fatalf("second fixture launch must be denied, effect=%s error=%v", second.Effect, err)
	}
	observation, err := driver.Inspect(ctx, *outcome.Window)
	if err != nil || observation.Modal || observation.Truncated {
		t.Fatalf("fixture inspection failed, error=%v", err)
	}
	t.Logf("bounded inspection returned %d elements", len(observation.Elements))
	find := func(observation Observation, id string) Element {
		t.Helper()
		var matches []Element
		for _, el := range observation.Elements {
			if el.AutomationID == id {
				matches = append(matches, el)
			}
		}
		if len(matches) != 1 {
			t.Fatalf("fixture control %s must be unique, count=%d", id, len(matches))
		}
		return matches[0]
	}
	input := find(observation, "TestInput")
	value := Proposal{Type: SetValue, Window: &observation.Window, Element: &input, Text: "Dedicated fixture 你好", Expected: "TEXT_MATCH"}
	outcome, err = driver.Execute(ctx, value)
	if err != nil || outcome.Effect != "ISSUED" {
		t.Fatalf("semantic value operation failed, effect=%s error=%v", outcome.Effect, err)
	}
	verified, err = driver.Verify(ctx, value, outcome)
	if err != nil || !verified.Verified {
		t.Fatalf("semantic value effect could not be verified, error=%v", err)
	}
	t.Logf("semantic value and fresh hash verified after %s", time.Since(startedAt).Round(time.Millisecond))
	observation, err = driver.Inspect(ctx, *outcome.Window)
	if err != nil {
		t.Fatal("post-value fixture inspection failed")
	}
	button := find(observation, "SubmitButton")
	invoke := Proposal{Type: Invoke, Window: &observation.Window, Element: &button, Expected: "TEST_SUBMITTED"}
	outcome, err = driver.Execute(ctx, invoke)
	if err != nil || outcome.Effect != "ISSUED" {
		t.Fatalf("semantic invocation failed, effect=%s error=%v", outcome.Effect, err)
	}
	verified, err = driver.Verify(ctx, invoke, outcome)
	if err != nil || !verified.Verified {
		t.Fatalf("semantic invocation effect could not be verified, error=%v", err)
	}
	t.Logf("semantic invocation and fresh status verified after %s", time.Since(startedAt).Round(time.Millisecond))
	if err := driver.Close(); err != nil {
		t.Fatalf("owned fixture did not shut down gracefully, error=%v", err)
	}
}
