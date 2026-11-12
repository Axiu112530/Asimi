//go:build windows

package command

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func windowsFileVersion(path string) string {
	var handle windows.Handle
	size, err := windows.GetFileVersionInfoSize(path, &handle)
	if err != nil || size == 0 || size > 1<<20 {
		return ""
	}
	data := make([]byte, size)
	if windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&data[0])) != nil {
		return ""
	}
	var value *windows.VS_FIXEDFILEINFO
	if windows.VerQueryValue(unsafe.Pointer(&data[0]), "\\", unsafe.Pointer(&value), &size) != nil || value == nil ||
		uintptr(size) < unsafe.Sizeof(*value) || value.Signature != 0xfeef04bd {
		return ""
	}
	version := formatVersion(value.ProductVersionMS, value.ProductVersionLS)
	if version == "0.0.0.0" {
		version = formatVersion(value.FileVersionMS, value.FileVersionLS)
	}
	runtime.KeepAlive(data)
	if version == "0.0.0.0" {
		return ""
	}
	return version
}
