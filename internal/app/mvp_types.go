package app

import (
	"context"
	"encoding/json"
	"sync"

	"asimi/internal/command"
	"asimi/internal/developer"
)

type Workspace struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Root        string `json:"root"`
	CreatedAt   string `json:"createdAt"`
	LastUsedAt  string `json:"lastUsedAt"`
	TrustStatus string `json:"trustStatus"`
}
type AgentStep struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Title    string          `json:"title"`
	Status   string          `json:"status"`
	Result   json.RawMessage `json:"result,omitempty"`
	ActionID string          `json:"actionId,omitempty"`
	Error    string          `json:"error,omitempty"`
}
type AgentTask struct {
	ID             string      `json:"id"`
	WorkspaceID    string      `json:"workspaceId"`
	ConversationID string      `json:"conversationId,omitempty"`
	Goal           string      `json:"goal"`
	Status         string      `json:"status"`
	CurrentStep    int         `json:"currentStep"`
	Steps          []AgentStep `json:"steps"`
	Summary        string      `json:"summary"`
	CreatedAt      string      `json:"createdAt"`
	UpdatedAt      string      `json:"updatedAt"`
	Agent          *agent02Run `json:"agent,omitempty"`
}
type ProductProposal struct {
	WorkspaceID   string            `json:"workspaceId"`
	Root          string            `json:"root"`
	File          *developer.Change `json:"file,omitempty"`
	Command       *command.Proposal `json:"command,omitempty"`
	PolicyVersion string            `json:"policyVersion,omitempty"`
	PlanVersion   int               `json:"planVersion,omitempty"`
	StepID        string            `json:"stepId,omitempty"`
	GitStatus     string            `json:"gitStatus,omitempty"`
	GitDirtyFile  bool              `json:"gitDirtyFile,omitempty"`
}
type ProductAction struct {
	ID        string          `json:"id"`
	TaskID    string          `json:"taskId,omitempty"`
	Kind      string          `json:"kind"`
	Status    string          `json:"status"`
	Hash      string          `json:"hash"`
	ExpiresAt string          `json:"expiresAt"`
	CreatedAt string          `json:"createdAt"`
	UpdatedAt string          `json:"updatedAt"`
	Proposal  ProductProposal `json:"proposal"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}
type Skill struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Version         string         `json:"version"`
	Enabled         bool           `json:"enabled"`
	Tools           []string       `json:"tools"`
	Risk            string         `json:"risk"`
	Schema          map[string]any `json:"schema"`
	InputSchema     map[string]any `json:"inputSchema"`
	OutputSchema    map[string]any `json:"outputSchema"`
	GrantsAuthority bool           `json:"grantsAuthority"`
}
type ToolDescriptor struct {
	ID       string         `json:"id"`
	Effect   string         `json:"effect"`
	Approval string         `json:"approval"`
	Risk     string         `json:"risk"`
	Schema   map[string]any `json:"schema"`
}
type MVPState struct {
	Workspaces []Workspace     `json:"workspaces"`
	AgentTasks []AgentTask     `json:"agentTasks"`
	Actions    []ProductAction `json:"actions"`
	Skills     []Skill         `json:"skills"`
	Onboarding struct {
		Completed bool `json:"completed"`
	} `json:"onboarding"`
	Tools          []ToolDescriptor `json:"tools"`
	CommandCatalog []command.Spec   `json:"commandCatalog"`
}
type mvpRuntime struct {
	mu            sync.Mutex
	signalMu      sync.Mutex // Cancellation never waits for state/SQLite work.
	picker        func(context.Context) (string, error)
	controls      map[string]context.CancelFunc
	immutable     map[string]string
	faults        map[string]bool
	faultErr      error
	stopped       uint64
	stopping      int
	stoppingTasks map[string]int
}

func (s *Service) mvpGeneration() uint64 {
	s.mvp.signalMu.Lock()
	defer s.mvp.signalMu.Unlock()
	return s.mvp.stopped
}
func (s *Service) registerMVPControl(key string, cancel context.CancelFunc, epoch uint64, taskID ...string) {
	s.mvp.signalMu.Lock()
	defer s.mvp.signalMu.Unlock()
	s.mvp.controls[key] = cancel
	if epoch != s.mvp.stopped || s.mvp.stopping > 0 || len(taskID) > 0 && s.mvp.stoppingTasks[taskID[0]] > 0 || s.lifetime.Err() != nil {
		cancel()
	}
}

func (s *Service) mvpAdmission(epoch uint64, taskID string) bool {
	s.mvp.signalMu.Lock()
	defer s.mvp.signalMu.Unlock()
	return epoch == s.mvp.stopped && s.mvp.stopping == 0 && s.mvp.stoppingTasks[taskID] == 0 && s.lifetime.Err() == nil
}
func (s *Service) removeMVPControl(key string) {
	s.mvp.signalMu.Lock()
	defer s.mvp.signalMu.Unlock()
	if cancel := s.mvp.controls[key]; cancel != nil {
		cancel()
		delete(s.mvp.controls, key)
	}
}
func (s *Service) cancelMVPControl(key string) {
	s.mvp.signalMu.Lock()
	defer s.mvp.signalMu.Unlock()
	if cancel := s.mvp.controls[key]; cancel != nil {
		cancel()
	}
}
func (s *Service) activeMVPControl(key string) bool {
	s.mvp.signalMu.Lock()
	defer s.mvp.signalMu.Unlock()
	_, ok := s.mvp.controls[key]
	return ok
}

// Only the native application composition root may install this callback. It is
// not a Wails binding: Call is the service's only exposed business bridge.
func (s *Service) SetWorkspacePicker(picker func(context.Context) (string, error)) {
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	s.mvp.picker = picker
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func stringSchema() map[string]any { return map[string]any{"type": "string"} }
func commonToolCatalog() []ToolDescriptor {
	scope := objectSchema(map[string]any{"workspaceId": stringSchema()}, "workspaceId")
	return []ToolDescriptor{
		{"workspace.analyze", "read", "none", "bounded files", scope},
		{"workspace.read", "read", "none", "bounded text", objectSchema(map[string]any{"workspaceId": stringSchema(), "path": stringSchema()}, "workspaceId", "path")},
		{"environment.diagnose", "process-read", "user initiated", "fixed version commands", objectSchema(map[string]any{})},
		{"file.change", "write", "exact single action", "one file", objectSchema(map[string]any{"workspaceId": stringSchema(), "path": stringSchema(), "content": stringSchema()}, "workspaceId", "path", "content")},
		{"command.run", "process", "exact single action", "project code and possible network", objectSchema(map[string]any{"workspaceId": stringSchema(), "commandId": stringSchema(), "args": map[string]any{"type": "array", "items": stringSchema()}, "network": map[string]any{"type": "boolean"}}, "workspaceId", "commandId", "network")},
		{"computer.control", "desktop", "existing semantic target approval chain", "desktop state", objectSchema(map[string]any{"action": stringSchema()})},
		{"screen.capture", "capture", "existing explicit capture and review chain", "screen privacy", objectSchema(map[string]any{})},
		{"provider.text", "network", "explicit confirmSend through existing provider adapter", "external text disclosure", objectSchema(map[string]any{"confirmSend": map[string]any{"type": "boolean"}}, "confirmSend")},
		{"provider.vision", "network", "existing reviewed image and confirmSend chain", "external image disclosure", objectSchema(map[string]any{"confirmSend": map[string]any{"type": "boolean"}}, "confirmSend")},
		{"legacy.file.create", "write", "existing immutable approval chain", "scoped file creation", objectSchema(map[string]any{"fileName": stringSchema(), "content": stringSchema()}, "fileName", "content")},
	}
}

func builtinSkills() []Skill {
	rows := []Skill{
		{ID: "project-overview", Name: "Project overview", Description: "Summarize project structure and detected manifests.", Tools: []string{"workspace.analyze"}, Risk: "read"},
		{ID: "code-explanation", Name: "Code explanation", Description: "Search bounded project text and explain with a chosen provider after consent.", Tools: []string{"workspace.analyze", "workspace.read", "provider.text"}, Risk: "read; optional external text"},
		{ID: "environment-diagnosis", Name: "Environment diagnosis", Description: "Inspect fixed runtime versions and suggest official installation sources.", Tools: []string{"environment.diagnose"}, Risk: "fixed read-only processes"},
		{ID: "test-project", Name: "Project tests", Description: "Prepare a fixed test command for individual approval.", Tools: []string{"command.run"}, Risk: "project code; network"},
		{ID: "file-edit", Name: "File edit", Description: "Preview and verify one approved file change.", Tools: []string{"workspace.read", "file.change"}, Risk: "write"},
		{ID: "error-diagnosis", Name: "Error diagnosis", Description: "Search bounded project context and explain an error with optional approved provider analysis.", Tools: []string{"workspace.analyze", "workspace.read", "provider.text"}, Risk: "read; optional external text"},
		{ID: "screen-analysis", Name: "Screen analysis", Description: "Use the existing screen capture, review and vision controls.", Tools: []string{"screen.capture", "provider.vision"}, Risk: "capture; optional external image"},
		{ID: "desktop-assistance", Name: "Notepad Assistant", Description: "Use only the existing semantic desktop approval chain for supported Notepad.", Tools: []string{"computer.control"}, Risk: "desktop"},
	}
	for i := range rows {
		rows[i].Enabled = true
		rows[i].Version = "1.0.0"
		rows[i].Schema = objectSchema(map[string]any{"goal": stringSchema(), "workspaceId": stringSchema()}, "goal")
		rows[i].InputSchema = rows[i].Schema
		rows[i].OutputSchema = objectSchema(map[string]any{"summary": stringSchema(), "evidence": map[string]any{"type": "array", "items": stringSchema()}}, "summary")
	}
	return rows
}
