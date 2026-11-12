package developer

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
)

type gitObjectReader struct {
	scan  *gitScan
	packs []gitPack
	ready bool
}

type gitPack struct {
	file    *os.File
	offsets map[string]int64
	length  int64
}

func (r *gitObjectReader) readCommitTree(head string, entries map[string]gitIndexEntry) error {
	kind, commit, err := r.object(head, 0)
	if err != nil {
		return err
	}
	tree, _, _ := bytes.Cut(commit, []byte("\n"))
	oid, ok := strings.CutPrefix(string(tree), "tree ")
	if kind != "commit" || !ok || !validGitOID(oid) {
		return errGitUnsupported
	}
	count := 0
	return r.readTree(oid, "", entries, map[string]bool{}, &count, 0)
}

func (r *gitObjectReader) readTree(oid, parent string, entries map[string]gitIndexEntry, seen map[string]bool, count *int, depth int) error {
	if depth > 30 || *count > gitMaxEntries {
		return errGitUnsupported
	}
	kind, data, err := r.object(oid, 0)
	if err != nil {
		return err
	}
	if kind != "tree" {
		return errGitUnsupported
	}
	for len(data) > 0 {
		(*count)++
		if *count > gitMaxEntries {
			return errGitUnsupported
		}
		record, rest, ok := bytes.Cut(data, []byte{0})
		rawMode, base, modeOK := strings.Cut(string(record), " ")
		mode, parseErr := strconv.ParseUint(rawMode, 8, 32)
		if !ok || !modeOK || parseErr != nil || !validName(base) || len(rest) < sha1.Size {
			return errGitUnsupported
		}
		name := path.Join(parent, base)
		key := strings.ToLower(name)
		if seen[key] || !gitSafePath(name) {
			return errGitUnsupported
		}
		seen[key] = true
		id := [sha1.Size]byte(rest[:sha1.Size])
		data = rest[sha1.Size:]
		if mode == 040000 {
			if err := r.readTree(hex.EncodeToString(id[:]), name, entries, seen, count, depth+1); err != nil {
				return err
			}
		} else if gitSupportedMode(uint32(mode)) {
			entries[name] = gitIndexEntry{id, uint32(mode)}
		} else {
			return errGitUnsupported
		}
	}
	return nil
}

func (r *gitObjectReader) object(oid string, depth int) (string, []byte, error) {
	if depth > 32 || !validGitOID(oid) {
		return "", nil, errGitUnsupported
	}
	file, err := r.scan.w.gitOpen(".git/objects/" + oid[:2] + "/" + oid[2:])
	var kind string
	var data []byte
	if err == nil {
		defer file.Close()
		raw, err := r.inflate(file, gitMaxFile+128)
		if err != nil {
			return "", nil, err
		}
		header, content, ok := bytes.Cut(raw, []byte{0})
		name, size, split := strings.Cut(string(header), " ")
		if !ok || !split || strconv.Itoa(len(content)) != size {
			return "", nil, errGitUnsupported
		}
		kind, data = name, content
	} else if errors.Is(err, fs.ErrNotExist) {
		if err := r.openPacks(); err != nil {
			return "", nil, err
		}
		for _, pack := range r.packs {
			offset, ok := pack.offsets[oid]
			if ok {
				kind, data, err = r.packedObject(pack, offset, depth)
				if err != nil {
					return "", nil, err
				}
				break
			}
		}
	} else {
		return "", nil, err
	}
	if !slices.Contains([]string{"commit", "tree", "blob", "tag"}, kind) {
		return "", nil, errGitUnsupported
	}
	digest := sha1.New()
	fmt.Fprintf(digest, "%s %d%c", kind, len(data), byte(0))
	digest.Write(data)
	if hex.EncodeToString(digest.Sum(nil)) != oid {
		return "", nil, errGitUnsupported
	}
	return kind, data, nil
}

