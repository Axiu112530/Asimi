// Package app exposes only business operations; task state and permits are
// private to the local runtime. The Wails adapter binds Call, not this package's
// repositories or tool executor.
package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"deskpilot/internal/domain"
	"deskpilot/internal/provider"
	"deskpilot/internal/tools"
)

type executor interface {
	Prepare(string, string, string, string, time.Time) (domain.Operation, error)
	Execute(context.Context, domain.Operation, func(context.Context, domain.Operation) error, ...tools.ExecutionHooks) (domain.Verification, error)
	Inspect(domain.Operation, domain.FileWitness) (domain.Verification, error)
	Workspace() string
}
type control struct {
	ctx    context.Context
	cancel context.CancelFunc
}
type workerFault struct {
	code         string
	verification *domain.Verification
}
type Service struct {
	agent02Secret    []byte
	mvp              mvpRuntime
	desktopControl   controlState
	screens          screenCache
	mu               sync.Mutex
	db               *sql.DB
	registry         executor
	provider         provider.Provider
	dataDir          string
	emit             func(string, any)
	controls         map[string]control
	admission        sync.Mutex // Never held during database or filesystem work.
	wg               sync.WaitGroup
	lifetime         context.Context
	stop             context.CancelFunc
	releaseProfile   func() error
	checkpoint       func(string)
	workerFaults     map[string]workerFault
	workerErr        error
	closeOnce        sync.Once
	closeErr         error
	chatControls     map[string]chatControl
	attachmentMu     sync.Mutex
	removeAttachment func(string) error
}

func newID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("system entropy unavailable")
	}
	return hex.EncodeToString(b[:])
}
func New(dataDir string, registry *tools.Registry, emit func(string, any)) (*Service, error) {
	if registry == nil {
		return nil, errors.New("registry is required")
	}
	if e := os.MkdirAll(dataDir, 0700); e != nil {
		return nil, e
	}
	release, e := acquireProfile(dataDir)
	if e != nil {
		return nil, e
	}
	db, e := openStore(dataDir)
	if e != nil {
		return nil, errors.Join(e, release())
	}
	lifetime, stop := context.WithCancel(context.Background())
	s := &Service{db: db, registry: registry, provider: provider.Mock{}, dataDir: dataDir, emit: emit, controls: map[string]control{}, lifetime: lifetime, stop: stop, releaseProfile: release, workerFaults: map[string]workerFault{}, chatControls: map[string]chatControl{}}
	e = s.transaction(func(tx *sql.Tx) error {
		stamp := now()
		defaults := domain.Settings{AppLanguage: "en-US", AgentLanguage: "follow-app", Theme: "light", Startup: "welcome"}
		if _, e := tx.Exec("INSERT OR IGNORE INTO app_settings(id,json,created_at,updated_at) VALUES('app',?,?,?)", encode(defaults), stamp, stamp); e != nil {
			return e
		}
		for _, p := range provider.Profiles() {
			if _, e := tx.Exec(`INSERT INTO provider_profiles(id,json,created_at,updated_at) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET json=excluded.json,updated_at=excluded.updated_at`, p.ID, encode(p), stamp, stamp); e != nil {
				return e
			}
		}
		return nil
	})
	if e == nil {
		e = s.initializeAI()
	}
	if e == nil {
		e = s.recoverTasks()
	}
	if e == nil {
		e = recoverComputer(db)
	}
	if e == nil {
		e = s.initializeMVP()
	}
	if e != nil {
		stop()
		return nil, errors.Join(e, db.Close(), release())
	}
	s.wg.Add(1)
	go s.expiryLoop()
	return s, nil
}

func (s *Service) changed(conversationID, taskID string) {
	if s.emit != nil {
		s.emit("deskpilot:changed", map[string]string{"conversationId": conversationID, "taskId": taskID})
	}
}

func (s *Service) markCheckpoint(name string) {
	if s.checkpoint != nil {
		s.checkpoint(name)
	}
}

