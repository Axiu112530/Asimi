// providerfixture is an explicit loopback-only protocol simulator, never a model.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	ready := flag.String("ready-file", "", "write local listening URL here")
	screenAuditPath := flag.String("screen-audit-file", "", "record metadata for the explicit screen fixture model")
	listenPort := flag.Uint("listen-port", 0, "reuse an isolated fixture profile's original loopback port")
	flag.Parse()
	if *ready == "" || *listenPort > 65535 {
		os.Exit(2)
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *listenPort))
	if err != nil {
		os.Exit(2)
	}
	mux := http.NewServeMux()
	screenFixture := &screenAudit{path: *screenAuditPath}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
		go server.Shutdown(context.Background())
	})
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			w.WriteHeader(401)
			return
		}
		var input map[string]any
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 30<<20)).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		encoded, _ := json.Marshal(input)
		if strings.Contains(string(encoded), "ERROR_429") {
			w.WriteHeader(429)
			return
		}
		if strings.Contains(string(encoded), "ERROR_401") {
			w.WriteHeader(401)
			return
		}
		text := "Fixture response: hello 世界."
		if strings.Contains(string(encoded), "data:image/") {
			text = "Fixture image received: red square."
		}
		if screenFixture.path != "" && input["model"] == screenModel {
			answer, inspectErr := screenFixture.inspect(encoded)
			if inspectErr != nil {
				_ = screenFixture.write(screenRequestAudit{Kind: "rejected"})
				w.WriteHeader(422)
				return
			}
			text = answer
		}
		stream, _ := input["stream"].(bool)
		responses := strings.HasSuffix(r.URL.Path, "/responses")
		usage := map[string]any{"input_tokens": 12, "output_tokens": 6, "total_tokens": 18, "input_tokens_details": map[string]any{"cached_tokens": 2}}
		if !responses {
			usage = map[string]any{"prompt_tokens": 12, "completion_tokens": 6, "total_tokens": 18, "prompt_tokens_details": map[string]any{"cached_tokens": 2}}
		}
		w.Header().Set("x-request-id", "fixture-provider-request")
		if !stream {
			w.Header().Set("Content-Type", "application/json")
			if responses {
				json.NewEncoder(w).Encode(map[string]any{"id": "fixture-response", "status": "completed", "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}}}, "usage": usage})
			} else {
				json.NewEncoder(w).Encode(map[string]any{"id": "fixture-chat", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}}, "usage": usage})
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		send := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\r\n\r\n", b); flusher.Flush() }
		if responses {
			send(map[string]any{"type": "response.created", "response": map[string]any{"id": "fixture-response", "status": "in_progress"}})
		}
		for _, part := range []string{text[:8], text[8:]} {
			if responses {
				send(map[string]any{"type": "response.output_text.delta", "delta": part})
			} else {
				send(map[string]any{"id": "fixture-chat", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": part}}}})
			}
			if strings.Contains(string(encoded), "SLOW_REQUEST") {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(3 * time.Second):
				}
			}
			if strings.Contains(string(encoded), "INTERRUPT_REQUEST") {
				return
			}
		}
		if responses {
			send(map[string]any{"type": "response.completed", "response": map[string]any{"id": "fixture-response", "status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}}}, "usage": usage}})
		} else {
			send(map[string]any{"id": "fixture-chat", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
			send(map[string]any{"choices": []any{}, "usage": usage})
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
		}
	})
	if os.WriteFile(*ready, []byte("http://"+listener.Addr().String()+"/v1"), 0600) != nil {
		listener.Close()
		os.Exit(2)
	}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		os.Exit(1)
	}
}
