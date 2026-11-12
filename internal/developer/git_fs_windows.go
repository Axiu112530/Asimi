//go:build windows

package developer

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func (w *workspace) pinGitDirectory() (io.Closer, error) {
	handle, err := gitOpenChild(w.root, ".git", true)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), ".git"), nil
}

func (w *workspace) gitOpen(relative string) (*os.File, error) {
	parts := strings.Split(relative, "/")
	if len(parts) > 34 {
		return nil, errGitUnsupported
	}
	parent := w.root
	var directories []windows.Handle
	defer func() {
		for _, directory := range directories {
			windows.CloseHandle(directory)
		}
	}()
	for _, name := range parts[:len(parts)-1] {
		child, err := gitOpenChild(parent, name, true)
		if err != nil {
			if errors.Is(err, windows.STATUS_NOT_A_DIRECTORY) {
				return nil, fs.ErrNotExist
			}
			return nil, err
		}
		directories = append(directories, child)
		parent = child
	}
	handle, err := gitOpenChild(parent, parts[len(parts)-1], false)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), relative), nil
}

func gitOpenChild(parent windows.Handle, name string, directory bool) (windows.Handle, error) {
	share := uint32(windows.FILE_SHARE_READ)
	options := uint32(windows.FILE_NON_DIRECTORY_FILE)
	if directory {
		share |= windows.FILE_SHARE_WRITE
		options = windows.FILE_DIRECTORY_FILE
	}
	handle, err := openRelative(parent, name, windows.FILE_GENERIC_READ, share, windows.FILE_OPEN, options)
	if missing(err) {
		return 0, fs.ErrNotExist
	}
	if errors.Is(err, windows.STATUS_FILE_IS_A_DIRECTORY) {
		return 0, errGitDirectory
	}
	if err != nil {
		return 0, err
	}
	if _, err := info(handle, directory); err != nil {
		windows.CloseHandle(handle)
		return 0, errGitUnsupported
	}
	return handle, nil
}

func (w *workspace) verifyGitPins(closer io.Closer) error {
	if err := w.checkPins(&directory{}); err != nil {
		return err
	}
	handle := windows.Handle(closer.(*os.File).Fd())
	if _, err := info(handle, true); err != nil {
		return err
	}
	name, err := handlePath(handle)
	if err != nil || !strings.EqualFold(name, filepath.Join(w.path, ".git")) {
		return errGitUnsupported
	}
	return nil
}
