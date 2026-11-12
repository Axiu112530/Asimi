package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var user = syscall.NewLazyDLL("user32.dll")
var gdi = syscall.NewLazyDLL("gdi32.dll")

func u(name string, args ...uintptr) uintptr { v, _, _ := user.NewProc(name).Call(args...); return v }
func g(name string, args ...uintptr) uintptr { v, _, _ := gdi.NewProc(name).Call(args...); return v }

type rect struct{ L, T, R, B int32 }
type point struct{ X, Y int32 }
type fixture struct {
	PID                          uint32
	Window, Edit, Button, Status uintptr
}
type input struct {
	Type uint32
	Pad  uint32
	Data [32]byte
}
type blob struct {
	Size uint32
	Data *byte
}

func must(ok bool, msg string) {
	if !ok {
		panic(msg)
	}
}
func title(h uintptr) string {
	b := make([]uint16, 256)
	u("GetWindowTextW", h, uintptr(unsafe.Pointer(&b[0])), 256)
	return syscall.UTF16ToString(b)
}
func controlText(h uintptr) string {
	b := make([]uint16, 256)
	var result uintptr
	must(u("SendMessageTimeoutW", h, 0x000D, 256, uintptr(unsafe.Pointer(&b[0])), 2, 500, uintptr(unsafe.Pointer(&result))) != 0, "WM_GETTEXT timeout")
	return syscall.UTF16ToString(b)
}
func bounds(h uintptr) rect {
	var r rect
	must(u("GetWindowRect", h, uintptr(unsafe.Pointer(&r))) != 0, "GetWindowRect")
	return r
}
func send(events []input) {
	must(u("SendInput", uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(input{})) == uintptr(len(events)), "SendInput blocked or partial")
}
func mouse(flags uint32, x, y int32) input {
	a := input{}
	binary.LittleEndian.PutUint32(a.Data[0:], uint32(x))
	binary.LittleEndian.PutUint32(a.Data[4:], uint32(y))
	binary.LittleEndian.PutUint32(a.Data[12:], flags)
	return a
}
func key(c uint16, flags uint32) input {
	a := input{Type: 1}
	binary.LittleEndian.PutUint16(a.Data[2:], c)
	binary.LittleEndian.PutUint32(a.Data[4:], flags)
	return a
}
func normalized(p, origin, extent int32) int32 {
	must(extent > 1 && p >= origin && p < origin+extent, "coordinate outside desktop")
	return int32((int64(p-origin) * 65535) / int64(extent-1))
}
func capture(r rect) map[string]any {
	w, h := int(r.R-r.L), int(r.B-r.T)
	must(w > 0 && h > 0 && w*h < 2000000, "capture bounds")
	dc := u("GetDC", 0)
	must(dc != 0, "GetDC")
	defer u("ReleaseDC", 0, dc)
	mem := g("CreateCompatibleDC", dc)
	must(mem != 0, "CreateCompatibleDC")
	defer g("DeleteDC", mem)
	bmp := g("CreateCompatibleBitmap", dc, uintptr(w), uintptr(h))
	must(bmp != 0, "CreateCompatibleBitmap")
	defer g("DeleteObject", bmp)
	old := g("SelectObject", mem, bmp)
	must(g("BitBlt", mem, 0, 0, uintptr(w), uintptr(h), dc, uintptr(r.L), uintptr(r.T), 0x00CC0020|0x40000000) != 0, "BitBlt")
	g("SelectObject", mem, old)
	// BITMAPINFOHEADER, top-down BGRA. No screenshot is ever written to disk.
	header := make([]byte, 44)
	binary.LittleEndian.PutUint32(header, 40)
	binary.LittleEndian.PutUint32(header[4:], uint32(w))
	binary.LittleEndian.PutUint32(header[8:], uint32(-h))
	binary.LittleEndian.PutUint16(header[12:], 1)
	binary.LittleEndian.PutUint16(header[14:], 32)
	pixels := make([]byte, w*h*4)
	must(g("GetDIBits", mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&header[0])), 0) == uintptr(h), "GetDIBits")
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	nonzero := false
	for i := 0; i < len(pixels); i += 4 {
		img.Pix[i] = pixels[i+2]
		img.Pix[i+1] = pixels[i+1]
		img.Pix[i+2] = pixels[i]
		img.Pix[i+3] = 255
		if pixels[i] != 0 || pixels[i+1] != 0 || pixels[i+2] != 0 {
			nonzero = true
		}
	}
	var buf bytes.Buffer
	must(png.Encode(&buf, img) == nil, "PNG encode")
	decoded, e := png.DecodeConfig(bytes.NewReader(buf.Bytes()))
	must(e == nil && decoded.Width == w && decoded.Height == h, "PNG roundtrip")
	return map[string]any{"width": w, "height": h, "pngBytes": buf.Len(), "nonBlackPixels": nonzero, "memoryOnly": true}
}
func dpapi() map[string]any {
	data := []byte("phase0-dummy-key-not-a-real-secret")
	entropy := []byte("Asimi/phase0")
	in := blob{uint32(len(data)), &data[0]}
	ent := blob{uint32(len(entropy)), &entropy[0]}
	var out blob
	crypt := syscall.NewLazyDLL("crypt32.dll")
	kernel := syscall.NewLazyDLL("kernel32.dll")
	ok, _, e := crypt.NewProc("CryptProtectData").Call(uintptr(unsafe.Pointer(&in)), 0, uintptr(unsafe.Pointer(&ent)), 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	must(ok != 0, "CryptProtectData: "+e.Error())
	defer kernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(out.Data)))
	ciphertext := unsafe.Slice(out.Data, out.Size)
	sum := sha256.Sum256(ciphertext)
	var plain blob
	ok, _, _ = crypt.NewProc("CryptUnprotectData").Call(uintptr(unsafe.Pointer(&out)), 0, uintptr(unsafe.Pointer(&ent)), 0, 0, 1, uintptr(unsafe.Pointer(&plain)))
	must(ok != 0, "CryptUnprotectData")
	defer kernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(plain.Data)))
	match := bytes.Equal(data, unsafe.Slice(plain.Data, plain.Size))
	wrong := []byte("wrong")
	went := blob{uint32(len(wrong)), &wrong[0]}
	var bad blob
	badOK, _, _ := crypt.NewProc("CryptUnprotectData").Call(uintptr(unsafe.Pointer(&out)), 0, uintptr(unsafe.Pointer(&went)), 0, 0, 1, uintptr(unsafe.Pointer(&bad)))
	if badOK != 0 {
		kernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(bad.Data)))
	}
	return map[string]any{"currentUserRoundTrip": match, "wrongEntropyRejected": badOK == 0, "cipherBytes": out.Size, "cipherSHA256": hex.EncodeToString(sum[:])}
}

