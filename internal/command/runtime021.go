package command

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"deskpilot/internal/developer"
)

type RuntimeDiagnostic struct {
	Component      string   `json:"component"`
	Status         string   `json:"status"`
	Executable     string   `json:"executable,omitempty"`
	Version        string   `json:"version,omitempty"`
	MinimumVersion string   `json:"minimumVersion,omitempty"`
	Source         string   `json:"source,omitempty"`
	Problem        string   `json:"problem,omitempty"`
	DownloadURL    string   `json:"downloadUrl,omitempty"`
	InstallPreview []string `json:"installPreview,omitempty"`
}

type runtimeCandidate struct{ executable, source string }

// InspectProjectRuntimes reads installation metadata. It never runs a process,
// imports a package, downloads a dependency, or evaluates a project script.
func InspectProjectRuntimes(root string, project developer.ProjectInspection) []RuntimeDiagnostic {
	types := project.Summary.Types
	required := map[string]bool{
		"go": slices.Contains(types, "Go"), "node": slices.Contains(types, "Node.js"),
		"npm": slices.Contains(types, "Node.js"), "java": slices.Contains(types, "Java"),
		"python": slices.Contains(types, "Python"), "pytest": project.Summary.Pytest,
		"dotnet": slices.Contains(types, ".NET"), "git": project.Git.Status != "NOT_REPOSITORY",
	}
	specs := map[string]Spec{}
	for _, s := range Catalog() {
		if !s.ExecutesCode {
			specs[s.Program] = s
		}
	}
	specs["pytest"] = Spec{DownloadURL: "https://docs.pytest.org/en/stable/getting-started.html", InstallPreview: []string{"python", "-m", "pip", "install", "pytest"}}
	out := make([]RuntimeDiagnostic, 0, len(required))
	for _, component := range []string{"go", "node", "npm", "java", "python", "pytest", "dotnet", "git"} {
		spec := specs[component]
		d := RuntimeDiagnostic{Component: component, Status: "NOT_REQUIRED", MinimumVersion: project.Summary.RuntimeRequirements[component], DownloadURL: spec.DownloadURL, InstallPreview: spec.InstallPreview}
		if required[component] {
			inspectRuntime(root, &d)
		}
		out = append(out, d)
	}
	return out
}

func inspectRuntime(root string, d *RuntimeDiagnostic) {
	d.Status, d.Problem = "MISSING", "INSTALL_FROM_OFFICIAL_SOURCE"
	program := d.Component
	switch program {
	case "npm":
		program = "node"
	case "pytest":
		program = "python"
	}
	for _, candidate := range runtimeCandidates(root, program) {
		found := false
		var unsupportedArchitecture bool
		err := developer.WithLocalMetadataFile(candidate.executable, func(file *os.File) error {
			found = true
			d.Executable, d.Source = candidate.executable, candidate.source
			d.Status, d.Problem = "ERROR", "METADATA_UNAVAILABLE"
			machine, err := readPEMachine(file)
			if err != nil {
				return err
			}
			if !compatiblePE(machine) {
				unsupportedArchitecture = true
				return nil
			}
			if d.Component == "go" {
				d.Version = readGoVersion(filepath.Dir(candidate.executable))
			} else if d.Component == "dotnet" || d.Component == "npm" || d.Component == "pytest" {
				d.Version = readRuntimePackage(d.Component, filepath.Dir(candidate.executable))
			} else {
				d.Version = windowsFileVersion(candidate.executable)
			}
			return nil
		})
		if !found {
			continue
		}
		if err != nil {
			return
		}
		if unsupportedArchitecture {
			d.Status, d.Problem = "INCOMPATIBLE", "ARCHITECTURE"
			return
		}
		if d.Version == "" {
			if d.Component == "pytest" || d.Component == "npm" || d.Component == "dotnet" {
				d.Status, d.Problem = "MISSING", "PACKAGE_UNAVAILABLE"
			}
			return
		}
		if compareRuntimeVersion(d.Version, d.MinimumVersion) < 0 {
			d.Status, d.Problem = "INCOMPATIBLE", "MINIMUM_VERSION"
			return
		}
		d.Status, d.Problem = "AVAILABLE", ""
		return
	}
}

