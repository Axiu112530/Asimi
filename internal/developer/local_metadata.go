package developer

import (
	"os"
	"path/filepath"
	"strings"
)

// WithLocalMetadataFile pins a fixed-drive file and its ancestors during a
// local runtime metadata lookup. Callers only receive a read handle.
func WithLocalMetadataFile(absolute string, inspect func(*os.File) error) error {
	w, err := openWorkspace(filepath.Dir(absolute), false)
	if err != nil {
		return err
	}
	defer w.Close()
	if !strings.EqualFold(w.path, filepath.Dir(absolute)) {
		return Error("RUNTIME_PATH_UNSAFE")
	}
	f, err := w.gitOpen(filepath.Base(absolute))
	if err != nil {
		return err
	}
	defer f.Close()
	return inspect(f)
}

func ListLocalMetadataDirectories(absolute string) ([]string, error) {
	w, err := openWorkspace(absolute, false)
	if err != nil {
		return nil, err
	}
	defer w.Close()
	if !strings.EqualFold(w.path, absolute) {
		return nil, Error("RUNTIME_PATH_UNSAFE")
	}
	entries, truncated, err := w.list("", 128)
	if err != nil || truncated {
		return nil, Error("RUNTIME_DIRECTORY_UNAVAILABLE")
	}
	var names []string
	for _, entry := range entries {
		if entry.directory && !entry.unsafe {
			names = append(names, entry.name)
		}
	}
	return names, nil
}
