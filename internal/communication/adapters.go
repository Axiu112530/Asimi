package communication

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"

	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)
var toolPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
var channelPattern = regexp.MustCompile(`^[CGD][A-Z0-9]{2,64}$`)

type issueInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
type textInput struct {
	Text string `json:"text"`
}
type mailInput struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
	To      string `json:"to"`
}
type eventInput struct {
	Subject  string `json:"subject"`
	Start    string `json:"start"`
	End      string `json:"end"`
	TimeZone string `json:"timeZone"`
}
type toolInput struct {
	Arguments map[string]any `json:"arguments"`
}

func strict(raw []byte, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return ErrInvalid
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return ErrInvalid
	}
	return nil
}
func build(kind, operation, resource string, raw json.RawMessage, validate bool) (string, string, json.RawMessage, error) {
	if operation == "identity.read" {
		method, path := "GET", ""
		expected := "$identity"
		switch kind {
		case "github":
			path = "/user"
		case "gitlab":
			path = "/api/v4/user"
		case "graph":
			path = "/v1.0/me"
			expected = "me"
		case "slack":
			method = "POST"
			path = "/api/auth.test"
		default:
			return "", "", nil, ErrInvalid
		}
		if resource != expected {
			return "", "", nil, ErrInvalid
		}
		if validate {
			if e := strict(raw, &struct{}{}); e != nil {
				return "", "", nil, e
			}
		}
		return method, path, json.RawMessage(`{}`), nil
	}
	method := "GET"
	path := ""
	var input any = &struct{}{}
	switch kind {
	case "github", "gitlab":
		if !repoPattern.MatchString(resource) {
			return "", "", nil, ErrInvalid
		}
		path = "/repos/" + resource
		if kind == "gitlab" {
			path = "/api/v4/projects/" + url.PathEscape(resource)
		}
		switch operation {
		case "repo.read":
		case "issues.list":
			path += "/issues?per_page=30"
		case "issues.create":
			method = "POST"
			path += "/issues"
			input = &issueInput{}
		default:
			return "", "", nil, ErrInvalid
		}
	case "graph":
		if resource != "me" {
			return "", "", nil, ErrInvalid
		}
		path = "/v1.0/me"
		switch operation {
		case "mail.list":
			path += "/messages?$top=20"
		case "mail.send":
			method = "POST"
			path += "/messages"
			input = &mailInput{}
		case "calendar.list":
			path += "/events?$top=20"
		case "calendar.create":
			method = "POST"
			path += "/events"
			input = &eventInput{}
		default:
			return "", "", nil, ErrInvalid
		}
	case "slack":
		if !channelPattern.MatchString(resource) {
			return "", "", nil, ErrInvalid
		}
		switch operation {
		case "messages.list":
			path = "/api/conversations.history?channel=" + url.QueryEscape(resource) + "&limit=20"
		case "messages.send":
			method = "POST"
			path = "/api/chat.postMessage"
			input = &textInput{}
		default:
			return "", "", nil, ErrInvalid
		}
	case "mcp":
		method = "POST"
		path = "/mcp"
		switch operation {
		case "tools.list":
			if resource != "$catalog" {
				return "", "", nil, ErrInvalid
			}
		case "tools.call":
			if !toolPattern.MatchString(resource) {
				return "", "", nil, ErrInvalid
			}
			input = &toolInput{}
		default:
			return "", "", nil, ErrInvalid
		}
	default:
		return "", "", nil, ErrInvalid
	}
	if !validate {
		return method, path, json.RawMessage(`{}`), nil
	}
	if e := strict(raw, input); e != nil {
		return "", "", nil, e
	}
	switch v := input.(type) {
	case *issueInput:
		if strings.TrimSpace(v.Title) == "" || len(v.Title) > 256 || len(v.Body) > 32000 {
			return "", "", nil, ErrInvalid
		}
	case *textInput:
		if strings.TrimSpace(v.Text) == "" || len(v.Text) > 16000 {
			return "", "", nil, ErrInvalid
		}
	case *mailInput:
		address, e := mail.ParseAddress(v.To)
		if e != nil || address.Address != v.To || strings.TrimSpace(v.Subject) == "" || len(v.Subject) > 256 || len(v.Body) > 32000 {
			return "", "", nil, ErrInvalid
		}
	case *eventInput:
		begin, e := time.Parse("2006-01-02T15:04:05", v.Start)
		end, ee := time.Parse("2006-01-02T15:04:05", v.End)
		if e != nil || ee != nil || !end.After(begin) || v.TimeZone != "UTC" || strings.TrimSpace(v.Subject) == "" || len(v.Subject) > 256 {
			return "", "", nil, ErrInvalid
		}
	case *toolInput:
		if v.Arguments == nil {
			return "", "", nil, ErrInvalid
		}
	}
	value, _ := json.Marshal(input)
	return method, path, value, nil
}

