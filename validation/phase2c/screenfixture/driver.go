// Package screenfixture supplies the explicit Stage 7 in-memory display.
package screenfixture

import (
	"context"
	"image"
	"image/color"

	"deskpilot/internal/screen"
)

type Driver struct{}

var _ screen.Driver = Driver{}

const displayID = "phase2c-fixture"

func display() screen.Display {
	bounds := screen.Rect{X: -120, Y: 0, Width: 120, Height: 80}
	return screen.Display{
		ID: displayID, Name: "Synthetic display", Primary: true,
		PhysicalBounds: bounds, LogicalBounds: bounds, DPI: 96, Scale: 100,
	}
}

type failure string

func (e failure) Error() string { return string(e) }
func (e failure) Code() string  { return string(e) }

func (Driver) EnumerateDisplays(ctx context.Context) ([]screen.Display, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []screen.Display{display()}, nil
}

func (d Driver) CaptureDisplay(ctx context.Context, id string) (screen.Display, *image.RGBA, error) {
	bounds := display().PhysicalBounds
	return d.CaptureRegion(ctx, id, screen.Rect{Width: bounds.Width, Height: bounds.Height})
}

func (Driver) CaptureRegion(ctx context.Context, id string, rect screen.Rect) (screen.Display, *image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return screen.Display{}, nil, err
	}
	if id != displayID {
		return screen.Display{}, nil, failure("SCREEN_DISPLAY_NOT_FOUND")
	}
	fixture := display()
	if rect.X < 0 || rect.Y < 0 || rect.Width < 1 || rect.Height < 1 ||
		rect.Width > fixture.PhysicalBounds.Width-rect.X || rect.Height > fixture.PhysicalBounds.Height-rect.Y {
		return screen.Display{}, nil, failure("SCREEN_REGION_INVALID")
	}
	result := image.NewRGBA(image.Rect(0, 0, rect.Width, rect.Height))
	blue := color.RGBA{R: 28, G: 112, B: 184, A: 255}
	red := color.RGBA{R: 221, G: 61, B: 75, A: 255}
	for y := range rect.Height {
		for x := range rect.Width {
			pixel := blue
			if x+rect.X >= 60 && y+rect.Y >= 40 {
				pixel = red
			}
			result.SetRGBA(x, y, pixel)
		}
	}
	return fixture, result, nil
}
