package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"deskpilot/internal/ai"
	"deskpilot/internal/domain"
	"deskpilot/internal/screen"
)

type fakeScreen struct{ captures atomic.Int32 }

func (f *fakeScreen) EnumerateDisplays(context.Context) ([]screen.Display, error) {
	return []screen.Display{{
		ID: "synthetic", Name: "Synthetic",
		Primary: true, DPI: 144, Scale: 150,
		PhysicalBounds: screen.Rect{X: -40, Width: 40, Height: 24},
		LogicalBounds:  screen.Rect{X: -27, Width: 27, Height: 16},
	}}, nil
}
func (f *fakeScreen) CaptureDisplay(ctx context.Context, id string) (screen.Display, *image.RGBA, error) {
	return f.CaptureRegion(ctx, id, screen.Rect{Width: 40, Height: 24})
}
func (f *fakeScreen) CaptureRegion(ctx context.Context, _ string, r screen.Rect) (screen.Display, *image.RGBA, error) {
	f.captures.Add(1)
	displays, _ := f.EnumerateDisplays(ctx)
	im := image.NewRGBA(image.Rect(0, 0, r.Width, r.Height))
	for y := 0; y < r.Height; y++ {
		for x := 0; x < r.Width; x++ {
			im.SetRGBA(x, y, color.RGBA{R: 220, G: uint8(x), B: uint8(y), A: 255})
		}
	}
	return displays[0], im, nil
}

