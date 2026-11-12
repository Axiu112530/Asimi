//go:build windows

package developer

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestProjectInspectionGitReadOnly(t *testing.T) {
	git, err := exec.LookPath("git.exe")
	if err != nil {
		t.Skip("Git is needed to create the local test fixture")
	}
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = root
		cmd.Env = []string{
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + root,
			"TEMP=" + root,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=NUL",
			"GIT_OPTIONAL_LOCKS=0",
		}
		result, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v, %s", args, err, result)
		}
		return strings.TrimSpace(string(result))
	}
	write := func(name, value string) {
		t.Helper()
		target := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	run("init", "-b", "main")
	run("config", "core.autocrlf", "true")
	write("go.mod", "module fixture\n\ngo 1.26.0\n")
	write("main.go", "package main\nfunc main() {}\n")
	write("main_test.go", "package main\n")
	write("tracked.txt", "Tracked text\n")
	write("package.json", "{\"engines\":{\"node\":\">=22\"},\"dependencies\":{\"vue\":\"^3.0.0\"}}\n")
	write(".gitignore", "*.tmp\nvendor/\ntarget/\n")
	write(".gitattributes", "* text=auto\n*.go text eol=lf\n")
	run("add", "--all")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "Fixture")
	head := run("rev-parse", "HEAD")
	write("main.go", "package main\nfunc main() { println(1) }\n")
	run("add", "main.go")
	write("main.go", "package main\r\nfunc main() { println(2) }\r\n")
	write("main_test.go", "package main\r\n")
	write("notes.txt", "untracked\n")
	write("ignored.tmp", "ignored\n")
	write("vendor/demo/pom.xml", "<project/>")
	write("target/demo/demo.csproj", "<Project/>")
	check := func() {
		t.Helper()
		before := read(".git/index")
		project, err := InspectProject(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := project.Git
		if got.Status != "AVAILABLE" || got.Branch != "main" || got.Head != head || got.Staged != 1 ||
			got.Unstaged != 1 || got.Untracked != 1 || got.ChangedFiles != 2 || got.Truncated ||
			!got.DirtyPath("MAIN.GO") || !got.DirtyPath("notes.txt") || len(got.ForAgent().Changes) != 0 {
			t.Fatalf("Git context: %+v", got)
		}
		if !bytes.Equal(before, read(".git/index")) {
			t.Fatal("inspection changed the index")
		}
		if !slices.Equal(project.Summary.Types, []string{"Go", "Node.js"}) ||
			!slices.Contains(project.Summary.Frameworks, "Vue") ||
			project.Summary.RuntimeRequirements["go"] != "1.26.0" || project.Summary.RuntimeRequirements["node"] != "22" ||
			!slices.Contains(project.Summary.TestPaths, "main_test.go") {
			t.Fatalf("project: %+v", project.Summary)
		}
	}
	check()
	run("gc", "--prune=now")
	check()
	context, err := BuildAgentContext(root, "repair the entry point", []string{"main.go"})
	if err != nil || context.Git.Head != head || len(context.Files) != 1 || !context.Files[0].GitDirty || len(context.Git.Changes) != 0 {
		t.Fatalf("Agent context: %+v, %v", context, err)
	}
	scoped, err := BuildAgentContextWithExclusions(root, "review project", []string{"main.go"}, []string{"main.go", "notes.txt"})
	if err != nil || len(scoped.Files) != 0 || scoped.Git.Status != "AVAILABLE" || scoped.Git.Staged != 0 ||
		scoped.Git.Unstaged != 0 || scoped.Git.Untracked != 0 || !scoped.Git.Truncated {
		t.Fatalf("excluded changes in Agent context: %+v, %v", scoped, err)
	}
	config := read(".git/config")
	write(".git/config", string(config)+"\n[include]\npath = ../outside\n")
	if got, err := ReadGitContext(root); err != nil || got.Status != "UNSUPPORTED" || got.Head != "" {
		t.Fatalf("unsupported config context: %+v, %v", got, err)
	}

	write(".git/config", string(config))
	write(".gitattributes", "*.txt -text\n")
	run("add", ".gitattributes")
	if err := os.Remove(filepath.Join(root, ".gitattributes")); err != nil {
		t.Fatal(err)
	}
	write("tracked.txt", "Tracked text\r\n")
	if status := run("status", "--porcelain=v1"); !strings.Contains(status, " M tracked.txt") {
		t.Fatalf("Git did not use indexed attributes: %s", status)
	}
	got, err := ReadGitContext(root)
	if err != nil || got.Status != "AVAILABLE" || !got.DirtyPath("tracked.txt") {
		t.Fatalf("indexed attributes context: %+v, %v", got, err)
	}
}

func TestGitContextNeverFollowsLinkedRepository(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+outside+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadGitContext(root)
	if err != nil || got.Status != "UNSUPPORTED" || got.Head != "" {
		t.Fatalf("linked repository context: %+v, %v", got, err)
	}
}

func TestGitContextRejectsJunctionMetadata(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "HEAD"), []byte("ref: refs/heads/outside\n"), 0600); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(root, ".git"), outside)
	got, err := ReadGitContext(root)
	if err != nil || got.Status != "UNSUPPORTED" || got.Branch != "" {
		t.Fatalf("junction metadata context: %+v, %v", got, err)
	}
}

func TestSuggestAgentPathsAndContextHonorExclusions(t *testing.T) {
	root := fixture(t)
	put(t, root, "go.mod", "module fixture\n\ngo 1.26.0\n")
	put(t, root, "main.go", "package main\n")
	put(t, root, "calculate.go", "package main\nfunc Calculate() int { return 1 }\n")
	put(t, root, "calculate_test.go", "package main\n")
	put(t, root, "calculate_secret.go", "package main\nconst api_key = \"sensitive-fixture-value\"\n")
	put(t, root, "calculate_oversize.go", "package main\n"+strings.Repeat(" ", agentMaxFileBytes))
	put(t, root, "skipped/calculate.go", "package skipped\n")
	put(t, root, "skipped/pom.xml", "<project/>")
	put(t, root, ".credentials/calculate.go", "package credentials\n")
	exclusions := []string{"skipped"}
	paths, err := SuggestAgentPaths(root, "Fix calculate", exclusions)
	if err != nil || !slices.Equal(paths, []string{"calculate_test.go", "calculate.go", "main.go"}) {
		t.Fatalf("suggested paths: %v, %v", paths, err)
	}
	context, err := BuildAgentContextWithExclusions(root, "Fix calculate", []string{"calculate.go", "skipped/calculate.go"}, exclusions)
	if err != nil || len(context.Files) != 1 || context.Files[0].Path != "calculate.go" ||
		!slices.Equal(context.Project.Types, []string{"Go"}) || len(context.Omitted) == 0 {
		t.Fatalf("context: %+v, %v", context, err)
	}
}
