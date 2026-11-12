package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureProfile(protocol, base string) Profile {
	return Profile{ID: "fixture", Name: "Loopback fixture", Enabled: true, Protocol: protocol, BaseURL: base, Models: []Model{{ID: "fixture-model", Capabilities: Capabilities{Text: Supported, Vision: Supported, Streaming: Supported, Temperature: Supported, ImageDetails: []string{"auto", "low", "high"}}}}}
}
func fixtureRequest() Request {
	return Request{RequestID: "request-local", ProviderID: "fixture", ModelID: "fixture-model", MaxOutputTokens: 100, Messages: []Message{{Role: "user", Content: []Content{{Type: "text", Text: "first"}}}, {Role: "assistant", Content: []Content{{Type: "text", Text: "previous"}}}, {Role: "user", Content: []Content{{Type: "text", Text: "look"}, {Type: "image", DataURL: "data:image/png;base64,aW1hZ2U=", Detail: "low"}}}}}
}
func clientFixture(t *testing.T, protocol string, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c, e := NewClient(fixtureProfile(protocol, s.URL+"/v1"))
	if e != nil {
		t.Fatal(e)
	}
	return c, s
}
func assertCode(t *testing.T, e error, code string) {
	t.Helper()
	var coded Error
	if !errors.As(e, &coded) || string(coded) != code {
		t.Fatalf("got %v, want %s", e, code)
	}
}

