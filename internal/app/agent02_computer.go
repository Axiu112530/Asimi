package app

import (
	"database/sql"
	"time"
)

const schemaAgent02Computer = `
CREATE TABLE computer_task_evidence (
 id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 action_id TEXT UNIQUE REFERENCES computer_actions(id),
 request_id TEXT UNIQUE REFERENCES ai_requests(id),
 created_at TEXT NOT NULL,
 CHECK((action_id IS NULL) <> (request_id IS NULL))
);
CREATE INDEX computer_task_evidence_by_task ON computer_task_evidence(task_id, created_at DESC, id);`

// The linked journal or request remains the source of execution status, including
// recovery. Computer evidence does not advance the task's independently run plan.
type computerTaskEvidence struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	ActionID       string `json:"actionId,omitempty"`
	ActionType     string `json:"actionType,omitempty"`
	RequestID      string `json:"requestId,omitempty"`
	MessageID      string `json:"messageId,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	Status         string `json:"status"`
	Verified       bool   `json:"verified"`
	ErrorCode      string `json:"errorCode,omitempty"`
	CreatedAt      string `json:"createdAt"`
	Selector       string `json:"selector,omitempty"`
	ObservedAt     string `json:"observedAt,omitempty"`
}

func validateComputerTask(q querier, taskID string) error {
	if taskID == "" || len(taskID) > 128 {
		return codedError("INVALID_REQUEST")
	}
	var found int
	return q.QueryRow("SELECT 1 FROM agent_tasks WHERE id=?", taskID).Scan(&found)
}

func linkComputerTask(tx *sql.Tx, taskID, actionID, requestID string) error {
	if taskID == "" {
		return nil
	}
	if err := validateComputerTask(tx, taskID); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format(chatTimeFormat)
	_, err := tx.Exec(`INSERT INTO computer_task_evidence(id,task_id,action_id,request_id,created_at)
VALUES(?,?,NULLIF(?,''),NULLIF(?,''),?)`, newID(), taskID, actionID, requestID, stamp)
	return err
}

func (s *Service) computerTaskEvidence(taskID string) ([]computerTaskEvidence, error) {
	if err := validateComputerTask(s.db, taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
WITH model_actions AS (
 SELECT r.id,r.message_id,r.conversation_id,meta.json,
        json_extract(meta.json,'$.computerSuggestion.actionId') AS action_id
 FROM computer_task_evidence source_e
 JOIN ai_requests r ON r.id=source_e.request_id
 JOIN ai_message_metadata meta ON meta.message_id=r.message_id
 WHERE source_e.task_id=?1 AND json_type(meta.json,'$.computerSuggestion.actionId')='text'
)
SELECT e.id, CASE WHEN a.id IS NULL THEN 'computer.screen_analysis' ELSE 'computer.action' END,
 COALESCE(a.id,''), COALESCE(a.kind,''), COALESCE(r.id,source.id,''),
 COALESCE(r.message_id,source.message_id,''), COALESCE(r.conversation_id,source.conversation_id,''),
 COALESCE(a.status,r.status), COALESCE(a.verified,0), COALESCE(a.code,json_extract(u.json,'$.errorCode'),''), e.created_at,
 COALESCE(json_extract(source.json,'$.computerSuggestion.selector'),json_extract(response.json,'$.computerSuggestion.selector'),''),
 COALESCE(json_extract(source.json,'$.computerSuggestion.observedAt'),json_extract(response.json,'$.computerSuggestion.observedAt'),'')
FROM computer_task_evidence e
LEFT JOIN computer_actions a ON a.id=e.action_id
LEFT JOIN ai_requests r ON r.id=e.request_id
LEFT JOIN ai_usage u ON u.request_id=r.id
LEFT JOIN ai_message_metadata response ON response.message_id=r.message_id
LEFT JOIN model_actions source ON source.action_id=a.id
WHERE e.task_id=?1 ORDER BY e.created_at DESC,e.id LIMIT 50`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	evidence := []computerTaskEvidence{}
	for rows.Next() {
		var entry computerTaskEvidence
		if err := rows.Scan(&entry.ID, &entry.Kind, &entry.ActionID, &entry.ActionType, &entry.RequestID, &entry.MessageID, &entry.ConversationID,
			&entry.Status, &entry.Verified, &entry.ErrorCode, &entry.CreatedAt, &entry.Selector, &entry.ObservedAt); err != nil {
			return nil, err
		}
		evidence = append(evidence, entry)
	}
	return evidence, rows.Err()
}
