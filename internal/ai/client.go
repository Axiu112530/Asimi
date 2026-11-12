package ai

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	maxBodyBytes    = 8 << 20
	maxRequestBytes = 32 << 20
	maxStreamBytes  = 16 << 20
	maxEventBytes   = 1 << 20
)

// Client is an immutable text/vision adapter. It never performs retries or tools.
type Client struct {
	profile  Profile
	endpoint string
	http     *http.Client
}

var _ Adapter = (*Client)(nil)

// ValidateProfile performs no network activity and rejects unsafe credential URLs.
func ValidateProfile(p Profile) error {
	if p.ID == "" || len(p.ID) > 128 || strings.TrimSpace(p.Name) == "" || len(p.Name) > 256 || (p.Protocol != Responses && p.Protocol != ChatCompletions) {
		return Error("INVALID_PROFILE")
	}
	if p.TimeoutSeconds < 0 || p.TimeoutSeconds > 120 {
		return Error("INVALID_PROFILE")
	}
	if _, err := validateURL(p); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, m := range p.Models {
		if strings.TrimSpace(m.ID) == "" || len(m.ID) > 256 || strings.ContainsAny(m.ID, "\r\n\x00") || seen[m.ID] {
			return Error("INVALID_PROFILE")
		}
		seen[m.ID] = true
		for _, v := range []string{m.Capabilities.Text, m.Capabilities.Vision, m.Capabilities.Streaming, m.Capabilities.ToolCalling, m.Capabilities.StructuredOutput, m.Capabilities.ComputerUse, m.Capabilities.Temperature} {
			if v != "" && v != Supported && v != Unsupported && v != Unknown {
				return Error("INVALID_PROFILE")
			}
		}
		if m.Capabilities.ContextLimit != nil && *m.Capabilities.ContextLimit <= 0 {
			return Error("INVALID_PROFILE")
		}
		for _, d := range m.Capabilities.ImageDetails {
			if d != "auto" && d != "low" && d != "high" && d != "original" {
				return Error("INVALID_PROFILE")
			}
		}
		if q := m.Pricing; q != nil {
			if !finiteNonnegative(q.InputPerMillion) || !finiteNonnegative(q.OutputPerMillion) || q.CachedInputPerMillion != nil && !finiteNonnegative(*q.CachedInputPerMillion) || strings.TrimSpace(q.Currency) == "" {
				return Error("INVALID_PROFILE")
			}
		}
	}
	return nil
}
func finiteNonnegative(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
func validateURL(p Profile) (*url.URL, error) {
	if len(p.BaseURL) > 2048 || strings.TrimSpace(p.BaseURL) != p.BaseURL || strings.ContainsAny(p.BaseURL, "\\\r\n\t") {
		return nil, Error("INVALID_BASE_URL")
	}
	u, e := url.Parse(p.BaseURL)
	if e != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
		return nil, Error("INVALID_BASE_URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, Error("INVALID_BASE_URL")
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return nil, Error("INVALID_BASE_URL")
		}
	}
	if strings.Contains(u.Path, "..") || strings.Contains(u.Hostname(), "%") {
		return nil, Error("INVALID_BASE_URL")
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		// Literal private addresses prevent DNS rebinding from turning a trusted
		// intranet profile into a cleartext public credential destination.
		if strings.EqualFold(u.Hostname(), "localhost") {
			u.Host = net.JoinHostPort("127.0.0.1", u.Port())
			if u.Port() == "" {
				u.Host = "127.0.0.1"
			}
		} else if ip == nil || (!ip.IsLoopback() && !(p.TrustedHTTP && ip.IsPrivate())) {
			return nil, Error("INSECURE_ENDPOINT")
		}
	}
	return u, nil
}
func NewClient(p Profile) (*Client, error) {
	if err := ValidateProfile(p); err != nil {
		return nil, err
	}
	u, _ := validateURL(p)
	path := strings.TrimRight(u.Path, "/")
	if path == "" && !(p.Protocol == Responses && u.Scheme == "https" && u.Host == "api.deepseek.com") {
		path = "/v1"
	}
	suffix := "/responses"
	if p.Protocol == ChatCompletions {
		suffix = "/chat/completions"
	}
	if !strings.HasSuffix(path, suffix) {
		path += suffix
	}
	u.Path = path
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: -1}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second, ExpectContinueTimeout: time.Second, DisableKeepAlives: true, MaxResponseHeaderBytes: 64 << 10}
	// Deep-copy caller-owned slices so later profile edits cannot change admission.
	b, _ := json.Marshal(p)
	var snapshot Profile
	_ = json.Unmarshal(b, &snapshot)
	return &Client{snapshot, u.String(), &http.Client{Transport: transport, Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Close releases transport resources after the caller's in-flight request ends.
// Request cancellation remains owned by the context supplied to Generate/Stream.
func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) GetCapabilities(id string) (Capabilities, error) {
	for _, m := range c.profile.Models {
		if m.ID == id {
			v := m.Capabilities
			for _, p := range []*string{&v.Text, &v.Vision, &v.Streaming, &v.ToolCalling, &v.StructuredOutput, &v.ComputerUse, &v.Temperature} {
				if *p == "" {
					*p = Unknown
				}
			}
			v.ComputerUse = Unsupported
			v.ToolCalling = Unsupported
			v.ImageDetails = append([]string(nil), v.ImageDetails...)
			if v.ContextLimit != nil {
				n := *v.ContextLimit
				v.ContextLimit = &n
			}
			return v, nil
		}
	}
	return Capabilities{}, Error("MODEL_NOT_FOUND")
}
func (c *Client) Generate(ctx context.Context, r Request, key []byte) (Response, error) {
	r.Stream = false
	return c.execute(ctx, r, key, nil)
}
func (c *Client) Stream(ctx context.Context, r Request, key []byte, onDelta func(Delta) error) (Response, error) {
	r.Stream = true
	return c.execute(ctx, r, key, onDelta)
}
func (c *Client) TestConnection(ctx context.Context, r Request, key []byte) (Response, error) {
	r.Stream = false
	return c.execute(ctx, r, key, nil)
}

func (c *Client) requestBody(r Request) ([]byte, error) {
	if !c.profile.Enabled {
		return nil, Error("PROVIDER_UNAVAILABLE")
	}
	if r.ProviderID != "" && r.ProviderID != c.profile.ID {
		return nil, Error("INVALID_REQUEST")
	}
	cap, err := c.GetCapabilities(r.ModelID)
	if err != nil {
		return nil, err
	}
	if cap.Text != Supported || r.Stream && cap.Streaming != Supported || r.JSONOutput && cap.StructuredOutput != Supported {
		return nil, Error("UNSUPPORTED_CAPABILITY")
	}
	if r.MaxOutputTokens < 1 || r.MaxOutputTokens > 131072 || len(r.Messages) == 0 || len(r.Messages) > 1000 {
		return nil, Error("INVALID_REQUEST")
	}
	if r.Temperature != nil && (cap.Temperature != Supported || !finiteNonnegative(*r.Temperature) || *r.Temperature > 2) {
		return nil, Error("UNSUPPORTED_CAPABILITY")
	}
	var messages []any
	instruction := r.SystemInstruction
	if r.ResponseLanguage != "" {
		instruction += "\nRespond in language: " + r.ResponseLanguage
	}
	if len(instruction) > 65536 {
		return nil, Error("INVALID_REQUEST")
	}
	if c.profile.Protocol == ChatCompletions && instruction != "" {
		messages = append(messages, map[string]any{"role": "system", "content": instruction})
	}
	budget := len(instruction)
	for _, m := range r.Messages {
		if m.Role != "user" && m.Role != "assistant" && m.Role != "system" && m.Role != "developer" {
			return nil, Error("INVALID_REQUEST")
		}
		if len(m.Content) == 0 {
			return nil, Error("INVALID_REQUEST")
		}
		var parts []any
		for _, v := range m.Content {
			switch v.Type {
			case "text":
				budget += len(v.Text)
				kind := "text"
				if c.profile.Protocol == Responses {
					kind = "input_text"
					if m.Role == "assistant" {
						kind = "output_text"
					}
				}
				parts = append(parts, map[string]any{"type": kind, "text": v.Text})
			case "image":
				if cap.Vision != Supported || m.Role != "user" {
					return nil, Error("UNSUPPORTED_CAPABILITY")
				}
				d := v.Detail
				if d == "" {
					d = "auto"
				}
				found := false
				for _, allowed := range cap.ImageDetails {
					if d == allowed {
						found = true
					}
				}
				if !found {
					return nil, Error("UNSUPPORTED_CAPABILITY")
				}
				if !validDataURL(v.DataURL) {
					return nil, Error("INVALID_ATTACHMENT")
				}
				budget += len(v.DataURL)
				if c.profile.Protocol == Responses {
					parts = append(parts, map[string]any{"type": "input_image", "image_url": v.DataURL, "detail": d})
				} else {
					parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": v.DataURL, "detail": d}})
				}
			default:
				return nil, Error("INVALID_REQUEST")
			}
			if budget > maxRequestBytes {
				return nil, Error("CONTEXT_TOO_LARGE")
			}
		}
		if c.profile.Protocol == Responses && m.Role == "assistant" {
			// Easy input assistant messages use the documented string form;
			// output_text parts otherwise belong to full output message items.
			var text strings.Builder
			for _, v := range m.Content {
				text.WriteString(v.Text)
			}
			messages = append(messages, map[string]any{"role": m.Role, "content": text.String()})
		} else {
			messages = append(messages, map[string]any{"role": m.Role, "content": parts})
		}
	}
	body := map[string]any{"model": r.ModelID, "stream": r.Stream}
	jsonObject := map[string]string{"type": "json_object"}
	if c.profile.Protocol == Responses {
		body["input"] = messages
		body["store"] = false
		body["max_output_tokens"] = r.MaxOutputTokens
		if r.JSONOutput {
			body["text"] = map[string]any{"format": jsonObject}
		}
		if r.ModelID == "deepseek-flash" && (c.endpoint == "https://api.deepseek.com/responses" ||
			c.endpoint == "https://api.deepseek.com/v1/responses") {
			body["reasoning"] = map[string]string{"effort": "none"}
		}
		if instruction != "" {
			body["instructions"] = instruction
		}
	} else {
		body["messages"] = messages
		body["max_tokens"] = r.MaxOutputTokens
		if r.JSONOutput {
			body["response_format"] = jsonObject
		}
		if r.ModelID == "deepseek-flash" && (c.endpoint == "https://api.deepseek.com/v1/chat/completions" ||
			c.endpoint == "https://api.deepseek.com/chat/completions") {
			// The official endpoint defaults to thinking. Keep the response
			// budget available for visible text, including multi-turn vision.
			body["thinking"] = map[string]string{"type": "disabled"}
		}
		if r.Stream {
			body["stream_options"] = map[string]any{"include_usage": true}
		}
	}
	if r.Temperature != nil {
		body["temperature"] = *r.Temperature
	}
	b, err := json.Marshal(body)
	if err != nil || len(b) > maxRequestBytes {
		return nil, Error("INVALID_REQUEST")
	}
	return b, nil
}
func validDataURL(v string) bool {
	if len(v) > 28<<20 {
		return false
	}
	head, body, ok := strings.Cut(v, ",")
	if !ok {
		return false
	}
	switch head {
	case "data:image/png;base64", "data:image/jpeg;base64", "data:image/webp;base64", "data:image/gif;base64":
	default:
		return false
	}
	if body == "" || strings.ContainsAny(body, "\r\n \t") {
		return false
	}
	_, err := base64.StdEncoding.Strict().DecodeString(body)
	return err == nil
}

func (c *Client) execute(ctx context.Context, r Request, key []byte, onDelta func(Delta) error) (out Response, err error) {
	start := time.Now()
	out = Response{RequestID: r.RequestID, ProviderID: c.profile.ID, ModelID: r.ModelID, Status: "FAILED", Usage: Usage{Source: "UNAVAILABLE"}}
	defer func() {
		out.DurationMS = time.Since(start).Milliseconds()
		if !safeID(out.ProviderRequestID, key) {
			out.ProviderRequestID = ""
		}
		if err != nil {
			code, ok := err.(Error)
			if !ok {
				code = Error("INVALID_RESPONSE")
			}
			err = code
			out.ErrorCode = string(code)
			if code == "REQUEST_CANCELLED" {
				out.Status = "CANCELLED"
			} else if code == "STREAM_INTERRUPTED" {
				out.Status = "INTERRUPTED"
			} else {
				out.Status = "FAILED"
			}
		}
	}()
	if ctx == nil {
		return out, Error("INVALID_REQUEST")
	}
	if c.profile.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.profile.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	if ctx.Err() != nil {
		return out, contextError(ctx)
	}
	body, e := c.requestBody(r)
	if e != nil {
		return out, e
	}
	if len(key) > 8192 || bytes.ContainsAny(key, "\r\n\x00") {
		return out, Error("INVALID_API_KEY")
	}
	// Revalidate before attaching credentials even though client configuration is private.
	if _, e = validateURL(c.profile); e != nil {
		return out, e
	}
	var dispatched atomic.Bool
	trace := &httptrace.ClientTrace{WroteHeaders: func() { dispatched.Store(true) }, WroteRequest: func(httptrace.WroteRequestInfo) { dispatched.Store(true) }}
	req, e := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodPost, c.endpoint, bytes.NewReader(body))
	if e != nil {
		return out, Error("INVALID_REQUEST")
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/json")
	if r.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if len(key) > 0 {
		req.Header.Set("Authorization", "Bearer "+string(key))
	}
	resp, e := c.http.Do(req)
	if e != nil {
		return out, transportError(ctx, e, dispatched.Load())
	}
	defer resp.Body.Close()
	out.HTTPStatus = resp.StatusCode
	// Provider metadata is opaque and bounded, never a free-form error message.
	if id := resp.Header.Get("X-Request-Id"); safeID(id, key) {
		out.ProviderRequestID = id
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity {
			// Only allowlisted machine codes may influence the error. The raw
			// message and request body are never returned or logged.
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			return out, providerError(b, httpError(resp.StatusCode))
		}
		return out, httpError(resp.StatusCode)
	}
	if r.Stream {
		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		if !strings.HasPrefix(ct, "text/event-stream") {
			return out, Error("INVALID_RESPONSE")
		}
		var streamText strings.Builder
		emit := func(v string) error {
			if v == "" {
				return nil
			}
			if len(out.Content)+len(v) > maxBodyBytes {
				return Error("INVALID_RESPONSE")
			}
			if out.FirstTokenLatencyMS == nil {
				ms := time.Since(start).Milliseconds()
				out.FirstTokenLatencyMS = &ms
			}
			streamText.WriteString(v)
			out.Content = streamText.String()
			if onDelta != nil {
				if e := onDelta(Delta{Content: v}); e != nil {
					return Error("REQUEST_CANCELLED")
				}
			}
			return nil
		}
		if c.profile.Protocol == Responses {
			e = parseResponsesStream(ctx, resp.Body, &out, emit)
		} else {
			e = parseChatStream(ctx, resp.Body, &out, emit)
		}
		if e != nil {
			return out, e
		}
	} else {
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
		if readErr != nil {
			if ctx.Err() != nil {
				return out, contextError(ctx)
			}
			return out, Error("REQUEST_OUTCOME_UNKNOWN")
		}
		if len(b) > maxBodyBytes {
			return out, Error("INVALID_RESPONSE")
		}
		if c.profile.Protocol == Responses {
			e = parseResponse(b, &out)
		} else {
			e = parseChat(b, &out)
		}
		if e != nil {
			return out, e
		}
	}
	out.Status = "COMPLETED"
	return out, nil
}
func safeID(id string, key []byte) bool {
	if len(id) > 256 || len(key) > 0 && strings.Contains(id, string(key)) {
		return false
	}
	for _, v := range id {
		if !(v >= 'a' && v <= 'z' || v >= 'A' && v <= 'Z' || v >= '0' && v <= '9' || strings.ContainsRune("_:-.", v)) {
			return false
		}
	}
	return true
}
func contextError(ctx context.Context) Error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Error("REQUEST_TIMEOUT")
	}
	return Error("REQUEST_CANCELLED")
}
func transportError(ctx context.Context, e error, sent bool) Error {
	if ctx.Err() != nil {
		return contextError(ctx)
	}
	var certErr *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	if errors.As(e, &certErr) || errors.As(e, &unknown) {
		return Error("TLS_ERROR")
	}
	if sent {
		return Error("REQUEST_OUTCOME_UNKNOWN")
	}
	var ne net.Error
	if errors.As(e, &ne) && ne.Timeout() {
		return Error("REQUEST_TIMEOUT")
	}
	return Error("NETWORK_ERROR")
}
func httpError(status int) Error {
	switch status {
	case 401, 403:
		return Error("INVALID_API_KEY")
	case 404:
		return Error("MODEL_NOT_FOUND")
	case 408, 504:
		return Error("REQUEST_TIMEOUT")
	case 429:
		return Error("RATE_LIMITED")
	}
	return Error("PROVIDER_UNAVAILABLE")
}
