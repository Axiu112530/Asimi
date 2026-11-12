// Package providerprobe runs the explicitly requested live provider acceptance.
package providerprobe

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"os"
	"strings"
	"time"

	"asimi/internal/ai"
)

const (
	officialBaseURL = "https://api.deepseek.com"
	modelID         = "deepseek-flash"
	profileID       = "acceptance-deepseek"
	requestTimeout  = 25 * time.Second
)

type Report struct {
	Provider      string     `json:"provider"`
	BaseURL       string     `json:"baseUrl"`
	Model         string     `json:"model"`
	Status        string     `json:"status"`
	Passed        bool       `json:"passed"`
	ErrorCode     string     `json:"errorCode,omitempty"`
	StartedAt     string     `json:"startedAt"`
	ExternalCalls int        `json:"externalCalls"`
	Pricing       ai.Pricing `json:"peakRateSnapshot"`
	PricingSource string     `json:"pricingSource"`
	Results       []Result   `json:"results"`
}

type Result struct {
	Name                string          `json:"name"`
	Protocol            string          `json:"protocol"`
	MaxOutputTokens     int             `json:"maxOutputTokens"`
	HTTPStatus          int             `json:"httpStatus"`
	Status              string          `json:"status"`
	FinishReason        string          `json:"finishReason,omitempty"`
	ErrorCode           string          `json:"errorCode,omitempty"`
	ProviderRequestID   string          `json:"providerRequestId,omitempty"`
	DurationMS          int64           `json:"durationMs"`
	FirstTokenLatencyMS *int64          `json:"firstTokenLatencyMs,omitempty"`
	DeltaCount          int             `json:"deltaCount"`
	Usage               ai.Usage        `json:"usage"`
	EstimatedPeakUSD    *float64        `json:"estimatedPeakUsd"`
	Checks              map[string]bool `json:"checks"`
}

// Run sends up to four requests using the product adapter, without opening a
// runtime, profile database, attachment directory, or desktop capture.
func Run(ctx context.Context) (Report, error) {
	peakCached := 0.006
	report := Report{
		Provider: "DeepSeek", BaseURL: officialBaseURL, Model: modelID,
		Status: "FAILED", StartedAt: time.Now().UTC().Format(time.RFC3339),
		Pricing: ai.Pricing{
			InputPerMillion: 0.30, OutputPerMillion: 1.20, CachedInputPerMillion: &peakCached,
			Currency: "USD", EffectiveAt: "2026-11-11",
		},
		PricingSource: "https://api-docs.deepseek.com/quick_start/pricing/",
		Results:       make([]Result, 0, 4),
	}
	value := os.Getenv("DEEPSEEK_API_KEY")
	if strings.TrimSpace(value) == "" {
		report.Status = "PENDING"
		return reportFailure(report, "CREDENTIAL_MISSING")
	}
	key := []byte(value)
	defer clear(key)

	chat, err := client(ai.ChatCompletions)
	if err != nil {
		return reportFailure(report, "INVALID_PROFILE")
	}
	defer chat.Close()
	responses, err := client(ai.Responses)
	if err != nil {
		return reportFailure(report, "INVALID_PROFILE")
	}
	defer responses.Close()
	imagePart, err := syntheticImage()
	if err != nil {
		return reportFailure(report, "SYNTHETIC_IMAGE_FAILED")
	}

	first := request("simple-text", 48, message("user", "Remember the fictional code MAPLE-47. Reply with exactly READY and nothing else."))
	report.ExternalCalls++
	firstCtx, cancelFirst := context.WithTimeout(ctx, requestTimeout)
	firstReply, err := chat.Generate(firstCtx, first, key)
	cancelFirst()
	simple := completedResult(first, ai.ChatCompletions, firstReply, 0, err, &report.Pricing)
	firstText := strings.TrimSpace(firstReply.Content)
	simple.Checks["expectedText"] = firstText == "READY"
	report.Results = append(report.Results, simple)
	if firstReply.HTTPStatus != http.StatusOK {
		return reportFailure(report, "PROVIDER_UNREACHABLE")
	}
	if err != nil || !simple.Checks["expectedText"] {
		return reportFailure(report, "PROVIDER_CHECK_FAILED")
	}

	followUp := request("multi-turn-json-sse", 64, first.Messages[0],
		message("assistant", firstText),
		message("user", `Return only a JSON object whose code field contains the fictional code from my first message. Use this format: {"code":"example"}.`))
	followUp.JSONOutput = true
	report.ExternalCalls++
	streamCtx, cancelStream := context.WithTimeout(ctx, requestTimeout)
	var assembled strings.Builder
	deltas := 0
	multiReply, err := chat.Stream(streamCtx, followUp, key, func(d ai.Delta) error {
		deltas++
		assembled.WriteString(d.Content)
		return nil
	})
	cancelStream()
	multi := completedResult(followUp, ai.ChatCompletions, multiReply, deltas, err, &report.Pricing)
	var parsed map[string]string
	multi.Checks["conversationHistoryInJSON"] = json.Unmarshal([]byte(multiReply.Content), &parsed) == nil &&
		len(parsed) == 1 && parsed["code"] == "MAPLE-47"
	multi.Checks["streamAssembly"] = deltas > 0 && multiReply.FirstTokenLatencyMS != nil &&
		assembled.String() == multiReply.Content
	report.Results = append(report.Results, multi)

	visionPrompt := message("user", "Name the solid fill color on the left half of this image, then the color on the right half. Reply with the two English color names separated by a comma.")
	visionPrompt.Content = append(visionPrompt.Content, imagePart)
	visionRequest := request("synthetic-vision", 64, visionPrompt)
	report.ExternalCalls++
	visionCtx, cancelVision := context.WithTimeout(ctx, requestTimeout)
	visionReply, err := responses.Generate(visionCtx, visionRequest, key)
	cancelVision()
	vision := completedResult(visionRequest, ai.Responses, visionReply, 0, err, &report.Pricing)
	visionText := strings.ToLower(visionReply.Content)
	red, blue := strings.Index(visionText, "red"), strings.Index(visionText, "blue")
	vision.Checks["observedColorsInOrder"] = red >= 0 && blue > red
	report.Results = append(report.Results, vision)

	cancelRequest := request("cancel-active-sse", 160,
		message("user", "Write the English names of the integers from one through one hundred, separated by commas."))
	report.ExternalCalls++
	cancelCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	deltas = 0
	cancelReply, err := chat.Stream(cancelCtx, cancelRequest, key, func(d ai.Delta) error {
		deltas++
		cancel()
		return nil
	})
	cancel()
	canceled := result(cancelRequest, ai.ChatCompletions, cancelReply, deltas, &report.Pricing)
	canceled.Checks = map[string]bool{
		"http200":                cancelReply.HTTPStatus == http.StatusOK,
		"cancelAfterFirstDelta":  deltas == 1 && cancelReply.FirstTokenLatencyMS != nil && cancelReply.Content != "",
		"cancellationClassified": errors.Is(err, ai.Error("REQUEST_CANCELLED")) && cancelReply.Status == "CANCELLED",
		"unknownCostStaysNull":   cancelReply.Usage.UsageAvailable || canceled.EstimatedPeakUSD == nil,
	}
	report.Results = append(report.Results, canceled)

	for _, step := range report.Results {
		for _, passed := range step.Checks {
			if !passed {
				return reportFailure(report, "PROVIDER_CHECK_FAILED")
			}
		}
	}
	report.Status, report.Passed = "PASSED", true
	return report, nil
}

