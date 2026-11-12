// Package computer defines the narrow data and driver contract used by the action broker.
package computer

import (
	"context"
	"deskpilot/internal/screen"
)

type ActionType string

const (
	LaunchHost ActionType = "app.launch_test_host"
	Focus      ActionType = "window.focus"
	Invoke     ActionType = "uia.invoke"
	SetValue   ActionType = "uia.set_value"
	TypeText   ActionType = "keyboard.type_text"
)

const (
	TestHost      = "test_host"
	Notepad       = "notepad"
	PolicyVersion = "2d-v1"
)

type Error string

func (e Error) Error() string { return string(e) }
func (e Error) Code() string  { return string(e) }

type Window struct {
	ID             string      `json:"id"`
	App            string      `json:"app"`
	HWND           string      `json:"hwnd"`
	PID            uint32      `json:"pid"`
	CreationTime   string      `json:"creationTime"`
	ImagePath      string      `json:"imagePath"`
	ImageHash      string      `json:"imageHash,omitempty"`
	PackageFamily  string      `json:"packageFamily,omitempty"`
	AppUserModelID string      `json:"appUserModelId,omitempty"`
	SessionID      uint32      `json:"sessionId"`
	Integrity      uint32      `json:"integrity"`
	ClassName      string      `json:"className"`
	Title          string      `json:"title"`
	Bounds         screen.Rect `json:"bounds"`
	Foreground     bool        `json:"foreground"`
	ObservedAt     string      `json:"observedAt"`
}

type Element struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	AutomationID  string      `json:"automationId"`
	ControlType   string      `json:"controlType"`
	Bounds        screen.Rect `json:"bounds"`
	Enabled       bool        `json:"enabled"`
	Offscreen     bool        `json:"offscreen"`
	Focused       bool        `json:"focused"`
	Password      bool        `json:"password"`
	ProcessID     uint32      `json:"processId"`
	NativeHandle  string      `json:"nativeHandle"`
	Patterns      []string    `json:"patterns"`
	ParentPath    string      `json:"parentPath"`
	RuntimeID     string      `json:"runtimeId"`
	Depth         int         `json:"depth"`
	Blank         bool        `json:"blank"`
	ValueSHA256   string      `json:"valueSha256,omitempty"`
	ValueWritable bool        `json:"valueWritable,omitempty"`
}

type Observation struct {
	ID         string    `json:"id"`
	Window     Window    `json:"window"`
	Elements   []Element `json:"elements"`
	Truncated  bool      `json:"truncated"`
	Modal      bool      `json:"modal"`
	ObservedAt string    `json:"observedAt"`
}

type Proposal struct {
	ObservationID   string     `json:"observationId,omitempty"`
	SourceRequestID string     `json:"sourceRequestId,omitempty"`
	TaskID          string     `json:"taskId,omitempty"`
	ID              string     `json:"id"`
	Type            ActionType `json:"type"`
	Window          *Window    `json:"window,omitempty"`
	Element         *Element   `json:"element,omitempty"`
	Text            string     `json:"text,omitempty"`
	Expected        string     `json:"expected"`
	ObservedAt      string     `json:"observedAt"`
	ExpiresAt       string     `json:"expiresAt"`
	Hash            string     `json:"hash"`
	TargetHash      string     `json:"targetHash"`
	PolicyVersion   string     `json:"policyVersion"`
	Status          string     `json:"status"`
}

type Outcome struct {
	// Effect is NOT_STARTED, ISSUED, or UNKNOWN.
	Effect string
	Window *Window
}

type Verification struct {
	Verified bool   `json:"verified"`
	Code     string `json:"code"`
}

type Record struct {
	ID       string     `json:"id"`
	Type     ActionType `json:"type"`
	Status   string     `json:"status"`
	Code     string     `json:"code,omitempty"`
	At       string     `json:"at"`
	Verified bool       `json:"verified"`
}

type Driver interface {
	Enumerate(context.Context) ([]Window, error)
	Inspect(context.Context, Window) (Observation, error)
	Execute(context.Context, Proposal) (Outcome, error)
	Verify(context.Context, Proposal, Outcome) (Verification, error)
	Close() error
}
