package app

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Hold the lock for the lifetime of SQLite and its workers. Windows releases
// the handle on process termination, including a crash.
func acquireProfile(dir string) (func() error, error) {
	path, err := windows.UTF16PtrFromString(filepath.Join(dir, "deskpilot.lock"))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0,
		nil, windows.OPEN_ALWAYS, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return nil, codedError("PROFILE_IN_USE")
	}
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || info.NumberOfLinks != 1 {
		windows.CloseHandle(handle)
		return nil, codedError("STORAGE_IO")
	}
	return func() error { return windows.CloseHandle(handle) }, nil
}
