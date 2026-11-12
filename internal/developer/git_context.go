package developer

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
)

const (
	gitMaxEntries = 16000
	gitMaxChanges = 240
	gitMaxFile    = 16 << 20
	gitMaxTotal   = 128 << 20
	gitMaxConfig  = 128 << 10
)

var errGitUnsupported = errors.New("unsupported Git repository")

type GitContext struct {
	Status       string      `json:"status"`
	Branch       string      `json:"branch,omitempty"`
	Head         string      `json:"head,omitempty"`
	Staged       int         `json:"staged"`
	Unstaged     int         `json:"unstaged"`
	Untracked    int         `json:"untracked"`
	ChangedFiles int         `json:"changedFiles"`
	Changes      []GitChange `json:"changes,omitempty"`
	Truncated    bool        `json:"truncated"`
	dirtyPaths   map[string]GitChange
}

type GitChange struct {
	Path      string `json:"path"`
	Staged    bool   `json:"staged"`
	Unstaged  bool   `json:"unstaged"`
	Untracked bool   `json:"untracked"`
}

// ForAgent keeps reviewed aggregate metadata. Filenames and file contents are
// handled separately by the explicit source-selection and approval flow.
func (g GitContext) ForAgent() GitContext {
	g.Changes, g.dirtyPaths = nil, nil
	if RedactAgentText(g.Branch) != g.Branch {
		g.Branch = ""
	}
	return g
}

func (g GitContext) DirtyPath(name string) bool {
	name, err := relativePath(name)
	if err != nil {
		return false
	}
	_, ok := g.dirtyPaths[strings.ToLower(name)]
	return ok
}

func ReadGitContext(root string) (GitContext, error) {
	w, err := openWorkspace(root, false)
	if err != nil {
		return GitContext{}, err
	}
	defer w.Close()
	return inspectGit(w, nil), nil
}

type gitScan struct {
	w          *workspace
	config     gitConfig
	metadata   map[string][]byte
	absent     map[string]bool
	attributes map[string][]gitAttributeRule
	indexAttrs map[string]gitAttributeIndexEntry
	pins       []*os.File
	gitPin     io.Closer
	budget     int64
	metaBudget int
	entries    int
	objects    gitObjectReader
	exclusions []string
}

func inspectGit(w *workspace, exclusions []string) GitContext {
	gitPin, err := w.pinGitDirectory()
	if errors.Is(err, fs.ErrNotExist) {
		return GitContext{Status: "NOT_REPOSITORY"}
	}
	if err != nil {
		return GitContext{Status: "UNSUPPORTED"}
	}
	defer gitPin.Close()
	scan := gitScan{
		w: w, gitPin: gitPin, metadata: map[string][]byte{}, absent: map[string]bool{},
		attributes: map[string][]gitAttributeRule{}, budget: gitMaxTotal, metaBudget: 32 << 20, exclusions: exclusions,
	}
	defer func() {
		for _, file := range scan.pins {
			file.Close()
		}
	}()
	scan.objects = gitObjectReader{scan: &scan}
	result, err := scan.run()
	if errors.Is(err, errGitUnsupported) {
		return GitContext{Status: "UNSUPPORTED"}
	}
	if err != nil {
		return GitContext{Status: "ERROR"}
	}
	return result
}

