package app

import (
	"asimi/internal/communication"
	"encoding/json"
	"strings"
	"testing"
)

func TestCommunicationBusinessAPIAndImmutableApproval(t *testing.T) {
	s, _, _ := fixture(t)
	reply := s.Call("SaveConnection", `{"kind":"github","name":"public","origin":"https://api.github.com","enabled":true,"scopes":[{"resource":"octocat/Hello-World","operations":["repo.read"]}]}`)
	if reply.Error != nil {
		t.Fatalf("SaveConnection unavailable: %+v", reply.Error)
	}
	c, ok := reply.Data.(communication.Connection)
	if !ok || c.ID == "" {
		t.Fatalf("connection shape %+v", reply.Data)
	}
	action := mustCall(t, s, "PrepareConnectorAction", communication.PrepareInput{ConnectionID: c.ID, Operation: "repo.read", Resource: "octocat/Hello-World", Request: json.RawMessage(`{}`)}).(communication.Action)
	if action.Status != "NEEDS_APPROVAL" || action.Origin != "https://api.github.com" || action.Method != "GET" || action.Path != "/repos/octocat/Hello-World" {
		t.Fatalf("approval %+v", action)
	}
	r := s.Call("ApproveConnectorAction", encode(map[string]string{"id": action.ID, "hash": "wrong"}))
	if r.Error == nil || !strings.HasPrefix(r.Error.Code, "CONNECTOR_") {
		t.Fatalf("tampered approval %+v", r)
	}
	mustCall(t, s, "RejectConnectorAction", map[string]string{"id": action.ID, "hash": action.Hash})
	r = s.Call("ApproveConnectorAction", encode(map[string]string{"id": action.ID, "hash": action.Hash}))
	if r.Error == nil {
		t.Fatal("rejected approval allowed")
	}
	r = s.Call("SaveConnection", `{"kind":"github","origin":"https://api.github.com","enabled":true,"scopes":[],"secret":"not-allowed"}`)
	if r.Error == nil || r.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("unknown fields %+v", r)
	}
}
