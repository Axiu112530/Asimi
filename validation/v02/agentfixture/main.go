// The loopback protocol fixture never reads project files or executes tools.
package main

import (
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	ready := flag.String("ready-file", "", "isolated launcher path")
	flag.Parse()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil || *ready == "" {
		os.Exit(1)
	}
	if os.WriteFile(*ready, []byte("http://"+listener.Addr().String()+"/v1"), 0600) != nil {
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", reply)
	server := http.Server{Handler: mux, ReadHeaderTimeout: 4 * time.Second}
	_ = server.Serve(listener)
}

type contextEnvelope struct {
	ProjectContext struct {
		Files []struct{ Path, Content, SHA256 string }
	} `json:"projectContext"`
	Observations []struct{ Kind, Status, Evidence string } `json:"observations"`
}

func reply(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request) != nil || request.Model != "agent-fixture" || len(request.Messages) == 0 {
		http.Error(w, "invalid fixture request", 400)
		return
	}
	var contextJSON string
	content := request.Messages[len(request.Messages)-1].Content
	if json.Unmarshal(content, &contextJSON) != nil {
		var blocks []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(content, &blocks) != nil || len(blocks) != 1 {
			http.Error(w, "missing fixture context", 400)
			return
		}
		contextJSON = blocks[0].Text
	}
	var context contextEnvelope
	if json.Unmarshal([]byte(contextJSON), &context) != nil {
		http.Error(w, "bad context", 400)
		return
	}
	var file struct{ Path, Content, SHA256 string }
	for _, entry := range context.ProjectContext.Files {
		if entry.Path == "calc.go" {
			file = entry
			break
		}
	}
	if file.Path == "" {
		http.Error(w, "fixture file absent", 400)
		return
	}
	step := map[string]any{"type": "PROPOSE_FILE_CHANGE", "path": file.Path, "baseSha256": file.SHA256, "proposedContent": strings.Replace(file.Content, "a - b", "a + b", 1), "summary": "Replace subtraction with addition."}
	for _, observed := range context.Observations {
		if observed.Kind == "file" && observed.Status == "COMPLETED" {
			step = map[string]any{"type": "PROPOSE_COMMAND", "commandId": "go.test", "args": []string{"./..."}, "summary": "Run the project regression test."}
		}
		if observed.Kind == "command" && observed.Status == "COMPLETED" && strings.Contains(observed.Evidence, "\"exitCode\":0") {
			step = map[string]any{"type": "FINALIZE", "summary": "The Add implementation was updated and the observed Go regression test passed."}
			break
		}
	}
	plan, _ := json.Marshal(map[string]any{"objective": "Correct Add", "assumptions": []string{}, "requiredCapabilities": []string{"workspace"}, "steps": []any{step}, "expectedResults": []string{"Correct addition"}, "riskLevel": "read"})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "agent-fixture-request", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(plan)}}}, "usage": map[string]int{"prompt_tokens": 256, "completion_tokens": 128, "total_tokens": 384}})
}
