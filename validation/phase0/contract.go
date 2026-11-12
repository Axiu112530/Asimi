// Package main contains disposable contract probes, not the Agent Runtime.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
)

type Capability string

const (
	Unknown     Capability = "unknown"
	Supported   Capability = "supported"
	Unsupported Capability = "unsupported"
)

type Model struct {
	ID                  string
	Text, Vision, Tools Capability
}
type Request struct {
	Model  Model
	Prompt string
	Image  []byte
}
type Action struct {
	Type            string `json:"type"`
	X, Y            *float64
	CoordinateSpace string `json:"coordinateSpace"`
}
type Result struct {
	Actions []Action
	Text    string
}
type Provider interface {
	Generate(context.Context, Request) (Result, error)
}
type MockProvider struct{}

func (MockProvider) Generate(ctx context.Context, r Request) (Result, error) {
	if e := ctx.Err(); e != nil {
		return Result{}, e
	}
	if r.Model.Text != Supported || r.Model.Tools != Supported {
		return Result{}, errors.New("capability unavailable")
	}
	if len(r.Image) > 0 && r.Model.Vision != Supported {
		return Result{}, errors.New("vision unverified")
	}
	return Result{Actions: []Action{{Type: "screenshot"}}}, nil
}

// Offline shape probes only; no network transport or provider credentials exist.
func encodeWire(protocol string, r Request) ([]byte, error) {
	var content []any
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(r.Image)
	switch protocol {
	case "chat-completions":
		content = append(content, map[string]any{"type": "text", "text": r.Prompt})
		if len(r.Image) > 0 {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": url}})
		}
		return json.Marshal(map[string]any{"model": r.Model.ID, "messages": []any{map[string]any{"role": "user", "content": content}}})
	case "responses":
		content = append(content, map[string]any{"type": "input_text", "text": r.Prompt})
		if len(r.Image) > 0 {
			content = append(content, map[string]any{"type": "input_image", "image_url": url})
		}
		return json.Marshal(map[string]any{"model": r.Model.ID, "input": []any{map[string]any{"role": "user", "content": content}}})
	default:
		return nil, errors.New("unknown wire protocol")
	}
}
func validateAction(raw []byte) (Action, error) {
	var a Action
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&a); e != nil {
		return a, e
	}
	if d.Decode(new(any)) != io.EOF {
		return a, errors.New("trailing data")
	}
	switch a.Type {
	case "screenshot":
		if a.X != nil || a.Y != nil || a.CoordinateSpace != "" {
			return a, errors.New("unexpected screenshot coordinates")
		}
	case "click":
		if a.X == nil || a.Y == nil || a.CoordinateSpace != "image" || *a.X < 0 || *a.Y < 0 || math.IsNaN(*a.X) || math.IsNaN(*a.Y) || math.IsInf(*a.X, 0) || math.IsInf(*a.Y, 0) {
			return a, errors.New("invalid click coordinates")
		}
	default:
		return a, errors.New("unregistered action")
	}
	return a, nil
}
func imagePoint(x, y float64, width, height int, crop rect) (point, error) {
	if width <= 0 || height <= 0 || crop.R <= crop.L || crop.B <= crop.T || x < 0 || y < 0 || x >= float64(width) || y >= float64(height) || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return point{}, errors.New("invalid image geometry")
	}
	return point{crop.L + int32(math.Floor(x*float64(crop.R-crop.L)/float64(width))), crop.T + int32(math.Floor(y*float64(crop.B-crop.T)/float64(height)))}, nil
}
