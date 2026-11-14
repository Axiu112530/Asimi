//go:build windows

package app

import (
	"asimi/internal/ai"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"asimi/internal/computer"
	"asimi/internal/screen"
)

func TestNativeHarnessDesktopTask(t *testing.T) {
	if os.Getenv("ASIMI_HARNESS_DESKTOP_TEST") != "1" {
		t.Skip("dedicated native fixture is opt-in")
	}
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	status := mustCall(t, s, "GetControlStatus", struct{}{}).(map[string]any)
	if status["available"] != true {
		t.Fatal("fixed UIA helpers and a medium integrity interactive session are required")
	}
	launch := mustCall(t, s, "ProposeControlAction", map[string]string{"type": string(computer.LaunchHost)}).(computer.Proposal)
	r := mustCall(t, s, "ApproveControlAction", controlApproval(launch)).(computer.Record)
	if !r.Verified {
		t.Fatalf("dedicated fixture launch: %+v", r)
	}
	windows := mustCall(t, s, "ListControlWindows", struct{}{}).([]computer.Window)
	var window computer.Window
	for _, candidate := range windows {
		if candidate.App == computer.TestHost {
			window = candidate
			break
		}
	}
	if window.ID == "" {
		t.Fatal("dedicated fixture window not observed")
	}
	var lastTask string
	for _, step := range []struct{ selector, action, text string }{
		{"TestInput", string(computer.SetValue), "Harness native test 你好"},
		{"SubmitButton", string(computer.Invoke), ""},
	} {
		o := mustCall(t, s, "InspectControlWindow", map[string]string{"windowId": window.ID}).(computer.Observation)
		var element computer.Element
		for _, candidate := range o.Elements {
			if candidate.AutomationID == step.selector {
				element = candidate
				break
			}
		}
		if element.ID == "" {
			t.Fatal("dedicated control missing")
		}
		raw := mustCall(t, s, "CreateDesktopTask", map[string]string{
			"workspaceId": w.ID, "type": step.action, "observationId": o.ID, "elementId": element.ID, "text": step.text,
		})
		var run struct {
			AgentTask
			Proposal computer.Proposal `json:"proposal"`
		}
		if err := json.Unmarshal([]byte(encode(raw)), &run); err != nil {
			t.Fatal(err)
		}
		if run.Status != "WAITING_APPROVAL" {
			t.Fatal("action did not wait for approval")
		}
		result := mustCall(t, s, "ApproveControlAction", controlApproval(run.Proposal)).(computer.Record)
		task := mustCall(t, s, "GetAgentTask", map[string]string{"id": run.ID}).(AgentTask)
		if !result.Verified || task.Status != "COMPLETED" {
			t.Fatalf("native %s not verified: %s %s", step.action, result.Status, task.Status)
		}
		screenError(t, s, "ApproveControlAction", controlApproval(run.Proposal), "CONTROL_APPROVAL_USED")
		lastTask = task.ID
		t.Logf("%s: fresh observation, consumed approval, native verification and task completion", step.selector)
	}

	// Capture only the owned fixture's selected control, using the real
	// native screen driver. The local provider returns a fixed suggestion.
	o := mustCall(t, s, "InspectControlWindow", map[string]string{"windowId": window.ID}).(computer.Observation)
	var target computer.Element
	for _, el := range o.Elements {
		if el.AutomationID == "ResetButton" {
			target = el
		}
	}
	if target.ID == "" {
		t.Fatal("reset button missing")
	}
	displays := mustCall(t, s, "ListDisplays", struct{}{}).([]screen.Display)
	var display screen.Display
	for _, d := range displays {
		if target.Bounds.X >= d.PhysicalBounds.X && target.Bounds.Y >= d.PhysicalBounds.Y &&
			target.Bounds.X+target.Bounds.Width <= d.PhysicalBounds.X+d.PhysicalBounds.Width &&
			target.Bounds.Y+target.Bounds.Height <= d.PhysicalBounds.Y+d.PhysicalBounds.Height {
			display = d
			break
		}
	}
	if display.ID == "" {
		t.Fatal("fixture control is not fully visible on a display")
	}
	region := screen.Rect{X: target.Bounds.X - display.PhysicalBounds.X, Y: target.Bounds.Y - display.PhysicalBounds.Y,
		Width: target.Bounds.Width, Height: target.Bounds.Height}
	capture := mustCall(t, s, "CaptureScreen", map[string]any{"displayId": display.ID, "region": region, "confirmCapture": true}).(screenPreview)
	if capture.Backend != "WIN32_GDI" || capture.Width != region.Width || capture.Height != region.Height || capture.ByteSize <= 0 {
		t.Fatal("expected a real native region capture")
	}
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || !strings.Contains(string(body), "data:image/png;base64,") {
			t.Error("reviewed native PNG missing")
		}
		uploads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		suggestion := "{\"summary\":\"Local fixture reset suggestion\",\"action\":{\"type\":\"uia.invoke\",\"text\":\"\"}}"
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0,
			"message": map[string]string{"content": suggestion}, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	profile := aiTestProfile("native-loopback", server.URL)
	realVision := os.Getenv("ASIMI_NATIVE_REAL_VISION") == "1"
	visionName := "loopback"
	externalCalls := 0
	if realVision {
		if os.Getenv("DEEPSEEK_API_KEY") == "" {
			t.Fatal("real vision needs the authorized environment credential")
		}
		profile.ID = "native-real-deepseek"
		profile.Name = "Native real DeepSeek vision"
		profile.BaseURL = "https://api.deepseek.com/v1"
		profile.Models[0].ID = "deepseek-flash"
		profile.Models[0].Pricing = nil
		visionName = "real DeepSeek deepseek-flash"
		externalCalls = 1
	}
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": profile})
	if realVision {
		mustCall(t, s, "StoreCredential", map[string]string{"providerId": profile.ID, "secret": os.Getenv("DEEPSEEK_API_KEY")})
	}
	review := mustCall(t, s, "PrepareScreenUpload", map[string]any{"captureId": capture.CaptureID, "providerId": profile.ID,
		"modelId": profile.Models[0].ID, "imageDetail": "high", "taskId": lastTask, "controlObservationId": o.ID, "controlElementId": target.ID}).(screenReview)
	send := map[string]any{"consentId": review.ConsentID, "content": "Reset the dedicated fixture", "confirmSend": false}
	screenError(t, s, "StartScreenAnalysis", send, "CONFIRM_SEND_REQUIRED")
	send["confirmSend"] = true
	started := mustCall(t, s, "StartScreenAnalysis", send).(map[string]string)
	visionCompleted := false
	until := time.Now().Add(35 * time.Second)
	for time.Now().Before(until) {
		for _, usage := range mustCall(t, s, "GetUsage", struct{}{}).([]ai.UsageRecord) {
			if usage.RequestID == started["requestId"] {
				if usage.Status != "COMPLETED" {
					t.Fatal("vision request failed: " + usage.Status)
				}
				visionCompleted = true
			}
		}
		if visionCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !visionCompleted {
		t.Fatal("vision request did not settle")
	}
	p := mustCall(t, s, "ProposeComputerSuggestion", map[string]any{"taskId": lastTask, "requestId": started["requestId"], "asTask": true}).(computer.Proposal)
	if p.SourceRequestID != started["requestId"] || !p.HarnessTask || (!realVision && uploads.Load() != 1) || (realVision && uploads.Load() != 0) {
		t.Fatal("visual source binding missing")
	}
	r = mustCall(t, s, "ApproveControlAction", controlApproval(p)).(computer.Record)
	final := mustCall(t, s, "GetAgentTask", map[string]string{"id": p.TaskID}).(AgentTask)
	if !r.Verified || final.Status != "COMPLETED" {
		t.Fatal("native visual/UIA task failed")
	}
	evidence := readComputerEvidence(t, s, p.TaskID)
	if len(evidence) != 1 || evidence[0]["requestId"] != started["requestId"] || evidence[0]["verified"] != true {
		t.Fatal("native provenance missing")
	}
	screenError(t, s, "ApproveControlAction", controlApproval(p), "CONTROL_APPROVAL_USED")
	if directory := os.Getenv("ASIMI_LIVE_EVIDENCE_DIR"); realVision && directory != "" {
		receipt := map[string]any{"passed": true, "provider": profile.ID, "model": profile.Models[0].ID, "screenshotBackend": capture.Backend,
			"width": capture.Width, "height": capture.Height, "bytes": capture.ByteSize, "selector": "ResetButton", "sourceRequestId": started["requestId"],
			"taskId": final.ID, "status": final.Status, "verified": r.Verified, "externalProviderCalls": externalCalls, "evidence": evidence}
		body, _ := json.MarshalIndent(receipt, "", "  ")
		if err := os.WriteFile(filepath.Join(directory, "native-desktop-real-vision.json"), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Native %dx%d PNG (%d bytes), reviewed %s vision, UIA reset, verification and Harness completion; external provider calls=%d",
		capture.Width, capture.Height, capture.ByteSize, visionName, externalCalls)
}