// workerFailureLocked exposes an unsaved outcome without changing its durable
// record. Restart will reconcile any unsettled intent using the file witness.
func (s *Service) workerFailureLocked(conversationID, taskID string, e error, evidence ...domain.Verification) {
	code := errorCode(e)
	if taskID != "" {
		fault := workerFault{code: code}
		if len(evidence) > 0 {
			v := evidence[0]
			fault.verification = &v
		}
		s.workerFaults[taskID] = fault
	}
	if s.workerErr == nil {
		s.workerErr = codedError(code)
	}
	log.Printf("Asimi task persistence: %s (task %s)", code, taskID)
	if s.emit != nil {
		s.emit("deskpilot:changed", map[string]string{"conversationId": conversationID, "taskId": taskID, "errorCode": code})
	}
}

func (s *Service) taskForDisplay(t domain.Task) domain.Task {
	if fault, ok := s.workerFaults[t.ID]; ok {
		lang := s.taskLanguageLocked(t.ID)
		t.Status = "RECOVERY_REQUIRED"
		t.ErrorCode = fault.code
		t.Summary = provider.Text(lang, "The execution result could not be saved. Reopen Asimi to reconcile any recorded file evidence.", "执行结果未能保存。请重新打开 Asimi 以核对已记录的文件证据。")
		markUnfinishedSteps(&t, "SKIPPED")
		if fault.verification != nil {
			t.Verification = fault.verification
			if t.Verification.Created {
				t.Summary = provider.Text(lang, "The file was committed. Asimi could not save the result. Reopen Asimi to reconcile the recorded file evidence.", "文件已提交，但 Asimi 未能保存结果。请重新打开 Asimi 以核对已记录的文件证据。")
				if len(t.Steps) > 1 {
					t.Steps[1].Status = "COMPLETED"
					t.Steps[1].Result = t.Verification.Path
				}
				if len(t.Steps) > 2 && t.Verification.Passed {
					t.Steps[2].Status = "COMPLETED"
					t.Steps[2].Result = t.Verification.ActualHash
				}
			}
		}
	}
	return t
}
func fail(code string) domain.Reply { return domain.Reply{Error: &domain.Error{Code: code}} }
func result(data any, e error) domain.Reply {
	if e != nil {
		return fail(errorCode(e))
	}
	return domain.Reply{Data: data}
}

type codedError string

func (e codedError) Error() string { return string(e) }
func (e codedError) Code() string  { return string(e) }
func errorCode(e error) string {
	if errors.Is(e, sql.ErrNoRows) {
		return "NOT_FOUND"
	}
	if errors.Is(e, context.Canceled) {
		return "CANCELLED"
	}
	var c interface{ Code() string }
	if errors.As(e, &c) {
		return c.Code()
	}
	if code := classifyStorage(e); code != "" {
		return code
	}
	return "INTERNAL_ERROR"
}
func decode(payload string, dst any) error {
	if payload == "" {
		payload = "{}"
	}
	if len(payload) > 262144 || !strings.HasPrefix(strings.TrimSpace(payload), "{") {
		return codedError("INVALID_REQUEST")
	}
	d := json.NewDecoder(strings.NewReader(payload))
	d.DisallowUnknownFields()
	if e := d.Decode(dst); e != nil {
		return codedError("INVALID_REQUEST")
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return codedError("INVALID_REQUEST")
	}
	return nil
}

