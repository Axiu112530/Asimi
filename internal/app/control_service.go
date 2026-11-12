package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"deskpilot/internal/computer"
	"deskpilot/internal/domain"
)

type controlState struct {
	mu           sync.Mutex
	driver       computer.Driver
	clock        func() time.Time
	windows      map[string]computer.Window
	windowsUntil time.Time
	observation  *computer.Observation
	pending      *computer.Proposal
	halted       atomic.Bool
	signalMu     sync.Mutex
	working      bool
	cancel       context.CancelFunc
}

func (c *controlState) time() time.Time {
	if c.clock != nil {
		return c.clock().UTC()
	}
	return time.Now().UTC()
}

func hashControlText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func controlHash(value any) string {
	raw, _ := json.Marshal(value)
	return hashControlText(string(raw))
}

func proposalHash(p computer.Proposal) string {
	p.Hash = ""
	p.Status = ""
	return controlHash(p)
}

func controlTargetHash(p computer.Proposal) string {
	return controlHash(struct {
		Window  *computer.Window
		Element *computer.Element
	}{p.Window, p.Element})
}

func sameControlWindow(a, b computer.Window) bool {
	a.Foreground, b.Foreground = false, false
	a.ObservedAt, b.ObservedAt = "", ""
	return a == b
}

func sameControlElement(a, b computer.Element) bool {
	// UIA references are freshly acquired. Runtime identity and semantic selectors
	// remain bound while the approval dialog may change keyboard focus.
	a.ID, b.ID = "", ""
	a.Focused, b.Focused = false, false
	return reflect.DeepEqual(a, b)
}

