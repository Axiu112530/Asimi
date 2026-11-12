package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"deskpilot/internal/domain"
	"deskpilot/internal/provider"
	"deskpilot/internal/tools"
)

var transitions = map[string]map[string]bool{
	"CREATED":           {"PLANNING": true, "CANCELLED": true, "PAUSED": true},
	"PLANNING":          {"WAITING_APPROVAL": true, "COMPLETED": true, "FAILED": true, "PAUSED": true, "CANCELLED": true},
	"WAITING_APPROVAL":  {"RUNNING": true, "FAILED": true, "CANCELLED": true, "PAUSED": true},
	"RUNNING":           {"VERIFYING": true, "FAILED": true, "CANCELLED": true, "PAUSED": true, "RECOVERY_REQUIRED": true},
	"VERIFYING":         {"COMPLETED": true, "FAILED": true, "CANCELLED": true, "PAUSED": true, "RECOVERY_REQUIRED": true},
	"PAUSED":            {"CANCELLED": true, "RECOVERY_REQUIRED": true, "VERIFYING": true},
	"FAILED":            {"RECOVERY_REQUIRED": true, "VERIFYING": true},
	"CANCELLED":         {"RECOVERY_REQUIRED": true},
	"RECOVERY_REQUIRED": {"VERIFYING": true, "CANCELLED": true},
}

