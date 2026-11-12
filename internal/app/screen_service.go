package app

import (
	"bytes"
	"encoding/json"
	"image"
	"net/url"
	"strings"
	"time"

	"asimi/internal/ai"
	"asimi/internal/domain"
	"asimi/internal/screen"
)

type screenReview struct {
	ConsentID          string `json:"consentId"`
	ProviderName       string `json:"providerName"`
	ModelID            string `json:"modelId"`
	APIHost            string `json:"apiHost"`
	Bytes              int    `json:"bytes"`
	SHA256             string `json:"sha256"`
	ExpiresAt          string `json:"expiresAt"`
	TaskID             string `json:"taskId,omitempty"`
	SuggestionSelector string `json:"suggestionSelector,omitempty"`
}

type screenConsent struct {
	id, conversationID, detail, modelID string
	taskID                              string
	profile                             ai.Profile
	suggestion                          *computerSuggestionScope
}

type screenUpload struct {
	metadata   screen.Metadata
	dataURL    string
	taskID     string
	suggestion *computerSuggestionScope
}

type screenTarget struct {
	CaptureID            string `json:"captureId"`
	ProviderID           string `json:"providerId"`
	ModelID              string `json:"modelId"`
	ConversationID       string `json:"conversationId"`
	ImageDetail          string `json:"imageDetail"`
	TaskID               string `json:"taskId"`
	ControlObservationID string `json:"controlObservationId"`
	ControlElementID     string `json:"controlElementId"`
}

type screenSend struct {
	ConsentID             string `json:"consentId"`
	Content               string `json:"content"`
	ConfirmSend           bool   `json:"confirmSend"`
	ConfirmProviderSwitch bool   `json:"confirmProviderSwitch"`
	Stream                bool   `json:"stream"`
	MaxOutputTokens       int    `json:"maxOutputTokens"`
}

// SetAcceptanceScreenDriver is a Go-only seam called before test bindings exist.
// Production creates the native driver lazily on the first capture-related call.
func (s *Service) SetAcceptanceScreenDriver(driver screen.Driver) {
	s.screens.mu.Lock()
	defer s.screens.mu.Unlock()
	if !s.screens.closed && s.screens.driver == nil {
		s.screens.driver = driver
		s.screens.backend = "SYNTHETIC_FIXTURE"
	}
}