func comProbe() map[string]any {
	ole := syscall.NewLazyDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 0)
	must(hr == 0 || hr == 1, "COM MTA initialization")
	defer ole.NewProc("CoUninitialize").Call()
	clsid := [16]byte{0xa4, 0xdb, 0x48, 0xff, 0xef, 0x60, 0x01, 0x42, 0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}
	iid := [16]byte{0x7d, 0xe5, 0xcb, 0x30, 0xd0, 0xd9, 0x2a, 0x45, 0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}
	var object *struct{ VTable *[3]uintptr }
	hr, _, _ = ole.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(&clsid[0])), 0, 1, uintptr(unsafe.Pointer(&iid[0])), uintptr(unsafe.Pointer(&object)))
	must(hr == 0 && object != nil, "CoCreateInstance CUIAutomation")
	release := object.VTable[2]
	syscall.SyscallN(release, uintptr(unsafe.Pointer(object)))
	return map[string]any{"MTAInitialized": true, "IUIAutomationCreated": true, "released": true, "treeTraversalTestedInGo": false}
}
func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	must(len(os.Args) == 3, "usage: native fixture.json evidence.json")
	// A thread-local context change; no global display settings are modified.
	must(u("SetThreadDpiAwarenessContext", ^uintptr(3)) != 0, "PerMonitorV2 context")
	raw, e := os.ReadFile(os.Args[1])
	must(e == nil, "read fixture")
	var f fixture
	must(json.Unmarshal(raw, &f) == nil, "decode fixture")
	var pid uint32
	u("GetWindowThreadProcessId", f.Window, uintptr(unsafe.Pointer(&pid)))
	must(pid == f.PID && pid != 0 && title(f.Window) == "Asimi Phase0 Fixture", "fixture identity")
	must(u("IsChild", f.Window, f.Edit) != 0 && u("IsChild", f.Window, f.Button) != 0, "fixture children")
	var monitors []rect
	cb := syscall.NewCallback(func(_ uintptr, _ uintptr, r *rect, _ uintptr) uintptr { monitors = append(monitors, *r); return 1 })
	must(u("EnumDisplayMonitors", 0, 0, cb, 0) != 0, "EnumDisplayMonitors")
	originX := int32(u("GetSystemMetrics", 76))
	originY := int32(u("GetSystemMetrics", 77))
	width := int32(u("GetSystemMetrics", 78))
	height := int32(u("GetSystemMetrics", 79))
	// Start-Process uses Hidden per host rules; explicitly reveal only the known
	// fixture for this short, user-authorized interactive integration test.
	u("ShowWindow", f.Window, 5)
	must(u("IsWindowVisible", f.Button) != 0, "fixture visibility guard")
	u("SetForegroundWindow", f.Window)
	time.Sleep(250 * time.Millisecond)
	must(u("GetForegroundWindow") == f.Window, "focus guard")
	screenshot := capture(bounds(f.Window))
	oldCursor := point{}
	u("GetCursorPos", uintptr(unsafe.Pointer(&oldCursor)))
	defer u("SetCursorPos", uintptr(oldCursor.X), uintptr(oldCursor.Y))
	r := bounds(f.Button)
	cx, cy := (r.L+r.R)/2, (r.T+r.B)/2
	send([]input{mouse(0x8000|0x4000|1, normalized(cx, originX, width), normalized(cy, originY, height))})
	time.Sleep(100 * time.Millisecond)
	var actual point
	u("GetCursorPos", uintptr(unsafe.Pointer(&actual)))
	target := u("WindowFromPoint", uintptr(uint64(uint32(actual.X))|uint64(uint32(actual.Y))<<32))
	var targetPID uint32
	u("GetWindowThreadProcessId", target, uintptr(unsafe.Pointer(&targetPID)))
	class := make([]uint16, 256)
	u("GetClassNameW", target, uintptr(unsafe.Pointer(&class[0])), 256)
	must(target == f.Button, fmt.Sprintf("pointer target guard: wanted=(%d,%d), actual=(%d,%d), target=%d class=%s pid=%d, fixture=%d pid=%d button=%d visible=%d enabled=%d, virtual=(%d,%d,%d,%d)", cx, cy, actual.X, actual.Y, target, syscall.UTF16ToString(class), targetPID, f.Window, f.PID, f.Button, u("IsWindowVisible", f.Button), u("IsWindowEnabled", f.Button), originX, originY, width, height))
	must(u("GetForegroundWindow") == f.Window, "focus before click")
	send([]input{mouse(2, 0, 0), mouse(4, 0, 0)})
	time.Sleep(100 * time.Millisecond)
	must(title(f.Status) == "clicked", "click postcondition")
	r = bounds(f.Edit)
	cx, cy = (r.L+r.R)/2, (r.T+r.B)/2
	send([]input{mouse(0x8000|0x4000|1, normalized(cx, originX, width), normalized(cy, originY, height))})
	time.Sleep(50 * time.Millisecond)
	must(u("GetForegroundWindow") == f.Window, "focus before text")
	send([]input{mouse(2, 0, 0), mouse(4, 0, 0)})
	for _, c := range utf16.Encode([]rune("Asimi验证")) {
		must(u("GetForegroundWindow") == f.Window, "focus during text")
		send([]input{key(c, 4), key(c, 4|2)})
	}
	time.Sleep(100 * time.Millisecond)
	must(controlText(f.Edit) == "Asimi验证", "unicode input postcondition")
	var scale uint32
	monitor := u("MonitorFromWindow", f.Window, 2)
	scaleHR, _, _ := syscall.NewLazyDLL("shcore.dll").NewProc("GetScaleFactorForMonitor").Call(monitor, uintptr(unsafe.Pointer(&scale)))
	result := map[string]any{"go": runtime.Version(), "os": "windows/amd64", "monitors": monitors, "virtualDesktop": rect{originX, originY, originX + width, originY + height}, "fixtureDPI": u("GetDpiForWindow", f.Window), "monitorScalePercent": scale, "scaleQueryHRESULT": scaleHR, "directGoCOM": comProbe(), "threadDPIContext": "PER_MONITOR_AWARE_V2", "screenshot": screenshot, "input": map[string]any{"inputStructBytes": unsafe.Sizeof(input{}), "mouseClickVerified": true, "unicodeTextVerified": true, "foregroundGuard": true}, "dpapi": dpapi()}
	b, e := json.MarshalIndent(result, "", "  ")
	must(e == nil, "json")
	must(os.WriteFile(os.Args[2], b, 0600) == nil, "write evidence")
	fmt.Println(string(b))
}
