package app

import (
	"database/sql"
	"time"
)

type cognitionLearningRecord struct {
	TaskID      string `json:"taskId"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	Error       string `json:"error,omitempty"`
	DurationMS  int64  `json:"durationMs"`
	CreatedAt   string `json:"createdAt"`
	CompletedAt string `json:"completedAt,omitempty"`
}

func (s *Service) cognitionHistory(project string) ([]cognitionLearningRecord, error) {
	rows, err := s.db.Query(`SELECT h.task_id,CASE WHEN h.status='queued' THEN j.status ELSE h.status END,
 h.attempts,h.last_error,h.duration_ms,h.created_at,COALESCE(h.completed_at,'') FROM cognition_learning_history h
 JOIN learning_jobs j ON j.task_id=h.task_id JOIN agent_tasks t ON t.id=h.task_id
 WHERE t.workspace_id=? ORDER BY h.created_at DESC,h.task_id LIMIT 100`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []cognitionLearningRecord{}
	for rows.Next() {
		var entry cognitionLearningRecord
		if err := rows.Scan(&entry.TaskID, &entry.Status, &entry.Attempts, &entry.Error, &entry.DurationMS, &entry.CreatedAt, &entry.CompletedAt); err != nil {
			return nil, err
		}
		records = append(records, entry)
	}
	return records, rows.Err()
}

func (s *Service) updateCognitionJob(payload string) error {
	var in struct {
		ID          string `json:"id"`
		WorkspaceID string `json:"workspaceId"`
		Operation   string `json:"operation"`
	}
	if err := decode(payload, &in); err != nil {
		return err
	}
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	return s.transaction(func(tx *sql.Tx) error {
		scope := ` AND EXISTS(SELECT 1 FROM agent_tasks WHERE agent_tasks.id=cognition_learning_history.task_id AND workspace_id=?)`
		var result sql.Result
		var err error
		switch in.Operation {
		case "cancel":
			result, err = tx.Exec("UPDATE cognition_learning_history SET status='cancelled',completed_at=? WHERE task_id=? AND status='queued'"+scope, now(), in.ID, in.WorkspaceID)
		case "retry":
			result, err = tx.Exec("UPDATE cognition_learning_history SET status='queued',attempts=0,last_error='',next_attempt_at='',completed_at=NULL,created_at=? WHERE task_id=? AND status IN ('failed','cancelled','expired')"+scope, now(), in.ID, in.WorkspaceID)
		default:
			return codedError("INVALID_REQUEST")
		}
		return cognitionAffected(result, err)
	})
}

func recordCognitionFailure(db *sql.DB, id string, elapsed time.Duration, err error) error {
	if id == "" {
		return nil
	}
	code := errorCode(err)
	if !cognitionText(code, 100) {
		code = "COGNITION_LEARNING_FAILED"
	}
	_, err = db.Exec(`UPDATE cognition_learning_history SET attempts=attempts+1,last_error=?,duration_ms=?,
 next_attempt_at=?,status=CASE WHEN attempts>=2 THEN 'failed' ELSE 'queued' END,
 completed_at=CASE WHEN attempts>=2 THEN ? ELSE NULL END WHERE task_id=? AND status='queued'`,
		code, elapsed.Milliseconds(), time.Now().UTC().Add(2*time.Second).Format(time.RFC3339Nano), now(), id)
	return err
}
