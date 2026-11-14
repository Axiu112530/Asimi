package app

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"asimi/internal/domain"
	"asimi/internal/provider"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type querier interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}

func openStore(dir string) (*sql.DB, error) {
	path, err := resolveStorePath(dir)
	if err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Size() < 100 {
			return nil, codedError("STORAGE_CORRUPT")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// Apply connection-local policy again if database/sql replaces a connection.
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	if version > 6 || version < 0 {
		db.Close()
		return nil, codedError("STORAGE_SCHEMA_VERSION")
	}
	var journalMode string
	if err = db.QueryRow("PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil {
		db.Close()
		return nil, err
	}
	if journalMode != "wal" {
		db.Close()
		return nil, codedError("STORAGE_IO")
	}
	if version == 0 {
		if err := applyMigration(db, schemaV1); err != nil {
			db.Close()
			return nil, fmt.Errorf("schema v1: %w", err)
		}
	}
	if version < 2 {
		if err := applyMigration(db, schemaV2); err != nil {
			db.Close()
			return nil, fmt.Errorf("schema v2: %w", err)
		}
	}
	if version < 3 {
		if err := applyMigration(db, schemaV3); err != nil {
			db.Close()
			return nil, fmt.Errorf("schema v3: %w", err)
		}
	}
	if version < 4 {
		if err := applyMigration(db, schemaV4); err != nil {
			db.Close()
			return nil, fmt.Errorf("schema v4: %w", err)
		}
	}
	if version < 5 {
		if err := applyMigration(db, schemaV5); err != nil {
			db.Close()
			return nil, fmt.Errorf("schema v5: %w", err)
		}
	}
	if version < 6 {
		if err := applyMigration(db, schemaV6); err != nil {
			db.Close()
			return nil, fmt.Errorf("schema v6: %w", err)
		}
	}
	if err := applyMigration(db, cognitionSchema+cognitionV11Schema+cognitionPreferenceSchema+cognitionEvolutionSchema+unifiedAgentSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("cognition schema: %w", err)
	}
	return db, nil
}

const schemaV1 = `
CREATE TABLE app_settings(id TEXT PRIMARY KEY CHECK(id='app'),json TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE provider_profiles(id TEXT PRIMARY KEY,json TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE conversations(id TEXT PRIMARY KEY,title TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE task_sessions(id TEXT PRIMARY KEY,conversation_id TEXT NOT NULL REFERENCES conversations(id),status TEXT NOT NULL,json TEXT NOT NULL,draft_json TEXT,language TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE messages(id TEXT PRIMARY KEY,conversation_id TEXT NOT NULL REFERENCES conversations(id),task_id TEXT REFERENCES task_sessions(id),role TEXT NOT NULL,content TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE task_steps(id TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES task_sessions(id),ordinal INTEGER NOT NULL,status TEXT NOT NULL,json TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE approval_records(id TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES task_sessions(id),status TEXT NOT NULL,operation_json TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,expires_at TEXT NOT NULL,decided_at TEXT NOT NULL DEFAULT '');
CREATE TABLE tool_executions(id TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES task_sessions(id),status TEXT NOT NULL,json TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE audit_events(id TEXT PRIMARY KEY,task_id TEXT REFERENCES task_sessions(id),code TEXT NOT NULL,json TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE INDEX tasks_conversation_created ON task_sessions(conversation_id,created_at);
CREATE INDEX tasks_status ON task_sessions(status);
CREATE INDEX messages_conversation_created ON messages(conversation_id,created_at);
CREATE INDEX steps_task_ordinal ON task_steps(task_id,ordinal);
CREATE INDEX approvals_status_expiry ON approval_records(status,expires_at);
CREATE INDEX approvals_task ON approval_records(task_id);
CREATE INDEX executions_task ON tool_executions(task_id,created_at);
CREATE INDEX events_task ON audit_events(task_id,created_at);
PRAGMA user_version=1;`

const schemaV2 = `
CREATE TABLE file_intents (
 execution_id TEXT PRIMARY KEY REFERENCES tool_executions(id),
 task_id TEXT NOT NULL UNIQUE REFERENCES task_sessions(id),
 approval_id TEXT NOT NULL UNIQUE REFERENCES approval_records(id),
 witness_json TEXT,
 settled INTEGER NOT NULL DEFAULT 0 CHECK(settled IN(0,1))
);
CREATE INDEX file_intents_unsettled ON file_intents(settled) WHERE settled=0;
PRAGMA user_version=2;`

func applyMigration(db *sql.DB, schema string) error {
	return storeTransaction(db, func(tx *sql.Tx) error {
		_, err := tx.Exec(schema)
		return err
	})
}

func now() string         { return time.Now().UTC().Format(time.RFC3339Nano) }
func encode(v any) string { b, _ := json.Marshal(v); return string(b) }

func (s *Service) transaction(fn func(*sql.Tx) error) error {
	return storeTransaction(s.db, fn)
}

func storeTransaction(db *sql.DB, fn func(*sql.Tx) error) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			discardConnection(conn)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		// SQLite can leave a transaction active after a failed COMMIT.
		// database/sql has already marked the Tx done, so roll back on its
		// reserved connection before returning it to the pool.
		if _, rollbackErr := conn.ExecContext(ctx, "ROLLBACK"); rollbackErr != nil {
			discardConnection(conn)
		}
		return err
	}
	return nil
}

