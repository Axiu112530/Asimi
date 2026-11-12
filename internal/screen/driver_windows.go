package screen

import (
	"context"
	"image"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")
	shcore = windows.NewLazySystemDLL("shcore.dll")

	setThreadDPIContext = user32.NewProc("SetThreadDpiAwarenessContext")
	enumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	getMonitorInfo      = user32.NewProc("GetMonitorInfoW")
	getDC               = user32.NewProc("GetDC")
	releaseDC           = user32.NewProc("ReleaseDC")
	getDPIForMonitor    = shcore.NewProc("GetDpiForMonitor")
	getScaleForMonitor  = shcore.NewProc("GetScaleFactorForMonitor")

	createCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	createCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	selectObject           = gdi32.NewProc("SelectObject")
	bitBlt                 = gdi32.NewProc("BitBlt")
	getDIBits              = gdi32.NewProc("GetDIBits")
	deleteDC               = gdi32.NewProc("DeleteDC")
	deleteObject           = gdi32.NewProc("DeleteObject")
)

const (
	perMonitorAwareV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4).
	monitorPrimary    = 1
	sourceCopy        = 0x00CC0020 // SRCCOPY.
	captureLayered    = 0x40000000 // CAPTUREBLT.
)

type windowsDriver struct{}

func NewDriver() Driver { return windowsDriver{} }

func (windowsDriver) EnumerateDisplays(ctx context.Context) ([]Display, error) {
	var displays []Display
	err := onDPIThread(ctx, func() error {
		var err error
		displays, err = enumerateDisplays()
		return err
	})
	if err != nil {
		return nil, err
	}
	return displays, nil
}

func (d windowsDriver) CaptureDisplay(ctx context.Context, id string) (Display, *image.RGBA, error) {
	return d.capture(ctx, id, nil)
}

func (d windowsDriver) CaptureRegion(ctx context.Context, id string, region Rect) (Display, *image.RGBA, error) {
	return d.capture(ctx, id, &region)
}

func (windowsDriver) capture(ctx context.Context, id string, region *Rect) (Display, *image.RGBA, error) {
	var display Display
	var pixels *image.RGBA
	err := onDPIThread(ctx, func() error {
		displays, err := enumerateDisplays()
		if err != nil {
			return err
		}
		for _, candidate := range displays {
			if candidate.ID == id {
				display = candidate
				break
			}
		}
		if display.ID == "" {
			return ErrDisplayNotFound
		}
		bounds, err := physicalCaptureBounds(display.PhysicalBounds, region)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ErrCaptureFailed
		}
		screenDC, _, _ := getDC.Call(0)
		if screenDC == 0 {
			return ErrCaptureFailed
		}
		defer releaseDC.Call(0, screenDC)
		pixels, err = copyScreenPixels(screenDC, bounds)
		return err
	})
	if err != nil {
		if pixels != nil {
			clear(pixels.Pix)
		}
		return Display{}, nil, err
	}
	return display, pixels, nil
}

// Run on a dedicated goroutine so changes to the DPI context are confined to
// a pinned thread and cannot affect the caller's window or message loop.
func onDPIThread(ctx context.Context, operation func() error) error {
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	result := make(chan error)
	go func() {
		runtime.LockOSThread()
		var previous uintptr
		var err error
		defer func() {
			if previous != 0 {
				restored, _, _ := setThreadDPIContext.Call(previous)
				if restored == 0 {
					// Leaving the thread locked makes the runtime retire it.
					result <- ErrUnavailable
					return
				}
			}
			runtime.UnlockOSThread()
			result <- err
		}()
		if setThreadDPIContext.Find() != nil {
			err = ErrUnavailable
			return
		}
		previous, _, _ = setThreadDPIContext.Call(perMonitorAwareV2)
		if previous == 0 || ctx.Err() != nil {
			err = ErrUnavailable
			return
		}
		err = operation()
		if err == nil && ctx.Err() != nil {
			err = ErrCaptureFailed
		}
	}()
	return <-result
}

type monitorRect struct{ left, top, right, bottom int32 }

type monitorInfo struct {
	size    uint32
	monitor monitorRect
	work    monitorRect
	flags   uint32
	device  [32]uint16
}

type monitorEnumeration struct {
	displays []Display
}

// Windows retains each callback created by NewCallback for the life of the
// process. Keep one callback and pass the per-call state through LPARAM.
var monitorCallback = sync.OnceValue(func() uintptr {
	return windows.NewCallback(appendMonitor)
})