func (s *Service) callControl(method, payload string) (domain.Reply, bool) {
	if reply, handled := s.callComputerSuggestion(method, payload); handled {
		return reply, true
	}
	switch method {
	case "ListComputerTaskEvidence":
		var q struct {
			TaskID string `json:"taskId"`
		}
		if e := decode(payload, &q); e != nil {
			return result(nil, e), true
		}
		s.desktopControl.mu.Lock()
		err := s.expireControlPending()
		s.desktopControl.mu.Unlock()
		if err != nil {
			return result(nil, err), true
		}
		rows, err := s.computerTaskEvidence(q.TaskID)
		return result(rows, err), true
	case "GetControlStatus", "ListControlWindows", "InspectControlWindow", "ProposeControlAction", "ApproveControlAction", "RejectControlAction", "ListControlJournal", "StopComputerControl", "ResumeComputerControl":
	default:
		return domain.Reply{}, false
	}
	c := &s.desktopControl
	c.mu.Lock()
	if c.driver == nil {
		c.driver = computer.NewDriver()
	}
	driver := c.driver
	c.mu.Unlock()
	switch method {
	case "GetControlStatus", "ListControlWindows", "ListControlJournal", "StopComputerControl", "ResumeComputerControl":
		if e := decode(payload, &struct{}{}); e != nil {
			return fail(errorCode(e)), true
		}
	}
	switch method {
	case "GetControlStatus":
		stopped := c.halted.Load()
		available := true
		if d, ok := driver.(interface{ Available() bool }); ok {
			available = d.Available()
		}
		c.mu.Lock()
		notepadText := false
		if o := c.observation; o != nil {
			stamp, err := time.Parse(chatTimeFormat, o.ObservedAt)
			notepadText = err == nil && c.time().Sub(stamp) <= time.Minute && computer.NotepadTextTarget(*o, "")
		}
		c.mu.Unlock()
		return result(map[string]any{"stopped": stopped, "available": available, "permissionMode": "PER_ACTION_APPROVAL", "rawInput": false, "notepadText": available && !stopped && notepadText, "notepadTextStatus": "CONDITIONAL"}, nil), true
	case "StopComputerControl":
		return result(true, s.stopDesktopControl()), true
	case "ResumeComputerControl":
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.working {
			return fail("CONTROL_BUSY"), true
		}
		c.halted.Store(false)
		return result(true, nil), true
	case "ListControlJournal":
		c.mu.Lock()
		err := s.expireControlPending()
		c.mu.Unlock()
		if err != nil {
			return result(nil, err), true
		}
		rows, e := listControlRecords(s.db)
		return result(rows, e), true
	case "ListControlWindows":
		ctx, cancel := context.WithTimeout(s.lifetime, 6*time.Second)
		defer cancel()
		windows, e := driver.Enumerate(ctx)
		if e != nil {
			return result(nil, e), true
		}
		allowed := []computer.Window{}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.windows = map[string]computer.Window{}
		for _, w := range windows {
			if len(allowed) >= 32 {
				break
			}
			if !validControlWindow(w) {
				continue
			}
			c.windows[w.ID] = w
			allowed = append(allowed, w)
		}
		c.windowsUntil = c.time().Add(time.Minute)
		return result(allowed, nil), true
	case "InspectControlWindow":
		var q struct {
			WindowID string `json:"windowId"`
		}
		if e := decode(payload, &q); e != nil {
			return fail(errorCode(e)), true
		}
		c.mu.Lock()
		w, ok := c.windows[q.WindowID]
		live := c.time().Before(c.windowsUntil)
		c.mu.Unlock()
		if !ok || !live {
			return fail("CONTROL_STALE_TARGET"), true
		}
		ctx, cancel := context.WithTimeout(s.lifetime, 6*time.Second)
		defer cancel()
		observation, e := driver.Inspect(ctx, w)
		if e != nil {
			return result(nil, e), true
		}
		if !sameControlWindow(w, observation.Window) || len(observation.Elements) > 500 {
			return fail("CONTROL_STALE_TARGET"), true
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		observation.ID = newID()
		observation.ObservedAt = c.time().Format(chatTimeFormat)
		c.observation = &observation
		return result(observation, nil), true
	case "ProposeControlAction":
		var q struct {
			Type          computer.ActionType `json:"type"`
			ObservationID string              `json:"observationId"`
			ElementID     string              `json:"elementId"`
			Text          string              `json:"text"`
			TaskID        string              `json:"taskId"`
		}
		if e := decode(payload, &q); e != nil {
			return fail(errorCode(e)), true
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.halted.Load() || s.lifetime.Err() != nil {
			return fail("CONTROL_STOPPED"), true
		}
		if e := s.expireControlPending(); e != nil {
			return result(nil, e), true
		}
		if c.working || c.pending != nil {
			return fail("CONTROL_BUSY"), true
		}
		p := computer.Proposal{ID: newID(), TaskID: q.TaskID, Type: q.Type, Text: q.Text, PolicyVersion: computer.PolicyVersion, Status: "AWAITING_APPROVAL", ObservedAt: c.time().Format(chatTimeFormat), ExpiresAt: c.time().Add(45 * time.Second).Format(chatTimeFormat)}
		if q.Type == computer.LaunchHost {
			if q.ObservationID != "" || q.ElementID != "" || q.Text != "" || q.TaskID != "" {
				return fail("CONTROL_ACTION_DENIED"), true
			}
			p.Expected = "HOST_VISIBLE"
		} else {
			o := c.observation
			if o == nil || o.ID != q.ObservationID || o.Modal || o.Truncated {
				return fail("CONTROL_STALE_TARGET"), true
			}
			observed, e := time.Parse(chatTimeFormat, o.ObservedAt)
			if e != nil || c.time().Sub(observed) > time.Minute {
				return fail("CONTROL_STALE_TARGET"), true
			}
			if o.Window.App != computer.TestHost && o.Window.App != computer.Notepad {
				return fail("CONTROL_ACTION_DENIED"), true
			}
			if o.Window.App == computer.Notepad && (q.Type != computer.SetValue && q.Type != computer.TypeText || !computer.NotepadTextTarget(*o, q.ElementID)) {
				return fail("CONTROL_ACTION_DENIED"), true
			}
			w := o.Window
			p.Window = &w
			if q.Type == computer.Focus {
				if q.ElementID != "" || q.Text != "" {
					return fail("CONTROL_ACTION_DENIED"), true
				}
				p.Expected = "TARGET_FOREGROUND"
			} else {
				for _, element := range o.Elements {
					if element.ID == q.ElementID {
						copy := element
						p.Element = &copy
						break
					}
				}
				if !validControlAction(p) {
					return fail("CONTROL_ACTION_DENIED"), true
				}
				if q.Type == computer.Invoke {
					if p.Element.AutomationID == "SubmitButton" {
						p.Expected = "TEST_SUBMITTED"
					} else {
						p.Expected = "TEST_RESET"
					}
				} else {
					if !computer.ValidateText(q.Text) {
						return fail("CONTROL_INPUT_REJECTED"), true
					}
					p.Expected = "TEXT_MATCH"
				}
			}
		}
		p.TargetHash = controlTargetHash(p)
		p.Hash = proposalHash(p)
		if e := saveControlProposal(s.db, p); e != nil {
			return result(nil, e), true
		}
		c.pending = &p
		return result(p, nil), true
	case "ApproveControlAction", "RejectControlAction":
		var q struct {
			ActionID   string `json:"actionId"`
			ActionHash string `json:"actionHash"`
		}
		if e := decode(payload, &q); e != nil {
			return fail(errorCode(e)), true
		}
		row, e := s.decideControl(q.ActionID, q.ActionHash, method == "ApproveControlAction")
		return result(row, e), true
	}
	return fail("CONTROL_ACTION_DENIED"), true
}

func validControlWindow(w computer.Window) bool {
	return (w.App == computer.TestHost || w.App == computer.Notepad) && w.ID != "" && w.HWND != "" && w.PID != 0 && w.CreationTime != "" && w.ImagePath != "" && w.ClassName != "" && w.Integrity == 0x2000
}

func validControlAction(p computer.Proposal) bool {
	e := p.Element
	if e == nil || p.Window == nil || e.ProcessID != p.Window.PID || !e.Enabled || e.Offscreen || e.Password || e.RuntimeID == "" || e.ParentPath == "" || e.Bounds.Width <= 0 || e.Bounds.Height <= 0 {
		return false
	}
	if p.Window.App == computer.Notepad {
		return (p.Type == computer.SetValue || p.Type == computer.TypeText) &&
			computer.NotepadTextTarget(computer.Observation{Window: *p.Window, Elements: []computer.Element{*e}}, e.ID)
	}
	if p.Window.App != computer.TestHost {
		return false
	}
	has := func(pattern string) bool {
		for _, s := range e.Patterns {
			if s == pattern {
				return true
			}
		}
		return false
	}
	switch p.Type {
	case computer.Invoke:
		return p.Text == "" && e.ControlType == "Button" && (e.AutomationID == "SubmitButton" || e.AutomationID == "ResetButton") && has("Invoke")
	case computer.SetValue, computer.TypeText:
		return e.ControlType == "Edit" && e.AutomationID == "TestInput" && has("Value")
	}
	return false
}

func (s *Service) decideControl(id, hash string, approve bool) (computer.Record, error) {
	c := &s.desktopControl
	c.mu.Lock()
	if c.halted.Load() {
		c.mu.Unlock()
		return computer.Record{}, codedError("CONTROL_STOPPED")
	}
	if c.pending == nil {
		c.mu.Unlock()
		return computer.Record{}, codedError("CONTROL_APPROVAL_USED")
	}
	p := *c.pending
	if id != p.ID || hash != p.Hash || proposalHash(p) != hash {
		c.mu.Unlock()
		return computer.Record{}, codedError("CONTROL_APPROVAL_MISMATCH")
	}
	stamp := c.time()
	expiry, e := time.Parse(chatTimeFormat, p.ExpiresAt)
	if e != nil || !stamp.Before(expiry) {
		row := computer.Record{ID: p.ID, Type: p.Type, Status: "EXPIRED", Code: "CONTROL_EXPIRED", At: stamp.Format(chatTimeFormat)}
		e = finishControl(s.db, row, "AWAITING_APPROVAL")
		if e == nil {
			c.pending = nil
		}
		c.mu.Unlock()
		if e != nil {
			return row, e
		}
		return row, codedError("CONTROL_EXPIRED")
	}
	if !approve {
		row := computer.Record{ID: p.ID, Type: p.Type, Status: "REJECTED", At: stamp.Format(chatTimeFormat)}
		e = finishControl(s.db, row, "AWAITING_APPROVAL")
		if e == nil {
			c.pending = nil
		}
		c.mu.Unlock()
		return row, e
	}
	if e = saveControlApproval(s.db, p, stamp.Format(chatTimeFormat)); e != nil {
		c.mu.Unlock()
		return computer.Record{}, e
	}
	ctx, cancel := context.WithTimeout(s.lifetime, 20*time.Second)
	c.pending = nil
	if p.Window != nil && p.Window.App == computer.Notepad {
		c.observation = nil
	}
	c.working = true
	c.signalMu.Lock()
	c.cancel = cancel
	c.signalMu.Unlock()
	driver := c.driver
	c.mu.Unlock()
	defer func() {
		cancel()
		c.mu.Lock()
		c.signalMu.Lock()
		c.cancel = nil
		c.signalMu.Unlock()
		if p.TaskID != "" {
			// A new task-linked action requires an observation made after this
			// effect settles, including when the result could not be verified.
			c.observation = nil
		}
		c.working = false
		c.mu.Unlock()
	}()
	before := "REVALIDATING"
	finish := func(status, code string, verified bool) (computer.Record, error) {
		row := computer.Record{ID: p.ID, Type: p.Type, Status: status, Code: code, Verified: verified, At: time.Now().UTC().Format(chatTimeFormat)}
		return row, finishControl(s.db, row, before)
	}
	if p.Window != nil {
		o, err := driver.Inspect(ctx, *p.Window)
		if err != nil {
			return finish("STALE_TARGET", driverCode(err, "CONTROL_STALE_TARGET"), false)
		}
		if o.Modal || o.Truncated || !sameControlWindow(*p.Window, o.Window) {
			return finish("STALE_TARGET", "CONTROL_STALE_TARGET", false)
		}
		if p.Element != nil {
			matches := 0
			for _, element := range o.Elements {
				if sameControlElement(*p.Element, element) {
					matches++
				}
			}
			if matches != 1 {
				return finish("STALE_TARGET", "CONTROL_STALE_TARGET", false)
			}
			if p.Window.App == computer.Notepad && !computer.NotepadTextTarget(o, p.Element.ID) {
				return finish("STALE_TARGET", "CONTROL_STALE_TARGET", false)
			}
		}
	}
	c.mu.Lock()
	if c.halted.Load() || ctx.Err() != nil {
		c.mu.Unlock()
		return finish("CANCELLED", "CONTROL_STOPPED", false)
	}
	if p.ObservationID != "" && (c.observation == nil || c.observation.ID != p.ObservationID) {
		c.mu.Unlock()
		return finish("STALE_TARGET", "CONTROL_STALE_TARGET", false)
	}
	if !c.time().Before(expiry) {
		c.mu.Unlock()
		return finish("EXPIRED", "CONTROL_EXPIRED", false)
	}
	e = consumeControlApproval(s.db, p, c.time().Format(chatTimeFormat))
	c.mu.Unlock()
	if e != nil {
		return finish("FAILED", errorCode(e), false)
	}
	before = "EXECUTING"
	s.markCheckpoint("control.intent_persisted")
	if ctx.Err() != nil {
		return finish("CANCELLED", "CONTROL_STOPPED", false)
	}
	outcome, err := driver.Execute(ctx, p)
	s.markCheckpoint("control.effect_returned")
	if outcome.Effect == "NOT_STARTED" {
		if ctx.Err() != nil {
			return finish("CANCELLED", "CONTROL_STOPPED", false)
		}
		return finish("FAILED", driverCode(err, "CONTROL_ACTION_DENIED"), false)
	}
	if ctx.Err() != nil {
		return finish("RECOVERY_REQUIRED", "CONTROL_UNKNOWN_RESULT", false)
	}
	if err != nil || outcome.Effect != "ISSUED" {
		return finish("UNKNOWN_RESULT", driverCode(err, "CONTROL_UNKNOWN_RESULT"), false)
	}
	verifying := computer.Record{ID: p.ID, Type: p.Type, Status: "VERIFYING", At: time.Now().UTC().Format(chatTimeFormat)}
	if e = storeTransaction(s.db, func(tx *sql.Tx) error { return controlTransition(tx, verifying, before) }); e != nil {
		return computer.Record{}, e
	}
	before = "VERIFYING"
	verification, err := driver.Verify(ctx, p, outcome)
	s.markCheckpoint("control.verification_returned")
	if ctx.Err() != nil {
		return finish("RECOVERY_REQUIRED", "CONTROL_UNKNOWN_RESULT", false)
	}
	if err != nil || !verification.Verified {
		return finish("UNKNOWN_RESULT", driverCode(err, "CONTROL_UNKNOWN_RESULT"), false)
	}
	row, err := finish("COMPLETED", "", true)
	if err == nil {
		s.markCheckpoint("control.final_committed")
	}
	return row, err
}

func (c *controlState) signalStop() {
	c.halted.Store(true)
	c.signalMu.Lock()
	defer c.signalMu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

func (s *Service) stopDesktopControl() error {
	c := &s.desktopControl
	c.signalStop()
	c.mu.Lock()
	defer c.mu.Unlock()
	// Serialize with an explicitly concurrent Resume call as well as setting
	// the immediate cancellation flag above.
	c.halted.Store(true)
	c.observation = nil
	if c.pending == nil {
		return nil
	}
	p := c.pending
	e := finishControl(s.db, computer.Record{ID: p.ID, Type: p.Type, Status: "CANCELLED", Code: "CONTROL_STOPPED", At: c.time().Format(chatTimeFormat)}, "AWAITING_APPROVAL")
	// An in-memory authorization is revoked even if its cancellation cannot be
	// persisted. Startup recovery will invalidate the remaining durable row.
	c.pending = nil
	return e
}

// Caller holds the broker state mutex.
func (s *Service) expireControlPending() error {
	c := &s.desktopControl
	if c.pending == nil {
		return nil
	}
	p := c.pending
	expires, e := time.Parse(chatTimeFormat, p.ExpiresAt)
	if e == nil && c.time().Before(expires) {
		return nil
	}
	e = finishControl(s.db, computer.Record{ID: p.ID, Type: p.Type, Status: "EXPIRED", Code: "CONTROL_EXPIRED", At: c.time().Format(chatTimeFormat)}, "AWAITING_APPROVAL")
	if e == nil {
		c.pending = nil
	}
	return e
}

func (s *Service) closeDesktopControl() error {
	e := s.stopDesktopControl()
	c := &s.desktopControl
	c.mu.Lock()
	driver := c.driver
	c.mu.Unlock()
	if driver != nil {
		return errors.Join(e, driver.Close())
	}
	return e
}
