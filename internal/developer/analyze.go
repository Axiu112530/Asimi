package developer

import (
	"bufio"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strings"
)

func Analyze(root, query string) (Report, error) { return AnalyzeWithOptions(root, query, Options{}) }
func AnalyzeWithOptions(root, query string, options Options) (Report, error) {
	options, err := checkedOptions(options)
	if err != nil {
		return Report{}, err
	}
	if len(query) > 512 {
		return Report{}, Error("QUERY_TOO_LARGE")
	}
	w, err := openWorkspace(root, false)
	if err != nil {
		return Report{}, err
	}
	defer w.Close()
	report := Report{Root: w.path, Entries: []Entry{}, Languages: []Language{}, Dependencies: []Dependency{}, Matches: []Match{}, Symbols: []Symbol{}, Calls: []Call{}, Warnings: []string{"Symbol and call analysis is local and lightweight; non-Go results are lexical hints, not resolved references."}}
	languages := map[string]int{}
	visited := 0
	bytesRead := 0
	query = strings.ToLower(query)
	var scan func(string, int)
	scan = func(directory string, depth int) {
		remaining := options.MaxFiles - visited
		if remaining <= 0 {
			report.Truncated = true
			return
		}
		entries, truncated, e := w.list(directory, remaining)
		if e != nil {
			report.Warnings = appendBounded(report.Warnings, "Skipped inaccessible or unsafe directory: "+directory, 30)
			return
		}
		if truncated {
			report.Truncated = true
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
		for _, entry := range entries {
			if visited >= options.MaxFiles {
				report.Truncated = true
				return
			}
			visited++
			relative := path.Join(directory, entry.name)
			if entry.unsafe || excluded(relative, options.Exclusions) || strings.HasPrefix(strings.ToLower(entry.name), ".deskpilot-") {
				continue
			}
			if entry.directory {
				report.Entries = append(report.Entries, Entry{Path: relative, Type: "directory"})
				if depth < options.MaxDepth {
					scan(relative, depth+1)
				} else {
					report.Truncated = true
				}
				continue
			}
			kind, language := fileKind(relative)
			report.Entries = append(report.Entries, Entry{Path: relative, Type: kind, Language: language, Bytes: entry.bytes})
			languages[language]++
			if entry.bytes > MaxFileBytes || int64(bytesRead)+entry.bytes > int64(options.MaxBytes) {
				report.Truncated = true
				continue
			}
			if bytesRead >= options.MaxBytes {
				report.Truncated = true
				continue
			}
			f, e := w.snapshotBounded(relative, options.MaxBytes-bytesRead)
			bytesRead += int(f.Bytes)
			if e != nil {
				continue
			}
			if query != "" {
				for i, line := range strings.Split(f.Content, "\n") {
					if strings.Contains(strings.ToLower(line), query) || i == 0 && strings.Contains(strings.ToLower(relative), query) {
						if len(report.Matches) >= MaxMatches {
							report.Truncated = true
							break
						}
						if len(line) > 2048 {
							line = line[:2048]
						}
						report.Matches = append(report.Matches, Match{Path: relative, Line: i + 1, Text: line})
					}
				}
			}
			dependencies := dependencies(relative, f.Content)
			for _, dependency := range dependencies {
				if len(report.Dependencies) >= 500 {
					report.Truncated = true
					break
				}
				report.Dependencies = append(report.Dependencies, dependency)
			}
			symbols, calls := understand(relative, f.Content, language)
			if len(symbols)+len(report.Symbols) > 1000 {
				symbols = symbols[:max(0, 1000-len(report.Symbols))]
				report.Truncated = true
			}
			if len(calls)+len(report.Calls) > 2000 {
				calls = calls[:max(0, 2000-len(report.Calls))]
				report.Truncated = true
			}
			report.Symbols = append(report.Symbols, symbols...)
			report.Calls = append(report.Calls, calls...)
		}
	}
	scan("", 1)
	report.BytesRead = int64(bytesRead)
	report.FilesVisited = visited
	for language, count := range languages {
		report.Languages = append(report.Languages, Language{Name: language, Files: count})
	}
	sort.Slice(report.Languages, func(i, j int) bool { return report.Languages[i].Name < report.Languages[j].Name })
	return report, nil
}
func appendBounded(values []string, value string, limit int) []string {
	if len(values) < limit {
		return append(values, value)
	}
	return values
}

func dependencies(name, content string) []Dependency {
	result := []Dependency{}
	manifest := path.Base(name)
	if manifest == "package.json" {
		var pkg struct {
			Dependencies map[string]string `json:"dependencies"`
			Dev          map[string]string `json:"devDependencies"`
			Peer         map[string]string `json:"peerDependencies"`
		}
		if json.Unmarshal([]byte(content), &pkg) == nil {
			for _, group := range []struct {
				kind  string
				items map[string]string
			}{{"runtime", pkg.Dependencies}, {"development", pkg.Dev}, {"peer", pkg.Peer}} {
				keys := make([]string, 0, len(group.items))
				for key := range group.items {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					if len(result) >= 500 {
						break
					}
					result = append(result, Dependency{Name: key, Version: group.items[key], Manifest: name, Kind: group.kind})
				}
			}
		}
	} else if manifest == "go.mod" {
		inRequire := false
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			line := strings.TrimSpace(strings.SplitN(scanner.Text(), "//", 2)[0])
			if strings.HasPrefix(line, "require (") {
				inRequire = true
				continue
			}
			if line == ")" {
				inRequire = false
				continue
			}
			if strings.HasPrefix(line, "require ") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
			} else if !inRequire {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 && len(result) < 500 {
				result = append(result, Dependency{Name: fields[0], Version: fields[1], Manifest: name, Kind: "module"})
			}
		}
	} else if manifest == "requirements.txt" {
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
			if line == "" || strings.HasPrefix(line, "-") {
				continue
			}
			fields := requirement.FindStringSubmatch(line)
			if len(fields) > 1 && len(result) < 500 {
				result = append(result, Dependency{Name: fields[1], Version: strings.TrimSpace(strings.TrimPrefix(line, fields[1])), Manifest: name, Kind: "runtime"})
			}
		}
	} else if manifest == "Cargo.toml" {
		group := ""
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "[") {
				group = line
				continue
			}
			if group != "[dependencies]" && group != "[dev-dependencies]" {
				continue
			}
			fields := strings.SplitN(line, "=", 2)
			if len(fields) == 2 && len(result) < 500 {
				result = append(result, Dependency{Name: strings.TrimSpace(fields[0]), Version: strings.Trim(strings.TrimSpace(fields[1]), `"`), Manifest: name, Kind: strings.Trim(group, "[]")})
			}
		}
	}
	return result
}

