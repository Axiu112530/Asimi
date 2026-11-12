package developer

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type gitIgnoreRule struct {
	base     string
	pattern  *regexp.Regexp
	negative bool
	dirOnly  bool
}

func parseGitIgnore(base string, data []byte, ignoreCase bool) ([]gitIgnoreRule, error) {
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return nil, errGitUnsupported
	}
	var rules []gitIgnoreRule
	for raw := range strings.Lines(string(data)) {
		line := strings.TrimSuffix(raw, "\n")
		line = strings.TrimSuffix(line, "\r")
		for strings.HasSuffix(line, " ") && !strings.HasSuffix(line, "\\ ") {
			line = strings.TrimSuffix(line, " ")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := gitIgnoreRule{base: base, negative: strings.HasPrefix(line, "!"), dirOnly: strings.HasSuffix(line, "/")}
		line = strings.TrimSuffix(strings.TrimPrefix(line, "!"), "/")
		if line == "" || len(line) > 2048 || len(rules) >= 512 {
			return nil, errGitUnsupported
		}
		var expression strings.Builder
		if ignoreCase {
			expression.WriteString("(?i)")
		}
		expression.WriteByte('^')
		if !strings.Contains(line, "/") {
			expression.WriteString("(?:.*/)?")
		}
		line = strings.TrimPrefix(line, "/")
		for i := 0; i < len(line); {
			switch line[i] {
			case '\\':
				if i+1 == len(line) {
					return nil, errGitUnsupported
				}
				character, size := utf8.DecodeRuneInString(line[i+1:])
				expression.WriteString(regexp.QuoteMeta(string(character)))
				i += size + 1
			case '?':
				expression.WriteString("[^/]")
				i++
			case '*':
				end := i + 1
				for end < len(line) && line[end] == '*' {
					end++
				}
				if end-i == 2 && (i == 0 || line[i-1] == '/') && (end == len(line) || line[end] == '/') {
					if end == len(line) {
						expression.WriteString(".*")
					} else {
						expression.WriteString("(?:.*/)?")
						end++
					}
				} else {
					expression.WriteString("[^/]*")
				}
				i = end
			case '[':
				end := strings.IndexByte(line[i+1:], ']')
				if end <= 0 {
					return nil, errGitUnsupported
				}
				end += i + 1
				group := line[i+1 : end]
				if strings.ContainsAny(group, "\\/[") || strings.HasPrefix(group, "^") {
					return nil, errGitUnsupported
				}
				expression.WriteByte('[')
				if strings.HasPrefix(group, "!") {
					expression.WriteString("^/")
					group = strings.TrimPrefix(group, "!")
				}
				expression.WriteString(group)
				expression.WriteByte(']')
				i = end + 1
			default:
				character, size := utf8.DecodeRuneInString(line[i:])
				expression.WriteString(regexp.QuoteMeta(string(character)))
				i += size
			}
		}
		expression.WriteByte('$')
		var err error
		rule.pattern, err = regexp.Compile(expression.String())
		if err != nil {
			return nil, errGitUnsupported
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func gitIgnored(rules []gitIgnoreRule, name string, directory bool) bool {
	ignored := false
	for _, rule := range rules {
		if rule.dirOnly && !directory {
			continue
		}
		relative := name
		if rule.base != "" {
			var applies bool
			relative, applies = strings.CutPrefix(name, rule.base+"/")
			if !applies {
				continue
			}
		}
		if rule.pattern.MatchString(relative) {
			ignored = !rule.negative
		}
	}
	return ignored
}
