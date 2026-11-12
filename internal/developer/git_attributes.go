package developer

import (
	"encoding/hex"
	"path"
	"strings"
)

type gitText uint8

const (
	gitTextDefault gitText = iota
	gitTextNever
	gitTextAuto
	gitTextAlways
)

type gitAttributeRule struct {
	path gitIgnoreRule
	text gitText
	eol  bool
}

type gitAttributeIndexEntry struct {
	name string
	file gitIndexEntry
}

// Git attributes may activate external conversion and encoding. Only ordinary
// line-ending declarations have semantics in this reader.
func parseGitAttributes(base string, data []byte, ignoreCase bool) ([]gitAttributeRule, error) {
	var out []gitAttributeRule
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) < 2 || len(out) >= 512 || strings.HasPrefix(fields[0], "[attr]") ||
			strings.ContainsAny(fields[0], "\"\\!") || strings.HasSuffix(fields[0], "/") {
			return nil, errGitUnsupported
		}
		patterns, err := parseGitIgnore(base, []byte(fields[0]), ignoreCase)
		if err != nil || len(patterns) != 1 {
			return nil, errGitUnsupported
		}
		rule := gitAttributeRule{path: patterns[0]}
		for _, field := range fields[1:] {
			switch field {
			case "text":
				rule.text = gitTextAlways
			case "text=auto":
				rule.text = gitTextAuto
			case "-text", "binary":
				rule.text = gitTextNever
			case "eol=lf", "eol=crlf":
				rule.eol = true
			default:
				return nil, errGitUnsupported
			}
		}
		out = append(out, rule)
	}
	return out, nil
}

func (s *gitScan) gitTextMode(name string) (gitText, error) {
	text, eol := gitTextDefault, false
	var parents []string
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		parents = append(parents, parent)
	}
	parents = append(parents, "")
	for i := len(parents) - 1; i >= 0; i-- {
		base := parents[i]
		rules, cached := s.attributes[base]
		if !cached {
			data, err := s.readGitAttributes(path.Join(base, ".gitattributes"))
			if err != nil {
				return gitTextDefault, err
			}
			rules, err = parseGitAttributes(base, data, s.config.ignoreCase)
			if err != nil {
				return gitTextDefault, err
			}
			s.attributes[base] = rules
		}
		for _, rule := range rules {
			if gitIgnored([]gitIgnoreRule{rule.path}, name, false) {
				if rule.text != gitTextDefault {
					text = rule.text
				}
				eol = eol || rule.eol
			}
		}
	}
	if text == gitTextDefault {
		if eol {
			return gitTextAlways, nil
		}
		if s.config.normalizeCRLF {
			return gitTextAuto, nil
		}
	}
	return text, nil
}

func (s *gitScan) readGitAttributes(name string) ([]byte, error) {
	data, err := s.metadataFile(name, gitMaxConfig)
	if err != nil || data != nil {
		return data, err
	}
	indexed, ok := s.indexAttrs[strings.ToLower(name)]
	if !ok {
		return nil, nil
	}
	// Git falls back to the indexed copy if the worktree file is absent.
	// A case alias or a non-regular index entry has ambiguous semantics.
	if indexed.name != name || indexed.file.mode != 0100644 && indexed.file.mode != 0100755 {
		return nil, errGitUnsupported
	}
	kind, content, err := s.objects.object(hex.EncodeToString(indexed.file.oid[:]), 0)
	if err != nil {
		return nil, err
	}
	if kind != "blob" || len(content) > gitMaxConfig {
		return nil, errGitUnsupported
	}
	return content, nil
}