func (s *gitScan) metadataFile(name string, limit int) ([]byte, error) {
	if data, ok := s.metadata[name]; ok {
		return data, nil
	}
	if s.absent[name] {
		return nil, nil
	}
	if len(s.pins) >= 512 {
		return nil, errGitUnsupported
	}
	file, err := s.w.gitOpen(name)
	if errors.Is(err, fs.ErrNotExist) {
		s.absent[name] = true
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.pins = append(s.pins, file)
	data, err := readGitBytes(file, min(limit, s.metaBudget))
	if err == nil {
		s.metadata[name] = data
		s.metaBudget -= len(data)
	}
	return data, err
}

func readGitBytes(r io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if len(data) > limit {
		return nil, errGitUnsupported
	}
	return data, err
}

func (s *gitScan) run() (GitContext, error) {
	for _, name := range []string{".git/commondir", ".git/config.worktree", ".git/objects/info/alternates", ".git/objects/info/http-alternates", ".git/info/attributes"} {
		data, err := s.metadataFile(name, gitMaxConfig)
		if err != nil || data != nil {
			return GitContext{}, errGitUnsupported
		}
	}
	data, err := s.metadataFile(".git/config", gitMaxConfig)
	if err != nil {
		return GitContext{}, err
	}
	s.config, err = parseGitConfig(data)
	if err != nil {
		return GitContext{}, err
	}
	branch, head, err := s.readHead()
	if err != nil {
		return GitContext{}, err
	}
	data, err = s.metadataFile(".git/index", 16<<20)
	if err != nil {
		return GitContext{}, err
	}
	index, err := parseGitIndex(data)
	if err != nil {
		return GitContext{}, err
	}
	s.indexAttrs = map[string]gitAttributeIndexEntry{}
	for name, entry := range index {
		if strings.EqualFold(path.Base(name), ".gitattributes") {
			s.indexAttrs[strings.ToLower(name)] = gitAttributeIndexEntry{name, entry}
		}
	}
	committed := map[string]gitIndexEntry{}
	if head != "" {
		if err := s.objects.readCommitTree(head, committed); err != nil {
			return GitContext{}, err
		}
	}

	result := GitContext{Status: "AVAILABLE", Branch: branch, Head: head, dirtyPaths: map[string]GitChange{}}
	add := func(name string, staged, unstaged, untracked bool) {
		key := strings.ToLower(name)
		change := result.dirtyPaths[key]
		if change.Path == "" {
			change.Path = name
		}
		change.Staged = change.Staged || staged
		change.Unstaged = change.Unstaged || unstaged
		change.Untracked = change.Untracked || untracked
		result.dirtyPaths[key] = change
	}
	for name, entry := range committed {
		if excluded(name, s.exclusions) {
			result.Truncated = true
			continue
		}
		if indexed, ok := index[name]; !ok || indexed != entry {
			add(name, true, false, false)
		}
	}
	names := make([]string, 0, len(index))
	for name := range index {
		names = append(names, name)
	}
	slices.Sort(names)
	tracked := map[string]bool{}
	for _, name := range names {
		entry := index[name]
		tracked[strings.ToLower(name)] = true
		if excluded(name, s.exclusions) {
			result.Truncated = true
			continue
		}
		if _, ok := committed[name]; !ok {
			add(name, true, false, false)
		}
		matches, complete, err := s.worktreeMatches(name, entry)
		if err != nil {
			return GitContext{}, err
		}
		if !complete {
			result.Truncated = true
			continue
		}
		if !matches {
			add(name, false, true, false)
		}
	}
	ignored, err := s.metadataFile(".git/info/exclude", gitMaxConfig)
	if err != nil {
		return GitContext{}, err
	}
	rules, err := parseGitIgnore("", ignored, s.config.ignoreCase)
	if err != nil {
		return GitContext{}, err
	}
	if err := s.scanUntracked("", rules, tracked, &result.Truncated, func(name string) { add(name, false, false, true) }); err != nil {
		return GitContext{}, err
	}
	for name := range s.absent {
		file, err := s.w.gitOpen(name)
		if err == nil {
			file.Close()
			return GitContext{}, errors.New("Git metadata changed during inspection")
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return GitContext{}, err
		}
	}
	if err := s.w.verifyGitPins(s.gitPin); err != nil {
		return GitContext{}, err
	}

	for _, change := range result.dirtyPaths {
		if change.Staged {
			result.Staged++
		}
		if change.Unstaged {
			result.Unstaged++
		}
		if change.Untracked {
			result.Untracked++
		}
		if AgentContextPathAllowed(change.Path) && !excluded(change.Path, s.exclusions) {
			result.Changes = append(result.Changes, change)
		}
	}
	result.ChangedFiles = len(result.dirtyPaths)
	slices.SortFunc(result.Changes, func(a, b GitChange) int { return strings.Compare(a.Path, b.Path) })
	if len(result.Changes) > gitMaxChanges {
		result.Changes = result.Changes[:gitMaxChanges]
	}
	return result, nil
}

func (s *gitScan) readHead() (string, string, error) {
	head, err := s.metadataFile(".git/HEAD", 4096)
	if err != nil {
		return "", "", err
	}
	value := strings.TrimSpace(string(head))
	if !strings.HasPrefix(value, "ref: ") {
		if !validGitOID(value) {
			return "", "", errGitUnsupported
		}
		return "", value, nil
	}
	ref := strings.TrimPrefix(value, "ref: ")
	if !strings.HasPrefix(ref, "refs/heads/") || !gitSafePath(ref) || strings.Contains(ref, "..") || strings.ContainsAny(ref, " ~^[]@{}") || len(ref) > 240 {
		return "", "", errGitUnsupported
	}
	branch := strings.TrimPrefix(ref, "refs/heads/")
	loose, err := s.metadataFile(".git/"+ref, 4096)
	if err != nil {
		return "", "", err
	}
	if loose != nil {
		head := strings.TrimSpace(string(loose))
		if !validGitOID(head) {
			return "", "", errGitUnsupported
		}
		return branch, head, nil
	}
	packed, err := s.metadataFile(".git/packed-refs", 4<<20)
	if err != nil {
		return "", "", err
	}
	for line := range strings.Lines(string(packed)) {
		oid, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && name == ref {
			if !validGitOID(oid) {
				return "", "", errGitUnsupported
			}
			return branch, oid, nil
		}
	}
	return branch, "", nil
}

func validGitOID(value string) bool {
	if len(value) != 40 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func gitSafePath(name string) bool {
	if strings.Contains(name, "\\") {
		return false
	}
	clean, err := relativePath(name)
	if err != nil || clean != name {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.EqualFold(part, ".git") || shortWindowsAlias.MatchString(part) {
			return false
		}
	}
	return true
}

func (s *gitScan) worktreeMatches(name string, entry gitIndexEntry) (matches, complete bool, err error) {
	if entry.mode == 0160000 || entry.mode == 0120000 {
		return false, false, nil
	}
	textMode, err := s.gitTextMode(name)
	if err != nil {
		return false, false, err
	}
	file, err := s.w.gitOpen(name)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errGitDirectory) {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return false, false, err
	}
	if stat.Size() > gitMaxFile || stat.Size() > s.budget {
		return false, false, nil
	}
	data, err := readGitBytes(file, int(stat.Size()))
	if err != nil {
		return false, false, err
	}
	s.budget -= int64(len(data))
	if gitBlobID(data) == entry.oid {
		return true, true, nil
	}
	if textMode == gitTextAlways || textMode == gitTextAuto && isGitText(data) {
		return gitBlobID(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))) == entry.oid, true, nil
	}
	return false, true, nil
}

