// The explicit Phase 2C smoke test sends only fixed text and generated pixels.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"time"

	"asimi/internal/ai"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type evidence struct {
	Test              string          `json:"test"`
	Provider          string          `json:"provider"`
	Model             string          `json:"model"`
	MaxOutputTokens   int             `json:"maxOutputTokens"`
	Status            string          `json:"testResult"`
	HTTPStatus        int             `json:"httpStatus"`
	RequestID         string          `json:"requestId,omitempty"`
	ProviderRequestID string          `json:"providerRequestId,omitempty"`
	DurationMS        int64           `json:"durationMs"`
	FirstTokenMS      *int64          `json:"firstTokenLatencyMs"`
	Usage             ai.Usage        `json:"usage"`
	DeltaCount        int             `json:"deltaCount"`
	Checks            map[string]bool `json:"checks,omitempty"`
	ErrorCode         string          `json:"errorCode,omitempty"`
	Cost              *float64        `json:"actualCost"`
}

func main() {
	live := flag.Bool("live", false, "send two fixed requests to the official DeepSeek endpoint")
	flag.Parse()
	encoder := json.NewEncoder(os.Stdout)
	if !*live {
		_ = encoder.Encode(map[string]string{"testResult": "PENDING", "reason": "LIVE_FLAG_REQUIRED"})
		return
	}
	value, exists := os.LookupEnv("DEEPSEEK_API_KEY")
	if !exists || strings.TrimSpace(value) == "" {
		_ = encoder.Encode(map[string]string{"testResult": "PENDING", "reason": "CREDENTIAL_MISSING"})
		return
	}
	key := []byte(value)
	defer clear(key)
	value = ""
	profile := ai.Profile{
		ID: "deepseek", Name: "DeepSeek", Protocol: ai.ChatCompletions,
		BaseURL: "https://api.deepseek.com", Enabled: true,
		Models: []ai.Model{{ID: "deepseek-flash", Capabilities: ai.Capabilities{
			Text: ai.Supported, Vision: ai.Supported, Streaming: ai.Supported,
			ImageDetails: []string{"high"},
		}}},
	}
	client, err := ai.NewClient(profile)
	if err != nil {
		_ = encoder.Encode(map[string]string{"testResult": "FAIL", "reason": "INVALID_PROFILE"})
		return
	}
	defer client.Close()
	parts := []ai.Content{{Type: "text", Text: "Reply with exactly ASIMI_STREAM_23."}}
	run := func(name string, limit int, parts []ai.Content, validate func(string) map[string]bool) {
		var id [16]byte
		rand.Read(id[:])
		req := ai.Request{RequestID: hex.EncodeToString(id[:]), ProviderID: profile.ID, ModelID: "deepseek-flash", MaxOutputTokens: limit, Messages: []ai.Message{{Role: "user", Content: parts}}}
		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		defer cancel()
		count := 0
		resp, _ := client.Stream(ctx, req, key, func(d ai.Delta) error { count++; return nil })
		checks := validate(resp.Content)
		checks["http200"] = resp.HTTPStatus == 200
		checks["sseCompleted"] = resp.Status == "COMPLETED" && resp.FinishReason == "stop" && count > 0
		checks["providerRequestId"] = resp.ProviderRequestID != ""
		checks["usageAvailable"] = resp.Usage.UsageAvailable && resp.Usage.InputTokens != nil && resp.Usage.OutputTokens != nil && resp.Usage.TotalTokens != nil
		status := "PASS"
		for _, ok := range checks {
			if !ok {
				status = "FAIL"
				break
			}
		}
		// Log objective checks and metrics only, never prompts, image bytes or raw responses.
		_ = encoder.Encode(evidence{Test: name, Provider: profile.Name, Model: req.ModelID, MaxOutputTokens: limit, Status: status, HTTPStatus: resp.HTTPStatus, RequestID: req.RequestID, ProviderRequestID: resp.ProviderRequestID, DurationMS: resp.DurationMS, FirstTokenMS: resp.FirstTokenLatencyMS, Usage: resp.Usage, DeltaCount: count, Checks: checks, ErrorCode: resp.ErrorCode})
	}
	run("text", 128, parts, func(s string) map[string]bool {
		return map[string]bool{"expectedText": strings.TrimSpace(s) == "ASIMI_STREAM_23"}
	})
	data, err := syntheticImage()
	if err != nil {
		_ = encoder.Encode(map[string]string{"test": "image", "testResult": "FAIL", "reason": "SYNTHETIC_IMAGE_FAILED"})
		return
	}
	parts = []ai.Content{
		{Type: "text", Text: "Read the full printed heading including the number. Name the color and shape on the left, then on the right. Use one concise English line."},
		{Type: "image", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), Detail: "high"},
	}
	run("image", 256, parts, func(s string) map[string]bool {
		s = strings.ToLower(s)
		return map[string]bool{"heading": strings.Contains(s, "asimi vision 47"), "redCircle": strings.Contains(s, "red") && strings.Contains(s, "circle"), "blueTriangle": strings.Contains(s, "blue") && strings.Contains(s, "triangle")}
	})
}

func syntheticImage() ([]byte, error) {
	small := image.NewRGBA(image.Rect(0, 0, 144, 24))
	draw.Draw(small, small.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	d := font.Drawer{Dst: small, Src: image.NewUniform(color.Black), Face: basicfont.Face7x13, Dot: fixed.P(5, 17)}
	d.DrawString("ASIMI VISION 47")
	im := image.NewRGBA(image.Rect(0, 0, 720, 400))
	draw.Draw(im, im.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for y := 0; y < 120; y++ {
		for x := 0; x < 720; x++ {
			im.Set(x, y, small.At(x/5, y/5))
		}
	}
	for y := 140; y < 375; y++ {
		for x := 50; x < 670; x++ {
			if (x-186)*(x-186)+(y-255)*(y-255) <= 94*94 {
				im.Set(x, y, color.RGBA{R: 225, A: 255})
			}
			if y >= 151 && y <= 353 && abs(x-530)*2 <= y-151 {
				im.Set(x, y, color.RGBA{B: 220, A: 255})
			}
		}
	}
	var buffer bytes.Buffer
	err := png.Encode(&buffer, im)
	return buffer.Bytes(), err
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
