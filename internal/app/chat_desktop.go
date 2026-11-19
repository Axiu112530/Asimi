package app

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"asimi/internal/command"
	"asimi/internal/domain"
)

func (s *Service) callDiagnoseChatEnvironment(payload string) domain.Reply {
	var input struct {
		ConversationID string `json:"conversationId"`
		Content        string `json:"content"`
	}
	if err := decode(payload, &input); err != nil {
		return result(nil, err)
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" || len(input.Content) > 8192 {
		return fail("INVALID_REQUEST")
	}
	diagnostics, err := s.diagnoseEnvironment()
	if err != nil {
		return result(nil, err)
	}
	if len(diagnostics) == 0 {
		return fail("ENVIRONMENT_DIAGNOSIS_FAILED")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	settings, err := s.settingsLocked()
	if err != nil {
		return result(nil, err)
	}
	reply := environmentDiagnosisText(diagnostics, strings.HasPrefix(responseLanguage(input.Content, settings), "zh"))
	c, err := s.saveEnvironmentChatLocked(input.ConversationID, input.Content, reply)
	if err == nil {
		s.changed(c.ID, "")
	}
	return result(c, err)
}

func (s *Service) saveEnvironmentChatLocked(id, content, reply string) (domain.Conversation, error) {
	stamp := time.Now().UTC()
	title := conversationTitle(content)
	create := id == ""
	if create {
		id = newID()
	} else {
		c, err := s.conversationLocked(id, true)
		if err != nil {
			return c, err
		}
		if c.Playground || len(c.Tasks) > 0 {
			return c, codedError("CONVERSATION_MODE_MISMATCH")
		}
		var deleted int
		var since string
		err = s.db.QueryRow("SELECT deleted,context_since FROM ai_conversations WHERE conversation_id=?", id).Scan(&deleted, &since)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return c, err
		}
		if deleted != 0 {
			return c, codedError("NOT_FOUND")
		}
		if errors.Is(err, sql.ErrNoRows) && len(c.Messages) > 0 {
			return c, codedError("LEGACY_CONVERSATION")
		}
		for _, active := range s.chatControls {
			if active.conversationID == id {
				return c, codedError("REQUEST_ACTIVE")
			}
		}
		for _, at := range []string{since, c.UpdatedAt} {
			if previous, e := time.Parse(time.RFC3339Nano, at); e == nil && !stamp.After(previous) {
				stamp = previous.Add(time.Nanosecond)
			}
		}
	}
	user := domain.Message{ID: newID(), ConversationID: id, Role: "user", Content: content, CreatedAt: stamp.Format(chatTimeFormat)}
	assistant := domain.Message{ID: newID(), ConversationID: id, Role: "assistant", Content: reply, CreatedAt: stamp.Add(time.Nanosecond).Format(chatTimeFormat)}
	err := s.transaction(func(tx *sql.Tx) error {
		if create {
			if _, err := tx.Exec("INSERT INTO conversations(id,title,created_at,updated_at) VALUES(?,?,?,?)", id, title, user.CreatedAt, assistant.CreatedAt); err != nil {
				return err
			}
		} else if _, err := tx.Exec("UPDATE conversations SET title=CASE WHEN TRIM(title)='' THEN ? ELSE title END,updated_at=? WHERE id=?", title, assistant.CreatedAt, id); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO ai_conversations(conversation_id) VALUES(?)", id); err != nil {
			return err
		}
		for _, m := range []domain.Message{user, assistant} {
			if _, err := tx.Exec("INSERT INTO messages(id,conversation_id,role,content,created_at,updated_at) VALUES(?,?,?,?,?,?)",
				m.ID, m.ConversationID, m.Role, m.Content, m.CreatedAt, m.CreatedAt); err != nil {
				return err
			}
		}
		// Keep observed local versions out of future provider requests.
		if _, err := tx.Exec("INSERT INTO ai_message_metadata(message_id,json) VALUES(?,?)", assistant.ID, encode(messageMetadata{Status: "LOCAL_COMPLETED"})); err != nil {
			return err
		}
		return audit(tx, "", "ENVIRONMENT_DIAGNOSED", map[string]string{"conversationId": id})
	})
	if err != nil {
		return domain.Conversation{}, err
	}
	return s.conversationLocked(id, true)
}

func environmentDiagnosisText(diagnostics []command.Diagnostic, chinese bool) string {
	intro, missing, failed := "I checked the development runtimes on this computer.", "Not found on PATH", "Version check failed"
	outro := "For any missing runtime you need, download and run its official installer."
	if chinese {
		intro, missing, failed = "我已检查这台电脑上的开发环境。", "在 PATH 中未找到", "版本检查失败"
		outro = "需要安装的组件，请从对应的官方网站下载安装程序并运行。"
	}
	names := map[string]string{"go": "Go", "java": "Java", "node": "Node.js", "npm": "npm", "dotnet": ".NET", "python": "Python", "git": "Git"}
	var text strings.Builder
	text.WriteString(intro + "\n\n")
	for _, diagnostic := range diagnostics {
		name, version := names[diagnostic.Component], missing
		if name == "" {
			name = diagnostic.Component
		}
		switch diagnostic.Status {
		case "OK":
			version, _, _ = strings.Cut(diagnostic.Version, "\n")
			version = strings.TrimSpace(version)
		case "ERROR":
			version = failed
		}
		if diagnostic.Status != "OK" && diagnostic.DownloadURL != "" {
			version += " · " + diagnostic.DownloadURL
		}
		text.WriteString("- **" + name + "** · " + version + "\n")
	}
	text.WriteString("\n" + outro)
	return text.String()
}
