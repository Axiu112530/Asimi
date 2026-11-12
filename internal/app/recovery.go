package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"deskpilot/internal/domain"
	"deskpilot/internal/provider"
)

type recoveryIntent struct {
	executionID string
	approvalID  string
	witness     sql.NullString
}

// recoverTasks runs before the service admits calls. It never executes an old
// operation. Native inspection runs with no SQLite transaction or task lock.
func (s *Service) recoverTasks() error {
	rows, e := s.db.Query(`SELECT t.id FROM task_sessions t
WHERE t.status IN ('CREATED','PLANNING','WAITING_APPROVAL','RUNNING','VERIFYING')
   OR EXISTS(SELECT 1 FROM file_intents i WHERE i.task_id=t.id AND i.settled=0)
   OR EXISTS(SELECT 1 FROM tool_executions x WHERE x.task_id=t.id AND x.status IN ('RUNNING','VERIFYING'))
ORDER BY t.created_at,t.id`)
	if e != nil {
		return e
	}
	var ids []string
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
		if e = s.recoverTask(id); e != nil {
			return e
		}
	}
	// Grants from any other old state are also unusable after a restart.
	_, e = s.db.Exec(`UPDATE approval_records SET status='INVALIDATED',updated_at=?,decided_at=? WHERE status IN ('PENDING','APPROVED')`, now(), now())
	return e
}

func (s *Service) recoveryRecords(taskID string) (*recoveryIntent, *domain.ToolExecution, error) {
	var i recoveryIntent
	e := s.db.QueryRow(`SELECT execution_id,approval_id,witness_json FROM file_intents WHERE task_id=? AND settled=0`, taskID).Scan(&i.executionID, &i.approvalID, &i.witness)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, nil, e
	}
	var intent *recoveryIntent
	var raw string
	if e == nil {
		intent = &i
		e = s.db.QueryRow(`SELECT json FROM tool_executions WHERE id=? AND task_id=?`, i.executionID, taskID).Scan(&raw)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, nil, codedError("STORAGE_CORRUPT")
		}
	} else {
		e = s.db.QueryRow(`SELECT json FROM tool_executions WHERE task_id=? AND status IN ('RUNNING','VERIFYING') ORDER BY created_at DESC,id DESC LIMIT 1`, taskID).Scan(&raw)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, nil, nil
		}
	}
	if e != nil {
		return nil, nil, e
	}
	var x domain.ToolExecution
	if e = json.Unmarshal([]byte(raw), &x); e != nil {
		return nil, nil, codedError("STORAGE_CORRUPT")
	}
	return intent, &x, nil
}

