package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"asimi/internal/ai"
	"asimi/internal/configuration"
	"asimi/internal/credential"
	"asimi/internal/domain"
	"asimi/internal/screen"
)

func (s *Service) startChatLocked(in ChatInput, test bool) (map[string]string, error) {
	return s.startChatWithScreenLocked(in, test, nil)
}

func (s *Service) startChatWithScreenLocked(in ChatInput, test bool, upload *screenUpload) (map[string]string, error) {
	if !in.ConfirmSend {
		return nil, codedError("CONFIRM_SEND_REQUIRED")
	}
	if len(in.Content) > 196608 || len(in.AttachmentIDs) > configuration.MaxAttachments || strings.TrimSpace(in.Content) == "" && len(in.AttachmentIDs) == 0 {
		return nil, codedError("INVALID_REQUEST")
	}
	config, err := s.resolvedConfiguration()
	if err != nil {
		return nil, err
	}
	if len(in.AttachmentIDs) > config.Media.Attachments {
		return nil, codedError("ATTACHMENT_LIMIT_EXCEEDED")
	}
	imageLimit := config.Media.ImageMiB << 20
	if upload != nil && upload.metadata.ByteSize > imageLimit {
		return nil, codedError("IMAGE_TOO_LARGE")
	}
	p, e := s.aiProfileLocked(in.ProviderID)
	if e != nil {
		return nil, e
	}
	if !p.Enabled {
		return nil, codedError("PROVIDER_UNAVAILABLE")
	}
	var model ai.Model
	found := false
	for _, m := range p.Models {
		if m.ID == in.ModelID {
			model = m
			found = true
			break
		}
	}
	if !found {
		return nil, codedError("MODEL_NOT_FOUND")
	}
	caps := model.Capabilities
	hasImages := len(in.AttachmentIDs) > 0 || upload != nil
	if hasImages && caps.Vision != ai.Supported {
		if upload != nil {
			return nil, codedError("MODEL_SCREENSHOT_UNSUPPORTED")
		}
		return nil, codedError("MODEL_IMAGE_UNSUPPORTED")
	}
	if caps.Text != ai.Supported || in.Stream && caps.Streaming != ai.Supported {
		return nil, codedError("UNSUPPORTED_CAPABILITY")
	}
	if in.Temperature != nil && (caps.Temperature != ai.Supported || math.IsNaN(*in.Temperature) || math.IsInf(*in.Temperature, 0) || *in.Temperature < 0 || *in.Temperature > 2) {
		return nil, codedError("UNSUPPORTED_CAPABILITY")
	}
	settings := config.Selection
	if in.MaxOutputTokens == 0 {
		in.MaxOutputTokens = settings.MaxOutputTokens
	}
	if in.MaxOutputTokens < 1 || in.MaxOutputTokens > settings.MaxOutputTokens || in.MaxOutputTokens > 131072 {
		return nil, codedError("OUTPUT_LIMIT_EXCEEDED")
	}
	if in.ImageDetail == "" {
		in.ImageDetail = "auto"
	}
	if hasImages {
		allowed := false
		for _, detail := range caps.ImageDetails {
			if detail == in.ImageDetail {
				allowed = true
			}
		}
		if !allowed {
			return nil, codedError("UNSUPPORTED_CAPABILITY")
		}
	}
	adapter, e := ai.NewClient(p)
	if e != nil {
		return nil, e
	}
	c := domain.Conversation{ID: in.ConversationID, Title: strings.TrimSpace(in.Content), CreatedAt: now(), UpdatedAt: now(), Messages: []domain.Message{}}
	create := c.ID == ""
	register := false
	if create {
		c.ID = newID()
	} else {
		c, e = s.conversationLocked(c.ID, true)
		if e != nil {
			return nil, e
		}
		if len(c.Tasks) > 0 {
			return nil, codedError("LEGACY_CONVERSATION")
		}
		var playground, deleted int
		e = s.db.QueryRow("SELECT playground,deleted FROM ai_conversations WHERE conversation_id=?", c.ID).Scan(&playground, &deleted)
		if errors.Is(e, sql.ErrNoRows) {
			if len(c.Messages) > 0 {
				return nil, codedError("LEGACY_CONVERSATION")
			}
			register = true
		} else if e != nil {
			return nil, e
		} else if deleted != 0 || in.Playground != (playground == 1) {
			return nil, codedError("CONVERSATION_MODE_MISMATCH")
		}
	}
	for _, active := range s.chatControls {
		if active.conversationID == c.ID {
			return nil, codedError("REQUEST_ACTIVE")
		}
	}
	lastProvider := ""
	for _, m := range c.Messages {
		if m.ProviderID != "" {
			lastProvider = m.ProviderID
		}
	}
	if lastProvider != "" && lastProvider != p.ID && !in.ConfirmProviderSwitch {
		return nil, codedError("PROVIDER_SWITCH_CONFIRMATION_REQUIRED")
	}
	appSettings, e := s.settingsLocked()
	if e != nil {
		return nil, e
	}
	lang := responseLanguage(in.Content, appSettings)
	req := ai.Request{RequestID: newID(), ConversationID: c.ID, ProviderID: p.ID, ModelID: model.ID, MaxOutputTokens: in.MaxOutputTokens, Stream: in.Stream, ResponseLanguage: lang, Temperature: in.Temperature, Messages: []ai.Message{}, SystemInstruction: "You are Asimi, a text and image assistant. You have no access to tools, files, computer controls, or shell. Respond in " + lang + " unless the user explicitly asks for another language."}
	if upload != nil {
		if upload.suggestion != nil {
			req.SystemInstruction += "\n" + upload.suggestion.instruction()
			req.MaxOutputTokens = min(req.MaxOutputTokens, 2048)
		} else {
			req.SystemInstruction += "\n" + screen.VisionInstruction
		}
	}
	contextBytes := len(req.SystemInstruction)
	imageBytes := 0
	imageCount := 0
	for _, m := range c.Messages {
		if upload != nil && upload.suggestion != nil {
			// The dedicated suggestion uses the current objective and reviewed image.
			break
		}
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if m.Role == "assistant" && m.Status != "COMPLETED" {
			continue
		}
		parts := []ai.Content{}
		if m.Content != "" {
			parts = append(parts, ai.Content{Type: "text", Text: m.Content})
			contextBytes += len(m.Content)
		}
		historicalImages := m.Attachments
		if upload != nil {
			// This consent covers the displayed screenshot and text context.
			historicalImages = nil
		}
		for _, meta := range historicalImages {
			if caps.Vision != ai.Supported {
				return nil, codedError("MODEL_HISTORY_IMAGE_UNSUPPORTED")
			}
			a, e := s.attachmentLocked(meta.ID, true)
			if e != nil {
				return nil, e
			}
			if a.Bytes > int64(imageLimit) {
				return nil, codedError("IMAGE_TOO_LARGE")
			}
			imageBytes += len(a.PreviewDataURL)
			imageCount++
			if imageBytes > 32*1024*1024 || imageCount > 16 {
				return nil, codedError("CONTEXT_LIMIT_EXCEEDED")
			}
			parts = append(parts, ai.Content{Type: "image", DataURL: a.PreviewDataURL, Detail: in.ImageDetail})
		}
		if len(parts) > 0 {
			req.Messages = append(req.Messages, ai.Message{Role: m.Role, Content: parts, Timestamp: m.CreatedAt})
		}
	}
	parts := []ai.Content{}
	if in.Content != "" {
		parts = append(parts, ai.Content{Type: "text", Text: in.Content})
	}
	contextBytes += len(in.Content)
	if upload != nil {
		parts = append(parts, ai.Content{Type: "image", DataURL: upload.dataURL, Detail: in.ImageDetail})
		imageBytes += len(upload.dataURL)
		imageCount++
	}
	attachments := []ai.Attachment{}
	seen := map[string]bool{}
	for _, id := range in.AttachmentIDs {
		if seen[id] {
			return nil, codedError("INVALID_ATTACHMENT")
		}
		seen[id] = true
		a, e := s.attachmentLocked(id, true)
		if e != nil {
			return nil, e
		}
		if a.Bytes > int64(imageLimit) {
			return nil, codedError("IMAGE_TOO_LARGE")
		}
		imageBytes += len(a.PreviewDataURL)
		imageCount++
		if imageBytes > 32*1024*1024 || imageCount > 16 {
			return nil, codedError("CONTEXT_LIMIT_EXCEEDED")
		}
		parts = append(parts, ai.Content{Type: "image", DataURL: a.PreviewDataURL, Detail: in.ImageDetail})
		a.PreviewDataURL = ""
		attachments = append(attachments, a)
	}
	if contextBytes > settings.MaxContextBytes {
		return nil, codedError("CONTEXT_LIMIT_EXCEEDED")
	}
	// UTF-8 byte count is a conservative text token upper bound; never fabricate a tokenizer estimate.
	if caps.ContextLimit != nil && int64(contextBytes+req.MaxOutputTokens) > *caps.ContextLimit {
		return nil, codedError("CONTEXT_LIMIT_EXCEEDED")
	}
	req.Messages = append(req.Messages, ai.Message{Role: "user", Content: parts, Timestamp: now()})
	stamp := time.Now().UTC()
	if !create {
		var last string
		if e := s.db.QueryRow("SELECT COALESCE(MAX(created_at),'') FROM messages WHERE conversation_id=?", c.ID).Scan(&last); e != nil {
			return nil, e
		}
		if parsed, e := time.Parse(time.RFC3339Nano, last); e == nil && !stamp.After(parsed) {
			stamp = parsed.Add(time.Nanosecond)
		}
		var since string
		if e := s.db.QueryRow("SELECT context_since FROM ai_conversations WHERE conversation_id=?", c.ID).Scan(&since); e == nil {
			if parsed, e := time.Parse(time.RFC3339Nano, since); e == nil && !stamp.After(parsed) {
				stamp = parsed.Add(time.Nanosecond)
			}
		}
	}
	user := domain.Message{ID: newID(), ConversationID: c.ID, Role: "user", Content: in.Content, CreatedAt: stamp.Format(chatTimeFormat)}
	assistant := domain.Message{ID: newID(), ConversationID: c.ID, Role: "assistant", CreatedAt: stamp.Add(time.Nanosecond).Format(chatTimeFormat)}
	meta := messageMetadata{ProviderID: p.ID, ModelID: model.ID, RequestID: req.RequestID, Status: "PENDING", Attachments: attachments}
	var suggestion *computerSuggestionScope
	if upload != nil {
		capture := upload.metadata
		capture.ProviderID, capture.ModelID = p.ID, model.ID
		capture.RequestID, capture.ConversationID = req.RequestID, c.ID
		meta.ScreenCapture = &capture
		suggestion = upload.suggestion
	}
	e = s.transaction(func(tx *sql.Tx) error {
		if create {
			title := []rune(c.Title)
			if len(title) > 80 {
				title = title[:80]
			}
			if _, e := tx.Exec("INSERT INTO conversations(id,title,created_at,updated_at) VALUES(?,?,?,?)", c.ID, string(title), c.CreatedAt, c.UpdatedAt); e != nil {
				return e
			}
		}
		if create || register {
			if _, e := tx.Exec("INSERT INTO ai_conversations(conversation_id,playground) VALUES(?,?)", c.ID, in.Playground); e != nil {
				return e
			}
		}
		for _, m := range []domain.Message{user, assistant} {
			if _, e := tx.Exec("INSERT INTO messages(id,conversation_id,task_id,role,content,created_at,updated_at) VALUES(?,?,NULL,?,?,?,?)", m.ID, c.ID, m.Role, m.Content, m.CreatedAt, m.CreatedAt); e != nil {
				return e
			}
		}
		meta.Status = "SENT"
		if _, e := tx.Exec("INSERT INTO ai_message_metadata(message_id,json) VALUES(?,?)", user.ID, encode(meta)); e != nil {
			return e
		}
		meta.Status = "PENDING"
		meta.Attachments = nil
		meta.ScreenCapture = nil
		if _, e := tx.Exec("INSERT INTO ai_message_metadata(message_id,json) VALUES(?,?)", assistant.ID, encode(computerSuggestionMetadata{meta, suggestion})); e != nil {
			return e
		}
		for _, a := range attachments {
			if _, e := tx.Exec("INSERT INTO ai_message_attachments(message_id,attachment_id) VALUES(?,?)", user.ID, a.ID); e != nil {
				return e
			}
		}
		if _, e := tx.Exec("INSERT INTO ai_requests(id,conversation_id,message_id,provider_id,model_id,status,created_at) VALUES(?,?,?,?,?,'PENDING',?)", req.RequestID, c.ID, assistant.ID, p.ID, model.ID, assistant.CreatedAt); e != nil {
			return e
		}
		if upload != nil {
			if e := linkComputerTask(tx, upload.taskID, "", req.RequestID); e != nil {
				return e
			}
		}
		if _, e := tx.Exec("UPDATE conversations SET updated_at=? WHERE id=?", now(), c.ID); e != nil {
			return e
		}
		event := "AI_REQUEST_CONFIRMED"
		params := map[string]string{"requestId": req.RequestID, "providerId": p.ID, "conversationId": c.ID}
		if upload != nil {
			event = "SCREEN_UPLOAD_CONFIRMED"
			params["captureId"] = upload.metadata.CaptureID
			params["sha256"] = upload.metadata.SHA256
		}
		return audit(tx, "", event, params)
	})
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(s.lifetime)
	// A Call is already admitted. Child registration is serialized with shutdown.
	s.admission.Lock()
	if s.lifetime.Err() != nil {
		cancel()
	}
	s.wg.Add(1)
	s.admission.Unlock()
	s.chatControls[req.RequestID] = chatControl{cancel: cancel, providerID: p.ID, conversationID: c.ID}
	go s.runChat(ctx, cancel, adapter, req, assistant, meta, model.Pricing, test, p.HasCredential, upload != nil, suggestion)
	return map[string]string{"requestId": req.RequestID, "messageId": assistant.ID, "conversationId": c.ID}, nil
}
func (s *Service) runChat(ctx context.Context, cancel context.CancelFunc, adapter ai.Adapter, req ai.Request, message domain.Message, meta messageMetadata, pricing *ai.Pricing, test bool, hasCredential bool, screenAnalysis bool, suggestion *computerSuggestionScope) {
	defer s.wg.Done()
	defer cancel()
	if closer, ok := adapter.(interface{ Close() }); ok {
		defer closer.Close()
	}
	sequence := 0
	emit := func(kind, status, delta, code string, response *ai.Response) {
		sequence++
		if s.emit != nil {
			s.emit("assistant.stream."+kind, map[string]any{"conversationId": req.ConversationID, "messageId": message.ID, "requestId": req.RequestID, "sequence": sequence, "contentDelta": delta, "status": status, "errorCode": code, "response": response})
		}
	}
	started := time.Now()
	emit("started", "RUNNING", "", "", nil)
	var response ai.Response
	var requestErr error
	partial := ""
	// Decryption happens only inside the admitted worker immediately before HTTP.
	var secret []byte
	var e error
	if hasCredential {
		secret, e = (credential.Vault{DB: s.db}).GetCredential(req.ProviderID)
	}
	if e != nil {
		requestErr = codedError("CREDENTIAL_DECRYPT_FAILED")
	} else {
		if e = ctx.Err(); e != nil {
			requestErr = e
		} else if test {
			response, requestErr = adapter.TestConnection(ctx, req, secret)
		} else if req.Stream {
			response, requestErr = adapter.Stream(ctx, req, secret, func(d ai.Delta) error {
				if e := ctx.Err(); e != nil {
					return e
				}
				partial += d.Content
				emit("delta", "RUNNING", d.Content, "", nil)
				return nil
			})
		} else {
			response, requestErr = adapter.Generate(ctx, req, secret)
		}
		credential.Wipe(secret)
	}
	response.RequestID = req.RequestID
	response.ProviderID = req.ProviderID
	response.ModelID = req.ModelID
	if response.Content == "" {
		response.Content = partial
	}
	if response.DurationMS == 0 {
		response.DurationMS = time.Since(started).Milliseconds()
	}
	status, kind, code := "COMPLETED", "completed", ""
	if requestErr != nil {
		status, kind = "FAILED", "failed"
		code = chatErrorCode(requestErr)
	}
	if ctx.Err() != nil || errors.Is(requestErr, context.Canceled) {
		status, kind, code = "CANCELLED", "cancelled", "REQUEST_CANCELLED"
		if partial != "" {
			response.Content = partial
		}
	}
	if screenAnalysis {
		// A custom endpoint can echo its input. Keep the known image payload
		// out of persisted replies even when returned as plain text.
		response.Content = removeScreenEcho(response.Content, req.Messages)
	}
	// Drop the worker's request image references before writing results or
	// broadcasting terminal status.
	req.Messages = nil
	response.Status = status
	response.ErrorCode = code
	if !response.Usage.UsageAvailable {
		response.Usage.Source = "UNAVAILABLE"
	}
	meta.Status = status
	meta.ErrorCode = code
	if screenAnalysis && status == "COMPLETED" {
		var vision screen.VisionResult
		if suggestion == nil {
			vision = screen.ParseVision(response.Content)
		} else {
			vision = suggestionVisionResult(response.Content)
		}
		meta.Vision = &vision
	}
	usage := ai.UsageRecord{RequestID: req.RequestID, ConversationID: req.ConversationID, MessageID: message.ID, ProviderID: req.ProviderID, ModelID: req.ModelID, Usage: response.Usage, Pricing: pricing, EstimatedCost: estimateCost(response.Usage, pricing), DurationMS: response.DurationMS, FirstTokenLatencyMS: response.FirstTokenLatencyMS, HTTPStatus: response.HTTPStatus, Status: status, ErrorCode: code, CreatedAt: message.CreatedAt, ProviderRequestID: response.ProviderRequestID}
	s.mu.Lock()
	e = s.transaction(func(tx *sql.Tx) error {
		if _, e := tx.Exec("UPDATE messages SET content=?,updated_at=? WHERE id=?", response.Content, now(), message.ID); e != nil {
			return e
		}
		if _, e := tx.Exec("UPDATE ai_message_metadata SET json=? WHERE message_id=?", encode(computerSuggestionMetadata{meta, suggestion}), message.ID); e != nil {
			return e
		}
		if _, e := tx.Exec("UPDATE ai_requests SET status=? WHERE id=?", status, req.RequestID); e != nil {
			return e
		}
		if _, e := tx.Exec("INSERT INTO ai_usage(request_id,json) VALUES(?,?)", req.RequestID, encode(usage)); e != nil {
			return e
		}
		if _, e := tx.Exec("UPDATE conversations SET updated_at=? WHERE id=?", now(), req.ConversationID); e != nil {
			return e
		}
		return audit(tx, "", "AI_REQUEST_"+status, map[string]string{"requestId": req.RequestID, "errorCode": code})
	})
	delete(s.chatControls, req.RequestID)
	if e != nil {
		if s.workerErr == nil {
			s.workerErr = codedError(errorCode(e))
		}
		s.mu.Unlock()
		emit("failed", "UNKNOWN", "", errorCode(e), nil)
		return
	}
	s.mu.Unlock()
	emit(kind, status, "", code, &response)
	s.changed(req.ConversationID, "")
}
func chatErrorCode(e error) string {
	code := errorCode(e)
	switch code {
	case "PROVIDER_UNAVAILABLE", "INVALID_API_KEY", "MODEL_NOT_FOUND", "UNSUPPORTED_CAPABILITY", "RATE_LIMITED", "REQUEST_TIMEOUT", "NETWORK_ERROR", "TLS_ERROR", "INVALID_RESPONSE", "STREAM_INTERRUPTED", "REQUEST_CANCELLED", "REQUEST_OUTCOME_UNKNOWN", "USAGE_UNAVAILABLE", "UNSUPPORTED_TOOL_CALL", "CREDENTIAL_DECRYPT_FAILED", "PROVIDER_REFUSAL", "RESPONSE_INCOMPLETE", "INVALID_PROFILE", "INVALID_BASE_URL", "INSECURE_ENDPOINT", "INVALID_REQUEST", "INVALID_ATTACHMENT", "CONTEXT_TOO_LARGE":
		return code
	}
	if errors.Is(e, context.DeadlineExceeded) {
		return "REQUEST_TIMEOUT"
	}
	return "PROVIDER_UNAVAILABLE"
}
func estimateCost(u ai.Usage, p *ai.Pricing) *float64 {
	return ai.EstimateCost(u, p)
}

