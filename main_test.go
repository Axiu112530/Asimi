package main

import (
	"path/filepath"
	"testing"

	backend "asimi/internal/app"
	"asimi/internal/tools"
)

func TestReleaseDesktopServiceRejectsDemoRequests(t *testing.T) {
	dir := t.TempDir()
	registry, err := tools.New(filepath.Join(dir, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	app, err := backend.New(dir, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	service := &DesktopService{service: app}
	for _, method := range []string{"SendMessage", "CreateTask", "ListModels", "GetModelCapabilities"} {
		t.Run(method, func(t *testing.T) {
			reply := service.Call(method, "{}")
			if reply.Error == nil || reply.Error.Code != "METHOD_NOT_ALLOWED" {
				t.Fatalf("demo request %q returned %+v", method, reply)
			}
		})
	}
	providers := service.Call("ListProviders", "{}")
	if items, ok := providers.Data.([]any); providers.Error != nil || !ok || len(items) != 0 {
		t.Fatalf("demo provider catalog exposed: %+v", providers)
	}
	info := service.Call("GetAppInfo", "{}")
	data, ok := info.Data.(map[string]any)
	if info.Error != nil || !ok || data["version"] == nil {
		t.Fatalf("application information missing: %+v", info)
	}
	for _, field := range []string{"mode", "provider", "model"} {
		if _, found := data[field]; found {
			t.Errorf("demo metadata exposed in %s", field)
		}
	}
}
