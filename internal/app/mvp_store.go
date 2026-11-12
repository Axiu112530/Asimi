package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func (s *Service) initializeMVP() error {
	s.mvp.controls = map[string]context.CancelFunc{}
	s.mvp.immutable = map[string]string{}
	s.mvp.faults = map[string]bool{}
	return s.transaction(func(tx *sql.Tx) error {
		stamp := now()
		for _, skill := range builtinSkills() {
			if _, e := tx.Exec("INSERT OR IGNORE INTO skill_settings(id,enabled,updated_at) VALUES(?,1,?)", skill.ID, stamp); e != nil {
				return e
			}
		}
		// A restart cannot restore a native selection or execution context. Unused
		// approvals expire; consumed intents remain evidence and never replay.
		if _, e := tx.Exec("UPDATE product_actions SET status=CASE WHEN status='EXECUTING' THEN 'RECOVERY_REQUIRED' ELSE 'EXPIRED' END,error_code=CASE WHEN status='EXECUTING' THEN 'UNKNOWN_RESULT' ELSE 'APPROVAL_SESSION_ENDED' END,updated_at=? WHERE status IN ('PENDING','EXECUTING')", stamp); e != nil {
			return e
		}
		rows, e := tx.Query(`SELECT json FROM agent_tasks WHERE status IN ('RUNNING','WAITING_APPROVAL')
OR (status IN ('PAUSED','CANCELLED') AND EXISTS (SELECT 1 FROM product_actions WHERE task_id=agent_tasks.id AND status IN ('RECOVERY_REQUIRED','EXPIRED'))) `)
		if e != nil {
			return e
		}
		var tasks []AgentTask
		for rows.Next() {
			var raw string
			var t AgentTask
			if e = rows.Scan(&raw); e != nil {
				break
			}
			if e = json.Unmarshal([]byte(raw), &t); e != nil {
				break
			}
			tasks = append(tasks, t)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return e
		}
		for _, t := range tasks {
			if t.Status != "CANCELLED" {
				t.Status = "PAUSED"
				t.Summary = "RECOVERY_REQUIRED"
			}
			t.UpdatedAt = stamp
			for i := range t.Steps {
				if t.Steps[i].ActionID != "" {
					var actionStatus string
					if e = tx.QueryRow("SELECT status FROM product_actions WHERE id=? AND task_id=?", t.Steps[i].ActionID, t.ID).Scan(&actionStatus); e != nil {
						return e
					}
					if actionStatus == "RECOVERY_REQUIRED" || actionStatus == "EXPIRED" {
						t.Steps[i].Status = actionStatus
						continue
					}
				}
				if t.Steps[i].Status == "RUNNING" {
					t.Steps[i].Status = "PENDING"
				}
				if t.Steps[i].Status == "WAITING_APPROVAL" {
					t.Steps[i].Status = "RECOVERY_REQUIRED"
				}
				if t.Steps[i].Status == "EXECUTING" {
					t.Steps[i].Status = "RECOVERY_REQUIRED"
				}
			}
			if e = saveAgentTask(tx, t); e != nil {
				return e
			}
		}
		return nil
	})
}

func saveAgentTask(q querier, t AgentTask) error {
	if _, e := q.Exec(`INSERT INTO agent_tasks(id,workspace_id,status,json,created_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,json=excluded.json,updated_at=excluded.updated_at`, t.ID, t.WorkspaceID, t.Status, encode(t), t.CreatedAt, t.UpdatedAt); e != nil {
		return e
	}
	for i, step := range t.Steps {
		if _, e := q.Exec(`INSERT INTO agent_task_steps(id,task_id,ordinal,status,json) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,json=excluded.json`, step.ID, t.ID, i, step.Status, encode(step)); e != nil {
			return e
		}
	}
	return nil
}
func loadAgentTask(q querier, id string) (AgentTask, error) {
	var t AgentTask
	var raw string
	e := q.QueryRow("SELECT json FROM agent_tasks WHERE id=?", id).Scan(&raw)
	if e == nil {
		e = json.Unmarshal([]byte(raw), &t)
	}
	return t, mvpNotFound(e)
}
func mvpNotFound(e error) error {
	if errors.Is(e, sql.ErrNoRows) {
		return codedError("NOT_FOUND")
	}
	return e
}
func (s *Service) workspace(id string) (Workspace, error) {
	var w Workspace
	e := s.db.QueryRow("SELECT id,name,root,created_at,last_used_at,trust_status FROM workspaces WHERE id=?", id).Scan(&w.ID, &w.Name, &w.Root, &w.CreatedAt, &w.LastUsedAt, &w.TrustStatus)
	return w, mvpNotFound(e)
}

