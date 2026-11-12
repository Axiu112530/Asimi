//go:build windows

package developer

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func junction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Mkdir(link, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(link) })
	name, _ := windows.UTF16PtrFromString(link)
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
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

// Reparse traversal and hardlink aliases must not expose or overwrite files
// outside a workspace even when the link itself is inside the selected root.
func TestJunctionAncestorsAndHardlinksDenied(t *testing.T) {
	root := fixture(t)
	outside := fixture(t)
	put(t, outside, "secret.txt", "outside")
	link := filepath.Join(root, "junction")
	junction(t, link, outside)
	for _, candidate := range []string{link, filepath.Join(link, "nested")} {
		if _, err := ValidateRoot(candidate); err == nil {
			t.Fatalf("accepted junction root %q", candidate)
		}
	}
	if _, err := Read(root, "junction/secret.txt"); err == nil {
		t.Fatal("junction read escaped")
	}
	if _, err := Prepare(root, "junction/new.txt", "escape"); err == nil {
		t.Fatal("junction prepare escaped")
	}
	if err := os.Link(filepath.Join(outside, "secret.txt"), filepath.Join(root, "hard.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(root, "hard.txt"); err == nil {
		t.Fatal("hardlink read accepted")
	}
	if _, err := Prepare(root, "hard.txt", "escape"); err == nil {
		t.Fatal("hardlink overwrite accepted")
	}
	report, err := Analyze(root, "outside")
	if err != nil || len(report.Matches) != 0 {
		t.Fatalf("analysis escaped: %#v %v", report, err)
	}
}

func TestPostApprovalJunctionCannotRedirectChange(t *testing.T) {
	root := fixture(t)
	outside := fixture(t)
	put(t, root, "src/main.txt", "old")
	put(t, outside, "main.txt", "old")
	c, err := Prepare(root, "src/main.txt", "new")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "src"), filepath.Join(root, "old-src")); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(root, "src"), outside)
	if _, err := Apply(context.Background(), root, c); err == nil {
		t.Fatal("post approval junction accepted")
	}
	actual, _ := os.ReadFile(filepath.Join(outside, "main.txt"))
	if string(actual) != "old" {
		t.Fatal("outside target modified")
	}
}

func TestPinsPreventAncestorRenameAndSourceWrite(t *testing.T) {
	root := fixture(t)
	put(t, root, "src/main.txt", "old")
	w, err := openWorkspace(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	parent, _, err := w.parent("src/main.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := os.Rename(root, root+"-moved"); err == nil {
		os.Rename(root+"-moved", root)
		t.Fatal("root pin allowed rename")
	}
	if err := os.Rename(filepath.Join(root, "src"), filepath.Join(root, "src-moved")); err == nil {
		os.Rename(filepath.Join(root, "src-moved"), filepath.Join(root, "src"))
		t.Fatal("parent pin allowed rename")
	}
}

func TestNativeReplacementCannotBypassDenyWriteHandle(t *testing.T) {
	root := fixture(t)
	put(t, root, "target.txt", "old")
	c, err := Prepare(root, "target.txt", "new")
	if err != nil {
		t.Fatal(err)
	}
	w, err := openWorkspace(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	h, err := openRelative(w.root, "target.txt", windows.FILE_GENERIC_READ, windows.FILE_SHARE_READ, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	result, err := Apply(context.Background(), root, c)
	if err == nil || result.Applied {
		t.Fatalf("replacement bypassed deny-write handle: %#v %v", result, err)
	}
	actual, _ := os.ReadFile(filepath.Join(root, "target.txt"))
	if string(actual) != "old" {
		t.Fatal("blocked replacement changed file")
	}
}

func TestTransactionBlocksConcurrentWriteAndRename(t *testing.T) {
	root := fixture(t)
	put(t, root, "target.txt", "original")
	target := filepath.Join(root, "target.txt")
	tx, err := beginFileTransaction()
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(tx)
	h, err := openReplacement(tx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	var written uint32
	if err = windows.WriteFile(h, []byte("changed!"), &written, nil); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, []byte("raced"), 0600); err == nil {
		t.Fatal("external writer reached transactional file")
	}
	if err = os.Rename(target, filepath.Join(root, "moved.txt")); err == nil {
		t.Fatal("external rename reached transactional file")
	}
	if ok, _, _ := rollbackTransaction.Call(uintptr(tx)); ok == 0 {
		t.Fatal("rollback failed")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatalf("rollback result: %q %v", data, err)
	}
}
