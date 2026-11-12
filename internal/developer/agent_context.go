package developer

import (
	"encoding/xml"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	agentMaxFiles       = 12
	agentMaxFileBytes   = 16 << 10
	agentMaxSourceBytes = 48 << 10
	agentMaxVisited     = 512
	agentMaxCandidates  = 24
	agentMaxManifest    = 32 << 10
)

type AgentContext struct {
	Objective string             `json:"objective"`
	Project   ProjectSummary     `json:"project"`
	Git       GitContext         `json:"git"`
	Files     []AgentContextFile `json:"files"`
	Omitted   []string           `json:"omitted"`
}

type AgentContextFile struct {
	Path      string   `json:"path"`
	Content   string   `json:"content"`
	SHA256    string   `json:"sha256"`
	Editable  bool     `json:"editable"`
	Truncated bool     `json:"truncated"`
	Redacted  bool     `json:"redacted"`
	Symbols   []string `json:"symbols"`
	GitDirty  bool     `json:"gitDirty,omitempty"`
}

type ProjectSummary struct {
	Types               []string          `json:"types"`
	Manifests           []string          `json:"manifests"`
	Dependencies        []string          `json:"dependencies"`
	CandidatePaths      []string          `json:"candidatePaths"`
	TestGuidance        []string          `json:"testGuidance"`
	Languages           []string          `json:"languages,omitempty"`
	Frameworks          []string          `json:"frameworks,omitempty"`
	Managers            []string          `json:"managers,omitempty"`
	EntryPoints         []string          `json:"entryPoints,omitempty"`
	TestPaths           []string          `json:"testPaths,omitempty"`
	RuntimeRequirements map[string]string `json:"runtimeRequirements,omitempty"`
	Pytest              bool              `json:"pytest,omitempty"`
	Truncated           bool              `json:"truncated"`
}

// BuildAgentContext reads selected files through the same native workspace
// handles as Developer reads. The returned DTO is suitable for scope review
// before sending to a provider and contains no root or native identities.
func BuildAgentContext(root, objective string, selectedPaths []string) (AgentContext, error) {
	return BuildAgentContextWithExclusions(root, objective, selectedPaths, nil)
}

func BuildAgentContextWithExclusions(root, objective string, selectedPaths, exclusions []string) (AgentContext, error) {
	options, err := checkedOptions(Options{Exclusions: slices.Clone(exclusions)})
	if err != nil {
		return AgentContext{}, err
	}
	w, err := openWorkspace(root, false)
	if err != nil {
		return AgentContext{}, err
	}
	defer w.Close()
	removeRoot := rootRedactor(w.path)
	git := inspectGit(w, options.Exclusions)
	context := AgentContext{
		Objective: agentPrefix(RedactAgentText(removeRoot(objective)), 4096),
		Git:       git.ForAgent(),
		Files:     []AgentContextFile{},
		Omitted:   []string{},
	}
	seen := map[string]bool{}
	remaining := agentMaxSourceBytes
	for i, input := range selectedPaths {
		if i >= agentMaxFiles {
			context.Omitted = append(context.Omitted, "Additional selections exceeded the file limit.")
			break
		}
		name, err := relativePath(input)
		if err != nil || agentExcluded(name) {
			context.Omitted = append(context.Omitted, "A selection was unsafe or matched a sensitive path.")
			continue
		}
		if excluded(name, options.Exclusions) {
			context.Omitted = append(context.Omitted, "A selection matched a workspace exclusion.")
			continue
		}
		if seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		if remaining == 0 {
			context.Omitted = append(context.Omitted, "Additional selections exceeded the source byte limit.")
			break
		}
		f, err := w.snapshot(name)
		if err != nil {
			context.Omitted = append(context.Omitted, "A selection could not be read as bounded UTF-8 text.")
			continue
		}
		content := RedactAgentText(removeRoot(f.Content))
		snippet := agentPrefix(content, min(remaining, agentMaxFileBytes))
		remaining -= len(snippet)
		redacted, truncated := content != f.Content, len(snippet) != len(content)
		file := AgentContextFile{Path: name, Content: snippet, SHA256: f.Hash, Editable: !redacted && !truncated, Redacted: redacted, Truncated: truncated, Symbols: []string{}, GitDirty: git.DirtyPath(name)}
		symbols, _ := understand(name, content, f.Language)
		for _, symbol := range symbols {
			if len(symbol.Name) <= 80 && RedactAgentText(symbol.Name) == symbol.Name {
				file.Symbols = appendBounded(file.Symbols, symbol.Name, 12)
			}
		}
		context.Files = append(context.Files, file)
	}
	context.Project = buildProjectSummary(w, context.Objective, seen, options.Exclusions, false)
	return context, nil
}

