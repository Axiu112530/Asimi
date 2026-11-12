package app

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCommitFailureDoesNotPoisonConnection(t *testing.T) {
	db, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{db: db}
	_, err = db.Exec(`CREATE TABLE fault_child(
		id TEXT REFERENCES conversations(id) DEFERRABLE INITIALLY DEFERRED
	)`)
	if err != nil {
		t.Fatal(err)
	}
	err = s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO fault_child VALUES('missing')")
		if err != nil {
			t.Fatalf("fault must be raised by COMMIT: %v", err)
		}
		return nil
	})
	if err == nil {
		t.Fatal("invalid deferred reference committed")
	}
	if got := classifyStorage(err); got != "STORAGE_CONSTRAINT" {
		t.Fatalf("commit classification = %s: %v", got, err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM fault_child").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed commit leaked uncommitted data: rows=%d err=%v", count, err)
	}
	if err := s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO fault_child VALUES(NULL)")
		return err
	}); err != nil {
		t.Fatalf("failed commit poisoned next transaction: %v", err)
	}
}

func openV1(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "asimi.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(db, schemaV1); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

func TestV2MigrationFailureAndInterruptedDDL(t *testing.T) {
	const fixtureKey = "ASIMI_MIGRATION_FIXTURE"
	if dir := os.Getenv(fixtureKey); dir != "" {
		db, err := sql.Open("sqlite", filepath.Join(dir, "asimi.db"))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(schemaV2); err != nil {
			t.Fatal(err)
		}
		os.Exit(77)
	}

	dir := t.TempDir()
	db := openV1(t, dir)
	if _, err := db.Exec(`INSERT INTO conversations VALUES ('historic','Keep this','date','date');
		CREATE INDEX file_intents_unsettled ON conversations(title);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if opened, err := openStore(dir); err == nil {
		opened.Close()
		t.Fatal("conflicting index did not fail migration")
	}
	assertVersion := func(db *sql.DB, want int) {
		t.Helper()
		var version int
		if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != want {
			t.Fatalf("schema version = %d, want %d: %v", version, want, err)
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "asimi.db"))
	if err != nil {
		t.Fatal(err)
	}
	assertVersion(db, 1)
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='file_intents'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed DDL did not roll back: %d %v", count, err)
	}
	if _, err := db.Exec("DROP INDEX file_intents_unsettled"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	runInterruptedFixture(t, "^TestV2MigrationFailureAndInterruptedDDL$", fixtureKey+"="+dir)
	db, err = sql.Open("sqlite", filepath.Join(dir, "asimi.db"))
	if err != nil {
		t.Fatal(err)
	}
	assertVersion(db, 1)
	db.Close()
	for i := 0; i < 2; i++ {
		db, err = openStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		assertVersion(db, 6)
		var title string
		if err := db.QueryRow("SELECT title FROM conversations WHERE id='historic'").Scan(&title); err != nil || title != "Keep this" {
			t.Fatalf("v1 history changed: %q %v", title, err)
		}
		db.Close()
	}
}

func runInterruptedFixture(t *testing.T, testPattern string, environment ...string) {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(program, "-test.run="+testPattern, "-test.timeout=15s")
	command.Env = append(os.Environ(), environment...)
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 77 {
		t.Fatalf("child missed the injection boundary: %v\n%s", err, output)
	}
}

func TestSQLiteFaultClassification(t *testing.T) {
	dir := t.TempDir()
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expect := func(want string, err error) {
		t.Helper()
		if got := classifyStorage(err); got != want {
			t.Fatalf("classification = %q, want %q: %v", got, want, err)
		}
	}
	if _, err := db.Exec("CREATE TABLE fault_payload(value BLOB); PRAGMA busy_timeout=25"); err != nil {
		t.Fatal(err)
	}
	writer, err := sql.Open("sqlite", filepath.Join(dir, "asimi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	tx, err := writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO fault_payload VALUES(1)"); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO fault_payload VALUES(2)")
	expect("STORAGE_BUSY", err)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO fault_payload VALUES(2)")
	expect("STORAGE_READ_ONLY", err)
	if _, err := db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	var pages int
	if err := db.QueryRow("PRAGMA page_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA max_page_count=%d", pages)); err != nil {
		t.Fatal(err)
	}
	err = storeTransaction(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO fault_payload VALUES(3)"); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO fault_payload VALUES(zeroblob(1048576))")
		return err
	})
	expect("STORAGE_FULL", err)
	var count int
	if err := db.QueryRow("SELECT count(*) FROM fault_payload").Scan(&count); err != nil || count != 0 {
		t.Fatalf("SQLITE_FULL left partial data: %d %v", count, err)
	}
	db.Close()
	expect("STORAGE_UNAVAILABLE", storeTransaction(db, func(*sql.Tx) error { return nil }))
}

func TestDamagedDatabaseIsPreserved(t *testing.T) {
	for _, bytes := range [][]byte{nil, []byte("SQLite format 3\x00"), make([]byte, 256)} {
		dir := t.TempDir()
		path := filepath.Join(dir, "asimi.db")
		if err := os.WriteFile(path, bytes, 0600); err != nil {
			t.Fatal(err)
		}
		db, err := openStore(dir)
		if err == nil {
			db.Close()
			t.Fatal("incomplete database was initialized")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(bytes) {
			t.Fatalf("damaged data changed: %v", err)
		}
	}
}

func TestReadOnlyDatabaseFileRejectsWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "asimi.db")
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	db, err = openStore(dir)
	if err == nil {
		_, err = db.Exec("INSERT INTO conversations VALUES ('blocked','blocked','date','date')")
		db.Close()
	}
	if code := classifyStorage(err); code != "STORAGE_READ_ONLY" && code != "STORAGE_IO" {
		t.Fatalf("read-only file write classification = %q: %v", code, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("read-only database changed: %v", err)
	}
}

func TestProfileLockIsExclusiveAndReusable(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireProfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := acquireProfile(dir)
	if err == nil {
		second()
		release()
		t.Fatal("simultaneous recovery was allowed")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	release, err = acquireProfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}