func (s *Service) callScreen(method, payload string) (domain.Reply, bool) {
	reply := func(value any, err error) (domain.Reply, bool) {
		return result(value, err), true
	}
	switch method {
	case "GetCaptureCapabilities":
		if err := decode(payload, &struct{}{}); err != nil {
			return reply(nil, err)
		}
		return reply(map[string]any{
			"backend": s.screens.backendName(), "display": true, "region": true,
			"window": false, "control": false, "maxPixels": screen.MaxPixels,
			"maxDimension": screen.MaxDimension, "maxBytes": maxScreenBytes,
			"ttlSeconds": int(screenTTL.Seconds()),
		}, nil)
	case "ListDisplays":
		if err := decode(payload, &struct{}{}); err != nil {
			return reply(nil, err)
		}
		driver, err := s.screens.beginWork()
		if err != nil {
			return reply(nil, err)
		}
		defer s.screens.endWork()
		displays, err := driver.EnumerateDisplays(s.lifetime)
		return reply(displays, err)
	case "CaptureScreen":
		var input struct {
			DisplayID      string       `json:"displayId"`
			ConfirmCapture bool         `json:"confirmCapture"`
			Region         *screen.Rect `json:"region"`
		}
		if err := decode(payload, &input); err != nil {
			return reply(nil, err)
		}
		if !input.ConfirmCapture {
			return reply(nil, codedError("SCREEN_CAPTURE_CONSENT_REQUIRED"))
		}
		preview, err := s.captureScreen(input.DisplayID, input.Region)
		return reply(preview, err)
	case "TransformScreenCapture":
		var input struct {
			CaptureID  string        `json:"captureId"`
			Region     *screen.Rect  `json:"region"`
			Redactions []screen.Rect `json:"redactions"`
			Quality    int           `json:"quality"`
		}
		if err := decode(payload, &input); err != nil {
			return reply(nil, err)
		}
		preview, err := s.transformCapture(input.CaptureID, input.Region, input.Redactions, input.Quality)
		return reply(preview, err)
	case "DiscardScreenCapture":
		var input struct {
			CaptureID string `json:"captureId"`
		}
		if err := decode(payload, &input); err != nil {
			return reply(nil, err)
		}
		s.screens.mu.Lock()
		defer s.screens.mu.Unlock()
		return reply(s.screens.removeLocked(input.CaptureID), nil)
	case "PrepareScreenUpload":
		var input screenTarget
		if err := decode(payload, &input); err != nil {
			return reply(nil, err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		review, err := s.prepareScreenUpload(input)
		return reply(review, err)
	case "StartScreenAnalysis":
		var input screenSend
		if err := decode(payload, &input); err != nil {
			return reply(nil, err)
		}
		if !input.ConfirmSend {
			return reply(nil, codedError("CONFIRM_SEND_REQUIRED"))
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		started, err := s.startScreenAnalysis(input)
		return reply(started, err)
	case "ListScreenHistory":
		if err := decode(payload, &struct{}{}); err != nil {
			return reply(nil, err)
		}
		history, err := s.listScreenHistory()
		return reply(history, err)
	default:
		return domain.Reply{}, false
	}
}

func (s *Service) captureScreen(displayID string, selection *screen.Rect) (screenPreview, error) {
	if displayID == "" || len(displayID) > 128 {
		return screenPreview{}, codedError("SCREEN_DISPLAY_NOT_FOUND")
	}
	c := &s.screens
	driver, err := c.beginWork()
	if err != nil {
		return screenPreview{}, err
	}
	defer c.endWork()
	c.mu.Lock()
	c.pruneLocked()
	full := len(c.entries) >= maxScreenCache
	c.mu.Unlock()
	if full {
		return screenPreview{}, codedError("SCREEN_LIMIT_EXCEEDED")
	}
	var display screen.Display
	var im *image.RGBA
	if selection == nil {
		display, im, err = driver.CaptureDisplay(s.lifetime, displayID)
	} else {
		display, im, err = driver.CaptureRegion(s.lifetime, displayID, *selection)
	}
	if err != nil {
		return screenPreview{}, err
	}
	capturedAt := time.Now().UTC()
	if im == nil {
		return screenPreview{}, codedError("SCREEN_CAPTURE_FAILED")
	}
	defer clear(im.Pix)
	r := im.Bounds()
	width, height := r.Dx(), r.Dy()
	if width <= 0 || height <= 0 || width > screen.MaxDimension || height > screen.MaxDimension ||
		width*height > screen.MaxPixels {
		return screenPreview{}, codedError("SCREEN_LIMIT_EXCEEDED")
	}
	selected := screen.Rect{Width: width, Height: height}
	if selection != nil {
		selected = *selection
	}
	region, err := screenRect(selected, display.PhysicalBounds.Width, display.PhysicalBounds.Height)
	if err != nil || width != region.Dx() || height != region.Dy() {
		return screenPreview{}, codedError("SCREEN_CAPTURE_FAILED")
	}
	pixels, format, err := encodeScreen(im, 0)
	if err != nil {
		return screenPreview{}, err
	}
	metadata := screen.Metadata{
		CaptureID: newID(), DisplayID: display.ID, DisplayName: display.Name, DPI: display.DPI,
		CapturedAt: capturedAt.Format(time.RFC3339Nano), Backend: c.backendName(),
	}
	setScreenRegion(&metadata, display, region)
	applyScreenEncoding(&metadata, pixels, format)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.addLocked(&screenEntry{metadata: metadata, pixels: pixels, expiresAt: capturedAt.Add(screenTTL)})
}

func (s *Service) transformCapture(id string, selection *screen.Rect, masks []screen.Rect, quality int) (screenPreview, error) {
	if len(masks) > 32 || (quality != 0 && (quality < 60 || quality > 100)) {
		return screenPreview{}, codedError("INVALID_REQUEST")
	}
	c := &s.screens
	if _, err := c.beginWork(); err != nil {
		return screenPreview{}, err
	}
	defer c.endWork()
	c.mu.Lock()
	c.pruneLocked()
	source := c.entries[id]
	if source == nil {
		c.mu.Unlock()
		return screenPreview{}, codedError("SCREEN_CAPTURE_EXPIRED")
	}
	metadata, expires := source.metadata, source.expiresAt
	data := bytes.Clone(source.pixels)
	c.mu.Unlock()
	defer clear(data)
	r := screen.Rect{Width: metadata.Width, Height: metadata.Height}
	if selection != nil {
		r = *selection
	}
	region, err := screenRect(r, metadata.Width, metadata.Height)
	if err != nil {
		return screenPreview{}, err
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return screenPreview{}, codedError("SCREEN_CAPTURE_FAILED")
	}
	if rgba, ok := im.(*image.RGBA); ok {
		defer clear(rgba.Pix)
	}
	if nrgba, ok := im.(*image.NRGBA); ok {
		defer clear(nrgba.Pix)
	}
	if ycbcr, ok := im.(*image.YCbCr); ok {
		defer func() { clear(ycbcr.Y); clear(ycbcr.Cb); clear(ycbcr.Cr) }()
	}
	transformed, err := transformScreen(im, region, masks)
	if err != nil {
		return screenPreview{}, err
	}
	defer clear(transformed.Pix)
	pixels, format, err := encodeScreen(transformed, quality)
	if err != nil {
		return screenPreview{}, err
	}
	metadata.CaptureID = newID()
	setScreenRegion(&metadata, screen.Display{
		PhysicalBounds: metadata.PhysicalBounds, LogicalBounds: metadata.LogicalBounds, DPI: metadata.DPI,
	}, region)
	applyScreenEncoding(&metadata, pixels, format)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	if c.entries[id] != source {
		clear(pixels)
		return screenPreview{}, codedError("SCREEN_CAPTURE_EXPIRED")
	}
	c.removeLocked(id)
	return c.addLocked(&screenEntry{metadata: metadata, pixels: pixels, expiresAt: expires})
}

func (s *Service) prepareScreenUpload(in screenTarget) (screenReview, error) {
	if in.TaskID != "" {
		if err := validateComputerTask(s.db, in.TaskID); err != nil {
			return screenReview{}, err
		}
	}
	var suggestion *computerSuggestionScope
	if in.ControlObservationID != "" || in.ControlElementID != "" {
		if in.TaskID == "" || len(in.ControlObservationID) != 32 || in.ControlElementID == "" || len(in.ControlElementID) > 128 {
			return screenReview{}, codedError("INVALID_REQUEST")
		}
		control := &s.desktopControl
		control.mu.Lock()
		scope, _, err := control.suggestionTarget(in.ControlObservationID, in.ControlElementID)
		control.mu.Unlock()
		if err != nil {
			return screenReview{}, err
		}
		suggestion = &scope
	}
	p, err := s.aiProfileLocked(in.ProviderID)
	if err != nil {
		return screenReview{}, err
	}
	if !p.Enabled {
		return screenReview{}, codedError("PROVIDER_UNAVAILABLE")
	}
	model := findAIModel(p, in.ModelID)
	if model == nil {
		return screenReview{}, codedError("MODEL_NOT_FOUND")
	}
	if !allowsScreen(model.Capabilities, in.ImageDetail) {
		return screenReview{}, codedError("UNSUPPORTED_CAPABILITY")
	}
	endpoint, err := url.Parse(p.BaseURL)
	if err != nil || endpoint.Host == "" {
		return screenReview{}, codedError("INVALID_BASE_URL")
	}
	config, err := s.resolvedConfiguration()
	if err != nil {
		return screenReview{}, err
	}
	c := &s.screens
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	entry := c.entries[in.CaptureID]
	if entry == nil {
		return screenReview{}, codedError("SCREEN_CAPTURE_EXPIRED")
	}
	if entry.metadata.ByteSize > config.Media.ImageMiB<<20 {
		return screenReview{}, codedError("IMAGE_TOO_LARGE")
	}
	entry.consent = &screenConsent{
		id: newID(), conversationID: in.ConversationID, detail: in.ImageDetail,
		modelID: in.ModelID, profile: p, taskID: in.TaskID, suggestion: suggestion,
	}
	review := screenReview{
		ConsentID: entry.consent.id, ProviderName: p.Name, ModelID: in.ModelID,
		APIHost: endpoint.Host, Bytes: entry.metadata.ByteSize, SHA256: entry.metadata.SHA256,
		ExpiresAt: entry.expiresAt.UTC().Format(time.RFC3339Nano), TaskID: in.TaskID,
	}
	if suggestion != nil {
		review.SuggestionSelector = suggestion.Selector
	}
	return review, nil
}

func findAIModel(p ai.Profile, modelID string) *ai.Model {
	for i := range p.Models {
		if p.Models[i].ID == modelID {
			return &p.Models[i]
		}
	}
	return nil
}

func allowsScreen(caps ai.Capabilities, detail string) bool {
	if caps.Vision != ai.Supported || caps.Text != ai.Supported {
		return false
	}
	for _, allowed := range caps.ImageDetails {
		if allowed == detail {
			return true
		}
	}
	return false
}

func (s *Service) startScreenAnalysis(in screenSend) (map[string]string, error) {
	if in.ConsentID == "" || len(in.ConsentID) != 32 || len(in.Content) > 196608 {
		return nil, codedError("SCREEN_UPLOAD_REVIEW_REQUIRED")
	}
	if s.lifetime.Err() != nil {
		return nil, codedError("SERVICE_CLOSED")
	}
	emptyObjective := strings.TrimSpace(in.Content) == ""
	if emptyObjective {
		settings, err := s.settingsLocked()
		if err != nil {
			return nil, err
		}
		in.Content = "Analyze this screenshot."
		if settings.AppLanguage == "zh-CN" {
			in.Content = "请分析这张截图。"
		}
	}
	upload, consent, err := s.takeReviewedScreen(in.ConsentID)
	if err != nil {
		return nil, err
	}
	if upload.suggestion != nil {
		if emptyObjective || len(in.Content) > 16<<10 {
			return nil, codedError("INVALID_REQUEST")
		}
		control := &s.desktopControl
		control.mu.Lock()
		_, err := control.verifySuggestionScope(*upload.suggestion)
		control.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	chat := ChatInput{
		ConversationID: consent.conversationID, ProviderID: consent.profile.ID,
		ModelID: consent.modelID, Content: in.Content, ImageDetail: consent.detail,
		ConfirmSend: true, ConfirmProviderSwitch: in.ConfirmProviderSwitch,
		Stream: in.Stream, MaxOutputTokens: in.MaxOutputTokens,
	}
	return s.startChatWithScreenLocked(chat, false, upload)
}

func (s *Service) takeReviewedScreen(id string) (*screenUpload, screenConsent, error) {
	c := &s.screens
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	for captureID, entry := range c.entries {
		if entry.consent == nil || entry.consent.id != id {
			continue
		}
		consent := *entry.consent
		entry.consent = nil
		current, err := s.aiProfileLocked(consent.profile.ID)
		if err != nil || encode(current) != encode(consent.profile) {
			return nil, screenConsent{}, codedError("SCREEN_UPLOAD_TARGET_CHANGED")
		}
		upload := &screenUpload{metadata: entry.metadata, dataURL: entry.dataURL(), taskID: consent.taskID, suggestion: consent.suggestion}
		c.removeLocked(captureID)
		return upload, consent, nil
	}
	return nil, screenConsent{}, codedError("SCREEN_UPLOAD_REVIEW_REQUIRED")
}

func (s *Service) listScreenHistory() ([]screen.Metadata, error) {
	rows, err := s.db.Query(`SELECT json_extract(metadata.json,'$.screenCapture')
FROM ai_message_metadata AS metadata
JOIN messages ON messages.id=metadata.message_id
WHERE json_type(metadata.json,'$.screenCapture')='object'
ORDER BY messages.created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	history := []screen.Metadata{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var metadata screen.Metadata
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
			return nil, err
		}
		history = append(history, metadata)
	}
	return history, rows.Err()
}

func removeScreenEcho(content string, messages []ai.Message) string {
	for _, message := range messages {
		for _, part := range message.Content {
			if part.Type != "image" || part.DataURL == "" {
				continue
			}
			content = strings.ReplaceAll(content, part.DataURL, "[image omitted]")
			if _, base64, ok := strings.Cut(part.DataURL, ","); ok && base64 != "" {
				content = strings.ReplaceAll(content, base64, "[image omitted]")
			}
		}
	}
	return content
}