// SuggestAgentPaths ranks source and test files within the same bounded scope
// used for review. Suggestions contain paths whose complete text fits the
// review budget and passes the existing redaction checks.
func SuggestAgentPaths(root, objective string, exclusions []string) ([]string, error) {
	options, err := checkedOptions(Options{Exclusions: slices.Clone(exclusions)})
	if err != nil {
		return nil, err
	}
	w, err := openWorkspace(root, false)
	if err != nil {
		return nil, err
	}
	defer w.Close()
	removeRoot := rootRedactor(w.path)
	objective = agentPrefix(RedactAgentText(removeRoot(objective)), 4096)
	summary := buildProjectSummary(w, objective, nil, options.Exclusions, true)
	suggestions := make([]string, 0, 3)
	for _, name := range summary.CandidatePaths {
		file, err := w.snapshotBounded(name, agentMaxFileBytes)
		if err != nil || file.Content == "" || RedactAgentText(removeRoot(file.Content)) != file.Content {
			continue
		}
		suggestions = append(suggestions, name)
		if len(suggestions) == cap(suggestions) {
			break
		}
	}
	return suggestions, nil
}

func agentPrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// AgentContextPathAllowed applies the same relative-path and sensitivity
// checks to metadata produced by local Agent searches.
func AgentContextPathAllowed(relative string) bool {
	name, err := relativePath(relative)
	return err == nil && !agentExcluded(name)
}

var shortWindowsAlias = regexp.MustCompile(`~[0-9]+(?:\.|$)`)

func agentExcluded(name string) bool {
	if excluded(name, nil) || RedactAgentText(name) != name || len(name) > 240 {
		return true
	}
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		switch part {
		case "secrets", "credentials", "private", "vendor", "target", "coverage", "__pycache__", "venv", "id_rsa", "id_ed25519", "kubeconfig":
			return true
		}
		if strings.HasPrefix(part, ".") && part != ".github" ||
			strings.HasPrefix(part, "secrets.") || strings.HasPrefix(part, "credentials.") ||
			strings.HasSuffix(part, ".p12") || strings.HasSuffix(part, ".jks") || strings.HasSuffix(part, ".kdbx") ||
			shortWindowsAlias.MatchString(part) {
			return true
		}
	}
	return false
}

type agentCandidate struct {
	name string
	rank int
}

type agentProjectKind struct {
	name string
	test string
}

func projectKind(name string) agentProjectKind {
	switch strings.ToLower(path.Base(name)) {
	case "go.mod", "go.work":
		return agentProjectKind{"Go", "go.test"}
	case "package.json":
		return agentProjectKind{"Node.js", "node.test"}
	case "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts":
		return agentProjectKind{"Java", "java.test"}
	case "pyproject.toml", "requirements.txt", "setup.py", "setup.cfg", "pipfile":
		return agentProjectKind{"Python", "python.unittest / python.pytest"}
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".csproj", ".fsproj", ".vbproj", ".sln", ".slnx":
		return agentProjectKind{".NET", "dotnet.test"}
	}
	return agentProjectKind{}
}

var (
	agentTerms   = regexp.MustCompile(`[[:alnum:]_]{3,}`)
	agentDepName = regexp.MustCompile(`^[[:alnum:]@][[:alnum:]@._:/-]{0,159}$`)
)

