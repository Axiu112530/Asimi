package computer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

type hostProcess struct {
	pid            uint32
	creation, path string
	input          io.WriteCloser
	exited         chan struct{}
}

type windowsDriver struct {
	lifetime                            context.Context
	cancel                              context.CancelFunc
	directory, programFiles, systemRoot string
	self                                processIdentity
	worker                              chan struct{}
	ready                               bool

	mu   sync.Mutex
	host *hostProcess
}

func NewDriver() Driver {
	ctx, cancel := context.WithCancel(context.Background())
	d := &windowsDriver{lifetime: ctx, cancel: cancel, worker: make(chan struct{}, 1)}
	if len(hostSHA256) != 64 || len(workerSHA256) != 64 {
		return d
	}
	self, ok := queryProcess(windows.CurrentProcess(), uint32(os.Getpid()))
	if !ok || self.integrity != mediumIntegrity {
		return d
	}
	exe, err := os.Executable()
	if err != nil {
		return d
	}
	path, err := canonicalFile(exe)
	if err != nil {
		return d
	}
	d.systemRoot, err = windows.GetWindowsDirectory()
	if err != nil {
		return d
	}
	d.directory = filepath.Dir(path)
	d.self = self
	d.programFiles, _ = windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DEFAULT)
	for name, digest := range map[string]string{hostFile: hostSHA256, workerFile: workerSHA256} {
		binary, err := d.pinBinary(name, digest)
		if err != nil {
			return d
		}
		binary.Close()
	}
	d.ready = true
	return d
}

func (d *windowsDriver) Available() bool {
	return d.ready && d.lifetime.Err() == nil
}

func (d *windowsDriver) Close() error {
	d.cancel()
	if host := d.ownedHost(); host != nil {
		// EOF belongs to the exact host process created by this driver. It
		// requests a normal Form.Close through the fixture's private pipe.
		_ = host.input.Close()
		select {
		case <-host.exited:
		case <-time.After(750 * time.Millisecond):
			return Error("CONTROL_UNKNOWN_RESULT")
		}
	}
	return nil
}

