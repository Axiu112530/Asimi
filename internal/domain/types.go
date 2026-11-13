// Package domain defines provider-neutral, UI-safe Phase 1 contracts.
package domain

import (
	"asimi/internal/ai"
	"asimi/internal/screen"
)

type Error struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params,omitempty"`
}
type Reply struct {
	Data  any    `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
}
type Settings struct {
	AppLanguage   string `json:"appLanguage"`
	AgentLanguage string `json:"agentLanguage"`
	Theme         string `json:"theme"`
	Startup       string `json:"startup"`
	InterfaceMode string `json:"interfaceMode"`
}
type Message struct {
	ID             string               `json:"id"`
	ConversationID string               `json:"conversationId"`
	Role           string               `json:"role"`
	Content        string               `json:"content"`
	CreatedAt      string               `json:"createdAt"`
	TaskID         string               `json:"taskId,omitempty"`
	ProviderID     string               `json:"providerId,omitempty"`
	ModelID        string               `json:"modelId,omitempty"`
	RequestID      string               `json:"requestId,omitempty"`
	Status         string               `json:"status,omitempty"`
	ErrorCode      string               `json:"errorCode,omitempty"`
	Attachments    []ai.Attachment      `json:"attachments,omitempty"`
	Usage          *ai.Usage            `json:"usage,omitempty"`
	ScreenCapture  *screen.Metadata     `json:"screenCapture,omitempty"`
	Vision         *screen.VisionResult `json:"vision,omitempty"`
}
type Conversation struct {
	Playground bool      `json:"playground"`
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	CreatedAt  string    `json:"createdAt"`
	UpdatedAt  string    `json:"updatedAt"`
	Messages   []Message `json:"messages"`
	Tasks      []Task    `json:"tasks"`
}
type Step struct {
	ID             string `json:"id"`
	NameKey        string `json:"nameKey"`
	DescriptionKey string `json:"descriptionKey"`
	Status         string `json:"status"`
	ToolName       string `json:"toolName,omitempty"`
	Result         string `json:"result,omitempty"`
	ErrorCode      string `json:"errorCode,omitempty"`
}
type Operation struct {
	TaskID         string `json:"taskId"`
	ToolName       string `json:"toolName"`
	FileName       string `json:"fileName"`
	NormalizedPath string `json:"normalizedPath"`
	Content        string `json:"content"`
	ContentHash    string `json:"contentHash"`
	Bytes          int    `json:"bytes"`
	Operation      string `json:"operation"`
	ExpiresAt      string `json:"expiresAt"`
	ApprovalID     string `json:"approvalId"`
}
type Approval struct {
	ID        string    `json:"id"`
	Operation Operation `json:"operation"`
	Status    string    `json:"status"`
	CreatedAt string    `json:"createdAt"`
	ExpiresAt string    `json:"expiresAt"`
	DecidedAt string    `json:"decidedAt,omitempty"`
}
type Verification struct {
	Path           string `json:"path"`
	ExpectedBytes  int    `json:"expectedBytes"`
	ActualBytes    int    `json:"actualBytes"`
	ExpectedHash   string `json:"expectedHash"`
	ActualHash     string `json:"actualHash"`
	ContentMatched bool   `json:"contentMatched"`
	Passed         bool   `json:"passed"`
	Created        bool   `json:"created"`
	Code           string `json:"code,omitempty"`
}
type Task struct {
	ID             string        `json:"id"`
	ConversationID string        `json:"conversationId"`
	Title          string        `json:"title"`
	Description    string        `json:"description"`
	Status         string        `json:"status"`
	CurrentStep    string        `json:"currentStep"`
	Steps          []Step        `json:"steps"`
	Approval       *Approval     `json:"approval,omitempty"`
	Verification   *Verification `json:"verification,omitempty"`
	Summary        string        `json:"summary,omitempty"`
	ErrorCode      string        `json:"errorCode,omitempty"`
	CreatedAt      string        `json:"createdAt"`
	UpdatedAt      string        `json:"updatedAt"`
}
type AuditEvent struct {
	ID        string            `json:"id"`
	TaskID    string            `json:"taskId,omitempty"`
	Code      string            `json:"code"`
	Params    map[string]string `json:"params,omitempty"`
	CreatedAt string            `json:"createdAt"`
}
type ToolExecution struct {
	ID        string        `json:"id"`
	TaskID    string        `json:"taskId"`
	ToolName  string        `json:"toolName"`
	Path      string        `json:"path"`
	Status    string        `json:"status"`
	Result    *Verification `json:"result,omitempty"`
	ErrorCode string        `json:"errorCode,omitempty"`
	CreatedAt string        `json:"createdAt"`
	UpdatedAt string        `json:"updatedAt"`
}
type Model struct {
	ProviderID       string         `json:"providerId"`
	ID               string         `json:"id"`
	Text             string         `json:"text"`
	Vision           string         `json:"vision"`
	ToolCalling      string         `json:"toolCalling"`
	Streaming        string         `json:"streaming"`
	ComputerUse      string         `json:"computerUse"`
	StructuredOutput string         `json:"structuredOutput"`
	ContextLength    int            `json:"contextLength"`
	PricingMetadata  map[string]any `json:"pricingMetadata"`
}
type ProviderProfile struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	BaseURL string  `json:"baseUrl"`
	Enabled bool    `json:"enabled"`
	Status  string  `json:"status"`
	Models  []Model `json:"models"`
}

// CredentialVault is the legacy Phase 1 contract; Phase 2B uses internal/credential.
type CredentialVault interface {
	Store(providerID string, secret []byte) error
	Load(providerID string) ([]byte, error)
	Delete(providerID string) error
}
