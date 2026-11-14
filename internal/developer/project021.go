package developer

import (
	"context"
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type ProjectInspection struct {
	Summary ProjectSummary `json:"summary"`
	Git     GitContext     `json:"git"`
}

func InspectProject(root string, exclusions []string) (ProjectInspection, error) {
	options, err := checkedOptions(Options{Exclusions: slices.Clone(exclusions)})
	if err != nil {
		return ProjectInspection{}, err
	}
	w, err := openWorkspace(root, false)
	if err != nil {
		return ProjectInspection{}, err
	}
	defer w.Close()
	return ProjectInspection{
		Summary: buildProjectSummary(context.Background(), w, "", nil, options.Exclusions, false),
		Git:     inspectGit(w, options.Exclusions),
	}, nil
}

func (s *ProjectSummary) recognizePath(name string) {
	base := strings.ToLower(path.Base(name))
	switch base {
	case "go.mod", "go.work":
		s.Managers = appendBounded(s.Managers, "Go Modules", 12)
	case "package.json", "package-lock.json", "npm-shrinkwrap.json":
		s.Managers = appendBounded(s.Managers, "npm", 12)
	case "pom.xml":
		s.Managers = appendBounded(s.Managers, "Maven", 12)
	case "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts":
		s.Managers = appendBounded(s.Managers, "Gradle", 12)
	case "requirements.txt", "setup.py", "setup.cfg", "pyproject.toml":
		s.Managers = appendBounded(s.Managers, "pip", 12)
	case "pipfile":
		s.Managers = appendBounded(s.Managers, "Pipenv", 12)
	}
	switch strings.ToLower(path.Ext(base)) {
	case ".csproj", ".fsproj", ".vbproj", ".sln", ".slnx":
		s.Managers = appendBounded(s.Managers, "NuGet", 12)
	}
	switch base {
	case "main.go", "main.py", "__main__.py", "manage.py", "program.cs", "main.java", "main.js", "index.js", "main.ts", "index.ts", "app.py":
		s.EntryPoints = appendBounded(s.EntryPoints, name, 16)
	case "pytest.ini", "conftest.py":
		s.Pytest = true
	}
	if strings.HasSuffix(base, "_test.go") || strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") ||
		strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") || strings.HasSuffix(base, "test.java") {
		s.TestPaths = appendBounded(s.TestPaths, name, 16)
	}
}

func (s *ProjectSummary) recognizeFramework(dependency string) {
	name := strings.ToLower(dependency)
	for _, framework := range []struct{ prefix, name string }{
		{"vue", "Vue"}, {"react", "React"}, {"next", "Next.js"}, {"express", "Express"},
		{"@angular/core", "Angular"}, {"django", "Django"}, {"flask", "Flask"}, {"fastapi", "FastAPI"},
		{"pytest", "pytest"}, {"org.springframework.boot:", "Spring Boot"}, {"org.junit", "JUnit"},
		{"xunit", "xUnit"}, {"microsoft.aspnetcore", "ASP.NET Core"},
		{"github.com/gin-gonic/gin", "Gin"}, {"github.com/gofiber/fiber", "Fiber"},
	} {
		if name == framework.prefix || strings.HasSuffix(framework.prefix, ":") && strings.HasPrefix(name, framework.prefix) ||
			strings.HasPrefix(name, framework.prefix+".") || strings.HasPrefix(name, framework.prefix+"/") {
			s.Frameworks = appendBounded(s.Frameworks, framework.name, 16)
		}
	}
	if name == "pytest" {
		s.Pytest = true
	}
}

var projectRuntimeRequirements = []struct {
	manifest, runtime string
	pattern           *regexp.Regexp
}{
	{"go.mod", "go", regexp.MustCompile(`(?m)^go[\t ]+([0-9]+(?:\.[0-9]+){1,2})[\t ]*(?://[^\n]*)?\r?$`)},
	{"go.work", "go", regexp.MustCompile(`(?m)^go[\t ]+([0-9]+(?:\.[0-9]+){1,2})[\t ]*(?://[^\n]*)?\r?$`)},
	{"pom.xml", "java", regexp.MustCompile(`<(?:java\.version|maven\.compiler\.release)>\s*(\d+(?:\.\d+)?)\s*</`)},
	{"build.gradle", "java", regexp.MustCompile(`JavaLanguageVersion\.of\(\s*(\d+)\s*\)`)},
	{"build.gradle.kts", "java", regexp.MustCompile(`JavaLanguageVersion\.of\(\s*(\d+)\s*\)`)},
	{"pyproject.toml", "python", regexp.MustCompile(`(?m)^\s*requires-python\s*=\s*["']\s*>=\s*(\d+(?:\.\d+){0,2})\s*["']`)},
}

var dotnetFramework = regexp.MustCompile(`<TargetFrameworks?>\s*([^<]+)</TargetFrameworks?>`)
var dotnetVersion = regexp.MustCompile(`^net((?:[5-9]|[1-9]\d+)\.\d+)(?:-.*)?$`)

func (s *ProjectSummary) recognizeManifest(name, content string) {
	base := strings.ToLower(path.Base(name))
	for _, rule := range projectRuntimeRequirements {
		if rule.manifest == base {
			if match := rule.pattern.FindStringSubmatch(content); match != nil {
				s.setMinimum(rule.runtime, match[1])
			}
		}
	}
	if base == "pyproject.toml" && strings.Contains(content, "[tool.pytest.ini_options]") {
		s.Pytest = true
	}
	switch strings.ToLower(path.Ext(base)) {
	case ".csproj", ".fsproj", ".vbproj":
		if frameworks := dotnetFramework.FindStringSubmatch(content); frameworks != nil {
			for _, framework := range strings.Split(frameworks[1], ";") {
				if match := dotnetVersion.FindStringSubmatch(strings.TrimSpace(framework)); match != nil {
					s.setMinimum("dotnet", match[1])
				}
			}
		}
	}
	if base != "package.json" {
		return
	}
	var manifest struct {
		Engines map[string]string `json:"engines"`
		Main    string            `json:"main"`
	}
	if json.Unmarshal([]byte(content), &manifest) != nil {
		return
	}
	main := strings.TrimPrefix(manifest.Main, "./")
	if main != "" && gitSafePath(main) {
		if candidate := path.Join(path.Dir(name), main); AgentContextPathAllowed(candidate) {
			s.EntryPoints = appendBounded(s.EntryPoints, candidate, 16)
		}
	}
	for _, runtime := range []string{"node", "npm"} {
		value := strings.TrimSpace(manifest.Engines[runtime])
		if minimum, ok := strings.CutPrefix(value, ">="); ok {
			s.setMinimum(runtime, strings.TrimSpace(minimum))
		}
	}
}

func (s *ProjectSummary) setMinimum(runtime, value string) {
	parse := func(version string) ([3]int, bool) {
		var parts [3]int
		fields := strings.Split(version, ".")
		if len(fields) > len(parts) {
			return parts, false
		}
		for i, field := range fields {
			n, err := strconv.Atoi(field)
			if err != nil || n < 0 || n > 9999 || len(field) > 4 {
				return parts, false
			}
			parts[i] = n
		}
		return parts, true
	}
	candidate, ok := parse(value)
	if !ok {
		return
	}
	current, _ := parse(s.RuntimeRequirements[runtime])
	if s.RuntimeRequirements[runtime] == "" || slices.Compare(candidate[:], current[:]) > 0 {
		s.RuntimeRequirements[runtime] = value
	}
}
