package app

import (
	"database/sql"
	"slices"
	"strings"
	"unicode"
)

func (s *Service) archiveCognitionMemory(payload string) error {
	var in struct {
		ID          string `json:"id"`
		WorkspaceID string `json:"workspaceId,omitempty"`
		Archived    bool   `json:"archived"`
		Priority    int    `json:"priority,omitempty"`
	}
	if err := decode(payload, &in); err != nil {
		return err
	}
	if in.ID == "" || in.Priority < 0 || in.Priority > 3 {
		return codedError("INVALID_REQUEST")
	}
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	return s.transaction(func(tx *sql.Tx) error {
		result, err := tx.Exec(`INSERT INTO cognition_memory_state(memory_id,archived,priority)
  SELECT id,?,? FROM memory_records WHERE id=? AND COALESCE(workspace_id,'')=?
  ON CONFLICT(memory_id) DO UPDATE SET archived=excluded.archived,priority=excluded.priority`,
			in.Archived, in.Priority, in.ID, in.WorkspaceID)
		if err := cognitionAffected(result, err); err != nil {
			return err
		}
		return audit(tx, "", "COGNITION_MEMORY_STATUS_CHANGED", map[string]string{"id": in.ID})
	})
}

func cognitionFTS(goal string) string {
	words := strings.FieldsFunc(strings.ToLower(goal), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	search := []string{}
	for _, concept := range cognitionConceptMatches(goal) {
		for _, phrase := range concept.phrases {
			search = append(search, "\""+phrase+"\"")
		}
	}
	for _, word := range words {
		if len(word) < 2 || len(word) > 80 {
			continue
		}
		search = append(search, "\""+word+"\"")
		if len(search) >= 32 {
			break
		}
	}
	slices.Sort(search)
	return strings.Join(slices.Compact(search), " OR ")
}

func (s *Service) searchCognitionMemories(project, goal string, includeArchived bool) ([]cognitionMemory, error) {
	query := `SELECT m.id,COALESCE(m.workspace_id,''),m.kind,m.key,m.text,m.source,m.verified,m.updated_at,
 COALESCE(a.archived,0),COALESCE(a.priority,0) FROM memory_records m
 LEFT JOIN cognition_memory_state a ON a.memory_id=m.id
 WHERE (m.workspace_id IS NULL OR m.workspace_id=?) AND (? OR (COALESCE(a.archived,0)=0
 AND NOT EXISTS(SELECT 1 FROM cognition_memory_lifetime l WHERE l.memory_id=m.id AND l.expires_at<>'' AND julianday(l.expires_at)<=julianday('now'))
 AND NOT EXISTS(SELECT 1 FROM cognition_preference_reviews r WHERE r.memory_id=m.id AND r.status='pending')))`
	args := []any{project, includeArchived}
	order := " ORDER BY m.updated_at DESC,m.id LIMIT 100"
	if term := cognitionFTS(goal); term != "" {
		order = ` ORDER BY COALESCE(a.priority,0) DESC, m.rowid IN
  (SELECT rowid FROM cognition_memory_search WHERE cognition_memory_search MATCH ?) DESC,
  m.verified DESC, m.updated_at DESC,m.id LIMIT 100`
		args = append(args, term)
	}
	rows, err := s.db.Query(query+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []cognitionMemory{}
	for rows.Next() {
		var m cognitionMemory
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.Kind, &m.Key, &m.Text, &m.Source, &m.Verified, &m.UpdatedAt, &m.Archived, &m.Priority); err != nil {
			return nil, err
		}
		records = append(records, m)
	}
	return records, rows.Err()
}

func cognitionRevision(q querier) (int64, error) {
	var revision int64
	err := q.QueryRow("SELECT revision FROM cognition_revision WHERE id=1").Scan(&revision)
	return revision, err
}