func TestScreenRequiresUserCaptureAndOneShotReviewedUpload(t *testing.T) {
	f := &fakeScreen{}
	s := aiFixture(t, nil)
	s.SetAcceptanceScreenDriver(f)
	var requests atomic.Int32
	var outboundPixels string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		n := requests.Add(1)
		imgCount := 0
		for _, m := range body.Messages {
			var parts []struct {
				Image *struct {
					URL string `json:"url"`
				} `json:"image_url"`
			}
			if json.Unmarshal(m.Content, &parts) != nil {
				continue
			}
			for _, part := range parts {
				if part.Image == nil {
					continue
				}
				imgCount++
				if m.Role != "user" || part.Image.URL != outboundPixels {
					t.Error("request did not contain the exact confirmed preview")
				}
			}
		}
		if (n == 1 && imgCount != 1) || (n > 1 && imgCount != 0) {
			t.Errorf("request %d has %d images", n, imgCount)
		}
		w.Header().Set("Content-Type", "application/json")
		content := `{"summary":"A fixture dialog","detectedText":["Synthetic only"],"suggestions":["Check the label."]}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"index": 0, "message": map[string]string{"content": content}, "finish_reason": "stop",
		}}, "usage": map[string]int{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5}})
	}))
	defer server.Close()
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": aiTestProfile("vision", server.URL)})
	mustCall(t, s, "ListDisplays", struct{}{})
	if f.captures.Load() != 0 || requests.Load() != 0 {
		t.Fatal("enumeration caused capture or network")
	}
	screenError(t, s, "CaptureScreen", map[string]any{"displayId": "synthetic"}, "SCREEN_CAPTURE_CONSENT_REQUIRED")
	screenError(t, s, "StartScreenAnalysis", map[string]any{"consentId": "fake", "confirmSend": true}, "SCREEN_UPLOAD_REVIEW_REQUIRED")
	preview := mustCall(t, s, "CaptureScreen", map[string]any{"displayId": "synthetic", "confirmCapture": true}).(screenPreview)
	review := func(id string) screenReview {
		return mustCall(t, s, "PrepareScreenUpload", map[string]any{"captureId": id, "providerId": "vision", "modelId": "fixture", "imageDetail": "high"}).(screenReview)
	}
	firstReview := review(preview.CaptureID)
	cropped := mustCall(t, s, "TransformScreenCapture", map[string]any{
		"captureId": preview.CaptureID, "region": screen.Rect{X: 5, Y: 2, Width: 20, Height: 10},
		"redactions": []screen.Rect{{X: 1, Y: 1, Width: 3, Height: 3}},
	}).(screenPreview)
	if cropped.Width != 20 || cropped.Height != 10 || cropped.PhysicalBounds.X != -35 || cropped.SHA256 == preview.SHA256 {
		t.Fatalf("wrong crop metadata: %+v", cropped.Metadata)
	}
	outboundPixels = cropped.PreviewDataURL
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(outboundPixels, "data:image/png;base64,"))
	pixels, e := png.Decode(bytes.NewReader(raw))
	if e != nil || pixels.At(2, 2) != (color.RGBA{A: 255}) {
		t.Fatal("redaction absent")
	}
	screenError(t, s, "StartScreenAnalysis", map[string]any{"consentId": firstReview.ConsentID, "confirmSend": true}, "SCREEN_UPLOAD_REVIEW_REQUIRED")
	approval := review(cropped.CaptureID)
	input := map[string]any{"consentId": approval.ConsentID, "content": "What is visible?", "confirmSend": false}
	screenError(t, s, "StartScreenAnalysis", input, "CONFIRM_SEND_REQUIRED")
	if requests.Load() != 0 {
		t.Fatal("preview and review dispatched a request")
	}
	input["confirmSend"] = true
	reply := mustCall(t, s, "StartScreenAnalysis", input).(map[string]string)
	if u := awaitAI(t, s, reply["requestId"]); u.Status != "COMPLETED" || u.Usage.TotalTokens == nil || *u.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected usage %+v", u)
	}
	screenError(t, s, "StartScreenAnalysis", input, "SCREEN_UPLOAD_REVIEW_REQUIRED")
	c := mustCall(t, s, "GetConversation", map[string]any{"id": reply["conversationId"]}).(domain.Conversation)
	if len(c.Messages) != 2 || c.Messages[0].ScreenCapture == nil || c.Messages[0].ScreenCapture.SHA256 != cropped.SHA256 || c.Messages[1].Vision == nil || !c.Messages[1].Vision.Parsed || len(c.Messages[0].Attachments) != 0 {
		t.Fatalf("screen metadata/result not restored: %+v", c.Messages)
	}
	history := mustCall(t, s, "ListScreenHistory", struct{}{}).([]screen.Metadata)
	if len(history) != 1 || history[0].RequestID != reply["requestId"] {
		t.Fatalf("unexpected history: %+v", history)
	}
	next := mustCall(t, s, "StartChat", ChatInput{ConversationID: c.ID, ProviderID: "vision", ModelID: "fixture", Content: "Follow up", ConfirmSend: true}).(map[string]string)
	if u := awaitAI(t, s, next["requestId"]); u.Status != "COMPLETED" || requests.Load() != 2 {
		t.Fatalf("followup failed %+v", u)
	}
	for _, name := range []string{"deskpilot.db", "deskpilot.db-wal"} {
		data, e := os.ReadFile(filepath.Join(s.dataDir, name))
		if e != nil && !os.IsNotExist(e) {
			t.Fatal(e)
		}
		if bytes.Contains(data, []byte(outboundPixels)) || bytes.Contains(data, raw) || bytes.Contains(data, []byte("data:image/")) {
			t.Fatalf("screenshot pixels leaked to %s", name)
		}
	}
}

func TestScreenExpiryDiscardTargetChangeAndShutdown(t *testing.T) {
	s := aiFixture(t, nil)
	f := &fakeScreen{}
	s.SetAcceptanceScreenDriver(f)
	profile := aiTestProfile("vision", "http://127.0.0.1:1")
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": profile})
	capture := func() screenPreview {
		return mustCall(t, s, "CaptureScreen", map[string]any{"displayId": "synthetic", "confirmCapture": true}).(screenPreview)
	}
	review := func(id string) map[string]any {
		return map[string]any{"captureId": id, "providerId": "vision", "modelId": "fixture", "imageDetail": "high"}
	}
	p := capture()
	s.screens.mu.Lock()
	s.screens.entries[p.CaptureID].expiresAt = time.Now().Add(-time.Second)
	s.screens.mu.Unlock()
	screenError(t, s, "PrepareScreenUpload", review(p.CaptureID), "SCREEN_CAPTURE_EXPIRED")
	p = capture()
	mustCall(t, s, "DiscardScreenCapture", map[string]any{"captureId": p.CaptureID})
	screenError(t, s, "PrepareScreenUpload", review(p.CaptureID), "SCREEN_CAPTURE_EXPIRED")
	p = capture()
	approval := mustCall(t, s, "PrepareScreenUpload", review(p.CaptureID)).(screenReview)
	profile.BaseURL = "http://127.0.0.1:2"
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": profile})
	screenError(t, s, "StartScreenAnalysis", map[string]any{"consentId": approval.ConsentID, "content": "test", "confirmSend": true}, "SCREEN_UPLOAD_TARGET_CHANGED")
	s.BeginShutdown()
	s.screens.mu.Lock()
	defer s.screens.mu.Unlock()
	if len(s.screens.entries) != 0 {
		t.Fatal("shutdown retained captures")
	}
}

func TestScreenEchoDoesNotRetainOutboundPixels(t *testing.T) {
	const payload = "data:image/png;base64,c2NyZWVuLWZpeHR1cmU="
	messages := []ai.Message{{Role: "user", Content: []ai.Content{{Type: "image", DataURL: payload}}}}
	actual := removeScreenEcho("Visible text "+payload+" and c2NyZWVuLWZpeHR1cmU=", messages)
	if actual != "Visible text [image omitted] and [image omitted]" {
		t.Fatal("provider echo leaked image data")
	}
}

func screenError(t *testing.T, s *Service, method string, input any, code string) {
	t.Helper()
	r := s.Call(method, encode(input))
	if r.Error == nil || r.Error.Code != code {
		t.Fatalf("%s = %+v, want %s", method, r, code)
	}
}
