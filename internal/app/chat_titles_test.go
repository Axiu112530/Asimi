package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"asimi/internal/domain"
)

func TestPrecreatedChatGetsTitleAndOldEmptyTitlesRecover(t *testing.T) {
	s := aiFixture(t, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { jsonFixture(w) }))
	defer server.Close()
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": aiTestProfile("chat", server.URL)})
	c := mustCall(t, s, "CreateConversation", struct{}{}).(domain.Conversation)
	reply := aiSend(t, s, ChatInput{ConversationID: c.ID, ProviderID: "chat", ModelID: "fixture", Content: "  请帮我整理本周的工作计划\n并列出优先级 ", ConfirmSend: true})
	awaitAI(t, s, reply["requestId"])
	updated := mustCall(t, s, "GetConversation", map[string]string{"id": c.ID}).(domain.Conversation)
	if !strings.Contains(updated.Title, "工作计划") || strings.Contains(updated.Title, "\n") {
		t.Fatalf("missing generated title: %q", updated.Title)
	}
	if _, err := s.db.Exec("UPDATE conversations SET title='' WHERE id=?", c.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.initializeAI(); err != nil {
		t.Fatal(err)
	}
	recovered := mustCall(t, s, "GetConversation", map[string]string{"id": c.ID}).(domain.Conversation)
	if recovered.Title != updated.Title {
		t.Fatalf("old title recovery: %q, want %q", recovered.Title, updated.Title)
	}
	if _, err := s.db.Exec("UPDATE conversations SET title='My chosen title' WHERE id=?", c.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.initializeAI(); err != nil {
		t.Fatal(err)
	}
	named := mustCall(t, s, "GetConversation", map[string]string{"id": c.ID}).(domain.Conversation)
	if named.Title != "My chosen title" {
		t.Fatal(named.Title)
	}
}

func TestRestoreImageOnlyChatTitlesFromStoredEvidence(t *testing.T) {
	for _, test := range []struct {
		name, language, title, content, role, metadata, want string
		attachment, unsent, deleted                          bool
	}{
		{name: "uploaded image", language: "en-US", role: "user", attachment: true, want: "Image discussion"},
		{name: "screenshot", language: "zh-CN", role: "user", metadata: `{"screenCapture":{"captureId":"capture-history"}}`, want: "图片分析"},
		{name: "blank image prompt", language: "en-US", role: "user", content: " \t\n", attachment: true, want: "Image discussion"},
		{name: "existing title", language: "zh-CN", title: "My chosen title", role: "user", attachment: true, want: "My chosen title"},
		{name: "never sent", language: "en-US", unsent: true},
		{name: "assistant evidence is insufficient", language: "en-US", role: "assistant", metadata: `{"screenCapture":{"captureId":"capture-history"}}`},
		{name: "null capture is insufficient", language: "en-US", role: "user", metadata: `{"screenCapture":null}`},
		{name: "text takes priority", language: "en-US", role: "user", content: "Could you explain this picture", attachment: true, want: "explain this picture"},
		{name: "deleted chat", language: "en-US", role: "user", attachment: true, deleted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := aiFixture(t, nil)
			stamp := now()
			if _, err := s.db.Exec("UPDATE app_settings SET json=json_set(json,'$.appLanguage',?) WHERE id='app'", test.language); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec("INSERT INTO conversations(id,title,created_at,updated_at) VALUES('history',?,?,?)", test.title, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec("INSERT INTO ai_conversations(conversation_id,deleted) VALUES('history',?)", test.deleted); err != nil {
				t.Fatal(err)
			}
			if !test.unsent {
				if _, err := s.db.Exec("INSERT INTO messages(id,conversation_id,role,content,created_at,updated_at) VALUES('history-message','history',?,?,?,?)", test.role, test.content, stamp, stamp); err != nil {
					t.Fatal(err)
				}
				if test.metadata != "" {
					if _, err := s.db.Exec("INSERT INTO ai_message_metadata(message_id,json) VALUES('history-message',?)", test.metadata); err != nil {
						t.Fatal(err)
					}
				}
				if test.attachment {
					if _, err := s.db.Exec("INSERT INTO ai_attachments(id,json,created_at) VALUES('historical-image','{}',?); INSERT INTO ai_message_attachments(message_id,attachment_id) VALUES('history-message','historical-image')", stamp); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := s.transaction(restoreChatTitles); err != nil {
				t.Fatal(err)
			}
			var actual string
			if err := s.db.QueryRow("SELECT title FROM conversations WHERE id='history'").Scan(&actual); err != nil {
				t.Fatal(err)
			}
			if actual != test.want {
				t.Fatalf("restored title = %q, want %q", actual, test.want)
			}
		})
	}
}
