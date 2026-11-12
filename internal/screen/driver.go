// Package screen provides read-only display capture and screen-analysis types.
package screen

import (
	"context"
	"image"
)

const (
	MaxPixels    = 24_000_000
	MaxDimension = 8192
	defaultDPI   = 96
)

type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Display struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Primary        bool   `json:"primary"`
	PhysicalBounds Rect   `json:"physicalBounds"`
	LogicalBounds  Rect   `json:"logicalBounds"`
	DPI            int    `json:"dpi"`
	Scale          int    `json:"scale"` // Percent of 96 DPI.
}

type Driver interface {
	EnumerateDisplays(context.Context) ([]Display, error)
	CaptureDisplay(context.Context, string) (Display, *image.RGBA, error)
	// CaptureRegion takes a rectangle relative to the display's physical origin.
	CaptureRegion(context.Context, string, Rect) (Display, *image.RGBA, error)
}

type driverError string

func (e driverError) Error() string { return string(e) }
func (e driverError) Code() string  { return string(e) }

const (
	ErrCaptureFailed   = driverError("SCREEN_CAPTURE_FAILED")
	ErrDisplayNotFound = driverError("SCREEN_DISPLAY_NOT_FOUND")
	ErrRegionInvalid   = driverError("SCREEN_REGION_INVALID")
	ErrLimitExceeded   = driverError("SCREEN_LIMIT_EXCEEDED")
	ErrUnavailable     = driverError("SCREEN_UNAVAILABLE")
)

func physicalCaptureBounds(display Rect, region *Rect) (Rect, error) {
	if display.Width <= 0 || display.Height <= 0 {
		return Rect{}, ErrCaptureFailed
	}
	bounds := display
	if region != nil {
		// Subtract the dimensions before comparing offsets so input cannot overflow.
		if region.X < 0 || region.Y < 0 || region.Width <= 0 || region.Height <= 0 ||
			region.Width > display.Width || region.Height > display.Height ||
			region.X > display.Width-region.Width || region.Y > display.Height-region.Height {
			return Rect{}, ErrRegionInvalid
		}
		bounds = Rect{display.X + region.X, display.Y + region.Y, region.Width, region.Height}
	}
	if bounds.Width > MaxDimension || bounds.Height > MaxDimension || bounds.Width > MaxPixels/bounds.Height {
		return Rect{}, ErrLimitExceeded
	}
	return bounds, nil
}

// Logical bounds express a monitor's rectangle at 96 DPI for display metadata.
// Native capture always uses the physical desktop coordinate space.
func logicalBounds(physical Rect, dpi int) Rect {
	convert := func(pixels int) int {
		scaled := int64(pixels) * defaultDPI
		halfDPI := int64(dpi / 2)
		if scaled < 0 {
			halfDPI = -halfDPI
		}
		return int((scaled + halfDPI) / int64(dpi))
	}
	return Rect{convert(physical.X), convert(physical.Y), max(1, convert(physical.Width)), max(1, convert(physical.Height))}
}
