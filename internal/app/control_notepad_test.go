package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"deskpilot/internal/computer"
)

func notepadFixture(t *testing.T) *controlFixtureDriver {
	t.Helper()
	d := newControlFixtureDriver()
	d.window.App = computer.Notepad
	d.window.ClassName = "Notepad"
	d.window.ImagePath = `C:\Program Files\WindowsApps\Microsoft.WindowsNotepad_1.0.0.0_x64__8wekyb3d8bbwe\Notepad\Notepad.exe`
	d.window.PackageFamily = "Microsoft.WindowsNotepad_8wekyb3d8bbwe"
	d.window.AppUserModelID = d.window.PackageFamily + "!App"
	d.element.Name = ""
	d.element.AutomationID = "TextEditor"
	d.element.ControlType = "Document"
	d.element.ValueSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	// Exercise the wire representation; absence of a writable witness must deny.
	raw := encode(d.element)
	var fields map[string]any
	if e := json.Unmarshal([]byte(raw), &fields); e != nil {
		t.Fatal(e)
	}
	fields["valueWritable"] = true
	if e := json.Unmarshal([]byte(encode(fields)), &d.element); e != nil {
		t.Fatal(e)
	}
	return d
}

func TestNotepadApprovalRequiresFreshBlankValueEditor(t *testing.T) {
	s := aiFixture(t, nil)
	d := notepadFixture(t)
	s.desktopControl.driver = d
	o := observeControlFixture(t, s)
	p := mustCall(t, s, "ProposeControlAction", map[string]string{"type": string(computer.TypeText), "observationId": o.ID, "elementId": o.Elements[0].ID, "text": "Hello DeskPilot"}).(computer.Proposal)
	if d.calls != 0 || p.Window.App != computer.Notepad || p.Expected != "TEXT_MATCH" {
		t.Fatal("proposal dispatched or lost exact target")
	}
	// A user typing after preview revokes the approved blank-document witness.
	d.element.Blank = false
	d.element.ValueSHA256 = "changed"
	r := mustCall(t, s, "ApproveControlAction", controlApproval(p)).(computer.Record)
	if r.Status != "STALE_TARGET" || d.calls != 0 {
		t.Fatalf("nonblank editor was dispatched: %+v", r)
	}
}

func TestNotepadPolicyRejectsUnsafeEditorAndAction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*controlFixtureDriver)
		action computer.ActionType
	}{
		{"nonempty", func(d *controlFixtureDriver) { d.element.Blank = false }, computer.TypeText},
		{"unread", func(d *controlFixtureDriver) { d.element.ValueSHA256 = "" }, computer.TypeText},
		{"text only", func(d *controlFixtureDriver) { d.element.Patterns = []string{"Text"} }, computer.TypeText},
		{"password", func(d *controlFixtureDriver) { d.element.Password = true }, computer.TypeText},
		{"disabled", func(d *controlFixtureDriver) { d.element.Enabled = false }, computer.TypeText},
		{"offscreen", func(d *controlFixtureDriver) { d.element.Offscreen = true }, computer.TypeText},
		{"invoke", func(d *controlFixtureDriver) {}, computer.Invoke},
		{"focus", func(d *controlFixtureDriver) {}, computer.Focus},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := aiFixture(t, nil)
			d := notepadFixture(t)
			tc.change(d)
			s.desktopControl.driver = d
			o := observeControlFixture(t, s)
			screenError(t, s, "ProposeControlAction", map[string]string{"type": string(tc.action), "observationId": o.ID, "elementId": o.Elements[0].ID, "text": "Hello DeskPilot"}, "CONTROL_ACTION_DENIED")
			if d.calls != 0 {
				t.Fatal("unsafe Notepad dispatch")
			}
		})
	}
}

type notepadTreeDriver struct {
	*controlFixtureDriver
	extra []computer.Element
}

func (d *notepadTreeDriver) Inspect(ctx context.Context, w computer.Window) (computer.Observation, error) {
	o, err := d.controlFixtureDriver.Inspect(ctx, w)
	o.Elements = append(o.Elements, d.extra...)
	return o, err
}

func TestNotepadApprovalRejectsNewAmbiguousEditor(t *testing.T) {
	s := aiFixture(t, nil)
	d := &notepadTreeDriver{controlFixtureDriver: notepadFixture(t)}
	s.desktopControl.driver = d
	o := observeControlFixture(t, s)
	p := mustCall(t, s, "ProposeControlAction", map[string]string{"type": string(computer.SetValue), "observationId": o.ID, "elementId": o.Elements[0].ID, "text": "Harmless sample"}).(computer.Proposal)
	e := d.element
	e.ID = "new-search"
	e.Offscreen = true
	d.extra = []computer.Element{e}
	r := mustCall(t, s, "ApproveControlAction", controlApproval(p)).(computer.Record)
	if r.Status != "STALE_TARGET" || d.calls != 0 {
		t.Fatalf("new editor bypassed blank-document revalidation: %+v", r)
	}
}

func TestNotepadConditionalCapabilityNeedsFreshInspectionAndSingleApproval(t *testing.T) {
	s := aiFixture(t, nil)
	d := notepadFixture(t)
	s.desktopControl.driver = d
	clock := time.Now()
	s.desktopControl.clock = func() time.Time { return clock }
	status := func() map[string]any { return mustCall(t, s, "GetControlStatus", struct{}{}).(map[string]any) }
	if got := status(); got["notepadText"] != false || got["rawInput"] != false || got["notepadTextStatus"] != "CONDITIONAL" {
		t.Fatalf("blanket capability claimed: %+v", got)
	}
	o := observeControlFixture(t, s)
	if status()["notepadText"] != true {
		t.Fatal("fresh blank editor missing conditional capability")
	}
	clock = clock.Add(61 * time.Second)
	if status()["notepadText"] != false {
		t.Fatal("expired inspection advertised writable capability")
	}
	o = observeControlFixture(t, s)
	p := mustCall(t, s, "ProposeControlAction", map[string]string{"type": string(computer.TypeText), "observationId": o.ID, "elementId": o.Elements[0].ID, "text": "Harmless sample"}).(computer.Proposal)
	r := mustCall(t, s, "ApproveControlAction", controlApproval(p)).(computer.Record)
	if r.Status != "COMPLETED" || !r.Verified || d.calls != 1 {
		t.Fatalf("fake-backed approval chain failed: %+v", r)
	}
	screenError(t, s, "ApproveControlAction", controlApproval(p), "CONTROL_APPROVAL_USED")
	if status()["notepadText"] != false {
		t.Fatal("consumed blank witness remained advertised")
	}
}
