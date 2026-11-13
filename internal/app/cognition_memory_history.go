package app

import "database/sql"

type cognitionMemoryChange struct {
	ID       int64  `json:"id"`
	MemoryID string `json:"memoryId"`
	Previous string `json:"previous"`
	Current  string `json:"current"`
	Source   string `json:"source"`
	At       string `json:"at"`
}

func (s *Service) cognitionMemoryChanges(project string) ([]cognitionMemoryChange, error) {
	rows, err := s.db.Query(`SELECT h.id,h.memory_id,h.previous,h.current,h.source,h.created_at
 FROM cognition_memory_changes h JOIN memory_records m ON m.id=h.memory_id
 WHERE m.workspace_id IS NULL OR m.workspace_id=? ORDER BY h.id DESC LIMIT 50`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	changes := []cognitionMemoryChange{}
	for rows.Next() {
		var c cognitionMemoryChange
		if err := rows.Scan(&c.ID, &c.MemoryID, &c.Previous, &c.Current, &c.Source, &c.At); err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	return changes, rows.Err()
}

func (s *Service) dismissCognitionMemoryChange(payload string) error {
	var in struct {
		ID          int64  `json:"id"`
		WorkspaceID string `json:"workspaceId,omitempty"`
	}
	if err := decode(payload, &in); err != nil {
		return err
	}
	return s.transaction(func(tx *sql.Tx) error {
		result, err := tx.Exec(`DELETE FROM cognition_memory_changes WHERE id=? AND EXISTS(
  SELECT 1 FROM memory_records WHERE id=memory_id AND (workspace_id IS NULL OR workspace_id=?))`, in.ID, in.WorkspaceID)
		return cognitionAffected(result, err)
	})
}
