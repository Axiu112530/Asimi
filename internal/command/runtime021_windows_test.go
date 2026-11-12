//go:build windows

package command

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"deskpilot/internal/developer"
)

func TestRuntimeInspectionUsesProjectAndLocalMetadata(t *testing.T) {
	workspace := t.TempDir()
	installation := t.TempDir()
	bin := filepath.Join(installation, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", workspace+";"+bin)
	t.Setenv("GOROOT", "")
	t.Setenv("ProgramFiles", "")
	t.Setenv("ProgramFiles(x86)", "")
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(workspace, "go.exe"), []byte("workspace file must be excluded"))
	// This file has only the minimum PE header needed for metadata inspection.
	// There is no executable program for the diagnostic to start.
	pe := make([]byte, 192)
	copy(pe, "MZ")
	binary.LittleEndian.PutUint32(pe[60:], 64)
	copy(pe[64:], "PE\x00\x00")
	machine := uint16(0x014c)
	if runtime.GOARCH == "arm64" {
		machine = 0xaa64
	}
	binary.LittleEndian.PutUint16(pe[68:], machine)
	binary.LittleEndian.PutUint16(pe[70:], 1)
	binary.LittleEndian.PutUint16(pe[84:], 64)
	binary.LittleEndian.PutUint16(pe[86:], 0x0002)
	binary.LittleEndian.PutUint16(pe[88:], 0x010b)
	binary.LittleEndian.PutUint32(pe[104:], 100)
	binary.LittleEndian.PutUint32(pe[144:], 200)
	write(filepath.Join(bin, "go.exe"), pe)
	write(filepath.Join(installation, "VERSION"), []byte("go1.26.0\n"))
	project := developer.ProjectInspection{
		Summary: developer.ProjectSummary{Types: []string{"Go"}, RuntimeRequirements: map[string]string{"go": "1.25"}},
		Git:     developer.GitContext{Status: "NOT_REPOSITORY"},
	}
	statuses := InspectProjectRuntimes(workspace, project)
	if statuses[0].Status != "AVAILABLE" || statuses[0].Source != "PATH" || statuses[0].Version != "1.26.0" {
		t.Fatalf("Go metadata: %+v", statuses[0])
	}
	for _, diagnostic := range statuses[1:] {
		if diagnostic.Status != "NOT_REQUIRED" || diagnostic.Executable != "" {
			t.Errorf("unrelated runtime: %+v", diagnostic)
		}
	}
	project.Summary.RuntimeRequirements["go"] = "1.27"
	if got := InspectProjectRuntimes(workspace, project)[0]; got.Status != "INCOMPATIBLE" || got.Problem != "MINIMUM_VERSION" {
		t.Fatalf("Go compatibility: %+v", got)
	}
	write(filepath.Join(bin, "go.exe"), []byte("invalid executable"))
	if got := InspectProjectRuntimes(workspace, project)[0]; got.Status != "ERROR" || got.Problem != "METADATA_UNAVAILABLE" {
		t.Fatalf("Go invalid metadata: %+v", got)
	}
	if err := os.Remove(filepath.Join(bin, "go.exe")); err != nil {
		t.Fatal(err)
	}
	if got := InspectProjectRuntimes(workspace, project)[0]; got.Status != "MISSING" || got.DownloadURL == "" {
		t.Fatalf("Go missing: %+v", got)
	}
}
