package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"unicode"

	"asimi/internal/ai"
	"asimi/internal/credential"
	"asimi/internal/domain"
	"asimi/internal/screen"
)

type chatControl struct {
	cancel                     context.CancelFunc
	providerID, conversationID string
}
type ChatInput struct {
	ConversationID        string   `json:"conversationId"`
	ProviderID            string   `json:"providerId"`
	ModelID               string   `json:"modelId"`
	Content               string   `json:"content"`
	AttachmentIDs         []string `json:"attachmentIds"`
	Stream                bool     `json:"stream"`
	MaxOutputTokens       int      `json:"maxOutputTokens"`
	Temperature           *float64 `json:"temperature"`
	ImageDetail           string   `json:"imageDetail"`
	ConfirmSend           bool     `json:"confirmSend"`
	ConfirmProviderSwitch bool     `json:"confirmProviderSwitch"`
	Playground            bool     `json:"playground"`
}
type messageMetadata struct {
	ProviderID    string               `json:"providerId"`
	ModelID       string               `json:"modelId"`
	RequestID     string               `json:"requestId"`
	Status        string               `json:"status"`
	ErrorCode     string               `json:"errorCode,omitempty"`
	Attachments   []ai.Attachment      `json:"attachments,omitempty"`
	Usage         *ai.Usage            `json:"usage,omitempty"`
	ScreenCapture *screen.Metadata     `json:"screenCapture,omitempty"`
	Vision        *screen.VisionResult `json:"vision,omitempty"`
}

