package screen

import (
	"errors"
	"testing"
)

func TestPhysicalCaptureBounds(t *testing.T) {
	negative := Rect{X: -1920, Y: -1080, Width: 1920, Height: 1080}
	for _, test := range []struct {
		name    string
		display Rect
		region  *Rect
		want    Rect
		code    string
	}{
		{name: "entire display", display: negative, want: negative},
		{name: "relative coordinates on negative desktop", display: negative, region: &Rect{10, 20, 30, 40}, want: Rect{-1910, -1060, 30, 40}},
		{name: "last pixel", display: negative, region: &Rect{1919, 1079, 1, 1}, want: Rect{-1, -1, 1, 1}},
		{name: "one pixel outside", display: negative, region: &Rect{1919, 1079, 2, 1}, code: "SCREEN_REGION_INVALID"},
		{name: "negative offset", display: negative, region: &Rect{-1, 0, 1, 1}, code: "SCREEN_REGION_INVALID"},
		{name: "zero height", display: negative, region: &Rect{0, 0, 1, 0}, code: "SCREEN_REGION_INVALID"},
		{name: "integer overflow in region extent", display: negative, region: &Rect{int(^uint(0) >> 1), 0, 2, 1}, code: "SCREEN_REGION_INVALID"},
		{name: "maximum pixel count", display: Rect{0, 0, 6000, 4000}, want: Rect{0, 0, 6000, 4000}},
		{name: "pixel limit", display: Rect{0, 0, 6000, 4001}, code: "SCREEN_LIMIT_EXCEEDED"},
		{name: "dimension limit", display: Rect{0, 0, 8193, 10}, code: "SCREEN_LIMIT_EXCEEDED"},
		{name: "small region of oversized display", display: Rect{0, 0, 10000, 6000}, region: &Rect{9900, 5900, 100, 100}, want: Rect{9900, 5900, 100, 100}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := physicalCaptureBounds(test.display, test.region)
			var coded interface{ Code() string }
			if test.code != "" {
				if !errors.As(err, &coded) || coded.Code() != test.code {
					t.Fatalf("got error %v, want %s", err, test.code)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("got %+v, %v, want %+v", got, err, test.want)
			}
		})
	}
}

func TestLogicalBoundsUseEffectiveDPI(t *testing.T) {
	physical := Rect{X: -1920, Y: -900, Width: 1920, Height: 1200}
	if got := logicalBounds(physical, 144); got != (Rect{-1280, -600, 1280, 800}) {
		t.Fatalf("got %+v for 150%% monitor", got)
	}
	if got := logicalBounds(Rect{-2, 3, 1001, 1001}, 120); got != (Rect{-2, 2, 801, 801}) {
		t.Fatalf("got %+v for fractional logical bounds", got)
	}
}
