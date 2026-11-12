//go:build windows

package credential

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

func protect(value []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(value)), Data: &value[0]}
	var out windows.DataBlob
	if e := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); e != nil {
		return nil, ErrUnavailable
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
func unprotect(value []byte) ([]byte, error) {
	if len(value) == 0 {
		return nil, ErrDecrypt
	}
	in := windows.DataBlob{Size: uint32(len(value)), Data: &value[0]}
	var out windows.DataBlob
	if e := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); e != nil {
		return nil, ErrDecrypt
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	defer Wipe(unsafe.Slice(out.Data, int(out.Size)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
