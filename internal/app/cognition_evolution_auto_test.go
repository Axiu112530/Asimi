package app

import (
	"database/sql"
	"testing"
	"time"
)

func evolutionOldMemory(t *testing.T, s *Service, project, kind, key, source string, verified bool, priority int) string {
	t.Helper()
	var id string
	if err := s.transaction(func(tx *sql.Tx) error {
		if err := saveCognitionMemory(tx, project, kind, key, "A local governance test record.", source, verified); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT id FROM memory_records WHERE COALESCE(workspace_id,'')=? AND kind=? AND key=?", project, kind, key).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE memory_records SET updated_at='2020-01-01T00:00:00Z' WHERE id=?", id); err != nil {
			return err
		}
		if priority > 0 {
			_, err := tx.Exec("INSERT INTO cognition_memory_state(memory_id,archived,priority) VALUES(?,0,?)", id, priority)
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return id
}
func evolutionArchived(t *testing.T, s *Service, id string) bool {
	t.Helper()
	var archived bool
	if err := s.db.QueryRow("SELECT COALESCE((SELECT archived FROM cognition_memory_state WHERE memory_id=?),0)", id).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	return archived
}
func TestEvolutionAutomaticMaintenanceAtStartupAcrossExistingScopes(t *testing.T) {
	s := aiFixture(t, nil)
	first := mvpWorkspace(t, s, t.TempDir())
	second := mvpWorkspace(t, s, t.TempDir())
	low := []string{evolutionOldMemory(t, s, first.ID, "episode", "low-first", "task:old-first", false, 0), evolutionOldMemory(t, s, second.ID, "procedure", "low-second", "task:old-second", false, 0)}
	high := []string{evolutionOldMemory(t, s, "", "user", "explicit-user", "preference:old-user", false, 0), evolutionOldMemory(t, s, first.ID, "project", "manual", "manual", true, 0), evolutionOldMemory(t, s, first.ID, "project", "correction", "correction:old", true, 0), evolutionOldMemory(t, s, second.ID, "episode", "priority", "task:old-priority", false, 2), evolutionOldMemory(t, s, second.ID, "procedure", "verified", "task:old-procedure", true, 0)}
	if err := s.initializeEvolution(); err != nil {
		t.Fatal(err)
	}
	for _, id := range low {
		if !evolutionArchived(t, s, id) {
			t.Fatal("startup left old low-value memory active")
		}
	}
	for _, id := range high {
		if evolutionArchived(t, s, id) {
			t.Fatal("startup archived protected memory")
		}
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM memory_records").Scan(&count); err != nil || count != 7 {
		t.Fatalf("automatic policy deleted records: %d %v", count, err)
	}
}
func TestEvolutionAutomaticMaintenanceRespectsSettingsAndPause(t *testing.T) {
	for _, mode := range []string{"memory-disabled", "learning-disabled", "paused", "stopping", "closed"} {
		t.Run(mode, func(t *testing.T) {
			s := aiFixture(t, nil)
			w := mvpWorkspace(t, s, t.TempDir())
			id := evolutionOldMemory(t, s, w.ID, "episode", "old", "task:old", false, 0)
			switch mode {
			case "memory-disabled":
				mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": false, "automaticLearning": true})
			case "learning-disabled":
				mustCall(t, s, "SetCognitionSettings", map[string]bool{"memory": true, "automaticLearning": false})
			case "paused":
				mustCall(t, s, "PauseCognitionLearning", struct{}{})
			case "stopping":
				done := s.beginMVPStop()
				defer done()
			case "closed":
				s.stop()
			}
			if err := s.initializeEvolution(); err != nil {
				t.Fatal(err)
			}
			if evolutionArchived(t, s, id) {
				t.Fatalf("autonomous maintenance ignored %s", mode)
			}
		})
	}
}

func TestEvolutionAutomaticMaintenanceDailyCadenceAndResume(t *testing.T) {
	s := aiFixture(t, nil)
	w := mvpWorkspace(t, s, t.TempDir())
	clock := time.Now().UTC()
	first := evolutionOldMemory(t, s, w.ID, "episode", "first-daily", "task:daily", false, 0)
	if err := s.automaticMemoryMaintenance(clock); err != nil {
		t.Fatal(err)
	}
	if !evolutionArchived(t, s, first) {
		t.Fatal("initial automatic pass did not archive")
	}
	later := evolutionOldMemory(t, s, w.ID, "episode", "later-daily", "task:later", false, 0)
	if err := s.automaticMemoryMaintenance(clock.Add(23 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if evolutionArchived(t, s, later) {
		t.Fatal("automatic pass ran before the daily interval")
	}
	mustCall(t, s, "PauseCognitionLearning", struct{}{})
	if err := s.automaticMemoryMaintenance(clock.Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if evolutionArchived(t, s, later) {
		t.Fatal("daily pass ignored learning pause")
	}
	var last string
	if err := s.db.QueryRow("SELECT last_run_at FROM cognition_memory_maintenance WHERE id=1").Scan(&last); err != nil || last != clock.Format(time.RFC3339Nano) {
		t.Fatalf("paused maintenance consumed schedule: %q %v", last, err)
	}
	mustCall(t, s, "ResumeCognitionLearning", struct{}{})
	if err := s.automaticMemoryMaintenance(clock.Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !evolutionArchived(t, s, later) {
		t.Fatal("overdue automatic pass did not resume")
	}
}
func TestEvolutionAutomaticMaintenanceStoppedContextDoesNotAdvanceSchedule(t *testing.T) {
	s := aiFixture(t, nil)
	w := mvpWorkspace(t, s, t.TempDir())
	id := evolutionOldMemory(t, s, w.ID, "episode", "stopped-daily", "task:stopped", false, 0)
	s.stop()
	before, err := cognitionRevision(s.db)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.automaticMemoryMaintenance(time.Now()); err != nil {
		t.Fatal(err)
	}
	after, err := cognitionRevision(s.db)
	if err != nil || after != before {
		t.Fatalf("stopped maintenance mutated records: %d %d %v", before, after, err)
	}
	if evolutionArchived(t, s, id) {
		t.Fatal("stopped maintenance archived record")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM cognition_memory_maintenance").Scan(&count); err != nil || count != 0 {
		t.Fatalf("stopped maintenance wrote schedule: %d %v", count, err)
	}
}
