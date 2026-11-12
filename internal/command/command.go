// Package command resolves fixed registry IDs; it never accepts shell strings.
package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"asimi/internal/developer"
)

type Error string

func (e Error) Error() string { return string(e) }
func (e Error) Code() string  { return string(e) }

type Spec struct {
	ID               string   `json:"id"`
	Program          string   `json:"program"`
	Args             []string `json:"args"`
	ExecutesCode     bool     `json:"executesCode"`
	Network          bool     `json:"network"`
	DownloadURL      string   `json:"downloadUrl"`
	InstallPreview   []string `json:"installPreview"`
	AgentAllowed     bool     `json:"agentAllowed"`
	RequiresDownload bool     `json:"requiresDownload"`
}
type Proposal struct {
	ID             string   `json:"id"`
	Executable     string   `json:"executable"`
	Args           []string `json:"args"`
	Directory      string   `json:"directory"`
	DirectoryID    string   `json:"directoryId"`
	Network        bool     `json:"network"`
	ExecutesCode   bool     `json:"executesCode"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	MaxOutputBytes int      `json:"maxOutputBytes"`
	ExecutableHash string   `json:"executableHash"`
	ScriptHash     string   `json:"scriptHash,omitempty"`
	Hash           string   `json:"hash"`
}
type Result struct {
	ExitCode   int    `json:"exitCode"`
	Output     string `json:"output"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Truncated  bool   `json:"truncated"`
	Cancelled  bool   `json:"cancelled"`
	TimedOut   bool   `json:"timedOut"`
	DurationMS int64  `json:"durationMs"`
	Code       string `json:"code,omitempty"`
}
type Diagnostic struct {
	Component      string   `json:"component"`
	Installed      bool     `json:"installed"`
	Version        string   `json:"version"`
	Executable     string   `json:"executable"`
	Status         string   `json:"status"`
	Problems       []string `json:"problems"`
	Suggestions    []string `json:"suggestions"`
	DownloadURL    string   `json:"downloadUrl"`
	InstallPreview []string `json:"installPreview"`
}