// request never follows redirects, retries a POST, returns raw network errors,
// or adopts URLs, tools, instructions, or headers received from remote content.
func (m *Manager) request(ctx context.Context, c Connection, secret []byte, method, path string, body any, headers map[string]string) (json.RawMessage, http.Header, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewBufferString(marshal(body))
	}
	req, e := http.NewRequestWithContext(ctx, method, c.Origin+path, reader)
	if e != nil {
		return nil, nil, failure("CONNECTOR_REQUEST_INVALID")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Asimi-Communication/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if len(secret) > 0 {
		if c.Kind == "gitlab" {
			req.Header.Set("PRIVATE-TOKEN", string(secret))
		} else {
			req.Header.Set("Authorization", "Bearer "+string(secret))
		}
	}
	if c.Kind == "graph" {
		req.Header.Set("Prefer", `IdType="ImmutableId"`)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, e := m.client().Do(req)
	if e != nil {
		return nil, nil, failure("CONNECTOR_NETWORK_FAILED")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, failure("CONNECTOR_HTTP_REJECTED")
	}
	var b []byte
	if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		b, e = readSSEResponse(response.Body)
	} else {
		b, e = io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	}
	if e != nil || len(b) > 1024*1024 {
		return nil, nil, failure("CONNECTOR_RESPONSE_INVALID")
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return json.RawMessage(`{}`), response.Header, nil
	}
	if !json.Valid(b) {
		return nil, nil, failure("CONNECTOR_RESPONSE_INVALID")
	}
	// An untrusted service may reflect a credential. Never journal that value.
	if len(secret) > 0 {
		var value any
		json.Unmarshal(b, &value)
		value = redact(value, string(secret))
		b, _ = json.Marshal(value)
	}
	return b, response.Header, nil
}
func redact(value any, secret string) any {
	switch v := value.(type) {
	case string:
		return strings.ReplaceAll(v, secret, "[REDACTED]")
	case []any:
		for i := range v {
			v[i] = redact(v[i], secret)
		}
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			out[strings.ReplaceAll(key, secret, "[REDACTED]")] = redact(item, secret)
		}
		return out
	}
	return value
}
func object(raw json.RawMessage) map[string]any {
	var v map[string]any
	json.Unmarshal(raw, &v)
	return v
}
func array(raw json.RawMessage) bool {
	var items []json.RawMessage
	return len(raw) > 0 && bytes.TrimSpace(raw)[0] == '[' && json.Unmarshal(raw, &items) == nil && items != nil
}
func text(v map[string]any, key string) string { s, _ := v[key].(string); return s }
func number(v map[string]any, key string) int {
	n, _ := v[key].(float64)
	if n != float64(int(n)) {
		return 0
	}
	return int(n)
}
func (m *Manager) execute(ctx context.Context, c Connection, a Action, secret []byte) (json.RawMessage, bool, bool, string) {
	if a.Operation == "identity.read" {
		return m.executeIdentity(ctx, c, a, secret)
	}
	if c.Kind == "mcp" {
		return m.executeMCP(ctx, c, a, secret)
	}
	write := a.Method != "GET"
	input := object(a.Request)
	var body any
	if write {
		body = input
		switch c.Kind {
		case "gitlab":
			body = map[string]any{"title": input["title"], "description": input["body"]}
		case "slack":
			body = map[string]any{"channel": a.Resource, "text": input["text"]}
		case "graph":
			if a.Operation == "calendar.create" {
				body = map[string]any{"subject": input["subject"], "start": map[string]any{"dateTime": input["start"], "timeZone": input["timeZone"]}, "end": map[string]any{"dateTime": input["end"], "timeZone": input["timeZone"]}}
			} else {
				body = map[string]any{"subject": input["subject"], "body": map[string]any{"contentType": "Text", "content": input["body"]}, "toRecipients": []any{map[string]any{"emailAddress": map[string]any{"address": input["to"]}}}}
			}
		}
	}
	raw, _, e := m.request(ctx, c, secret, a.Method, a.Path, body, nil)
	if e != nil {
		return nil, false, write, e.Error()
	}
	if !write {
		verified := false
		switch a.Operation {
		case "repo.read":
			key := "full_name"
			if c.Kind == "gitlab" {
				key = "path_with_namespace"
			}
			verified = strings.EqualFold(text(object(raw), key), a.Resource)
		case "issues.list":
			verified = array(raw)
		case "mail.list", "calendar.list":
			_, verified = object(raw)["value"].([]any)
		case "messages.list":
			v := object(raw)
			_, hasMessages := v["messages"].([]any)
			verified = v["ok"] == true && hasMessages
		}
		if !verified {
			return raw, false, false, "CONNECTOR_RESPONSE_INVALID"
		}
		return raw, true, false, ""
	}
	result := object(raw)
	readPath := ""
	idValue := ""
	switch c.Kind {
	case "github", "gitlab":
		key := "number"
		if c.Kind == "gitlab" {
			key = "iid"
		}
		n := number(result, key)
		if n > 0 {
			readPath = a.Path + "/" + strconv.Itoa(n)
		}
	case "slack":
		idValue = text(result, "ts")
		if result["ok"] == true && text(result, "channel") == a.Resource && idValue != "" {
			readPath = "/api/conversations.history?channel=" + url.QueryEscape(a.Resource) + "&oldest=" + url.QueryEscape(idValue) + "&latest=" + url.QueryEscape(idValue) + "&inclusive=true&limit=1"
		}
	case "graph":
		idValue = text(result, "id")
		if idValue != "" {
			readPath = a.Path + "/" + url.PathEscape(idValue)
		}
	}
	if readPath == "" {
		return raw, false, true, "CONNECTOR_VERIFICATION_UNAVAILABLE"
	}
	if c.Kind == "graph" && a.Operation == "mail.send" {
		if _, _, e = m.request(ctx, c, secret, "POST", readPath+"/send", nil, nil); e != nil {
			return raw, false, true, e.Error()
		}
	}
	read, _, e := m.request(ctx, c, secret, "GET", readPath, nil, nil)
	if e != nil {
		return raw, false, true, "CONNECTOR_READBACK_FAILED"
	}
	verified := false
	witness := object(read)
	switch c.Kind {
	case "github", "gitlab":
		field := "body"
		key := "number"
		if c.Kind == "gitlab" {
			field = "description"
			key = "iid"
		}
		verified = number(witness, key) == number(result, key) && text(witness, "title") == input["title"] && text(witness, field) == input["body"]
	case "slack":
		messages, _ := witness["messages"].([]any)
		if witness["ok"] == true {
			for _, item := range messages {
				msg, ok := item.(map[string]any)
				if ok && text(msg, "ts") == idValue && text(msg, "text") == input["text"] {
					verified = true
				}
			}
		}
	case "graph":
		if a.Operation == "calendar.create" {
			begin, _ := witness["start"].(map[string]any)
			end, _ := witness["end"].(map[string]any)
			verified = text(witness, "id") == idValue && text(witness, "subject") == input["subject"] && sameEventTime(begin, input["start"]) && sameEventTime(end, input["end"])
		} else {
			sent, _, err := m.request(ctx, c, secret, "GET", "/v1.0/me/mailFolders/sentitems?$select=id", nil, nil)
			recipients, _ := witness["toRecipients"].([]any)
			recipientOK := false
			if len(recipients) == 1 {
				recipient, _ := recipients[0].(map[string]any)
				address, _ := recipient["emailAddress"].(map[string]any)
				recipientOK = text(address, "address") == input["to"]
			}
			content, _ := witness["body"].(map[string]any)
			verified = err == nil && text(witness, "id") == idValue && witness["isDraft"] == false && text(witness, "parentFolderId") != "" && text(witness, "parentFolderId") == text(object(sent), "id") && text(witness, "subject") == input["subject"] && recipientOK && strings.EqualFold(text(content, "contentType"), "Text") && text(content, "content") == input["body"]
		}
	}
	if !verified {
		return read, false, true, "CONNECTOR_READBACK_MISMATCH"
	}
	return read, true, true, ""
}
func sameEventTime(v map[string]any, expected any) bool {
	s, ok := expected.(string)
	if !ok || text(v, "timeZone") != "UTC" {
		return false
	}
	parsed, e := time.Parse("2006-01-02T15:04:05.999999999", text(v, "dateTime"))
	want, ee := time.Parse("2006-01-02T15:04:05", s)
	return e == nil && ee == nil && parsed.Equal(want)
}
func (m *Manager) executeMCP(ctx context.Context, c Connection, a Action, secret []byte) (json.RawMessage, bool, bool, string) {
	write := a.Operation == "tools.call"
	headers := map[string]string{"Accept": "application/json, text/event-stream"}
	initialize := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "Asimi", "version": "1.0"}}}
	raw, h, e := m.request(ctx, c, secret, "POST", a.Path, initialize, headers)
	if e != nil {
		return nil, false, write, e.Error()
	}
	result, ok := rpcResult(raw, 1)
	if !ok || text(result, "protocolVersion") != "2025-03-26" {
		return nil, false, write, "CONNECTOR_MCP_NEGOTIATION_FAILED"
	}
	if session := h.Get("Mcp-Session-Id"); session != "" {
		if len(session) > 1024 || strings.ContainsAny(session, "\r\n") {
			return nil, false, write, "CONNECTOR_MCP_NEGOTIATION_FAILED"
		}
		headers["Mcp-Session-Id"] = session
	}
	headers["MCP-Protocol-Version"] = "2025-03-26"
	_, _, e = m.request(ctx, c, secret, "POST", a.Path, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, headers)
	if e != nil {
		return nil, false, write, e.Error()
	}
	method := "tools/list"
	params := map[string]any{}
	if write {
		method = "tools/call"
		params = map[string]any{"name": a.Resource, "arguments": object(a.Request)["arguments"]}
	}
	raw, _, e = m.request(ctx, c, secret, "POST", a.Path, map[string]any{"jsonrpc": "2.0", "id": 2, "method": method, "params": params}, headers)
	if e != nil {
		return nil, false, write, e.Error()
	}
	result, ok = rpcResult(raw, 2)
	if !ok || result["isError"] == true {
		return raw, false, write, "CONNECTOR_MCP_ERROR"
	}
	if write {
		return raw, false, true, "CONNECTOR_VERIFICATION_UNAVAILABLE"
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		return raw, false, false, "CONNECTOR_RESPONSE_INVALID"
	}
	for _, tool := range tools {
		v, ok := tool.(map[string]any)
		if !ok || !toolPattern.MatchString(text(v, "name")) {
			return raw, false, false, "CONNECTOR_RESPONSE_INVALID"
		}
		if _, ok = v["inputSchema"].(map[string]any); !ok {
			return raw, false, false, "CONNECTOR_RESPONSE_INVALID"
		}
	}
	return raw, true, false, ""
}
func rpcResult(raw json.RawMessage, id int) (map[string]any, bool) {
	v := object(raw)
	result, ok := v["result"].(map[string]any)
	return result, ok && text(v, "jsonrpc") == "2.0" && number(v, "id") == id && v["error"] == nil
}