func reportFailure(report Report, code string) (Report, error) {
	report.ErrorCode = code
	return report, errors.New(code)
}

func client(protocol string) (*ai.Client, error) {
	return ai.NewClient(ai.Profile{
		ID: profileID, Name: "DeepSeek acceptance", Protocol: protocol, BaseURL: officialBaseURL, Enabled: true,
		Models: []ai.Model{{ID: modelID, Capabilities: ai.Capabilities{
			Text: ai.Supported, Vision: ai.Supported, Streaming: ai.Supported,
			StructuredOutput: ai.Supported, ImageDetails: []string{"low"},
		}}},
	})
}

func request(id string, maxTokens int, messages ...ai.Message) ai.Request {
	return ai.Request{RequestID: id, ProviderID: profileID, ModelID: modelID, Messages: messages, MaxOutputTokens: maxTokens}
}

func message(role, text string) ai.Message {
	return ai.Message{Role: role, Content: []ai.Content{{Type: "text", Text: text}}}
}

func result(req ai.Request, protocol string, reply ai.Response, deltas int, pricing *ai.Pricing) Result {
	return Result{
		Name: req.RequestID, Protocol: protocol, MaxOutputTokens: req.MaxOutputTokens,
		HTTPStatus: reply.HTTPStatus, Status: reply.Status, FinishReason: reply.FinishReason,
		ErrorCode: reply.ErrorCode, ProviderRequestID: reply.ProviderRequestID, DurationMS: reply.DurationMS,
		FirstTokenLatencyMS: reply.FirstTokenLatencyMS, DeltaCount: deltas, Usage: reply.Usage,
		EstimatedPeakUSD: ai.EstimateCost(reply.Usage, pricing),
	}
}

func completedResult(req ai.Request, protocol string, reply ai.Response, deltas int, err error, pricing *ai.Pricing) Result {
	step := result(req, protocol, reply, deltas, pricing)
	u := reply.Usage
	step.Checks = map[string]bool{
		"http200":           reply.HTTPStatus == http.StatusOK,
		"completed":         err == nil && reply.Status == "COMPLETED" && reply.FinishReason == "stop",
		"providerRequestId": reply.ProviderRequestID != "",
		"usageReconciles": u.UsageAvailable && u.InputTokens != nil && u.OutputTokens != nil && u.TotalTokens != nil &&
			*u.InputTokens+*u.OutputTokens == *u.TotalTokens,
		"estimatedPeakCost": step.EstimatedPeakUSD != nil && *step.EstimatedPeakUSD > 0,
	}
	return step
}

func syntheticImage() (ai.Content, error) {
	im := image.NewRGBA(image.Rect(0, 0, 256, 192))
	draw.Draw(im, image.Rect(0, 0, 128, 192), image.NewUniform(color.RGBA{R: 225, A: 255}), image.Point{}, draw.Src)
	draw.Draw(im, image.Rect(128, 0, 256, 192), image.NewUniform(color.RGBA{B: 220, A: 255}), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		return ai.Content{}, err
	}
	return ai.Content{Type: "image", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), Detail: "low"}, nil
}
