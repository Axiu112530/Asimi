// Package communication gates every external connector action through durable,
// revision-bound, single-use approval. All returned remote content is untrusted.
package communication

import (
	"asimi/internal/credential"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Scope struct {
	Resource   string   `json:"resource"`
	Operations []string `json:"operations"`
}
type Identity struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	VerifiedAt  string `json:"verifiedAt"`
	Revision    int    `json:"revision"`
}
type Connection struct {
	Identity         *Identity `json:"identity,omitempty"`
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Kind             string    `json:"kind"`
	Origin           string    `json:"origin"`
	Scopes           []Scope   `json:"scopes"`
	Revision         int       `json:"revision"`
	Enabled          bool      `json:"enabled"`
	CredentialStatus string    `json:"credentialStatus"`
	CreatedAt        string    `json:"createdAt"`
	UpdatedAt        string    `json:"updatedAt"`
}
type SaveConnectionInput struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Origin     string  `json:"origin"`
	Scopes     []Scope `json:"scopes"`
	Revision   int     `json:"revision"`
	Enabled    bool    `json:"enabled"`
	Credential string  `json:"credential,omitempty"`
}
type PrepareInput struct {
	PublicOnly   bool            `json:"publicOnly,omitempty"`
	ConnectionID string          `json:"connectionId"`
	Operation    string          `json:"operation"`
	Resource     string          `json:"resource"`
	Request      json.RawMessage `json:"request"`
}
type Action struct {
	ID                 string          `json:"id"`
	Status             string          `json:"status"`
	Hash               string          `json:"hash"`
	ExpiresAt          string          `json:"expiresAt"`
	ConnectionID       string          `json:"connectionId"`
	ConnectionRevision int             `json:"connectionRevision"`
	Origin             string          `json:"origin"`
	Method             string          `json:"method"`
	Path               string          `json:"path"`
	Operation          string          `json:"operation"`
	Resource           string          `json:"resource"`
	Request            json.RawMessage `json:"request"`
	Verified           bool            `json:"verified"`
	Result             json.RawMessage `json:"result,omitempty"`
	ErrorCode          string          `json:"errorCode,omitempty"`
	CreatedAt          string          `json:"createdAt"`
	UpdatedAt          string          `json:"updatedAt"`
	Untrusted          bool            `json:"untrusted"`
}
type Options struct{ AllowLoopbackHTTP bool }
type Manager struct {
	db      *sql.DB
	mu      sync.Mutex
	options Options
	active  map[string]activeAction
}
type activeAction struct {
	connection string
	cancel     context.CancelFunc
}

var ErrInvalid = errors.New("CONNECTOR_INVALID")

func failure(code string) error { return errors.New(code) }
func stamp() string             { return time.Now().UTC().Format(time.RFC3339Nano) }
func id() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("system entropy unavailable")
	}
	return hex.EncodeToString(b[:])
}
func marshal(v any) string { b, _ := json.Marshal(v); return string(b) }

const schema = `CREATE TABLE IF NOT EXISTS communication_connections(id TEXT PRIMARY KEY,revision INTEGER NOT NULL,json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS communication_actions(id TEXT PRIMARY KEY,connection_id TEXT NOT NULL,status TEXT NOT NULL,json TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS communication_actions_connection ON communication_actions(connection_id,created_at);
CREATE TABLE IF NOT EXISTS communication_credentials(connection_id TEXT PRIMARY KEY REFERENCES communication_connections(id) ON DELETE CASCADE,encrypted_blob BLOB NOT NULL);`

