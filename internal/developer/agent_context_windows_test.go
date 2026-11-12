package developer

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAgentContextOnlySharesSelectedSource(t *testing.T) {
	root := fixture(t)
	source := "package demo\r\nfunc Calculate() int { return 1 }\r\n"
	put(t, root, "calculate.go", source)
	put(t, root, "calculate_test.go", "package demo\n// unapproved-marker\n")
	put(t, root, "go.mod", "module example.org/demo\n\nrequire example.org/private v1.0.0\n")
	put(t, root, "package.json", `{"dependencies":{"node-package":"https://user:secret-value@example.org/a"}}`)
	put(t, root, ".env", "PASSWORD=secret-value")
	put(t, root, filepath.Join("nested", ".azure", "hidden.go"), "package secret")
	context, err := BuildAgentContext(root, "Fix Calculate in "+root, []string{".env", "calculate.go", "nested/.azure/hidden.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Files) != 1 || len(context.Files[0].Symbols) != 1 {
		t.Fatalf("selected source and symbol expected: %+v", context.Files)
	}
	f := context.Files[0]
	if f.Path != "calculate.go" || f.Content != source || f.SHA256 != hash(source) || !f.Editable || f.Symbols[0] != "Calculate" {
		t.Fatalf("incorrect approved source: %+v", f)
	}
	data, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"unapproved-marker", "secret-value", ".env", ".azure", filepath.Base(root)} {
		if strings.Contains(string(data), banned) {
			t.Errorf("unexpected outbound value %q", banned)
		}
	}
	for _, expected := range []string{"calculate_test.go", "go.test", "example.org/private", "node-package"} {
		if !strings.Contains(string(data), expected) {
			t.Errorf("metadata missing %q: %s", expected, data)
		}
	}
	withoutSelection, err := BuildAgentContext(root, "Calculate", nil)
	if err != nil || len(withoutSelection.Files) != 0 {
		t.Fatalf("implicit source disclosure: %+v %v", withoutSelection, err)
	}
}

func TestAgentContextRejectsUnsafeSelectionsAndMarksPartialFiles(t *testing.T) {
	root := fixture(t)
	raw := "package demo\nvar apiKey string = \"hidden-value\"\n"
	put(t, root, "config.go", raw)
	put(t, root, "large.md", strings.Repeat("界", 20<<10))
	context, err := BuildAgentContext(root, "config", []string{"../outside.go", "config.go", "CONFIG.GO", "large.md"})
	if err != nil || len(context.Files) != 2 {
		t.Fatalf("%+v %v", context, err)
	}
	first, second := context.Files[0], context.Files[1]
	if first.Editable || !first.Redacted || first.SHA256 != hash(raw) || strings.Contains(first.Content, "hidden-value") {
		t.Fatalf("redacted source can be edited or has invalid metadata: %+v", first)
	}
	if second.Editable || !second.Truncated || !utf8.ValidString(second.Content) || len(second.Content) >= 20<<10 {
		t.Fatalf("large source should be a bounded UTF-8 excerpt")
	}
}

func TestRedactAgentTextCommonFormats(t *testing.T) {
	const input = "const clientSecret = \"hidden-1\";\n" +
		"\"api_key\": \"hidden-2\", \"password\": \"hidden-3\"\n" +
		"authorization: Bearer hidden-4\n" +
		"postgresql://admin:hidden-5@example.org/database\n" +
		"const connectionString = `Server=host;User ID=test;Password=hidden-6;`\n" +
		"private_key = \"\"\"\nhidden-7\n\"\"\"\n" +
		"-----BEGIN PRIVATE KEY-----\nhidden-8\n-----END PRIVATE KEY-----\n" +
		"service failed: token hidden-9\n"
	got := RedactAgentText(input)
	if strings.Contains(got, "hidden-") {
		t.Fatalf("secret survived: %s", got)
	}
	if RedactAgentText("func Calculate() int { return 4 }") != "func Calculate() int { return 4 }" {
		t.Fatal("redaction modified ordinary source")
	}
}

func TestAgentContextSharesOnlySafeRelativePaths(t *testing.T) {
	for _, name := range []string{"../outside.go", "nested/.aws/settings", "nested/SECRET~1.TXT", "conf/credentials.yaml", "secret.jks", "sk-hiddensecretvalue.go"} {
		if AgentContextPathAllowed(name) {
			t.Errorf("accepted sensitive or unsafe path %q", name)
		}
	}
	if !AgentContextPathAllowed("src/calculator_test.go") {
		t.Fatal("rejected ordinary relative path")
	}
	got := RedactAgentWorkspaceText(`C:\Some\Workspace`, `C:\SOME\WORKSPACE\test.go c:/some/workspace/test.go C:\\Some\\Workspace\\test.go`)
	if strings.Contains(strings.ToLower(got), "some") || strings.Count(got, "[WORKSPACE]") != 3 {
		t.Fatal(got)
	}
}
