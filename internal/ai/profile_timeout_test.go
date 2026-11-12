package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProfileRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	profile := fixtureProfile(ChatCompletions, server.URL)
	profile.TimeoutSeconds = 1
	client, err := NewClient(profile)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, err = client.TestConnection(ctx, fixtureRequest(), nil)
	if err != Error("REQUEST_TIMEOUT") || ctx.Err() != nil {
		t.Fatalf("profile timeout did not precede the caller deadline: %v", err)
	}
	profile.TimeoutSeconds = 121
	if err := ValidateProfile(profile); err != Error("INVALID_PROFILE") {
		t.Fatalf("unbounded provider timeout accepted: %v", err)
	}
}