func isGitText(data []byte) bool {
	var printable, nonPrintable int
	for i, c := range data {
		switch c {
		case 0:
			return false
		case '\r':
			if i+1 >= len(data) || data[i+1] != '\n' {
				return false
			}
		case '\b', '\t', '\n', '\x1b', '\f':
			printable++
		case '\x1a':
			if i+1 != len(data) {
				nonPrintable++
			}
		default:
			if c < 32 || c == 127 {
				nonPrintable++
			} else {
				printable++
			}
		}
	}
	return nonPrintable <= printable>>7
}

var errGitDirectory = errors.New("Git path is a directory")

func gitBlobID(data []byte) [sha1.Size]byte {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d%c", len(data), byte(0))
	h.Write(data)
	var id [sha1.Size]byte
	h.Sum(id[:0])
	return id
}

func (s *gitScan) scanUntracked(dir string, rules []gitIgnoreRule, tracked map[string]bool, truncated *bool, add func(string)) error {
	if strings.Count(dir, "/") >= 30 || s.entries >= gitMaxEntries {
		*truncated = true
		return nil
	}
	ignoreFile, err := s.metadataFile(path.Join(dir, ".gitignore"), gitMaxConfig)
	if err != nil {
		return err
	}
	local, err := parseGitIgnore(dir, ignoreFile, s.config.ignoreCase)
	if err != nil {
		return err
	}
	rules = append(slices.Clone(rules), local...)
	if len(rules) > 1024 {
		return errGitUnsupported
	}
	entries, limited, err := s.w.list(dir, gitMaxEntries-s.entries)
	if err != nil {
		return err
	}
	s.entries += len(entries)
	*truncated = *truncated || limited
	for _, entry := range entries {
		name := path.Join(dir, entry.name)
		if strings.EqualFold(entry.name, ".git") || gitIgnored(rules, name, entry.directory) {
			continue
		}
		if entry.unsafe || !gitSafePath(name) || excluded(name, s.exclusions) {
			*truncated = true
			continue
		}
		if entry.directory {
			nested, err := s.w.gitOpen(path.Join(name, ".git"))
			if err == nil {
				nested.Close()
			}
			if err == nil || errors.Is(err, errGitDirectory) {
				*truncated = true
				continue
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err := s.scanUntracked(name, rules, tracked, truncated, add); err != nil {
				return err
			}
		} else if !tracked[strings.ToLower(name)] {
			add(name)
		}
	}
	return nil
}
