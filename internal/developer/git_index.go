package developer

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"strings"
	"unicode/utf8"
)

type gitConfig struct {
	ignoreCase    bool
	normalizeCRLF bool
}

// This reader accepts ordinary single-worktree repositories with SHA-1
// objects. Configuration cannot supply paths or invoke programs.
func parseGitConfig(data []byte) (gitConfig, error) {
	var config gitConfig
	if !utf8.Valid(data) || bytes.ContainsAny(data, "\x00\\") {
		return config, errGitUnsupported
	}
	section := ""
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return config, errGitUnsupported
			}
			raw := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			fields := strings.Fields(raw)
			if len(fields) == 0 {
				return config, errGitUnsupported
			}
			section = fields[0]
			if strings.HasPrefix(section, "include") || section == "filter" || section == "extensions" ||
				strings.Contains(section, ".") || section == "remote" && strings.Contains(raw, "promisor") {
				return config, errGitUnsupported
			}
			continue
		}
		key, raw, assigned := strings.Cut(line, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		value := strings.ToLower(strings.TrimSpace(raw))
		if !assigned {
			value = "true"
		}
		if section == "remote" && key == "promisor" || section == "lfs" || section == "" {
			return config, errGitUnsupported
		}
		if section != "core" {
			continue
		}
		switch key {
		case "worktree", "attributesfile", "excludesfile", "sparsecheckout", "sparsecheckoutcone":
			return config, errGitUnsupported
		case "repositoryformatversion":
			if value != "0" {
				return config, errGitUnsupported
			}
		case "bare", "filemode":
			if value != "false" {
				return config, errGitUnsupported
			}
		case "ignorecase", "autocrlf":
			switch value {
			case "true", "yes", "on", "1", "input":
				if key == "ignorecase" {
					config.ignoreCase = true
				} else {
					config.normalizeCRLF = true
				}
			case "false", "no", "off", "0":
				if key == "ignorecase" {
					config.ignoreCase = false
				} else {
					config.normalizeCRLF = false
				}
			default:
				return config, errGitUnsupported
			}
		}
	}
	return config, nil
}

type gitIndexEntry struct {
	oid  [sha1.Size]byte
	mode uint32
}

func parseGitIndex(data []byte) (map[string]gitIndexEntry, error) {
	entries := map[string]gitIndexEntry{}
	if data == nil {
		return entries, nil
	}
	if len(data) < 12+sha1.Size || string(data[:4]) != "DIRC" {
		return nil, errGitUnsupported
	}
	content, checksum := data[:len(data)-sha1.Size], data[len(data)-sha1.Size:]
	actual := sha1.Sum(content)
	if !bytes.Equal(checksum, actual[:]) {
		return nil, errGitUnsupported
	}
	version, count := binary.BigEndian.Uint32(content[4:]), binary.BigEndian.Uint32(content[8:])
	if version < 2 || version > 3 || count > gitMaxEntries {
		return nil, errGitUnsupported
	}
	content = content[12:]
	folded := map[string]bool{}
	for range count {
		const fixed = 62
		if len(content) < fixed {
			return nil, errGitUnsupported
		}
		flags := binary.BigEndian.Uint16(content[60:fixed])
		if flags&0xf000 != 0 {
			return nil, errGitUnsupported // assume-unchanged, extended flags, or conflict stages
		}
		length := bytes.IndexByte(content[fixed:], 0)
		if length < 1 || length > 2048 || length != int(flags&0xfff) {
			return nil, errGitUnsupported
		}
		name := string(content[fixed : fixed+length])
		key := strings.ToLower(name)
		mode := binary.BigEndian.Uint32(content[24:])
		if folded[key] || !gitSafePath(name) || !gitSupportedMode(mode) {
			return nil, errGitUnsupported
		}
		folded[key] = true
		entries[name] = gitIndexEntry{[sha1.Size]byte(content[40:60]), mode}
		next := (fixed + length + 8) &^ 7
		if next > len(content) {
			return nil, errGitUnsupported
		}
		content = content[next:]
	}
	for len(content) != 0 {
		if len(content) < 8 || content[0] < 'A' || content[0] > 'Z' {
			return nil, errGitUnsupported // required extensions such as split or sparse index
		}
		size := uint64(binary.BigEndian.Uint32(content[4:8]))
		if size > uint64(len(content)-8) {
			return nil, errGitUnsupported
		}
		content = content[8+size:]
	}
	return entries, nil
}

func gitSupportedMode(mode uint32) bool {
	return mode == 0100644 || mode == 0100755 || mode == 0120000 || mode == 0160000
}
