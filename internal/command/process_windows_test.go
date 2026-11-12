package command

import (
	"context"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestManagedProcessFixture(t *testing.T) {
	marker := -1
	for i, a := range os.Args {
		if a == "--managed-fixture" {
			marker = i
			break
		}
	}
	if marker < 0 {
		return
	}
	if os.Args[marker+1] == "child" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestManagedProcessFixture$", "--", "--managed-fixture", "child")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e := cmd.Start(); e != nil {
		os.Exit(2)
	}
	if e := os.WriteFile(os.Args[marker+2], []byte(strconv.Itoa(cmd.Process.Pid)), 0600); e != nil {
		os.Exit(3)
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

func TestManagedCancellationKillsDescendants(t *testing.T) {
	path, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	hash, e := digest(path)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	directoryID, e := directoryIdentity(dir)
	if e != nil {
		t.Fatal(e)
	}
	pidFile := filepath.Join(dir, "child.pid")
	p := Proposal{ID: "fixture", Executable: path, ExecutableHash: hash, Args: []string{"-test.run=^TestManagedProcessFixture$", "--", "--managed-fixture", "parent", pidFile}, Directory: dir, DirectoryID: directoryID, TimeoutSeconds: 15, MaxOutputBytes: 1024}
	p.Hash = proposalHash(p)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Result, 1)
	go func() { r, _ := runManaged(ctx, p, nil); done <- r }()
	deadline := time.Now().Add(5 * time.Second)
	var childID int
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(pidFile)
		childID, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		if childID > 0 {
			break
		}
		select {
		case r := <-done:
			t.Fatalf("fixture never launched: %+v", r)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childID == 0 {
		t.Fatal("child not created")
	}
	child, e := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(childID))
	if e != nil {
		t.Fatal(e)
	}
	defer windows.CloseHandle(child)
	cancel()
	select {
	case r := <-done:
		if !r.Cancelled || r.ExitCode == 0 {
			t.Fatalf("%+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("process tree did not drain")
	}
	status, e := windows.WaitForSingleObject(child, 2000)
	if e != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant remained alive: %d %v", status, e)
	}
}
