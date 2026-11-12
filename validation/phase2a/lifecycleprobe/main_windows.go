// lifecycleprobe starts one Wails window with an isolated WebView2 profile.
// It uses the application's pinned module and has no database or bound service.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func run() int {
	profile := flag.String("profile", "", "fresh WebView2 user data path")
	closePath := flag.String("close-path", "window", "window or quit")
	flag.Parse()
	if !filepath.IsAbs(*profile) || (*closePath != "window" && *closePath != "quit") {
		slog.Error("probe.invalid_arguments")
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("pid", os.Getpid(), "probe", *closePath)
	logger.Info("probe.start", "bindings", 0, "database", false, "profile", *profile)
	desktop := application.New(application.Options{
		Name:     "DeskPilot Wails lifecycle probe",
		Logger:   logger,
		LogLevel: slog.LevelInfo,
		Windows:  application.WindowsOptions{WebviewUserDataPath: *profile},
		Assets: application.AssetOptions{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><html lang=en><title>Wails lifecycle probe</title><p>Local lifecycle probe</p>"))
		})},
		OnShutdown:   func() { logger.Info("probe.on_shutdown") },
		PostShutdown: func() { logger.Info("probe.post_shutdown") },
	})
	window := desktop.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "DeskPilot Wails lifecycle probe",
		Width:  640,
		Height: 420,
		URL:    "/",
	})
	window.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		logger.Info("probe.window_closing")
	})
	var scheduled sync.Once
	var closeRequested atomic.Bool
	window.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
		scheduled.Do(func() {
			logger.Info("probe.navigation_completed", "controller", "navigation observed")
			// Keep the fixture up briefly so the harness can sample its process tree.
			time.AfterFunc(time.Second, func() {
				closeRequested.Store(true)
				logger.Info("probe.close_requested")
				if *closePath == "window" {
					window.Close()
				} else {
					desktop.Quit()
				}
			})
		})
	})
	var timedOut atomic.Bool
	watchdog := time.AfterFunc(20*time.Second, func() {
		timedOut.Store(true)
		logger.Error("probe.watchdog")
		desktop.Quit()
	})
	defer watchdog.Stop()
	if err := desktop.Run(); err != nil {
		logger.Error("probe.run_failed", "error", err)
		return 1
	}
	logger.Info("probe.run_returned")
	if timedOut.Load() || !closeRequested.Load() {
		return 1
	}
	return 0
}

func main() { os.Exit(run()) }
