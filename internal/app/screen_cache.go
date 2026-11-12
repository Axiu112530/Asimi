package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"sync"
	"time"

	"deskpilot/internal/screen"
)

const (
	screenTTL      = 2 * time.Minute
	maxScreenBytes = 12 << 20
	maxScreenCache = 2
	screenBackend  = "WIN32_GDI"
)

type screenPreview struct {
	screen.Metadata
	PreviewDataURL string `json:"previewDataUrl"`
	ExpiresAt      string `json:"expiresAt"`
}

type screenEntry struct {
	metadata  screen.Metadata
	pixels    []byte
	expiresAt time.Time
	timer     *time.Timer
	consent   *screenConsent
}

type screenCache struct {
	mu      sync.Mutex
	driver  screen.Driver
	backend string
	entries map[string]*screenEntry
	working bool
	closed  bool
}

func (c *screenCache) backendName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.backend != "" {
		return c.backend
	}
	return screenBackend
}

func (c *screenCache) beginWork() (screen.Driver, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, codedError("SERVICE_CLOSED")
	}
	if c.working {
		return nil, codedError("SCREEN_CAPTURE_BUSY")
	}
	c.working = true
	if c.driver == nil {
		c.driver = screen.NewDriver()
	}
	return c.driver, nil
}

func (c *screenCache) endWork() {
	c.mu.Lock()
	c.working = false
	c.mu.Unlock()
}

func (c *screenCache) removeLocked(id string) bool {
	entry := c.entries[id]
	if entry == nil {
		return false
	}
	if entry.timer != nil {
		entry.timer.Stop()
	}
	clear(entry.pixels)
	delete(c.entries, id)
	return true
}

func (c *screenCache) pruneLocked() {
	now := time.Now()
	for id, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			c.removeLocked(id)
		}
	}
}

func (c *screenCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for id := range c.entries {
		c.removeLocked(id)
	}
}

func (c *screenCache) addLocked(entry *screenEntry) (screenPreview, error) {
	c.pruneLocked()
	if c.closed {
		clear(entry.pixels)
		return screenPreview{}, codedError("SERVICE_CLOSED")
	}
	if !time.Now().Before(entry.expiresAt) {
		clear(entry.pixels)
		return screenPreview{}, codedError("SCREEN_CAPTURE_EXPIRED")
	}
	if len(c.entries) >= maxScreenCache {
		clear(entry.pixels)
		return screenPreview{}, codedError("SCREEN_LIMIT_EXCEEDED")
	}
	if c.entries == nil {
		c.entries = make(map[string]*screenEntry)
	}
	id := entry.metadata.CaptureID
	c.entries[id] = entry
	entry.timer = time.AfterFunc(time.Until(entry.expiresAt), func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.removeLocked(id)
	})
	return screenPreview{
		Metadata: entry.metadata, PreviewDataURL: entry.dataURL(),
		ExpiresAt: entry.expiresAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (e *screenEntry) dataURL() string {
	return "data:image/" + e.metadata.Format + ";base64," + base64.StdEncoding.EncodeToString(e.pixels)
}

func encodeScreen(im image.Image, quality int) ([]byte, string, error) {
	var buffer limitedImage
	format := "png"
	var err error
	if quality == 0 {
		err = png.Encode(&buffer, im)
	} else {
		format = "jpeg"
		err = jpeg.Encode(&buffer, im, &jpeg.Options{Quality: quality})
	}
	if err != nil {
		clear(buffer.Bytes())
		return nil, "", codedError("SCREEN_LIMIT_EXCEEDED")
	}
	return buffer.Bytes(), format, nil
}

type limitedImage struct{ bytes.Buffer }

func (b *limitedImage) Write(p []byte) (int, error) {
	if len(p) > maxScreenBytes-b.Len() {
		return 0, codedError("SCREEN_LIMIT_EXCEEDED")
	}
	return b.Buffer.Write(p)
}

func applyScreenEncoding(metadata *screen.Metadata, pixels []byte, format string) {
	hash := sha256.Sum256(pixels)
	metadata.SHA256 = hex.EncodeToString(hash[:])
	metadata.Format = format
	metadata.ByteSize = len(pixels)
}

func screenRect(r screen.Rect, width, height int) (image.Rectangle, error) {
	if r.X < 0 || r.Y < 0 || r.Width <= 0 || r.Height <= 0 ||
		r.X >= width || r.Y >= height || r.Width > width-r.X || r.Height > height-r.Y {
		return image.Rectangle{}, codedError("SCREEN_REGION_INVALID")
	}
	return image.Rect(r.X, r.Y, r.X+r.Width, r.Y+r.Height), nil
}

func setScreenRegion(metadata *screen.Metadata, display screen.Display, region image.Rectangle) {
	metadata.PhysicalBounds.X = display.PhysicalBounds.X + region.Min.X
	metadata.PhysicalBounds.Y = display.PhysicalBounds.Y + region.Min.Y
	metadata.PhysicalBounds.Width = region.Dx()
	metadata.PhysicalBounds.Height = region.Dy()
	metadata.LogicalBounds = display.LogicalBounds
	if display.DPI > 0 {
		metadata.LogicalBounds.X += region.Min.X * 96 / display.DPI
		metadata.LogicalBounds.Y += region.Min.Y * 96 / display.DPI
		metadata.LogicalBounds.Width = (region.Dx()*96 + display.DPI - 1) / display.DPI
		metadata.LogicalBounds.Height = (region.Dy()*96 + display.DPI - 1) / display.DPI
	}
	metadata.Width = region.Dx()
	metadata.Height = region.Dy()
}

func transformScreen(im image.Image, region image.Rectangle, masks []screen.Rect) (*image.RGBA, error) {
	target := image.NewRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	draw.Draw(target, target.Bounds(), im, region.Min, draw.Src)
	for _, mask := range masks {
		area, err := screenRect(mask, region.Dx(), region.Dy())
		if err != nil {
			clear(target.Pix)
			return nil, err
		}
		draw.Draw(target, area, image.NewUniform(color.Black), image.Point{}, draw.Src)
	}
	return target, nil
}
