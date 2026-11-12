package computer

import (
	"encoding/json"
	"testing"

	"deskpilot/internal/screen"
)

func blankNotepad(t *testing.T) Observation {
	t.Helper()
	w := Window{ID: "np", App: Notepad, HWND: "101", PID: 7, CreationTime: "123", ClassName: "Notepad", Integrity: 0x2000,
		PackageFamily: "Microsoft.WindowsNotepad_8wekyb3d8bbwe", AppUserModelID: "Microsoft.WindowsNotepad_8wekyb3d8bbwe!App",
		ImagePath: `C:\Program Files\WindowsApps\Microsoft.WindowsNotepad_1.0.0.0_x64__8wekyb3d8bbwe\Notepad\Notepad.exe`, Bounds: screen.Rect{Width: 800, Height: 600}}
	var e Element
	if err := json.Unmarshal([]byte(`{"id":"editor","controlType":"Document","automationId":"TextEditor","enabled":true,"processId":7,"runtimeId":"1.2.3","parentPath":"1.2","depth":2,"blank":true,"valueWritable":true,"patterns":["Text","Value"],"bounds":{"x":20,"y":50,"width":500,"height":400},"valueSha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}`), &e); err != nil {
		t.Fatal(err)
	}
	return Observation{Window: w, Elements: []Element{e}}
}

func TestNotepadTargetRequiresOneProvenBlankWritableEditor(t *testing.T) {
	o := blankNotepad(t)
	if !NotepadTextTarget(o, "editor") {
		t.Fatal("single blank ValuePattern editor should be conditionally eligible")
	}
	for _, tc := range []struct {
		name   string
		change func(*Observation)
	}{
		{"nonempty", func(o *Observation) { o.Elements[0].Blank = false }},
		{"missing hash", func(o *Observation) { o.Elements[0].ValueSHA256 = "" }},
		{"text only", func(o *Observation) { o.Elements[0].Patterns = []string{"Text"} }},
		{"search box alone", func(o *Observation) { o.Elements[0].ControlType = "Edit"; o.Elements[0].AutomationID = "FindText" }},
		{"read only", func(o *Observation) { o.Elements[0].ValueWritable = false }},
		{"wrong process", func(o *Observation) { o.Elements[0].ProcessID++ }},
		{"ambiguous editor", func(o *Observation) { o.Elements = append(o.Elements, o.Elements[0]) }},
		{"hidden second editor", func(o *Observation) { e := o.Elements[0]; e.Offscreen = true; o.Elements = append(o.Elements, e) }},
		{"modal", func(o *Observation) { o.Modal = true }},
		{"truncated", func(o *Observation) { o.Truncated = true }},
		{"legacy", func(o *Observation) { o.Window.PackageFamily = "" }},
		{"wrong app", func(o *Observation) { o.Window.App = TestHost }},
		{"outside window", func(o *Observation) { o.Elements[0].Bounds.X = 900 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := blankNotepad(t)
			tc.change(&o)
			if NotepadTextTarget(o, "editor") {
				t.Fatal("unsafe editor eligible")
			}
		})
	}
}

func TestNotepadVerifierRequiresIndependentMatchingIdentityAndHash(t *testing.T) {
	o := blankNotepad(t)
	p := Proposal{Type: TypeText, Window: &o.Window, Element: &o.Elements[0], Text: "Hello DeskPilot", Expected: "TEXT_MATCH"}
	after := blankNotepad(t)
	after.Elements[0].Blank = false
	after.Elements[0].ValueSHA256 = "0eebadf51de0b866df6e933b8116251191640c21a1ecc9b9debb022d1f9a4047"
	if !verifyNotepad(p, after) {
		t.Fatal("fresh exact text hash should verify")
	}
	for _, change := range []func(*Observation){
		func(o *Observation) { o.Elements[0].ValueSHA256 = "wrong" },
		func(o *Observation) { o.Elements[0].RuntimeID = "replacement" },
		func(o *Observation) { o.Elements[0].Bounds.X++ },
		func(o *Observation) { o.Window.CreationTime = "replacement" },
		func(o *Observation) { o.Elements = append(o.Elements, o.Elements[0]) },
	} {
		o := after
		o.Elements = append([]Element(nil), after.Elements...)
		change(&o)
		if verifyNotepad(p, o) {
			t.Fatal("mismatched independent observation verified")
		}
	}
}
