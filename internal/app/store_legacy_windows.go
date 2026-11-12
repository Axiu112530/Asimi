package app

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// resolveStorePath upgrades a closed legacy database in the selected profile.
// The caller holds the current profile lock. The legacy lock prevents an older
// process from writing while SQLite checkpoints and the file is renamed.
func resolveStorePath(dir string) (path string, err error) {
	path = filepath.Join(dir, "asimi.db")
	if _, err = os.Lstat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	legacyPath := filepath.Join(dir, "deskpilot.db")
	info, err := os.Lstat(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() < 100 {
		return "", codedError("STORAGE_CORRUPT")
	}
	release, err := acquireProfileFile(filepath.Join(dir, "deskpilot.lock"))
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, release()) }()

	db, err := sql.Open("sqlite", legacyPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return "", err
	}
	var integrity string
	err = db.QueryRow("PRAGMA quick_check").Scan(&integrity)
	if err == nil && integrity != "ok" {
		err = codedError("STORAGE_CORRUPT")
	}
	var busy, pages, checkpointed int
	if err == nil {
		err = db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &checkpointed)
	}
	if err == nil && busy != 0 {
		err = codedError("STORAGE_BUSY")
	}
	if err = errors.Join(err, db.Close()); err != nil {
		return "", err
	}
	source, err := windows.UTF16PtrFromString(legacyPath)
	if err != nil {
		return "", err
	}
	target, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	// MoveFile fails if the destination already exists, preserving both files
	// when another creator wins the race.
	if err = windows.MoveFile(source, target); err != nil {
		return "", err
	}
	return path, nil
}
