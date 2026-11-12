//go:build windows

package developer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string { t.Helper(); return t.TempDir() }
func put(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// Omitting child-name validation would permit workspace escape and NTFS aliases.
func TestReadRejectsUnsafeNamesAndNonText(t *testing.T) {
	root := fixture(t)
	put(t, root, "ok.go", "package main\n")
	f, err := Read(root, "ok.go")
	if err != nil || f.Content != "package main\n" || f.Language != "Go" || len(f.Hash) != 64 {
		t.Fatalf("read: %#v %v", f, err)
	}
	for _, name := range []string{"../outside", "a/../ok.go", filepath.Join(root, "ok.go"), `C:ok.go`, `ok.go:stream`, `NUL.txt`, `a./x`, `.git/config`, "a\\..\\ok.go"} {
		if _, err := Read(root, name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	put(t, root, "binary", "\x00\xff")
	if _, err := Read(root, "binary"); err == nil {
		t.Fatal("accepted binary")
	}
	put(t, root, "large", strings.Repeat("a", MaxFileBytes+1))
	if _, err := Read(root, "large"); err == nil {
		t.Fatal("accepted over-limit file")
	}
}

// Losing bounded scan or exclusion enforcement would leak excluded project files.
func TestAnalyzeBoundedSearchLanguagesDependenciesAndSymbols(t *testing.T) {
	root := fixture(t)
	put(t, root, "main.go", "package main\nfunc main() { helper() }\nfunc helper() {}\n")
	put(t, root, "go.mod", "module fixture\n\ngo 1.26\nrequire example.org/dependency v1.2.3\n")
	put(t, root, "package.json", `{"dependencies":{"vue":"3.5.0"},"scripts":{"test":"secret shell"}}`)
	put(t, root, "node_modules/hidden.go", "hidden helper")
	put(t, root, "private/hidden.go", "hidden helper")
	report, err := AnalyzeWithOptions(root, "helper", Options{Exclusions: []string{"private"}, MaxFiles: 20, MaxDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Matches) != 2 || len(report.Dependencies) != 2 || len(report.Symbols) < 2 || len(report.Calls) < 1 {
		t.Fatalf("incomplete report: %#v", report)
	}
	for _, entry := range report.Entries {
		if strings.Contains(entry.Path, "hidden") {
			t.Fatalf("excluded: %#v", entry)
		}
	}
	limited, err := AnalyzeWithOptions(root, "", Options{MaxFiles: 1, MaxDepth: 1})
	if err != nil || !limited.Truncated || len(limited.Entries) > 1 {
		t.Fatalf("scan budget: %#v %v", limited, err)
	}
}

// If source hashes or identity are ignored, an approved proposal overwrites newer work.
func TestPrepareExactDiffAndRejectStaleTargets(t *testing.T) {
	root := fixture(t)
	put(t, root, "main.go", "old\n")
	c, err := Prepare(root, "main.go", "new\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Operation != "replace" || c.BeforeContent != "old\n" || c.AfterContent != "new\n" || !strings.Contains(c.Diff, "-old\n") || !strings.Contains(c.Diff, "+new\n") {
		t.Fatalf("proposal: %#v", c)
	}
	put(t, root, "main.go", "changed\n")
	if _, err := Apply(context.Background(), root, c); err == nil {
		t.Fatal("stale content applied")
	}
	actual, _ := os.ReadFile(filepath.Join(root, "main.go"))
	if string(actual) != "changed\n" {
		t.Fatal("stale proposal modified target")
	}
	put(t, root, "main.go", "old\n")
	c, err = Prepare(root, "main.go", "new\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "main.go"), filepath.Join(root, "old.go")); err != nil {
		t.Fatal(err)
	}
	put(t, root, "main.go", "old\n")
	if _, err := Apply(context.Background(), root, c); err == nil {
		t.Fatal("same-content replacement applied")
	}
}

// Atomic no-replace creation must never overwrite a file created after approval.
func TestCreateVerifiedAndConflictAndCancellation(t *testing.T) {
	root := fixture(t)
	c, err := Prepare(root, "new.txt", "hello\n")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), root, c)
	if err != nil || !result.Applied || !result.Verified || result.AfterHash != c.AfterHash {
		t.Fatalf("apply: %#v %v", result, err)
	}
	actual, _ := os.ReadFile(filepath.Join(root, "new.txt"))
	if string(actual) != "hello\n" {
		t.Fatal("incorrect created bytes")
	}
	if _, err := Apply(context.Background(), root, c); err == nil {
		t.Fatal("replayed create")
	}
	c, err = Prepare(root, "conflict.txt", "new")
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "conflict.txt", "other")
	if _, err := Apply(context.Background(), root, c); err == nil {
		t.Fatal("overwrote create conflict")
	}
	c, err = Prepare(root, "cancelled.txt", "new")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Apply(ctx, root, c); err == nil {
		t.Fatal("cancelled operation applied")
	}
	if _, err := os.Stat(filepath.Join(root, "cancelled.txt")); !os.IsNotExist(err) {
		t.Fatal("cancelled operation wrote file")
	}
}

// A successful transaction must preserve the original identity and recovery bytes.
func TestReplacePreservesRecoveryAndTouchesOnlyApprovedTarget(t *testing.T) {
	root := fixture(t)
	put(t, root, "src/main.go", "old\r\n你好")
	put(t, root, "src/other.go", "untouched")
	c, err := Prepare(root, "src/main.go", "new\n你好\n")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), root, c)
	if err != nil || !result.Applied || !result.Verified || !result.Atomic || result.Identity != c.BeforeIdentity {
		t.Fatalf("replace: %#v %v", result, err)
	}
	actual, _ := os.ReadFile(filepath.Join(root, "src/main.go"))
	if string(actual) != "new\n你好\n" {
		t.Fatalf("target bytes %q", actual)
	}
	backup, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(result.RecoveryPath)))
	if string(backup) != "old\r\n你好" {
		t.Fatalf("recovery bytes %q", backup)
	}
	other, _ := os.ReadFile(filepath.Join(root, "src/other.go"))
	if string(other) != "untouched" {
		t.Fatal("unapproved target modified")
	}
	entries, _ := os.ReadDir(filepath.Join(root, "src"))
	if len(entries) != 3 {
		t.Fatalf("unexpected temp leakage: %d", len(entries))
	}
}