func buildProjectSummary(w *workspace, objective string, selected map[string]bool, exclusions []string, sourceOnly bool) ProjectSummary {
	summary := ProjectSummary{
		Types: []string{}, Manifests: []string{}, Dependencies: []string{},
		CandidatePaths: []string{}, TestGuidance: []string{},
		RuntimeRequirements: map[string]string{},
	}
	terms := agentTerms.FindAllString(strings.ToLower(objective), 24)
	kinds := map[agentProjectKind]bool{}
	depNames := map[string]bool{}
	var candidates []agentCandidate
	visited, manifestBytes := 0, agentMaxSourceBytes
	queue := []string{""}
	for len(queue) > 0 && visited < agentMaxVisited {
		dir := queue[0]
		queue = queue[1:]
		entries, truncated, err := w.list(dir, agentMaxVisited-visited)
		summary.Truncated = summary.Truncated || truncated || err != nil
		if err != nil {
			continue
		}
		visited += len(entries)
		slices.SortFunc(entries, func(a, b nativeEntry) int { return strings.Compare(a.name, b.name) })
		for _, entry := range entries {
			name := path.Join(dir, entry.name)
			if entry.unsafe || agentExcluded(name) || excluded(name, exclusions) {
				continue
			}
			if entry.directory {
				switch strings.ToLower(entry.name) {
				case "test", "tests", "__tests__", "spec", "specs":
					summary.TestPaths = appendBounded(summary.TestPaths, name, 16)
				}
				if strings.Count(name, "/") < 5 {
					queue = append(queue, name)
				} else {
					summary.Truncated = true
				}
				continue
			}
			kind := projectKind(name)
			summary.recognizePath(name)
			rank := rankAgentCandidate(name, terms, selected)
			if kind.name != "" {
				rank += 10
				kinds[kind] = true
				summary.Manifests = appendBounded(summary.Manifests, name, 16)
				if entry.bytes >= 0 && entry.bytes <= int64(min(agentMaxManifest, manifestBytes)) && manifestBytes > 0 {
					manifestBytes -= int(entry.bytes)
					if file, err := w.snapshotBounded(name, int(entry.bytes)); err == nil {
						summary.recognizeManifest(name, file.Content)
						for _, dependency := range agentDependencyNames(name, file.Content) {
							if len(depNames) < 30 && agentDepName.MatchString(dependency) && !strings.Contains(dependency, "://") && RedactAgentText(dependency) == dependency {
								depNames[dependency] = true
							}
						}
					}
				}
			}
			fileType, language := fileKind(name)
			if fileType == "source" {
				summary.Languages = appendBounded(summary.Languages, language, 16)
			}
			if fileType == "source" || !sourceOnly && (kind.name != "" || fileType == "document") {
				candidates = append(candidates, agentCandidate{name, rank})
			}
		}
	}
	summary.Truncated = summary.Truncated || len(queue) > 0 || len(candidates) > agentMaxCandidates
	slices.SortFunc(candidates, func(a, b agentCandidate) int {
		if a.rank != b.rank {
			return b.rank - a.rank
		}
		return strings.Compare(a.name, b.name)
	})
	for _, candidate := range candidates[:min(agentMaxCandidates, len(candidates))] {
		summary.CandidatePaths = append(summary.CandidatePaths, candidate.name)
	}
	for kind := range kinds {
		summary.Types = append(summary.Types, kind.name)
		summary.TestGuidance = append(summary.TestGuidance, kind.test)
	}
	for dependency := range depNames {
		summary.Dependencies = append(summary.Dependencies, dependency)
		summary.recognizeFramework(dependency)
	}
	slices.Sort(summary.Types)
	slices.Sort(summary.TestGuidance)
	slices.Sort(summary.Dependencies)
	slices.Sort(summary.Languages)
	slices.Sort(summary.Frameworks)
	slices.Sort(summary.Managers)
	slices.Sort(summary.EntryPoints)
	slices.Sort(summary.TestPaths)
	return summary
}

func rankAgentCandidate(name string, terms []string, selected map[string]bool) int {
	lower := strings.ToLower(name)
	rank := max(0, 5-strings.Count(lower, "/"))
	if selected[lower] {
		rank += 1000
	}
	for _, term := range terms {
		if strings.Contains(strings.ToLower(path.Base(lower)), term) {
			rank += 16
		} else if strings.Contains(lower, term) {
			rank += 4
		}
	}
	if strings.Contains(lower, "test") || strings.Contains(lower, "spec") {
		rank += 5
	}
	return rank
}

func agentDependencyNames(name, content string) []string {
	var out []string
	for _, dependency := range dependencies(strings.ToLower(name), content) {
		out = append(out, dependency.Name)
	}
	switch strings.ToLower(path.Base(name)) {
	case "pom.xml":
		var project struct {
			Dependencies []struct {
				Group    string `xml:"groupId"`
				Artifact string `xml:"artifactId"`
			} `xml:"dependencies>dependency"`
		}
		if xml.Unmarshal([]byte(content), &project) == nil {
			for _, d := range project.Dependencies {
				out = append(out, strings.TrimSpace(d.Group)+":"+strings.TrimSpace(d.Artifact))
			}
		}
	default:
		ext := strings.ToLower(path.Ext(name))
		if ext != ".csproj" && ext != ".fsproj" && ext != ".vbproj" {
			break
		}
		var project struct {
			Dependencies []struct {
				Name string `xml:"Include,attr"`
			} `xml:"ItemGroup>PackageReference"`
		}
		if xml.Unmarshal([]byte(content), &project) == nil {
			for _, d := range project.Dependencies {
				out = append(out, d.Name)
			}
		}
	}
	return out
}
