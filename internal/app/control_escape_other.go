//go:build !windows

package app

func nativeControlEscapeAvailable() bool     { return false }
func nativeControlEscapeReader() func() bool { return nil }