func Catalog() []Spec {
	return []Spec{
		{ID: "go.version", Program: "go", Args: []string{"version"}, DownloadURL: "https://go.dev/dl/", InstallPreview: []string{"winget", "install", "--exact", "--id", "GoLang.Go"}},
		{ID: "java.version", Program: "java", Args: []string{"-version"}, DownloadURL: "https://adoptium.net/"},
		{ID: "node.version", Program: "node", Args: []string{"--version"}, DownloadURL: "https://nodejs.org/en/download"},
		{ID: "npm.version", Program: "npm", Args: []string{"--version"}, DownloadURL: "https://docs.npmjs.com/downloading-and-installing-node-js-and-npm"},
		{ID: "dotnet.info", Program: "dotnet", Args: []string{"--info"}, DownloadURL: "https://dotnet.microsoft.com/download"},
		{ID: "python.version", Program: "python", Args: []string{"--version"}, DownloadURL: "https://www.python.org/downloads/"},
		{ID: "git.version", Program: "git", Args: []string{"--version"}, DownloadURL: "https://git-scm.com/downloads"},
		{ID: "git.status", Program: "git", Args: []string{"--no-pager", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=NUL", "status", "--porcelain=v1", "--untracked-files=normal"}, ExecutesCode: true, Network: true},
		{ID: "git.diff", Program: "git", Args: []string{"--no-pager", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=NUL", "diff", "--no-ext-diff", "--no-textconv", "--"}, ExecutesCode: true, Network: true},
		{ID: "go.test", Program: "go", Args: []string{"test"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "go.vet", Program: "go", Args: []string{"vet"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "go.build", Program: "go", Args: []string{"build"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "go.deps", Program: "go", Args: []string{"list", "-m", "all"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "npm.test", Program: "npm", Args: []string{"test"}, ExecutesCode: true, Network: true},
		{ID: "node.test", Program: "node", Args: []string{"--test"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "node.check", Program: "node", Args: []string{"--check"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "npm.deps", Program: "npm", Args: []string{"ls", "--depth=0", "--ignore-scripts"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "java.test", Program: "java", Args: []string{"-Xmx256m", "-ea", "-classpath", "."}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "java.build", Program: "javac", Args: []string{"-J-Xmx256m", "-proc:none"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "java.deps", Program: "jdeps", Args: []string{"-J-Xmx256m", "-summary"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "dotnet.test", Program: "dotnet", Args: []string{"test", "--no-restore", "--disable-build-servers"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "dotnet.build", Program: "dotnet", Args: []string{"build", "--no-restore", "--disable-build-servers"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "dotnet.deps", Program: "dotnet", Args: []string{"package", "list", "--no-restore"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "python.pytest", Program: "python", Args: []string{"-B", "-m", "pytest"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "python.unittest", Program: "python", Args: []string{"-B", "-m", "unittest", "discover"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "python.check", Program: "python", Args: []string{"-I", "-m", "py_compile"}, ExecutesCode: true, Network: true, AgentAllowed: true},
		{ID: "python.deps", Program: "python", Args: []string{"-I", "-m", "pip", "--disable-pip-version-check", "list"}, ExecutesCode: true, Network: true, AgentAllowed: true},
	}
}

func localPath(path string) bool {
	v := filepath.VolumeName(path)
	return filepath.IsAbs(path) && len(v) == 2 && v[1] == ':' && ((v[0] >= 'A' && v[0] <= 'Z') || (v[0] >= 'a' && v[0] <= 'z')) && fixedDrive(v+string(os.PathSeparator))
}
func digest(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", Error("COMMAND_UNAVAILABLE")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return "", Error("COMMAND_UNAVAILABLE")
	}
	h := sha256.New()
	if n, err := io.Copy(h, io.LimitReader(f, (256<<20)+1)); err != nil || n != info.Size() {
		return "", Error("COMMAND_CHANGED")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func proposalHash(p Proposal) string {
	p.Hash = ""
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func Prepare(id string, args []string, dir string, network bool) (Proposal, error) {
	spec, ok := findSpec(id)
	if !ok {
		return Proposal{}, Error("COMMAND_DENIED")
	}
	if !localPath(dir) {
		return Proposal{}, Error("COMMAND_DIRECTORY")
	}
	dir = filepath.Clean(dir)
	info, e := os.Lstat(dir)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Proposal{}, Error("COMMAND_DIRECTORY")
	}
	directoryID, e := directoryIdentity(dir)
	if e != nil {
		return Proposal{}, e
	}
	if spec.Network && !network {
		return Proposal{}, Error("COMMAND_NETWORK_CONSENT")
	}
	targets, _, e := commandTargets(id, args, dir)
	if e != nil {
		return Proposal{}, e
	}
	exe, script, e := resolve(spec.Program, dir)
	if e != nil {
		return Proposal{}, e
	}
	finalArgs := append(slices.Clone(spec.Args), targets...)
	scriptHash := ""
	if script != "" {
		scriptHash, e = digest(script)
		if e != nil {
			return Proposal{}, e
		}
		finalArgs = append([]string{script}, finalArgs...)
	}
	exeHash, e := digest(exe)
	if e != nil {
		return Proposal{}, e
	}
	p := Proposal{ID: id, Executable: exe, Args: finalArgs, Directory: dir, DirectoryID: directoryID, Network: network, ExecutesCode: spec.ExecutesCode, TimeoutSeconds: 120, MaxOutputBytes: 128 << 10, ExecutableHash: exeHash, ScriptHash: scriptHash}
	if !spec.ExecutesCode {
		p.TimeoutSeconds = 15
	}
	p.Hash = proposalHash(p)
	return p, nil
}

// Only program names from the fixed catalog reach this resolver. No current-
// directory search, script wrappers, or workspace-provided executables.
func resolve(program, dir string) (string, string, error) {
	name := program + ".exe"
	if program == "npm" {
		name = "node.exe"
	}
	for _, base := range filepath.SplitList(os.Getenv("PATH")) {
		if !localPath(base) {
			continue
		}
		base = filepath.Clean(base)
		rel, e := filepath.Rel(dir, base)
		if e == nil && (rel == "." || !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..") {
			continue
		}
		candidate := filepath.Join(base, name)
		info, e := os.Lstat(candidate)
		if e != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		canonical, e := filepath.EvalSymlinks(candidate)
		if e != nil || !strings.EqualFold(canonical, candidate) {
			continue
		}
		if program == "npm" {
			script := filepath.Join(base, "node_modules", "npm", "bin", "npm-cli.js")
			if _, e = os.Stat(script); e != nil {
				continue
			}
			return candidate, script, nil
		}
		return candidate, "", nil
	}
	return "", "", Error("COMMAND_UNAVAILABLE")
}

func Run(ctx context.Context, p Proposal) (Result, error) {
	if p.Hash == "" || proposalHash(p) != p.Hash {
		return Result{ExitCode: -1}, Error("COMMAND_CHANGED")
	}
	if p.TimeoutSeconds < 1 || p.TimeoutSeconds > 120 || p.MaxOutputBytes < 1 || p.MaxOutputBytes > 128<<10 {
		return Result{ExitCode: -1}, Error("COMMAND_DENIED")
	}
	if ctx.Err() != nil {
		return interruptedCommand(Result{ExitCode: -1}, ctx.Err())
	}
	targets, err := approvedTargets(p)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	if ctx.Err() != nil {
		return interruptedCommand(Result{ExitCode: -1}, ctx.Err())
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutSeconds)*time.Second)
	defer cancel()
	return runManaged(ctx, p, targets)
}

func environment(p Proposal) []string {
	out := []string{}
	for _, name := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "ProgramFiles", "ProgramFiles(x86)", "GOROOT", "JAVA_HOME", "DOTNET_ROOT"} {
		if v := os.Getenv(name); v != "" {
			out = append(out, name+"="+v)
		}
	}
	// Exclude relative and workspace PATH entries, and all injection hooks and secrets.
	paths := []string{filepath.Dir(p.Executable)}
	for _, path := range filepath.SplitList(os.Getenv("PATH")) {
		if !localPath(path) {
			continue
		}
		rel, e := filepath.Rel(p.Directory, path)
		if e == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
			continue
		}
		paths = append(paths, path)
	}
	out = append(out, "PATH="+strings.Join(paths, string(os.PathListSeparator)), "GOTOOLCHAIN=local", "GOENV=off", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=NUL", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1", "PYTHONNOUSERSITE=1", "PYTEST_DISABLE_PLUGIN_AUTOLOAD=1", "npm_config_offline=true", "npm_config_audit=false", "npm_config_fund=false")
	return out
}

func redact(s string) string { return developer.RedactAgentText(s) }

func interruptedCommand(r Result, reason error) (Result, error) {
	r.TimedOut = reason == context.DeadlineExceeded
	r.Cancelled = !r.TimedOut
	r.Code = "COMMAND_CANCELLED"
	if r.TimedOut {
		r.Code = "COMMAND_TIMEOUT"
	}
	return r, Error(r.Code)
}

// InspectAvailability is used by autonomous read-only planning. It only looks
// up registered executable files and does not start a version command.
func InspectAvailability(workspace string) map[string]string {
	status := map[string]string{}
	for _, spec := range Catalog() {
		if spec.DownloadURL == "" {
			continue
		}
		_, _, err := resolve(spec.Program, workspace)
		if err != nil {
			status[spec.Program] = "NOT_FOUND_ON_PATH"
		} else {
			status[spec.Program] = "FOUND_ON_PATH_VERSION_UNVERIFIED"
		}
	}
	return status
}

func Diagnose(ctx context.Context) []Diagnostic {
	out := []Diagnostic{}
	dir, e := os.MkdirTemp("", "asimi-diagnosis-")
	if e != nil {
		return out
	}
	defer os.Remove(dir)
	for _, s := range Catalog() {
		if s.ExecutesCode || strings.HasPrefix(s.ID, "git.") && s.ID != "git.version" {
			continue
		}
		d := Diagnostic{Component: s.Program, Status: "MISSING", Problems: []string{}, Suggestions: []string{}, DownloadURL: s.DownloadURL, InstallPreview: s.InstallPreview}
		p, e := Prepare(s.ID, nil, dir, false)
		if e != nil {
			d.Problems = append(d.Problems, "NOT_ON_PATH")
			d.Suggestions = append(d.Suggestions, "INSTALL_FROM_OFFICIAL_SOURCE")
		} else {
			d.Installed = true
			d.Executable = p.Executable
			r, e := Run(ctx, p)
			d.Version = strings.TrimSpace(redact(r.Output))
			d.Status = "OK"
			if len(d.Version) > 4096 {
				d.Version = d.Version[:4096]
			}
			if e != nil || r.ExitCode != 0 {
				d.Status = "ERROR"
				d.Problems = append(d.Problems, "VERSION_CHECK_FAILED")
			}
		}
		out = append(out, d)
	}
	return out
}