func New(db *sql.DB, options Options) (*Manager, error) {
	if _, e := db.Exec(schema); e != nil {
		return nil, e
	}
	m := &Manager{db: db, options: options, active: map[string]activeAction{}}
	actions, e := m.unsettledActions()
	if e != nil {
		return nil, e
	}
	for _, a := range actions {
		if a.Status == "RUNNING" {
			a.Status = "UNKNOWN"
			a.ErrorCode = "CONNECTOR_RECOVERY_REQUIRED"
		} else {
			a.Status = "EXPIRED"
			a.ErrorCode = "CONNECTOR_APPROVAL_EXPIRED"
		}
		if e = m.putAction(a); e != nil {
			return nil, e
		}
	}
	return m, nil
}
func normalizeOrigin(origin, kind string, allow bool) (string, error) {
	u, e := url.Parse(origin)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" {
		return "", ErrInvalid
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	loop := strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loop && (allow || kind == "mcp")) {
		return "", ErrInvalid
	}
	if !allow {
		expected := ""
		switch kind {
		case "github":
			expected = "api.github.com"
		case "graph":
			expected = "graph.microsoft.com"
		case "slack":
			expected = "slack.com"
		}
		if expected != "" && (!strings.EqualFold(host, expected) || (u.Port() != "" && u.Port() != "443")) {
			return "", failure("CONNECTOR_ORIGIN_DENIED")
		}
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = ""
	return u.String(), nil
}
func (m *Manager) connection(q interface{ QueryRow(string, ...any) *sql.Row }, key string) (Connection, error) {
	var raw string
	var c Connection
	if e := q.QueryRow("SELECT json FROM communication_connections WHERE id=?", key).Scan(&raw); e != nil {
		return c, failure("CONNECTOR_NOT_FOUND")
	}
	if e := json.Unmarshal([]byte(raw), &c); e != nil {
		return c, e
	}
	return c, nil
}
func (m *Manager) ListConnections() ([]Connection, error) {
	rows, e := m.db.Query("SELECT json FROM communication_connections ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		var raw string
		var c Connection
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &c); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (m *Manager) SaveConnection(in SaveConnectionInput) (Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var c Connection
	origin, e := normalizeOrigin(in.Origin, in.Kind, m.options.AllowLoopbackHTTP)
	if e != nil {
		return c, e
	}
	if len(in.Scopes) == 0 || len(in.Scopes) > 32 || len(in.Name) > 200 || len(in.Credential) > 16384 || strings.ContainsAny(in.Credential, "\r\n") {
		return c, ErrInvalid
	}
	for _, scope := range in.Scopes {
		if len(scope.Operations) == 0 || len(scope.Operations) > 16 {
			return c, ErrInvalid
		}
		for _, op := range scope.Operations {
			if _, _, _, e = build(in.Kind, op, scope.Resource, json.RawMessage(`{}`), false); e != nil {
				return c, e
			}
		}
	}
	tx, e := m.db.Begin()
	if e != nil {
		return c, e
	}
	defer tx.Rollback()
	created := stamp()
	revision := 1
	credentialStatus := "missing"
	if in.Kind == "github" {
		credentialStatus = "not_required"
	}
	if in.ID != "" {
		old, err := m.connection(tx, in.ID)
		if err != nil {
			return c, err
		}
		if old.Revision != in.Revision {
			return c, failure("CONNECTOR_REVISION_CHANGED")
		}
		created = old.CreatedAt
		revision = old.Revision + 1
		credentialStatus = old.CredentialStatus
		if old.Origin != origin || old.Kind != in.Kind {
			if e = deleteConnectionCredential(tx, in.ID); e != nil {
				return c, e
			}
			credentialStatus = "missing"
			if in.Kind == "github" {
				credentialStatus = "not_required"
			}
		}
	} else {
		in.ID = id()
	}
	if in.Credential != "" {
		credentialStatus = "stored"
	}
	c = Connection{ID: in.ID, Name: in.Name, Kind: in.Kind, Origin: origin, Scopes: in.Scopes, Revision: revision, Enabled: in.Enabled, CredentialStatus: credentialStatus, CreatedAt: created, UpdatedAt: stamp()}
	if _, e = tx.Exec("INSERT INTO communication_connections(id,revision,json) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,json=excluded.json", c.ID, c.Revision, marshal(c)); e != nil {
		return c, e
	}
	if in.Credential != "" {
		secret := []byte(in.Credential)
		defer credential.Wipe(secret)
		blob, err := credential.Protect(secret)
		if err != nil {
			return c, err
		}
		defer credential.Wipe(blob)
		if _, err = tx.Exec("INSERT INTO communication_credentials(connection_id,encrypted_blob) VALUES(?,?) ON CONFLICT(connection_id) DO UPDATE SET encrypted_blob=excluded.encrypted_blob", c.ID, blob); err != nil {
			return c, err
		}
	}
	if e = tx.Commit(); e != nil {
		return c, e
	}
	m.cancelConnection(c.ID)
	return c, nil
}
func deleteConnectionCredential(tx *sql.Tx, connectionID string) error {
	_, err := tx.Exec("DELETE FROM communication_credentials WHERE connection_id=?", connectionID)
	return err
}

func (m *Manager) connectionCredential(connectionID string) ([]byte, error) {
	var blob []byte
	if err := m.db.QueryRow("SELECT encrypted_blob FROM communication_credentials WHERE connection_id=?", connectionID).Scan(&blob); err != nil {
		return nil, credential.ErrUnavailable
	}
	defer credential.Wipe(blob)
	return credential.Unprotect(blob)
}

func (m *Manager) cancelConnection(key string) {
	for _, a := range m.active {
		if a.connection == key {
			a.cancel()
		}
	}
}
func (m *Manager) Revoke(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, e := m.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	c, e := m.connection(tx, key)
	if e != nil {
		return e
	}
	c.Enabled = false
	c.Identity = nil
	c.Revision++
	c.CredentialStatus = "missing"
	c.UpdatedAt = stamp()
	if e = deleteConnectionCredential(tx, key); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE communication_connections SET revision=?,json=? WHERE id=?", c.Revision, marshal(c), c.ID); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	m.cancelConnection(key)
	return nil
}
func allowed(c Connection, operation, resource string) bool {
	if !c.Enabled {
		return false
	}
	for _, s := range c.Scopes {
		if s.Resource == resource {
			for _, op := range s.Operations {
				if op == operation {
					return true
				}
			}
		}
	}
	return false
}
func actionHash(a Action) string {
	b, _ := json.Marshal(struct {
		ID, ConnectionID, Origin, Method, Path, Operation, Resource, ExpiresAt string
		Revision                                                               int
		Request                                                                json.RawMessage
	}{a.ID, a.ConnectionID, a.Origin, a.Method, a.Path, a.Operation, a.Resource, a.ExpiresAt, a.ConnectionRevision, a.Request})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (m *Manager) Prepare(in PrepareInput) (Action, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var a Action
	c, e := m.connection(m.db, in.ConnectionID)
	if e != nil {
		return a, e
	}
	if in.PublicOnly && (c.Kind != "github" || c.Origin != "https://api.github.com" || c.CredentialStatus != "not_required" || in.Operation != "repo.read") {
		return a, failure("CONNECTOR_SCOPE_DENIED")
	}
	if !allowed(c, in.Operation, in.Resource) {
		return a, failure("CONNECTOR_SCOPE_DENIED")
	}
	if len(in.Request) == 0 {
		in.Request = json.RawMessage(`{}`)
	}
	if len(in.Request) > 64*1024 {
		return a, ErrInvalid
	}
	method, path, request, e := build(c.Kind, in.Operation, in.Resource, in.Request, true)
	if e != nil {
		return a, e
	}
	a = Action{ID: id(), Status: "NEEDS_APPROVAL", ExpiresAt: time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano), ConnectionID: c.ID, ConnectionRevision: c.Revision, Origin: c.Origin, Method: method, Path: path, Operation: in.Operation, Resource: in.Resource, Request: request, CreatedAt: stamp(), UpdatedAt: stamp(), Untrusted: true}
	a.Hash = actionHash(a)
	_, e = m.db.Exec("INSERT INTO communication_actions(id,connection_id,status,json,created_at,updated_at) VALUES(?,?,?,?,?,?)", a.ID, a.ConnectionID, a.Status, marshal(a), a.CreatedAt, a.UpdatedAt)
	return a, e
}
func (m *Manager) action(key string) (Action, error) {
	var a Action
	var raw string
	if e := m.db.QueryRow("SELECT json FROM communication_actions WHERE id=?", key).Scan(&raw); e != nil {
		return a, failure("CONNECTOR_ACTION_NOT_FOUND")
	}
	e := json.Unmarshal([]byte(raw), &a)
	return a, e
}
func (m *Manager) putAction(a Action) error {
	a.UpdatedAt = stamp()
	_, e := m.db.Exec("UPDATE communication_actions SET status=?,json=?,updated_at=? WHERE id=?", a.Status, marshal(a), a.UpdatedAt, a.ID)
	return e
}

// ExpirePending settles elapsed approvals without issuing a request. Both
// columns are updated together so the unified task trigger sees the same state.
func (m *Manager) ExpirePending() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := stamp()
	_, err := m.db.Exec(`UPDATE communication_actions
		SET status='EXPIRED', updated_at=?,
		    json=json_set(json,'$.status','EXPIRED','$.errorCode','CONNECTOR_APPROVAL_EXPIRED','$.updatedAt',?)
		WHERE status='NEEDS_APPROVAL'
		  AND COALESCE(julianday(json_extract(json,'$.expiresAt')),0)<=julianday(?)`, now, now, now)
	return err
}

