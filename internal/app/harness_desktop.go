package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"asimi/internal/computer"
	"asimi/internal/domain"
)

type desktopTaskInput struct {
	WorkspaceID   string              `json:"workspaceId"`
	Type          computer.ActionType `json:"type"`
	ObservationID string              `json:"observationId"`
	ElementID     string              `json:"elementId"`
	Text          string              `json:"text"`
	TaskID        string              `json:"taskId,omitempty"`
}

func (s *Service) createDesktopTask(input desktopTaskInput) (AgentTask, computer.Proposal, error) {
	if input.TaskID != "" || input.ObservationID == "" || input.Type == computer.LaunchHost {
		return AgentTask{}, computer.Proposal{}, codedError("CONTROL_ACTION_DENIED")
	}
	if input.WorkspaceID == "" {
		// Reuse the application's local workspace for the task journal.
		// Desktop execution itself receives only the bound UIA proposal.
		stamp := now()
		root := s.registry.Workspace()
		err := s.transaction(func(tx *sql.Tx) error {
			if _, err := tx.Exec("INSERT INTO workspaces(id,root,name,created_at,last_used_at,trust_status) VALUES(?,?,?,?,?,'USER_SELECTED') ON CONFLICT(root) DO NOTHING",
				newID(), root, "Desktop tasks", stamp, stamp); err != nil {
				return err
			}
			return tx.QueryRow("SELECT id FROM workspaces WHERE root=?", root).Scan(&input.WorkspaceID)
		})
		if err != nil {
			return AgentTask{}, computer.Proposal{}, err
		}
	} else if _, err := s.workspace(input.WorkspaceID); err != nil {
		return AgentTask{}, computer.Proposal{}, err
	}
	stamp := now()
	task := AgentTask{ID: newID(), Desktop: true, WorkspaceID: input.WorkspaceID,
		Goal: "DESKTOP_TASK", Status: "WAITING_APPROVAL", Summary: "AGENT_ACTION_REVIEW", CreatedAt: stamp, UpdatedAt: stamp}
	input.TaskID = task.ID
	c := &s.desktopControl
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.halted.Load() || !s.mvpAdmission(s.mvpGeneration(), task.ID) {
		return AgentTask{}, computer.Proposal{}, codedError("CONTROL_STOPPED")
	}
	p, err := s.proposeControlLocked(input, &task)
	if err != nil {
		return AgentTask{}, computer.Proposal{}, err
	}
	s.changed("", task.ID)
	return task, p, nil
}

func startDesktopTask(q querier, p computer.Proposal, task *AgentTask) error {
	if task == nil {
		return nil
	}
	task.Steps = []AgentStep{
		{ID: newID(), Kind: "desktop.observe", Title: "DESKTOP_OBSERVATION", Status: "COMPLETED", Result: json.RawMessage(encode(map[string]any{
			"observationId": p.ObservationID, "app": p.Window.App, "targetHash": p.TargetHash, "observedAt": p.ObservedAt,
			"visionRequestId": p.SourceRequestID,
		}))},
		{ID: newID(), Kind: "desktop.action", Title: string(p.Type), Status: "WAITING_APPROVAL", ActionID: p.ID},
	}
	task.CurrentStep = 1
	return saveAgentTask(q, *task)
}