func transition(t *domain.Task, to string) error {
	if !transitions[t.Status][to] {
		return codedError("INVALID_STATE")
	}
	t.Status = to
	t.UpdatedAt = now()
	return nil
}
func terminal(state string) bool {
	return state == "COMPLETED" || state == "FAILED" || state == "CANCELLED" || state == "RECOVERY_REQUIRED"
}
func newSteps() []domain.Step {
	return []domain.Step{
		{ID: newID(), NameKey: "steps.plan", DescriptionKey: "steps.planDescription", Status: "RUNNING"},
		{ID: newID(), NameKey: "steps.create", DescriptionKey: "steps.createDescription", Status: "PENDING", ToolName: "filesystem.file.create"},
		{ID: newID(), NameKey: "steps.verify", DescriptionKey: "steps.verifyDescription", Status: "PENDING"},
	}
}
func assistantMessage(t domain.Task, text string) domain.Message {
	return domain.Message{ID: newID(), ConversationID: t.ConversationID, TaskID: t.ID, Role: "assistant", Content: text, CreatedAt: now()}
}
func (s *Service) send(conversationID, input string) (map[string]string, error) {
	if strings.TrimSpace(input) == "" || len(input) > 196608 {
		return nil, codedError("INVALID_REQUEST")
	}
	s.mu.Lock()
	if s.lifetime.Err() != nil {
		s.mu.Unlock()
		return nil, codedError("SERVICE_CLOSED")
	}
	if conversationID == "" {
		c, e := s.createConversationLocked()
		if e != nil {
			s.mu.Unlock()
			return nil, e
		}
		conversationID = c.ID
	}
	c, e := s.conversationLocked(conversationID, true)
	if e != nil {
		s.mu.Unlock()
		return nil, e
	}
	settings, e := s.settingsLocked()
	if e != nil {
		s.mu.Unlock()
		return nil, e
	}
	var draft *provider.Draft
	if len(c.Tasks) > 0 && c.Tasks[0].Status == "PAUSED" && c.Tasks[0].ErrorCode == "CLARIFICATION_REQUIRED" {
		var raw sql.NullString
		if e = s.db.QueryRow("SELECT draft_json FROM task_sessions WHERE id=?", c.Tasks[0].ID).Scan(&raw); e != nil {
			s.mu.Unlock()
			return nil, e
		}
		if raw.Valid {
			var v provider.Draft
			if e = json.Unmarshal([]byte(raw.String), &v); e != nil {
				s.mu.Unlock()
				return nil, e
			}
			draft = &v
		}
	}
	title := []rune(input)
	if len(title) > 80 {
		title = title[:80]
	}
	t := domain.Task{ID: newID(), ConversationID: conversationID, Title: string(title), Description: input, Status: "CREATED", CurrentStep: "steps.plan", Steps: newSteps(), CreatedAt: now(), UpdatedAt: now()}
	ctx, cancel := context.WithCancel(s.lifetime)
	s.controls[t.ID] = control{ctx, cancel}
	e = s.transaction(func(tx *sql.Tx) error {
		if e := saveTask(tx, t, nil, ""); e != nil {
			return e
		}
		if e := insertMessage(tx, domain.Message{ID: newID(), ConversationID: conversationID, TaskID: t.ID, Role: "user", Content: input, CreatedAt: now()}); e != nil {
			return e
		}
		if c.Title == "" {
			if _, e := tx.Exec("UPDATE conversations SET title=? WHERE id=?", t.Title, conversationID); e != nil {
				return e
			}
		}
		return audit(tx, t.ID, "TASK_CREATED", nil)
	})
	if e != nil {
		cancel()
		delete(s.controls, t.ID)
		s.mu.Unlock()
		return nil, e
	}
	s.markCheckpoint("task-created")
	s.changed(conversationID, t.ID)
	if ctx.Err() != nil {
		s.mu.Unlock()
		return nil, codedError("CANCELLED")
	}
	if e = transition(&t, "PLANNING"); e == nil {
		e = s.transaction(func(tx *sql.Tx) error { return saveTask(tx, t, nil, "") })
	}
	if e != nil {
		cancel()
		delete(s.controls, t.ID)
		s.mu.Unlock()
		return nil, e
	}
	s.changed(conversationID, t.ID)
	s.mu.Unlock()
	response, genErr := s.provider.Generate(ctx, provider.Request{Input: input, Messages: c.Messages, AppLanguage: settings.AppLanguage, AgentLanguage: settings.AgentLanguage, Draft: draft})
	s.mu.Lock()
	defer s.mu.Unlock()
	current, e := s.taskLocked(t.ID)
	if e != nil {
		return nil, e
	}
	t = current
	ids := map[string]string{"conversationId": conversationID, "taskId": t.ID}
	if t.Status == "CANCELLED" || ctx.Err() != nil {
		cancel()
		delete(s.controls, t.ID)
		return ids, nil
	}
	if genErr != nil {
		e = s.failPlanLocked(&t, errorCode(genErr), "en-US")
		cancel()
		delete(s.controls, t.ID)
		return ids, e
	}
	lang := response.Language
	if response.Intent == "CREATE" && response.Draft != nil && response.Draft.Content != nil {
		aid := newID()
		expires := time.Now().UTC().Add(5 * time.Minute)
		registry := s.registry
		// Security preparation may wait on the private filesystem runtime. Keep
		// the independent cancel lane free while it does so.
		s.mu.Unlock()
		op, prepErr := registry.Prepare(t.ID, response.Draft.FileName, *response.Draft.Content, aid, expires)
		s.mu.Lock()
		t, e = s.taskLocked(t.ID)
		if e != nil {
			return nil, e
		}
		if t.Status == "CANCELLED" || ctx.Err() != nil {
			cancel()
			delete(s.controls, t.ID)
			return ids, nil
		}
		if prepErr != nil {
			e = s.failPlanLocked(&t, errorCode(prepErr), lang)
			cancel()
			delete(s.controls, t.ID)
			return ids, e
		}
		a := domain.Approval{ID: aid, Operation: op, Status: "PENDING", CreatedAt: now(), ExpiresAt: op.ExpiresAt}
		t.Title = provider.Text(lang, "Create "+op.FileName, "创建 "+op.FileName)
		t.Approval = &a
		t.Steps[0].Status = "COMPLETED"
		t.CurrentStep = "steps.create"
		if e = transition(&t, "WAITING_APPROVAL"); e != nil {
			return nil, e
		}
		e = s.transaction(func(tx *sql.Tx) error {
			if e := saveTask(tx, t, nil, lang); e != nil {
				return e
			}
			if c.Title == "" {
				if _, e := tx.Exec("UPDATE conversations SET title=? WHERE id=?", t.Title, conversationID); e != nil {
					return e
				}
			}
			if e := saveApproval(tx, a); e != nil {
				return e
			}
			if e := insertMessage(tx, assistantMessage(t, response.Text)); e != nil {
				return e
			}
			return audit(tx, t.ID, "APPROVAL_REQUESTED", map[string]string{"approvalId": a.ID})
		})
		if e == nil {
			s.markCheckpoint("approval-persisted")
			s.changed(conversationID, t.ID)
		} else {
			cancel()
			delete(s.controls, t.ID)
		}
		return ids, e
	}
	t.Steps[0].Status = "COMPLETED"
	t.Steps[1].Status = "SKIPPED"
	t.Steps[2].Status = "SKIPPED"
	t.Summary = response.Text
	state := "COMPLETED"
	if response.Intent == "CLARIFY" {
		state = "PAUSED"
		t.ErrorCode = "CLARIFICATION_REQUIRED"
	} else if response.Intent == "UNSUPPORTED" {
		state = "FAILED"
		t.ErrorCode = "UNSUPPORTED_REQUEST"
	}
	if e = transition(&t, state); e != nil {
		return nil, e
	}
	e = s.transaction(func(tx *sql.Tx) error {
		if e := saveTask(tx, t, response.Draft, lang); e != nil {
			return e
		}
		if e := insertMessage(tx, assistantMessage(t, response.Text)); e != nil {
			return e
		}
		return audit(tx, t.ID, "TASK_"+state, nil)
	})
	cancel()
	delete(s.controls, t.ID)
	if e == nil {
		s.changed(conversationID, t.ID)
	}
	return ids, e
}
func (s *Service) failPlanLocked(t *domain.Task, code, lang string) error {
	t.ErrorCode = code
	t.Steps[0].Status = "FAILED"
	t.Steps[0].ErrorCode = code
	t.Steps[1].Status = "SKIPPED"
	t.Steps[2].Status = "SKIPPED"
	t.Summary = provider.Text(lang, "The request was blocked before execution ("+code+"). No file was created.", "请求在执行前被阻止（"+code+"）。尚未创建文件。")
	if e := transition(t, "FAILED"); e != nil {
		return e
	}
	e := s.transaction(func(tx *sql.Tx) error {
		if e := saveTask(tx, *t, nil, lang); e != nil {
			return e
		}
		if e := insertMessage(tx, assistantMessage(*t, t.Summary)); e != nil {
			return e
		}
		return audit(tx, t.ID, "TASK_FAILED", map[string]string{"errorCode": code})
	})
	if e == nil {
		s.changed(t.ConversationID, t.ID)
	}
	return e
}

