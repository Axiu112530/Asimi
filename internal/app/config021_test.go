package app

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"deskpilot/internal/ai"
	"deskpilot/internal/configuration"
)

func TestMediaLimitAppliesToHistoricalOutboundImages(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		jsonFixture(w)
	}))
	defer server.Close()
	service := aiFixture(t, nil)
	profile := aiTestProfile("historical-image", server.URL)
	mustCall(t, service, "SaveAIProfile", map[string]any{"profile": profile})

	pixels := image.NewNRGBA(image.Rect(0, 0, 800, 800))
	if _, err := rand.Read(pixels.Pix); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	attachment := mustCall(t, service, "ImportAttachment", map[string]string{
		"name": "noise-fixture.png", "dataBase64": base64.StdEncoding.EncodeToString(encoded.Bytes()),
	}).(ai.Attachment)
	if attachment.Bytes <= 1<<20 {
		t.Fatal("fixture did not exceed the lowered image limit")
	}
	request := aiSend(t, service, ChatInput{
		ProviderID: profile.ID, ModelID: "fixture", Content: "Review this fixture", ConfirmSend: true,
		AttachmentIDs: []string{attachment.ID},
	})
	if record := awaitAI(t, service, request["requestId"]); record.Status != "COMPLETED" {
		t.Fatal("initial fixture request failed")
	}
	mustCall(t, service, "SaveAgentPreferences", map[string]any{"media": configuration.MediaLimits{ImageMiB: 1}})
	reply := service.Call("StartChat", encode(ChatInput{
		ConversationID: request["conversationId"], ProviderID: profile.ID, ModelID: "fixture",
		Content: "Text follow-up", ConfirmSend: true,
	}))
	if reply.Error == nil || reply.Error.Code != "IMAGE_TOO_LARGE" || calls.Load() != 1 {
		t.Fatalf("historical image bypassed its active send limit: error=%+v requests=%d", reply.Error, calls.Load())
	}
	view := mustCall(t, service, "GetAttachment", map[string]string{"id": attachment.ID}).(ai.Attachment)
	if view.PreviewDataURL == "" || view.Bytes != attachment.Bytes {
		t.Fatal("lowering the send limit prevented local history viewing")
	}
}

func TestCommunityMediaOverridesFailBeforeRequest(t *testing.T) {
	s, _, _ := fixture(t)
	value := mustCall(t, s, "SaveAgentPreferences", map[string]any{
		"media": configuration.MediaLimits{Attachments: 1, ImageMiB: 1},
	}).(configuration.Resolved)
	if value.Media.Attachments != 1 || value.Sources["media.imageMiB"] != configuration.UserDefined {
		t.Fatalf("incorrect media settings: %+v", value)
	}
	reply := s.Call("StartChat", encode(ChatInput{Content: "Fixture", ConfirmSend: true, AttachmentIDs: []string{"a", "b"}}))
	if reply.Error == nil || reply.Error.Code != "ATTACHMENT_LIMIT_EXCEEDED" {
		t.Fatalf("expected a local attachment count rejection: %+v", reply.Error)
	}
	oversize := strings.Repeat("A", base64.StdEncoding.EncodedLen(1<<20)+4)
	if _, err := s.importAttachmentLocked("large.png", oversize); errorCode(err) != "IMAGE_TOO_LARGE" {
		t.Fatalf("expected size rejection before decoding: %v", err)
	}
	if reply := s.Call("SaveAgentPreferences", encode(map[string]any{
		"media": configuration.MediaLimits{Attachments: 5},
	})); reply.Error == nil || reply.Error.Code != "CONFIGURATION_INVALID" {
		t.Fatalf("accepted a media cap above the default: %+v", reply.Error)
	}
	reset := mustCall(t, s, "RestoreConfigurationDefaults", map[string]bool{"confirm": true}).(configuration.Resolved)
	if reset.Media != configuration.DefaultMedia() || reset.Sources["media.imageMiB"] != configuration.Default {
		t.Fatalf("reset left the old media caps active: %+v", reset)
	}
}