func (d *windowsDriver) withLifetime(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(d.lifetime, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (d *windowsDriver) ownedHost() *hostProcess {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.host
}

func (d *windowsDriver) Enumerate(ctx context.Context) ([]Window, error) {
	if !d.Available() {
		return nil, Error("CONTROL_UNAVAILABLE")
	}
	ctx, done := d.withLifetime(ctx)
	defer done()
	return d.enumerate(ctx)
}

func (d *windowsDriver) enumerate(ctx context.Context) ([]Window, error) {
	handles, err := listWindowHandles(ctx)
	if err != nil {
		return nil, err
	}
	out := []Window{}
	for _, hwnd := range handles {
		if ctx.Err() != nil {
			return nil, Error("CONTROL_QUERY_TIMEOUT")
		}
		if w, ok := d.observeWindow(hwnd); ok {
			out = append(out, w)
			if len(out) == 32 {
				break
			}
		}
	}
	return out, nil
}

func (d *windowsDriver) refresh(expected Window) (Window, uintptr, bool) {
	number, err := strconv.ParseUint(expected.HWND, 10, strconv.IntSize)
	if err != nil || number == 0 {
		return Window{}, 0, false
	}
	hwnd := uintptr(number)
	w, ok := d.observeWindow(hwnd)
	return w, hwnd, ok && sameWindow(expected, w)
}

func (d *windowsDriver) Inspect(ctx context.Context, expected Window) (Observation, error) {
	if !d.Available() {
		return Observation{}, Error("CONTROL_UNAVAILABLE")
	}
	ctx, done := d.withLifetime(ctx)
	defer done()
	w, _, ok := d.refresh(expected)
	if !ok {
		return Observation{}, Error("CONTROL_STALE_TARGET")
	}
	reply, _, err := d.callWorker(ctx, workerRequest{Op: "inspect", Window: w})
	if err != nil {
		return Observation{}, err
	}
	if reply.Code != "" {
		return Observation{}, workerError(reply.Code)
	}
	if len(reply.Elements) > 500 {
		return Observation{}, Error("CONTROL_QUERY_TIMEOUT")
	}
	w, _, ok = d.refresh(w)
	if !ok {
		return Observation{}, Error("CONTROL_STALE_TARGET")
	}
	for i := range reply.Elements {
		el := &reply.Elements[i]
		if el.ProcessID != w.PID {
			return Observation{}, Error("CONTROL_STALE_TARGET")
		}
		el.ID = fingerprint([]any{w.ID, el.RuntimeID, el.NativeHandle, el.ParentPath, el.AutomationID, el.ControlType})
	}
	return Observation{
		Window: w, Elements: reply.Elements, Truncated: reply.Truncated,
		Modal: reply.Modal, ObservedAt: w.ObservedAt,
	}, nil
}

func (d *windowsDriver) Execute(ctx context.Context, action Proposal) (Outcome, error) {
	before := Outcome{Effect: "NOT_STARTED"}
	if !d.Available() {
		return before, Error("CONTROL_UNAVAILABLE")
	}
	ctx, done := d.withLifetime(ctx)
	defer done()
	if ctx.Err() != nil {
		return before, Error("CONTROL_STOPPED")
	}
	if action.Type == LaunchHost && action.Window == nil && action.Element == nil && action.Text == "" && action.Expected == "HOST_VISIBLE" {
		return d.launch(ctx)
	}
	if action.Window == nil || action.Window.App != TestHost && action.Window.App != Notepad {
		return before, Error("CONTROL_ACTION_DENIED")
	}
	w, hwnd, ok := d.refresh(*action.Window)
	if !ok {
		return before, Error("CONTROL_STALE_TARGET")
	}
	if w.App == TestHost && action.Type == Focus && action.Expected == "TARGET_FOREGROUND" && action.Element == nil && action.Text == "" {
		return d.focus(ctx, w, hwnd)
	}
	var op string
	var err error
	if w.App == Notepad {
		op, err = notepadOperation(action, w)
		if err == nil {
			var observed Observation
			observed, err = d.Inspect(ctx, w)
			if err == nil && !NotepadTextTarget(observed, action.Element.ID) {
				err = Error("CONTROL_STALE_TARGET")
			}
		}
	} else {
		op, err = fixtureOperation(action, w)
	}
	if err != nil {
		return before, err
	}
	reply, started, err := d.callWorker(ctx, workerRequest{Op: op, Window: w, Element: action.Element, Text: action.Text})
	if err != nil {
		if started {
			return Outcome{Effect: "UNKNOWN"}, err
		}
		return before, err
	}
	switch reply.Effect {
	case "ISSUED":
		if reply.Code == "" {
			return Outcome{Effect: "ISSUED", Window: &w}, nil
		}
	case "NOT_STARTED":
		if reply.Code != "" {
			return before, workerError(reply.Code)
		}
	case "UNKNOWN":
		if reply.Code != "" {
			return Outcome{Effect: "UNKNOWN"}, workerError(reply.Code)
		}
	}
	return Outcome{Effect: "UNKNOWN"}, Error("CONTROL_UNKNOWN_RESULT")
}

func workerError(code string) Error {
	switch code {
	case "CONTROL_ACTION_DENIED", "CONTROL_INPUT_REJECTED", "CONTROL_STALE_TARGET", "CONTROL_QUERY_TIMEOUT", "CONTROL_UNAVAILABLE", "CONTROL_UNKNOWN_RESULT":
		return Error(code)
	default:
		return Error("CONTROL_UNKNOWN_RESULT")
	}
}

func fixtureOperation(action Proposal, w Window) (string, error) {
	el := action.Element
	if el == nil || el.ProcessID != w.PID || !el.Enabled || el.Offscreen || el.Password {
		return "", Error("CONTROL_ACTION_DENIED")
	}
	switch action.Type {
	case Invoke:
		expected := map[string]string{"SubmitButton": "TEST_SUBMITTED", "ResetButton": "TEST_RESET"}
		if action.Text == "" && el.ControlType == "Button" && slices.Contains(el.Patterns, "Invoke") &&
			expected[el.AutomationID] != "" && expected[el.AutomationID] == action.Expected {
			return "invoke", nil
		}
	case SetValue, TypeText:
		if !ValidateText(action.Text) {
			return "", Error("CONTROL_INPUT_REJECTED")
		}
		if el.AutomationID == "TestInput" && el.ControlType == "Edit" && slices.Contains(el.Patterns, "Value") && action.Expected == "TEXT_MATCH" {
			if action.Type == SetValue {
				return "set_value", nil
			}
			return "type_text", nil
		}
	}
	return "", Error("CONTROL_ACTION_DENIED")
}

func (d *windowsDriver) focus(ctx context.Context, w Window, hwnd uintptr) (Outcome, error) {
	before := Outcome{Effect: "NOT_STARTED"}
	obs, err := d.Inspect(ctx, w)
	if err != nil {
		return before, err
	}
	if obs.Modal || obs.Truncated {
		return before, Error("CONTROL_STALE_TARGET")
	}
	_, _, ok := d.refresh(w)
	enabled, _, _ := isWindowEnabled.Call(hwnd)
	minimized, _, _ := isIconic.Call(hwnd)
	popup, _, _ := getLastActivePopup.Call(hwnd)
	if !ok || enabled == 0 || minimized != 0 || popup != hwnd || windowPID(hwnd) != w.PID {
		return before, Error("CONTROL_STALE_TARGET")
	}
	if ctx.Err() != nil {
		return before, Error("CONTROL_STOPPED")
	}
	// Do not synthesize input or attach to the other thread's input queue.
	setForegroundWindow.Call(hwnd)
	return Outcome{Effect: "ISSUED", Window: &w}, nil
}

func (d *windowsDriver) startHost(ctx context.Context) (*hostProcess, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.host != nil {
		return nil, false, Error("CONTROL_BUSY")
	}
	binary, err := d.pinBinary(hostFile, hostSHA256)
	if err != nil {
		return nil, false, err
	}
	if ctx.Err() != nil || d.lifetime.Err() != nil {
		binary.Close()
		return nil, false, Error("CONTROL_STOPPED")
	}
	cmd := exec.Command(binary.path)
	d.configure(cmd)
	// LaunchHost is an explicitly approved visible fixture action.
	cmd.SysProcAttr.HideWindow = false
	input, err := cmd.StdinPipe()
	if err != nil {
		binary.Close()
		return nil, false, Error("CONTROL_UNAVAILABLE")
	}
	if err := cmd.Start(); err != nil {
		input.Close()
		binary.Close()
		return nil, false, Error("CONTROL_UNAVAILABLE")
	}
	// Record ownership before exposing any fixture window. Keep the verified
	// image and runtime config locked until the user closes the fixture.
	host := &hostProcess{pid: uint32(cmd.Process.Pid), path: binary.path, input: input, exited: make(chan struct{})}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, host.pid)
	if err == nil {
		identity, ok := queryProcess(handle, host.pid)
		windows.CloseHandle(handle)
		if ok && strings.EqualFold(identity.path, host.path) && identity.integrity == d.self.integrity &&
			identity.user == d.self.user && identity.session == d.self.session {
			host.creation = identity.creation
		}
	}
	d.host = host
	go func() {
		_ = cmd.Wait()
		input.Close()
		binary.Close()
		d.mu.Lock()
		d.host = nil
		d.mu.Unlock()
		close(host.exited)
	}()
	if host.creation == "" {
		return nil, true, Error("CONTROL_UNKNOWN_RESULT")
	}
	return host, true, nil
}

func (d *windowsDriver) launch(ctx context.Context) (Outcome, error) {
	host, started, err := d.startHost(ctx)
	if err != nil {
		if started {
			return Outcome{Effect: "UNKNOWN"}, err
		}
		return Outcome{Effect: "NOT_STARTED"}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for d.ownedHost() == host {
		handles, err := listWindowHandles(ctx)
		if err != nil {
			break
		}
		for _, hwnd := range handles {
			if windowPID(hwnd) != host.pid {
				continue
			}
			if w, ok := d.observeWindow(hwnd); ok && w.App == TestHost {
				return Outcome{Effect: "ISSUED", Window: &w}, nil
			}
		}
		select {
		case <-ticker.C:
			continue
		case <-ctx.Done():
		}
		break
	}
	return Outcome{Effect: "UNKNOWN"}, queryError(ctx, "CONTROL_UNKNOWN_RESULT")
}

func (d *windowsDriver) Verify(ctx context.Context, action Proposal, outcome Outcome) (Verification, error) {
	fail := Verification{Code: "CONTROL_UNKNOWN_RESULT"}
	if outcome.Effect != "ISSUED" || outcome.Window == nil {
		return fail, nil
	}
	ctx, done := d.withLifetime(ctx)
	defer done()
	if !d.Available() || ctx.Err() != nil {
		return fail, Error("CONTROL_QUERY_TIMEOUT")
	}
	switch action.Type {
	case LaunchHost, Focus:
		w, _, ok := d.refresh(*outcome.Window)
		if ok && w.App == TestHost && ((action.Type == LaunchHost && action.Expected == "HOST_VISIBLE") ||
			(action.Type == Focus && action.Expected == "TARGET_FOREGROUND" && w.Foreground)) {
			return Verification{Verified: true}, nil
		}
	case Invoke, SetValue, TypeText:
		current := *outcome.Window
		if current.App == Notepad {
			// Content may change the title; acquire the same native target and
			// accept only that title change for the independent post-effect read.
			number, err := strconv.ParseUint(current.HWND, 10, strconv.IntSize)
			if err != nil {
				return fail, nil
			}
			live, ok := d.observeWindow(uintptr(number))
			expected := current
			expected.Title = live.Title
			if !ok || !sameWindow(expected, live) {
				return fail, nil
			}
			current = live
		}
		obs, err := d.Inspect(ctx, current)
		if err != nil {
			return fail, err
		}
		if (current.App == TestHost && verifyFixture(action, obs)) || (current.App == Notepad && verifyNotepad(action, obs)) {
			return Verification{Verified: true}, nil
		}
	}
	return fail, nil
}

func verifyFixture(action Proposal, obs Observation) bool {
	if obs.Modal || obs.Truncated {
		return false
	}
	var unique int
	var matched bool
	for _, el := range obs.Elements {
		if el.ProcessID != obs.Window.PID || el.Offscreen || el.Password {
			continue
		}
		switch action.Type {
		case Invoke:
			if el.AutomationID == "ResultLabel" && el.ControlType == "Text" {
				unique++
				matched = (action.Expected == "TEST_SUBMITTED" && el.Name == "Submitted") ||
					(action.Expected == "TEST_RESET" && el.Name == "Reset")
			}
		case SetValue, TypeText:
			if action.Element != nil && action.Expected == "TEXT_MATCH" && el.ID == action.Element.ID &&
				el.AutomationID == "TestInput" && el.ControlType == "Edit" && slices.Contains(el.Patterns, "Value") {
				unique++
				digest := sha256.Sum256([]byte(action.Text))
				matched = el.ValueSHA256 == hex.EncodeToString(digest[:])
			}
		}
	}
	return unique == 1 && matched
}