func (s *Service) decide(id string, approve bool) (domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifetime.Err() != nil {
		return domain.Task{}, codedError("SERVICE_CLOSED")
	}
	if e := s.expireLocked(); e != nil {
		return domain.Task{}, e
	}
	a, e := loadApproval(s.db, id)
	if e != nil {
		return domain.Task{}, e
	}
	t, e := s.taskLocked(a.Operation.TaskID)
	if e != nil {
		return t, e
	}
	if a.Status == "EXPIRED" {
		return t, codedError("APPROVAL_EXPIRED")
	}
	if a.Status != "PENDING" {
		return t, codedError("APPROVAL_DENIED")
	}
	if t.Status != "WAITING_APPROVAL" {
		return t, codedError("INVALID_STATE")
	}
	a.DecidedAt = now()
	if !approve {
		a.Status = "REJECTED"
		t.Approval = &a
		t.ErrorCode = "APPROVAL_DENIED"
		t.Steps[1].Status = "CANCELLED"
		t.Steps[1].ErrorCode = t.ErrorCode
		t.Steps[2].Status = "SKIPPED"
		lang := s.taskLanguageLocked(t.ID)
		t.Summary = provider.Text(lang, "You rejected the operation. No file was created.", "你已拒绝操作。尚未创建文件。")
		if e = transition(&t, "FAILED"); e != nil {
			return t, e
		}
		e = s.transaction(func(tx *sql.Tx) error {
			if e := saveApproval(tx, a); e != nil {
				return e
			}
			if e := saveTask(tx, t, nil, ""); e != nil {
				return e
			}
			if e := insertMessage(tx, assistantMessage(t, t.Summary)); e != nil {
				return e
			}
			return audit(tx, t.ID, "APPROVAL_REJECTED", nil)
		})
		if c, ok := s.controls[t.ID]; ok {
			c.cancel()
			delete(s.controls, t.ID)
		}
		if e == nil {
			s.changed(t.ConversationID, t.ID)
		}
		return t, e
	}
	c, ok := s.controls[t.ID]
	if !ok || c.ctx.Err() != nil {
		return t, codedError("CANCELLED")
	}
	a.Status = "APPROVED"
	t.Approval = &a
	t.Steps[1].Status = "RUNNING"
	if e = transition(&t, "RUNNING"); e != nil {
		return t, e
	}
	x := domain.ToolExecution{ID: newID(), TaskID: t.ID, ToolName: a.Operation.ToolName, Path: a.Operation.NormalizedPath, Status: "RUNNING", CreatedAt: now(), UpdatedAt: now()}
	e = s.transaction(func(tx *sql.Tx) error {
		if e := saveApproval(tx, a); e != nil {
			return e
		}
		if e := saveTask(tx, t, nil, ""); e != nil {
			return e
		}
		if e := saveExecution(tx, x); e != nil {
			return e
		}
		return audit(tx, t.ID, "APPROVAL_APPROVED", nil)
	})
	if e != nil {
		return t, e
	}
	s.wg.Add(1)
	go s.execute(c.ctx, a.Operation, x, t.ConversationID)
	s.changed(t.ConversationID, t.ID)
	return t, nil
}
func (s *Service) taskLanguageLocked(id string) string {
	var lang string
	if e := s.db.QueryRow("SELECT language FROM task_sessions WHERE id=?", id).Scan(&lang); e != nil || lang == "" {
		return "en-US"
	}
	return lang
}