var requirement = regexp.MustCompile(`^([A-Za-z0-9_.-]+)`)
var declaration = regexp.MustCompile(`^\s*(?:export\s+)?(?:async\s+)?(?:function|class|def|fn|interface)\s+([A-Za-z_$][\w$]*)|^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=.*=>`)
var callHint = regexp.MustCompile(`([A-Za-z_$][\w$.]*)\s*\(`)

func understand(name, content, language string) ([]Symbol, []Call) {
	symbols := []Symbol{}
	calls := []Call{}
	if language == "Go" {
		positions := token.NewFileSet()
		parsed, err := parser.ParseFile(positions, name, content, 0)
		if err != nil {
			return symbols, calls
		}
		for _, decl := range parsed.Decls {
			switch node := decl.(type) {
			case *ast.FuncDecl:
				symbols = append(symbols, Symbol{Name: node.Name.Name, Path: name, Line: positions.Position(node.Pos()).Line, Kind: "function"})
				if node.Body != nil {
					ast.Inspect(node.Body, func(n ast.Node) bool {
						if call, ok := n.(*ast.CallExpr); ok {
							callee := ""
							switch f := call.Fun.(type) {
							case *ast.Ident:
								callee = f.Name
							case *ast.SelectorExpr:
								callee = f.Sel.Name
								if x, ok := f.X.(*ast.Ident); ok {
									callee = x.Name + "." + callee
								}
							}
							if callee != "" && len(calls) < 2000 {
								calls = append(calls, Call{Caller: node.Name.Name, Callee: callee, Path: name, Line: positions.Position(call.Pos()).Line})
							}
						}
						return len(calls) < 2000
					})
				}
			case *ast.GenDecl:
				for _, spec := range node.Specs {
					if typ, ok := spec.(*ast.TypeSpec); ok && len(symbols) < 1000 {
						symbols = append(symbols, Symbol{Name: typ.Name.Name, Path: name, Line: positions.Position(typ.Pos()).Line, Kind: "type"})
					}
				}
			}
			if len(symbols) >= 1000 {
				break
			}
		}
		return symbols, calls
	}
	if language != "JavaScript" && language != "TypeScript" && language != "Vue" && language != "Python" && language != "Rust" {
		return symbols, calls
	}
	caller := "(module)"
	for i, line := range strings.Split(content, "\n") {
		if found := declaration.FindStringSubmatch(line); len(found) > 1 {
			caller = found[1]
			if caller == "" {
				caller = found[2]
			}
			if len(symbols) < 1000 {
				symbols = append(symbols, Symbol{Name: caller, Path: name, Line: i + 1, Kind: "lexical declaration"})
			}
		}
		for _, found := range callHint.FindAllStringSubmatch(line, 64) {
			callee := found[1]
			if callee == caller || callee == "if" || callee == "for" || callee == "while" || callee == "switch" {
				continue
			}
			if len(calls) < 2000 {
				calls = append(calls, Call{Caller: caller, Callee: callee, Path: name, Line: i + 1})
			}
		}
	}
	return symbols, calls
}
