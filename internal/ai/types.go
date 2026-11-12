// Package ai defines text/vision-only provider contracts. It has no tool authority.
package ai

import "context"

const (
	Supported       = "SUPPORTED"
	Unsupported     = "UNSUPPORTED"
	Unknown         = "UNKNOWN"
	Responses       = "responses"
	ChatCompletions = "chat-completions"
)

type Capabilities struct {
	Text             string   `json:"text"`
	Vision           string   `json:"vision"`
	Streaming        string   `json:"streaming"`
	ToolCalling      string   `json:"toolCalling"`
	StructuredOutput string   `json:"structuredOutput"`
	ComputerUse      string   `json:"computerUse"`
	Temperature      string   `json:"temperature"`
	ContextLimit     *int64   `json:"contextLimit"`
	ImageDetails     []string `json:"imageDetails"`
}
type Pricing struct {
	InputPerMillion       float64  `json:"inputPerMillion"`
	OutputPerMillion      float64  `json:"outputPerMillion"`
	CachedInputPerMillion *float64 `json:"cachedInputPerMillion"`
	Currency              string   `json:"currency"`
	EffectiveAt           string   `json:"effectiveAt"`
}
type Model struct {
	ID           string       `json:"id"`
	DisplayName  string       `json:"displayName"`
	Capabilities Capabilities `json:"capabilities"`
	Pricing      *Pricing     `json:"pricing"`
}
type Profile struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Protocol       string  `json:"protocol"`
	BaseURL        string  `json:"baseUrl"`
	CredentialID   string  `json:"credentialId,omitempty"`
	HasCredential  bool    `json:"hasCredential"`
	Enabled        bool    `json:"enabled"`
	TrustedHTTP    bool    `json:"trustedHttp"`
	TimeoutSeconds int     `json:"timeoutSeconds,omitempty"`
	Models         []Model `json:"models"`
}
type Content struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	DataURL string `json:"-"`
	Detail  string `json:"detail,omitempty"`
}
type Message struct {
	Role      string    `json:"role"`
	Content   []Content `json:"content"`
	Timestamp string    `json:"timestamp,omitempty"`
}
type Request struct {
	RequestID         string    `json:"requestId"`
	ConversationID    string    `json:"conversationId"`
	ProviderID        string    `json:"providerId"`
	ModelID           string    `json:"modelId"`
	SystemInstruction string    `json:"systemInstruction"`
	Messages          []Message `json:"messages"`
	MaxOutputTokens   int       `json:"maxOutputTokens"`
	Stream            bool      `json:"stream"`
	ResponseLanguage  string    `json:"responseLanguage"`
	Temperature       *float64  `json:"temperature,omitempty"`
	JSONOutput        bool      `json:"jsonOutput,omitempty"`
}
type Usage struct {
	InputTokens       *int64 `json:"inputTokens"`
	OutputTokens      *int64 `json:"outputTokens"`
	TotalTokens       *int64 `json:"totalTokens"`
	CachedInputTokens *int64 `json:"cachedInputTokens"`
	ReasoningTokens   *int64 `json:"reasoningTokens"`
	UsageAvailable    bool   `json:"usageAvailable"`
	Source            string `json:"source"`
}
type Response struct {
	RequestID           string `json:"requestId"`
	ProviderID          string `json:"providerId"`
	ModelID             string `json:"modelId"`
	Content             string `json:"content"`
	Usage               Usage  `json:"usage"`
	FinishReason        string `json:"finishReason"`
	ProviderRequestID   string `json:"providerRequestId"`
	DurationMS          int64  `json:"durationMs"`
	FirstTokenLatencyMS *int64 `json:"firstTokenLatencyMs"`
	HTTPStatus          int    `json:"httpStatus"`
	Status              string `json:"status"`
	ErrorCode           string `json:"errorCode,omitempty"`
}
type Delta struct {
	Content string `json:"content"`
}
type Adapter interface {
	Generate(context.Context, Request, []byte) (Response, error)
	Stream(context.Context, Request, []byte, func(Delta) error) (Response, error)
	TestConnection(context.Context, Request, []byte) (Response, error)
	GetCapabilities(string) (Capabilities, error)
}
type Error string

func (e Error) Error() string { return string(e) }
func (e Error) Code() string  { return string(e) }

type Attachment struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	MIME           string `json:"mime"`
	Bytes          int64  `json:"bytes"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	SHA256         string `json:"sha256"`
	PreviewDataURL string `json:"previewDataUrl,omitempty"`
}
type Selection struct {
	DefaultProviderID string   `json:"defaultProviderId"`
	DefaultModelID    string   `json:"defaultModelId"`
	VisionProviderID  string   `json:"visionProviderId"`
	VisionModelID     string   `json:"visionModelId"`
	MaxOutputTokens   int      `json:"maxOutputTokens"`
	MaxContextBytes   int      `json:"maxContextBytes"`
	WarningThreshold  *float64 `json:"warningThreshold"`
}
type UsageRecord struct {
	RequestID           string   `json:"requestId"`
	ConversationID      string   `json:"conversationId"`
	MessageID           string   `json:"messageId"`
	ProviderID          string   `json:"providerId"`
	ModelID             string   `json:"modelId"`
	Usage               Usage    `json:"usage"`
	Pricing             *Pricing `json:"pricing"`
	EstimatedCost       *float64 `json:"estimatedCost"`
	DurationMS          int64    `json:"durationMs"`
	FirstTokenLatencyMS *int64   `json:"firstTokenLatencyMs"`
	HTTPStatus          int      `json:"httpStatus"`
	Status              string   `json:"status"`
	ErrorCode           string   `json:"errorCode,omitempty"`
	CreatedAt           string   `json:"createdAt"`
	ProviderRequestID   string   `json:"providerRequestId"`
}
