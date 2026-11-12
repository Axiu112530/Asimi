package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
)

type wireUsage struct {
	Input        *int64 `json:"input_tokens"`
	Output       *int64 `json:"output_tokens"`
	Total        *int64 `json:"total_tokens"`
	Prompt       *int64 `json:"prompt_tokens"`
	Completion   *int64 `json:"completion_tokens"`
	InputDetails struct {
		Cached *int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	PromptDetails struct {
		Cached *int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	OutputDetails struct {
		Reasoning *int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
	CompletionDetails struct {
		Reasoning *int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func applyUsage(w *wireUsage, out *Response) error {
	if w == nil {
		return nil
	}
	in, output, cached, reason := w.Input, w.Output, w.InputDetails.Cached, w.OutputDetails.Reasoning
	if in == nil {
		in = w.Prompt
	}
	if output == nil {
		output = w.Completion
	}
	if cached == nil {
		cached = w.PromptDetails.Cached
	}
	if reason == nil {
		reason = w.CompletionDetails.Reasoning
	}
	for _, n := range []*int64{in, output, w.Total, cached, reason} {
		if n != nil && *n < 0 {
			return Error("INVALID_RESPONSE")
		}
	}
	available := in != nil || output != nil || w.Total != nil || cached != nil || reason != nil
	if available {
		out.Usage = Usage{InputTokens: in, OutputTokens: output, TotalTokens: w.Total, CachedInputTokens: cached, ReasoningTokens: reason, UsageAvailable: true, Source: "PROVIDER_REPORTED"}
	}
	return nil
}

type responsePart struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}
type responseItem struct {
	Type    string         `json:"type"`
	Content []responsePart `json:"content"`
}
type responseWire struct {
	ID         string          `json:"id"`
	Status     string          `json:"status"`
	Output     []responseItem  `json:"output"`
	Usage      *wireUsage      `json:"usage"`
	Error      json.RawMessage `json:"error"`
	Incomplete struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
}

func toolType(v string) bool {
	v = strings.ToLower(v)
	return strings.Contains(v, "call") || strings.Contains(v, "tool") || strings.Contains(v, "computer") || strings.Contains(v, "shell") || strings.Contains(v, "function") || strings.Contains(v, "mcp") || strings.Contains(v, "browser")
}
func responseText(w responseWire) (string, error) {
	var b strings.Builder
	refusal := false
	for _, item := range w.Output {
		if toolType(item.Type) {
			return b.String(), Error("UNSUPPORTED_TOOL_CALL")
		}
		if item.Type != "message" && item.Type != "reasoning" {
			return b.String(), Error("INVALID_RESPONSE")
		}
		for _, p := range item.Content {
			switch p.Type {
			case "output_text":
				b.WriteString(p.Text)
			case "refusal":
				refusal = true
				b.WriteString(p.Refusal)
			default:
				if toolType(p.Type) {
					return b.String(), Error("UNSUPPORTED_TOOL_CALL")
				}
				return b.String(), Error("INVALID_RESPONSE")
			}
		}
	}
	if refusal {
		return b.String(), Error("PROVIDER_REFUSAL")
	}
	return b.String(), nil
}
func parseResponse(b []byte, out *Response) error {
	var w responseWire
	if json.Unmarshal(b, &w) != nil {
		return Error("INVALID_RESPONSE")
	}
	if out.ProviderRequestID == "" && safeID(w.ID, nil) {
		out.ProviderRequestID = w.ID
	}
	if e := applyUsage(w.Usage, out); e != nil {
		return e
	}
	text, e := responseText(w)
	out.Content = text
	if e != nil {
		return e
	}
	if hasJSON(w.Error) {
		return providerError(w.Error, Error("PROVIDER_UNAVAILABLE"))
	}
	switch w.Status {
	case "completed":
		out.FinishReason = "stop"
	case "incomplete":
		out.FinishReason = "incomplete"
		return Error("RESPONSE_INCOMPLETE")
	case "cancelled":
		return Error("REQUEST_CANCELLED")
	case "failed":
		return Error("PROVIDER_UNAVAILABLE")
	default:
		return Error("INVALID_RESPONSE")
	}
	return nil
}
func hasJSON(v json.RawMessage) bool { return len(v) > 0 && string(v) != "null" }

func providerError(b []byte, fallback Error) Error {
	var v struct {
		Code  string          `json:"code"`
		Type  string          `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(b, &v) != nil {
		return fallback
	}
	if hasJSON(v.Error) {
		var nested struct {
			Code string `json:"code"`
			Type string `json:"type"`
		}
		if json.Unmarshal(v.Error, &nested) == nil {
			v.Code = nested.Code
			v.Type = nested.Type
		}
	}
	for _, code := range []string{v.Code, v.Type} {
		switch code {
		case "invalid_api_key", "authentication_error", "unauthorized":
			return Error("INVALID_API_KEY")
		case "model_not_found":
			return Error("MODEL_NOT_FOUND")
		case "rate_limit_exceeded", "rate_limit_error", "insufficient_quota":
			return Error("RATE_LIMITED")
		case "context_length_exceeded":
			return Error("CONTEXT_TOO_LARGE")
		case "unsupported_capability":
			return Error("UNSUPPORTED_CAPABILITY")
		}
	}
	return fallback
}

type chatMessage struct {
	Content      json.RawMessage `json:"content"`
	Refusal      *string         `json:"refusal"`
	ToolCalls    json.RawMessage `json:"tool_calls"`
	FunctionCall json.RawMessage `json:"function_call"`
}
type chatChoice struct {
	Index   int         `json:"index"`
	Message chatMessage `json:"message"`
	Delta   chatMessage `json:"delta"`
	Finish  *string     `json:"finish_reason"`
}
type chatWire struct {
	ID      string          `json:"id"`
	Choices []chatChoice    `json:"choices"`
	Usage   *wireUsage      `json:"usage"`
	Error   json.RawMessage `json:"error"`
}

func chatText(m chatMessage) (string, error) {
	if hasJSON(m.ToolCalls) && string(m.ToolCalls) != "[]" || hasJSON(m.FunctionCall) {
		return "", Error("UNSUPPORTED_TOOL_CALL")
	}
	if m.Refusal != nil && *m.Refusal != "" {
		return *m.Refusal, Error("PROVIDER_REFUSAL")
	}
	if !hasJSON(m.Content) {
		return "", nil
	}
	var text string
	if json.Unmarshal(m.Content, &text) == nil {
		return text, nil
	}
	var parts []responsePart
	if json.Unmarshal(m.Content, &parts) != nil {
		return "", Error("INVALID_RESPONSE")
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type != "text" {
			if toolType(p.Type) {
				return b.String(), Error("UNSUPPORTED_TOOL_CALL")
			}
			return b.String(), Error("INVALID_RESPONSE")
		}
		b.WriteString(p.Text)
	}
	return b.String(), nil
}
func chatFinish(v string, out *Response) error {
	out.FinishReason = v
	switch v {
	case "stop":
		return nil
	case "tool_calls", "function_call":
		return Error("UNSUPPORTED_TOOL_CALL")
	case "length":
		return Error("RESPONSE_INCOMPLETE")
	case "content_filter":
		return Error("PROVIDER_REFUSAL")
	default:
		return Error("INVALID_RESPONSE")
	}
}
func parseChat(b []byte, out *Response) error {
	var w chatWire
	if json.Unmarshal(b, &w) != nil {
		return Error("INVALID_RESPONSE")
	}
	if out.ProviderRequestID == "" && safeID(w.ID, nil) {
		out.ProviderRequestID = w.ID
	}
	if e := applyUsage(w.Usage, out); e != nil {
		return e
	}
	if hasJSON(w.Error) {
		return providerError(w.Error, Error("PROVIDER_UNAVAILABLE"))
	}
	if len(w.Choices) != 1 || w.Choices[0].Index != 0 {
		return Error("INVALID_RESPONSE")
	}
	t, e := chatText(w.Choices[0].Message)
	out.Content = t
	if e != nil {
		return e
	}
	if w.Choices[0].Finish == nil {
		return Error("INVALID_RESPONSE")
	}
	return chatFinish(*w.Choices[0].Finish, out)
}

type sseEvent struct{ event, data string }

// readSSE frames independently of transport chunks, preserving multiline data.
// EOF is never a success: only each protocol's explicit terminal event completes.
func readSSE(ctx context.Context, r io.Reader, handle func(sseEvent) (bool, error)) error {
	scan := bufio.NewScanner(io.LimitReader(r, maxStreamBytes+1))
	scan.Buffer(make([]byte, 4096), maxEventBytes+1)
	var fields []string
	event := ""
	total, eventBytes := 0, 0
	dispatch := func() (bool, error) {
		if len(fields) == 0 {
			event = ""
			eventBytes = 0
			return false, nil
		}
		v := sseEvent{event: event, data: strings.Join(fields, "\n")}
		fields = nil
		event = ""
		eventBytes = 0
		return handle(v)
	}
	for scan.Scan() {
		if ctx.Err() != nil {
			return contextError(ctx)
		}
		line := scan.Text()
		total += len(line) + 1
		eventBytes += len(line) + 1
		if total > maxStreamBytes || eventBytes > maxEventBytes {
			return Error("INVALID_RESPONSE")
		}
		if line == "" {
			done, err := dispatch()
			if err != nil {
				return err
			}
			if done {
				return nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			fields = append(fields, value)
		}
	}
	if ctx.Err() != nil {
		return contextError(ctx)
	}
	if scan.Err() != nil {
		return Error("STREAM_INTERRUPTED")
	}
	// The final record still requires a blank separator as mandated by SSE.
	return Error("STREAM_INTERRUPTED")
}
func parseChatStream(ctx context.Context, r io.Reader, out *Response, emit func(string) error) error {
	finished := false
	return readSSE(ctx, r, func(ev sseEvent) (bool, error) {
		if ev.event == "error" {
			return false, providerError([]byte(ev.data), Error("PROVIDER_UNAVAILABLE"))
		}
		if ev.data == "[DONE]" {
			if !finished {
				return false, Error("INVALID_RESPONSE")
			}
			return true, nil
		}
		var w chatWire
		if json.Unmarshal([]byte(ev.data), &w) != nil {
			return false, Error("INVALID_RESPONSE")
		}
		if out.ProviderRequestID == "" && safeID(w.ID, nil) {
			out.ProviderRequestID = w.ID
		}
		if e := applyUsage(w.Usage, out); e != nil {
			return false, e
		}
		if hasJSON(w.Error) {
			return false, providerError(w.Error, Error("PROVIDER_UNAVAILABLE"))
		}
		if len(w.Choices) > 1 {
			return false, Error("INVALID_RESPONSE")
		}
		for _, ch := range w.Choices {
			if ch.Index != 0 {
				return false, Error("INVALID_RESPONSE")
			}
			v, e := chatText(ch.Delta)
			if e != nil {
				return false, e
			}
			if finished && (v != "" || ch.Finish != nil) {
				return false, Error("INVALID_RESPONSE")
			}
			if e = emit(v); e != nil {
				return false, e
			}
			if ch.Finish != nil {
				if e = chatFinish(*ch.Finish, out); e != nil {
					return false, e
				}
				finished = true
			}
		}
		return false, nil
	})
}
func parseResponsesStream(ctx context.Context, r io.Reader, out *Response, emit func(string) error) error {
	refusal := false
	return readSSE(ctx, r, func(ev sseEvent) (bool, error) {
		var w struct {
			Type     string       `json:"type"`
			Delta    string       `json:"delta"`
			Text     string       `json:"text"`
			Response responseWire `json:"response"`
			Item     responseItem `json:"item"`
			Part     responsePart `json:"part"`
		}
		if json.Unmarshal([]byte(ev.data), &w) != nil {
			return false, Error("INVALID_RESPONSE")
		}
		kind := w.Type
		if kind == "" {
			kind = ev.event
		}
		if kind == "" || ev.event != "" && ev.event != kind {
			return false, Error("INVALID_RESPONSE")
		}
		if e := applyUsage(w.Response.Usage, out); e != nil {
			return false, e
		}
		if toolType(kind) || toolType(w.Item.Type) || toolType(w.Part.Type) {
			return false, Error("UNSUPPORTED_TOOL_CALL")
		}
		switch kind {
		case "response.output_text.delta":
			return false, emit(w.Delta)
		case "response.refusal.delta":
			refusal = true
			return false, emit(w.Delta)
		case "response.refusal.done":
			refusal = true
		case "error", "response.failed":
			if hasJSON(w.Response.Error) {
				return false, providerError(w.Response.Error, Error("PROVIDER_UNAVAILABLE"))
			}
			return false, providerError([]byte(ev.data), Error("PROVIDER_UNAVAILABLE"))
		case "response.cancelled":
			return false, Error("REQUEST_CANCELLED")
		case "response.incomplete":
			out.FinishReason = "incomplete"
			return false, Error("RESPONSE_INCOMPLETE")
		case "response.completed":
			text, e := responseText(w.Response)
			if e != nil {
				return false, e
			}
			if w.Response.Status != "completed" || hasJSON(w.Response.Error) {
				return false, Error("INVALID_RESPONSE")
			}
			if refusal {
				return false, Error("PROVIDER_REFUSAL")
			}
			if out.Content == "" && text != "" {
				if e = emit(text); e != nil {
					return false, e
				}
			} else if text != "" && text != out.Content {
				return false, Error("INVALID_RESPONSE")
			}
			out.FinishReason = "stop"
			return true, nil
		case "response.created", "response.in_progress", "response.output_item.added", "response.output_item.done", "response.content_part.added", "response.content_part.done", "response.output_text.done", "response.reasoning_summary_part.added", "response.reasoning_summary_part.done", "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done", "response.queued":
		default:
			return false, Error("INVALID_RESPONSE")
		}
		return false, nil
	})
}