func (s *Service) recoverTask(id string) error {
	t, e := s.taskLocked(id)
	if e != nil {
		return e
	}
	intent, x, e := s.recoveryRecords(id)
	if e != nil {
		return e
	}
	lang := s.taskLanguageLocked(id)
	auditCode, reason := "TASK_INTERRUPTED", "no-consumed-approval"
	settled := false
	wasRequired := t.Status == "RECOVERY_REQUIRED"
	previousVerification := t.Verification
	var verification *domain.Verification
	if intent != nil {
		if t.Approval == nil || t.Approval.Status != "CONSUMED" || t.Approval.ID != intent.approvalID ||
			t.Approval.Operation.TaskID != id || t.Approval.Operation.ApprovalID != intent.approvalID || x.ID != intent.executionID || x.TaskID != id ||
			x.ToolName != t.Approval.Operation.ToolName || x.Path != t.Approval.Operation.NormalizedPath {
			auditCode, reason = "TASK_RECOVERY_REQUIRED", "WITNESS_MISMATCH"
		} else if !intent.witness.Valid {
			// A v2 executor cannot rename without first committing the witness.
			reason = "no-durable-witness"
			settled = true
		} else {
			op := t.Approval.Operation
			var witness domain.FileWitness
			if e = json.Unmarshal([]byte(intent.witness.String), &witness); e != nil {
				auditCode, reason = "TASK_RECOVERY_REQUIRED", "WITNESS_MISMATCH"
			} else {
				v, inspectErr := s.registry.Inspect(op, witness)
				if inspectErr == nil && exactVerification(op, v) {
					auditCode, reason = "TASK_RECOVERED", "verified-file-witness"
					settled = true
				} else {
					v.Passed = false
					reason = "VERIFICATION_FAILED"
					if inspectErr != nil {
						reason = errorCode(inspectErr)
					}
					v.Code = reason
					auditCode = "TASK_RECOVERY_REQUIRED"
				}
				verification = &v
			}
		}
	} else if t.Approval != nil && t.Approval.Status == "CONSUMED" {
		// Phase 1 approved operations had no durable pre-rename witness. Even
		// an identical file on disk cannot establish this task's ownership.
		auditCode, reason = "TASK_RECOVERY_REQUIRED", "legacy-consumed-approval"
	}
	if wasRequired && auditCode == "TASK_RECOVERY_REQUIRED" &&
		(verification == nil || (previousVerification != nil && *previousVerification == *verification)) {
		return nil
	}

	if verification != nil {
		applyFileEvidence(&t, x, *verification)
	}
	if t.Approval != nil && (t.Approval.Status == "PENDING" || t.Approval.Status == "APPROVED") {
		t.Approval.Status = "INVALIDATED"
		t.Approval.DecidedAt = now()
	}
	switch auditCode {
	case "TASK_RECOVERED":
		cancelled, e := s.wasCancelled(t)
		if e != nil {
			return e
		}
		if cancelled {
			if t.Status != "CANCELLED" {
				if e = transition(&t, "CANCELLED"); e != nil {
					return e
				}
			}
			t.ErrorCode = "CANCELLED"
			t.Summary = provider.Text(lang, "The cancelled task's committed file was recovered and verified. The file was preserved.", "已恢复并验证取消任务所提交的文件。文件已保留。")
		} else {
			if t.Status != "COMPLETED" {
				if t.Status != "VERIFYING" {
					if e = transition(&t, "VERIFYING"); e != nil {
						return e
					}
				}
				if e = transition(&t, "COMPLETED"); e != nil {
					return e
				}
			}
			t.ErrorCode = ""
			t.Summary = provider.Text(lang, "The committed file was recovered and independently verified after restart.", "重启后已恢复并独立验证提交的文件。")
		}
		t.CurrentStep = "steps.verify"
		for j := range t.Steps {
			t.Steps[j].Status = "COMPLETED"
			t.Steps[j].ErrorCode = ""
		}
	case "TASK_RECOVERY_REQUIRED":
		if t.Status != "RECOVERY_REQUIRED" {
			if e = transition(&t, "RECOVERY_REQUIRED"); e != nil {
				return e
			}
		}
		t.ErrorCode = "RECOVERY_REQUIRED"
		t.Summary = provider.Text(lang, "Asimi could not confirm the interrupted file operation. Review the file evidence before submitting a new request.", "Asimi 无法确认中断的文件操作。请先检查文件证据，再提交新请求。")
		markUnfinishedSteps(&t, "SKIPPED")
	default:
		if !terminal(t.Status) && t.Status != "PAUSED" {
			if e = transition(&t, "PAUSED"); e != nil {
				return e
			}
		}
		if t.Status == "PAUSED" {
			t.ErrorCode = "INTERRUPTED"
			t.Summary = provider.Text(lang, "The interrupted task is paused. No file was committed and the operation will not resume automatically.", "中断的任务已暂停。文件尚未提交，操作不会自动恢复。")
		} else if t.Status == "CANCELLED" {
			t.Summary = provider.Text(lang, "The task was cancelled before a file was committed.", "任务在提交文件前已取消。")
		}
		markUnfinishedSteps(&t, "SKIPPED")
	}
	if x != nil {
		x.Status = t.Status
		x.ErrorCode = t.ErrorCode
		if auditCode == "TASK_RECOVERY_REQUIRED" && verification != nil {
			x.ErrorCode = verification.Code
		}
		x.UpdatedAt = now()
	}
	t.UpdatedAt = now()
	e = s.transaction(func(tx *sql.Tx) error {
		if t.Approval != nil {
			if e := saveApproval(tx, *t.Approval); e != nil {
				return e
			}
		}
		if e := saveTask(tx, t, nil, ""); e != nil {
			return e
		}
		if x != nil {
			if e := saveExecution(tx, *x); e != nil {
				return e
			}
		}
		if settled {
			if e := settleIntent(tx, intent.executionID, id, intent.approvalID); e != nil {
				return e
			}
		}
		if wasRequired && auditCode == "TASK_RECOVERY_REQUIRED" {
			return nil
		}
		if e := insertMessage(tx, assistantMessage(t, t.Summary)); e != nil {
			return e
		}
		return audit(tx, id, auditCode, map[string]string{"reason": reason, "created": boolString(t.Verification != nil && t.Verification.Created)})
	})
	if e == nil {
		s.changed(t.ConversationID, id)
	}
	return e
}

func (s *Service) wasCancelled(t domain.Task) (bool, error) {
	if t.Status == "CANCELLED" {
		return true, nil
	}
	var recorded bool
	e := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM audit_events WHERE task_id=? AND code='TASK_CANCELLED')`, t.ID).Scan(&recorded)
	return recorded, e
}

func markUnfinishedSteps(t *domain.Task, state string) {
	for j := range t.Steps {
		if t.Steps[j].Status == "RUNNING" || t.Steps[j].Status == "PENDING" {
			t.Steps[j].Status = state
		}
	}
}