func (m *Manager) ListActions(connection string) ([]Action, error) {
	if err := m.ExpirePending(); err != nil {
		return nil, err
	}
	q := "SELECT json FROM communication_actions"
	args := []any{}
	if connection != "" {
		q += " WHERE connection_id=?"
		args = append(args, connection)
	}
	q += " ORDER BY created_at DESC LIMIT 500"
	rows, e := m.db.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Action{}
	for rows.Next() {
		var raw string
		var a Action
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &a); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (m *Manager) checkApproval(key, hash string) (Action, Connection, error) {
	a, e := m.action(key)
	if e != nil {
		return a, Connection{}, e
	}
	if a.Status != "NEEDS_APPROVAL" {
		return a, Connection{}, failure("CONNECTOR_APPROVAL_CONSUMED")
	}
	expires, e := time.Parse(time.RFC3339Nano, a.ExpiresAt)
	if e != nil || !time.Now().Before(expires) {
		a.Status = "EXPIRED"
		if e = m.putAction(a); e != nil {
			return a, Connection{}, e
		}
		return a, Connection{}, failure("CONNECTOR_APPROVAL_EXPIRED")
	}
	if a.Hash != hash || actionHash(a) != hash {
		return a, Connection{}, failure("CONNECTOR_APPROVAL_MISMATCH")
	}
	c, e := m.connection(m.db, a.ConnectionID)
	if e != nil {
		return a, c, e
	}
	if c.Revision != a.ConnectionRevision || c.Origin != a.Origin || !allowed(c, a.Operation, a.Resource) {
		return a, c, failure("CONNECTOR_REVISION_CHANGED")
	}
	return a, c, nil
}
func (m *Manager) Reject(key, hash string) (Action, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, _, e := m.checkApproval(key, hash)
	if e != nil {
		return a, e
	}
	a.Status = "REJECTED"
	e = m.putAction(a)
	return a, e
}
func (m *Manager) Approve(parent context.Context, key, hash string) (Action, error) {
	m.mu.Lock()
	a, c, e := m.checkApproval(key, hash)
	if e != nil {
		m.mu.Unlock()
		return a, e
	}
	if parent.Err() != nil {
		m.mu.Unlock()
		return a, failure("CONNECTOR_STOPPED")
	}
	var secret []byte
	if c.CredentialStatus == "stored" {
		secret, e = m.connectionCredential(c.ID)
		if e != nil {
			m.mu.Unlock()
			return a, e
		}
	} else if (a.Operation == "identity.read" || (c.Kind != "github" && c.Kind != "mcp")) && !m.options.AllowLoopbackHTTP {
		m.mu.Unlock()
		return a, failure("CONNECTOR_CREDENTIAL_REQUIRED")
	}
	defer credential.Wipe(secret)
	a.Status = "RUNNING"
	if e = m.putAction(a); e != nil {
		m.mu.Unlock()
		return a, e
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	m.active[a.ID] = activeAction{c.ID, cancel}
	m.mu.Unlock()
	result, verified, write, code := m.execute(ctx, c, a, secret)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, a.ID)
	cancel()
	if verified && a.Operation == "identity.read" {
		current, err := m.connection(m.db, c.ID)
		if err != nil {
			return a, err
		}
		if current.Revision == a.ConnectionRevision && current.Enabled {
			var identity Identity
			if err = json.Unmarshal(result, &identity); err != nil {
				return a, err
			}
			current.Identity = &identity
			if _, err = m.db.Exec("UPDATE communication_connections SET json=? WHERE id=? AND revision=?", marshal(current), current.ID, current.Revision); err != nil {
				return a, err
			}
		}
	}
	a.Result = result
	a.Verified = verified
	a.ErrorCode = code
	a.Status = "SUCCEEDED"
	if !verified {
		a.Status = "FAILED"
		if write {
			a.Status = "UNKNOWN"
		}
	}
	e = m.putAction(a)
	return a, e
}
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.active {
		a.cancel()
	}
	actions, e := m.unsettledActions()
	if e != nil {
		return e
	}
	for _, a := range actions {
		if a.Status == "NEEDS_APPROVAL" {
			a.Status = "REJECTED"
			a.ErrorCode = "CONNECTOR_STOPPED"
			if e = m.putAction(a); e != nil {
				return e
			}
		}
	}
	return nil
}
func (m *Manager) client() *http.Client {
	return &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return failure("CONNECTOR_REDIRECT_DENIED") }}
}

// Safety transitions must not inherit the public journal display limit.
func (m *Manager) unsettledActions() ([]Action, error) {
	rows, e := m.db.Query("SELECT json FROM communication_actions WHERE status IN ('RUNNING','NEEDS_APPROVAL')")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Action{}
	for rows.Next() {
		var raw string
		var a Action
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &a); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Cancel reduces authority only. It consumes an unapproved action or cancels
// the exact active HTTP context; consumed network effects remain uncertain.
func (m *Manager) Cancel(key string) (Action, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, e := m.action(key)
	if e != nil {
		return a, e
	}
	switch a.Status {
	case "NEEDS_APPROVAL":
		a.Status = "REJECTED"
		a.ErrorCode = "CONNECTOR_STOPPED"
		e = m.putAction(a)
		return a, e
	case "RUNNING":
		active, ok := m.active[key]
		if !ok {
			return a, failure("CONNECTOR_RECOVERY_REQUIRED")
		}
		active.cancel()
		return a, nil
	default:
		return a, failure("CONNECTOR_ACTION_TERMINAL")
	}
}
