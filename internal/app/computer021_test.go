package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"asimi/internal/computer"
)

func TestComputerSuggestionStrictResult(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"text", `{"summary":"Fixture input","action":{"type":"uia.set_value","text":"Hello Asimi"}}`, true},
		{"invoke", `{"summary":"Fixture button","action":{"type":"uia.invoke","text":""}}`, true},
		{"abstain", `{"summary":"Control is unclear","action":null}`, true},
		{"unwrapped", `{"type":"uia.invoke","text":""}`, false},
		{"duplicate", `{"summary":"First","summary":"Second","action":null}`, false},
		{"missing", `{"summary":"No action field"}`, false},
		{"alias", `{"Summary":"Wrong casing","action":null}`, false},
		{"coordinates", `{"summary":"Click","action":{"type":"uia.invoke","text":"","x":2,"y":5}}`, false},
		{"replacement target", `{"summary":"Click","action":{"type":"uia.invoke","text":"","elementId":"other"}}`, false},
		{"duplicate type", `{"summary":"Click","action":{"type":"uia.invoke","type":"uia.set_value","text":"Changed"}}`, false},
		{"keyboard", `{"summary":"Type","action":{"type":"keyboard.type_text","text":"Hello"}}`, false},
		{"secret", `{"summary":"Fill","action":{"type":"uia.set_value","text":"password"}}`, false},
		{"trailing", `{"summary":"Fixture","action":null} {}`, false},
		{"oversize", strings.Repeat(" ", 8192), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := parseComputerSuggestion(tc.raw)
			if ok != tc.valid {
				t.Fatalf("valid = %v, want %v", ok, tc.valid)
			}
		})
	}
}

