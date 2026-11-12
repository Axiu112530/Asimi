//go:build !windows

package app

import "os"

func openAttachmentFile(path string) (*os.File, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, codedError("ATTACHMENT_UNAVAILABLE")
	}
	return os.Open(path)
}
