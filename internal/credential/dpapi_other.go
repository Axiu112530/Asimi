//go:build !windows

package credential

func protect([]byte) ([]byte, error)   { return nil, ErrUnavailable }
func unprotect([]byte) ([]byte, error) { return nil, ErrUnavailable }