// consume is the permission engine's single-use compare-and-swap. All fields
// come from the immutable persisted operation, never from a model/client grant.
func (s *Service) consume(ctx context.Context, op domain.Operation, executionID ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifetime.Err() != nil || ctx.Err() != nil {
		return codedError("CANCELLED")
	}
	a, e := loadApproval(s.db, op.ApprovalID)
	if errors.Is(e, sql.ErrNoRows) {
		return codedError("APPROVAL_REQUIRED")
	}
	if e != nil {
		return codedError(errorCode(e))
	}
	if a.Operation != op || a.ID != op.ApprovalID || a.ExpiresAt != op.ExpiresAt {
		return codedError("APPROVAL_DENIED")
	}
	expires, e := time.Parse(time.RFC3339Nano, a.ExpiresAt)
	if e != nil || !expires.After(time.Now().UTC()) {
		return codedError("APPROVAL_EXPIRED")
	}
	if a.Status != "APPROVED" {
		return codedError("APPROVAL_DENIED")
	}
	t, e := s.taskLocked(op.TaskID)
	if e != nil {
		return e
	}
	if t.Status != "RUNNING" {
		return codedError("CANCELLED")
	}
	c, ok := s.controls[t.ID]
	if !ok || c.ctx.Err() != nil {
		return codedError("CANCELLED")
	}
	e = s.transaction(func(tx *sql.Tx) error {
		if ctx.Err() != nil {
			return codedError("CANCELLED")
		}
		var id string
		if len(executionID) > 0 {
			id = executionID[0]
		} else {
			if e := tx.QueryRow("SELECT id FROM tool_executions WHERE task_id=? AND status='RUNNING'", op.TaskID).Scan(&id); e != nil {
				return e
			}
		}
		r, e := tx.Exec("UPDATE approval_records SET status='CONSUMED',updated_at=? WHERE id=? AND status='APPROVED' AND operation_json=? AND expires_at=? AND task_id=?", now(), a.ID, encode(op), op.ExpiresAt, op.TaskID)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return codedError("APPROVAL_DENIED")
		}
		r, e = tx.Exec(`INSERT INTO file_intents(execution_id,task_id,approval_id) SELECT id,task_id,? FROM tool_executions WHERE id=? AND task_id=? AND status='RUNNING'`, a.ID, id, t.ID)
		if e != nil {
			return e
		}
		if e = requireOne(r); e != nil {
			return e
		}
		if ctx.Err() != nil {
			return codedError("CANCELLED")
		}
		return audit(tx, t.ID, "APPROVAL_CONSUMED", map[string]string{"approvalId": a.ID})
	})
	if e == nil {
		s.markCheckpoint("approval-consumed")
		s.changed(t.ConversationID, t.ID)
	}
	if e != nil {
		return codedError(errorCode(e))
	}
	return nil
}
func requireOne(result sql.Result) error {
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return codedError("STORAGE_CONSTRAINT")
	}
	return nil
}

