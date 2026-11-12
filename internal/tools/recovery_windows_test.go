//go:build windows

package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"asimi/internal/domain"
)

func executeWitness(t *testing.T, r *Registry, op domain.Operation) domain.FileWitness {
	t.Helper()
	var witness domain.FileWitness
	recorded, committed := false, false
	v, err := r.Execute(context.Background(), op, allow, ExecutionHooks{
		Record: func(w domain.FileWitness) error {
			recorded = true
			witness = w
			if _, err := os.Stat(op.NormalizedPath); !os.IsNotExist(err) {
				t.Errorf("final name exists before witness recording: %v", err)
			}
			if _, err := os.Stat(filepath.Join(r.Workspace(), w.TempName)); err != nil {
				t.Errorf("flushed temporary file missing at witness recording: %v", err)
			}
			return nil
		},
		Committed: func() {
			committed = true
			if !recorded {
				t.Error("rename occurred before witness recording")
			}
			if _, err := os.Stat(op.NormalizedPath); err != nil {
				t.Errorf("committed file missing after rename: %v", err)
			}
		},
	})
	if err != nil || !v.Created || !v.Passed || !recorded || !committed || witness == (domain.FileWitness{}) {
		t.Fatalf("witnessed create failed: %+v %v", v, err)
	}
	return witness
}

func TestInspectRequiresOriginalFileIdentityAfterRestart(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "created.txt", "same exact content")
	witness := executeWitness(t, r, op)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(r.Workspace())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	// Inspection reconciles a past operation even after its approval expired.
	op.ExpiresAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if v, err := reopened.Inspect(op, witness); err != nil || !v.Created || !v.Passed {
		t.Fatalf("original file rejected: %+v %v", v, err)
	}
	// Allocate the impostor while the original still exists so Windows cannot
	// recycle its file record or inherit its creation time during allocation.
	impostor := filepath.Join(r.Workspace(), "impostor.txt")
	if err := os.WriteFile(impostor, []byte(op.Content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(op.NormalizedPath); err != nil {
		t.Fatal(err)
	}
	if v, err := reopened.Inspect(op, witness); code(err) != "FILE_NOT_FOUND" || v.Created || v.Passed {
		t.Fatalf("missing file incorrectly recognized: %+v %v", v, err)
	}
	if err := os.Rename(impostor, op.NormalizedPath); err != nil {
		t.Fatal(err)
	}
	if v, err := reopened.Inspect(op, witness); code(err) != "WITNESS_MISMATCH" || v.Created || v.Passed {
		t.Fatalf("same-name same-content impostor accepted: %+v %v", v, err)
	}
}

func TestInspectRejectsTamperingAndReparseTarget(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "checked.txt", "approved content")
	witness := executeWitness(t, r, op)
	changed := op
	changed.NormalizedPath = filepath.Join(filepath.Dir(r.Workspace()), "checked.txt")
	if v, err := r.Inspect(changed, witness); code(err) != "INVALID_OPERATION" || v.Created {
		t.Fatalf("tampered path accepted: %+v %v", v, err)
	}
	changed = op
	changed.ContentHash = "tampered"
	if v, err := r.Inspect(changed, witness); code(err) != "INVALID_OPERATION" || v.Created {
		t.Fatalf("tampered hash accepted: %+v %v", v, err)
	}
	if v, err := r.Inspect(op, domain.FileWitness{}); code(err) != "WITNESS_MISMATCH" || v.Created {
		t.Fatalf("empty identity accepted: %+v %v", v, err)
	}
	alias := filepath.Join(r.Workspace(), "alias.txt")
	if err := os.Link(op.NormalizedPath, alias); err != nil {
		t.Fatal(err)
	}
	if v, err := r.Inspect(op, witness); code(err) != "WITNESS_MISMATCH" || v.Created {
		t.Fatalf("ambiguous file with multiple links accepted: %+v %v", v, err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(op.NormalizedPath); err != nil {
		t.Fatal(err)
	}
	makeJunction(t, op.NormalizedPath, t.TempDir())
	if v, err := r.Inspect(op, witness); code(err) != "WITNESS_MISMATCH" || v.Created || v.Passed {
		t.Fatalf("reparse target accepted: %+v %v", v, err)
	}
}

func TestWitnessFailureOrCancellationPreventsRename(t *testing.T) {
	for _, want := range []string{"STORAGE_WRITE_FAILED", "CANCELLED"} {
		t.Run(want, func(t *testing.T) {
			r := testRegistry(t)
			op := prepare(t, r, "blocked.txt", "no commit")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			committed := false
			v, err := r.Execute(ctx, op, allow, ExecutionHooks{
				Record: func(domain.FileWitness) error {
					if want == "CANCELLED" {
						cancel()
						return nil
					}
					return toolError(want)
				},
				Committed: func() { committed = true },
			})
			if code(err) != want || v.Created || committed {
				t.Fatalf("unexpected outcome: %+v %v committed=%v", v, err, committed)
			}
			entries, err := os.ReadDir(r.Workspace())
			if err != nil || len(entries) != 0 {
				t.Fatalf("workspace contains an uncommitted file: %v %v", entries, err)
			}
		})
	}
}

func TestConsumeStorageFailurePreventsFileEffects(t *testing.T) {
	for _, want := range []string{"STORAGE_BUSY", "STORAGE_CONSTRAINT"} {
		t.Run(want, func(t *testing.T) {
			r := testRegistry(t)
			op := prepare(t, r, "blocked.txt", "no commit")
			v, err := r.Execute(context.Background(), op, func(context.Context, domain.Operation) error {
				return toolError(want)
			})
			if code(err) != want || v.Code != want || v.Created || v.Passed {
				t.Errorf("unexpected outcome: %+v %v", v, err)
			}
			entries, err := os.ReadDir(r.Workspace())
			if err != nil || len(entries) != 0 {
				t.Fatalf("approval transaction failure left files: %v %v", entries, err)
			}
		})
	}
}

func TestWitnessAcceptsCurrentAndLegacyTemporaryNames(t *testing.T) {
	witness := domain.FileWitness{VolumeSerial: 1, FileID: [16]byte{1}, CreationTime: 1}
	for _, prefix := range []string{".asimi-", ".deskpilot-"} {
		witness.TempName = prefix + "0123456789abcdef0123456789abcdef.tmp"
		if !validWitness(witness) {
			t.Fatalf("valid temporary name rejected: %s", witness.TempName)
		}
		witness.TempName = prefix + "../0123456789abcdef0123456789abcdef.tmp"
		if validWitness(witness) {
			t.Fatalf("unsafe temporary name accepted: %s", witness.TempName)
		}
	}
}
