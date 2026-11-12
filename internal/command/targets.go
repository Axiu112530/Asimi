package command

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var targetPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_. -]*$`)

type projectTarget struct {
	absolute  string
	directory bool
}

// commandTargets converts user paths to fixed relative argv entries. The only
// wildcard syntax is Go's terminal package pattern.
func commandTargets(id string, args []string, dir string) ([]string, []projectTarget, error) {
	minArgs, maxArgs := 0, 0
	var extensions []string
	allowDirectory, goPackages := false, false
	prefix := ""
	switch id {
	case "go.test", "go.vet", "go.build":
		maxArgs, allowDirectory, goPackages = 8, true, true
		if len(args) == 0 {
			args = []string{"./..."}
		}
	case "node.test":
		maxArgs, extensions = 8, []string{".js", ".mjs", ".cjs"}
	case "node.check":
		minArgs, maxArgs, extensions = 1, 1, []string{".js", ".mjs", ".cjs"}
	case "java.test":
		minArgs, maxArgs, extensions = 1, 1, []string{".java"}
	case "java.build":
		minArgs, maxArgs, extensions = 1, 8, []string{".java"}
	case "java.deps":
		minArgs, maxArgs, extensions = 1, 8, []string{".class", ".jar"}
	case "dotnet.test", "dotnet.build", "dotnet.deps":
		maxArgs, extensions = 1, []string{".csproj", ".fsproj", ".vbproj", ".sln", ".slnx"}
		if id == "dotnet.deps" {
			prefix = "--project"
		}
	case "python.pytest":
		maxArgs, allowDirectory, extensions = 8, true, []string{".py"}
	case "python.unittest":
		maxArgs, allowDirectory, prefix = 1, true, "-s"
	case "python.check":
		minArgs, maxArgs, extensions = 1, 8, []string{".py"}
	}
	if len(args) < minArgs || len(args) > maxArgs {
		return nil, nil, Error("COMMAND_ARGUMENTS")
	}
	var argv []string
	var paths []projectTarget
	if prefix != "" && len(args) != 0 {
		argv = append(argv, prefix)
	}
	for _, arg := range args {
		rel, pattern, ok := relativeTarget(arg, goPackages)
		if !ok {
			return nil, nil, Error("COMMAND_ARGUMENTS")
		}
		candidate := filepath.Join(dir, filepath.FromSlash(rel))
		canonical, err := filepath.EvalSymlinks(candidate)
		if err != nil || !strings.EqualFold(canonical, candidate) {
			return nil, nil, Error("COMMAND_ARGUMENTS")
		}
		info, err := os.Lstat(candidate)
		if err != nil {
			return nil, nil, Error("COMMAND_ARGUMENTS")
		}
		if info.IsDir() {
			if !allowDirectory {
				return nil, nil, Error("COMMAND_ARGUMENTS")
			}
		} else if pattern || !info.Mode().IsRegular() || !slices.Contains(extensions, strings.ToLower(filepath.Ext(rel))) {
			return nil, nil, Error("COMMAND_ARGUMENTS")
		}
		paths = append(paths, projectTarget{absolute: candidate, directory: info.IsDir()})
		relativeArg := "./" + rel
		if rel == "." {
			relativeArg = "."
		}
		if pattern {
			relativeArg += "/..."
		}
		argv = append(argv, relativeArg)
	}
	return argv, paths, nil
}

func relativeTarget(input string, goPackages bool) (relative string, pattern, ok bool) {
	if len(input) > 240 || input == "" {
		return "", false, false
	}
	relative = strings.TrimPrefix(input, "./")
	if input == "./..." && goPackages {
		return ".", true, true
	}
	if goPackages && strings.HasSuffix(relative, "/...") {
		relative = strings.TrimSuffix(relative, "/...")
		pattern = true
	}
	if relative == "." {
		return relative, pattern, true
	}
	for _, part := range strings.Split(relative, "/") {
		if !targetPattern.MatchString(part) || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return "", false, false
		}
		base, _, _ := strings.Cut(strings.ToUpper(part), ".")
		base = strings.TrimRight(base, " ")
		switch base {
		case "NUL", "AUX", "PRN", "CON":
			return "", false, false
		}
		if len(base) == 4 && base[3] >= '0' && base[3] <= '9' && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
			return "", false, false
		}
	}
	return relative, pattern, true
}

func findSpec(id string) (Spec, bool) {
	for _, spec := range Catalog() {
		if spec.ID == id {
			return spec, true
		}
	}
	return Spec{}, false
}

func approvedTargets(p Proposal) ([]projectTarget, error) {
	spec, ok := findSpec(p.ID)
	if !ok || spec.ExecutesCode != p.ExecutesCode || spec.Network && !p.Network || !localPath(p.Directory) {
		return nil, Error("COMMAND_CHANGED")
	}
	exe, script, err := resolve(spec.Program, p.Directory)
	if err != nil || exe != p.Executable {
		return nil, Error("COMMAND_CHANGED")
	}
	args := p.Args
	if script != "" {
		if p.ScriptHash == "" || len(args) == 0 || args[0] != script {
			return nil, Error("COMMAND_CHANGED")
		}
		args = args[1:]
	} else if p.ScriptHash != "" {
		return nil, Error("COMMAND_CHANGED")
	}
	if len(args) < len(spec.Args) || !slices.Equal(args[:len(spec.Args)], spec.Args) {
		return nil, Error("COMMAND_CHANGED")
	}
	targetArgs := args[len(spec.Args):]
	input := targetArgs
	if len(input) > 0 && (p.ID == "dotnet.deps" || p.ID == "python.unittest") {
		input = input[1:]
	}
	canonical, refs, err := commandTargets(p.ID, input, p.Directory)
	if err != nil || !slices.Equal(canonical, targetArgs) {
		return nil, Error("COMMAND_CHANGED")
	}
	return refs, nil
}