func TestGenerateRequestShapesAndUsage(t *testing.T) {
	for _, protocol := range []string{Responses, ChatCompletions} {
		t.Run(protocol, func(t *testing.T) {
			c, _ := clientFixture(t, protocol, func(w http.ResponseWriter, r *http.Request) {
				wantPath := "/v1/responses"
				if protocol == ChatCompletions {
					wantPath = "/v1/chat/completions"
				}
				if r.Method != "POST" || r.URL.Path != wantPath {
					t.Errorf("wrong method/path %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer fixture-secret" {
					t.Error("missing auth")
				}
				var b map[string]any
				if json.NewDecoder(r.Body).Decode(&b) != nil {
					t.Error("invalid request JSON")
				}
				if b["model"] != "fixture-model" || b["stream"] != false {
					t.Errorf("wrong model/stream %v", b)
				}
				for _, k := range []string{"tools", "conversation", "previous_response_id"} {
					if _, ok := b[k]; ok {
						t.Errorf("unexpected %s", k)
					}
				}
				key := "input"
				if protocol == ChatCompletions {
					key = "messages"
				} else if b["store"] != false || b["max_output_tokens"] != float64(100) {
					t.Error("Responses storage/output settings")
				}
				messages := b[key].([]any)
				if len(messages) != 3 {
					t.Errorf("history count %d", len(messages))
				}
				last := messages[2].(map[string]any)["content"].([]any)[1].(map[string]any)
				if protocol == Responses {
					if last["type"] != "input_image" || last["image_url"] != "data:image/png;base64,aW1hZ2U=" || last["detail"] != "low" {
						t.Error("Responses image shape")
					}
					if messages[1].(map[string]any)["content"] != "previous" {
						t.Error("assistant history shape")
					}
				} else {
					image := last["image_url"].(map[string]any)
					if image["url"] != "data:image/png;base64,aW1hZ2U=" || image["detail"] != "low" {
						t.Error("Chat image shape")
					}
				}
				w.Header().Set("X-Request-Id", "req-fixture-1")
				if protocol == Responses {
					fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hello"}]}],"usage":{"input_tokens":11,"output_tokens":5,"total_tokens":16,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}}}`)
				} else {
					fmt.Fprint(w, `{"choices":[{"index":0,"message":{"content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":2}}}`)
				}
			})
			out, e := c.Generate(context.Background(), fixtureRequest(), []byte("fixture-secret"))
			if e != nil {
				t.Fatal(e)
			}
			if out.Content != "Hello" || out.Status != "COMPLETED" || out.ProviderRequestID != "req-fixture-1" || out.FirstTokenLatencyMS != nil {
				t.Fatalf("response %+v", out)
			}
			if !out.Usage.UsageAvailable || *out.Usage.InputTokens != 11 || *out.Usage.OutputTokens != 5 || *out.Usage.TotalTokens != 16 || *out.Usage.CachedInputTokens != 3 || *out.Usage.ReasoningTokens != 2 || out.Usage.Source != "PROVIDER_REPORTED" {
				t.Fatalf("usage %+v", out.Usage)
			}
		})
	}
}
func TestProtocolStreamsChunkingCRLFMultilineAndNoUsage(t *testing.T) {
	cases := []struct{ protocol, wire string }{
		{Responses, ": heartbeat\r\n\r\nevent: response.created\r\ndata: {\"type\":\"response.created\"}\r\n\r\nevent: response.output_text.delta\r\ndata: {\"type\":\"response.output_text.delta\",\r\ndata: \"delta\":\"你\"}\r\n\r\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"好\"}\r\n\r\nevent: response.completed\r\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"你好\"}]}]}}\r\n\r\n"},
		{ChatCompletions, ": keepalive\r\n\r\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\r\n\r\ndata: {\"choices\":[{\"index\":0,\r\ndata: \"delta\":{\"content\":\"你\"}}]}\r\n\r\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"},\"finish_reason\":\"stop\"}]}\r\n\r\ndata: [DONE]\r\n\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			c, _ := clientFixture(t, tc.protocol, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				for i := 0; i < len(tc.wire); i++ {
					_, _ = w.Write([]byte(tc.wire[i : i+1]))
					w.(http.Flusher).Flush()
				}
			})
			var text strings.Builder
			out, e := c.Stream(context.Background(), fixtureRequest(), nil, func(d Delta) error { text.WriteString(d.Content); return nil })
			if e != nil {
				t.Fatal(e)
			}
			if out.Content != "你好" || text.String() != out.Content || out.Status != "COMPLETED" || out.FirstTokenLatencyMS == nil {
				t.Fatalf("response %+v / %s", out, text.String())
			}
			if out.Usage.UsageAvailable || out.Usage.InputTokens != nil || out.Usage.TotalTokens != nil || out.Usage.Source != "UNAVAILABLE" {
				t.Fatalf("unknown usage %+v", out.Usage)
			}
		})
	}
}
func TestStreamUsage(t *testing.T) {
	for _, protocol := range []string{Responses, ChatCompletions} {
		t.Run(protocol, func(t *testing.T) {
			wire := "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":0,\"output_tokens\":1},\"output\":[]}}\n\n"
			if protocol == ChatCompletions {
				wire = "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"
			}
			c, _ := clientFixture(t, protocol, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, wire)
			})
			out, e := c.Stream(context.Background(), fixtureRequest(), nil, nil)
			if e != nil {
				t.Fatal(e)
			}
			if !out.Usage.UsageAvailable || out.Usage.InputTokens == nil || *out.Usage.InputTokens != 0 || out.Usage.TotalTokens != nil {
				t.Fatalf("partial usage %+v", out.Usage)
			}
		})
	}
}
func TestHTTPFailuresAreRedactedAndNeverRetried(t *testing.T) {
	for status, code := range map[int]string{401: "INVALID_API_KEY", 429: "RATE_LIMITED", 500: "PROVIDER_UNAVAILABLE", 503: "PROVIDER_UNAVAILABLE", 404: "MODEL_NOT_FOUND"} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var n atomic.Int32
			c, _ := clientFixture(t, Responses, func(w http.ResponseWriter, r *http.Request) {
				n.Add(1)
				w.Header().Set("X-Request-Id", "fixture-secret")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"message":"fixture-secret raw private content"}}`)
			})
			out, e := c.Generate(context.Background(), fixtureRequest(), []byte("fixture-secret"))
			assertCode(t, e, code)
			b, _ := json.Marshal(out)
			if strings.Contains(string(b), "fixture-secret") || strings.Contains(e.Error(), "private") {
				t.Fatal("secret disclosed")
			}
			if n.Load() != 1 || out.HTTPStatus != status {
				t.Fatalf("requests/status %d/%d", n.Load(), out.HTTPStatus)
			}
		})
	}
}
func TestRedirectDoesNotDispatchOrLeakCredentials(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	c, _ := clientFixture(t, Responses, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	_, e := c.Generate(context.Background(), fixtureRequest(), []byte("fixture-secret"))
	assertCode(t, e, "PROVIDER_UNAVAILABLE")
	if targetCalls.Load() != 0 {
		t.Fatal("redirect followed")
	}
}
func TestEndpointValidation(t *testing.T) {
	for _, base := range []string{"https://user:secret@example.test/v1", "https://example.test/v1?api_key=secret", "https://example.test/v1#secret", "http://example.test/v1", "http://8.8.8.8/v1", "http://192.168.1.2/v1", "file:///x", "https://example.test/../x", "https://example.test/%2fsecret", "https://example.test:0/v1", "https://example.test:65536/v1"} {
		p := fixtureProfile(Responses, base)
		if e := ValidateProfile(p); e == nil {
			t.Errorf("accepted %s", base)
		}
	}
	for _, base := range []string{"https://gateway.example.test/custom/v1", "http://127.0.0.1:8080/v1", "http://[::1]:8080/v1", "http://localhost:8080/v1"} {
		if e := ValidateProfile(fixtureProfile(Responses, base)); e != nil {
			t.Errorf("%s: %v", base, e)
		}
	}
	p := fixtureProfile(Responses, "http://192.168.1.2/v1")
	p.TrustedHTTP = true
	if e := ValidateProfile(p); e != nil {
		t.Fatal(e)
	}
	p.BaseURL = "http://8.8.8.8/v1"
	if ValidateProfile(p) == nil {
		t.Fatal("public HTTP trust bypass")
	}
}
func TestCancellationAndPreservedPartial(t *testing.T) {
	c, _ := clientFixture(t, ChatCompletions, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, e := c.Stream(ctx, fixtureRequest(), nil, func(Delta) error { cancel(); return nil })
	assertCode(t, e, "REQUEST_CANCELLED")
	if out.Content != "partial" || out.Status != "CANCELLED" {
		t.Fatalf("lost partial %+v", out)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	_, e = c.Generate(ctx2, fixtureRequest(), nil)
	assertCode(t, e, "REQUEST_CANCELLED")
}
func TestDeadline(t *testing.T) {
	c, _ := clientFixture(t, Responses, func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, e := c.Generate(ctx, fixtureRequest(), nil)
	assertCode(t, e, "REQUEST_TIMEOUT")
}
func TestDispatchUnknownNotRetried(t *testing.T) {
	var count atomic.Int32
	c, _ := clientFixture(t, Responses, func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		conn, _, e := w.(http.Hijacker).Hijack()
		if e == nil {
			_ = conn.Close()
		}
	})
	out, e := c.Generate(context.Background(), fixtureRequest(), nil)
	assertCode(t, e, "REQUEST_OUTCOME_UNKNOWN")
	if count.Load() != 1 || out.Status != "FAILED" {
		t.Fatal("retried unknown request")
	}
}
func TestInterruptionsRequireTerminalMarker(t *testing.T) {
	for _, protocol := range []string{Responses, ChatCompletions} {
		t.Run(protocol, func(t *testing.T) {
			wire := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
			if protocol == ChatCompletions {
				wire = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"stop\"}]}\n\n"
			}
			c, _ := clientFixture(t, protocol, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, wire)
			})
			out, e := c.Stream(context.Background(), fixtureRequest(), nil, nil)
			assertCode(t, e, "STREAM_INTERRUPTED")
			if out.Content != "partial" || out.Status != "INTERRUPTED" {
				t.Fatalf("partial %+v", out)
			}
		})
	}
}
func TestUnsafeOutputsAndProviderEvents(t *testing.T) {
	tests := []struct {
		protocol, body, code string
		stream               bool
	}{
		{Responses, `{"status":"completed","output":[{"type":"function_call","name":"filesystem.file.create"}]}`, "UNSUPPORTED_TOOL_CALL", false},
		{Responses, `{"status":"completed","output":[{"type":"computer_call"}]}`, "UNSUPPORTED_TOOL_CALL", false},
		{Responses, `{"status":"completed","output":[{"type":"shell_call"}]}`, "UNSUPPORTED_TOOL_CALL", false},
		{ChatCompletions, `{"choices":[{"index":0,"message":{"tool_calls":[{"type":"function"}]},"finish_reason":"tool_calls"}]}`, "UNSUPPORTED_TOOL_CALL", false},
		{ChatCompletions, `{"choices":[{"index":0,"message":{"function_call":{"name":"shell"}},"finish_reason":"function_call"}]}`, "UNSUPPORTED_TOOL_CALL", false},
		{Responses, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\"}}\n\n", "UNSUPPORTED_TOOL_CALL", true},
		{ChatCompletions, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"type\":\"function\"}]}}]}\n\n", "UNSUPPORTED_TOOL_CALL", true},
		{Responses, "event: error\ndata: {\"type\":\"error\",\"message\":\"fixture-secret\"}\n\n", "PROVIDER_UNAVAILABLE", true},
		{ChatCompletions, "data: {\"error\":{\"message\":\"fixture-secret\"}}\n\n", "PROVIDER_UNAVAILABLE", true},
		{Responses, `{"status":"incomplete","output":[],"usage":{"input_tokens":5}}`, "RESPONSE_INCOMPLETE", false},
		{Responses, `{"status":"failed","error":{"message":"fixture-secret"}}`, "PROVIDER_UNAVAILABLE", false},
		{Responses, `{"status":"cancelled","output":[]}`, "REQUEST_CANCELLED", false},
		{Responses, `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"declined"}]}]}`, "PROVIDER_REFUSAL", false},
		{ChatCompletions, `{"choices":[{"index":0,"message":{"refusal":"declined"},"finish_reason":"stop"}]}`, "PROVIDER_REFUSAL", false},
		{ChatCompletions, "data: [DONE]\n\n", "INVALID_RESPONSE", true},
		{ChatCompletions, "data: {invalid}\n\n", "INVALID_RESPONSE", true},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			c, _ := clientFixture(t, tt.protocol, func(w http.ResponseWriter, r *http.Request) {
				if tt.stream {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				fmt.Fprint(w, tt.body)
			})
			var out Response
			var e error
			if tt.stream {
				out, e = c.Stream(context.Background(), fixtureRequest(), []byte("fixture-secret"), nil)
			} else {
				out, e = c.Generate(context.Background(), fixtureRequest(), []byte("fixture-secret"))
			}
			assertCode(t, e, tt.code)
			if strings.Contains(e.Error(), "fixture-secret") || out.Status == "COMPLETED" {
				t.Fatalf("unsafe failure %+v %v", out, e)
			}
		})
	}
}
func TestRejectedOutputRetainsReportedUsage(t *testing.T) {
	tests := []struct {
		name, protocol, body, code string
		stream                     bool
	}{
		{"responses refusal", Responses, `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"declined"}]}],"usage":{"input_tokens":5}}`, "PROVIDER_REFUSAL", false},
		{"responses streamed refusal", Responses, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"refusal\",\"refusal\":\"declined\"}]}],\"usage\":{\"input_tokens\":5}}}\n\n", "PROVIDER_REFUSAL", true},
		{"chat tool call", ChatCompletions, `{"choices":[{"index":0,"message":{"tool_calls":[{"type":"function"}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5}}`, "UNSUPPORTED_TOOL_CALL", false},
		{"chat streamed tool call", ChatCompletions, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"type\":\"function\"}]}}],\"usage\":{\"prompt_tokens\":5}}\n\n", "UNSUPPORTED_TOOL_CALL", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := clientFixture(t, tt.protocol, func(w http.ResponseWriter, _ *http.Request) {
				if tt.stream {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				fmt.Fprint(w, tt.body)
			})
			var result Response
			var err error
			if tt.stream {
				result, err = client.Stream(context.Background(), fixtureRequest(), nil, nil)
			} else {
				result, err = client.Generate(context.Background(), fixtureRequest(), nil)
			}
			assertCode(t, err, tt.code)
			usage := result.Usage
			if result.Status == "COMPLETED" || !usage.UsageAvailable || usage.Source != "PROVIDER_REPORTED" ||
				usage.InputTokens == nil || *usage.InputTokens != 5 || usage.OutputTokens != nil || usage.TotalTokens != nil {
				t.Fatalf("unexpected status or usage: %+v", result)
			}
		})
	}
}

func TestAdmissionAndProfileSnapshot(t *testing.T) {
	var count atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		fmt.Fprint(w, `{"status":"completed","output":[]}`)
	}))
	defer s.Close()
	p := fixtureProfile(Responses, s.URL)
	p.Models[0].Capabilities.Vision = Unknown
	c, e := NewClient(p)
	if e != nil {
		t.Fatal(e)
	}
	p.Models[0].Capabilities.Vision = Supported
	_, e = c.Generate(context.Background(), fixtureRequest(), nil)
	assertCode(t, e, "UNSUPPORTED_CAPABILITY")
	if count.Load() != 0 {
		t.Fatal("unknown vision sent")
	}
	r := fixtureRequest()
	r.Messages = r.Messages[:1]
	r.ProviderID = "other"
	_, e = c.Generate(context.Background(), r, nil)
	assertCode(t, e, "INVALID_REQUEST")
	r.ProviderID = "fixture"
	r.Messages[0].Role = "tool"
	_, e = c.Generate(context.Background(), r, nil)
	assertCode(t, e, "INVALID_REQUEST")
}
func TestBoundedResponsesAndSSE(t *testing.T) {
	c, _ := clientFixture(t, Responses, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", maxBodyBytes+1))
	})
	_, e := c.Generate(context.Background(), fixtureRequest(), nil)
	assertCode(t, e, "INVALID_RESPONSE")
	c2, _ := clientFixture(t, Responses, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+strings.Repeat("x", maxEventBytes)+"\n\n")
	})
	_, e = c2.Stream(context.Background(), fixtureRequest(), nil, nil)
	if e == nil {
		t.Fatal("oversized event accepted")
	}
}
func TestTLSVerification(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS reached handler") }))
	defer s.Close()
	c, e := NewClient(fixtureProfile(Responses, s.URL))
	if e != nil {
		t.Fatal(e)
	}
	_, e = c.Generate(context.Background(), fixtureRequest(), []byte("fixture-secret"))
	assertCode(t, e, "TLS_ERROR")
}

func TestRequestJSONOutputIsOptIn(t *testing.T) {
	for _, protocol := range []string{ChatCompletions, Responses} {
		t.Run(protocol, func(t *testing.T) {
			p := fixtureProfile(protocol, "http://127.0.0.1")
			p.Models[0].Capabilities.StructuredOutput = Supported
			c, err := NewClient(p)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, enabled := range []bool{false, true} {
				request := fixtureRequest()
				request.JSONOutput = enabled
				body, err := c.requestBody(request)
				if err != nil {
					t.Fatal(err)
				}
				want := `"response_format":{"type":"json_object"}`
				if protocol == Responses {
					want = `"text":{"format":{"type":"json_object"}}`
				}
				if strings.Contains(string(body), want) != enabled {
					t.Fatalf("JSON output field does not match opt-in = %t", enabled)
				}
			}
		})
	}
	p := fixtureProfile(ChatCompletions, "http://127.0.0.1")
	c, err := NewClient(p)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	request := fixtureRequest()
	request.JSONOutput = true
	_, err = c.requestBody(request)
	assertCode(t, err, "UNSUPPORTED_CAPABILITY")
}

func TestOfficialDeepSeekResponsesWire(t *testing.T) {
	p := fixtureProfile(Responses, "https://api.deepseek.com")
	p.Models[0].ID = "deepseek-flash"
	c, err := NewClient(p)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.endpoint != "https://api.deepseek.com/responses" {
		t.Fatalf("incorrect official Responses endpoint: %s", c.endpoint)
	}
	request := fixtureRequest()
	request.ModelID = "deepseek-flash"
	body, err := c.requestBody(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"reasoning":{"effort":"none"}`) {
		t.Fatal("missing official DeepSeek reasoning control")
	}
	var out Response
	err = parseResponse([]byte(`{"id":"fixture-response-id","status":"completed","output":[]}`), &out)
	if err != nil || out.ProviderRequestID != "fixture-response-id" {
		t.Fatalf("missing response ID: %q, %v", out.ProviderRequestID, err)
	}
}
