package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompatibilityRegistryTargetValidation(t *testing.T) {
	root := t.TempDir()
	writeCommandFixture(t, root, "sample.js", "console.log('fixture')\n")
	writeCommandFixture(t, root, "test_fixture.py", "import unittest\n")
	for _, tt := range []struct {
		id, arg string
	}{
		{"node.test", "./..."},
		{"node.check", "test_fixture.py"},
		{"node.check", "sample.js."},
		{"node.check", "./../sample.js"},
		{"node.check", "--inspect"},
		{"node.check", "./sample.js/.."},
		{"python.check", "sample.js"},
		{"python.deps", "test_fixture.py"},
		{"java.test", "./..."},
		{"dotnet.build", "sample.js"},
		{"go.test", "sample.js"},
	} {
		if _, err := Prepare(tt.id, []string{tt.arg}, root, true); err != Error("COMMAND_ARGUMENTS") {
			t.Errorf("%s %s should fail argument validation, got %v", tt.id, tt.arg, err)
		}
	}
	for _, id := range []string{"go.build", "go.deps", "node.test", "java.test", "dotnet.build", "python.unittest"} {
		if _, err := Prepare(id, nil, root, false); err != Error("COMMAND_NETWORK_CONSENT") {
			t.Errorf("%s should require explicit project-code consent, got %v", id, err)
		}
	}
}

func writeCommandFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibilityCommandStreamsTimeoutAndCancellation(t *testing.T) {
	root := t.TempDir()
	writeCommandFixture(t, root, "report.test.cjs", "const test = require('node:test'); test('fixture', () => { console.error('stderr-marker'); console.log('stdout-marker'); });\n")
	p, err := Prepare("node.test", []string{"report.test.cjs"}, root, true)
	if err == Error("COMMAND_UNAVAILABLE") {
		t.Skip("Node unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), p)
	if err != nil || !strings.Contains(result.Stdout, "stdout-marker") || !strings.Contains(result.Output, "stdout-marker") {
		t.Fatalf("unexpected stream capture: %+v %v", result, err)
	}
	writeCommandFixture(t, root, "bad.cjs", "const invalid = ;\n")
	bad, err := Prepare("node.check", []string{"bad.cjs"}, root, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err = Run(context.Background(), bad)
	if err != Error("COMMAND_EXIT_FAILED") || result.Stdout != "" || !strings.Contains(result.Stderr, "SyntaxError") || !strings.Contains(result.Output, "SyntaxError") {
		t.Fatalf("unexpected diagnostic capture: %+v %v", result, err)
	}
	writeCommandFixture(t, root, "wait.test.cjs", "setTimeout(() => {}, 10000);\n")
	p, err = Prepare("node.test", []string{"wait.test.cjs"}, root, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	result, err = Run(ctx, p)
	if err != Error("COMMAND_TIMEOUT") || result.Code != "COMMAND_TIMEOUT" || !result.TimedOut || result.Cancelled {
		t.Fatalf("unexpected timeout: %+v %v", result, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	result, err = Run(ctx, p)
	if err != Error("COMMAND_CANCELLED") || result.Code != "COMMAND_CANCELLED" || result.TimedOut || !result.Cancelled {
		t.Fatalf("unexpected cancellation: %+v %v", result, err)
	}
}

func TestCompatibilityProjectRuntimes(t *testing.T) {
	t.Run("Go", func(t *testing.T) {
		root := t.TempDir()
		writeCommandFixture(t, root, "go.mod", "module fixture.test/compat\n\ngo 1.26.0\n")
		writeCommandFixture(t, root, "calc/answer.go", "package calc\nfunc Answer() int { return 42 }\n")
		writeCommandFixture(t, root, "calc/answer_test.go", "package calc\nimport \"testing\"\nfunc TestAnswer(t *testing.T) { if Answer() != 42 { t.Fatal(\"unexpected answer\") } }\n")
		runCompatibilityFixture(t, root, "go.test", "ok", "calc")
		runCompatibilityFixture(t, root, "go.build", "", "calc")
		runCompatibilityFixture(t, root, "go.vet", "", "calc")
		runCompatibilityFixture(t, root, "go.deps", "fixture.test/compat")
	})
	t.Run("Node", func(t *testing.T) {
		root := t.TempDir()
		writeCommandFixture(t, root, "fixture.test.cjs", "const test = require('node:test'); test('fixture', () => {});\n")
		writeCommandFixture(t, root, "package.json", `{"name":"compat-fixture","version":"1.0.0","private":true}`)
		runCompatibilityFixture(t, root, "node.test", "pass 1", "fixture.test.cjs")
		runCompatibilityFixture(t, root, "node.check", "", "fixture.test.cjs")
		runCompatibilityFixture(t, root, "npm.deps", "compat-fixture")
	})
	t.Run("Java", func(t *testing.T) {
		root := t.TempDir()
		for _, program := range []string{"java", "javac", "jdeps"} {
			if _, _, err := resolve(program, root); err == Error("COMMAND_UNAVAILABLE") {
				t.Skip("canonical local JDK bin directory unavailable")
			} else if err != nil {
				t.Fatal(err)
			}
		}
		writeCommandFixture(t, root, "Fixture.java", "public class Fixture { public static void main(String[] args) { assert 20 + 22 == 42; System.out.println(\"fixture passed\"); } }\n")
		runCompatibilityFixture(t, root, "java.test", "fixture passed", "Fixture.java")
		runCompatibilityFixture(t, root, "java.build", "", "Fixture.java")
		runCompatibilityFixture(t, root, "java.deps", "java.base", "Fixture.class")
	})
	t.Run("Python", func(t *testing.T) {
		root := t.TempDir()
		writeCommandFixture(t, root, "test_fixture.py", "import unittest\n\nclass Fixture(unittest.TestCase):\n    def test_value(self):\n        self.assertEqual(20 + 22, 42)\n")
		runCompatibilityFixture(t, root, "python.unittest", "Ran 1 test")
		runCompatibilityFixture(t, root, "python.check", "", "test_fixture.py")
		runCompatibilityFixture(t, root, "python.deps", "pip")
	})
	t.Run("DotNetSDK", func(t *testing.T) {
		runCompatibilityFixture(t, t.TempDir(), "dotnet.info", ".NET SDK")
	})
}

func TestCommandRejectsRehashedUnregisteredProposal(t *testing.T) {
	p, err := Prepare("git.version", nil, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	p.ID = "unknown"
	p.Hash = proposalHash(p)
	if _, err := Run(context.Background(), p); err != Error("COMMAND_CHANGED") {
		t.Fatalf("unknown proposal with recomputed hash: %v", err)
	}
	p.ID = "git.version"
	p.Args = []string{"status"}
	p.Hash = proposalHash(p)
	if _, err := Run(context.Background(), p); err != Error("COMMAND_CHANGED") {
		t.Fatalf("wrong fixed arguments with recomputed hash: %v", err)
	}
}

func runCompatibilityFixture(t *testing.T, root, id, expected string, args ...string) {
	t.Helper()
	t.Run(id, func(t *testing.T) {
		p, err := Prepare(id, args, root, true)
		if err == Error("COMMAND_UNAVAILABLE") {
			t.Skip("local runtime unavailable")
		}
		if err != nil {
			t.Fatal(err)
		}
		r, err := Run(context.Background(), p)
		if err != nil || r.ExitCode != 0 || !strings.Contains(r.Output, expected) {
			t.Fatalf("runtime failed: %+v %v", r, err)
		}
	})
}
