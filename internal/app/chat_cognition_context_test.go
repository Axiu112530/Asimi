package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"asimi/internal/domain"
)

func TestChatSuppliesMatchedPersonalSkillAndMemory(t *testing.T) {
	body := make(chan string, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body <- string(raw)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"Done"},"finish_reason":"stop"}]}`)
	}))
	defer provider.Close()

	s := aiFixture(t, nil)
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": aiTestProfile("fixture", provider.URL)})
	mustCall(t, s, "SaveCognitionMemory", map[string]string{"kind": "user", "key": "reply-style", "text": "Keep answers concise."})
	mustCall(t, s, "SaveCognitionSkill", map[string]any{
		"scope": "user", "slug": "release-review", "name": "Release Review",
		"description": "Review before publishing.", "instructions": "Use the acceptance marker BLUEPRINT_CHECK.",
		"triggers": []string{"release review"},
	})
	started := aiSend(t, s, ChatInput{ProviderID: "fixture", ModelID: "fixture", Content: "Do a release review", ConfirmSend: true})
	if usage := awaitAI(t, s, started["requestId"]); usage.Status != "COMPLETED" {
		t.Fatalf("unexpected completion status: %s (%s)", usage.Status, usage.ErrorCode)
	}
	chat := mustCall(t, s, "GetConversation", map[string]string{"id": started["conversationId"]}).(domain.Conversation)
	references := chat.Messages[1].Cognition
	if references == nil || references.Memories != 1 || references.Skills != 1 || chat.Messages[0].Cognition != nil {
		t.Fatalf("unexpected references on the persisted reply: %+v", chat.Messages)
	}
	select {
	case request := <-body:
		if !strings.Contains(request, "Keep answers concise.") || !strings.Contains(request, "BLUEPRINT_CHECK") {
			t.Fatal("expected both the saved memory and the matched skill in the provider input")
		}
	default:
		t.Fatal("provider request did not occur")
	}
}
