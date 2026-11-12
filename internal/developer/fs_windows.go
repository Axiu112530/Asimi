//go:build windows

package developer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

type workspace struct {
	path, identity string
	pins           []windows.Handle
	root           windows.Handle
}
type directory struct {
	handle   windows.Handle
	identity string
	pins     []windows.Handle
}
type nativeEntry struct {
	name      string
	directory bool
	bytes     int64
	unsafe    bool
}

func (w *workspace) Close() {
	for i := len(w.pins) - 1; i >= 0; i-- {
		windows.CloseHandle(w.pins[i])
	}
}
func (d *directory) Close() {
	for i := len(d.pins) - 1; i >= 0; i-- {
		windows.CloseHandle(d.pins[i])
	}
}

func openRelative(root windows.Handle, name string, access, share, disposition, options uint32) (windows.Handle, error) {
	if !validName(name) {
		return 0, Error("PATH_DENIED")
	}
	u, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, Error("PATH_DENIED")
	}
	attrs := windows.OBJECT_ATTRIBUTES{RootDirectory: root, ObjectName: u, Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE}
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, access, &attrs, &windows.IO_STATUS_BLOCK{}, nil, windows.FILE_ATTRIBUTE_NORMAL, share, disposition, options|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	return handle, err
}
func info(h windows.Handle, dir bool) (string, error) {
	var i windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &i) != nil || i.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (i.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != dir {
		return "", Error("PATH_DENIED")
	}
	if !dir && i.NumberOfLinks != 1 {
		return "", Error("PATH_DENIED")
	}
	return fmt.Sprintf("%08x:%08x%08x:%08x%08x", i.VolumeSerialNumber, i.FileIndexHigh, i.FileIndexLow, i.CreationTime.HighDateTime, i.CreationTime.LowDateTime), nil
}
func handlePath(h windows.Handle) (string, error) {
	buffer := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(h, &buffer[0], uint32(len(buffer)), 0)
	if err != nil || n == 0 || int(n) >= len(buffer) {
		return "", Error("WORKSPACE_UNSAFE")
	}
	name := windows.UTF16ToString(buffer[:n])
	if !strings.HasPrefix(name, `\\?\`) {
		return "", Error("WORKSPACE_UNSAFE")
	}
	name = strings.TrimPrefix(name, `\\?\`)
	if strings.HasPrefix(name, `UNC\`) || !filepath.IsAbs(name) {
		return "", Error("WORKSPACE_UNSAFE")
	}
	return filepath.Clean(name), nil
}
func openWorkspace(root string, writable bool) (*workspace, error) {
	if !utf8.ValidString(root) || strings.Contains(root, "/") || strings.HasPrefix(root, `\\`) || !filepath.IsAbs(root) || filepath.Clean(root) != strings.TrimSuffix(root, `\`) {
		return nil, Error("WORKSPACE_UNSAFE")
	}
	vol := filepath.VolumeName(root)
	if len(vol) != 2 || vol[1] != ':' {
		return nil, Error("WORKSPACE_UNSAFE")
	}
	components := strings.Split(strings.TrimPrefix(root, vol+`\`), `\`)
	for _, part := range components {
		if !validName(part) {
			return nil, Error("WORKSPACE_UNSAFE")
		}
	}
	drive, _ := windows.UTF16PtrFromString(vol + `\`)
	if windows.GetDriveType(drive) != windows.DRIVE_FIXED {
		return nil, Error("WORKSPACE_UNSAFE")
	}
	w := &workspace{}
	ok := false
	defer func() {
		if !ok {
			w.Close()
		}
	}()
	drivePath, _ := windows.UTF16PtrFromString(`\\?\` + vol + `\`)
	h, err := windows.CreateFile(drivePath, windows.FILE_READ_ATTRIBUTES|windows.FILE_TRAVERSE|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, Error("WORKSPACE_UNSAFE")
	}
	w.pins = append(w.pins, h)
	if _, err = info(h, true); err != nil {
		return nil, err
	}
	for i, part := range components {
		access := uint32(windows.FILE_READ_ATTRIBUTES | windows.FILE_TRAVERSE | windows.FILE_LIST_DIRECTORY | windows.SYNCHRONIZE)
		if writable && i == len(components)-1 {
			access |= windows.FILE_WRITE_DATA
		}
		child, e := openRelative(h, part, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE)
		if e != nil {
			return nil, Error("WORKSPACE_UNSAFE")
		}
		w.pins = append(w.pins, child)
		if _, e = info(child, true); e != nil {
			return nil, e
		}
		h = child
	}
	w.root = h
	w.path, err = handlePath(h)
	if err != nil {
		return nil, err
	}
	w.identity, err = info(h, true)
	if err != nil {
		return nil, err
	}
	ok = true
	return w, nil
}
func (w *workspace) parent(relative string, writable bool) (*directory, string, error) {
	components := strings.Split(relative, "/")
	d := &directory{handle: w.root, identity: w.identity}
	ok := false
	defer func() {
		if !ok {
			d.Close()
		}
	}()
	for i, part := range components[:len(components)-1] {
		access := uint32(windows.FILE_READ_ATTRIBUTES | windows.FILE_TRAVERSE | windows.FILE_LIST_DIRECTORY | windows.SYNCHRONIZE)
		if writable && i == len(components)-2 {
			access |= windows.FILE_WRITE_DATA
		}
		h, err := openRelative(d.handle, part, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE)
		if err != nil {
			return nil, "", Error("PATH_DENIED")
		}
		d.pins = append(d.pins, h)
		d.handle = h
		d.identity, err = info(h, true)
		if err != nil {
			return nil, "", err
		}
	}
	ok = true
	return d, components[len(components)-1], nil
}
func missing(err error) bool {
	return errors.Is(err, windows.STATUS_OBJECT_NAME_NOT_FOUND) || errors.Is(err, windows.STATUS_OBJECT_PATH_NOT_FOUND)
}
func readHandle(h windows.Handle, name string) (File, error) {
	return readHandleBounded(h, name, MaxFileBytes)
}
func readHandleBounded(h windows.Handle, name string, limit int) (File, error) {
	identity, err := info(h, false)
	if err != nil {
		windows.CloseHandle(h)
		return File{}, err
	}
	file := os.NewFile(uintptr(h), name)
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return File{}, Error("FILE_IO")
	}
	if !stat.Mode().IsRegular() || stat.Size() > int64(limit) {
		return File{}, Error("FILE_NOT_TEXT_OR_TOO_LARGE")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)))
	if err != nil {
		return File{Bytes: int64(len(data))}, Error("FILE_IO")
	}
	if len(data) > MaxFileBytes || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return File{Bytes: int64(len(data))}, Error("FILE_NOT_TEXT_OR_TOO_LARGE")
	}
	kind, language := fileKind(name)
	return File{Path: name, Content: string(data), Hash: hash(string(data)), Language: language, Type: kind, Identity: identity, Bytes: int64(len(data))}, nil
}
func (w *workspace) snapshot(relative string) (File, error) {
	return w.snapshotBounded(relative, MaxFileBytes)
}
func (w *workspace) snapshotBounded(relative string, limit int) (File, error) {
	parent, name, err := w.parent(relative, false)
	if err != nil {
		return File{}, err
	}
	defer parent.Close()
	h, err := openRelative(parent.handle, name, windows.FILE_GENERIC_READ, windows.FILE_SHARE_READ, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE)
	if missing(err) {
		return File{}, Error("FILE_NOT_FOUND")
	}
	if err != nil {
		return File{}, Error("PATH_DENIED")
	}
	return readHandleBounded(h, relative, min(limit, MaxFileBytes))
}
func (w *workspace) list(relative string, limit int) ([]nativeEntry, bool, error) {
	var handle windows.Handle
	var d *directory
	if relative != "" {
		var name string
		var err error
		d, name, err = w.parent(relative, false)
		if err != nil {
			return nil, false, err
		}
		defer d.Close()
		h, e := openRelative(d.handle, name, windows.FILE_GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE)
		if e != nil {
			return nil, false, Error("PATH_DENIED")
		}
		handle = h
	}
	if relative == "" {
		u, _ := windows.NewNTUnicodeString("")
		attrs := windows.OBJECT_ATTRIBUTES{RootDirectory: w.root, ObjectName: u, Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE}
		attrs.Length = uint32(unsafe.Sizeof(attrs))
		err := windows.NtCreateFile(&handle, windows.FILE_GENERIC_READ, &attrs, &windows.IO_STATUS_BLOCK{}, nil, windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
		if err != nil {
			return nil, false, Error("FILE_IO")
		}
	}
	if _, err := info(handle, true); err != nil {
		windows.CloseHandle(handle)
		return nil, false, err
	}
	file := os.NewFile(uintptr(handle), relative)
	defer file.Close()
	entries, err := file.ReadDir(limit + 1)
	if err != nil && err != io.EOF {
		return nil, false, Error("FILE_IO")
	}
	truncated := len(entries) > limit
	if truncated {
		entries = entries[:limit]
	}
	out := make([]nativeEntry, 0, len(entries))
	for _, e := range entries {
		entry := nativeEntry{name: e.Name(), directory: e.IsDir(), unsafe: e.Type()&os.ModeSymlink != 0 || !validName(e.Name())}
		if stat, err := e.Info(); err == nil {
			entry.bytes = stat.Size()
			if stat.Mode()&os.ModeIrregular != 0 {
				entry.unsafe = true
			}
		}
		out = append(out, entry)
	}
	return out, truncated, nil
}
func (w *workspace) checkPins(d *directory) error {
	for _, h := range append(append([]windows.Handle(nil), w.pins...), d.pins...) {
		if _, err := info(h, true); err != nil {
			return err
		}
	}
	actual, err := handlePath(w.root)
	if err != nil || actual != w.path {
		return Error("STALE_WORKSPACE")
	}
	return nil
}

type renameInformation struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [256]uint16
}

func renameRelative(h, root windows.Handle, name string) error {
	name16, err := windows.UTF16FromString(name)
	if err != nil || len(name16) > 256 {
		return Error("PATH_DENIED")
	}
	r := renameInformation{RootDirectory: root, FileNameLength: uint32((len(name16) - 1) * 2)}
	class := uint32(windows.FileRenameInformation)
	copy(r.FileName[:], name16)
	return windows.NtSetInformationFile(h, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&r)), uint32(unsafe.Offsetof(r.FileName))+r.FileNameLength, class)
}
func cleanupHandle(h windows.Handle) {
	flag := byte(1)
	_ = windows.NtSetInformationFile(h, &windows.IO_STATUS_BLOCK{}, &flag, 1, windows.FileDispositionInformation)
	windows.CloseHandle(h)
}
func writeNew(parent windows.Handle, name, content string) (windows.Handle, error) {
	h, err := openRelative(parent, name, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE, windows.FILE_SHARE_READ, windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE)
	if err != nil {
		return 0, Error("FILE_IO")
	}
	if len(content) > 0 {
		var written uint32
		if err = windows.WriteFile(h, []byte(content), &written, nil); err != nil || int(written) != len(content) {
			cleanupHandle(h)
			return 0, Error("FILE_IO")
		}
	}
	if windows.FlushFileBuffers(h) != nil {
		cleanupHandle(h)
		return 0, Error("FILE_IO")
	}
	return h, nil
}
func readWithoutClose(h windows.Handle, name string) (File, error) {
	// DuplicateHandle preserves the sharing contract while giving os.File ownership
	// of an independent handle. The shared file offset is reset for each read.
	if _, err := windows.Seek(h, 0, 0); err != nil {
		return File{}, Error("FILE_IO")
	}
	var duplicate windows.Handle
	if windows.DuplicateHandle(windows.CurrentProcess(), h, windows.CurrentProcess(), &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS) != nil {
		return File{}, Error("FILE_IO")
	}
	return readHandle(duplicate, name)
}

func (w *workspace) commit(ctx context.Context, d *directory, name string, c Change) (result ChangeResult, err error) {
	if c.Operation == "replace" {
		return w.replace(ctx, d, name, c)
	}
	result = ChangeResult{RelativePath: c.RelativePath}
	guard := func() error {
		if ctx.Err() != nil {
			return Error("CANCELLED")
		}
		return w.checkPins(d)
	}
	if err = guard(); err != nil {
		return
	}
	h, e := openRelative(d.handle, name, windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE)
	if e == nil {
		windows.CloseHandle(h)
		return result, Error("FILE_EXISTS")
	}
	if !missing(e) {
		return result, Error("PATH_DENIED")
	}
	tempName, e := randomName(".tmp")
	if e != nil {
		return result, e
	}
	writer, e := writeNew(d.handle, tempName, c.AfterContent)
	if e != nil {
		return result, e
	}
	committed := false
	defer func() {
		if committed {
			windows.CloseHandle(writer)
		} else {
			cleanupHandle(writer)
		}
	}()
	expectedID, e := info(writer, false)
	if e != nil {
		return result, e
	}
	if err = guard(); err != nil {
		return
	}
	if e = renameRelative(writer, d.handle, name); e != nil {
		if errors.Is(e, windows.STATUS_OBJECT_NAME_COLLISION) {
			return result, Error("FILE_EXISTS")
		}
		return result, Error("FILE_IO")
	}
	committed = true
	result.Applied = true
	result.Atomic = true
	// NTFS filename tunnelling can transfer the replaced name's creation time
	// to the new file during rename. The volume/file-index identity must remain
	// unchanged; capture the post-rename creation time from our same writer.
	committedID, e := info(writer, false)
	if e != nil || strings.Join(strings.Split(committedID, ":")[:2], ":") != strings.Join(strings.Split(expectedID, ":")[:2], ":") {
		return result, Error("UNKNOWN_RESULT")
	}
	expectedID = committedID
	result.Identity = expectedID
	// A committed write is never rolled back or replayed on verification failure.
	if err = guard(); err != nil {
		return result, Error("UNKNOWN_RESULT")
	}
	h, e = openRelative(d.handle, name, windows.FILE_GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE)
	if e != nil {
		return result, Error("UNKNOWN_RESULT")
	}
	after, e := readHandle(h, c.RelativePath)
	if e != nil || after.Identity != expectedID || after.Hash != c.AfterHash {
		return result, Error("UNKNOWN_RESULT")
	}
	result.Verified = true
	result.AfterHash = after.Hash
	return result, nil
}
