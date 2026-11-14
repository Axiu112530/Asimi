package app

import (
	"database/sql"
	"strings"
)

func conversationTitle(content string) string {
	text := strings.Join(strings.Fields(content), " ")
	for _, prefix := range []string{"请帮我", "请你", "帮我", "Could you ", "Can you "} {
		if after, ok := strings.CutPrefix(text, prefix); ok && strings.TrimSpace(after) != "" {
			text = strings.TrimSpace(after)
			break
		}
	}
	title := []rune(text)
	if len(title) > 32 {
		return strings.TrimSpace(string(title[:32])) + "…"
	}
	return text
}

func restoreChatTitles(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT c.id, (
 SELECT m.content FROM messages m
 WHERE m.conversation_id=c.id AND m.role='user' AND TRIM(m.content)<>''
 ORDER BY m.created_at,m.id LIMIT 1),
 EXISTS(SELECT 1 FROM messages m WHERE m.conversation_id=c.id AND m.role='user' AND (
 EXISTS(SELECT 1 FROM ai_message_attachments a WHERE a.message_id=m.id) OR
 EXISTS(SELECT 1 FROM ai_message_metadata meta WHERE meta.message_id=m.id AND json_type(meta.json,'$.screenCapture')='object'))),
 (SELECT json_extract(json,'$.appLanguage') FROM app_settings WHERE id='app')
 FROM conversations c JOIN ai_conversations ac ON ac.conversation_id=c.id
 WHERE TRIM(c.title)='' AND ac.deleted=0`)
	if err != nil {
		return err
	}
	type titleRow struct{ id, text string }
	var updates []titleRow
	for rows.Next() {
		var id string
		var content, language sql.NullString
		var hasImages bool
		if err := rows.Scan(&id, &content, &hasImages, &language); err != nil {
			rows.Close()
			return err
		}
		title := conversationTitle(content.String)
		if title == "" && hasImages {
			title = "Image discussion"
			if strings.HasPrefix(language.String, "zh") {
				title = "图片分析"
			}
		}
		if title != "" {
			updates = append(updates, titleRow{id: id, text: title})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, update := range updates {
		if _, err := tx.Exec("UPDATE conversations SET title=? WHERE id=? AND TRIM(title)=''", update.text, update.id); err != nil {
			return err
		}
	}
	return nil
}
