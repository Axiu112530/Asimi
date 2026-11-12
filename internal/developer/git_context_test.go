package developer

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"testing"
)

func TestGitIndexValidatesEntriesAndChecksum(t *testing.T) {
	index := func(name string) []byte {
		var data bytes.Buffer
		data.WriteString("DIRC")
		binary.Write(&data, binary.BigEndian, uint32(2))
		binary.Write(&data, binary.BigEndian, uint32(1))
		entry := make([]byte, 62)
		binary.BigEndian.PutUint32(entry[24:], 0100644)
		copy(entry[40:], bytes.Repeat([]byte{1}, 20))
		binary.BigEndian.PutUint16(entry[60:], uint16(len(name)))
		entry = append(entry, []byte(name)...)
		entry = append(entry, 0)
		for len(entry)%8 != 0 {
			entry = append(entry, 0)
		}
		data.Write(entry)
		checksum := sha1.Sum(data.Bytes())
		data.Write(checksum[:])
		return data.Bytes()
	}
	entries, err := parseGitIndex(index("src/main.go"))
	if err != nil || len(entries) != 1 || entries["src/main.go"].mode != 0100644 {
		t.Fatalf("index: %+v, %v", entries, err)
	}
	for _, name := range []string{"../outside", `.git/config`, `dir\escape`, "C:/outside", "COM1", "space. "} {
		if _, err := parseGitIndex(index(name)); err == nil {
			t.Errorf("accepted unsafe path %q", name)
		}
	}
	bad := index("src/main.go")
	bad[12] ^= 1
	if _, err := parseGitIndex(bad); err == nil {
		t.Fatal("accepted invalid checksum")
	}
}

func TestGitConfigRejectsUnsupportedScope(t *testing.T) {
	for _, config := range []string{
		"[include]\npath=../outside\n", "[filter \"custom\"]\nclean=app\n",
		"[core]\nworktree=../outside\n", "[extensions]\nobjectformat=sha256\n",
	} {
		if _, err := parseGitConfig([]byte(config)); err == nil {
			t.Errorf("accepted %q", config)
		}
	}
	config, err := parseGitConfig([]byte("[core]\nrepositoryformatversion=0\nignorecase=true\nautocrlf=true\n[remote \"origin\"]\nurl=https://invalid.example\n"))
	if err != nil || !config.ignoreCase || !config.normalizeCRLF {
		t.Fatalf("config: %+v, %v", config, err)
	}
}

func TestGitIgnorePatterns(t *testing.T) {
	rules, err := parseGitIgnore("src", []byte("build/\n*.tmp\n!keep.tmp\n/local.txt\nfoo/**/test?.js\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		path      string
		dir, want bool
	}{
		{"src/build", true, true}, {"src/build", false, false}, {"src/nested/BUILD", true, true},
		{"src/nested/hide.tmp", false, true}, {"src/nested/keep.tmp", false, false},
		{"src/local.txt", false, true}, {"src/nested/local.txt", false, false},
		{"src/foo/test1.js", false, true}, {"src/foo/sub/test1.js", false, true}, {"outside/test.tmp", false, false},
	} {
		if actual := gitIgnored(rules, row.path, row.dir); actual != row.want {
			t.Errorf("%s: %t, want %t", row.path, actual, row.want)
		}
	}
}