func (s *Service) recordWitness(op domain.Operation, executionID string, witness domain.FileWitness) error {
	// The registry holds its file lock here. This transaction touches SQLite
	// only and never acquires the task mutex.
	s.markCheckpoint("temp-written")
	e := s.transaction(func(tx *sql.Tx) error {
		result, e := tx.Exec(`UPDATE file_intents SET witness_json=? WHERE execution_id=? AND task_id=? AND approval_id=? AND settled=0 AND witness_json IS NULL`, encode(witness), executionID, op.TaskID, op.ApprovalID)
		if e != nil {
			return e
		}
		return requireOne(result)
	})
	if e != nil {
		return codedError(errorCode(e))
	}
	return nil
}

func settleIntent(tx *sql.Tx, executionID, taskID, approvalID string) error {
	result, e := tx.Exec(`UPDATE file_intents SET settled=1 WHERE execution_id=? AND task_id=? AND approval_id=? AND settled=0`, executionID, taskID, approvalID)
	if e != nil {
		return e
	}
	return requireOne(result)
}

func exactVerification(op domain.Operation, v domain.Verification) bool {
	return v.Passed && v.Created && v.ContentMatched && v.ExpectedBytes == op.Bytes && v.ActualBytes == op.Bytes && v.ExpectedHash == op.ContentHash && v.ActualHash == op.ContentHash && v.Path == op.NormalizedPath
}

func applyFileEvidence(t *domain.Task, x *domain.ToolExecution, v domain.Verification) {
	t.Verification = &v
	x.Result = &v
	if v.Created && len(t.Steps) > 1 {
		t.Steps[1].Result = v.Path
	}
	if v.ActualHash != "" && len(t.Steps) > 2 {
		t.Steps[2].Result = v.ActualHash
	}
}

