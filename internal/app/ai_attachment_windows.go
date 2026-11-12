//go:build windows

package app

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

func openAttachmentFile(path string) (*os.File, error) {
	// Pin the managed directory while opening, so it cannot be swapped for a junction.
	dirp, e := windows.UTF16PtrFromString(filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	dir, e := windows.CreateFile(dirp, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		return nil, e
	}
	defer windows.CloseHandle(dir)
	var di windows.ByHandleFileInformation
	if e = windows.GetFileInformationByHandle(dir, &di); e != nil {
		return nil, e
	}
	if di.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, codedError("ATTACHMENT_UNAVAILABLE")
	}
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		return nil, e
	}
	var info windows.ByHandleFileInformation
	if e = windows.GetFileInformationByHandle(h, &info); e != nil {
		windows.CloseHandle(h)
		return nil, e
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		windows.CloseHandle(h)
		return nil, codedError("ATTACHMENT_UNAVAILABLE")
	}
	return os.NewFile(uintptr(h), path), nil
}
