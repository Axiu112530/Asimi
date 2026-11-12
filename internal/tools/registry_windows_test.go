//go:build windows

package tools

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"deskpilot/internal/domain"
	"golang.org/x/sys/windows"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := New(filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func prepare(t *testing.T, r *Registry, name, content string) domain.Operation {
	t.Helper()
	op, err := r.Prepare("task", name, content, "approval-"+name, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func code(err error) string {
	var c interface{ Code() string }
	if errors.As(err, &c) {
		return c.Code()
	}
	return ""
}

func allow(context.Context, domain.Operation) error { return nil }

func TestSingleBasenameAndSizeBoundary(t *testing.T) {
	r := testRegistry(t)
	for _, name := range []string{"", ".", "..", "../outside.txt", `..\outside.txt`, `C:\outside.txt`, `C:outside`, `\\server\share\x`, "x/y", `x\y`, "x:ads", "CON", "nul.txt", "COM1.log", "LPT9", "COM¹.txt", "x.", "x ", "x\n.txt", "x?.txt", `x".txt`, "x*.txt", "x|.txt", "CONIN$", "CONOUT$"} {
		if _, err := r.Prepare("task", name, "hello", "approval", time.Now().Add(time.Minute)); err == nil {
			t.Errorf("accepted unsafe basename %q", name)
		}
	}
	if _, err := r.Prepare("task", "too-big.txt", strings.Repeat("你", 21846), "big", time.Now().Add(time.Minute)); code(err) != "CONTENT_TOO_LARGE" {
		t.Fatalf("size boundary: %v", err)
	}
	if _, err := r.Prepare("task", "invalid.txt", string([]byte{255}), "bad", time.Now().Add(time.Minute)); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
	if _, err := r.Prepare("task", "limit.txt", strings.Repeat("a", 65536), "limit", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestApprovalRequiredDeniedAndCancelled(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "test.txt", "你好\x00world")
	if v, err := r.Execute(context.Background(), op, nil); code(err) != "APPROVAL_REQUIRED" || v.Created {
		t.Fatalf("missing approval: %+v %v", v, err)
	}
	if _, err := r.Execute(context.Background(), op, func(context.Context, domain.Operation) error { return errors.New("rejected") }); code(err) != "APPROVAL_DENIED" {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if v, err := r.Execute(ctx, op, func(context.Context, domain.Operation) error { called = true; return nil }); code(err) != "CANCELLED" || v.Created || called {
		t.Fatalf("cancel: %+v %v called=%v", v, err, called)
	}
	if v, err := r.Execute(context.Background(), op, func(context.Context, domain.Operation) error { cancel(); return nil }); err != nil || !v.Passed {
		t.Fatalf("create: %+v %v", v, err)
	}
}

func TestExactApprovalBinding(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "bound.txt", "hello")
	mutations := []func(*domain.Operation){
		func(o *domain.Operation) { o.TaskID += "changed" }, func(o *domain.Operation) { o.ToolName = "filesystem.file.read" },
		func(o *domain.Operation) {
			o.FileName = "other.txt"
			o.NormalizedPath = filepath.Join(r.Workspace(), o.FileName)
		},
		func(o *domain.Operation) {
			o.NormalizedPath = filepath.Join(filepath.Dir(r.Workspace()), "outside.txt")
		},
		func(o *domain.Operation) { o.Content += "changed" }, func(o *domain.Operation) { o.ContentHash = strings.Repeat("0", 64) },
		func(o *domain.Operation) { o.Bytes++ }, func(o *domain.Operation) { o.Operation = "overwrite" },
		func(o *domain.Operation) { o.ApprovalID += "changed" }, func(o *domain.Operation) { o.ExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano) },
	}
	for i, mutate := range mutations {
		changed := op
		mutate(&changed)
		called := false
		v, err := r.Execute(context.Background(), changed, func(context.Context, domain.Operation) error { called = true; return nil })
		if err == nil || called || v.Created {
			t.Errorf("mutation %d: %+v %v called=%v", i, v, err, called)
		}
	}
	if _, err := r.Prepare("task", "changed.txt", "other", op.ApprovalID, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("rebound existing approval ID")
	}
}

func TestCreateVerifiedNoOverwriteOrReplay(t *testing.T) {
	r := testRegistry(t)
	content := "Hello\r\n你好\x00🙂"
	op := prepare(t, r, "created.txt", content)
	v, err := r.Execute(context.Background(), op, allow)
	if err != nil || !v.Passed || !v.Created || !v.ContentMatched || v.ExpectedHash != v.ActualHash || v.ActualBytes != len(content) {
		t.Fatalf("%+v %v", v, err)
	}
	actual, err := os.ReadFile(op.NormalizedPath)
	if err != nil || string(actual) != content {
		t.Fatalf("readback %q %v", actual, err)
	}
	if _, err := r.Execute(context.Background(), op, allow); err == nil {
		t.Fatal("replayed approval")
	}
	op2, err := r.Prepare("task2", op.FileName, "replace", "approval2", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if v, err := r.Execute(context.Background(), op2, allow); code(err) != "FILE_EXISTS" || v.Created {
		t.Fatalf("overwrite %+v %v", v, err)
	}
	actual, _ = os.ReadFile(op.NormalizedPath)
	if string(actual) != content {
		t.Fatal("existing file overwritten")
	}
	entries, _ := os.ReadDir(r.Workspace())
	if len(entries) != 1 {
		t.Fatalf("temporary files left: %v", entries)
	}
}

func TestCancelDuringApprovalCreatesNothing(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "cancelled.txt", "hello")
	ctx, cancel := context.WithCancel(context.Background())
	v, err := r.Execute(ctx, op, func(context.Context, domain.Operation) error { cancel(); return nil })
	if code(err) != "CANCELLED" || v.Created {
		t.Fatalf("%+v %v", v, err)
	}
	entries, _ := os.ReadDir(r.Workspace())
	if len(entries) != 0 {
		t.Fatalf("cancelled operation wrote files: %v", entries)
	}
}

func TestExpiryAndClosedRegistry(t *testing.T) {
	r := testRegistry(t)
	if _, err := r.Prepare("task", "x.txt", "x", "expired", time.Now().Add(-time.Second)); code(err) != "APPROVAL_EXPIRED" {
		t.Fatal(err)
	}
	op, err := r.Prepare("task", "x.txt", "x", "expires", time.Now().Add(20*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := r.Execute(context.Background(), op, allow); code(err) != "APPROVAL_EXPIRED" {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(context.Background(), op, allow); code(err) != "REGISTRY_CLOSED" {
		t.Fatal(err)
	}
}

// A backend callback may hold the service mutex while another service request
// prepares an operation. Holding the registry mutex across it deadlocks both.
func TestApprovalCallbackDoesNotHoldRegistryLock(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "concurrent.txt", "hello")
	callback := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := r.Execute(context.Background(), op, func(context.Context, domain.Operation) error {
			close(callback)
			prepared := make(chan error, 1)
			go func() {
				_, err := r.Prepare("other-task", "other.txt", "other", "other-approval", time.Now().Add(time.Minute))
				prepared <- err
			}()
			select {
			case err := <-prepared:
				return err
			case <-time.After(time.Second):
				return errors.New("registry callback held lock")
			}
		})
		finished <- err
	}()
	select {
	case <-callback:
	case <-time.After(time.Second):
		t.Fatal("callback did not start")
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approval callback blocked by registry lock")
	}
}

func TestCloseDuringApprovalPreventsWrites(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "closed.txt", "hello")
	v, err := r.Execute(context.Background(), op, func(context.Context, domain.Operation) error { return r.Close() })
	if code(err) != "REGISTRY_CLOSED" || v.Created {
		t.Fatalf("%+v %v", v, err)
	}
	entries, _ := os.ReadDir(r.Workspace())
	if len(entries) != 0 {
		t.Fatal("closed registry wrote a file")
	}
}

// This context derives cancellation from a real filesystem event, making the
// before/after-commit distinction deterministic without production test hooks.
type cancelOnFile struct {
	context.Context
	pattern string
	done    chan struct{}
	once    sync.Once
}

func (c *cancelOnFile) Done() <-chan struct{} { return c.done }
func (c *cancelOnFile) Err() error {
	paths, _ := filepath.Glob(c.pattern)
	if len(paths) != 0 {
		c.once.Do(func() { close(c.done) })
	}
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}

func TestCancellationCleansOnlyUncommittedTemp(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "cancel-temp.txt", "content")
	ctx := &cancelOnFile{Context: context.Background(), pattern: filepath.Join(r.Workspace(), ".deskpilot-*.tmp"), done: make(chan struct{})}
	v, err := r.Execute(ctx, op, allow)
	if code(err) != "CANCELLED" || v.Created {
		t.Fatalf("%+v %v", v, err)
	}
	entries, _ := os.ReadDir(r.Workspace())
	if len(entries) != 0 {
		t.Fatalf("uncommitted temp not cleaned: %v", entries)
	}
}

func TestCancellationAfterCommitNeverDeletesCreatedFile(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "cancel-commit.txt", "committed content")
	ctx := &cancelOnFile{Context: context.Background(), pattern: op.NormalizedPath, done: make(chan struct{})}
	v, err := r.Execute(ctx, op, allow)
	if code(err) != "CANCELLED" || !v.Created || v.Passed {
		t.Fatalf("%+v %v", v, err)
	}
	actual, readErr := os.ReadFile(op.NormalizedPath)
	if readErr != nil || string(actual) != op.Content {
		t.Fatalf("committed file removed/changed: %q %v", actual, readErr)
	}
}

func TestIndependentVerifierRejectsChangedContent(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "verify.txt", "approved")
	witness := executeWitness(t, r, op)
	if err := os.WriteFile(op.NormalizedPath, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := r.Inspect(op, witness)
	if code(err) != "VERIFICATION_FAILED" || v.Passed || v.ContentMatched || !v.Created || v.ActualHash == v.ExpectedHash || v.ActualBytes != 8 {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestPinnedWorkspaceAndAncestorsCannotBeRenamed(t *testing.T) {
	r := testRegistry(t)
	if err := os.Rename(r.Workspace(), r.Workspace()+"-moved"); err == nil {
		t.Fatal("pinned workspace was renamed")
	}
	parent := filepath.Dir(r.Workspace())
	if err := os.Rename(parent, parent+"-moved"); err == nil {
		t.Fatal("pinned ancestor was renamed")
	}
}

// Directory mount points (junctions) require no symbolic-link privilege. This
// fixture uses only two empty project-local temporary directories.
func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Mkdir(link, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(link) })
	setJunction(t, link, target)
}

func setJunction(t *testing.T, link, target string) {
	t.Helper()
	name, _ := windows.UTF16PtrFromString(link)
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	substitute, _ := windows.UTF16FromString(`\??\` + target)
	printName, _ := windows.UTF16FromString(target)
	pathBuffer := append(substitute, printName...)
	buf := make([]byte, 16+len(pathBuffer)*2)
	binary.LittleEndian.PutUint32(buf[0:4], windows.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(buf[4:6], uint16(len(buf)-8))
	binary.LittleEndian.PutUint16(buf[10:12], uint16((len(substitute)-1)*2))
	binary.LittleEndian.PutUint16(buf[12:14], uint16(len(substitute)*2))
	binary.LittleEndian.PutUint16(buf[14:16], uint16((len(printName)-1)*2))
	for i, ch := range pathBuffer {
		binary.LittleEndian.PutUint16(buf[16+i*2:], ch)
	}
	var returned uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &returned, nil); err != nil {
		t.Fatal(err)
	}
}

func TestJunctionWorkspaceAndAncestorDenied(t *testing.T) {
	base := t.TempDir()
	target := t.TempDir()
	junction := filepath.Join(base, "junction")
	makeJunction(t, junction, target)
	for _, path := range []string{junction, filepath.Join(junction, "workspace")} {
		r, err := New(path)
		if err == nil {
			r.Close()
			t.Fatalf("junction accepted: %s", path)
		}
		if code(err) != "WORKSPACE_UNSAFE" {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 0 {
		t.Fatal("junction escape wrote outside workspace")
	}
}

func TestJunctionInjectedDuringApprovalDenied(t *testing.T) {
	r := testRegistry(t)
	target := t.TempDir()
	op := prepare(t, r, "escape.txt", "must not escape")
	v, err := r.Execute(context.Background(), op, func(context.Context, domain.Operation) error {
		setJunction(t, r.Workspace(), target)
		return nil
	})
	if code(err) != "WORKSPACE_UNSAFE" || v.Created {
		t.Fatalf("%+v %v", v, err)
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 0 {
		t.Fatal("post-approval junction escaped")
	}
}

func TestAtomicCommitCannotReplaceFileCreatedAfterApproval(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "race.txt", "approved new content")
	v, err := r.Execute(context.Background(), op, func(context.Context, domain.Operation) error {
		return os.WriteFile(op.NormalizedPath, []byte("concurrent existing content"), 0600)
	})
	if code(err) != "FILE_EXISTS" || v.Created {
		t.Fatalf("%+v %v", v, err)
	}
	actual, readErr := os.ReadFile(op.NormalizedPath)
	if readErr != nil || string(actual) != "concurrent existing content" {
		t.Fatalf("race overwrote file: %q %v", actual, readErr)
	}
	entries, _ := os.ReadDir(r.Workspace())
	if len(entries) != 1 {
		t.Fatalf("uncommitted temp leaked: %v", entries)
	}
}

func TestIndependentVerifierReportsActualOversize(t *testing.T) {
	r := testRegistry(t)
	op := prepare(t, r, "oversized.txt", "approved")
	witness := executeWitness(t, r, op)
	if err := os.WriteFile(op.NormalizedPath, []byte(strings.Repeat("x", 65538)), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := r.Inspect(op, witness)
	if code(err) != "VERIFICATION_FAILED" || v.Passed || !v.Created || v.ActualBytes != 65538 {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestCreateEmptyAndMaximumContent(t *testing.T) {
	r := testRegistry(t)
	for _, fixture := range []struct{ name, content string }{{"empty.txt", ""}, {"max.txt", strings.Repeat("a", 65536)}} {
		op := prepare(t, r, fixture.name, fixture.content)
		v, err := r.Execute(context.Background(), op, allow)
		if err != nil || !v.Passed || !v.Created || v.ActualBytes != len(fixture.content) {
			t.Fatalf("%s: %+v %v", fixture.name, v, err)
		}
	}
}
