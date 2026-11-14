package app

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHarnessRepairEvidenceTracksCurrentWorkingCopy(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("Go required for local test execution")
	}
	for _, scenario := range []struct {
		name      string
		test      bool
		stale     bool
		retest    bool
		noTests   bool
		unrelated bool
		want      string
	}{
		{name: "write only", want: "NEEDS_REVIEW"},
		{name: "write and test", test: true, want: "COMPLETED"},
		{name: "go exits zero without executing a test", test: true, noTests: true, want: "NEEDS_REVIEW"},
		{name: "only unrelated Go tests pass", test: true, noTests: true, unrelated: true, want: "NEEDS_REVIEW"},
		{name: "write and test then external edit", test: true, stale: true, want: "NEEDS_REVIEW"},
		{name: "a subsequent failed test supersedes a pass", test: true, retest: true, want: "NEEDS_REVIEW"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			const initial = "package calc\nfunc Add(a,b int) int {return a-b}\n"
			const fixed = "package calc\nfunc Add(a,b int) int {return a+b}\n"
			patched := fixed
			var rounds int
			s, workspace, task := agent02TestRun(t, func(w http.ResponseWriter, _ *http.Request) {
				rounds++
				step := agent02PlanStep{Type: "FINALIZE", Summary: "Fix and tests complete"}
				if rounds == 1 {
					step = agent02PlanStep{Type: "PROPOSE_FILE_CHANGE", Path: "calc.go",
						BaseSHA256: hashControlText(initial), ProposedContent: &patched}
				} else if (rounds == 2 && scenario.test) || (rounds == 3 && scenario.retest) {
					step = agent02PlanStep{Type: "PROPOSE_COMMAND", CommandID: "go.test"}
				}
				w.Header().Set("Content-Type", "application/json")
				plan := agent02Plan{Objective: "Fix Add", Steps: []agent02PlanStep{step}}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "harness-evidence", "choices": []any{map[string]any{
					"message": map[string]string{"content": encode(plan)}, "finish_reason": "stop",
				}}})
			})
			for name, content := range map[string]string{
				"go.mod":       "module fixture\n\ngo 1.26\n",
				"calc_test.go": "package calc\nimport \"testing\"\nfunc TestAdd(t *testing.T) {if Add(2,3)!=5{t.Fatal(\"wrong result\")}}\n",
			} {
				if err := os.WriteFile(filepath.Join(workspace.Root, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.noTests {
				if err := os.Remove(filepath.Join(workspace.Root, "calc_test.go")); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.unrelated {
				other := filepath.Join(workspace.Root, "other")
				if err := os.Mkdir(other, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(other, "other_test.go"), []byte("package other\nimport \"testing\"\nfunc TestOther(t *testing.T) {}\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			approve := func(want string) {
				t.Helper()
				task = harnessRunRound(t, s, task)
				if task.Status != "WAITING_APPROVAL" {
					t.Fatalf("no approval in %s (%s)", task.Status, task.Summary)
				}
				action, err := loadProductAction(s.db, task.Steps[task.CurrentStep].ActionID)
				if err != nil {
					t.Fatal(err)
				}
				finished := mustCall(t, s, "ApproveProductAction", map[string]any{"id": action.ID, "hash": action.Hash, "acceptNetwork": true}).(ProductAction)
				if finished.Status != want {
					t.Fatalf("action %s: %s %s", action.Kind, finished.Status, finished.Error)
				}
			}
			approve("SUCCEEDED")
			file := filepath.Join(workspace.Root, "calc.go")
			if content, err := os.ReadFile(file); err != nil || string(content) != fixed {
				t.Fatalf("change not independently visible: %v, %q", err, content)
			}
			if scenario.test {
				approve("SUCCEEDED")
			}
			if scenario.retest {
				failing := "package calc\nimport \"testing\"\nfunc TestAdd(t *testing.T) {t.Fatal(\"second run failed\")}\n"
				if err := os.WriteFile(filepath.Join(workspace.Root, "calc_test.go"), []byte(failing), 0600); err != nil {
					t.Fatal(err)
				}
				approve("FAILED")
			}
			if scenario.stale {
				if err := os.WriteFile(file, []byte(initial), 0600); err != nil {
					t.Fatal(err)
				}
			}
			task = harnessRunRound(t, s, task)
			if task.Status != scenario.want {
				t.Fatalf("completion state %s (%s), want %s", task.Status, task.Summary, scenario.want)
			}
		})
	}
}
