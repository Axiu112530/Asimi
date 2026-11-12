// workspaceprobe measures bounded analysis on generated projects.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"deskpilot/internal/developer"
)

type scan struct {
	FixtureFiles        int     `json:"fixtureFiles"`
	AnalysisMS          float64 `json:"analysisMs"`
	FilesVisited        int     `json:"filesVisited"`
	BytesRead           int64   `json:"bytesRead"`
	AnalysisTruncated   bool    `json:"analysisTruncated"`
	ProjectInspectionMS float64 `json:"projectInspectionMs"`
	ProjectTruncated    bool    `json:"projectTruncated"`
}

func fixture(root string, files int) (scan, error) {
	out := scan{FixtureFiles: files}
	path, err := os.MkdirTemp(root, fmt.Sprintf("project-%d-", files))
	if err != nil {
		return out, err
	}
	if err := os.WriteFile(filepath.Join(path, "go.mod"), []byte("module example.com/workspaceprobe\n\ngo 1.26.0\n"), 0600); err != nil {
		return out, err
	}
	for i := 1; i < files; i++ {
		name := fmt.Sprintf("file%05d.go", i)
		if err := os.WriteFile(filepath.Join(path, name), []byte("package workspaceprobe\n"), 0600); err != nil {
			return out, err
		}
	}
	start := time.Now()
	report, err := developer.Analyze(path, "workspaceprobe")
	out.AnalysisMS = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		return out, err
	}
	out.FilesVisited, out.BytesRead, out.AnalysisTruncated = report.FilesVisited, report.BytesRead, report.Truncated
	start = time.Now()
	inspection, err := developer.InspectProject(path, nil)
	out.ProjectInspectionMS = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		return out, err
	}
	out.ProjectTruncated = inspection.Summary.Truncated
	if report.FilesVisited > developer.MaxFiles || (files > developer.MaxFiles && !report.Truncated) {
		return out, fmt.Errorf("bounded scan failed for %d files", files)
	}
	return out, nil
}

func main() {
	root := flag.String("root", "", "dedicated fixture directory")
	flag.Parse()
	if *root == "" {
		fmt.Fprintln(os.Stderr, "fixture directory is required")
		os.Exit(1)
	}
	directory, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	report := struct {
		OS     string `json:"os"`
		Arch   string `json:"arch"`
		Go     string `json:"go"`
		Scans  []scan `json:"scans"`
		Passed bool   `json:"passed"`
	}{OS: runtime.GOOS, Arch: runtime.GOARCH, Go: runtime.Version()}
	for _, size := range []int{250, 2000, 11000} {
		row, err := fixture(directory, size)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		report.Scans = append(report.Scans, row)
	}
	report.Passed = true
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