// Omitting directory identity checks could execute an old approval in a swapped
// directory containing an otherwise identical target.
func TestRejectReplacedParentAndWorkspace(t *testing.T) {
	root := fixture(t)
	put(t, root, "src/main.go", "old")
	c, err := Prepare(root, "src/main.go", "new")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "src"), filepath.Join(root, "old-src")); err != nil {
		t.Fatal(err)
	}
	put(t, root, "src/main.go", "old")
	if _, err := Apply(context.Background(), root, c); err == nil {
		t.Fatal("replaced parent accepted")
	}
	another := fixture(t)
	put(t, another, "src/main.go", "old")
	if _, err := Apply(context.Background(), another, c); err == nil {
		t.Fatal("another workspace accepted")
	}
}

// Exclusions must constrain direct reads and changes as well as the tree scan.
func TestUserExclusionsAndInvalidDiffRejectBeforeWriting(t *testing.T) {
	root := fixture(t)
	put(t, root, "private/config.txt", "secret")
	options := Options{Exclusions: []string{"private"}}
	if _, err := ReadWithOptions(root, "private/config.txt", options); err == nil {
		t.Fatal("read ignored exclusion")
	}
	if _, err := PrepareWithOptions(root, "private/config.txt", "overwrite", options); err == nil {
		t.Fatal("prepare ignored exclusion")
	}
	c, err := Prepare(root, "new.txt", "hello")
	if err != nil {
		t.Fatal(err)
	}
	c.Diff = "pretend exact diff"
	if _, err := Apply(context.Background(), root, c); err == nil {
		t.Fatal("changed diff accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("invalid proposal wrote")
	}
}

// Invalid text consumes the same I/O budget as source text. Otherwise many
// binary files can silently multiply the configured scan-byte limit.
func TestAnalysisCountsBinaryReadBytesAgainstBudget(t *testing.T) {
	root := fixture(t)
	put(t, root, "a.bin", strings.Repeat("\x00", 300))
	put(t, root, "b.bin", strings.Repeat("\x00", 300))
	put(t, root, "z.txt", "needle")
	report, err := AnalyzeWithOptions(root, "needle", Options{MaxBytes: 300})
	if err != nil {
		t.Fatal(err)
	}
	if report.BytesRead != 300 || len(report.Matches) != 0 || !report.Truncated {
		t.Fatalf("binary budget ignored: %#v", report)
	}
}

type cancelWhenCreated struct {
	context.Context
	target string
}

func (c cancelWhenCreated) Err() error {
	if _, err := os.Stat(c.target); err == nil {
		return context.Canceled
	}
	return nil
}

// Cancellation after the rename must report an unknown result and preserve the
// committed target rather than rolling back or making its approval replayable.
func TestCancellationAfterCommitPreservesTarget(t *testing.T) {
	root := fixture(t)
	c, err := Prepare(root, "committed.txt", "preserve")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(cancelWhenCreated{Context: context.Background(), target: filepath.Join(root, "committed.txt")}, root, c)
	if err != Error("UNKNOWN_RESULT") || !result.Applied || result.Verified {
		t.Fatalf("postcommit state: %#v %v", result, err)
	}
	actual, _ := os.ReadFile(filepath.Join(root, "committed.txt"))
	if string(actual) != "preserve" {
		t.Fatal("committed target removed")
	}
}

func TestRootIdentityDetectsReplacedDirectory(t *testing.T) {
	base := fixture(t)
	root := filepath.Join(base, "workspace")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	first, err := RootIdentity(root)
	if err != nil || first == "" {
		t.Fatalf("identity %q %v", first, err)
	}
	if err := os.Rename(root, filepath.Join(base, "old-workspace")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	second, err := RootIdentity(root)
	if err != nil || first == second {
		t.Fatalf("identity did not change: %q %q %v", first, second, err)
	}
}
