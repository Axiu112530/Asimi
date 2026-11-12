package computer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"asimi/internal/screen"
	"golang.org/x/sys/windows"
)

const (
	mediumIntegrity = 0x2000
	fixtureTitle    = "Asimi Control Test Host"
	notepadFamily   = "Microsoft.WindowsNotepad_8wekyb3d8bbwe"
)

var (
	controlUser32       = windows.NewLazySystemDLL("user32.dll")
	controlKernel32     = windows.NewLazySystemDLL("kernel32.dll")
	enumWindows         = controlUser32.NewProc("EnumWindows")
	getWindowPID        = controlUser32.NewProc("GetWindowThreadProcessId")
	isWindowVisible     = controlUser32.NewProc("IsWindowVisible")
	isWindowEnabled     = controlUser32.NewProc("IsWindowEnabled")
	isIconic            = controlUser32.NewProc("IsIconic")
	getWindow           = controlUser32.NewProc("GetWindow")
	getLastActivePopup  = controlUser32.NewProc("GetLastActivePopup")
	getForegroundWindow = controlUser32.NewProc("GetForegroundWindow")
	setForegroundWindow = controlUser32.NewProc("SetForegroundWindow")
	getClassName        = controlUser32.NewProc("GetClassNameW")
	getWindowText       = controlUser32.NewProc("GetWindowTextW")
	getWindowRect       = controlUser32.NewProc("GetWindowRect")
	threadDPI           = controlUser32.NewProc("SetThreadDpiAwarenessContext")
	getPackageFamily    = controlKernel32.NewProc("GetPackageFamilyName")
	getPackageFull      = controlKernel32.NewProc("GetPackageFullName")
	getAppModelID       = controlKernel32.NewProc("GetApplicationUserModelId")
)

type processIdentity struct {
	path, user, creation string
	session, integrity   uint32
}

func queryProcess(handle windows.Handle, pid uint32) (processIdentity, bool) {
	var identity processIdentity
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(handle, &created, &exited, &kernel, &user) != nil ||
		windows.ProcessIdToSessionId(pid, &identity.session) != nil || identity.session == 0 {
		return processIdentity{}, false
	}
	var image [32768]uint16
	size := uint32(len(image))
	if windows.QueryFullProcessImageName(handle, 0, &image[0], &size) != nil || size == 0 {
		return processIdentity{}, false
	}
	identity.path = windows.UTF16ToString(image[:size])
	var token windows.Token
	if windows.OpenProcessToken(handle, windows.TOKEN_QUERY, &token) != nil {
		return processIdentity{}, false
	}
	defer token.Close()
	var uiAccess, used uint32
	if windows.GetTokenInformation(token, windows.TokenUIAccess, (*byte)(unsafe.Pointer(&uiAccess)), 4, &used) != nil || uiAccess != 0 {
		return processIdentity{}, false
	}
	var label [128]uintptr
	if windows.GetTokenInformation(token, windows.TokenIntegrityLevel, (*byte)(unsafe.Pointer(&label[0])), uint32(unsafe.Sizeof(label)), &used) != nil {
		return processIdentity{}, false
	}
	sid := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&label[0])).Label.Sid
	if sid == nil || !sid.IsValid() || sid.SubAuthorityCount() != 1 {
		return processIdentity{}, false
	}
	identity.integrity = sid.SubAuthority(0)
	owner, err := token.GetTokenUser()
	if err != nil || owner.User.Sid == nil {
		return processIdentity{}, false
	}
	identity.user = owner.User.Sid.String()
	identity.creation = strconv.FormatUint(uint64(created.HighDateTime)<<32|uint64(created.LowDateTime), 10)
	return identity, identity.user != "" && identity.creation != "0"
}

type windowScan struct {
	ctx     context.Context
	handles []uintptr
}

// Keep one Win32 callback trampoline for the process. Each enumeration has a
// unique integer key and passes no Go pointer through the operating system.
var (
	windowScans    sync.Map
	scanMu         sync.Mutex
	scanKey        uintptr
	windowCallback = sync.OnceValue(func() uintptr {
		return windows.NewCallback(func(hwnd, key uintptr) uintptr {
			value, ok := windowScans.Load(key)
			if !ok {
				return 0
			}
			scan := value.(*windowScan)
			if scan.ctx.Err() != nil || len(scan.handles) >= 4096 {
				return 0
			}
			visible, _, _ := isWindowVisible.Call(hwnd)
			if visible != 0 {
				scan.handles = append(scan.handles, hwnd)
			}
			return 1
		})
	})
)

