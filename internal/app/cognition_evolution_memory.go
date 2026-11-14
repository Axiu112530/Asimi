package app

import (
	"context"
	"database/sql"
	"slices"
	"strconv"
	"strings"
	"time"
)

type cognitionExpiration struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expiresAt"`
}
type cognitionMaintenanceResult struct {
	ExpiredIDs   []string `json:"expiredIds"`
	ArchiveIDs   []string `json:"archiveIds"`
	PreservedIDs []string `json:"preservedIds"`
	Applied      bool     `json:"applied"`
}

func (s *Service) runMemoryMaintenance(payload string) (cognitionMaintenanceResult, error) {
	out := cognitionMaintenanceResult{ExpiredIDs: []string{}, ArchiveIDs: []string{}, PreservedIDs: []string{}}
	var in struct {
		WorkspaceID      string                `json:"workspaceId"`
		Apply            bool                  `json:"apply"`
		ArchiveAfterDays int                   `json:"archiveAfterDays"`
		Expirations      []cognitionExpiration `json:"expirations"`
	}
	if e := decode(payload, &in); e != nil {
		return out, e
	}
	if in.ArchiveAfterDays == 0 {
		in.ArchiveAfterDays = 180
	}
	if in.ArchiveAfterDays < 1 || in.ArchiveAfterDays > 36500 || len(in.Expirations) > 100 {
		return out, codedError("INVALID_REQUEST")
	}
	if in.WorkspaceID != "" {
		if _, e := s.workspace(in.WorkspaceID); e != nil {
			return out, e
		}
	}
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	overrides := map[string]string{}
	for _, expiry := range in.Expirations {
		if expiry.ID == "" {
			return out, codedError("INVALID_REQUEST")
		}
		stamp := ""
		if expiry.ExpiresAt != "" {
			parsed, e := time.Parse(time.RFC3339, expiry.ExpiresAt)
			if e != nil {
				return out, codedError("INVALID_REQUEST")
			}
			stamp = parsed.UTC().Format(time.RFC3339Nano)
		}
		overrides[expiry.ID] = stamp
	}
	rows, e := s.db.Query(`SELECT m.id,m.kind,m.source,m.verified,m.updated_at,COALESCE(a.archived,0),COALESCE(a.priority,0),COALESCE(l.expires_at,'')
 FROM memory_records m LEFT JOIN cognition_memory_state a ON a.memory_id=m.id LEFT JOIN cognition_memory_lifetime l ON l.memory_id=m.id
 WHERE m.workspace_id IS NULL OR m.workspace_id=? ORDER BY m.id`, in.WorkspaceID)
	if e != nil {
		return out, e
	}
	seen := map[string]bool{}
	clock := time.Now()
	cutoff := clock.AddDate(0, 0, -in.ArchiveAfterDays)
	for rows.Next() {
		var id, kind, source, updated, expiry string
		var verified, archived bool
		var priority int
		if e = rows.Scan(&id, &kind, &source, &verified, &updated, &archived, &priority, &expiry); e != nil {
			break
		}
		seen[id] = true
		if stamp, ok := overrides[id]; ok {
			expiry = stamp
		}
		if expiry != "" {
			stamp, parseErr := time.Parse(time.RFC3339Nano, expiry)
			if parseErr == nil && !stamp.After(clock) {
				out.ExpiredIDs = append(out.ExpiredIDs, id)
			}
		}
		preserve := cognitionMemoryProtected(kind, source, verified, priority)
		if preserve {
			out.PreservedIDs = append(out.PreservedIDs, id)
			continue
		}
		stamp, parseErr := time.Parse(time.RFC3339Nano, updated)
		if !archived && parseErr == nil && stamp.Before(cutoff) {
			out.ArchiveIDs = append(out.ArchiveIDs, id)
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return out, e
	}
	for id := range overrides {
		if !seen[id] {
			return out, codedError("NOT_FOUND")
		}
	}
	if !in.Apply {
		return out, nil
	}
	e = s.transaction(func(tx *sql.Tx) error {
		ids := make([]string, 0, len(overrides))
		for id := range overrides {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if _, e := tx.Exec(`INSERT INTO cognition_memory_lifetime(memory_id,expires_at) VALUES(?,?) ON CONFLICT(memory_id) DO UPDATE SET expires_at=excluded.expires_at`, id, overrides[id]); e != nil {
				return e
			}
		}
		for _, id := range out.ArchiveIDs {
			if _, e := tx.Exec(`INSERT INTO cognition_memory_state(memory_id,archived,priority) VALUES(?,1,0) ON CONFLICT(memory_id) DO UPDATE SET archived=1`, id); e != nil {
				return e
			}
		}
		return audit(tx, "", "COGNITION_MEMORY_MAINTENANCE", map[string]string{"archived": strconv.Itoa(len(out.ArchiveIDs)), "expirationControls": strconv.Itoa(len(overrides))})
	})
	out.Applied = e == nil
	return out, e
}

// Automatic governance runs locally at startup and checks its persisted daily
// cadence from cognitionLoop. It shares the manual policy and only archives.
func cognitionMemoryProtected(kind, source string, verified bool, priority int) bool {
	return kind == "user" || source == "manual" || strings.HasPrefix(source, "correction:") || priority >= 2 || kind == "procedure" && verified
}

func (s *Service) automaticMemoryMaintenance(clock time.Time) error {
	if s.lifetime == nil || s.lifetime.Err() != nil {
		return nil
	}
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	s.mvp.signalMu.Lock()
	defer s.mvp.signalMu.Unlock()
	if s.lifetime.Err() != nil || s.mvp.stopping > 0 {
		return nil
	}
	// The transaction's context prevents a shutdown from committing a partial
	// maintenance pass. Stop uses the same signal lock as the learning worker.
	conn, err := s.db.Conn(s.lifetime)
	if err != nil {
		return err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(s.lifetime, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	err = func() error {
		settings, err := loadCognitionSettings(tx)
		if err != nil || !settings.Memory || !settings.AutomaticLearning {
			return err
		}
		var paused bool
		if err := tx.QueryRowContext(s.lifetime, "SELECT EXISTS(SELECT 1 FROM cognition_learning_control WHERE paused=1)").Scan(&paused); err != nil {
			return err
		}
		if paused {
			return nil
		}
		var last string
		err = tx.QueryRowContext(s.lifetime, "SELECT last_run_at FROM cognition_memory_maintenance WHERE id=1").Scan(&last)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if stamp, parseErr := time.Parse(time.RFC3339Nano, last); parseErr == nil && clock.Before(stamp.Add(24*time.Hour)) {
			return nil
		}
		cutoff := clock.UTC().AddDate(0, 0, -180).Format(time.RFC3339Nano)
		rows, err := tx.QueryContext(s.lifetime, `SELECT m.id,m.kind,m.source,m.verified,COALESCE(a.priority,0)
 FROM memory_records m LEFT JOIN cognition_memory_state a ON a.memory_id=m.id
 WHERE COALESCE(a.archived,0)=0 AND julianday(m.updated_at)<julianday(?)
 AND (m.workspace_id IS NULL OR EXISTS(SELECT 1 FROM workspaces w WHERE w.id=m.workspace_id)) ORDER BY m.id`, cutoff)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id, kind, source string
			var verified bool
			var priority int
			if err = rows.Scan(&id, &kind, &source, &verified, &priority); err != nil {
				break
			}
			if !cognitionMemoryProtected(kind, source, verified, priority) {
				ids = append(ids, id)
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := tx.ExecContext(s.lifetime, `INSERT INTO cognition_memory_state(memory_id,archived,priority) VALUES(?,1,0)
 ON CONFLICT(memory_id) DO UPDATE SET archived=1`, id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(s.lifetime, `INSERT INTO cognition_memory_maintenance(id,last_run_at) VALUES(1,?)
 ON CONFLICT(id) DO UPDATE SET last_run_at=excluded.last_run_at`, clock.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		if err := audit(tx, "", "COGNITION_AUTOMATIC_MEMORY_MAINTENANCE", map[string]string{"archived": strconv.Itoa(len(ids)), "archiveAfterDays": "180"}); err != nil {
			return err
		}
		return s.lifetime.Err()
	}()
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && rollbackErr != sql.ErrTxDone {
			discardConnection(conn)
		}
		return err
	}
	if err = tx.Commit(); err != nil {
		if _, rollbackErr := conn.ExecContext(context.Background(), "ROLLBACK"); rollbackErr != nil {
			discardConnection(conn)
		}
		return err
	}
	return nil
}
