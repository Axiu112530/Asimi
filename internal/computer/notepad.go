package computer

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"slices"
	"strings"
)

const notepadPackage = "Microsoft.WindowsNotepad_8wekyb3d8bbwe"
const emptyTextSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// NotepadTextTarget reports conditional eligibility from a bounded, freshly
// inspected tree. Native process/package identity is independently rechecked by
// the Windows driver and worker. An empty elementID asks about the whole tree.
func NotepadTextTarget(o Observation, elementID string) bool {
	e, ok := notepadEditor(o)
	return ok && (elementID == "" || e.ID == elementID) && e.Blank && e.ValueSHA256 == emptyTextSHA256
}

func notepadEditor(o Observation) (Element, bool) {
	w := o.Window
	if o.Modal || o.Truncated || w.App != Notepad || w.PackageFamily != notepadPackage ||
		w.AppUserModelID != notepadPackage+"!App" || w.ClassName != "Notepad" || w.Integrity != 0x2000 ||
		w.ID == "" || w.HWND == "" || w.PID == 0 || w.CreationTime == "" || w.ImageHash != "" ||
		!strings.HasSuffix(strings.ToLower(w.ImagePath), `\notepad\notepad.exe`) {
		return Element{}, false
	}
	var e Element
	count := 0
	for _, candidate := range o.Elements {
		// Hidden and disabled editors also make the document ambiguous.
		if candidate.ControlType == "Edit" || candidate.ControlType == "Document" {
			e = candidate
			count++
		}
	}
	b, outer := e.Bounds, w.Bounds
	// Document is required as a positive semantic identity. A lone Edit can be
	// Notepad's search or replace input when the document provider is missing.
	return e, count == 1 && e.ControlType == "Document" && e.ID != "" && e.RuntimeID != "" && e.ParentPath != "" && e.ProcessID == w.PID &&
		e.Enabled && !e.Offscreen && !e.Password && e.ValueWritable && slices.Contains(e.Patterns, "Value") &&
		b.Width > 0 && b.Height > 0 && b.X >= outer.X && b.Y >= outer.Y &&
		int64(b.X)+int64(b.Width) <= int64(outer.X)+int64(outer.Width) &&
		int64(b.Y)+int64(b.Height) <= int64(outer.Y)+int64(outer.Height)
}

func notepadOperation(action Proposal, w Window) (string, error) {
	if action.Type != SetValue && action.Type != TypeText || action.Expected != "TEXT_MATCH" || action.Element == nil ||
		!NotepadTextTarget(Observation{Window: w, Elements: []Element{*action.Element}}, action.Element.ID) {
		return "", Error("CONTROL_ACTION_DENIED")
	}
	if !ValidateText(action.Text) {
		return "", Error("CONTROL_INPUT_REJECTED")
	}
	if action.Type == SetValue {
		return "set_value", nil
	}
	return "type_text", nil
}

func verifyNotepad(p Proposal, observed Observation) bool {
	if p.Window == nil || p.Element == nil {
		return false
	}
	if _, err := notepadOperation(p, *p.Window); err != nil {
		return false
	}
	e, ok := notepadEditor(observed)
	if !ok {
		return false
	}
	wantWindow, currentWindow := *p.Window, observed.Window
	// Notepad may derive the window title from the inserted first line. The
	// process, package, HWND, class and bounds must remain the same.
	wantWindow.Title, currentWindow.Title = "", ""
	wantWindow.ObservedAt, currentWindow.ObservedAt = "", ""
	wantWindow.Foreground, currentWindow.Foreground = false, false
	if wantWindow != currentWindow {
		return false
	}
	digest := sha256.Sum256([]byte(p.Text))
	if e.Blank || e.ValueSHA256 != hex.EncodeToString(digest[:]) {
		return false
	}
	want := *p.Element
	want.Blank, e.Blank = false, false
	want.ValueSHA256, e.ValueSHA256 = "", ""
	want.Focused, e.Focused = false, false
	return reflect.DeepEqual(want, e)
}