func enumerateDisplays() ([]Display, error) {
	var state monitorEnumeration
	ok, _, _ := enumDisplayMonitors.Call(0, 0, monitorCallback(), uintptr(unsafe.Pointer(&state)))
	if ok == 0 || len(state.displays) == 0 {
		return nil, ErrUnavailable
	}
	sort.Slice(state.displays, func(i, j int) bool {
		left, right := state.displays[i], state.displays[j]
		if left.Primary != right.Primary {
			return left.Primary
		}
		return left.ID < right.ID
	})
	return state.displays, nil
}

func appendMonitor(monitor, _, _ uintptr, state *monitorEnumeration) uintptr {
	info := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
	ok, _, _ := getMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return 0
	}
	id := windows.UTF16ToString(info.device[:])
	width := int64(info.monitor.right) - int64(info.monitor.left)
	height := int64(info.monitor.bottom) - int64(info.monitor.top)
	const maxCoordinate = 1<<31 - 1
	if id == "" || width <= 0 || height <= 0 || width > maxCoordinate || height > maxCoordinate {
		return 1
	}
	dpi, scale := monitorDPI(monitor)
	if dpi == 0 {
		return 0
	}
	physical := Rect{int(info.monitor.left), int(info.monitor.top), int(width), int(height)}
	state.displays = append(state.displays, Display{
		ID: id, Name: strings.TrimPrefix(id, `\\.\`), Primary: info.flags&monitorPrimary != 0,
		PhysicalBounds: physical, LogicalBounds: logicalBounds(physical, dpi), DPI: dpi, Scale: scale,
	})
	return 1
}

func monitorDPI(monitor uintptr) (dpi, scale int) {
	var effectiveX, effectiveY, deviceScale uint32
	if getDPIForMonitor.Find() == nil {
		// MDT_EFFECTIVE_DPI is the DPI used for Windows UI scaling.
		hr, _, _ := getDPIForMonitor.Call(monitor, 0, uintptr(unsafe.Pointer(&effectiveX)), uintptr(unsafe.Pointer(&effectiveY)))
		if int32(hr) >= 0 && effectiveX != 0 {
			dpi = int(effectiveX)
			scale = (dpi*100 + defaultDPI/2) / defaultDPI
		}
	}
	if getScaleForMonitor.Find() == nil {
		hr, _, _ := getScaleForMonitor.Call(monitor, uintptr(unsafe.Pointer(&deviceScale)))
		// GetDpiForMonitor can report process-virtualized DPI. The monitor's
		// scale factor lets us recover effective DPI in that case.
		if int32(hr) >= 0 && deviceScale != 0 && int(deviceScale) != scale {
			scale = int(deviceScale)
			dpi = (scale*defaultDPI + 50) / 100
		}
	}
	return dpi, scale
}

// BITMAPINFOHEADER, with no following color table for a 32-bit BI_RGB image.
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// copyScreenPixels copies from a caller-owned GDI source into a bounded image.
// The caller has already validated the exact physical rectangle.
func copyScreenPixels(source uintptr, bounds Rect) (*image.RGBA, error) {
	memoryDC, _, _ := createCompatibleDC.Call(source)
	if memoryDC == 0 {
		return nil, ErrCaptureFailed
	}
	var bitmap uintptr
	defer func() {
		// Deleting the memory DC releases its selection even on an error path.
		deleteDC.Call(memoryDC)
		if bitmap != 0 {
			deleteObject.Call(bitmap)
		}
	}()
	width, height := uintptr(bounds.Width), uintptr(bounds.Height)
	bitmap, _, _ = createCompatibleBitmap.Call(source, width, height)
	if bitmap == 0 {
		return nil, ErrCaptureFailed
	}
	old, _, _ := selectObject.Call(memoryDC, bitmap)
	if old == 0 || old == ^uintptr(0) {
		return nil, ErrCaptureFailed
	}
	copied, _, _ := bitBlt.Call(memoryDC, 0, 0, width, height, source, uintptr(int32(bounds.X)), uintptr(int32(bounds.Y)), sourceCopy|captureLayered)
	// GetDIBits requires that the bitmap be deselected from every DC.
	deselected, _, _ := selectObject.Call(memoryDC, old)
	if copied == 0 || deselected != bitmap {
		return nil, ErrCaptureFailed
	}
	img := image.NewRGBA(image.Rect(0, 0, bounds.Width, bounds.Height))
	header := bitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(width), Height: -int32(height),
		Planes: 1, BitCount: 32,
	}
	rows, _, _ := getDIBits.Call(memoryDC, bitmap, 0, height, uintptr(unsafe.Pointer(&img.Pix[0])), uintptr(unsafe.Pointer(&header)), 0)
	if rows != height {
		clear(img.Pix)
		return nil, ErrCaptureFailed
	}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i]
		img.Pix[i+3] = 255
	}
	return img, nil
}
