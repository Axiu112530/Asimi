// Asimi is the Windows desktop entry point.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	backend "asimi/internal/app"
	"asimi/internal/domain"
	"asimi/internal/tools"
	"asimi/validation/phase2c/screenfixture"
	"asimi/validation/v02/providerprobe"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/sys/windows"
)

//go:embed frontend/dist
var webAssets embed.FS

//go:embed validation/phase1/acceptance.js
var acceptanceScript []byte

//go:embed validation/phase2a/lifecycle-pending.js
var lifecyclePendingScript []byte

//go:embed validation/phase2b/acceptance.js
var byokAcceptanceScript []byte

//go:embed validation/phase2c/acceptance.js
var screenAcceptanceScript []byte

//go:embed validation/phase2d/acceptance.js
var controlAcceptanceScript []byte

//go:embed validation/mvp01/acceptance.js
var mvpAcceptanceScript []byte

//go:embed validation/v02/agent-acceptance.js
var agent02AcceptanceScript []byte

//go:embed validation/v021/acceptance.js
var presetAcceptanceScript []byte

//go:embed validation/v021/soak.js
var presetSoakScript []byte

const pendingCloseStage = 4
const screenAcceptanceStage = 7
const productName = "Asimi"
const productDescription = "Asimi is a local-first AI desktop assistant for developers, combining AI conversations, project understanding, code changes, controlled computer interaction, and secure local execution."

// DesktopService exposes only the explicitly allowlisted business dispatcher.
// No native file, shell, Windows, network, key retrieval or state setter is bound.
type DesktopService struct {
	service              *backend.Service
	connectPresetFixture func(string) (domain.Reply, bool)
}

func (s *DesktopService) Call(method, payload string) domain.Reply {
	if method == "ConnectPresetProvider" && s.connectPresetFixture != nil {
		if reply, handled := s.connectPresetFixture(payload); handled {
			return reply
		}
	}
	return s.service.Call(method, payload)
}

// AcceptanceService is NEVER registered in an ordinary launch.
type AcceptanceService struct {
	report      string
	requestExit func(string)
	once        sync.Once
}

func (s *AcceptanceService) Metrics() map[string]uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return map[string]uint64{"goroutines": uint64(runtime.NumGoroutine()), "heapAllocatedBytes": stats.HeapAlloc, "heapInUseBytes": stats.HeapInuse}
}

func (s *AcceptanceService) Report(payload string) bool {
	success := false
	s.once.Do(func() {
		var value map[string]any
		if json.Unmarshal([]byte(payload), &value) != nil {
			return
		}
		if err := os.WriteFile(s.report, []byte(payload), 0600); err != nil {
			slog.Error("acceptance.write_failed")
			return
		}
		success = true
		if s.requestExit != nil {
			s.requestExit("acceptance_report")
		}
	})
	return success
}

func main() { os.Exit(run()) }

func queryEscape(value string) string { return url.QueryEscape(value) }

func isScreenFixtureURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Path != "/v1" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	return err == nil && port != 0
}

