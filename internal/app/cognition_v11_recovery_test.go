package app

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCognitionV11RollsBackLearningAndRetries(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	mustExec := func(query string) {
		t.Helper()
		if _, err := s.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO cognition_settings VALUES(1,'{"memory":true,"automaticLearning":true,"projectLearning":true}')`)
	mustExec(`CREATE TRIGGER cognition_test_fault BEFORE UPDATE OF status ON learning_jobs WHEN NEW.status='done'
 BEGIN SELECT RAISE(ABORT,'injected storage failure'); END`)
	task := AgentTask{ID: newID(), WorkspaceID: w.ID, Goal: "Record the project result", Status: "COMPLETED", CreatedAt: now(), UpdatedAt: now()}
	if err := s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, task) }); err != nil {
		t.Fatal(err)
	}
	process := func() { s.cognitionMu.Unlock(); s.processCognitionJob(); s.cognitionMu.Lock() }
	process()
	history, err := s.cognitionHistory(w.ID)
	if err != nil || len(history) != 1 || history[0].Attempts != 1 || history[0].Error == "" {
		t.Fatalf("history %+v err %v", history, err)
	}
	records, err := s.cognitionMemories(w.ID)
	if err != nil || len(records) != 0 {
		t.Fatalf("partial transaction persisted: %+v %v", records, err)
	}
	mustExec("DROP TRIGGER cognition_test_fault")
	mustExec("UPDATE cognition_learning_history SET next_attempt_at=''")
	process()
	records, err = s.cognitionMemories(w.ID)
	if err != nil || len(records) != 1 || records[0].Key != task.ID {
		t.Fatalf("retry did not recover: %+v %v", records, err)
	}
}

func TestCognitionV11PackageProjectionRecoversAfterPartialWrite(t *testing.T) {
	s, dir, _ := fixture(t)
	in := cognitionSkillInput{Scope: "user", Slug: "recover", Name: "Recover", Description: "Recover local skill", Instructions: "Read local notes."}
	mustCall(t, s, "SaveCognitionSkill", in)
	s.cognitionMu.Lock()
	var id string
	if err := s.db.QueryRow("SELECT id FROM skill_packages WHERE slug='recover'").Scan(&id); err != nil {
		s.cognitionMu.Unlock()
		t.Fatal(err)
	}
	path := filepath.Join(dir, "skills", id+"-v1")
	blocker := filepath.Join(path, "manifest.json")
	if err := os.MkdirAll(blocker, 0700); err != nil {
		s.cognitionMu.Unlock()
		t.Fatal(err)
	}
	s.cognitionMu.Unlock()
	s.syncCognitionPackage()
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	var queued int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM cognition_package_writes WHERE package_id=?", id).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("write was dropped: %d %v", queued, err)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		s.syncCognitionPackage()
		if raw, err := os.ReadFile(blocker); err == nil && strings.Contains(string(raw), "Recover local skill") {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("projection did not recover")
}

func BenchmarkCognitionV11Context(b *testing.B) {
	dir := b.TempDir()
	db, err := openStore(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	s := &Service{db: db}
	if _, err = db.Exec(`INSERT INTO cognition_settings VALUES(1,'{"memory":true}')`); err != nil {
		b.Fatal(err)
	}
	if err := s.initializeMVP(); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := saveCognitionMemory(db, "", "user", newID(), "Prefer incremental Go tests and focused changes", "manual", true); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := s.cognitionContext("", "verify Go tests", 4200, true)
		if err != nil || len(out.Memories) != 1 {
			b.Fatalf("%+v %v", out, err)
		}
	}
}