func updateDesktopTask(tx *sql.Tx, record computer.Record) error {
	var id string
	err := tx.QueryRow(`SELECT e.task_id FROM computer_task_evidence e JOIN agent_tasks t ON t.id=e.task_id
		WHERE e.action_id=? AND json_extract(t.json,'$.desktop')=1`, record.ID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	t, err := loadAgentTask(tx, id)
	if err != nil {
		return err
	}
	if t.CurrentStep != 1 || len(t.Steps) != 2 || t.Steps[1].ActionID != record.ID {
		return codedError("APPROVAL_MISMATCH")
	}
	step := &t.Steps[1]
	step.Status, step.Error = record.Status, record.Code
	step.Result = json.RawMessage(encode(record))
	if t.Status != "PAUSED" && t.Status != "CANCELLED" {
		switch record.Status {
		case "APPROVED", "REVALIDATING", "EXECUTING", "VERIFYING":
			t.Status, t.Summary = "RUNNING", "DESKTOP_VERIFYING"
		case "COMPLETED":
			if !record.Verified {
				return codedError("VERIFICATION_FAILED")
			}
			var consumed bool
			err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM computer_approvals WHERE action_id=? AND status='CONSUMED' AND consumed_at IS NOT NULL)", record.ID).Scan(&consumed)
			if err != nil {
				return err
			}
			if !consumed {
				return codedError("APPROVAL_MISMATCH")
			}
			t.Status, t.Summary = "COMPLETED", "DESKTOP_VERIFIED"
		case "UNKNOWN_RESULT", "RECOVERY_REQUIRED":
			t.Status, t.Summary = "PAUSED", "RECOVERY_REQUIRED"
		case "CANCELLED", "REJECTED":
			t.Status, t.Summary = "CANCELLED", "TASK_CANCELLED"
		default:
			t.Status, t.Summary = "FAILED", record.Code
		}
	}
	t.UpdatedAt = now()
	return saveAgentTask(tx, t)
}

func reflectDesktopTask(tx *sql.Tx, task AgentTask) error {
	verified := false
	text := "Desktop task ended with status " + task.Status + "."
	if len(task.Steps) == 2 && task.Steps[1].ActionID != "" {
		var kind, status, code string
		var checked, consumed bool
		err := tx.QueryRow(`SELECT a.kind,a.status,a.code,a.verified,
			EXISTS(SELECT 1 FROM computer_approvals p WHERE p.action_id=a.id
			AND p.action_hash=a.action_hash AND p.target_hash=a.target_hash AND p.status='CONSUMED' AND p.consumed_at IS NOT NULL)
			FROM computer_actions a JOIN computer_task_evidence e ON e.action_id=a.id
			WHERE e.task_id=? AND a.id=?`, task.ID, task.Steps[1].ActionID).Scan(&kind, &status, &code, &checked, &consumed)
		if err != nil {
			return err
		}
		verified = task.Status == "COMPLETED" && status == "COMPLETED" && checked && consumed
		if verified {
			text += " The approved semantic action " + kind + " was independently verified against the target."
		} else {
			text += " The recorded action outcome is " + status + "."
		}
	}
	return saveCognitionMemory(tx, task.WorkspaceID, "episode", task.ID, text, "task:"+task.ID, verified)
}

func validateDesktopTaskApproval(q querier, p computer.Proposal, status string) error {
	if !p.HarnessTask {
		return nil
	}
	task, err := loadAgentTask(q, p.TaskID)
	if err != nil {
		return err
	}
	if !task.Desktop {
		return codedError("APPROVAL_MISMATCH")
	}
	if task.Status != status || task.CurrentStep != 1 || len(task.Steps) != 2 || task.Steps[1].ActionID != p.ID {
		return codedError("CONTROL_STOPPED")
	}
	return nil
}

func (s *Service) invalidateDesktopTaskPending(id string) error {
	c := &s.desktopControl
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil || c.pending.TaskID != id {
		return nil
	}
	task, err := loadAgentTask(s.db, id)
	if err != nil {
		return err
	}
	if !task.Desktop {
		return nil
	}
	p := c.pending
	c.pending, c.observation = nil, nil
	err = finishControl(s.db, computer.Record{ID: p.ID, Type: p.Type, Status: "CANCELLED", Code: "CONTROL_STOPPED", At: c.time().Format(chatTimeFormat)}, "AWAITING_APPROVAL")
	if err == nil {
		s.changed("", id)
	}
	return err
}

func (s *Service) callCreateDesktopTask(payload string) domain.Reply {
	var input desktopTaskInput
	if err := decode(payload, &input); err != nil {
		return result(nil, err)
	}
	task, proposal, err := s.createDesktopTask(input)
	if err != nil {
		return result(nil, err)
	}
	// Return the proposal directly to the existing approval UI. Its plaintext
	// remains transient in the broker and never appears in the task journal.
	return result(struct {
		AgentTask
		Proposal computer.Proposal `json:"proposal"`
	}{task, proposal}, nil)
}

func desktopExecutionDeadline(expiry time.Time) time.Time {
	return minTime(expiry, time.Now().Add(20*time.Second))
}
