package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"asimi/internal/computer"
)

func addComputerEvidenceTask(t *testing.T, s *Service) AgentTask {
	t.Helper()
	stamp := now()
	task := AgentTask{ID: newID(), WorkspaceID: newID(), Goal: "Review the isolated fixture", Status: "COMPLETED", CreatedAt: stamp, UpdatedAt: stamp}
	if _, err := s.db.Exec(`INSERT INTO workspaces VALUES(?,?,?,?,?,'USER_SELECTED')`, task.WorkspaceID, t.TempDir(), "Fixture", stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := saveAgentTask(s.db, task); err != nil {
		t.Fatal(err)
	}
	return task
}

func readComputerEvidence(t *testing.T, s *Service, taskID string) []map[string]any {
	t.Helper()
	data := mustCall(t, s, "ListComputerTaskEvidence", map[string]string{"taskId": taskID})
	var rows []map[string]any
	if err := json.Unmarshal([]byte(encode(data)), &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestComputerTaskActionRequiresOriginalApprovalAndVerification(t *testing.T) {
	s := aiFixture(t, nil)
	driver := newControlFixtureDriver()
	s.desktopControl.driver = driver
	task := addComputerEvidenceTask(t, s)
	other := addComputerEvidenceTask(t, s)
	screenError(t, s, "ProposeControlAction", map[string]string{"type": string(computer.LaunchHost), "taskId": task.ID}, "CONTROL_ACTION_DENIED")
	const literal = "User approved fixture text"
	observation := observeControlFixture(t, s)
	input := map[string]string{"type": string(computer.SetValue), "observationId": observation.ID, "elementId": observation.Elements[0].ID, "text": literal, "taskId": newID()}
	screenError(t, s, "ProposeControlAction", input, "NOT_FOUND")
	input["taskId"] = task.ID
	proposal := mustCall(t, s, "ProposeControlAction", input).(computer.Proposal)
	check := func(status string, verified bool) {
		t.Helper()
		rows := readComputerEvidence(t, s, task.ID)
		if len(rows) != 1 || rows[0]["actionId"] != proposal.ID || rows[0]["kind"] != "computer.action" || rows[0]["status"] != status || rows[0]["verified"] != verified || strings.Contains(encode(rows), literal) {
			t.Fatalf("unexpected task evidence %+v", rows)
		}
	}
	check("AWAITING_APPROVAL", false)
	if len(readComputerEvidence(t, s, other.ID)) != 0 || driver.calls != 0 {
		t.Fatal("evidence crossed tasks or the proposal dispatched an effect")
	}
	bad := controlApproval(proposal)
	bad["actionHash"] = "different"
	screenError(t, s, "ApproveControlAction", bad, "CONTROL_APPROVAL_MISMATCH")
	check("AWAITING_APPROVAL", false)
	mustCall(t, s, "ApproveControlAction", controlApproval(proposal))
	check("COMPLETED", true)
	screenError(t, s, "ApproveControlAction", controlApproval(proposal), "CONTROL_APPROVAL_USED")
	if driver.calls != 1 {
		t.Fatalf("expected exactly one execution, got %d", driver.calls)
	}
	stored, err := loadAgentTask(s.db, task.ID)
	if err != nil || encode(stored) != encode(task) {
		t.Fatalf("link changed the original task state: %+v, %v", stored, err)
	}
}

func TestComputerTaskUnverifiedActionRequiresFreshObservation(t *testing.T) {
	s := aiFixture(t, nil)
	driver := newControlFixtureDriver()
	driver.verify = false
	s.desktopControl.driver = driver
	task := addComputerEvidenceTask(t, s)
	observation := observeControlFixture(t, s)
	input := map[string]string{"type": string(computer.SetValue), "observationId": observation.ID, "elementId": observation.Elements[0].ID, "text": "Unverified fixture text", "taskId": task.ID}
	proposal := mustCall(t, s, "ProposeControlAction", input).(computer.Proposal)
	mustCall(t, s, "ApproveControlAction", controlApproval(proposal))
	rows := readComputerEvidence(t, s, task.ID)
	if len(rows) != 1 || rows[0]["status"] != "UNKNOWN_RESULT" || rows[0]["verified"] != false {
		t.Fatalf("unverified action was misreported: %+v", rows)
	}
	screenError(t, s, "ApproveControlAction", controlApproval(proposal), "CONTROL_APPROVAL_USED")
	screenError(t, s, "ProposeControlAction", input, "CONTROL_STALE_TARGET")
	if driver.calls != 1 {
		t.Fatalf("unknown outcome reexecuted: %d effects", driver.calls)
	}
}

func TestComputerTaskScreenBindsSingleReviewedUpload(t *testing.T) {
	s := aiFixture(t, nil)
	s.SetAcceptanceScreenDriver(&fakeScreen{})
	task := addComputerEvidenceTask(t, s)
	other := addComputerEvidenceTask(t, s)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !strings.Contains(string(body), "data:image/png;base64,") || strings.Contains(string(body), task.ID) || strings.Contains(string(body), task.Goal) {
			t.Error("provider must receive the reviewed image without task metadata")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"content":"{\"summary\":\"Visible fixture\",\"detectedText\":[],\"suggestions\":[]}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": aiTestProfile("vision", server.URL)})
	preview := mustCall(t, s, "CaptureScreen", map[string]any{"displayId": "synthetic", "confirmCapture": true}).(screenPreview)
	target := map[string]string{"captureId": preview.CaptureID, "providerId": "vision", "modelId": "fixture", "imageDetail": "high", "taskId": newID()}
	screenError(t, s, "PrepareScreenUpload", target, "NOT_FOUND")
	target["taskId"] = task.ID
	review := mustCall(t, s, "PrepareScreenUpload", target).(screenReview)
	if len(readComputerEvidence(t, s, task.ID)) != 0 || requests.Load() != 0 {
		t.Fatal("preparing a review created evidence or dispatched an upload")
	}
	send := map[string]any{"consentId": review.ConsentID, "content": "Review the visible test image", "confirmSend": true, "taskId": other.ID}
	screenError(t, s, "StartScreenAnalysis", send, "INVALID_REQUEST")
	delete(send, "taskId")
	started := mustCall(t, s, "StartScreenAnalysis", send).(map[string]string)
	if u := awaitAI(t, s, started["requestId"]); u.Status != "COMPLETED" {
		t.Fatalf("fixture upload did not complete: %+v", u)
	}
	rows := readComputerEvidence(t, s, task.ID)
	if len(rows) != 1 || rows[0]["requestId"] != started["requestId"] || rows[0]["messageId"] != started["messageId"] || rows[0]["kind"] != "computer.screen_analysis" || rows[0]["status"] != "COMPLETED" || rows[0]["verified"] != false || strings.Contains(encode(rows), "Visible fixture") || strings.Contains(encode(rows), "data:image/") {
		t.Fatalf("unexpected task evidence %+v", rows)
	}
	screenError(t, s, "StartScreenAnalysis", send, "SCREEN_UPLOAD_REVIEW_REQUIRED")
	if len(readComputerEvidence(t, s, other.ID)) != 0 || requests.Load() != 1 {
		t.Fatal("evidence crossed tasks or the image was uploaded again")
	}
}