func listWindowHandles(ctx context.Context) ([]uintptr, error) {
	scanMu.Lock()
	scanKey++
	key := scanKey
	scanMu.Unlock()
	scan := &windowScan{ctx: ctx}
	windowScans.Store(key, scan)
	defer windowScans.Delete(key)
	ok, _, _ := enumWindows.Call(windowCallback(), key)
	if ok == 0 || ctx.Err() != nil {
		return nil, Error("CONTROL_QUERY_TIMEOUT")
	}
	return scan.handles, nil
}

func windowString(proc *windows.LazyProc, hwnd uintptr) string {
	var buf [1024]uint16
	n, _, _ := proc.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || n >= uintptr(len(buf)-1) {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func processString(proc *windows.LazyProc, handle windows.Handle) string {
	if proc.Find() != nil {
		return ""
	}
	var buf [256]uint16
	size := uint32(len(buf))
	r, _, _ := proc.Call(uintptr(handle), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 || size == 0 || size > uint32(len(buf)) {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

func windowPID(hwnd uintptr) uint32 {
	var pid uint32
	thread, _, _ := getWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if thread == 0 {
		return 0
	}
	return pid
}

func windowBounds(hwnd uintptr) (screen.Rect, bool) {
	if threadDPI.Find() != nil {
		return screen.Rect{}, false
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	old, _, _ := threadDPI.Call(^uintptr(3)) // Per-monitor aware v2.
	if old == 0 {
		return screen.Rect{}, false
	}
	defer threadDPI.Call(old)
	var r struct{ left, top, right, bottom int32 }
	ok, _, _ := getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ok == 0 || r.right <= r.left || r.bottom <= r.top {
		return screen.Rect{}, false
	}
	return screen.Rect{X: int(r.left), Y: int(r.top), Width: int(r.right) - int(r.left), Height: int(r.bottom) - int(r.top)}, true
}

func (d *windowsDriver) observeWindow(hwnd uintptr) (Window, bool) {
	pid := windowPID(hwnd)
	if pid == 0 || pid == uint32(os.Getpid()) {
		return Window{}, false
	}
	class := windowString(getClassName, hwnd)
	owned := d.ownedHost()
	fixture := owned != nil && owned.pid == pid && strings.HasPrefix(class, "WindowsForms10.Window.8.app.")
	if !fixture && class != "Notepad" {
		return Window{}, false
	}
	visible, _, _ := isWindowVisible.Call(hwnd)
	owner, _, _ := getWindow.Call(hwnd, 4) // GW_OWNER.
	if visible == 0 || owner != 0 {
		return Window{}, false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return Window{}, false
	}
	defer windows.CloseHandle(h)
	id, ok := queryProcess(h, pid)
	if !ok || id.session != d.self.session || id.user != d.self.user || id.integrity != mediumIntegrity {
		return Window{}, false
	}
	w := Window{
		HWND: strconv.FormatUint(uint64(hwnd), 10), PID: pid,
		CreationTime: id.creation, SessionID: id.session, Integrity: id.integrity, ClassName: class,
	}
	switch {
	case fixture:
		if owned.creation != id.creation || !strings.EqualFold(owned.path, id.path) {
			return Window{}, false
		}
		w.App, w.ImagePath, w.ImageHash = TestHost, owned.path, hostSHA256
	case class == "Notepad":
		w.PackageFamily = processString(getPackageFamily, h)
		w.AppUserModelID = processString(getAppModelID, h)
		fullName := processString(getPackageFull, h)
		expected := filepath.Join(d.programFiles, "WindowsApps", fullName, "Notepad", "Notepad.exe")
		if d.programFiles == "" || w.PackageFamily != notepadFamily || w.AppUserModelID != notepadFamily+"!App" ||
			!strings.HasPrefix(fullName, "Microsoft.WindowsNotepad_") || !strings.HasSuffix(fullName, "_8wekyb3d8bbwe") ||
			filepath.Base(fullName) != fullName || !strings.EqualFold(expected, id.path) {
			return Window{}, false
		}
		w.ImagePath, err = canonicalFile(id.path)
		if err != nil || !strings.EqualFold(expected, w.ImagePath) {
			return Window{}, false
		}
		w.App = Notepad
	}
	w.Title = windowString(getWindowText, hwnd)
	w.Bounds, ok = windowBounds(hwnd)
	if !ok || w.Title == "" || (fixture && w.Title != fixtureTitle) || windowPID(hwnd) != pid {
		return Window{}, false
	}
	foreground, _, _ := getForegroundWindow.Call()
	w.Foreground = foreground == hwnd
	w.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	w.ID = fingerprint([]any{w.App, w.HWND, w.PID, w.CreationTime, w.ImagePath})
	return w, true
}

func sameWindow(a, b Window) bool {
	a.ObservedAt, b.ObservedAt = "", ""
	a.Foreground, b.Foreground = false, false
	return a == b
}

func fingerprint(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