func (s *Service) clearChatLocked(id string, deleteAttachments, deleted bool) (bool, error) {
	for _, c := range s.chatControls {
		if c.conversationID == id {
			return false, codedError("REQUEST_ACTIVE")
		}
	}
	var tasks int
	if e := s.db.QueryRow("SELECT COUNT(*) FROM task_sessions WHERE conversation_id=?", id).Scan(&tasks); e != nil {
		return false, e
	}
	if tasks > 0 {
		return false, codedError("LEGACY_CONVERSATION")
	}
	var oldDeleted int
	if e := s.db.QueryRow("SELECT deleted FROM ai_conversations WHERE conversation_id=?", id).Scan(&oldDeleted); e != nil {
		return false, e
	}
	if oldDeleted != 0 && !deleted {
		return false, codedError("NOT_FOUND")
	}
	// Clear and delete are logical: retain immutable request/usage/audit relations.
	e := s.transaction(func(tx *sql.Tx) error {
		if deleteAttachments {
			if _, e := tx.Exec("INSERT OR IGNORE INTO ai_attachment_cleanup(attachment_id,conversation_id,created_at) SELECT DISTINCT attachment_id,?,? FROM ai_message_attachments WHERE message_id IN (SELECT id FROM messages WHERE conversation_id=?)", id, now(), id); e != nil {
				return e
			}
		}
		if _, e := tx.Exec("UPDATE ai_conversations SET context_since=?,deleted=? WHERE conversation_id=?", time.Now().UTC().Format(chatTimeFormat), deleted, id); e != nil {
			return e
		}
		if _, e := tx.Exec("UPDATE messages SET content='',updated_at=? WHERE conversation_id=? AND task_id IS NULL", now(), id); e != nil {
			return e
		}
		if _, e := tx.Exec("UPDATE conversations SET title='',updated_at=? WHERE id=?", now(), id); e != nil {
			return e
		}
		rows, e := tx.Query("SELECT mm.message_id,mm.json FROM ai_message_metadata mm JOIN messages m ON m.id=mm.message_id WHERE m.conversation_id=?", id)
		if e != nil {
			return e
		}
		type item struct{ id, raw string }
		items := []item{}
		for rows.Next() {
			var v item
			if e = rows.Scan(&v.id, &v.raw); e != nil {
				rows.Close()
				return e
			}
			items = append(items, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, v := range items {
			var metadata computerSuggestionMetadata
			if e = json.Unmarshal([]byte(v.raw), &metadata); e != nil {
				return e
			}
			metadata.Attachments = nil
			metadata.ScreenCapture = nil
			metadata.Vision = nil
			if metadata.ComputerSuggestion != nil && metadata.ComputerSuggestion.ActionID == "" {
				metadata.ComputerSuggestion = nil
			}
			if _, e = tx.Exec("UPDATE ai_message_metadata SET json=? WHERE message_id=?", encode(metadata), v.id); e != nil {
				return e
			}
		}
		if _, e := tx.Exec("DELETE FROM ai_message_attachments WHERE message_id IN (SELECT id FROM messages WHERE conversation_id=?)", id); e != nil {
			return e
		}
		return audit(tx, "", "AI_CONVERSATION_CLEARED", map[string]string{"conversationId": id})
	})
	if e != nil {
		return false, e
	}
	if e = s.retryAttachmentCleanup(id); e != nil {
		return false, e
	}
	s.changed(id, "")
	return true, nil
}

const chatTimeFormat = "2006-01-02T15:04:05.000000000Z"