func (s *Service) initializeAI() error {
	if e := s.reconcileAttachments(); e != nil {
		return e
	}
	if e := s.retryAttachmentCleanup(""); e != nil {
		return e
	}
	return s.transaction(func(tx *sql.Tx) error {
		if _, e := tx.Exec("INSERT OR IGNORE INTO ai_settings(id,json) VALUES('ai','{}')"); e != nil {
			return e
		}
		rows, e := tx.Query("SELECT id,conversation_id,message_id,provider_id,model_id,created_at FROM ai_requests WHERE status IN ('PENDING','RUNNING')")
		if e != nil {
			return e
		}
		pending := []ai.UsageRecord{}
		for rows.Next() {
			var r ai.UsageRecord
			if e = rows.Scan(&r.RequestID, &r.ConversationID, &r.MessageID, &r.ProviderID, &r.ModelID, &r.CreatedAt); e != nil {
				rows.Close()
				return e
			}
			pending = append(pending, r)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, r := range pending {
			r.Status = "UNKNOWN"
			r.ErrorCode = "REQUEST_OUTCOME_UNKNOWN"
			r.Usage = ai.Usage{Source: "UNAVAILABLE"}
			if _, e = tx.Exec("UPDATE ai_requests SET status='UNKNOWN' WHERE id=?", r.RequestID); e != nil {
				return e
			}
			if _, e = tx.Exec("INSERT OR REPLACE INTO ai_usage(request_id,json) VALUES(?,?)", r.RequestID, encode(r)); e != nil {
				return e
			}
			var raw string
			var m messageMetadata
			if e = tx.QueryRow("SELECT json FROM ai_message_metadata WHERE message_id=?", r.MessageID).Scan(&raw); e != nil {
				return e
			}
			if e = json.Unmarshal([]byte(raw), &m); e != nil {
				return e
			}
			m.Status = r.Status
			m.ErrorCode = r.ErrorCode
			m.Usage = nil
			if _, e = tx.Exec("UPDATE ai_message_metadata SET json=? WHERE message_id=?", encode(m), r.MessageID); e != nil {
				return e
			}
		}
		return nil
	})
}
func (s *Service) enrichMessagesLocked(messages []domain.Message) error {
	for i := range messages {
		var raw string
		var usage sql.NullString
		e := s.db.QueryRow(`SELECT meta.json, json_extract(usage.json,'$.usage')
FROM ai_message_metadata meta
LEFT JOIN ai_requests req ON req.id=json_extract(meta.json,'$.requestId') AND req.message_id=meta.message_id
LEFT JOIN ai_usage usage ON usage.request_id=req.id
WHERE meta.message_id=?`, messages[i].ID).Scan(&raw, &usage)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return e
		}
		var m messageMetadata
		if e = json.Unmarshal([]byte(raw), &m); e != nil {
			return e
		}
		messages[i].ProviderID = m.ProviderID
		messages[i].ModelID = m.ModelID
		messages[i].RequestID = m.RequestID
		messages[i].Status = m.Status
		messages[i].ErrorCode = m.ErrorCode
		messages[i].Attachments = m.Attachments
		if usage.Valid {
			if e = json.Unmarshal([]byte(usage.String), &m.Usage); e != nil {
				return e
			}
		}
		messages[i].Usage = m.Usage
		messages[i].ScreenCapture = m.ScreenCapture
		messages[i].Vision = m.Vision
	}
	return nil
}
func (s *Service) aiProfileLocked(id string) (ai.Profile, error) {
	var p ai.Profile
	var raw string
	e := s.db.QueryRow("SELECT json FROM ai_profiles WHERE id=? AND deleted=0", id).Scan(&raw)
	if e != nil {
		return p, e
	}
	if e = json.Unmarshal([]byte(raw), &p); e != nil {
		return p, e
	}
	rows, e := s.db.Query("SELECT json FROM ai_models WHERE provider_id=? ORDER BY id", id)
	if e != nil {
		return p, e
	}
	p.Models = []ai.Model{}
	for rows.Next() {
		var raw string
		var m ai.Model
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return p, e
		}
		if e = json.Unmarshal([]byte(raw), &m); e != nil {
			rows.Close()
			return p, e
		}
		p.Models = append(p.Models, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return p, e
	}
	p.HasCredential, e = (credential.Vault{DB: s.db}).HasCredential(id)
	return p, e
}
func (s *Service) aiProfilesLocked() ([]ai.Profile, error) {
	rows, e := s.db.Query("SELECT id FROM ai_profiles WHERE deleted=0 ORDER BY created_at,id")
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []ai.Profile{}
	for _, id := range ids {
		p, e := s.aiProfileLocked(id)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, nil
}
func (s *Service) providerActiveLocked(id string) bool {
	for _, c := range s.chatControls {
		if c.providerID == id {
			return true
		}
	}
	return false
}
func (s *Service) saveAIProfileLocked(p ai.Profile) (ai.Profile, error) {
	if p.ID == "" {
		p.ID = newID()
	}
	if s.providerActiveLocked(p.ID) {
		return p, codedError("REQUEST_ACTIVE")
	}
	if e := ai.ValidateProfile(p); e != nil {
		return p, e
	}
	// Credential ownership comes from the vault, never the incoming profile.
	var cid string
	e := s.db.QueryRow("SELECT credential_id FROM ai_credentials WHERE provider_id=?", p.ID).Scan(&cid)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return p, e
	}
	p.CredentialID = cid
	p.HasCredential = cid != ""
	var oldRaw string
	endpointChanged := false
	if oldErr := s.db.QueryRow("SELECT json FROM ai_profiles WHERE id=?", p.ID).Scan(&oldRaw); oldErr == nil {
		var old ai.Profile
		if e := json.Unmarshal([]byte(oldRaw), &old); e != nil {
			return p, e
		}
		endpointChanged = old.BaseURL != p.BaseURL || old.Protocol != p.Protocol
	} else if !errors.Is(oldErr, sql.ErrNoRows) {
		return p, oldErr
	}
	if endpointChanged {
		p.CredentialID = ""
		p.HasCredential = false
	}
	models := p.Models
	p.Models = nil
	p.HasCredential = false
	e = s.transaction(func(tx *sql.Tx) error {
		stamp := now()
		if endpointChanged {
			if _, e := tx.Exec("DELETE FROM ai_credentials WHERE provider_id=?", p.ID); e != nil {
				return e
			}
		}
		if _, e := tx.Exec("INSERT INTO ai_profiles(id,json,created_at,updated_at) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET json=excluded.json,deleted=0,updated_at=excluded.updated_at", p.ID, encode(p), stamp, stamp); e != nil {
			return e
		}
		if _, e := tx.Exec("DELETE FROM ai_models WHERE provider_id=?", p.ID); e != nil {
			return e
		}
		for _, m := range models {
			if _, e := tx.Exec("INSERT INTO ai_models(provider_id,id,json) VALUES(?,?,?)", p.ID, m.ID, encode(m)); e != nil {
				return e
			}
		}
		return audit(tx, "", "AI_PROFILE_SAVED", map[string]string{"providerId": p.ID})
	})
	if e != nil {
		return p, e
	}
	return s.aiProfileLocked(p.ID)
}
func (s *Service) aiSettingsLocked() (ai.Selection, error) {
	v, err := s.resolvedConfiguration()
	return v.Selection, err
}
func (s *Service) usageLocked() ([]ai.UsageRecord, error) {
	rows, e := s.db.Query("SELECT json FROM ai_usage ORDER BY rowid DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ai.UsageRecord{}
	for rows.Next() {
		var raw string
		var u ai.UsageRecord
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &u); e != nil {
			return nil, e
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Service) cancelAllChatsLocked() int {
	for _, c := range s.chatControls {
		c.cancel()
	}
	return len(s.chatControls)
}

func (s *Service) callAI(method, payload string) (domain.Reply, bool) {
	switch method {
	case "GetPresetProvider", "ConnectPresetProvider":
		return s.callPresetProvider(method, payload), true
	case "ListAIProfiles", "SaveAIProfile", "DeleteAIProfile", "StoreCredential", "DeleteCredential", "GetAISettings", "SaveAISettings", "StartChat", "TestAIConnection", "CancelChat", "GetUsage", "ImportAttachment", "GetAttachment", "DeleteAttachment", "ClearChatConversation", "DeleteChatConversation":
	default:
		return domain.Reply{}, false
	}
	if method == "ImportAttachment" {
		return s.importAttachmentCall(payload), true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifetime.Err() != nil {
		return fail("SERVICE_CLOSED"), true
	}
	done := func(v any, e error) (domain.Reply, bool) { return result(v, e), true }
	switch method {
	case "ListAIProfiles", "GetAISettings", "GetUsage":
		if e := decode(payload, &struct{}{}); e != nil {
			return done(nil, e)
		}
		if method == "ListAIProfiles" {
			v, e := s.aiProfilesLocked()
			return done(v, e)
		}
		if method == "GetAISettings" {
			v, e := s.aiSettingsLocked()
			return done(v, e)
		}
		v, e := s.usageLocked()
		return done(v, e)
	case "SaveAIProfile":
		var q struct {
			Profile ai.Profile `json:"profile"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		v, e := s.saveAIProfileLocked(q.Profile)
		return done(v, e)
	case "SaveAISettings":
		var v ai.Selection
		if e := decode(payload, &v); e != nil {
			return done(nil, e)
		}
		if v.MaxOutputTokens < 1 || v.MaxOutputTokens > 131072 || v.MaxContextBytes < 1024 || v.MaxContextBytes > 1048576 || v.WarningThreshold != nil && (math.IsNaN(*v.WarningThreshold) || math.IsInf(*v.WarningThreshold, 0) || *v.WarningThreshold < 0) {
			return done(nil, codedError("INVALID_SETTINGS"))
		}
		for _, sel := range [][2]string{{v.DefaultProviderID, v.DefaultModelID}, {v.VisionProviderID, v.VisionModelID}} {
			if sel[0] == "" && sel[1] == "" {
				continue
			}
			p, e := s.aiProfileLocked(sel[0])
			if e != nil {
				return done(nil, e)
			}
			found := false
			for _, m := range p.Models {
				if m.ID == sel[1] {
					found = true
				}
			}
			if !found {
				return done(nil, codedError("MODEL_NOT_FOUND"))
			}
		}
		v, e := s.saveResolvedAISettings(v)
		return done(v, e)
	case "DeleteAIProfile":
		var q struct {
			ID      string `json:"id"`
			Confirm bool   `json:"confirm"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		if !q.Confirm {
			return done(nil, codedError("CONFIRMATION_REQUIRED"))
		}
		if s.providerActiveLocked(q.ID) {
			return done(nil, codedError("REQUEST_ACTIVE"))
		}
		p, e := s.aiProfileLocked(q.ID)
		if e != nil {
			return done(nil, e)
		}
		p.Enabled = false
		p.CredentialID = ""
		p.HasCredential = false
		p.Models = nil
		e = s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec("DELETE FROM ai_credentials WHERE provider_id=?", q.ID); e != nil {
				return e
			}
			if _, e := tx.Exec("UPDATE ai_profiles SET deleted=1,json=?,updated_at=? WHERE id=?", encode(p), now(), q.ID); e != nil {
				return e
			}
			return audit(tx, "", "AI_PROFILE_DELETED", map[string]string{"providerId": q.ID})
		})
		return done(e == nil, e)
	case "StoreCredential", "DeleteCredential":
		var q struct {
			ProviderID string `json:"providerId"`
			Secret     string `json:"secret,omitempty"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		if method == "DeleteCredential" && q.Secret != "" {
			return done(nil, codedError("INVALID_REQUEST"))
		}
		if s.providerActiveLocked(q.ProviderID) {
			return done(nil, codedError("REQUEST_ACTIVE"))
		}
		p, e := s.aiProfileLocked(q.ProviderID)
		if e != nil {
			return done(nil, e)
		}
		secret := []byte(q.Secret)
		q.Secret = ""
		defer credential.Wipe(secret)
		if method == "StoreCredential" && (len(secret) == 0 || strings.ContainsAny(string(secret), "\r\n")) {
			return done(nil, codedError("INVALID_CREDENTIAL"))
		}
		e = s.transaction(func(tx *sql.Tx) error {
			vault := credential.Vault{DB: tx}
			if method == "StoreCredential" {
				cid := p.CredentialID
				if cid == "" {
					cid = newID()
				}
				if e := vault.StoreCredential(cid, p.ID, secret); e != nil {
					return codedError("CREDENTIAL_UNAVAILABLE")
				}
				p.CredentialID = cid
			} else {
				if e := vault.DeleteCredential(p.ID); e != nil {
					return e
				}
				p.CredentialID = ""
			}
			p.Models = nil
			p.HasCredential = false
			if _, e := tx.Exec("UPDATE ai_profiles SET json=?,updated_at=? WHERE id=?", encode(p), now(), p.ID); e != nil {
				return e
			}
			return audit(tx, "", "AI_CREDENTIAL_UPDATED", map[string]string{"providerId": p.ID})
		})
		return done(map[string]bool{"hasCredential": method == "StoreCredential"}, e)
	case "StartChat", "TestAIConnection":
		var q ChatInput
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		if method == "TestAIConnection" && strings.TrimSpace(q.Content) == "" && len(q.AttachmentIDs) == 0 {
			q.Content = "Reply with OK to test this connection."
		}
		v, e := s.startChatLocked(q, method == "TestAIConnection")
		return done(v, e)
	case "CancelChat":
		var q struct {
			RequestID string `json:"requestId"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		c, ok := s.chatControls[q.RequestID]
		if ok {
			c.cancel()
		}
		return done(ok, nil)

	case "GetAttachment", "DeleteAttachment":
		var q struct {
			ID string `json:"id"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		if method == "GetAttachment" {
			v, e := s.attachmentLocked(q.ID, true)
			return done(v, e)
		}
		v, e := s.deleteAttachmentLocked(q.ID)
		return done(v, e)
	case "ClearChatConversation", "DeleteChatConversation":
		var q struct {
			ID                string `json:"id"`
			DeleteAttachments bool   `json:"deleteAttachments"`
			Confirm           bool   `json:"confirm"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		if !q.Confirm {
			return done(nil, codedError("CONFIRMATION_REQUIRED"))
		}
		v, e := s.clearChatLocked(q.ID, q.DeleteAttachments, method == "DeleteChatConversation")
		return done(v, e)
	}
	return fail("METHOD_NOT_ALLOWED"), true
}

const base64PayloadLimit = (maxImageBytes+2)/3*4 + 4096

func responseLanguage(content string, settings domain.Settings) string {
	lower := strings.ToLower(content)
	for _, phrase := range []string{"reply in english", "respond in english", "in english please", "用英语", "用英文"} {
		if strings.Contains(lower, phrase) {
			return "en-US"
		}
	}
	for _, phrase := range []string{"reply in chinese", "respond in chinese", "用中文", "用简体中文"} {
		if strings.Contains(lower, phrase) {
			return "zh-CN"
		}
	}
	if settings.AgentLanguage == "en-US" || settings.AgentLanguage == "zh-CN" {
		return settings.AgentLanguage
	}
	if settings.AgentLanguage == "auto" {
		for _, r := range content {
			if unicode.Is(unicode.Han, r) {
				return "zh-CN"
			}
		}
		return "en-US"
	}
	return settings.AppLanguage
}

func (s *Service) importAttachmentCall(payload string) domain.Reply {
	var q struct {
		Name       string `json:"name"`
		DataBase64 string `json:"dataBase64"`
	}
	if len(payload) > base64PayloadLimit {
		return fail("IMAGE_TOO_LARGE")
	}
	d := json.NewDecoder(strings.NewReader(payload))
	d.DisallowUnknownFields()
	if e := d.Decode(&q); e != nil {
		return fail("INVALID_REQUEST")
	}
	if d.Decode(new(any)) != io.EOF {
		return fail("INVALID_REQUEST")
	}
	s.attachmentMu.Lock()
	defer s.attachmentMu.Unlock()
	if s.lifetime.Err() != nil {
		return fail("SERVICE_CLOSED")
	}
	v, e := s.importAttachmentLocked(q.Name, q.DataBase64)
	return result(v, e)
}
