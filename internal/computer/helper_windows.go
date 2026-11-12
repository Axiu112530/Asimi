package computer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	hostFile       = "DeskPilotControlHost.exe"
	workerFile     = "DeskPilotUIAWorker.exe"
	helperConfig   = "<configuration/>"
	maxWorkerBytes = 256 << 10
	maxHelperImage = 8 << 20
	workerTimeout  = 5 * time.Second
)

// The Windows build supplies these values. Ordinary development builds have
// no capability to start native executables.
var hostSHA256, workerSHA256 string

type pinnedBinary struct {
	path          string
	image, config *os.File
	directories   []windows.Handle
}

func (b *pinnedBinary) Close() {
	b.image.Close()
	b.config.Close()
	closeDirectories(b.directories)
}

func canonicalPath(handle windows.Handle) (string, error) {
	var path [32768]uint16
	n, err := windows.GetFinalPathNameByHandle(handle, &path[0], uint32(len(path)), 0)
	if err != nil || n == 0 || n >= uint32(len(path)) {
		return "", Error("CONTROL_UNAVAILABLE")
	}
	name := strings.TrimPrefix(windows.UTF16ToString(path[:n]), `\\?\`)
	if len(name) < 3 || name[1:3] != `:\` || !filepath.IsAbs(name) {
		return "", Error("CONTROL_UNAVAILABLE")
	}
	return name, nil
}

func openNative(path string, access, share, flags uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, access, share, nil, windows.OPEN_EXISTING, flags, 0)
}

func canonicalFile(path string) (string, error) {
	h, err := openNative(path, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	return canonicalPath(h)
}

func pinFile(path, wantHash string) (*os.File, error) {
	if len(wantHash) != 64 {
		return nil, Error("CONTROL_UNAVAILABLE")
	}
	h, err := openNative(path, windows.GENERIC_READ, windows.FILE_SHARE_READ, windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if err != nil {
		return nil, Error("CONTROL_UNAVAILABLE")
	}
	f := os.NewFile(uintptr(h), path)
	var info windows.ByHandleFileInformation
	canonical, err := canonicalPath(h)
	if err != nil || !strings.EqualFold(canonical, path) ||
		windows.GetFileInformationByHandle(h, &info) != nil ||
		info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 ||
		info.FileSizeHigh != 0 || info.FileSizeLow == 0 || info.FileSizeLow > maxHelperImage {
		f.Close()
		return nil, Error("CONTROL_UNAVAILABLE")
	}
	digest := sha256.New()
	if n, err := io.Copy(digest, io.LimitReader(f, maxHelperImage+1)); err != nil || n != int64(info.FileSizeLow) ||
		hex.EncodeToString(digest.Sum(nil)) != wantHash {
		f.Close()
		return nil, Error("CONTROL_UNAVAILABLE")
	}
	return f, nil
}

func closeDirectories(handles []windows.Handle) {
	for _, handle := range handles {
		windows.CloseHandle(handle)
	}
}

func pinDirectories(directory string) ([]windows.Handle, error) {
	volume := filepath.VolumeName(directory) + string(os.PathSeparator)
	relative, err := filepath.Rel(volume, directory)
	if err != nil || strings.HasPrefix(relative, "..") {
		return nil, Error("CONTROL_UNAVAILABLE")
	}
	paths := []string{volume}
	for _, component := range strings.Split(relative, string(os.PathSeparator)) {
		if component != "." {
			paths = append(paths, filepath.Join(paths[len(paths)-1], component))
		}
	}
	var handles []windows.Handle
	for _, path := range paths {
		handle, err := openNative(path, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT)
		if err != nil {
			closeDirectories(handles)
			return nil, Error("CONTROL_UNAVAILABLE")
		}
		handles = append(handles, handle)
		canonical, err := canonicalPath(handle)
		var info windows.ByHandleFileInformation
		if err != nil || !strings.EqualFold(canonical, path) || windows.GetFileInformationByHandle(handle, &info) != nil ||
			info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			closeDirectories(handles)
			return nil, Error("CONTROL_UNAVAILABLE")
		}
	}
	return handles, nil
}

func (d *windowsDriver) pinBinary(file, hash string) (*pinnedBinary, error) {
	// Hold every canonical ancestor against rename so CreateProcess cannot
	// resolve a substituted parent directory after the image hash check.
	directories, err := pinDirectories(d.directory)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(d.directory, file)
	image, err := pinFile(path, hash)
	if err != nil {
		closeDirectories(directories)
		return nil, err
	}
	configHash := sha256.Sum256([]byte(helperConfig))
	config, err := pinFile(path+".config", hex.EncodeToString(configHash[:]))
	if err != nil {
		image.Close()
		closeDirectories(directories)
		return nil, err
	}
	return &pinnedBinary{path: path, image: image, config: config, directories: directories}, nil
}

func (d *windowsDriver) configure(cmd *exec.Cmd) {
	cmd.Dir = d.directory
	// Do not inherit CLR profiling hooks, debug overrides, or user search paths.
	cmd.Env = []string{"SystemRoot=" + d.systemRoot, "WINDIR=" + d.systemRoot}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

type workerRequest struct {
	Op      string   `json:"op"`
	Window  Window   `json:"window"`
	Element *Element `json:"element,omitempty"`
	Text    string   `json:"text,omitempty"`
}

type workerReply struct {
	Effect    string    `json:"effect,omitempty"`
	Code      string    `json:"code,omitempty"`
	Elements  []Element `json:"elements,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	Modal     bool      `json:"modal,omitempty"`
}

type boundedOutput struct{ bytes.Buffer }

func (o *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > maxWorkerBytes-o.Len() {
		return 0, Error("CONTROL_UNKNOWN_RESULT")
	}
	return o.Buffer.Write(p)
}

func (d *windowsDriver) callWorker(ctx context.Context, req workerRequest) (workerReply, bool, error) {
	select {
	case d.worker <- struct{}{}:
		defer func() { <-d.worker }()
	default:
		return workerReply{}, false, Error("CONTROL_BUSY")
	}
	ctx, cancel := context.WithTimeout(ctx, workerTimeout)
	defer cancel()
	payload, err := json.Marshal(req)
	if err != nil || len(payload) > maxWorkerBytes {
		return workerReply{}, false, Error("CONTROL_ACTION_DENIED")
	}
	defer clear(payload)
	binary, err := d.pinBinary(workerFile, workerSHA256)
	if err != nil {
		return workerReply{}, false, err
	}
	defer binary.Close()
	cmd := exec.CommandContext(ctx, binary.path)
	d.configure(cmd)
	cmd.SysProcAttr.CreationFlags = windows.CREATE_NO_WINDOW
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Stdin = bytes.NewReader(payload)
	var output boundedOutput
	defer func() { clear(output.Bytes()) }()
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return workerReply{}, false, queryError(ctx, "CONTROL_UNAVAILABLE")
	}
	if err := cmd.Wait(); err != nil {
		return workerReply{}, true, queryError(ctx, "CONTROL_UNKNOWN_RESULT")
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	var reply workerReply
	if err := decoder.Decode(&reply); err != nil || decoder.Decode(new(any)) != io.EOF {
		return workerReply{}, true, Error("CONTROL_UNKNOWN_RESULT")
	}
	return reply, true, nil
}

func queryError(ctx context.Context, fallback Error) Error {
	if ctx.Err() != nil {
		return Error("CONTROL_QUERY_TIMEOUT")
	}
	return fallback
}