func (r *gitObjectReader) inflate(reader io.Reader, limit int) ([]byte, error) {
	compressed, err := zlib.NewReader(io.LimitReader(reader, gitMaxFile+1024))
	if err != nil {
		return nil, errGitUnsupported
	}
	defer compressed.Close()
	data, err := readGitBytes(compressed, min(limit, int(r.scan.budget)))
	r.scan.budget -= int64(len(data))
	return data, err
}

func (r *gitObjectReader) openPacks() error {
	if r.ready {
		return nil
	}
	entries, truncated, err := r.scan.w.list(".git/objects/pack", 128)
	if err != nil || truncated {
		return errGitUnsupported
	}
	totalObjects := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.name, ".promisor") {
			return errGitUnsupported
		}
		base, ok := strings.CutSuffix(entry.name, ".idx")
		if !ok {
			continue
		}
		oid, ok := strings.CutPrefix(base, "pack-")
		if !ok || !validGitOID(oid) || entry.unsafe || entry.directory || len(r.packs) >= 16 {
			return errGitUnsupported
		}
		index, err := r.scan.metadataFile(".git/objects/pack/"+entry.name, 8<<20)
		if err != nil {
			return err
		}
		offsets, err := parseGitPackIndex(index, oid)
		if err != nil {
			return err
		}
		totalObjects += len(offsets)
		if totalObjects > 200000 {
			return errGitUnsupported
		}
		file, err := r.scan.w.gitOpen(".git/objects/pack/" + base + ".pack")
		if err != nil {
			return err
		}
		r.scan.pins = append(r.scan.pins, file)
		info, err := file.Stat()
		if err != nil {
			return err
		}
		var header [12]byte
		var trailer [sha1.Size]byte
		if info.Size() < 12+sha1.Size {
			return errGitUnsupported
		}
		_, headerErr := file.ReadAt(header[:], 0)
		_, trailerErr := file.ReadAt(trailer[:], info.Size()-sha1.Size)
		if headerErr != nil || trailerErr != nil || string(header[:4]) != "PACK" ||
			binary.BigEndian.Uint32(header[4:8]) != 2 || binary.BigEndian.Uint32(header[8:]) != uint32(len(offsets)) ||
			hex.EncodeToString(trailer[:]) != oid {
			return errGitUnsupported
		}
		r.packs = append(r.packs, gitPack{file, offsets, info.Size() - sha1.Size})
	}
	r.ready = true
	return nil
}

func parseGitPackIndex(data []byte, oid string) (map[string]int64, error) {
	const start = 8 + 256*4
	if len(data) < start+2*sha1.Size || !bytes.Equal(data[:8], []byte{255, 't', 'O', 'c', 0, 0, 0, 2}) {
		return nil, errGitUnsupported
	}
	countValue := binary.BigEndian.Uint32(data[start-4 : start])
	if countValue > 200000 {
		return nil, errGitUnsupported
	}
	count := int(countValue)
	tablesEnd := start + count*(sha1.Size+8)
	end := len(data) - 2*sha1.Size
	if tablesEnd > end || (end-tablesEnd)%8 != 0 {
		return nil, errGitUnsupported
	}
	checksum := sha1.Sum(data[:len(data)-sha1.Size])
	if !bytes.Equal(checksum[:], data[len(data)-sha1.Size:]) || hex.EncodeToString(data[end:end+sha1.Size]) != oid {
		return nil, errGitUnsupported
	}
	offsets := make(map[string]int64, count)
	for i := range count {
		id := hex.EncodeToString(data[start+i*sha1.Size : start+(i+1)*sha1.Size])
		short := binary.BigEndian.Uint32(data[tablesEnd-(count-i)*4:])
		offset := uint64(short)
		if short&0x80000000 != 0 {
			position := uint64(tablesEnd) + uint64(short&0x7fffffff)*8
			if position > uint64(end-8) {
				return nil, errGitUnsupported
			}
			offset = binary.BigEndian.Uint64(data[position:])
		}
		if offset < 12 || offset > 1<<48 {
			return nil, errGitUnsupported
		}
		offsets[id] = int64(offset)
	}
	if len(offsets) != count {
		return nil, errGitUnsupported
	}
	return offsets, nil
}