func loadProductAction(q querier, id string) (ProductAction, error) {
	var a ProductAction
	var raw string
	var result sql.NullString
	var task sql.NullString
	e := q.QueryRow("SELECT id,task_id,kind,status,hash,expires_at,created_at,updated_at,proposal_json,result_json,error_code FROM product_actions WHERE id=?", id).Scan(&a.ID, &task, &a.Kind, &a.Status, &a.Hash, &a.ExpiresAt, &a.CreatedAt, &a.UpdatedAt, &raw, &result, &a.Error)
	if e == nil {
		a.TaskID = task.String
		a.Result = json.RawMessage(result.String)
		e = json.Unmarshal([]byte(raw), &a.Proposal)
	}
	return a, mvpNotFound(e)
}
func productEvent(q querier, a ProductAction, event string) error {
	_, e := q.Exec("INSERT INTO product_action_journal(id,action_id,event,json,created_at) VALUES(?,?,?,?,?)", newID(), a.ID, event, encode(map[string]string{"hash": a.Hash, "status": a.Status, "error": a.Error}), now())
	return e
}

func (s *Service) expireMVP() error {
	rows, e := s.db.Query("SELECT id FROM product_actions WHERE status='PENDING' AND expires_at<=?", now())
	if e != nil {
		return e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			break
		}
		ids = append(ids, id)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		if _, e = s.finishPendingAction(id, "EXPIRED", "APPROVAL_EXPIRED"); e != nil {
			return e
		}
	}
	return nil
}
func validProductExpiry(a ProductAction) bool {
	expiry, e := time.Parse(time.RFC3339Nano, a.ExpiresAt)
	return e == nil && time.Now().Before(expiry)
}

func (s *Service) mvpState() (MVPState, error) {
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	v := MVPState{Workspaces: []Workspace{}, AgentTasks: []AgentTask{}, Actions: []ProductAction{}, Skills: builtinSkills(), Tools: commonToolCatalog()}
	if e := s.expireMVP(); e != nil {
		return v, e
	}
	rows, e := s.db.Query("SELECT id,name,root,created_at,last_used_at,trust_status FROM workspaces ORDER BY created_at DESC,id")
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var w Workspace
		if e = rows.Scan(&w.ID, &w.Name, &w.Root, &w.CreatedAt, &w.LastUsedAt, &w.TrustStatus); e != nil {
			break
		}
		v.Workspaces = append(v.Workspaces, w)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return v, e
	}
	rows, e = s.db.Query("SELECT json FROM agent_tasks ORDER BY updated_at DESC,id LIMIT 200")
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var raw string
		var t AgentTask
		if e = rows.Scan(&raw); e != nil {
			break
		}
		if e = json.Unmarshal([]byte(raw), &t); e != nil {
			break
		}
		if s.mvp.faults["task:"+t.ID] {
			t.Status = "PAUSED"
			t.Summary = "RECOVERY_REQUIRED"
		}
		v.AgentTasks = append(v.AgentTasks, t)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return v, e
	}
	rows, e = s.db.Query("SELECT id FROM product_actions ORDER BY created_at DESC,id LIMIT 200")
	if e != nil {
		return v, e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			break
		}
		ids = append(ids, id)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return v, e
	}
	for _, id := range ids {
		a, err := loadProductAction(s.db, id)
		if err != nil {
			return v, err
		}
		if s.mvp.faults[id] {
			a.Status = "RECOVERY_REQUIRED"
			a.Error = "OUTCOME_NOT_SAVED"
		}
		v.Actions = append(v.Actions, a)
	}
	for i := range v.Skills {
		if e = s.db.QueryRow("SELECT enabled FROM skill_settings WHERE id=?", v.Skills[i].ID).Scan(&v.Skills[i].Enabled); e != nil {
			return v, e
		}
	}
	e = s.db.QueryRow("SELECT completed FROM onboarding_state WHERE id='onboarding'").Scan(&v.Onboarding.Completed)
	return v, e
}
