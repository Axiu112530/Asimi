package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistryRejectsShellAndUnsafeArguments(t *testing.T) {
	dir := t.TempDir()
	for _, q := range []struct {
		id      string
		args    []string
		network bool
	}{
		{"powershell", []string{"-Command", "whoami"}, false},
		{"go.test", []string{"../../outside"}, true},
		{"git.status", []string{"--exec-path=evil"}, false},
		{"go.test", []string{"./..."}, false},
		{"python.pytest", []string{"-p", "evil"}, true},
	} {
		if _, e := Prepare(q.id, q.args, dir, q.network); e == nil {
			t.Errorf("accepted %s %v", q.id, q.args)
		}
	}
}

func TestNetworkSharesAreRejectedBeforeFilesystemAccess(t *testing.T) {
	if _, e := Prepare("git.version", nil, `\\server\share`, false); e == nil || e.Error() != "COMMAND_DIRECTORY" {
		t.Fatalf("UNC directory accepted: %v", e)
	}
	for _, s := range []string{`\\server\share\git.exe`, `\\?\UNC\server\share`, `relative`, `C:relative`} {
		if localPath(s) {
			t.Fatal("not local", s)
		}
	}
}

func TestVersionExecutionAndImmutableProposal(t *testing.T) {
	p, e := Prepare("git.version", nil, t.TempDir(), false)
	if e != nil {
		t.Fatal(e)
	}
	r, e := Run(context.Background(), p)
	if e != nil || r.ExitCode != 0 || !strings.Contains(r.Output, "git version") {
		t.Fatalf("%+v %v", r, e)
	}
	p.Args = []string{"status"}
	if _, e = Run(context.Background(), p); e == nil {
		t.Fatal("tampered proposal executed")
	}
}

func TestProjectCommandRequiresNetworkConsentAndScopedTarget(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0600)
	p, e := Prepare("go.test", []string{"./..."}, dir, true)
	if e != nil {
		t.Fatal(e)
	}
	if !p.ExecutesCode || !p.Network || p.TimeoutSeconds < 1 {
		t.Fatalf("risk missing: %+v", p)
	}
	if _, e = Prepare("go.test", []string{"C:\\outside"}, dir, true); e == nil {
		t.Fatal("absolute target accepted")
	}
}

func TestCancellationBeforeDispatch(t *testing.T) {
	p, e := Prepare("git.version", nil, t.TempDir(), false)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	r, e := Run(ctx, p)
	if e == nil || r.ExitCode == 0 || time.Since(start) > time.Second {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestSanitizedEnvironmentAndOutputLimits(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "do-not-inherit")
	t.Setenv("NODE_OPTIONS", "--require malicious.js")
	p, e := Prepare("git.version", nil, t.TempDir(), false)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range environment(p) {
		if strings.Contains(v, "do-not-inherit") || strings.Contains(v, "NODE_OPTIONS") {
			t.Fatal("injection or secret inherited")
		}
	}
	if got := redact("API_KEY=supersecret password:bad authorization=Bearer_token"); strings.Contains(got, "supersecret") || strings.Contains(got, "Bearer_token") {
		t.Fatal(got)
	}
	p.MaxOutputBytes = 4
	p.Hash = proposalHash(p)
	r, e := Run(context.Background(), p)
	if e != nil || !r.Truncated || len(r.Output) > 4 {
		t.Fatalf("%+v %v", r, e)
	}
}
