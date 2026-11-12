package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"asimi/internal/ai"
	"asimi/internal/domain"
)

func aiFixture(t *testing.T, emit func(string, any)) *Service {
	t.Helper()
	dir := t.TempDir()
	db, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{db: db, dataDir: dir, lifetime: ctx, stop: cancel, controls: map[string]control{}, chatControls: map[string]chatControl{}, workerFaults: map[string]workerFault{}, releaseProfile: func() error { return nil }, emit: emit}
	_, e = db.Exec("INSERT INTO app_settings(id,json,created_at,updated_at) VALUES('app',?,?,?)", encode(domain.Settings{AppLanguage: "en-US", AgentLanguage: "follow-app", Theme: "light", Startup: "welcome"}), now(), now())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.initializeAI(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := s.Close(); e != nil {
			t.Error(e)
		}
	})
	return s
}
func aiTestProfile(id, url string) ai.Profile {
	return ai.Profile{ID: id, Name: id, Protocol: ai.ChatCompletions, BaseURL: url, Enabled: true, Models: []ai.Model{{ID: "fixture", DisplayName: "Fixture", Capabilities: ai.Capabilities{Text: ai.Supported, Vision: ai.Supported, Streaming: ai.Supported, Temperature: ai.Supported, ToolCalling: ai.Unsupported, ComputerUse: ai.Unsupported, ImageDetails: []string{"auto", "low", "high"}}, Pricing: &ai.Pricing{InputPerMillion: 2, OutputPerMillion: 4, CachedInputPerMillion: float64ptr(1), Currency: "USD"}}}}
}
func float64ptr(v float64) *float64 { return &v }
func awaitAI(t *testing.T, s *Service, requestID string) ai.UsageRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		records := mustCall(t, s, "GetUsage", struct{}{}).([]ai.UsageRecord)
		for _, r := range records {
			if r.RequestID == requestID {
				return r
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("AI request did not finish")
	return ai.UsageRecord{}
}
func aiSend(t *testing.T, s *Service, in ChatInput) map[string]string {
	t.Helper()
	return mustCall(t, s, "StartChat", in).(map[string]string)
}
func jsonFixture(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"local-fixture","choices":[{"message":{"content":"Fixture answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":25}}}`)
}

func TestAIOptionalCredentialContextIsolationSwitchAndUsage(t *testing.T) {
	requests := make(chan string, 10)
	auth := make(chan string, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- string(body)
		auth <- r.Header.Get("Authorization")
		jsonFixture(w)
	}))
	defer server.Close()
	s := aiFixture(t, nil)
	p := aiTestProfile("one", server.URL)
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": p})
	p.ID = "two"
	p.Name = "two"
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": p})
	first := aiSend(t, s, ChatInput{ProviderID: "one", ModelID: "fixture", Content: "first secret conversation", ConfirmSend: true})
	usage := awaitAI(t, s, first["requestId"])
	if usage.Status != "COMPLETED" || usage.EstimatedCost == nil || mathAbs(*usage.EstimatedCost-0.000255) > 1e-10 {
		t.Fatalf("usage %#v", usage)
	}
	if <-auth != "" {
		t.Fatal("optional credential must omit Authorization")
	}
	<-requests
	unrelated := aiSend(t, s, ChatInput{ProviderID: "one", ModelID: "fixture", Content: "unrelated content", ConfirmSend: true})
	awaitAI(t, s, unrelated["requestId"])
	if body := <-requests; strings.Contains(body, "first secret") {
		t.Fatal("other conversation leaked")
	}
	<-auth
	follow := aiSend(t, s, ChatInput{ConversationID: first["conversationId"], ProviderID: "one", ModelID: "fixture", Content: "next", ConfirmSend: true})
	awaitAI(t, s, follow["requestId"])
	if body := <-requests; !strings.Contains(body, "first secret") || !strings.Contains(body, "Fixture answer") {
		t.Fatal("same-conversation history missing")
	}
	<-auth
	reply := s.Call("StartChat", encode(ChatInput{ConversationID: first["conversationId"], ProviderID: "two", ModelID: "fixture", Content: "switch", ConfirmSend: true}))
	if reply.Error == nil || reply.Error.Code != "PROVIDER_SWITCH_CONFIRMATION_REQUIRED" {
		t.Fatal(reply)
	}
	switched := aiSend(t, s, ChatInput{ConversationID: first["conversationId"], ProviderID: "two", ModelID: "fixture", Content: "switch", ConfirmSend: true, ConfirmProviderSwitch: true})
	awaitAI(t, s, switched["requestId"])
	<-requests
	<-auth
	c := mustCall(t, s, "GetConversation", map[string]string{"id": first["conversationId"]}).(domain.Conversation)
	if len(c.Messages) != 6 || c.Messages[1].Usage == nil || c.Messages[1].ProviderID != "one" || c.Messages[5].ProviderID != "two" {
		t.Fatalf("message metadata missing %#v", c.Messages)
	}
	mustCall(t, s, "ClearChatConversation", map[string]any{"id": c.ID, "confirm": true, "deleteAttachments": true})
	c = mustCall(t, s, "GetConversation", map[string]string{"id": c.ID}).(domain.Conversation)
	if len(c.Messages) != 0 {
		t.Fatal("clear retained visible messages")
	}
	var count int
	if e := s.db.QueryRow("SELECT COUNT(*) FROM messages WHERE conversation_id=? AND content!=''", c.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("clear retained original text")
	}
	if len(mustCall(t, s, "GetUsage", struct{}{}).([]ai.UsageRecord)) != 4 {
		t.Fatal("clear deleted usage")
	}
	mustCall(t, s, "DeleteAIProfile", map[string]any{"id": "one", "confirm": true})
	if len(mustCall(t, s, "GetUsage", struct{}{}).([]ai.UsageRecord)) != 4 {
		t.Fatal("profile deletion deleted usage")
	}
}
func mathAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestAIStreamingCancelStopAllAndShutdownPersistPartial(t *testing.T) {
	for _, mode := range []string{"cancel", "stop-all", "close"} {
		t.Run(mode, func(t *testing.T) {
			delivered := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
				w.(http.Flusher).Flush()
				close(delivered)
				<-r.Context().Done()
			}))
			defer server.Close()
			var mu sync.Mutex
			events := []map[string]any{}
			s := aiFixture(t, func(name string, value any) {
				if strings.HasPrefix(name, "assistant.stream.") {
					mu.Lock()
					v := value.(map[string]any)
					v["kind"] = name
					events = append(events, v)
					mu.Unlock()
				}
			})
			p := aiTestProfile("stream", server.URL)
			mustCall(t, s, "SaveAIProfile", map[string]any{"profile": p})
			ids := aiSend(t, s, ChatInput{ProviderID: p.ID, ModelID: "fixture", Content: "stream", ConfirmSend: true, Stream: true})
			select {
			case <-delivered:
			case <-time.After(5 * time.Second):
				t.Fatal("local fixture was never reached")
			}
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				mu.Lock()
				n := len(events)
				mu.Unlock()
				if n >= 2 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			switch mode {
			case "cancel":
				mustCall(t, s, "CancelChat", map[string]string{"requestId": ids["requestId"]})
			case "stop-all":
				mustCall(t, s, "StopAll", struct{}{})
			case "close":
				if e := s.Close(); e != nil {
					t.Fatal(e)
				}
			}
			if mode != "close" {
				u := awaitAI(t, s, ids["requestId"])
				if u.Status != "CANCELLED" {
					t.Fatal(u)
				}
				c := mustCall(t, s, "GetConversation", map[string]string{"id": ids["conversationId"]}).(domain.Conversation)
				if c.Messages[1].Content != "partial" || c.Messages[1].Status != "CANCELLED" {
					t.Fatal(c.Messages)
				}
			} else {
				db, e := openStore(s.dataDir)
				if e != nil {
					t.Fatal(e)
				}
				defer db.Close()
				var content string
				if e = db.QueryRow("SELECT content FROM messages WHERE id=?", ids["messageId"]).Scan(&content); e != nil || content != "partial" {
					t.Fatalf("shutdown lost partial: %q %v", content, e)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			for i, event := range events {
				if event["sequence"] != i+1 {
					t.Fatal("event sequence")
				}
			}
			if len(events) < 3 || events[len(events)-1]["status"] != "CANCELLED" {
				t.Fatal(events)
			}
		})
	}
}

func TestAIAttachmentImportMetadataRemovalAndLimits(t *testing.T) {
	s := aiFixture(t, nil)
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, im)
	a := mustCall(t, s, "ImportAttachment", map[string]string{"name": "C:\\private\\photo.jpg", "dataBase64": base64.StdEncoding.EncodeToString(buf.Bytes())}).(ai.Attachment)
	if a.Name != "photo.png" || a.MIME != "image/png" || a.Width != 2 || a.PreviewDataURL == "" {
		t.Fatal(a)
	}
	var raw string
	s.db.QueryRow("SELECT json FROM ai_attachments WHERE id=?", a.ID).Scan(&raw)
	if strings.Contains(raw, "base64") || strings.Contains(raw, "private") {
		t.Fatal("private image data/path persisted")
	}
	if got := mustCall(t, s, "GetAttachment", map[string]string{"id": a.ID}).(ai.Attachment); got.SHA256 != a.SHA256 {
		t.Fatal("attachment roundtrip")
	}
	reply := s.Call("GetAttachment", `{"id":"../../secret"}`)
	if reply.Error == nil {
		t.Fatal("path accepted")
	}
	var animated bytes.Buffer
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White})
	gif.EncodeAll(&animated, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{0, 0}})
	reply = s.Call("ImportAttachment", encode(map[string]string{"name": "animated.gif", "dataBase64": base64.StdEncoding.EncodeToString(animated.Bytes())}))
	if reply.Error == nil || reply.Error.Code != "ANIMATED_IMAGE_UNSUPPORTED" {
		t.Fatal(reply)
	}
	path, _ := s.attachmentPath(a.ID)
	if e := os.WriteFile(path, []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	reply = s.Call("GetAttachment", encode(map[string]string{"id": a.ID}))
	if reply.Error == nil {
		t.Fatal("tampered file accepted")
	}
	mustCall(t, s, "DeleteAttachment", map[string]string{"id": a.ID})
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("file not deleted")
	}
}

func TestAIRequestStartupUnknownAndV3MigrationRollback(t *testing.T) {
	s := aiFixture(t, nil)
	e := s.transaction(func(tx *sql.Tx) error {
		if _, e := tx.Exec("INSERT INTO conversations VALUES('historic','retain','date','date');INSERT INTO messages VALUES('pending','historic',NULL,'assistant','','date','date')"); e != nil {
			return e
		}
		if _, e := tx.Exec("INSERT INTO ai_message_metadata VALUES('pending',?)", encode(messageMetadata{ProviderID: "p", ModelID: "m", RequestID: "r", Status: "PENDING"})); e != nil {
			return e
		}
		_, e := tx.Exec("INSERT INTO ai_requests VALUES('r','historic','pending','p','m','PENDING','date')")
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.initializeAI(); e != nil {
		t.Fatal(e)
	}
	u := mustCall(t, s, "GetUsage", struct{}{}).([]ai.UsageRecord)
	if len(u) != 1 || u[0].ErrorCode != "REQUEST_OUTCOME_UNKNOWN" {
		t.Fatal(u)
	}
	dir := t.TempDir()
	db := openV1(t, dir)
	if e = applyMigration(db, schemaV2); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE ai_models(conflict TEXT)"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if opened, e := openStore(dir); e == nil {
		opened.Close()
		t.Fatal("v3 migration conflict accepted")
	}
	db, e = sql.Open("sqlite", filepath.Join(dir, "asimi.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var version, count int
	db.QueryRow("PRAGMA user_version").Scan(&version)
	db.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name='ai_profiles'").Scan(&count)
	if version != 2 || count != 0 {
		t.Fatal("v3 migration not rolled back")
	}
}

func TestAICapabilityAndManualConfirmationFailClosed(t *testing.T) {
	s := aiFixture(t, nil)
	p := aiTestProfile("caps", "http://127.0.0.1:9")
	p.Models[0].Capabilities.Text = ai.Unknown
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": p})
	for _, in := range []ChatInput{{ProviderID: p.ID, ModelID: "fixture", Content: "no confirm"}, {ProviderID: p.ID, ModelID: "fixture", Content: "unknown caps", ConfirmSend: true}} {
		r := s.Call("StartChat", encode(in))
		if r.Error == nil {
			t.Fatal("unsafe request admitted")
		}
	}
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM ai_requests").Scan(&n)
	if n != 0 {
		t.Fatal("rejected request persisted/dispatched")
	}
}

func TestAIPriceCachedSubsetUnknownsAndLanguage(t *testing.T) {
	input, output, cached := int64(100), int64(20), int64(25)
	u := ai.Usage{InputTokens: &input, OutputTokens: &output, CachedInputTokens: &cached, UsageAvailable: true}
	p := &ai.Pricing{InputPerMillion: 2, OutputPerMillion: 4, CachedInputPerMillion: float64ptr(1)}
	if c := estimateCost(u, p); c == nil || mathAbs(*c-0.000255) > 1e-10 {
		t.Fatal(c)
	}
	p.CachedInputPerMillion = nil
	if estimateCost(u, p) != nil {
		t.Fatal("unknown cached price fabricated cost")
	}
	if estimateCost(u, nil) != nil {
		t.Fatal("missing price fabricated zero")
	}
	if responseLanguage("用中文回答", domain.Settings{AppLanguage: "en-US", AgentLanguage: "en-US"}) != "zh-CN" {
		t.Fatal("explicit language lost")
	}
}

func TestAIAttachmentCleanupQueueRetainsOwnershipAfterRemoveFailure(t *testing.T) {
	s := aiFixture(t, nil)
	var buf bytes.Buffer
	png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	a := mustCall(t, s, "ImportAttachment", map[string]string{"name": "fixture.png", "dataBase64": base64.StdEncoding.EncodeToString(buf.Bytes())}).(ai.Attachment)
	e := s.transaction(func(tx *sql.Tx) error {
		if _, e := tx.Exec("INSERT INTO conversations VALUES('cleanup','secret title','date','date');INSERT INTO ai_conversations(conversation_id,playground) VALUES('cleanup',1);INSERT INTO messages VALUES('cleanup-message','cleanup',NULL,'user','secret text','date','date')"); e != nil {
			return e
		}
		if _, e := tx.Exec("INSERT INTO ai_message_metadata VALUES('cleanup-message',?)", encode(messageMetadata{ProviderID: "p", Attachments: []ai.Attachment{a}})); e != nil {
			return e
		}
		_, e := tx.Exec("INSERT INTO ai_message_attachments VALUES('cleanup-message',?)", a.ID)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	s.removeAttachment = func(string) error { return os.ErrPermission }
	r := s.Call("DeleteChatConversation", `{"id":"cleanup","confirm":true,"deleteAttachments":true}`)
	if r.Error == nil || r.Error.Code != "ATTACHMENT_RECOVERY_REQUIRED" {
		t.Fatal(r)
	}
	var count int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM ai_attachment_cleanup WHERE conversation_id='cleanup' AND attachment_id=?", a.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("failed removal lost cleanup ownership")
	}
	conversations := mustCall(t, s, "ListConversations", struct{}{}).([]domain.Conversation)
	if len(conversations) != 1 || !conversations[0].Playground {
		t.Fatal("failed tombstone not discoverable with mode")
	}
	c := mustCall(t, s, "GetConversation", map[string]string{"id": "cleanup"}).(domain.Conversation)
	if !c.Playground || len(c.Messages) != 0 {
		t.Fatal("tombstone inspect failed")
	}
	path, _ := s.attachmentPath(a.ID)
	if _, e = os.Stat(path + ".deleting"); e != nil {
		t.Fatal("recoverable bytes missing")
	}
	s.removeAttachment = nil
	mustCall(t, s, "DeleteChatConversation", map[string]any{"id": "cleanup", "confirm": true, "deleteAttachments": true})
	if e = s.db.QueryRow("SELECT COUNT(*) FROM ai_attachment_cleanup WHERE conversation_id='cleanup'").Scan(&count); e != nil || count != 0 {
		t.Fatal("retry did not settle cleanup queue")
	}
	if _, e = os.Stat(path + ".deleting"); !os.IsNotExist(e) {
		t.Fatal("retry retained file")
	}
	if len(mustCall(t, s, "ListConversations", struct{}{}).([]domain.Conversation)) != 0 {
		t.Fatal("settled tombstone still visible")
	}
}
