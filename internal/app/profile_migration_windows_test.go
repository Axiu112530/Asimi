package app

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenStoreMigratesLegacyDatabase(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "deskpilot.db")
	db, err := sql.Open("sqlite", legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE migration_probe(value TEXT); INSERT INTO migration_probe VALUES('preserved')"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	var value string
	if err = migrated.QueryRow("SELECT value FROM migration_probe").Scan(&value); err != nil || value != "preserved" {
		t.Fatalf("legacy row missing: value=%q, error=%v", value, err)
	}
	if _, err = os.Stat(filepath.Join(dir, "asimi.db")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy database still present: %v", err)
	}
}

func TestOpenStorePreservesExistingAsimiDatabase(t *testing.T) {
	dir := t.TempDir()
	current, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = current.Exec("CREATE TABLE current_probe(value TEXT); INSERT INTO current_probe VALUES('current')"); err != nil {
		t.Fatal(err)
	}
	if err = current.Close(); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(dir, "deskpilot.db")
	legacyBytes := []byte("legacy file must remain untouched")
	if err = os.WriteFile(legacyPath, legacyBytes, 0600); err != nil {
		t.Fatal(err)
	}

	reopened, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var value string
	if err = reopened.QueryRow("SELECT value FROM current_probe").Scan(&value); err != nil || value != "current" {
		t.Fatalf("current row missing: value=%q, error=%v", value, err)
	}
	got, err := os.ReadFile(legacyPath)
	if err != nil || string(got) != string(legacyBytes) {
		t.Fatalf("legacy file changed: %v", err)
	}
}

func TestOpenStoreRejectsActiveLegacyProfile(t *testing.T) {
	dir := t.TempDir()
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	currentPath := filepath.Join(dir, "asimi.db")
	legacyPath := filepath.Join(dir, "deskpilot.db")
	if _, err = os.Stat(currentPath); err == nil {
		if err = os.Rename(currentPath, legacyPath); err != nil {
			t.Fatal(err)
		}
	}

	// The production legacy locker is used here to model the older process.
	release, err := acquireProfileFile(filepath.Join(dir, "deskpilot.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	reopened, err := openStore(dir)
	if err == nil {
		reopened.Close()
		t.Fatal("opened a profile while its legacy lock was held")
	}
	if _, err = os.Stat(legacyPath); err != nil {
		t.Fatal(err)
	}
}

func TestOpenStoreMigratesLegacyWAL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deskpilot.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA journal_mode=WAL; CREATE TABLE wal_probe(value TEXT); INSERT INTO wal_probe VALUES('committed')"); err != nil {
		t.Fatal(err)
	}
	beforeCheckpoint, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wal, err := os.ReadFile(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	// Recreate the persisted state left by a process exit before checkpoint.
	if err = os.WriteFile(path, beforeCheckpoint, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+"-wal", wal, 0600); err != nil {
		t.Fatal(err)
	}

	migrated, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	var value string
	if err = migrated.QueryRow("SELECT value FROM wal_probe").Scan(&value); err != nil || value != "committed" {
		t.Fatalf("WAL row missing: value=%q, error=%v", value, err)
	}
}

func TestOpenStoreKeepsCorruptLegacyInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deskpilot.db")
	content := make([]byte, 512)
	copy(content, "damaged legacy database")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := openStore(dir)
	if err == nil {
		db.Close()
		t.Fatal("corrupt legacy database opened")
	}
	current, readErr := os.ReadFile(path)
	if readErr != nil || string(current) != string(content) {
		t.Fatalf("legacy input changed: %v", readErr)
	}
	if _, err = os.Stat(filepath.Join(dir, "asimi.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("current database created after migration failure: %v", err)
	}
}
