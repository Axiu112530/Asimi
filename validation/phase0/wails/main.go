// Disposable Wails/WebView2/Vue/SQLite integration probe. Not a product shell.
package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/application"
	"io"
	_ "modernc.org/sqlite"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

//go:embed assets
var assets embed.FS

func main() {
	if len(os.Args) != 4 {
		panic("usage: wails-probe stage evidence-dir run-dir")
	}
	stage, e := strconv.Atoi(os.Args[1])
	if e != nil {
		panic(e)
	}
	ev, e := filepath.Abs(os.Args[2])
	if e != nil {
		panic(e)
	}
	local, e := filepath.Abs(os.Args[3])
	if e != nil {
		panic(e)
	}
	if e = os.MkdirAll(local, 0700); e != nil {
		panic(e)
	}
	db, e := sql.Open("sqlite", filepath.Join(local, "probe-settings.sqlite"))
	if e != nil {
		panic(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, e = db.Exec("PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL); PRAGMA user_version=1;"); e != nil {
		panic(e)
	}
	var app *application.App
	var once sync.Once
	failed := true
	// All paths are embedded in the binary for runtime independence from cwd.
	embedded, e := fsSub()
	if e != nil {
		panic(e)
	}
	fsHandler := application.AssetFileServerFS(embedded)
	mux := http.NewServeMux()
	mux.Handle("/", fsHandler)
	mux.HandleFunc("/phase0/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			locale := "en-US"
			e := db.QueryRow("SELECT value FROM settings WHERE key='appLanguage'").Scan(&locale)
			if e != nil && e != sql.ErrNoRows {
				http.Error(w, "storage.read", 500)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"locale": locale, "stage": stage})
			return
		}
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		var v struct {
			Locale string `json:"locale"`
		}
		if e := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&v); e != nil || (v.Locale != "en-US" && v.Locale != "zh-CN") {
			http.Error(w, "config.locale_invalid", 400)
			return
		}
		if _, e := db.Exec("INSERT INTO settings(key,value) VALUES('appLanguage',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", v.Locale); e != nil {
			http.Error(w, "storage.write", 500)
			return
		}
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/phase0/result", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			b, e := io.ReadAll(io.LimitReader(r.Body, 16384))
			if e != nil {
				panic(e)
			}
			var result struct {
				Stage  int
				Checks map[string]bool
				Error  string
			}
			if e = json.Unmarshal(b, &result); e != nil {
				panic(e)
			}
			failed = result.Error != "" || result.Stage != stage || len(result.Checks) != 12
			for _, v := range result.Checks {
				if !v {
					failed = true
				}
			}
			if e = os.WriteFile(filepath.Join(ev, fmt.Sprintf("wails-stage%d.json", stage)), b, 0600); e != nil {
				panic(e)
			}
			w.Write([]byte(`{}`))
			go func() { time.Sleep(150 * time.Millisecond); app.Quit() }()
		})
	})
	app = application.New(application.Options{Name: "Asimi Phase0", Assets: application.AssetOptions{Handler: mux}, Windows: application.WindowsOptions{WebviewUserDataPath: filepath.Join(local, "webview-profile")}})
	app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "Asimi Phase 0", Width: 720, Height: 640})
	go func() { time.Sleep(20 * time.Second); app.Quit() }()
	if e = app.Run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(2)
	}
	if failed {
		os.Exit(3)
	}
}