func TestComputerSuggestionRequiresReviewedVisionAndExactApproval(t *testing.T) {
	s := aiFixture(t, nil)
	driver := newControlFixtureDriver()
	s.desktopControl.driver = driver
	clock := time.Now()
	s.desktopControl.clock = func() time.Time { return clock }
	s.SetAcceptanceScreenDriver(&fakeScreen{})
	if err := s.initializeMVP(); err != nil {
		t.Fatal(err)
	}
	task := AgentTask{ID: newID()}
	workspaceID, stamp := newID(), now()
	if _, err := s.db.Exec("INSERT INTO workspaces(id,root,name,created_at,last_used_at,trust_status) VALUES(?,?,?,?,?,'USER_SELECTED')",
		workspaceID, s.dataDir, "Fixture workspace", stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO agent_tasks(id,workspace_id,status,json,created_at,updated_at) VALUES(?,?,'PLANNED',?,?,?)",
		task.ID, workspaceID, encode(AgentTask{ID: task.ID, WorkspaceID: workspaceID}), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	const raw = `{"summary":"A fixture input","action":{"type":"uia.set_value","text":"Hello Asimi"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(string(body), "data:image/") &&
			(!strings.Contains(string(body), "TestInput") || !strings.Contains(string(body), "uia.set_value") ||
				strings.Contains(string(body), "fixture.exe") || strings.Contains(string(body), "fixture-editor")) {
			t.Errorf("vision request scope missing or leaked local identity")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"index": 0, "message": map[string]string{"content": raw}, "finish_reason": "stop",
		}}})
	}))
	defer server.Close()
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": aiTestProfile("vision", server.URL)})
	ordinary := mustCall(t, s, "StartChat", ChatInput{ProviderID: "vision", ModelID: "fixture", Content: "Type text", ConfirmSend: true}).(map[string]string)
	awaitAI(t, s, ordinary["requestId"])
	stage := func(requestID string) map[string]string {
		return map[string]string{"requestId": requestID, "taskId": task.ID}
	}
	screenError(t, s, "ProposeComputerSuggestion", stage(ordinary["requestId"]), "CONTROL_SUGGESTION_UNAVAILABLE")
	analyze := func(o computer.Observation) string {
		preview := mustCall(t, s, "CaptureScreen", map[string]any{"displayId": "synthetic", "confirmCapture": true}).(screenPreview)
		target := map[string]any{
			"captureId": preview.CaptureID, "providerId": "vision", "modelId": "fixture", "imageDetail": "high",
			"taskId": task.ID, "controlObservationId": o.ID, "controlElementId": o.Elements[0].ID,
		}
		review := mustCall(t, s, "PrepareScreenUpload", target).(screenReview)
		input := map[string]any{"consentId": review.ConsentID, "content": "Fill the fixture", "confirmSend": false}
		screenError(t, s, "StartScreenAnalysis", input, "CONFIRM_SEND_REQUIRED")
		input["confirmSend"] = true
		request := mustCall(t, s, "StartScreenAnalysis", input).(map[string]string)["requestId"]
		if got := awaitAI(t, s, request); got.Status != "COMPLETED" {
			t.Fatalf("provider request did not complete: %+v", got)
		}
		return request
	}
	observation := observeControlFixture(t, s)
	stale := analyze(observation)
	fresh := observeControlFixture(t, s)
	screenError(t, s, "ProposeComputerSuggestion", stage(stale), "CONTROL_STALE_TARGET")
	invalidatedAfterReview := analyze(fresh)
	invalidatedProposal := mustCall(t, s, "ProposeComputerSuggestion", stage(invalidatedAfterReview)).(computer.Proposal)
	fresh = observeControlFixture(t, s)
	if r := mustCall(t, s, "ApproveControlAction", controlApproval(invalidatedProposal)).(computer.Record); r.Status != "STALE_TARGET" || driver.calls != 0 {
		t.Fatalf("a new inspection did not invalidate the staged observation: %+v", r)
	}
	fresh = observeControlFixture(t, s)
	expired := analyze(fresh)
	clock = clock.Add(computerSuggestionTTL)
	screenError(t, s, "ProposeComputerSuggestion", stage(expired), "CONTROL_STALE_TARGET")
	fresh = observeControlFixture(t, s)
	requestID := analyze(fresh)
	if driver.calls != 0 {
		t.Fatal("model output dispatched a desktop effect")
	}
	p := mustCall(t, s, "ProposeComputerSuggestion", stage(requestID)).(computer.Proposal)
	if driver.calls != 0 || p.Status != "AWAITING_APPROVAL" || p.SourceRequestID != requestID ||
		p.ObservationID != fresh.ID || p.Element.ID != fresh.Elements[0].ID || p.Text != "Hello Asimi" {
		t.Fatalf("proposal lost its source, target, or exact parameters: %+v", p)
	}
	bad := controlApproval(p)
	bad["actionHash"] = "different"
	screenError(t, s, "ApproveControlAction", bad, "CONTROL_APPROVAL_MISMATCH")
	r := mustCall(t, s, "ApproveControlAction", controlApproval(p)).(computer.Record)
	if !r.Verified || driver.calls != 1 {
		t.Fatalf("effect failed independent verification: %+v", r)
	}
	screenError(t, s, "ProposeComputerSuggestion", stage(requestID), "CONTROL_SUGGESTION_UNAVAILABLE")
	rows := mustCall(t, s, "ListComputerTaskEvidence", map[string]string{"taskId": task.ID}).([]computerTaskEvidence)
	found := false
	for _, row := range rows {
		found = found || row.ActionID == p.ID && row.RequestID == requestID && row.Verified && row.Selector == "TestInput"
	}
	if !found {
		t.Fatal("task history lost action provenance or verification")
	}
	fresh = observeControlFixture(t, s)
	visionRequest := analyze(fresh)
	managed := mustCall(t, s, "ProposeComputerSuggestion", map[string]any{
		"requestId": visionRequest, "taskId": task.ID, "asTask": true,
	}).(computer.Proposal)
	if !managed.HarnessTask || managed.TaskID == task.ID || driver.calls != 1 {
		t.Fatal("vision did not create a new task with a pending approval")
	}
	mustCall(t, s, "ApproveControlAction", controlApproval(managed))
	desktop := mustCall(t, s, "GetAgentTask", map[string]string{"id": managed.TaskID}).(AgentTask)
	if desktop.Status != "COMPLETED" || driver.calls != 2 || !strings.Contains(string(desktop.Steps[0].Result), visionRequest) ||
		strings.Contains(encode(desktop), "Hello Asimi") {
		t.Fatalf("vision-to-UIA task lost verification, provenance or privacy: %s", desktop.Status)
	}
	evidence := readComputerEvidence(t, s, managed.TaskID)
	if len(evidence) != 1 || evidence[0]["requestId"] != visionRequest || evidence[0]["selector"] != "TestInput" {
		t.Fatalf("desktop evidence lost the reviewed visual source: %v", evidence)
	}
	driver.window.App = computer.Notepad
	notepad := observeControlFixture(t, s)
	preview := mustCall(t, s, "CaptureScreen", map[string]any{"displayId": "synthetic", "confirmCapture": true}).(screenPreview)
	screenError(t, s, "PrepareScreenUpload", map[string]any{
		"captureId": preview.CaptureID, "providerId": "vision", "modelId": "fixture", "imageDetail": "high",
		"taskId": task.ID, "controlObservationId": notepad.ID, "controlElementId": notepad.Elements[0].ID,
	}, "CONTROL_ACTION_DENIED")
}
