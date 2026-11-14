package app

import (
	"database/sql"
	"strings"
	"unicode/utf8"

	"asimi/internal/computer"
)

// Computer actions use a separate journal so desktop effects cannot acquire
// the Phase 1 file capability or reuse a file approval.
const schemaV4 = `
CREATE TABLE computer_actions (
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL,
 status TEXT NOT NULL,
 action_hash TEXT NOT NULL,
 target_hash TEXT NOT NULL,
 text_hash TEXT NOT NULL DEFAULT '',
 text_chars INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 code TEXT NOT NULL DEFAULT '',
 verified INTEGER NOT NULL DEFAULT 0 CHECK(verified IN (0,1))
);
CREATE TABLE computer_approvals (
 id TEXT PRIMARY KEY,
 action_id TEXT NOT NULL UNIQUE REFERENCES computer_actions(id),
 action_hash TEXT NOT NULL,
 target_hash TEXT NOT NULL,
 approved_at TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('APPROVED','CONSUMED','INVALIDATED')),
 consumed_at TEXT
);
CREATE TABLE computer_action_events (
 id INTEGER PRIMARY KEY,
 action_id TEXT NOT NULL REFERENCES computer_actions(id),
 status TEXT NOT NULL,
 code TEXT NOT NULL DEFAULT '',
 at TEXT NOT NULL
);
PRAGMA user_version=4;`

func controlEvent(tx *sql.Tx, id, status, code, at string) error {
	_, e := tx.Exec("INSERT INTO computer_action_events(action_id,status,code,at) VALUES(?,?,?,?)", id, status, code, at)
	return e
}

func controlChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return codedError("CONTROL_APPROVAL_USED")
	}
	return nil
}

func saveControlProposal(db *sql.DB, p computer.Proposal) error {
	return saveControlProposalSource(db, p, nil)
}

