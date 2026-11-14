package app

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestHarnessCommandDeadline(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node required for the local command fixture")
	}
	for _, expired := range []bool{false, true} {
		name := "running"
		if expired {
			name = "before approval"
		}
		t.Run(name, func(t *testing.T) {
			s, workspace, task := harnessTestPlan(t, "Run the tests",
				agent02PlanStep{Type: "PROPOSE_COMMAND", CommandID: "node.test", Args: []string{"slow.test.cjs"}})
			test := "require('node:test')('slow fixture', async () => {await new Promise(r => setTimeout(r, 2500))})\n"
			if err := os.WriteFile(filepath.Join(workspace.Root, "slow.test.cjs"), []byte(test), 0600); err != nil {
				t.Fatal(err)
			}
			task = harnessRunRound(t, s, task)
			if task.Status != "WAITING_APPROVAL" {
				t.Fatalf("no command approval: %s %s", task.Status, task.Summary)
			}
			action, err := loadProductAction(s.db, task.Agent.Rounds[0].ActionID)
			if err != nil {
				t.Fatal(err)
			}
			task.Agent.Deadline = time.Now().Add(800 * time.Millisecond)
			if expired {
				task.Agent.Deadline = time.Now().Add(-time.Second)
			}
			if err := s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, task) }); err != nil {
				t.Fatal(err)
			}
			args := map[string]any{"id": action.ID, "hash": action.Hash, "acceptNetwork": true}
			if expired {
				reply := s.Call("ApproveProductAction", encode(args))
				if reply.Error == nil || reply.Error.Code != "AGENT_DEADLINE_EXCEEDED" {
					t.Fatalf("expired task approval was admitted: %+v", reply)
				}
				var consumed int
				if err := s.db.QueryRow("SELECT consumed FROM product_actions WHERE id=?", action.ID).Scan(&consumed); err != nil || consumed != 0 {
					t.Fatalf("expired approval consumed=%d, err=%v", consumed, err)
				}
			} else {
				finished := mustCall(t, s, "ApproveProductAction", args).(ProductAction)
				var result struct{ TimedOut bool }
				if json.Unmarshal(finished.Result, &result) != nil || !result.TimedOut || finished.Status == "SUCCEEDED" {
					t.Fatalf("command crossed task deadline: %s %s", finished.Status, finished.Result)
				}
			}
			current := mustCall(t, s, "GetAgentTask", map[string]string{"id": task.ID}).(AgentTask)
			if current.Status != "FAILED" || current.Summary != "AGENT_DEADLINE_EXCEEDED" {
				t.Fatalf("expired task remained active: %s %s", current.Status, current.Summary)
			}
		})
	}
}
