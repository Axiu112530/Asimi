package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func textRequest() Request {
	return Request{Model: Model{ID: "mock-model", Text: Supported, Vision: Unknown, Tools: Supported}, Prompt: "Observe fixture"}
}
func TestProviderNeutralMock(t *testing.T) {
	var p Provider = MockProvider{}
	r, e := p.Generate(context.Background(), textRequest())
	if e != nil || len(r.Actions) != 1 || r.Actions[0].Type != "screenshot" {
		t.Fatalf("mock result: %+v %v", r, e)
	}
}
func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := (MockProvider{}).Generate(ctx, textRequest())
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("got %v", e)
	}
}
func TestVisionFailsClosed(t *testing.T) {
	r := textRequest()
	r.Image = []byte{1}
	for _, state := range []Capability{Unknown, Unsupported} {
		r.Model.Vision = state
		if _, e := (MockProvider{}).Generate(context.Background(), r); e == nil {
			t.Fatal("vision accepted", state)
		}
	}
	r.Model.Vision = Supported
	if _, e := (MockProvider{}).Generate(context.Background(), r); e != nil {
		t.Fatal(e)
	}
}
func TestWireShapes(t *testing.T) {
	r := textRequest()
	r.Image = []byte{1, 2, 3}
	for _, protocol := range []string{"chat-completions", "responses"} {
		wire, e := encodeWire(protocol, r)
		if e != nil {
			t.Fatal(e)
		}
		var d map[string]any
		if e = json.Unmarshal(wire, &d); e != nil {
			t.Fatal(e)
		}
		if d["model"] != "mock-model" {
			t.Fatal(d)
		}
		key := "messages"
		if protocol == "responses" {
			key = "input"
		}
		if _, ok := d[key]; !ok {
			t.Fatal(d)
		}
	}
	if _, e := encodeWire("unrecognized", r); e == nil {
		t.Fatal("unknown protocol accepted")
	}
}
func TestStrictActionValidation(t *testing.T) {
	for _, raw := range []string{`{"type":"click","X":1,"Y":2,"coordinateSpace":"image"}`, `{"type":"screenshot"}`} {
		if _, e := validateAction([]byte(raw)); e != nil {
			t.Fatal(raw, e)
		}
	}
	for _, raw := range []string{`{"type":"shell"}`, `{"type":"click"}`, `{"type":"click","X":-1,"Y":2,"coordinateSpace":"image"}`, `{"type":"click","X":1,"Y":2,"coordinateSpace":"vendor-magic"}`, `{"type":"screenshot","policyOverride":true}`, `{"type":"screenshot"} {}`} {
		if _, e := validateAction([]byte(raw)); e == nil {
			t.Fatal("unsafe action accepted", raw)
		}
	}
}
func TestCropMappingAndNegativeMonitor(t *testing.T) {
	p, e := imagePoint(50, 25, 100, 50, rect{-1920, 0, -1720, 100})
	if e != nil || p.X != -1820 || p.Y != 50 {
		t.Fatalf("%+v %v", p, e)
	}
	if normalized(-1920, -1920, 3840) != 0 || normalized(1919, -1920, 3840) != 65535 {
		t.Fatal("virtual screen extrema")
	}
}
func TestRejectBadCoordinates(t *testing.T) {
	for _, x := range []float64{-1, 100, math.NaN(), math.Inf(1)} {
		if _, e := imagePoint(x, 0, 100, 100, rect{0, 0, 200, 200}); e == nil {
			t.Fatal(x)
		}
	}
	if _, e := imagePoint(0, 0, 0, 100, rect{0, 0, 200, 200}); e == nil {
		t.Fatal("empty image")
	}
}