func saveControlProposalSource(db *sql.DB, p computer.Proposal, source *computerSuggestionRow, tasks ...*AgentTask) error {
	textHash := ""
	if p.Text != "" {
		textHash = hashControlText(p.Text)
	}
	return storeTransaction(db, func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO computer_actions(id,kind,status,action_hash,target_hash,text_hash,text_chars,created_at,updated_at,expires_at)
VALUES(?,?,?,?,?,?,?,?,?,?)`, p.ID, p.Type, p.Status, p.Hash, p.TargetHash, textHash, utf8.RuneCountInString(p.Text), p.ObservedAt, p.ObservedAt, p.ExpiresAt)
		if e != nil {
			return e
		}
		for _, status := range []string{"PROPOSED", "VALIDATING", "AWAITING_APPROVAL"} {
			if e = controlEvent(tx, p.ID, status, "", p.ObservedAt); e != nil {
				return e
			}
		}
		if source != nil {
			if e = controlChanged(tx.Exec(`UPDATE ai_message_metadata SET json=json_set(json,'$.computerSuggestion.actionId',?)
WHERE message_id=? AND json_type(json,'$.computerSuggestion')='object'
 AND json_extract(json,'$.computerSuggestion.observationId')=?
 AND json_extract(json,'$.computerSuggestion.targetHash')=?
 AND json_extract(json,'$.computerSuggestion.actionId') IS NULL
 AND EXISTS(SELECT 1 FROM messages WHERE id=? AND content=?)`,
				p.ID, source.messageID, source.scope.ObservationID, source.scope.TargetHash, source.messageID, source.content)); e != nil {
				return e
			}
		}
		if len(tasks) > 0 {
			if err := startDesktopTask(tx, p, tasks[0]); err != nil {
				return err
			}
		}
		return linkComputerTask(tx, p.TaskID, p.ID, "")
	})
}

func controlTransition(tx *sql.Tx, record computer.Record, before string) error {
	if e := controlChanged(tx.Exec(`UPDATE computer_actions SET status=?,code=?,updated_at=?,verified=?
WHERE id=? AND status=?`, record.Status, record.Code, record.At, record.Verified, record.ID, before)); e != nil {
		return e
	}
	if err := controlEvent(tx, record.ID, record.Status, record.Code, record.At); err != nil {
		return err
	}
	return updateDesktopTask(tx, record)
}

func saveControlApproval(db *sql.DB, p computer.Proposal, stamp string) error {
	return storeTransaction(db, func(tx *sql.Tx) error {
		if err := validateDesktopTaskApproval(tx, p, "WAITING_APPROVAL"); err != nil {
			return err
		}
		r := computer.Record{ID: p.ID, Type: p.Type, Status: "APPROVED", At: stamp}
		if e := controlTransition(tx, r, "AWAITING_APPROVAL"); e != nil {
			return e
		}
		if _, e := tx.Exec(`INSERT INTO computer_approvals(id,action_id,action_hash,target_hash,approved_at,expires_at,status)
VALUES(?,?,?,?,?,?,'APPROVED')`, newID(), p.ID, p.Hash, p.TargetHash, stamp, p.ExpiresAt); e != nil {
			return e
		}
		r.Status = "REVALIDATING"
		return controlTransition(tx, r, "APPROVED")
	})
}

func consumeControlApproval(db *sql.DB, p computer.Proposal, stamp string) error {
	return storeTransaction(db, func(tx *sql.Tx) error {
		if err := validateDesktopTaskApproval(tx, p, "RUNNING"); err != nil {
			return err
		}
		if e := controlChanged(tx.Exec(`UPDATE computer_approvals SET status='CONSUMED',consumed_at=?
WHERE action_id=? AND action_hash=? AND target_hash=? AND status='APPROVED' AND consumed_at IS NULL AND expires_at>?`,
			stamp, p.ID, p.Hash, p.TargetHash, stamp)); e != nil {
			return e
		}
		return controlTransition(tx, computer.Record{ID: p.ID, Type: p.Type, Status: "EXECUTING", At: stamp}, "REVALIDATING")
	})
}

func finishControl(db *sql.DB, r computer.Record, before string) error {
	return storeTransaction(db, func(tx *sql.Tx) error {
		if _, e := tx.Exec("UPDATE computer_approvals SET status='INVALIDATED' WHERE action_id=? AND status='APPROVED'", r.ID); e != nil {
			return e
		}
		return controlTransition(tx, r, before)
	})
}

func recoverComputer(db *sql.DB) error {
	return storeTransaction(db, func(tx *sql.Tx) error {
		stamp := now()
		for _, state := range []struct{ before, after, code string }{
			{"AWAITING_APPROVAL", "CANCELLED", "CONTROL_RESTARTED"},
			{"APPROVED", "CANCELLED", "CONTROL_RESTARTED"},
			{"REVALIDATING", "CANCELLED", "CONTROL_RESTARTED"},
			{"EXECUTING", "RECOVERY_REQUIRED", "CONTROL_UNKNOWN_RESULT"},
			{"VERIFYING", "RECOVERY_REQUIRED", "CONTROL_UNKNOWN_RESULT"},
		} {
			if _, e := tx.Exec(`INSERT INTO computer_action_events(action_id,status,code,at)
SELECT id,?,?,? FROM computer_actions WHERE status=?`, state.after, state.code, stamp, state.before); e != nil {
				return e
			}
			if _, e := tx.Exec("UPDATE computer_actions SET status=?,code=?,updated_at=? WHERE status=?", state.after, state.code, stamp, state.before); e != nil {
				return e
			}
		}
		_, e := tx.Exec("UPDATE computer_approvals SET status='INVALIDATED' WHERE status='APPROVED'")
		return e
	})
}

func listControlRecords(db *sql.DB) ([]computer.Record, error) {
	rows, err := db.Query(`SELECT a.id,a.kind,e.status,e.code,e.at,
CASE WHEN e.status='COMPLETED' THEN a.verified ELSE 0 END
FROM computer_action_events e JOIN computer_actions a ON a.id=e.action_id
ORDER BY e.id DESC LIMIT 150`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []computer.Record{}
	for rows.Next() {
		var r computer.Record
		if err := rows.Scan(&r.ID, &r.Type, &r.Status, &r.Code, &r.At, &r.Verified); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func driverCode(err error, fallback string) string {
	code := errorCode(err)
	if strings.HasPrefix(code, "CONTROL_") {
		return code
	}
	return fallback
}