// Call is deliberately an allowlist. There is no tool-by-name, arbitrary file, SQL or shell binding.
// Real model configuration and requests use a separate text/vision-only service.
func (s *Service) Call(method, payload string) domain.Reply {
	if !s.beginCall() {
		return fail("SERVICE_CLOSED")
	}
	defer s.wg.Done()
	if reply, handled := s.callMVP(method, payload); handled {
		return reply
	}
	if reply, handled := s.callControl(method, payload); handled {
		return reply
	}
	var controlStopErr error
	if method == "StopAll" {
		if e := decode(payload, &struct{}{}); e != nil {
			return fail(errorCode(e))
		}
		defer s.beginMVPStop()()
		controlStopErr = s.stopDesktopControl()
		controlStopErr = errors.Join(controlStopErr, s.stopMVP())
	}
	if reply, handled := s.callScreen(method, payload); handled {
		return reply
	}
	if reply, handled := s.callAI(method, payload); handled {
		return reply
	}
	if method == "SendMessage" || method == "CreateTask" {
		var q struct {
			ConversationID string `json:"conversationId"`
			Content        string `json:"content"`
		}
		if e := decode(payload, &q); e != nil {
			return fail(errorCode(e))
		}
		data, e := s.send(q.ConversationID, q.Content)
		return result(data, e)
	}
	if method == "ApproveAction" || method == "RejectAction" {
		var q struct {
			ID string `json:"id"`
		}
		if e := decode(payload, &q); e != nil {
			return fail(errorCode(e))
		}
		data, e := s.decide(q.ID, method == "ApproveAction")
		return result(data, e)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifetime.Err() != nil {
		return fail("SERVICE_CLOSED")
	}
	if method != "StopAll" && method != "CancelTask" {
		if e := s.expireLocked(); e != nil {
			return fail(errorCode(e))
		}
	}
	switch method {
	case "GetAppInfo":
		if e := decode(payload, &struct{}{}); e != nil {
			return fail(errorCode(e))
		}
		return result(map[string]any{"version": "0.2.1", "goVersion": runtime.Version(), "wailsVersion": "3.1.0", "dataDir": s.dataDir, "workspace": s.registry.Workspace(), "mode": "MOCK_LOCAL", "provider": "mock", "model": "mock-local-v1"}, nil)
	case "GetSettings":
		if e := decode(payload, &struct{}{}); e != nil {
			return fail(errorCode(e))
		}
		v, e := s.settingsLocked()
		return result(v, e)
	case "UpdateSettings":
		var v domain.Settings
		if e := decode(payload, &v); e != nil {
			return fail(errorCode(e))
		}
		if !validSettings(v) {
			return fail("INVALID_SETTINGS")
		}
		e := s.transaction(func(tx *sql.Tx) error {
			if _, e := tx.Exec("UPDATE app_settings SET json=?,updated_at=? WHERE id='app'", encode(v), now()); e != nil {
				return e
			}
			return audit(tx, "", "SETTINGS_UPDATED", nil)
		})
		if e == nil {
			s.changed("", "")
		}
		return result(v, e)
	case "CreateConversation":
		if e := decode(payload, &struct{}{}); e != nil {
			return fail(errorCode(e))
		}
		v, e := s.createConversationLocked()
		return result(v, e)
	case "ListConversations":
		if e := decode(payload, &struct{}{}); e != nil {
			return fail(errorCode(e))
		}
		rows, e := s.db.Query("SELECT id,title,created_at,updated_at,COALESCE((SELECT playground FROM ai_conversations ac WHERE ac.conversation_id=conversations.id),0) FROM conversations WHERE NOT EXISTS (SELECT 1 FROM ai_conversations ac WHERE ac.conversation_id=conversations.id AND ac.deleted=1 AND NOT EXISTS (SELECT 1 FROM ai_attachment_cleanup cleanup WHERE cleanup.conversation_id=conversations.id)) ORDER BY updated_at DESC,id DESC")
		if e != nil {
			return fail(errorCode(e))
		}
		defer rows.Close()
		v := []domain.Conversation{}
		for rows.Next() {
			c := domain.Conversation{Messages: []domain.Message{}, Tasks: []domain.Task{}}
			if e = rows.Scan(&c.ID, &c.Title, &c.CreatedAt, &c.UpdatedAt, &c.Playground); e != nil {
				return fail(errorCode(e))
			}
			v = append(v, c)
		}
		return result(v, rows.Err())
	case "GetConversation", "GetTask", "CancelTask":
		var q struct {
			ID     string `json:"id"`
			Reason string `json:"reason,omitempty"`
		}
		if e := decode(payload, &q); e != nil {
			return fail(errorCode(e))
		}
		if q.ID == "" {
			return fail("INVALID_REQUEST")
		}
		if method == "GetConversation" {
			v, e := s.conversationLocked(q.ID, true)
			for i := range v.Tasks {
				v.Tasks[i] = s.taskForDisplay(v.Tasks[i])
			}
			return result(v, e)
		}
		if method == "CancelTask" {
			v, e := s.cancelLocked(q.ID, q.Reason)
			return result(v, e)
		}
		v, e := s.taskLocked(q.ID)
		return result(s.taskForDisplay(v), e)
	case "ListTasks":
		if e := decode(payload, &struct{}{}); e != nil {
			return fail(errorCode(e))
		}
		v, e := s.tasksLocked("")
		for i := range v {
			v[i] = s.taskForDisplay(v[i])
		}
		return result(v, e)
	case "StopAll":
		if e := decode(payload, &struct{}{}); e != nil {
			return fail(errorCode(e))
		}
		chatCount := s.cancelAllChatsLocked()
		n, e := s.stopAllLocked()
		n += chatCount
		return result(map[string]int{"cancelled": n}, errors.Join(controlStopErr, e))
	case "GetPendingApprovals", "GetApprovalHistory":
		if e := decode(payload, &struct{}{}); e != nil {
			return fail(errorCode(e))
		}
		v, e := s.approvalsLocked(method == "GetPendingApprovals")
		return result(v, e)
	case "ListProviders", "ListModels", "GetModelCapabilities":
		var q struct {
			ProviderID string `json:"providerId"`
			ModelID    string `json:"modelId"`
		}
		if e := decode(payload, &q); e != nil {
			return fail(errorCode(e))
		}
		profiles := provider.Profiles()
		if method == "ListProviders" {
			if q.ProviderID != "" || q.ModelID != "" {
				return fail("INVALID_REQUEST")
			}
			return result(profiles, nil)
		}
		for _, p := range profiles {
			if p.ID == q.ProviderID {
				if method == "ListModels" {
					return result(p.Models, nil)
				}
				for _, m := range p.Models {
					if m.ID == q.ModelID {
						return result(m, nil)
					}
				}
			}
		}
		return fail("NOT_FOUND")
	case "GetSecurityStatus":
		if e := decode(payload, &struct{}{}); e != nil {
			return fail(errorCode(e))
		}
		var pending, active int
		if e := s.db.QueryRow("SELECT COUNT(*) FROM approval_records WHERE status='PENDING'").Scan(&pending); e != nil {
			return fail(errorCode(e))
		}
		if e := s.db.QueryRow("SELECT COUNT(*) FROM task_sessions WHERE status IN ('CREATED','PLANNING','WAITING_APPROVAL','RUNNING','VERIFYING')").Scan(&active); e != nil {
			return fail(errorCode(e))
		}
		return result(map[string]any{"workspace": s.registry.Workspace(), "maxFileBytes": 65536, "toolNames": []string{"filesystem.file.create"}, "pendingApprovals": pending, "activeTasks": active, "stopAvailable": true, "externalNetwork": false, "externalModelNetwork": "USER_CONFIRMED_BYOK_ONLY", "realModelTools": false, "desktopControl": false}, nil)
	case "GetAuditEvents", "GetToolExecutions":
		if e := decode(payload, &struct{}{}); e != nil {
			return fail(errorCode(e))
		}
		return s.recordsLocked(method)
	default:
		return fail("METHOD_NOT_ALLOWED")
	}
}

func validSettings(v domain.Settings) bool {
	return (v.AppLanguage == "en-US" || v.AppLanguage == "zh-CN") && (v.AgentLanguage == "follow-app" || v.AgentLanguage == "auto" || v.AgentLanguage == "en-US" || v.AgentLanguage == "zh-CN") && (v.Theme == "light" || v.Theme == "dark" || v.Theme == "system") && (v.Startup == "welcome" || v.Startup == "last")
}
func (s *Service) settingsLocked() (domain.Settings, error) {
	var v domain.Settings
	var raw string
	e := s.db.QueryRow("SELECT json FROM app_settings WHERE id='app'").Scan(&raw)
	if e == nil {
		e = json.Unmarshal([]byte(raw), &v)
	}
	return v, e
}
func (s *Service) createConversationLocked() (domain.Conversation, error) {
	c := domain.Conversation{ID: newID(), Title: "", CreatedAt: now(), UpdatedAt: now(), Messages: []domain.Message{}, Tasks: []domain.Task{}}
	_, e := s.db.Exec("INSERT INTO conversations(id,title,created_at,updated_at) VALUES(?,?,?,?)", c.ID, c.Title, c.CreatedAt, c.UpdatedAt)
	if e == nil {
		s.changed(c.ID, "")
	}
	return c, e
}
func (s *Service) approvalsLocked(pending bool) ([]domain.Approval, error) {
	q := "SELECT id FROM approval_records"
	if pending {
		q += " WHERE status='PENDING'"
	}
	q += " ORDER BY created_at DESC,id DESC"
	rows, e := s.db.Query(q)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []domain.Approval{}
	for _, id := range ids {
		a, e := loadApproval(s.db, id)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, nil
}
func (s *Service) recordsLocked(method string) domain.Reply {
	table := "audit_events"
	if method == "GetToolExecutions" {
		table = "tool_executions"
	}
	rows, e := s.db.Query("SELECT json FROM " + table + " ORDER BY created_at DESC,id DESC")
	if e != nil {
		return fail(errorCode(e))
	}
	defer rows.Close()
	if method == "GetToolExecutions" {
		out := []domain.ToolExecution{}
		for rows.Next() {
			var raw string
			var v domain.ToolExecution
			if e = rows.Scan(&raw); e != nil {
				return fail(errorCode(e))
			}
			if e = json.Unmarshal([]byte(raw), &v); e != nil {
				return fail("INTERNAL_ERROR")
			}
			if fault, ok := s.workerFaults[v.TaskID]; ok && v.Status == "RUNNING" {
				v.Status = "RECOVERY_REQUIRED"
				v.ErrorCode = fault.code
				v.Result = fault.verification
			}
			out = append(out, v)
		}
		return result(out, rows.Err())
	}
	out := []domain.AuditEvent{}
	for rows.Next() {
		var raw string
		var v domain.AuditEvent
		if e = rows.Scan(&raw); e != nil {
			return fail(errorCode(e))
		}
		if e = json.Unmarshal([]byte(raw), &v); e != nil {
			return fail("INTERNAL_ERROR")
		}
		out = append(out, v)
	}
	return result(out, rows.Err())
}

func (s *Service) beginCall() bool {
	s.admission.Lock()
	defer s.admission.Unlock()
	if s.lifetime.Err() != nil {
		return false
	}
	s.wg.Add(1)
	return true
}

// BeginShutdown closes admission and cancels every task context independently
// of the database and the runtime state mutex.
func (s *Service) BeginShutdown() {
	s.admission.Lock()
	s.stop()
	s.admission.Unlock()
	s.screens.close()
	s.desktopControl.signalStop()
	s.signalMVPStop()
}

func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		s.BeginShutdown()
		s.wg.Wait()
		controlErr := errors.Join(s.closeDesktopControl(), s.stopMVP(), s.mvp.faultErr)
		s.mu.Lock()
		clear(s.agent02Secret)
		s.agent02Secret = nil
		_, stopErr := s.stopAllLocked()
		s.closeErr = errors.Join(s.workerErr, stopErr, controlErr, s.db.Close(), s.releaseProfile())
		s.mu.Unlock()
	})
	return s.closeErr
}