func (r *gitObjectReader) packedObject(pack gitPack, offset int64, depth int) (string, []byte, error) {
	if depth > 32 || offset < 12 || offset >= pack.length {
		return "", nil, errGitUnsupported
	}
	reader := bufio.NewReader(io.NewSectionReader(pack.file, offset, pack.length-offset))
	first, err := reader.ReadByte()
	if err != nil {
		return "", nil, err
	}
	size := uint64(first & 15)
	objectType := (first >> 4) & 7
	for shift := 4; first&128 != 0; shift += 7 {
		first, err = reader.ReadByte()
		if err != nil || shift > 25 {
			return "", nil, errGitUnsupported
		}
		size |= uint64(first&127) << shift
	}
	if size > gitMaxFile {
		return "", nil, errGitUnsupported
	}
	var kind string
	var base []byte
	switch objectType {
	case 1, 2, 3, 4:
		kind = [...]string{"", "commit", "tree", "blob", "tag"}[objectType]
	case 6:
		b, err := reader.ReadByte()
		if err != nil {
			return "", nil, err
		}
		distance := int64(b & 127)
		for b&128 != 0 {
			b, err = reader.ReadByte()
			if err != nil || distance > 1<<48 {
				return "", nil, errGitUnsupported
			}
			distance = ((distance + 1) << 7) | int64(b&127)
		}
		if distance == 0 {
			return "", nil, errGitUnsupported
		}
		kind, base, err = r.packedObject(pack, offset-distance, depth+1)
		if err != nil {
			return "", nil, err
		}
	case 7:
		var id [sha1.Size]byte
		if _, err := io.ReadFull(reader, id[:]); err != nil {
			return "", nil, err
		}
		kind, base, err = r.object(hex.EncodeToString(id[:]), depth+1)
		if err != nil {
			return "", nil, err
		}
	default:
		return "", nil, errGitUnsupported
	}
	data, err := r.inflate(reader, int(size))
	if err != nil || uint64(len(data)) != size {
		return "", nil, errGitUnsupported
	}
	if objectType >= 6 {
		data, err = applyGitDelta(base, data, int(r.scan.budget))
		r.scan.budget -= int64(len(data))
	}
	return kind, data, err
}

func applyGitDelta(base, data []byte, limit int) ([]byte, error) {
	original, read := binary.Uvarint(data)
	if read <= 0 || original != uint64(len(base)) {
		return nil, errGitUnsupported
	}
	data = data[read:]
	size, read := binary.Uvarint(data)
	if read <= 0 || size > uint64(min(limit, gitMaxFile)) {
		return nil, errGitUnsupported
	}
	data = data[read:]
	result := make([]byte, 0, int(size))
	for len(data) > 0 {
		op := data[0]
		data = data[1:]
		if op&128 == 0 {
			length := int(op)
			if length == 0 || length > len(data) || length > cap(result)-len(result) {
				return nil, errGitUnsupported
			}
			result = append(result, data[:length]...)
			data = data[length:]
			continue
		}
		offset, length := uint64(0), uint64(0)
		for i := range 7 {
			if op&(1<<i) == 0 {
				continue
			}
			if len(data) == 0 {
				return nil, errGitUnsupported
			}
			if i < 4 {
				offset |= uint64(data[0]) << (8 * i)
			} else {
				length |= uint64(data[0]) << (8 * (i - 4))
			}
			data = data[1:]
		}
		if length == 0 {
			length = 65536
		}
		if offset+length > uint64(len(base)) || length > uint64(cap(result)-len(result)) {
			return nil, errGitUnsupported
		}
		result = append(result, base[offset:offset+length]...)
	}
	if uint64(len(result)) != size {
		return nil, errGitUnsupported
	}
	return result, nil
}