func readSSEResponse(reader io.Reader) ([]byte, error) {
	scan := bufio.NewScanner(io.LimitReader(reader, 1024*1024+1))
	scan.Buffer(make([]byte, 4096), 1024*1024)
	lines := []string{}
	size := 0
	for scan.Scan() {
		line := scan.Text()
		size += len(line) + 1
		if size > 1024*1024 {
			return nil, ErrInvalid
		}
		if line == "" {
			if len(lines) > 0 {
				data := []byte(strings.Join(lines, "\n"))
				var envelope map[string]json.RawMessage
				if json.Unmarshal(data, &envelope) == nil && envelope["id"] != nil && (envelope["result"] != nil || envelope["error"] != nil) {
					return data, nil
				}
			}
			lines = nil
		} else if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	return nil, ErrInvalid
}

func (m *Manager) executeIdentity(ctx context.Context, c Connection, a Action, secret []byte) (json.RawMessage, bool, bool, string) {
	var body any
	if a.Method == "POST" {
		body = map[string]any{}
	}
	raw, _, e := m.request(ctx, c, secret, a.Method, a.Path, body, nil)
	if e != nil {
		return nil, false, false, e.Error()
	}
	v := object(raw)
	key, nameKey, fallback := "id", "name", "login"
	switch c.Kind {
	case "gitlab":
		fallback = "username"
	case "graph":
		nameKey = "displayName"
		fallback = ""
	case "slack":
		if v["ok"] != true {
			return nil, false, false, "CONNECTOR_IDENTITY_UNVERIFIED"
		}
		key = "user_id"
		nameKey = "user"
		fallback = ""
	}
	identityID := text(v, key)
	if identityID == "" {
		if n := number(v, key); n > 0 {
			identityID = strconv.Itoa(n)
		}
	}
	name := text(v, nameKey)
	if name == "" && fallback != "" {
		name = text(v, fallback)
	}
	if identityID == "" || len(identityID) > 256 || len(name) > 256 || strings.ContainsAny(identityID, "\r\n\x00") || strings.ContainsAny(name, "\r\n\x00") {
		return nil, false, false, "CONNECTOR_IDENTITY_UNVERIFIED"
	}
	identity := Identity{ID: identityID, DisplayName: name, VerifiedAt: stamp(), Revision: a.ConnectionRevision}
	out, _ := json.Marshal(identity)
	return out, true, false, ""
}