func discardConnection(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
}

func classifyStorage(err error) string {
	var sqlErr *sqlite.Error
	if errors.As(err, &sqlErr) {
		switch sqlErr.Code() & 0xff {
		case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
			return "STORAGE_BUSY"
		case sqlite3.SQLITE_READONLY, sqlite3.SQLITE_PERM:
			return "STORAGE_READ_ONLY"
		case sqlite3.SQLITE_FULL:
			return "STORAGE_FULL"
		case sqlite3.SQLITE_CORRUPT, sqlite3.SQLITE_NOTADB:
			return "STORAGE_CORRUPT"
		case sqlite3.SQLITE_IOERR, sqlite3.SQLITE_CANTOPEN:
			return "STORAGE_IO"
		case sqlite3.SQLITE_CONSTRAINT:
			return "STORAGE_CONSTRAINT"
		default:
			return "STORAGE_UNAVAILABLE"
		}
	}
	if errors.Is(err, sql.ErrConnDone) || errors.Is(err, driver.ErrBadConn) ||
		(err != nil && err.Error() == "sql: database is closed") {
		return "STORAGE_UNAVAILABLE"
	}
	return ""
}
func saveTask(q querier, t domain.Task, d *provider.Draft, lang string) error {
	_, e := q.Exec(`INSERT INTO task_sessions(id,conversation_id,status,json,draft_json,language,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,json=excluded.json,draft_json=COALESCE(excluded.draft_json,task_sessions.draft_json),language=CASE WHEN excluded.language='' THEN task_sessions.language ELSE excluded.language END,updated_at=excluded.updated_at`, t.ID, t.ConversationID, t.Status, encode(t), nullableDraft(d), lang, t.CreatedAt, t.UpdatedAt)
	if e != nil {
		return e
	}
	for i, step := range t.Steps {
		if _, e = q.Exec(`INSERT INTO task_steps(id,task_id,ordinal,status,json,created_at,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,json=excluded.json,updated_at=excluded.updated_at`, step.ID, t.ID, i, step.Status, encode(step), t.CreatedAt, t.UpdatedAt); e != nil {
			return e
		}
	}
	_, e = q.Exec("UPDATE conversations SET updated_at=? WHERE id=?", t.UpdatedAt, t.ConversationID)
	return e
}
func nullableDraft(d *provider.Draft) any {
	if d == nil {
		return nil
	}
	return encode(d)
}
func saveApproval(q querier, a domain.Approval) error {
	_, e := q.Exec(`INSERT INTO approval_records(id,task_id,status,operation_json,created_at,updated_at,expires_at,decided_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,updated_at=excluded.updated_at,decided_at=excluded.decided_at`, a.ID, a.Operation.TaskID, a.Status, encode(a.Operation), a.CreatedAt, now(), a.ExpiresAt, a.DecidedAt)
	return e
}
func loadApproval(q querier, id string) (domain.Approval, error) {
	var a domain.Approval
	var raw string
	e := q.QueryRow("SELECT id,operation_json,status,created_at,expires_at,decided_at FROM approval_records WHERE id=?", id).Scan(&a.ID, &raw, &a.Status, &a.CreatedAt, &a.ExpiresAt, &a.DecidedAt)
	if e == nil {
		e = json.Unmarshal([]byte(raw), &a.Operation)
	}
	return a, e
}
func (s *Service) taskLocked(id string) (domain.Task, error) {
	var t domain.Task
	var raw string
	e := s.db.QueryRow("SELECT json FROM task_sessions WHERE id=?", id).Scan(&raw)
	if e != nil {
		return t, e
	}
	if e = json.Unmarshal([]byte(raw), &t); e != nil {
		return t, e
	}
	var aid string
	e = s.db.QueryRow("SELECT id FROM approval_records WHERE task_id=? ORDER BY created_at DESC LIMIT 1", id).Scan(&aid)
	if e == nil {
		a, err := loadApproval(s.db, aid)
		if err != nil {
			return t, err
		}
		t.Approval = &a
	} else if !errors.Is(e, sql.ErrNoRows) {
		return t, e
	}
	return t, nil
}
func (s *Service) taskIDsLocked(conversationID string) ([]string, error) {
	query := "SELECT id FROM task_sessions"
	args := []any{}
	if conversationID != "" {
		query += " WHERE conversation_id=?"
		args = append(args, conversationID)
	}
	query += " ORDER BY created_at DESC,id DESC"
	rows, e := s.db.Query(query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Service) tasksLocked(conversationID string) ([]domain.Task, error) {
	ids, e := s.taskIDsLocked(conversationID)
	if e != nil {
		return nil, e
	}
	out := []domain.Task{}
	for _, id := range ids {
		t, e := s.taskLocked(id)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, nil
}
func (s *Service) conversationLocked(id string, full bool) (domain.Conversation, error) {
	c := domain.Conversation{Messages: []domain.Message{}, Tasks: []domain.Task{}}
	e := s.db.QueryRow("SELECT id,title,created_at,updated_at FROM conversations WHERE id=?", id).Scan(&c.ID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	if e != nil {
		return c, e
	}
	var playground int
	if modeErr := s.db.QueryRow("SELECT playground FROM ai_conversations WHERE conversation_id=?", id).Scan(&playground); modeErr != nil && !errors.Is(modeErr, sql.ErrNoRows) {
		return c, modeErr
	}
	c.Playground = playground == 1
	if !full {
		return c, nil
	}
	rows, e := s.db.Query("SELECT id,conversation_id,COALESCE(task_id,''),role,content,created_at FROM messages WHERE conversation_id=? AND NOT EXISTS (SELECT 1 FROM ai_conversations ac WHERE ac.conversation_id=messages.conversation_id AND (ac.deleted=1 OR messages.created_at < ac.context_since)) ORDER BY created_at,messages.rowid", id)
	if e != nil {
		return c, e
	}
	for rows.Next() {
		var m domain.Message
		if e = rows.Scan(&m.ID, &m.ConversationID, &m.TaskID, &m.Role, &m.Content, &m.CreatedAt); e != nil {
			rows.Close()
			return c, e
		}
		c.Messages = append(c.Messages, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return c, e
	}
	if e = s.enrichMessagesLocked(c.Messages); e != nil {
		return c, e
	}
	c.Tasks, e = s.tasksLocked(id)
	return c, e
}
func insertMessage(q querier, m domain.Message) error {
	_, e := q.Exec("INSERT INTO messages(id,conversation_id,task_id,role,content,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", m.ID, m.ConversationID, m.TaskID, m.Role, m.Content, m.CreatedAt, m.CreatedAt)
	return e
}
func audit(q querier, taskID, code string, params map[string]string) error {
	a := domain.AuditEvent{ID: newID(), TaskID: taskID, Code: code, Params: params, CreatedAt: now()}
	var tid any = taskID
	if taskID == "" {
		tid = nil
	}
	_, e := q.Exec("INSERT INTO audit_events(id,task_id,code,json,created_at,updated_at) VALUES(?,?,?,?,?,?)", a.ID, tid, a.Code, encode(a), a.CreatedAt, a.CreatedAt)
	return e
}
func saveExecution(q querier, x domain.ToolExecution) error {
	_, e := q.Exec(`INSERT INTO tool_executions(id,task_id,status,json,created_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,json=excluded.json,updated_at=excluded.updated_at`, x.ID, x.TaskID, x.Status, encode(x), x.CreatedAt, x.UpdatedAt)
	return e
}