func readGoVersion(bin string) string {
	var version string
	_ = developer.WithLocalMetadataFile(filepath.Join(filepath.Dir(bin), "VERSION"), func(file *os.File) error {
		line, _ := bufio.NewReader(io.LimitReader(file, 512)).ReadString('\n')
		version = numericVersion(strings.TrimPrefix(strings.TrimSpace(line), "go"))
		return nil
	})
	return version
}

func runtimeCandidates(root, program string) []runtimeCandidate {
	var candidates []runtimeCandidate
	seen := map[string]bool{}
	add := func(directory, source string) {
		if !localPath(directory) {
			return
		}
		directory = filepath.Clean(directory)
		relative, err := filepath.Rel(root, directory)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return
		}
		executable := filepath.Join(directory, program+".exe")
		// Store aliases can invoke Microsoft Store and are not installations.
		if strings.Contains(strings.ToLower(executable), `\microsoft\windowsapps\`) {
			return
		}
		key := strings.ToLower(executable)
		if !seen[key] {
			seen[key] = true
			candidates = append(candidates, runtimeCandidate{executable, source})
		}
	}
	for i, directory := range filepath.SplitList(os.Getenv("PATH")) {
		if i >= 128 {
			break
		}
		add(directory, "PATH")
	}
	for _, variable := range []struct{ program, name, relative string }{
		{"go", "GOROOT", "bin"}, {"java", "JAVA_HOME", "bin"}, {"dotnet", "DOTNET_ROOT", ""},
	} {
		if program == variable.program && os.Getenv(variable.name) != "" {
			add(filepath.Join(os.Getenv(variable.name), variable.relative), variable.name)
		}
	}
	for _, parent := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if parent == "" {
			continue
		}
		switch program {
		case "go":
			add(filepath.Join(parent, "Go", "bin"), "STANDARD")
		case "node":
			add(filepath.Join(parent, "nodejs"), "STANDARD")
		case "git":
			add(filepath.Join(parent, "Git", "cmd"), "STANDARD")
		case "dotnet":
			add(filepath.Join(parent, "dotnet"), "STANDARD")
		case "java":
			for _, directory := range []string{"Java", "Eclipse Adoptium", "Microsoft"} {
				runtimeSubdirectories(filepath.Join(parent, directory), "jdk", "bin", add)
			}
		case "python":
			runtimeSubdirectories(parent, "python", "", add)
		}
	}
	if program == "python" && os.Getenv("LOCALAPPDATA") != "" {
		runtimeSubdirectories(filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Python"), "python", "", add)
	}
	return candidates
}

func runtimeSubdirectories(root, prefix, suffix string, add func(string, string)) {
	names, _ := developer.ListLocalMetadataDirectories(root)
	for _, name := range names {
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			add(filepath.Join(root, name, suffix), "STANDARD")
		}
	}
}

func readPEMachine(file io.ReaderAt) (uint16, error) {
	var header [64]byte
	if _, err := file.ReadAt(header[:], 0); err != nil || string(header[:2]) != "MZ" {
		return 0, Error("RUNTIME_INVALID_EXECUTABLE")
	}
	offset := binary.LittleEndian.Uint32(header[60:])
	var pe [24]byte
	if offset < 64 || offset > 16<<20 {
		return 0, Error("RUNTIME_INVALID_EXECUTABLE")
	}
	if _, err := file.ReadAt(pe[:], int64(offset)); err != nil || string(pe[:4]) != "PE\x00\x00" ||
		binary.LittleEndian.Uint16(pe[22:])&0x2002 != 0x0002 {
		return 0, Error("RUNTIME_INVALID_EXECUTABLE")
	}
	sections := binary.LittleEndian.Uint16(pe[6:])
	optionalSize := binary.LittleEndian.Uint16(pe[20:])
	var optional [64]byte
	if sections < 1 || sections > 96 || optionalSize < uint16(len(optional)) || optionalSize > 4096 {
		return 0, Error("RUNTIME_INVALID_EXECUTABLE")
	}
	if _, err := file.ReadAt(optional[:], int64(offset)+24); err != nil {
		return 0, Error("RUNTIME_INVALID_EXECUTABLE")
	}
	magic := binary.LittleEndian.Uint16(optional[:])
	entryPoint, imageSize := binary.LittleEndian.Uint32(optional[16:]), binary.LittleEndian.Uint32(optional[56:])
	if magic != 0x010b && magic != 0x020b || entryPoint == 0 || entryPoint >= imageSize {
		return 0, Error("RUNTIME_INVALID_EXECUTABLE")
	}
	var lastSectionByte [1]byte
	if _, err := file.ReadAt(lastSectionByte[:], int64(offset)+24+int64(optionalSize)+40*int64(sections)-1); err != nil {
		return 0, Error("RUNTIME_INVALID_EXECUTABLE")
	}
	return binary.LittleEndian.Uint16(pe[4:]), nil
}

func compatiblePE(machine uint16) bool {
	switch runtime.GOARCH {
	case "amd64":
		return machine == 0x8664 || machine == 0x014c
	case "arm64":
		return machine == 0xaa64 || machine == 0x8664 || machine == 0x014c
	default:
		return machine == 0x014c
	}
}

func readRuntimePackage(component, root string) string {
	switch component {
	case "npm":
		base := filepath.Join(root, "node_modules", "npm")
		if developer.WithLocalMetadataFile(filepath.Join(base, "bin", "npm-cli.js"), func(*os.File) error { return nil }) != nil {
			return ""
		}
		var pkg struct{ Name, Version string }
		if developer.WithLocalMetadataFile(filepath.Join(base, "package.json"), func(file *os.File) error {
			return json.NewDecoder(io.LimitReader(file, 128<<10)).Decode(&pkg)
		}) == nil && pkg.Name == "npm" {
			return numericVersion(pkg.Version)
		}
	case "dotnet", "pytest":
		base, prefix, suffix, marker := filepath.Join(root, "sdk"), "", "", "dotnet.dll"
		if component == "pytest" {
			base, prefix, suffix, marker = filepath.Join(root, "Lib", "site-packages"), "pytest-", ".dist-info", "METADATA"
			if developer.WithLocalMetadataFile(filepath.Join(base, "pytest", "__init__.py"), func(*os.File) error { return nil }) != nil {
				return ""
			}
		}
		names, _ := developer.ListLocalMetadataDirectories(base)
		best := ""
		for _, name := range names {
			matched, ok := strings.CutPrefix(name, prefix)
			if !ok {
				continue
			}
			matched, ok = strings.CutSuffix(matched, suffix)
			version := numericVersion(matched)
			if ok && version != "" && compareRuntimeVersion(version, best) > 0 &&
				developer.WithLocalMetadataFile(filepath.Join(base, name, marker), func(*os.File) error { return nil }) == nil {
				best = version
			}
		}
		return best
	}
	return ""
}

var runtimeNumericVersion = regexp.MustCompile(`^\d{1,5}(?:\.\d{1,5}){0,3}$`)

func numericVersion(value string) string {
	if !runtimeNumericVersion.MatchString(value) {
		return ""
	}
	return value
}

func compareRuntimeVersion(current, minimum string) int {
	parse := func(version string) [4]int {
		var parts [4]int
		for i, value := range strings.Split(version, ".") {
			if i >= len(parts) {
				break
			}
			parts[i], _ = strconv.Atoi(value)
		}
		return parts
	}
	a, b := parse(current), parse(minimum)
	return slices.Compare(a[:], b[:])
}

func formatVersion(ms, ls uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", ms>>16, ms&0xffff, ls>>16, ls&0xffff)
}
