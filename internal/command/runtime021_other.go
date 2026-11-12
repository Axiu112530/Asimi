//go:build !windows

package command

func windowsFileVersion(string) string { return "" }
