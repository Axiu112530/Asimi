package developer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

func hash(content string) string {
	s := sha256.Sum256([]byte(content))
	return hex.EncodeToString(s[:])
}
func randomName(suffix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", Error("FILE_IO")
	}
	return ".asimi-" + hex.EncodeToString(b[:]) + suffix, nil
}
func validName(name string) bool {
	if name == "" || name == "." || name == ".." || !utf8.ValidString(name) || strings.ContainsAny(name, `\/:<>"|?*`) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || len(utf16.Encode([]rune(name))) > 255 {
		return false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return false
		}
	}
	base := strings.ToUpper(strings.TrimRight(strings.SplitN(name, ".", 2)[0], " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
		tail := strings.TrimPrefix(strings.TrimPrefix(base, "COM"), "LPT")
		if len([]rune(tail)) == 1 && strings.ContainsRune("0123456789¹²³", []rune(tail)[0]) {
			return false
		}
	}
	return true
}
func relativePath(relative string) (string, error) {
	if len(relative) > 2048 {
		return "", Error("PATH_DENIED")
	}
	relative = strings.ReplaceAll(relative, `\`, "/")
	parts := strings.Split(relative, "/")
	if len(parts) > 32 {
		return "", Error("PATH_DENIED")
	}
	for _, part := range parts {
		if !validName(part) {
			return "", Error("PATH_DENIED")
		}
	}
	return strings.Join(parts, "/"), nil
}
func excluded(name string, exclusions []string) bool {
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		switch part {
		case "node_modules", ".git", "bin", "obj", "build", "dist", "target", "vendor", ".npmrc", ".pypirc", ".ssh", ".aws":
			return true
		}
		if part == ".env" || strings.HasPrefix(part, ".env.") || (strings.HasPrefix(part, ".asimi-") || strings.HasPrefix(part, ".deskpilot-")) || strings.HasSuffix(part, ".pfx") || strings.HasSuffix(part, ".pem") || strings.HasSuffix(part, ".key") {
			return true
		}
	}
	lower := strings.ToLower(name)
	for _, exclude := range exclusions {
		exclude = strings.ToLower(strings.TrimSuffix(strings.ReplaceAll(exclude, `\`, "/"), "/"))
		if lower == exclude || strings.HasPrefix(lower, exclude+"/") {
			return true
		}
	}
	return false
}
func checkedOptions(options Options) (Options, error) {
	if len(options.Exclusions) > 100 {
		return options, Error("INVALID_EXCLUSIONS")
	}
	for i, e := range options.Exclusions {
		clean, err := relativePath(strings.TrimSuffix(e, "/"))
		if err != nil {
			return options, Error("INVALID_EXCLUSIONS")
		}
		options.Exclusions[i] = clean
	}
	if options.MaxFiles <= 0 || options.MaxFiles > MaxFiles {
		options.MaxFiles = MaxFiles
	}
	if options.MaxDepth <= 0 || options.MaxDepth > MaxDepth {
		options.MaxDepth = MaxDepth
	}
	if options.MaxBytes <= 0 || options.MaxBytes > MaxTotalBytes {
		options.MaxBytes = MaxTotalBytes
	}
	return options, nil
}
func ValidateRoot(root string) (string, error) {
	w, err := openWorkspace(root, false)
	if err != nil {
		return "", err
	}
	defer w.Close()
	return w.path, nil
}

// RootIdentity returns the canonical native object's identity, not its pathname.
func RootIdentity(root string) (string, error) {
	w, err := openWorkspace(root, false)
	if err != nil {
		return "", err
	}
	defer w.Close()
	return w.identity, nil
}
func Read(root, relative string) (File, error) { return ReadWithOptions(root, relative, Options{}) }
func ReadWithOptions(root, relative string, options Options) (File, error) {
	options, err := checkedOptions(options)
	if err != nil {
		return File{}, err
	}
	relative, err = relativePath(relative)
	if err != nil || excluded(relative, options.Exclusions) {
		return File{}, Error("PATH_DENIED")
	}
	w, err := openWorkspace(root, false)
	if err != nil {
		return File{}, err
	}
	defer w.Close()
	return w.snapshot(relative)
}
func Prepare(root, relative, content string) (Change, error) {
	return PrepareWithOptions(root, relative, content, Options{})
}
func PrepareWithOptions(root, relative, content string, options Options) (Change, error) {
	c := Change{}
	options, err := checkedOptions(options)
	if err != nil {
		return c, err
	}
	relative, err = relativePath(relative)
	if err != nil || excluded(relative, options.Exclusions) {
		return c, Error("PATH_DENIED")
	}
	if len(content) > MaxFileBytes || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return c, Error("FILE_NOT_TEXT_OR_TOO_LARGE")
	}
	w, err := openWorkspace(root, false)
	if err != nil {
		return c, err
	}
	defer w.Close()
	parent, _, err := w.parent(relative, false)
	if err != nil {
		return c, err
	}
	defer parent.Close()
	c = Change{Root: w.path, RelativePath: relative, Operation: "create", AfterContent: content, AfterHash: hash(content), RootIdentity: w.identity, ParentIdentity: parent.identity, Exclusions: append([]string(nil), options.Exclusions...)}
	before, err := w.snapshot(relative)
	if err == nil {
		c.Operation = "replace"
		c.BeforeContent = before.Content
		c.BeforeHash = before.Hash
		c.BeforeIdentity = before.Identity
		backup, err := randomName(".recovery")
		if err != nil {
			return Change{}, err
		}
		c.RecoveryPath = path.Join(path.Dir(relative), backup)
	} else if err != Error("FILE_NOT_FOUND") {
		return Change{}, err
	}
	c.Diff = exactDiff(c)
	return c, nil
}

// exactDiff is a complete one-hunk unified diff. Content fields preserve every
// byte, including CRLF and absent final newlines; no context is elided.
func exactDiff(c Change) string {
	oldName := "a/" + c.RelativePath
	if c.Operation == "create" {
		oldName = "/dev/null"
	}
	oldLines := lineCount(c.BeforeContent)
	newLines := lineCount(c.AfterContent)
	oldStart := 1
	if oldLines == 0 {
		oldStart = 0
	}
	newStart := 1
	if newLines == 0 {
		newStart = 0
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ b/%s\n@@ -%d,%d +%d,%d @@\n", oldName, c.RelativePath, oldStart, oldLines, newStart, newLines)
	appendLines := func(prefix, content string) {
		if content == "" {
			return
		}
		lines := strings.SplitAfter(content, "\n")
		for _, line := range lines {
			if line != "" {
				b.WriteString(prefix)
				b.WriteString(line)
				if !strings.HasSuffix(line, "\n") {
					b.WriteString("\n\\ No newline at end of file\n")
				}
			}
		}
	}
	appendLines("-", c.BeforeContent)
	appendLines("+", c.AfterContent)
	return b.String()
}
func lineCount(content string) int {
	if content == "" {
		return 0
	}
	n := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		n++
	}
	return n
}
func Apply(ctx context.Context, root string, c Change) (ChangeResult, error) {
	result := ChangeResult{RelativePath: c.RelativePath, RecoveryPath: c.RecoveryPath}
	if ctx == nil || ctx.Err() != nil {
		return result, Error("CANCELLED")
	}
	options, err := checkedOptions(Options{Exclusions: append([]string(nil), c.Exclusions...)})
	if err != nil {
		return result, err
	}
	relative, err := relativePath(c.RelativePath)
	if err != nil || relative != c.RelativePath || excluded(relative, options.Exclusions) || c.AfterHash != hash(c.AfterContent) || len(c.AfterContent) > MaxFileBytes || !utf8.ValidString(c.AfterContent) || strings.ContainsRune(c.AfterContent, 0) || c.Diff != exactDiff(c) {
		return result, Error("INVALID_CHANGE")
	}
	if c.Operation != "create" && c.Operation != "replace" {
		return result, Error("INVALID_CHANGE")
	}
	if c.Operation == "replace" {
		recovery, e := relativePath(c.RecoveryPath)
		if e != nil || path.Dir(recovery) != path.Dir(relative) || !(strings.HasPrefix(path.Base(recovery), ".asimi-") || strings.HasPrefix(path.Base(recovery), ".deskpilot-")) || !strings.HasSuffix(recovery, ".recovery") || c.BeforeHash != hash(c.BeforeContent) || len(c.BeforeContent) > MaxFileBytes || c.BeforeIdentity == "" {
			return result, Error("INVALID_CHANGE")
		}
	} else if c.BeforeContent != "" || c.BeforeHash != "" || c.BeforeIdentity != "" || c.RecoveryPath != "" {
		return result, Error("INVALID_CHANGE")
	}
	w, err := openWorkspace(root, true)
	if err != nil {
		return result, err
	}
	defer w.Close()
	if !strings.EqualFold(w.path, c.Root) || w.identity != c.RootIdentity {
		return result, Error("STALE_WORKSPACE")
	}
	parent, name, err := w.parent(relative, true)
	if err != nil {
		return result, err
	}
	defer parent.Close()
	if parent.identity != c.ParentIdentity {
		return result, Error("STALE_TARGET")
	}
	return w.commit(ctx, parent, name, c)
}

func fileKind(name string) (string, string) {
	switch strings.ToLower(path.Ext(name)) {
	case ".go":
		return "source", "Go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "source", "JavaScript"
	case ".ts", ".tsx":
		return "source", "TypeScript"
	case ".vue":
		return "source", "Vue"
	case ".py":
		return "source", "Python"
	case ".java":
		return "source", "Java"
	case ".cs":
		return "source", "C#"
	case ".rs":
		return "source", "Rust"
	case ".c", ".h":
		return "source", "C"
	case ".cpp", ".hpp":
		return "source", "C++"
	case ".json":
		return "configuration", "JSON"
	case ".yaml", ".yml":
		return "configuration", "YAML"
	case ".md":
		return "document", "Markdown"
	case ".html":
		return "source", "HTML"
	case ".css", ".scss":
		return "source", "CSS"
	case ".xml", ".csproj":
		return "configuration", "XML"
	case ".toml":
		return "configuration", "TOML"
	default:
		return "text", "Text"
	}
}