func run() int {
	var dataDir, report, fixtureURL, providerReport string
	var acceptance, liveAgent, livePreset, gitFixture, presetProfileAgent bool
	var debugPort, acceptanceStage, soakSeconds int
	flag.StringVar(&dataDir, "data-dir", "", "Application-owned data directory")
	flag.BoolVar(&acceptance, "acceptance", false, "Enable own-WebView acceptance driver")
	flag.StringVar(&report, "acceptance-report", "", "Acceptance report output path")
	flag.IntVar(&debugPort, "acceptance-debug-port", 0, "Loopback WebView debugging, acceptance only")
	flag.IntVar(&acceptanceStage, "acceptance-stage", 1, "Acceptance restart stage")
	flag.StringVar(&fixtureURL, "acceptance-fixture", "", "Local provider fixture, acceptance only")
	flag.StringVar(&providerReport, "acceptance-provider", "", "Run the bounded official DeepSeek acceptance and write its redacted report")
	flag.BoolVar(&liveAgent, "acceptance-live-agent", false, "Enable bounded DeepSeek planning for the isolated Stage 12 project")
	flag.BoolVar(&livePreset, "acceptance-preset-live", false, "Enable one DeepSeek connection check for the isolated Stage 16 profile")
	flag.BoolVar(&gitFixture, "acceptance-git-fixture", false, "Check dirty Git context in the isolated Agent acceptance fixture")
	flag.BoolVar(&presetProfileAgent, "acceptance-preset-profile-agent", false, "Use the isolated verified Preset profile for Stage 12")
	flag.IntVar(&soakSeconds, "acceptance-soak-seconds", 0, "Duration of isolated loopback stability acceptance")
	flag.Parse()
	if providerReport != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		result, err := providerprobe.Run(ctx)
		data, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil || os.WriteFile(providerReport, data, 0600) != nil || err != nil {
			return 1
		}
		return 0
	}
	if liveAgent && (!acceptance || acceptanceStage != 12 || dataDir == "") {
		return 1
	}
	if presetProfileAgent && (!acceptance || acceptanceStage != 12 || dataDir == "" || !gitFixture || liveAgent) {
		return 1
	}
	if acceptance && acceptanceStage == 12 && !liveAgent && !presetProfileAgent {
		return 1
	}
	if gitFixture && (!acceptance || acceptanceStage < 11 || acceptanceStage > 13 || dataDir == "") {
		return 1
	}
	if livePreset != (acceptance && acceptanceStage == 16) || livePreset && dataDir == "" {
		return 1
	}
	presetAcceptance := acceptance && acceptanceStage >= 14 && acceptanceStage <= 18
	soakAcceptance := acceptance && acceptanceStage == 19
	if soakAcceptance && (dataDir == "" || !isScreenFixtureURL(fixtureURL) || soakSeconds < 120 || soakSeconds > 14400) ||
		!soakAcceptance && soakSeconds != 0 {
		return 1
	}
	if acceptance && acceptanceStage == 11 && !isScreenFixtureURL(fixtureURL) {
		return 1
	}
	if acceptance && report == "" {
		slog.Error("acceptance.report_required")
		return 1
	}
	screenAcceptance := acceptance && acceptanceStage == screenAcceptanceStage
	if screenAcceptance && !isScreenFixtureURL(fixtureURL) {
		slog.Error("acceptance.loopback_fixture_required")
		return 1
	}
	if dataDir == "" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			slog.Error("startup.local_data_unavailable")
			return 1
		}
		// New installations use the product-named profile directory.
		dataDir = filepath.Join(base, "Asimi")
	}
	var err error
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		panic(err)
	}
	// The data directory comes only from the trusted launcher/default, never a
	// model or UI request. Tool initialization then pins/rejects its entire chain
	// before any log, database or user file is written.
	if err = os.MkdirAll(dataDir, 0700); err != nil {
		slog.Error("startup.data_unavailable")
		return 1
	}
	registry, err := tools.New(filepath.Join(dataDir, "workspace"))
	if err != nil {
		slog.Error("startup.workspace_unavailable", "error", err)
		return 1
	}
	defer registry.Close()
	runtimeLog, err := os.OpenFile(filepath.Join(dataDir, "runtime.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		slog.Error("startup.log_unavailable")
		return 1
	}
	defer runtimeLog.Close()
	var logOutput io.Writer = runtimeLog
	stderr, stderrErr := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	if stderrErr == nil && stderr != 0 && stderr != windows.InvalidHandle {
		logOutput = io.MultiWriter(runtimeLog, os.Stderr)
	} else {
		os.Stderr = runtimeLog
		windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(runtimeLog.Fd()))
	}
	logger := slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: slog.LevelInfo})).With("pid", os.Getpid())
	slog.SetDefault(logger)
	var desktop *application.App
	var eventTarget atomic.Pointer[application.App]
	service, err := backend.New(dataDir, registry, func(name string, value any) {
		if target := eventTarget.Load(); target != nil {
			target.Event.Emit(name, value)
		}
	})
	if err != nil {
		slog.Error("startup.storage_unavailable", "error", err)
		return 1
	}
	defer service.Close()
	desktopService := &DesktopService{service: service}
	if livePreset {
		key := []byte(os.Getenv("DEEPSEEK_API_KEY"))
		_ = os.Unsetenv("DEEPSEEK_API_KEY")
		defer clear(key)
		if len(key) == 0 {
			slog.Error("acceptance.credential_unavailable")
			return 1
		}
		var connectOnce sync.Once
		var connectErr error
		desktopService.connectPresetFixture = func(payload string) (domain.Reply, bool) {
			if payload != `{"presetId":"deepseek","secret":"preset-native-acceptance-placeholder"}` {
				return domain.Reply{}, false
			}
			connectOnce.Do(func() {
				defer clear(key)
				if connectErr = service.ConnectPresetAcceptance(key); connectErr != nil {
					slog.Error("acceptance.preset_connect_failed")
				}
			})
			if connectErr != nil {
				return domain.Reply{Error: &domain.Error{Code: "PROVIDER_UNAVAILABLE"}}, true
			}
			return service.Call("GetPresetProvider", "{}"), true
		}
	}
	if liveAgent {
		if err = service.EnableAgent02LiveAcceptance(); err != nil {
			slog.Error("acceptance.credential_unavailable")
			return 1
		}
	}
	if screenAcceptance {
		service.SetAcceptanceScreenDriver(screenfixture.Driver{})
	}
	logger.Info("lifecycle.storage_open")
	files, err := fs.Sub(webAssets, "frontend/dist")
	if err != nil {
		panic(err)
	}
	fileServer := application.AssetFileServerFS(files)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent app-rendered content from loading remote resources or active HTML.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; font-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-src 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/acceptance.js" {
			if !acceptance {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/javascript")
			script := acceptanceScript
			if acceptanceStage == pendingCloseStage {
				script = lifecyclePendingScript
			}
			if acceptanceStage == 5 || acceptanceStage == 6 {
				script = byokAcceptanceScript
			}
			if screenAcceptance {
				script = screenAcceptanceScript
			}
			if acceptanceStage == 8 {
				script = controlAcceptanceScript
			}
			if acceptanceStage == 9 || acceptanceStage == 10 {
				script = mvpAcceptanceScript
			}
			if acceptanceStage == 11 || acceptanceStage == 12 || acceptanceStage == 13 {
				script = agent02AcceptanceScript
			}
			if presetAcceptance {
				script = presetAcceptanceScript
			}
			if soakAcceptance {
				script = presetSoakScript
			}
			_, _ = w.Write(script)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
	services := []application.Service{application.NewService(desktopService)}
	var acceptanceService *AcceptanceService
	if acceptance {
		acceptanceService = &AcceptanceService{report: report}
		services = append(services, application.NewService(acceptanceService))
	}
	windows := application.WindowsOptions{
		WebviewUserDataPath:           filepath.Join(dataDir, "webview-profile"),
		DisableQuitOnLastWindowClosed: true,
	}
	if acceptance && debugPort > 0 {
		windows.AdditionalBrowserArgs = []string{fmt.Sprintf("--remote-debugging-port=%d", debugPort), "--remote-debugging-address=127.0.0.1"}
	}
	var window *application.WebviewWindow
	var stopping, windowCloseAllowed, quitAllowed, fatalExit atomic.Bool
	drained := make(chan struct{})
	runReturned := make(chan struct{})
	var drainErr error
	var watchdog *time.Timer
	beginDrain := func(reason string) bool {
		if !stopping.CompareAndSwap(false, true) {
			return false
		}
		eventTarget.Store(nil)
		service.BeginShutdown()
		if watchdog != nil {
			watchdog.Stop()
		}
		if reason == "acceptance_timeout" {
			fatalExit.Store(true)
			logger.Error("lifecycle.acceptance_timeout")
		}
		go func() {
			defer close(drained)
			logger.Info("lifecycle.shutdown_requested", "reason", reason)
			// Close waits for admitted calls and workers with SQLite still open.
			// Release registry handles only after those workers return.
			if closeErr := service.Close(); closeErr != nil {
				drainErr = closeErr
				logger.Error("lifecycle.storage_close_failed", "error", closeErr)
			} else {
				logger.Info("lifecycle.storage_closed", "workers_drained", true, "profile_released", true)
			}
			if closeErr := registry.Close(); closeErr != nil {
				drainErr = errors.Join(drainErr, closeErr)
				logger.Error("lifecycle.registry_close_failed", "error", closeErr)
			} else {
				logger.Info("lifecycle.registry_closed")
			}
		}()
		return true
	}
	requestExit := func(reason string) {
		if !beginDrain(reason) {
			if windowCloseAllowed.Load() && !quitAllowed.Load() {
				window.Close()
			}
			return
		}
		go func() {
			<-drained
			select {
			case <-runReturned:
				return
			default:
			}
			// Wails v3.1.0 App.Quit queues window closing while ending the
			// native loop. Let Wails finish the native window close first.
			windowCloseAllowed.Store(true)
			logger.Info("lifecycle.native_close_requested")
			window.Close()
			if !waitForWindowRemoval(desktop, runReturned, func() {
				fatalExit.Store(true)
				logger.Error("lifecycle.native_close_timeout", "close_retry_available", true)
			}) {
				return
			}
			logger.Info("lifecycle.native_window_removed")
			select {
			case <-runReturned:
				return
			default:
			}
			quitAllowed.Store(true)
			desktop.Quit()
		}()
	}
	desktop = application.New(application.Options{
		Name: productName, Description: productDescription,
		Services: services, Assets: application.AssetOptions{Handler: handler}, Windows: windows, Logger: logger,
		ShouldQuit: func() bool {
			if quitAllowed.Load() {
				return true
			}
			requestExit("wails_quit")
			return false
		},
		OnShutdown: func() {
			logger.Info("lifecycle.wails_shutdown")
			beginDrain("wails_shutdown")
		},
		PostShutdown: func() { logger.Info("lifecycle.wails_post_shutdown") },
	})
	eventTarget.Store(desktop)
	if acceptanceService != nil && acceptanceStage != pendingCloseStage {
		acceptanceService.requestExit = requestExit
	}
	url := "/"
	if acceptance {
		url = fmt.Sprintf("/?acceptance=1&stage=%d", acceptanceStage)
		if fixtureURL != "" {
			url += "&fixture=" + queryEscape(fixtureURL)
		}
		if debugPort > 0 {
			url += "&visual=1"
		}
		if gitFixture {
			url += "&gitFixture=1"
		}
		if presetProfileAgent {
			url += "&presetProfileAgent=1"
		}
		if soakAcceptance {
			url += fmt.Sprintf("&seconds=%d", soakSeconds)
		}
	}
	window = desktop.Window.NewWithOptions(application.WebviewWindowOptions{Name: "main", Title: productName, URL: url, Width: 1400, Height: 900, MinWidth: 900, MinHeight: 620, BackgroundColour: application.NewRGB(247, 248, 250), DevToolsEnabled: acceptance, DefaultContextMenuDisabled: true, Permissions: map[application.PermissionType]application.Permission{
		application.PermissionCamera: application.PermissionDeny, application.PermissionMicrophone: application.PermissionDeny, application.PermissionGeolocation: application.PermissionDeny, application.PermissionNotifications: application.PermissionDeny, application.PermissionClipboardRead: application.PermissionDeny,
	}})
	service.SetWorkspacePicker(func(ctx context.Context) (string, error) {
		if ctx.Err() != nil || stopping.Load() {
			return "", context.Canceled
		}
		if acceptance && (acceptanceStage >= 9 && acceptanceStage <= 13 || presetAcceptance || soakAcceptance) {
			// Test fixture lives in the explicitly isolated launcher profile.
			return filepath.Join(dataDir, "mvp-fixture"), nil
		}
		return desktop.Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true).CanCreateDirectories(false).SetTitle(productName + " — Select workspace / 选择工作区").AttachToWindow(window).PromptForSingleSelection()
	})
	window.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
		logger.Info("lifecycle.navigation_completed")
	})
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if windowCloseAllowed.Load() {
			logger.Info("lifecycle.window_closing")
			return
		}
		e.Cancel()
		requestExit("window_close")
	})
	if acceptance {
		limit := 90 * time.Second
		if acceptanceStage == 12 {
			limit = 3 * time.Minute
		}
		if soakAcceptance {
			limit = time.Duration(soakSeconds+90) * time.Second
		}
		watchdog = time.AfterFunc(limit, func() { requestExit("acceptance_timeout") })
	}
	logger.Info("lifecycle.run_started", "bindings", len(services))
	err = desktop.Run()
	close(runReturned)
	beginDrain("run_returned")
	<-drained
	logger.Info("lifecycle.run_returned")
	if err != nil {
		slog.Error("runtime.failed", "error", err)
		return 1
	}
	if drainErr != nil || fatalExit.Load() {
		return 1
	}
	return 0
}

// The window manager removes the window only after its native close finishes.
// If that is delayed, keep accepting close attempts and wait for actual removal.
func waitForWindowRemoval(desktop *application.App, runReturned <-chan struct{}, onDelay func()) bool {
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		if _, exists := desktop.Window.GetByName("main"); !exists {
			return true
		}
		select {
		case <-poll.C:
		case <-deadline.C:
			onDelay()
			poll.Reset(250 * time.Millisecond)
		case <-runReturned:
			return false
		}
	}
}
