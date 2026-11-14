package app

import (
	"asimi/internal/ai"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveUnifiedPublicGitHubDeepSeek(t *testing.T) {
	if os.Getenv("ASIMI_LIVE_UNIFIED_ACCEPTANCE") != "1" {
		t.Skip("explicit real BYOK and public GitHub acceptance opt-in")
	}
	secret := os.Getenv("DEEPSEEK_API_KEY")
	if strings.TrimSpace(secret) == "" {
		t.Fatal("DEEPSEEK_API_KEY is required for the authorized opt-in test")
	}
	s, _, _ := fixture(t)
	profile := aiTestProfile("live-unified-deepseek", "https://api.deepseek.com/v1")
	profile.Models[0].ID = "deepseek-flash"
	profile.Models[0].DisplayName = "DeepSeek Flash"
	profile.Models[0].Pricing = nil
	profile.Models[0].Capabilities.StructuredOutput = ai.Supported
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": profile})
	mustCall(t, s, "StoreCredential", map[string]string{"providerId": profile.ID, "secret": secret})
	// No repository source, personal desktop content or account credentials are
	// included in the model request: it sees only public GitHub metadata.
	run := mustCall(t, s, "SubmitAgentGoal", map[string]any{"kind": "auto", "goal": "概述 https://github.com/octocat/Hello-World 的公开仓库元数据，只说明可验证事实。"}).(unifiedRun)
	if run.Kind != "communication" || run.Status != "WAITING_APPROVAL" {
		t.Fatalf("goal did not stage a scoped read: %s %s", run.Kind, run.Status)
	}
	if r := s.Call("PreviewUnifiedSummary", encode(map[string]string{"id": run.ID, "providerId": profile.ID, "modelId": "deepseek-flash"})); r.Error == nil {
		t.Fatal("unapproved facts reached summarization")
	}
	proposal := run.Proposal.(map[string]any)
	mustCall(t, s, "ApproveConnectorAction", map[string]any{"id": proposal["id"], "hash": proposal["hash"]})
	run, err := s.unifiedRunByID(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "COMPLETED" || !strings.Contains(encode(run.Result), "octocat/Hello-World") {
		t.Fatal("public repository identity was not independently validated")
	}
	preview := mustCall(t, s, "PreviewUnifiedSummary", map[string]string{"id": run.ID, "providerId": profile.ID, "modelId": "deepseek-flash"}).(unifiedSummaryPreview)
	if strings.Contains(preview.Content, secret) {
		t.Fatal("credential leaked into model context")
	}
	summary := mustCall(t, s, "SummarizeUnifiedRun", map[string]any{"id": run.ID, "contextHash": preview.ContextHash, "providerId": profile.ID, "modelId": "deepseek-flash", "confirmSend": true}).(unifiedRun)
	until := time.Now().Add(90 * time.Second)
	var usage ai.UsageRecord
	for time.Now().Before(until) {
		rows := mustCall(t, s, "GetUsage", struct{}{}).([]ai.UsageRecord)
		found := false
		for _, row := range rows {
			if row.RequestID == summary.RequestID {
				usage = row
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if usage.Status != "COMPLETED" {
		t.Fatalf("live provider request did not complete, status=%s", usage.Status)
	}
	summary, err = s.unifiedRunByID(summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "COMPLETED" || len(encode(summary.Result)) < 25 || summary.ParentID != run.ID {
		t.Fatal("live summary missing linked factual result")
	}
	var leaks int
	if err = s.db.QueryRow("SELECT count(*) FROM messages WHERE instr(content,?)>0", secret).Scan(&leaks); err != nil || leaks != 0 {
		t.Fatal("credential persisted in conversation text")
	}
	repeat := mustCall(t, s, "SummarizeUnifiedRun", map[string]any{"id": run.ID, "contextHash": preview.ContextHash, "providerId": profile.ID, "modelId": "deepseek-flash", "confirmSend": true}).(unifiedRun)
	if repeat.RequestID != summary.RequestID {
		t.Fatal("completed summary was spent again")
	}
	report := map[string]any{"passed": true, "scope": "public GitHub read -> exact approval -> validated identity -> reviewed real BYOK summary -> duplicate request reuse", "provider": "DeepSeek", "model": "deepseek-flash", "connector": "github", "resource": "octocat/Hello-World", "sourceRunId": run.ID, "summaryRunId": summary.ID, "summaryParentId": summary.ParentID, "usage": usage, "publicResult": run.Result, "modelAnswer": summary.Result, "paidRequests": 1, "externalWrites": 0, "credentialLeakCount": leaks}
	if directory := os.Getenv("ASIMI_LIVE_EVIDENCE_DIR"); directory != "" {
		if err = os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		body, _ := json.MarshalIndent(report, "", "  ")
		if strings.Contains(string(body), secret) {
			t.Fatal("credential in report")
		}
		if err = os.WriteFile(filepath.Join(directory, "live-github-deepseek.json"), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Real public GitHub verified; linked DeepSeek %s summary completed; paid requests=1, external writes=0, credential leaks=%d", profile.Models[0].ID, leaks)
}
