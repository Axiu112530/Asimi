package app

import (
	"context"
	"strings"
	"testing"

	"asimi/internal/domain"
)

func TestBlankConversationsAreExcludedFromHistory(t *testing.T) {
	s := aiFixture(t, nil)
	mustCall(t, s, "CreateConversation", struct{}{})
	if got := mustCall(t, s, "ListConversations", struct{}{}).([]domain.Conversation); len(got) != 0 {
		t.Fatalf("empty conversation appeared in history: %+v", got)
	}
}

func TestDefaultCognitionEnablesAutomaticSkillCreation(t *testing.T) {
	s := aiFixture(t, nil)
	settings, err := loadCognitionSettings(s.db)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Memory || !settings.AutomaticLearning ||
		!settings.AutomaticSkillCreation || !settings.ProjectLearning {
		t.Fatalf("cognition defaults: %+v", settings)
	}
}

func TestChatDiagnosesEnvironmentLocally(t *testing.T) {
	s := aiFixture(t, nil)
	s.mvp.controls = map[string]context.CancelFunc{}
	input := map[string]string{"content": "帮我装一下开发环境"}
	c := mustCall(t, s, "DiagnoseChatEnvironment", input).(domain.Conversation)
	if len(c.Messages) != 2 || c.Messages[0].Content != input["content"] ||
		!strings.Contains(c.Messages[1].Content, "Python") || c.Messages[1].ProviderID != "" {
		t.Fatalf("expected locally observed runtimes in conversation: %+v", c.Messages)
	}
	if got := mustCall(t, s, "ListConversations", struct{}{}).([]domain.Conversation); len(got) != 1 || got[0].ID != c.ID {
		t.Fatalf("diagnosis not in history: %+v", got)
	}
}