func (s *Service) execute(ctx context.Context, op domain.Operation, x domain.ToolExecution, conversationID string) {
	defer s.wg.Done()
	consumed, witnessRecorded := false, false
	consume := func(ctx context.Context, op domain.Operation) error {
		e := s.consume(ctx, op, x.ID)
		if e == nil {
			consumed = true
		}
		return e
	}
	hooks := tools.ExecutionHooks{Record: func(witness domain.FileWitness) error {
		e := s.recordWitness(op, x.ID, witness)
		if e == nil {
			witnessRecorded = true
		}
		return e
	}}
	if s.checkpoint != nil {
		hooks.Committed = func() { s.markCheckpoint("file-committed") }
	}
	v, runErr := s.registry.Execute(ctx, op, consume, hooks)
	// Task success is decided from the independent verifier before the provider
	// sees the outcome. Model continuation supplies text, never a success flag.
	verified := consumed && witnessRecorded && exactVerification(op, v)
	if verified && runErr == nil {
		s.markCheckpoint("file-verified")
	}
	code := ""
	if runErr != nil {
		code = errorCode(runErr)
	} else if !verified {
		code = "VERIFICATION_FAILED"
	}
	if !verified {
		v.Passed = false
		if v.Code == "" {
			v.Code = code
		}
	}
	s.mu.Lock()
	lang := s.taskLanguageLocked(op.TaskID)
	s.mu.Unlock()
	var summary string
	if ctx.Err() == nil {
		response, summaryErr := s.provider.Summarize(ctx, provider.SummaryRequest{Language: lang, Verification: v, ErrorCode: code})
		if summaryErr == nil {
			summary = response.Text
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.controls[op.TaskID]; ok {
		defer c.cancel()
		defer delete(s.controls, op.TaskID)
	}
	t, e := s.taskLocked(op.TaskID)
	if e != nil {
		s.workerFailureLocked(conversationID, op.TaskID, e, v)
		return
	}
	applyFileEvidence(&t, &x, v)
	x.UpdatedAt = now()
	if t.Status == "FAILED" {
		// An unconsumed approval may have expired while dispatch was queued.
		x.Status = "FAILED"
		x.ErrorCode = t.ErrorCode
	} else if t.Status == "CANCELLED" || ctx.Err() != nil {
		if t.Status != "CANCELLED" {
			if e = transition(&t, "CANCELLED"); e != nil {
				s.workerFailureLocked(conversationID, op.TaskID, e, v)
				return
			}
		}
		t.ErrorCode = "CANCELLED"
		x.Status = "CANCELLED"
		x.ErrorCode = code
		if v.Created {
			t.Summary = provider.Text(lang, "The task was cancelled after the file was committed. The file was preserved; review the recorded verification result.", "任务取消时文件已经提交。文件已保留，请查看记录的验证结果。")
		} else {
			t.Summary = provider.Text(lang, "The task was cancelled. No file was committed.", "任务已取消。尚未提交文件。")
		}
		t.Steps[1].Status = "CANCELLED"
		if v.Created {
			t.Steps[1].Status = "COMPLETED"
		}
		t.Steps[2].Status = "CANCELLED"
		if verified {
			t.Steps[2].Status = "COMPLETED"
		}
	} else {
		t.Steps[1].Status = "COMPLETED"
		if !v.Created {
			t.Steps[1].Status = "FAILED"
		}
		t.CurrentStep = "steps.verify"
		t.Steps[2].Status = "RUNNING"
		if e = transition(&t, "VERIFYING"); e != nil {
			s.workerFailureLocked(conversationID, op.TaskID, e, v)
			return
		}
		if code == "" {
			t.Steps[2].Status = "COMPLETED"
			x.Status = "COMPLETED"
			t.Summary = provider.Text(lang, "The file was created and independently verified. Its content, byte count and SHA-256 match the approved operation.", "文件已创建并通过独立验证。内容、字节数及 SHA-256 均与审批操作一致。")
			if e = transition(&t, "COMPLETED"); e != nil {
				s.workerFailureLocked(conversationID, op.TaskID, e, v)
				return
			}
		} else {
			t.Steps[2].Status = "FAILED"
			t.Steps[2].ErrorCode = code
			t.ErrorCode = code
			x.Status = "FAILED"
			x.ErrorCode = code
			if v.Created {
				t.Summary = provider.Text(lang, "A file was committed, but the task could not be verified ("+code+"). The file was preserved. Review the verification result.", "文件已提交，但任务未能通过验证（"+code+"）。文件已保留，请查看验证结果。")
			} else {
				t.Summary = provider.Text(lang, "The file was not created ("+code+").", "文件未创建（"+code+"）。")
			}
			if e = transition(&t, "FAILED"); e != nil {
				s.workerFailureLocked(conversationID, op.TaskID, e, v)
				return
			}
		}
		if summary != "" {
			t.Summary = summary
		}
	}
	t.UpdatedAt = now()
	invalidateApproval := !consumed && t.Approval != nil && t.Approval.Status == "APPROVED"
	if invalidateApproval {
		t.Approval.Status = "INVALIDATED"
		t.Approval.DecidedAt = t.UpdatedAt
	}
	e = s.transaction(func(tx *sql.Tx) error {
		if invalidateApproval {
			if e := saveApproval(tx, *t.Approval); e != nil {
				return e
			}
		}
		if e := saveTask(tx, t, nil, ""); e != nil {
			return e
		}
		if e := saveExecution(tx, x); e != nil {
			return e
		}
		if e := insertMessage(tx, assistantMessage(t, t.Summary)); e != nil {
			return e
		}
		if consumed {
			if e := settleIntent(tx, x.ID, t.ID, op.ApprovalID); e != nil {
				return e
			}
		}
		return audit(tx, t.ID, "TASK_"+t.Status, map[string]string{"errorCode": t.ErrorCode, "created": boolString(v.Created)})
	})
	if e != nil {
		s.workerFailureLocked(conversationID, op.TaskID, e, v)
		return
	}
	if verified && runErr == nil {
		s.markCheckpoint("result-persisted")
	}
	s.changed(t.ConversationID, t.ID)
}
func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (s *Service) cancelLocked(id, reason string) (domain.Task, error) {
	if fault, ok := s.workerFaults[id]; ok {
		return domain.Task{}, codedError(fault.code)
	}
	// Cancel first even if storage is unavailable.
	if c, ok := s.controls[id]; ok {
		c.cancel()
		delete(s.controls, id)
	}
	t, e := s.taskLocked(id)
	if e != nil {
		return t, e
	}
	if terminal(t.Status) {
		return t, nil
	}
	if len(reason) > 256 {
		reason = reason[:256]
	}
	if e = transition(&t, "CANCELLED"); e != nil {
		return t, e
	}
	t.ErrorCode = "CANCELLED"
	lang := s.taskLanguageLocked(id)
	t.Summary = provider.Text(lang, "The task was cancelled. No further operations will start. Any operation already committed will be reported when verification returns.", "任务已取消，不再启动后续操作。已经提交的操作会在验证返回后如实记录。")
	if t.Verification != nil && t.Verification.Created {
		t.Summary = provider.Text(lang, "The task was cancelled. The already committed file was preserved.", "任务已取消。已经提交的文件已保留。")
	}
	for i := range t.Steps {
		if t.Steps[i].Status == "RUNNING" || t.Steps[i].Status == "PENDING" {
			t.Steps[i].Status = "CANCELLED"
		}
	}
	if t.Approval != nil && (t.Approval.Status == "PENDING" || t.Approval.Status == "APPROVED") {
		t.Approval.Status = "INVALIDATED"
		t.Approval.DecidedAt = now()
	}
	e = s.transaction(func(tx *sql.Tx) error {
		if t.Approval != nil {
			if e := saveApproval(tx, *t.Approval); e != nil {
				return e
			}
		}
		if e := saveTask(tx, t, nil, ""); e != nil {
			return e
		}
		if e := insertMessage(tx, assistantMessage(t, t.Summary)); e != nil {
			return e
		}
		return audit(tx, id, "TASK_CANCELLED", map[string]string{"reason": reason})
	})
	if e == nil {
		s.changed(t.ConversationID, id)
	}
	return t, e
}
func (s *Service) stopAllLocked() (int, error) {
	for _, c := range s.controls {
		c.cancel()
	}
	ids, e := s.taskIDsLocked("")
	if e != nil {
		return 0, e
	}
	n := 0
	var failure error
	for _, id := range ids {
		t, e := s.taskLocked(id)
		if e != nil {
			failure = errors.Join(failure, e)
			continue
		}
		_, uncertain := s.workerFaults[id]
		if terminal(t.Status) || t.Status == "PAUSED" || uncertain {
			continue
		}
		if _, e = s.cancelLocked(id, "STOP_ALL"); e != nil {
			failure = errors.Join(failure, e)
			continue
		}
		n++
	}
	return n, failure
}

func (s *Service) expiryLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var previousCode string
	for {
		select {
		case <-s.lifetime.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.lifetime.Err() == nil {
				if e := s.expireLocked(); e != nil {
					code := errorCode(e)
					if code != previousCode {
						s.workerFailureLocked("", "", e)
					}
					previousCode = code
				} else {
					previousCode = ""
				}
			}
			s.mu.Unlock()
		}
	}
}
func (s *Service) expireLocked() error {
	rows, e := s.db.Query("SELECT id FROM approval_records WHERE status IN ('PENDING','APPROVED')")
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		a, e := loadApproval(s.db, id)
		if e != nil {
			return e
		}
		expiry, err := time.Parse(time.RFC3339Nano, a.ExpiresAt)
		if err == nil && expiry.After(time.Now().UTC()) {
			continue
		}
		a.Status = "EXPIRED"
		a.DecidedAt = now()
		t, e := s.taskLocked(a.Operation.TaskID)
		if e != nil {
			return e
		}
		t.Approval = &a
		if !terminal(t.Status) && t.Status != "PAUSED" {
			if e = transition(&t, "FAILED"); e != nil {
				return e
			}
			t.ErrorCode = "APPROVAL_EXPIRED"
			t.Steps[1].Status = "FAILED"
			t.Steps[1].ErrorCode = t.ErrorCode
			t.Steps[2].Status = "SKIPPED"
			t.Summary = provider.Text(s.taskLanguageLocked(t.ID), "The approval expired. No file was created. Submit a new request to review a fresh approval.", "审批已过期。尚未创建文件。请提交新请求以获取新的审批。")
		}
		if c, ok := s.controls[t.ID]; ok {
			c.cancel()
			delete(s.controls, t.ID)
		}
		e = s.transaction(func(tx *sql.Tx) error {
			if e := saveApproval(tx, a); e != nil {
				return e
			}
			if e := saveTask(tx, t, nil, ""); e != nil {
				return e
			}
			if e := insertMessage(tx, assistantMessage(t, t.Summary)); e != nil {
				return e
			}
			return audit(tx, t.ID, "APPROVAL_EXPIRED", nil)
		})
		if e != nil {
			return e
		}
		s.changed(t.ConversationID, t.ID)
	}
	return nil
}
