//go:build windows

package app

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"asimi/internal/ai"
)

func TestAICredentialSnapshotEndpointInvalidationCorruptionAndDeletion(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer replacement-fixture-key" {
			t.Error("credential replacement not used")
		}
		io.Copy(io.Discard, r.Body)
		jsonFixture(w)
	}))
	defer server.Close()
	s := aiFixture(t, nil)
	p := aiTestProfile("keys", server.URL)
	mustCall(t, s, "SaveAIProfile", map[string]any{"profile": p})
	for _, secret := range []string{"first-fixture-key", "replacement-fixture-key"} {
		v := mustCall(t, s, "StoreCredential", map[string]string{"providerId": p.ID, "secret": secret}).(map[string]bool)
		if !v["hasCredential"] {
			t.Fatal(v)
		}
	}
	profiles := mustCall(t, s, "ListAIProfiles", struct{}{}).([]ai.Profile)
	if len(profiles) != 1 || !profiles[0].HasCredential || profiles[0].CredentialID == "" {
		t.Fatal(profiles)
	}
	var blob []byte
	s.db.QueryRow("SELECT encrypted_blob FROM ai_credentials WHERE provider_id=?", p.ID).Scan(&blob)
	if bytes.Contains(blob, []byte("replacement-fixture-key")) {
		t.Fatal("plaintext stored")
	}
	ids := aiSend(t, s, ChatInput{ProviderID: p.ID, ModelID: "fixture", Content: "test replacement", ConfirmSend: true})
	if u := awaitAI(t, s, ids["requestId"]); u.Status != "COMPLETED" {
		t.Fatal(u)
	}
	if _, e := s.db.Exec("UPDATE ai_credentials SET encrypted_blob=? WHERE provider_id=?", []byte("corrupt"), p.ID); e != nil {
		t.Fatal(e)
	}
	ids = aiSend(t, s, ChatInput{ProviderID: p.ID, ModelID: "fixture", Content: "corrupt key", ConfirmSend: true})
	if u := awaitAI(t, s, ids["requestId"]); u.ErrorCode != "CREDENTIAL_DECRYPT_FAILED" {
		t.Fatal(u)
	}
	if requests.Load() != 1 {
		t.Fatal("corrupt key reached network")
	}
	mustCall(t, s, "StoreCredential", map[string]string{"providerId": p.ID, "secret": "replacement-fixture-key"})
	p.BaseURL = server.URL + "/changed-origin-or-path"
	updated := mustCall(t, s, "SaveAIProfile", map[string]any{"profile": p}).(ai.Profile)
	if updated.HasCredential || updated.CredentialID != "" {
		t.Fatal("endpoint edit retained credential")
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM ai_credentials").Scan(&count)
	if count != 0 {
		t.Fatal("invalidated ciphertext retained")
	}
	mustCall(t, s, "StoreCredential", map[string]string{"providerId": p.ID, "secret": "replacement-fixture-key"})
	mustCall(t, s, "DeleteCredential", map[string]string{"providerId": p.ID})
	profiles = mustCall(t, s, "ListAIProfiles", struct{}{}).([]ai.Profile)
	if profiles[0].HasCredential || profiles[0].CredentialID != "" {
		t.Fatal("deleted credential retained")
	}
}
