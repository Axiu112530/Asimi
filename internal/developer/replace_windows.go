package developer

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ktm                 = windows.NewLazySystemDLL("ktmw32.dll")
	createTransaction   = ktm.NewProc("CreateTransaction")
	commitTransaction   = ktm.NewProc("CommitTransaction")
	rollbackTransaction = ktm.NewProc("RollbackTransaction")
	openTransactedFile  = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateFileTransactedW")
)

func beginFileTransaction() (windows.Handle, error) {
	for _, proc := range []*windows.LazyProc{createTransaction, commitTransaction, rollbackTransaction, openTransactedFile} {
		if proc.Find() != nil {
			return 0, Error("WORKSPACE_REPLACE_UNAVAILABLE")
		}
	}
	handle, _, _ := createTransaction.Call(0, 0, 0, 0, 0, 15000, 0)
	if handle == uintptr(windows.InvalidHandle) {
		return 0, Error("WORKSPACE_REPLACE_UNAVAILABLE")
	}
	return windows.Handle(handle), nil
}

func openReplacement(transaction windows.Handle, target string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(`\\?\` + target)
	if err != nil {
		return 0, Error("PATH_DENIED")
	}
	handle, _, callErr := openTransactedFile.Call(uintptr(unsafe.Pointer(name)), windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ, 0, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0, uintptr(transaction), 0, 0)
	if handle == uintptr(windows.InvalidHandle) {
		if errors.Is(callErr, windows.ERROR_FILE_NOT_FOUND) || errors.Is(callErr, windows.ERROR_PATH_NOT_FOUND) || errors.Is(callErr, windows.ERROR_SHARING_VIOLATION) || errors.Is(callErr, windows.ERROR_TRANSACTIONAL_CONFLICT) {
			return 0, Error("STALE_TARGET")
		}
		return 0, Error("WORKSPACE_REPLACE_UNAVAILABLE")
	}
	return windows.Handle(handle), nil
}

// TxF pins the exact file against writes and renames through commit. Use the
// original inode so its ACL and other streams remain intact. Fail closed where
// the filesystem or Windows does not support the transaction API.
func (w *workspace) replace(ctx context.Context, d *directory, name string, change Change) (ChangeResult, error) {
	result := ChangeResult{RelativePath: change.RelativePath, RecoveryPath: change.RecoveryPath}
	transaction, err := beginFileTransaction()
	if err != nil {
		return result, err
	}
	defer windows.CloseHandle(transaction)
	defer rollbackTransaction.Call(uintptr(transaction))
	target := filepath.Join(w.path, filepath.FromSlash(change.RelativePath))
	file, err := openReplacement(transaction, target)
	if err != nil {
		return result, err
	}
	defer windows.CloseHandle(file)
	openedPath, err := handlePath(file)
	if err != nil || !strings.EqualFold(openedPath, target) {
		return result, Error("STALE_TARGET")
	}
	before, err := readWithoutClose(file, change.RelativePath)
	if err != nil || before.Identity != change.BeforeIdentity || before.Hash != change.BeforeHash {
		return result, Error("STALE_TARGET")
	}
	if ctx.Err() != nil {
		return result, Error("CANCELLED")
	}
	if err = w.checkPins(d); err != nil {
		return result, err
	}
	backup, err := writeNew(d.handle, path.Base(change.RecoveryPath), change.BeforeContent)
	if err != nil {
		return result, err
	}
	keepBackup := false
	defer func() {
		if keepBackup {
			windows.CloseHandle(backup)
		} else {
			cleanupHandle(backup)
		}
	}()
	if _, err = windows.Seek(file, 0, 0); err != nil {
		return result, Error("FILE_IO")
	}
	if change.AfterContent != "" {
		var written uint32
		if err = windows.WriteFile(file, []byte(change.AfterContent), &written, nil); err != nil || int(written) != len(change.AfterContent) {
			return result, Error("FILE_IO")
		}
	}
	if windows.SetEndOfFile(file) != nil {
		return result, Error("FILE_IO")
	}
	if ctx.Err() != nil {
		return result, Error("CANCELLED")
	}
	if err = w.checkPins(d); err != nil {
		return result, err
	}
	keepBackup = true
	if ok, _, _ := commitTransaction.Call(uintptr(transaction)); ok == 0 {
		result.Applied = true
		return result, Error("UNKNOWN_RESULT")
	}
	result.Applied, result.Atomic = true, true
	verify, err := openRelative(d.handle, name, windows.FILE_GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE)
	if err != nil {
		return result, Error("UNKNOWN_RESULT")
	}
	after, err := readHandle(verify, change.RelativePath)
	if err != nil || after.Identity != change.BeforeIdentity || after.Hash != change.AfterHash {
		return result, Error("UNKNOWN_RESULT")
	}
	result.Verified, result.Identity, result.AfterHash = true, after.Identity, after.Hash
	return result, nil
}
