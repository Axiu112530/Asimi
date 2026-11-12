//go:build windows

// Package tools owns the only Phase 1 OS execution boundary. No executor, file
// reader, or reusable directory grant is exported to the agent runtime.
package tools

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"deskpilot/internal/domain"
	"golang.org/x/sys/windows"
)

const (
	ToolName        = "filesystem.file.create"
	MaxFileBytes    = 65536
	maxApprovalLife = 5 * time.Minute
)

// toolError intentionally contains no path, file contents or syscall details.
type toolError string

func (e toolError) Error() string { return string(e) }
func (e toolError) Code() string  { return string(e) }

type prepared struct {
	op       domain.Operation
	reserved bool
}

// Registry makes security validation, durable permission consumption, execution,
// and independent verification an indivisible public entry point.
type Registry struct {
	mu        sync.Mutex
	workspace string
	pins      []windows.Handle
	root      windows.Handle
	prepared  map[string]prepared
	closed    bool
}

// New opens every existing directory component without following reparse points
// and keeps each handle open without FILE_SHARE_DELETE. Only the last directory
// may be created. Root-relative NT operations avoid pathname resolution races.
func New(workspace string) (*Registry, error) {
	if !utf8.ValidString(workspace) || strings.HasPrefix(workspace, `\\`) || strings.Contains(workspace, "/") || !filepath.IsAbs(workspace) {
		return nil, toolError("WORKSPACE_UNSAFE")
	}
	vol := filepath.VolumeName(workspace)
	if len(vol) != 2 || vol[1] != ':' || !isLetter(vol[0]) {
		return nil, toolError("WORKSPACE_UNSAFE")
	}
	components := strings.Split(strings.TrimSuffix(strings.TrimPrefix(workspace, vol+`\`), `\`), `\`)
	if len(components) == 0 {
		return nil, toolError("WORKSPACE_UNSAFE")
	}
	for _, part := range components {
		if validateName(part) != nil {
			return nil, toolError("WORKSPACE_UNSAFE")
		}
	}
	drive, _ := windows.UTF16PtrFromString(vol + `\`)
	if windows.GetDriveType(drive) != windows.DRIVE_FIXED {
		return nil, toolError("WORKSPACE_UNSAFE")
	}
	r := &Registry{prepared: make(map[string]prepared)}
	success := false
	defer func() {
		if !success {
			r.Close()
		}
	}()
	rootPath, _ := windows.UTF16PtrFromString(`\\?\` + vol + `\`)
	h, err := windows.CreateFile(rootPath, windows.FILE_READ_ATTRIBUTES|windows.FILE_TRAVERSE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, toolError("WORKSPACE_UNSAFE")
	}
	r.pins = append(r.pins, h)
	if err := safeDirectory(h); err != nil {
		return nil, err
	}
	for i, part := range components {
		access := uint32(windows.FILE_READ_ATTRIBUTES | windows.FILE_TRAVERSE | windows.SYNCHRONIZE)
		if i == len(components)-1 {
			// FILE_ADD_FILE is the directory meaning of FILE_WRITE_DATA (0x2).
			access |= windows.FILE_WRITE_DATA | windows.FILE_LIST_DIRECTORY
		}
		child, openErr := openRelative(h, part, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
			windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE)
		if openErr != nil && i == len(components)-1 && missing(openErr) {
			child, openErr = openRelative(h, part, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
				windows.FILE_CREATE, windows.FILE_DIRECTORY_FILE)
		}
		if openErr != nil {
			return nil, toolError("WORKSPACE_UNSAFE")
		}
		r.pins = append(r.pins, child)
		if err := safeDirectory(child); err != nil {
			return nil, err
		}
		h = child
	}
	r.root = h
	r.workspace, err = handlePath(h)
	if err != nil || !strings.EqualFold(filepath.VolumeName(r.workspace), vol) {
		return nil, toolError("WORKSPACE_UNSAFE")
	}
	success = true
	return r, nil
}

func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// Workspace is the canonical pinned workspace name used in approval previews.
func (r *Registry) Workspace() string { return r.workspace }

func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var first error
	for i := len(r.pins) - 1; i >= 0; i-- {
		if err := windows.CloseHandle(r.pins[i]); err != nil && first == nil {
			first = toolError("FILE_IO")
		}
	}
	r.pins = nil
	r.prepared = nil
	return first
}

// Prepare is the Security Engine. Reusing an approval ID with different inputs
// is forbidden. The immutable private copy also detects coherent DTO tampering.
func (r *Registry) Prepare(taskID, fileName, content, approvalID string, expires time.Time) (domain.Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return domain.Operation{}, toolError("REGISTRY_CLOSED")
	}
	op, err := r.canonical(taskID, fileName, content, approvalID, expires)
	if err != nil {
		return domain.Operation{}, err
	}
	if err := validateExpiry(expires); err != nil {
		return domain.Operation{}, err
	}
	if err := r.checkPins(); err != nil {
		return domain.Operation{}, err
	}
	for id, old := range r.prepared {
		expiry, parseErr := time.Parse(time.RFC3339Nano, old.op.ExpiresAt)
		if !old.reserved && (parseErr != nil || !expiry.After(time.Now())) {
			delete(r.prepared, id)
		}
	}
	if old, ok := r.prepared[approvalID]; ok && (old.op != op || old.reserved) {
		return domain.Operation{}, toolError("INVALID_OPERATION")
	}
	r.prepared[approvalID] = prepared{op: op}
	return op, nil
}

func (r *Registry) canonical(taskID, name, content, approvalID string, expires time.Time) (domain.Operation, error) {
	if taskID == "" || approvalID == "" || len(taskID) > 256 || len(approvalID) > 256 {
		return domain.Operation{}, toolError("INVALID_OPERATION")
	}
	if err := validateName(name); err != nil {
		return domain.Operation{}, err
	}
	if !utf8.ValidString(content) {
		return domain.Operation{}, toolError("INVALID_CONTENT")
	}
	if len(content) > MaxFileBytes {
		return domain.Operation{}, toolError("CONTENT_TOO_LARGE")
	}
	sum := sha256.Sum256([]byte(content))
	return domain.Operation{
		TaskID: taskID, ToolName: ToolName, FileName: name, NormalizedPath: filepath.Join(r.workspace, name),
		Content: content, ContentHash: hex.EncodeToString(sum[:]), Bytes: len(content), Operation: "create",
		ApprovalID: approvalID, ExpiresAt: expires.UTC().Format(time.RFC3339Nano),
	}, nil
}

func validateExpiry(expires time.Time) error {
	now := time.Now()
	if !expires.After(now) {
		return toolError("APPROVAL_EXPIRED")
	}
	if expires.After(now.Add(maxApprovalLife)) {
		return toolError("INVALID_OPERATION")
	}
	return nil
}

func validateName(name string) error {
	if name == "" || name == "." || name == ".." || !utf8.ValidString(name) || filepath.Base(name) != name ||
		filepath.IsAbs(name) || strings.ContainsAny(name, `\/:<>"|?*`) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || len(utf16.Encode([]rune(name))) > 255 {
		return toolError("PATH_DENIED")
	}
	for _, ch := range name {
		if unicode.IsControl(ch) {
			return toolError("PATH_DENIED")
		}
	}
	base := strings.ToUpper(strings.TrimRight(strings.SplitN(name, ".", 2)[0], " "))
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" ||
		len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' ||
		(strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && strings.ContainsRune("¹²³", []rune(base)[len([]rune(base))-1]) {
		return toolError("PATH_DENIED")
	}
	return nil
}

// Execute is the only execution API. The callback is the Permission Engine's
// durable single-use approval CAS, and is always called before the first write.
// Approval failures and storage errors default to deny.
func (r *Registry) Execute(ctx context.Context, op domain.Operation, consume func(context.Context, domain.Operation) error, hooks ...ExecutionHooks) (v domain.Verification, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v = expectedVerification(op)
	defer func() { setVerificationCode(&v, err) }()
	if r.closed {
		return v, toolError("REGISTRY_CLOSED")
	}
	if err = cancelled(ctx); err != nil {
		return v, err
	}
	if err = r.validateOperation(op); err != nil {
		return v, err
	}
	if consume == nil {
		return v, toolError("APPROVAL_REQUIRED")
	}
	var executionHooks ExecutionHooks
	if len(hooks) > 1 || len(hooks) == 1 && hooks[0].Record == nil {
		return v, toolError("INVALID_OPERATION")
	}
	if len(hooks) == 1 {
		executionHooks = hooks[0]
	}
	if err = r.checkPins(); err != nil {
		return v, err
	}
	if err = r.checkAbsent(ctx, op.FileName); err != nil {
		return v, err
	}
	if err = cancelled(ctx); err != nil {
		return v, err
	}
	if err = r.validateOperation(op); err != nil {
		return v, err
	}
	// Permission consumption takes the service mutex, so release r.mu around
	// that callback. Reserve this exact operation before releasing r.mu so
	// concurrent Execute cannot replay it.
	r.prepared[op.ApprovalID] = prepared{op: op, reserved: true}
	r.mu.Unlock()
	consumeErr := func() error {
		defer r.mu.Lock()
		return consume(ctx, op)
	}()
	if r.closed {
		return v, toolError("REGISTRY_CLOSED")
	}
	if consumeErr != nil {
		r.prepared[op.ApprovalID] = prepared{op: op}
		if cancelErr := cancelled(ctx); cancelErr != nil {
			return v, cancelErr
		}
		return v, callbackError(consumeErr, "APPROVAL_DENIED")
	}
	// Even if cancellation races with the CAS, a consumed approval never becomes
	// reusable. Cancellation/expiry checks still prevent any subsequent writes.
	delete(r.prepared, op.ApprovalID)
	return r.execute(ctx, op, executionHooks)
}

func (r *Registry) validateOperation(op domain.Operation) error {
	old, ok := r.prepared[op.ApprovalID]
	if !ok || old.reserved || old.op != op {
		return toolError("INVALID_OPERATION")
	}
	expires, err := r.validateCanonicalOperation(op)
	if err != nil {
		return err
	}
	return validateExpiry(expires)
}

func (r *Registry) validateCanonicalOperation(op domain.Operation) (time.Time, error) {
	expires, err := time.Parse(time.RFC3339Nano, op.ExpiresAt)
	if err != nil {
		return time.Time{}, toolError("INVALID_OPERATION")
	}
	expected, err := r.canonical(op.TaskID, op.FileName, op.Content, op.ApprovalID, expires)
	if err != nil {
		return time.Time{}, err
	}
	if expected != op {
		return time.Time{}, toolError("INVALID_OPERATION")
	}
	return expires, nil
}

func cancelled(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil {
		return toolError("CANCELLED")
	}
	return nil
}

// executionGuard is also checked immediately before the atomic commit. Expiry
// after durable approval consumption must not start an uncommitted operation.
func (r *Registry) executionGuard(ctx context.Context, op domain.Operation) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	expires, err := time.Parse(time.RFC3339Nano, op.ExpiresAt)
	if err != nil || !expires.After(time.Now()) {
		return toolError("APPROVAL_EXPIRED")
	}
	return r.checkPins()
}

func (r *Registry) checkPins() error {
	for _, pin := range r.pins {
		if err := safeDirectory(pin); err != nil {
			return err
		}
	}
	actual, err := handlePath(r.root)
	if err != nil || actual != r.workspace {
		return toolError("WORKSPACE_UNSAFE")
	}
	return nil
}

func safeDirectory(h windows.Handle) error {
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return toolError("WORKSPACE_UNSAFE")
	}
	return nil
}

func handlePath(h windows.Handle) (string, error) {
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
	if err != nil || n == 0 || n >= uint32(len(buf)) {
		return "", toolError("WORKSPACE_UNSAFE")
	}
	name := windows.UTF16ToString(buf[:n])
	if !strings.HasPrefix(name, `\\?\`) {
		return "", toolError("WORKSPACE_UNSAFE")
	}
	name = strings.TrimPrefix(name, `\\?\`)
	if strings.HasPrefix(name, "UNC\\") || !filepath.IsAbs(name) {
		return "", toolError("WORKSPACE_UNSAFE")
	}
	return filepath.Clean(name), nil
}

// openRelative always receives a single validated component or an internally
// generated temporary basename. OBJ_DONT_REPARSE rejects reparses rather than
// asking os.Root (which intentionally follows safe links) to enforce this policy.
func openRelative(root windows.Handle, name string, access, share, disposition, options uint32) (windows.Handle, error) {
	unicodeName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, err
	}
	oa := windows.OBJECT_ATTRIBUTES{RootDirectory: root, ObjectName: unicodeName, Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE}
	oa.Length = uint32(unsafe.Sizeof(oa))
	var h windows.Handle
	err = windows.NtCreateFile(&h, access, &oa, &windows.IO_STATUS_BLOCK{}, nil, windows.FILE_ATTRIBUTE_NORMAL, share, disposition,
		options|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	return h, err
}

func missing(err error) bool {
	return errors.Is(err, windows.STATUS_OBJECT_NAME_NOT_FOUND) || errors.Is(err, windows.STATUS_OBJECT_PATH_NOT_FOUND)
}

func reparse(err error) bool {
	return errors.Is(err, windows.STATUS_REPARSE_POINT_ENCOUNTERED) || errors.Is(err, windows.STATUS_STOPPED_ON_SYMLINK) || errors.Is(err, windows.STATUS_IO_REPARSE_TAG_NOT_HANDLED)
}

func (r *Registry) checkAbsent(ctx context.Context, name string) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	h, err := openRelative(r.root, name, windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN, 0)
	if err == nil {
		windows.CloseHandle(h)
		return toolError("FILE_EXISTS")
	}
	if missing(err) {
		return nil
	}
	if reparse(err) {
		return toolError("PATH_DENIED")
	}
	return toolError("FILE_IO")
}

// execute is private; callers cannot obtain a write handle or bypass approval.
func (r *Registry) execute(ctx context.Context, op domain.Operation, hooks ExecutionHooks) (v domain.Verification, err error) {
	v = expectedVerification(op)
	if err = r.executionGuard(ctx, op); err != nil {
		return
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return v, toolError("FILE_IO")
	}
	tempName := ".deskpilot-" + hex.EncodeToString(random[:]) + ".tmp"
	if strings.EqualFold(tempName, op.FileName) {
		return v, toolError("FILE_IO")
	}
	if err = r.executionGuard(ctx, op); err != nil {
		return
	}
	writer, err := openRelative(r.root, tempName, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE,
		windows.FILE_SHARE_READ, windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE)
	if err != nil {
		return v, toolError("FILE_IO")
	}
	defer func() {
		if v.Created {
			windows.CloseHandle(writer)
			return
		}
		// Delete by our exclusively created handle only. A pathname cleanup
		// could delete someone else's file after a name-swap race.
		deleteFlag := byte(1)
		deleteErr := windows.NtSetInformationFile(writer, &windows.IO_STATUS_BLOCK{}, &deleteFlag, 1, windows.FileDispositionInformation)
		closeErr := windows.CloseHandle(writer)
		if deleteErr != nil || closeErr != nil {
			err = toolError("FILE_IO")
		}
	}()
	if err = r.executionGuard(ctx, op); err != nil {
		return
	}
	if len(op.Content) != 0 {
		var written uint32
		writeErr := windows.WriteFile(writer, []byte(op.Content), &written, nil)
		if writeErr != nil || int(written) != op.Bytes {
			err = toolError("FILE_IO")
			return
		}
	}
	if err = r.executionGuard(ctx, op); err != nil {
		return
	}
	if windows.FlushFileBuffers(writer) != nil {
		err = toolError("FILE_IO")
		return
	}
	if err = r.executionGuard(ctx, op); err != nil {
		return
	}
	_, witness, identifyErr := identifyFile(writer, tempName)
	if identifyErr != nil {
		return v, identifyErr
	}
	if hooks.Record != nil {
		if err = hooks.Record(witness); err != nil {
			return v, callbackError(err, "WITNESS_RECORD_FAILED")
		}
	}
	if err = r.executionGuard(ctx, op); err != nil {
		return
	}
	if renameErr := renameNoReplace(writer, r.root, op.FileName); renameErr != nil {
		if errors.Is(renameErr, windows.STATUS_OBJECT_NAME_COLLISION) {
			err = toolError("FILE_EXISTS")
		} else {
			err = toolError("FILE_IO")
		}
		return
	}
	// Nothing after this point may delete or roll back the committed file.
	v.Created = true
	if hooks.Committed != nil {
		hooks.Committed()
	}
	// The writer still denies write/delete sharing while the verifier reopens
	// the final name and independently reads the actual file.
	return r.verify(ctx, op, witness, true)
}

type renameInformation struct {
	ReplaceIfExists byte
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [256]uint16
}

func renameNoReplace(file, root windows.Handle, name string) error {
	name16, err := windows.UTF16FromString(name)
	if err != nil || len(name16) > 256 {
		return toolError("PATH_DENIED")
	}
	info := renameInformation{RootDirectory: root, FileNameLength: uint32((len(name16) - 1) * 2)}
	copy(info.FileName[:], name16)
	return windows.NtSetInformationFile(file, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Offsetof(info.FileName))+info.FileNameLength, windows.FileRenameInformation)
}

// verify independently opens the committed final name, rejects reparse points,
// compares file identity, and reads actual bytes bounded by the size policy.
func (r *Registry) verify(ctx context.Context, op domain.Operation, witness domain.FileWitness, committed bool) (v domain.Verification, err error) {
	v = expectedVerification(op)
	v.Created = committed
	if err = cancelled(ctx); err != nil {
		return
	}
	if err = r.checkPins(); err != nil {
		return
	}
	share := uint32(windows.FILE_SHARE_READ)
	if committed {
		// Accommodate our existing writer's access. The writer prevents any
		// other process from opening the file for writing or deletion.
		share |= windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
	}
	h, openErr := openRelative(r.root, op.FileName, windows.FILE_GENERIC_READ, share, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE)
	if openErr != nil {
		if !committed && missing(openErr) {
			return v, toolError("FILE_NOT_FOUND")
		}
		if reparse(openErr) || errors.Is(openErr, windows.STATUS_FILE_IS_A_DIRECTORY) {
			return v, toolError("WITNESS_MISMATCH")
		}
		return v, toolError("VERIFICATION_FAILED")
	}
	defer windows.CloseHandle(h)
	if err = cancelled(ctx); err != nil {
		return
	}
	actual, actualWitness, identifyErr := identifyFile(h, witness.TempName)
	if identifyErr != nil || actualWitness != witness {
		return v, toolError("WITNESS_MISMATCH")
	}
	actualPath, pathErr := handlePath(h)
	if pathErr != nil || actualPath != op.NormalizedPath {
		return v, toolError("WITNESS_MISMATCH")
	}
	v.Created = true
	length := uint64(actual.FileSizeHigh)<<32 | uint64(actual.FileSizeLow)
	v.ActualBytes = int(length)
	if v.ActualBytes < 0 || uint64(v.ActualBytes) != length {
		// The public DTO cannot represent this size on the current architecture.
		v.ActualBytes = 0
		return v, toolError("VERIFICATION_FAILED")
	}
	if length > MaxFileBytes {
		return v, toolError("VERIFICATION_FAILED")
	}
	buf := make([]byte, MaxFileBytes+1)
	n := 0
	for {
		if err = cancelled(ctx); err != nil {
			return
		}
		var read uint32
		readErr := windows.ReadFile(h, buf[n:], &read, nil)
		n += int(read)
		if readErr != nil && !errors.Is(readErr, windows.ERROR_HANDLE_EOF) {
			return v, toolError("VERIFICATION_FAILED")
		}
		if read == 0 || n == len(buf) {
			break
		}
	}
	v.ActualBytes = n
	sum := sha256.Sum256(buf[:n])
	v.ActualHash = hex.EncodeToString(sum[:])
	v.ContentMatched = string(buf[:n]) == op.Content
	v.Passed = n == op.Bytes && uint64(n) == length && v.ActualHash == op.ContentHash && v.ContentMatched
	if err = cancelled(ctx); err != nil {
		v.Passed = false
		return
	}
	if !v.Passed {
		return v, toolError("VERIFICATION_FAILED")
	}
	return v, nil
}
