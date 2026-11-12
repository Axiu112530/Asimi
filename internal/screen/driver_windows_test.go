package screen

import (
	"context"
	"image/color"
	"os"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestSyntheticGDICaptureReleasesHandles(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	const fixtureWidth, fixtureHeight = 4, 3
	header := bitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: fixtureWidth, Height: -fixtureHeight,
		Planes: 1, BitCount: 32,
	}
	var bits unsafe.Pointer
	createDIBSection := windows.NewLazySystemDLL("gdi32.dll").NewProc("CreateDIBSection")
	bitmap, _, _ := createDIBSection.Call(0, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 {
		t.Fatal("CreateDIBSection failed for synthetic fixture")
	}
	defer deleteObject.Call(bitmap)
	source, _, _ := createCompatibleDC.Call(0)
	if source == 0 {
		t.Fatal("CreateCompatibleDC failed for synthetic fixture")
	}
	defer deleteDC.Call(source)
	old, _, _ := selectObject.Call(source, bitmap)
	if old == 0 || old == ^uintptr(0) {
		t.Fatal("SelectObject failed for synthetic fixture")
	}
	defer selectObject.Call(source, old)
	copy(unsafe.Slice((*byte)(bits), fixtureWidth*fixtureHeight*4), []byte{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 1, 2, 3, 0, 4, 5, 6, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 7, 8, 9, 0, 10, 11, 12, 0, 0, 0, 0, 0,
	})
	// A viewport origin makes negative source coordinates address the fixture.
	setViewportOrigin := gdi32.NewProc("SetViewportOrgEx")
	if ok, _, _ := setViewportOrigin.Call(source, 10, 20, 0); ok == 0 {
		t.Fatal("SetViewportOrgEx failed for synthetic fixture")
	}

	getGuiResources := windows.NewLazySystemDLL("user32.dll").NewProc("GetGuiResources")
	guiResources := func() uintptr {
		count, _, _ := getGuiResources.Call(uintptr(windows.CurrentProcess()), 0)
		return count
	}
	want := [4]color.RGBA{{R: 3, G: 2, B: 1, A: 255}, {R: 6, G: 5, B: 4, A: 255}, {R: 9, G: 8, B: 7, A: 255}, {R: 12, G: 11, B: 10, A: 255}}
	before := guiResources()
	if before < 2 {
		t.Fatalf("GetGuiResources failed to count the fixture bitmap and DC (%d)", before)
	}
	const samples = 128
	for i := 0; i < samples; i++ {
		img, err := copyScreenPixels(source, Rect{-9, -19, 2, 2})
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 2 {
			t.Fatalf("unexpected dimensions %v", img.Bounds())
		}
		for y := range 2 {
			for x := range 2 {
				if got := img.RGBAAt(x, y); got != want[y*2+x] {
					t.Fatalf("at (%d,%d) got %v, want %v", x, y, got, want[y*2+x])
				}
			}
		}
	}
	after := guiResources()
	if before != after {
		t.Fatalf("GDI resources before=%d, after=%d", before, after)
	}
	t.Logf("%d synthetic GDI captures, handles before=%d after=%d", samples, before, after)
}

func TestNativeDisplayEnumeration(t *testing.T) {
	if os.Getenv("ASIMI_SCREEN_NATIVE_TEST") != "1" {
		t.Skip("set ASIMI_SCREEN_NATIVE_TEST=1 to enumerate the current displays")
	}
	driver := NewDriver()
	ctx := context.Background()
	first, err := driver.EnumerateDisplays(ctx)
	if err != nil || len(first) == 0 {
		t.Fatalf("enumerating displays: count=%d, err=%v", len(first), err)
	}
	second, err := driver.EnumerateDisplays(ctx)
	if err != nil || len(second) != len(first) {
		t.Fatalf("re-enumerating displays: count=%d, err=%v", len(second), err)
	}
	seen := make(map[string]bool)
	for i, display := range first {
		if display.ID == "" || display.Name == "" || seen[display.ID] || second[i] != display ||
			display.DPI <= 0 || display.Scale <= 0 || display.PhysicalBounds.Width <= 0 || display.PhysicalBounds.Height <= 0 ||
			display.LogicalBounds.Width <= 0 || display.LogicalBounds.Height <= 0 {
			t.Fatalf("invalid or unstable display metadata at index %d", i)
		}
		seen[display.ID] = true
		t.Logf("display %d, physical=%dx%d, logical=%dx%d, dpi=%d, scale=%d%%", i, display.PhysicalBounds.Width, display.PhysicalBounds.Height, display.LogicalBounds.Width, display.LogicalBounds.Height, display.DPI, display.Scale)
	}
}
