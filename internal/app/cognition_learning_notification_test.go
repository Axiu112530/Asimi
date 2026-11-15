package app

import (
	"context"
	"database/sql"
	"testing"
)

func TestCognitionLearningNotifiesOnlyCommittedJobChanges(t *testing.T) {
	for _, scenario := range []string{"completed", "recorded failure", "unrecorded failure", "idle", "closed"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			db, err := openStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(func() {
				cancel()
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			s := &Service{db: db, lifetime: ctx}
			if err := s.initializeMVP(); err != nil {
				t.Fatal(err)
			}
			stamp := now()
			if _, err := db.Exec("INSERT INTO app_settings(id,json,created_at,updated_at) VALUES('app',?,?,?)", `{"appLanguage":"en-US","agentLanguage":"follow-app","theme":"light","startup":"welcome"}`, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			workspace := Workspace{ID: newID(), Root: dir, Name: "Learning notification", CreatedAt: stamp, LastUsedAt: stamp}
			if _, err := db.Exec("INSERT INTO workspaces(id,root,name,created_at,last_used_at,trust_status) VALUES(?,?,?,?,?,'USER_SELECTED')", workspace.ID, dir, workspace.Name, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO cognition_settings VALUES(1,'{"memory":true,"automaticLearning":true,"projectLearning":true}')`); err != nil {
				t.Fatal(err)
			}
			task := AgentTask{ID: newID(), WorkspaceID: workspace.ID, Goal: "Record a completed local observation", Status: "COMPLETED", CreatedAt: stamp, UpdatedAt: stamp}
			if scenario != "idle" {
				if err := s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, task) }); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "recorded failure" || scenario == "unrecorded failure" {
				if _, err := db.Exec(`CREATE TRIGGER learning_notification_fault BEFORE UPDATE OF status ON learning_jobs WHEN NEW.status='done' BEGIN SELECT RAISE(ABORT,'injected learning failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "unrecorded failure" {
				if _, err := db.Exec(`CREATE TRIGGER learning_notification_record_fault BEFORE UPDATE OF attempts ON cognition_learning_history BEGIN SELECT RAISE(ABORT,'injected failure-record write failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			events := 0
			s.emit = func(name string, payload any) {
				if name != "asimi:changed" {
					return
				}
				events++
				if !s.cognitionMu.TryLock() {
					t.Error("notification held cognition lock")
				} else {
					s.cognitionMu.Unlock()
				}
				if !s.mvp.signalMu.TryLock() {
					t.Error("notification held stop-admission lock")
				} else {
					s.mvp.signalMu.Unlock()
				}
				var event struct {
					TaskID         string `json:"taskId"`
					ConversationID string `json:"conversationId"`
				}
				if err := decode(encode(payload), &event); err != nil || event.TaskID != task.ID {
					t.Errorf("notification lacks changed task: %s", encode(payload))
				}
				state, err := s.getCognitionState(workspace.ID)
				if err != nil {
					t.Error(err)
					return
				}
				if len(state.Learning) != 1 || state.Learning[0].Attempts != 1 {
					t.Errorf("callback cannot read committed learning state: %+v", state.Learning)
					return
				}
				if scenario == "completed" {
					if state.Completed != 1 || state.Learning[0].Status != "done" || len(state.Memories) != 1 {
						t.Error("completion event preceded visible data commit")
					}
				} else if scenario == "recorded failure" {
					if state.Completed != 0 || state.Learning[0].Status != "pending" || state.Learning[0].Error == "" || len(state.Memories) != 0 {
						t.Error("failure event preceded visible rollback and failure record")
					}
				} else {
					t.Error("non-persisted or idle transition emitted notification")
				}
			}
			if scenario == "closed" {
				cancel()
			}
			s.processCognitionJob()
			want := 0
			if scenario == "completed" || scenario == "recorded failure" {
				want = 1
			}
			if events != want {
				t.Fatalf("notifications=%d want=%d", events, want)
			}
			if scenario != "unrecorded failure" {
				s.processCognitionJob()
				if events != want {
					t.Fatal("idle tick emitted a duplicate notification")
				}
			}
		})
	}
}
