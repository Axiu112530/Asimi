package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Explicit acceptance diagnostic: only this generated fixture is sent, once.
func TestLiveDevelopmentPlannerDiagnostic(t *testing.T) {
	if os.Getenv("ASIMI_LIVE_PLANNER_DIAGNOSTIC") != "1" {
		t.Skip("explicit one-request safe fixture planner diagnostic opt-in")
	}
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Fatal("authorized model credential unavailable")
	}
	evidence := os.Getenv("ASIMI_LIVE_EVIDENCE_DIR")
	if evidence == "" {
		t.Fatal("evidence directory required")
	}
	s, dir, _ := fixture(t)
	s.agent02Secret = []byte(key)
	project := mvpWorkspace(t, s, dir)
	for name, source := range map[string]string{
		"go.mod":       "module fixture\n\ngo 1.26\n",
		"calc.go":      "package calc\nfunc Add(a,b int) int{return a-b}\n",
		"calc_test.go": "package calc\nimport \"testing\"\nfunc TestAdd(t *testing.T){if got:=Add(2,3);got!=5{t.Fatalf(\"got %d want 5\",got)}}\n",
	} {
		if err := os.WriteFile(filepath.Join(project.Root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true, "automaticLearning": true, "projectLearning": true, "automaticSkillCreation": true})
	run := mustCall(t, s, "SubmitAgentGoal", map[string]any{"kind": "development", "goal": "Fix the incorrect Add function in calc.go and run the relevant Go tests. Keep the public API unchanged.", "workspaceId": project.ID, "providerId": agent02LiveProvider, "modelId": "deepseek-flash", "paths": []string{"calc.go", "calc_test.go"}}).(unifiedRun)
	preview := mustCall(t, s, "PreviewAgentRun", map[string]string{"id": run.TaskID}).(agent02Preview)
	response, err := s.requestAgent02Plan(t.Context(), preview)
	if err != nil {
		t.Fatalf("one request failed: %s", errorCode(err))
	}
	if strings.Contains(response.Content, key) {
		t.Fatal("credential in provider content")
	}
	_, parseErr := parseAgent02Plan(response.Content)
	parseCode := ""
	if parseErr != nil {
		parseCode = errorCode(parseErr)
	}

	if err := os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "live-planner-safe-fixture-response.json"), []byte(response.Content), 0600); err != nil {
		t.Fatal(err)
	}
	report := map[string]any{"scope": "one generated Go fixture first-round response; no effects executed", "model": "deepseek-flash", "httpStatus": response.HTTPStatus, "usage": response.Usage, "providerRequestId": response.ProviderRequestID, "durationMs": response.DurationMS, "parseError": parseCode, "parserAccepted": parseErr == nil, "modelRequests": 1}
	body, _ := json.MarshalIndent(report, "", "  ")
	if strings.Contains(string(body), key) {
		t.Fatal("credential in diagnostic metadata")
	}
	if err := os.WriteFile(filepath.Join(evidence, "live-planner-safe-fixture-diagnostic.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("one safe fixture request captured: parserAccepted=%t parseCode=%s", parseErr == nil, parseCode)
}
