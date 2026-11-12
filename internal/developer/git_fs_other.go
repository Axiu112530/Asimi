//go:build !windows

package developer

import (
	"io"
	"os"
)

func (*workspace) pinGitDirectory() (io.Closer, error) { return nil, errGitUnsupported }
func (*workspace) gitOpen(string) (*os.File, error)    { return nil, errGitUnsupported }
func (*workspace) verifyGitPins(io.Closer) error       { return errGitUnsupported }
