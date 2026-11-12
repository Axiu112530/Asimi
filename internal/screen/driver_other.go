//go:build !windows

package screen

import (
	"context"
	"image"
)

type unavailableDriver struct{}

func NewDriver() Driver { return unavailableDriver{} }

func (unavailableDriver) EnumerateDisplays(context.Context) ([]Display, error) {
	return nil, ErrUnavailable
}

func (unavailableDriver) CaptureDisplay(context.Context, string) (Display, *image.RGBA, error) {
	return Display{}, nil, ErrUnavailable
}

func (unavailableDriver) CaptureRegion(context.Context, string, Rect) (Display, *image.RGBA, error) {
	return Display{}, nil, ErrUnavailable
}
