package app

import "golang.org/x/sys/windows"

var controlGetAsyncKeyState = windows.NewLazySystemDLL("user32.dll").NewProc("GetAsyncKeyState")

func nativeControlEscapeAvailable() bool {
	return controlGetAsyncKeyState.Find() == nil
}

func nativeControlEscapeReader() func() bool {
	if !nativeControlEscapeAvailable() {
		return nil
	}
	return func() bool {
		// VK_ESCAPE only. Poll the current-down high bit; the low bit is
		// unreliable across processes. This never hooks or consumes input.
		state, _, _ := controlGetAsyncKeyState.Call(0x1b)
		return state&0x8000 != 0
	}
}
