// lifecycledb reads the durable state left by the pending-approval close probe.
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func inspect(path, taskID, approvalID string) error {
	uri := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(path), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return err
	}
	defer db.Close()
	var taskState, approvalState, integrity string
	var executions int
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if err := db.QueryRow("SELECT status FROM task_sessions WHERE id=?", taskID).Scan(&taskState); err != nil {
		return err
	}
	if err := db.QueryRow("SELECT status FROM approval_records WHERE id=? AND task_id=?", approvalID, taskID).Scan(&approvalState); err != nil {
		return err
	}
	if err := db.QueryRow("SELECT count(*) FROM tool_executions WHERE task_id=?", taskID).Scan(&executions); err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"taskId": taskID, "approvalId": approvalID, "taskStatus": taskState, "approvalStatus": approvalState,
		"toolExecutions": executions, "integrity": integrity,
	}); err != nil {
		return err
	}
	if taskState != "CANCELLED" || approvalState != "INVALIDATED" || executions != 0 || integrity != "ok" {
		return errors.New("unexpected durable state after shutdown")
	}
	return nil
}

func main() {
	path := flag.String("db", "", "path to the fixture's SQLite database")
	task := flag.String("task", "", "pending task ID")
	approval := flag.String("approval", "", "pending approval ID")
	flag.Parse()
	if !filepath.IsAbs(*path) || *task == "" || *approval == "" {
		fmt.Fprintln(os.Stderr, "absolute database path, task ID, and approval ID are required")
		os.Exit(1)
	}
	if err := inspect(*path, *task, *approval); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
