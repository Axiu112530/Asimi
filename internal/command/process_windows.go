package command

import (
	"bytes"
	"context"
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"
)

type limitedOutput struct {
	mu        sync.Mutex
	data      bytes.Buffer
	limit     int
	truncated bool
}

func fixedDrive(root string) bool {
	p, e := windows.UTF16PtrFromString(root)
	return e == nil && windows.GetDriveType(p) == windows.DRIVE_FIXED
}

func directoryIdentity(dir string) (string, error) {
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil || !strings.EqualFold(canonical, dir) {
		return "", Error("COMMAND_DIRECTORY")
	}
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return "", Error("COMMAND_DIRECTORY")
	}
	h, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return "", Error("COMMAND_DIRECTORY")
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return "", Error("COMMAND_DIRECTORY")
	}
	return identityOf(info), nil
}

func identityOf(info windows.ByHandleFileInformation) string {
	return fmt.Sprintf("%08x:%08x%08x:%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow, info.CreationTime.HighDateTime, info.CreationTime.LowDateTime)
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := b.limit - b.data.Len()
	if len(p) > left {
		p = p[:left]
		b.truncated = true
	}
	b.data.Write(p)
	return n, nil
}
func (b *limitedOutput) text() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	value := b.data.String()
	if b.truncated {
		// A truncated final line can contain an incomplete credential.
		value = value[:strings.LastIndexByte(value, '\n')+1]
	}
	value = redact(strings.ToValidUTF8(value, "\uFFFD"))
	if len(value) <= b.limit {
		return value, b.truncated
	}
	end := b.limit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end], true
}

func runManaged(ctx context.Context, p Proposal, targets []projectTarget) (Result, error) {
	started := time.Now()
	r := Result{ExitCode: -1}
	pins, pinErr := pinCommand(p, targets)
	if pinErr != nil {
		return r, pinErr
	}
	defer func() {
		for _, pin := range pins {
			windows.CloseHandle(pin)
		}
	}()
	if ctx.Err() != nil {
		return interruptedCommand(r, ctx.Err())
	}
	// Never silently inherit an administrator session.
	if windows.GetCurrentProcessToken().IsElevated() {
		return r, Error("COMMAND_ELEVATED_PROCESS")
	}
	job, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		return r, Error("COMMAND_JOB_UNAVAILABLE")
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_JOB_MEMORY
	limits.BasicLimitInformation.ActiveProcessLimit = 32
	limits.JobMemoryLimit = 2 << 30
	if _, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); e != nil {
		return r, Error("COMMAND_JOB_UNAVAILABLE")
	}
	cpu := struct{ Flags, Rate uint32 }{5, 5000}
	if _, e = windows.SetInformationJobObject(job, windows.JobObjectCpuRateControlInformation, uintptr(unsafe.Pointer(&cpu)), uint32(unsafe.Sizeof(cpu))); e != nil {
		return r, Error("COMMAND_JOB_UNAVAILABLE")
	}
	cmd := exec.Command(p.Executable, p.Args...)
	cmd.Dir = p.Directory
	cmd.Env = environment(p)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW}
	cmd.WaitDelay = 500 * time.Millisecond
	output := &limitedOutput{limit: p.MaxOutputBytes}
	stdout := &limitedOutput{limit: p.MaxOutputBytes}
	stderr := &limitedOutput{limit: p.MaxOutputBytes}
	cmd.Stdout = io.MultiWriter(stdout, output)
	cmd.Stderr = io.MultiWriter(stderr, output)
	if e = cmd.Start(); e != nil {
		return r, Error("COMMAND_START_FAILED")
	}
	process, e := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return r, Error("COMMAND_JOB_UNAVAILABLE")
	}
	defer windows.CloseHandle(process)
	if e = windows.AssignProcessToJobObject(job, process); e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return r, Error("COMMAND_JOB_UNAVAILABLE")
	}
	// The process has not executed a single instruction before job assignment.
	if ctx.Err() != nil {
		windows.TerminateJobObject(job, 1)
		cmd.Wait()
		return interruptedCommand(r, ctx.Err())
	}
	if e = resumeProcess(uint32(cmd.Process.Pid)); e != nil {
		windows.TerminateJobObject(job, 1)
		cmd.Wait()
		return r, Error("COMMAND_START_FAILED")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var interruption error
	select {
	case e = <-done:
	case <-ctx.Done():
		windows.TerminateJobObject(job, 1)
		e = <-done
		interruption = ctx.Err()
	}
	// Kill any descendants still alive after the parent exits, including holders
	// of stdout handles; WaitDelay bounds the Go pipe readers.
	windows.TerminateJobObject(job, 1)
	r.DurationMS = time.Since(started).Milliseconds()
	r.Output, r.Truncated = output.text()
	var stdoutTruncated, stderrTruncated bool
	r.Stdout, stdoutTruncated = stdout.text()
	r.Stderr, stderrTruncated = stderr.text()
	r.Truncated = r.Truncated || stdoutTruncated || stderrTruncated
	if cmd.ProcessState != nil {
		r.ExitCode = cmd.ProcessState.ExitCode()
	}
	if interruption != nil {
		return interruptedCommand(r, interruption)
	}
	if e != nil {
		r.Code = "COMMAND_EXIT_FAILED"
		return r, Error("COMMAND_EXIT_FAILED")
	}
	return r, nil
}

// Hold canonical ancestors against rename and executable/script bytes against
// writing from the last hash check through process completion.
func pinCommand(p Proposal, projectTargets []projectTarget) ([]windows.Handle, error) {
	if !localPath(p.Directory) || !localPath(p.Executable) {
		return nil, Error("COMMAND_DIRECTORY")
	}
	var pins []windows.Handle
	fail := func() ([]windows.Handle, error) {
		for _, h := range pins {
			windows.CloseHandle(h)
		}
		return nil, Error("COMMAND_CHANGED")
	}
	paths := []string{p.Directory, filepath.Dir(p.Executable)}
	if p.ScriptHash != "" {
		paths = append(paths, filepath.Dir(p.Args[0]))
	}
	for _, target := range projectTargets {
		dir := filepath.Dir(target.absolute)
		if target.directory {
			dir = target.absolute
		}
		paths = append(paths, dir)
	}
	for _, dir := range paths {
		volume := filepath.VolumeName(dir) + string(os.PathSeparator)
		rel, e := filepath.Rel(volume, dir)
		if e != nil || strings.HasPrefix(rel, "..") {
			return fail()
		}
		cur := volume
		parts := append([]string{""}, strings.Split(rel, string(os.PathSeparator))...)
		for _, part := range parts {
			if part != "" && part != "." {
				cur = filepath.Join(cur, part)
			}
			name, e := windows.UTF16PtrFromString(cur)
			if e != nil {
				return fail()
			}
			h, e := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
			if e != nil {
				return fail()
			}
			pins = append(pins, h)
			var info windows.ByHandleFileInformation
			if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
				return fail()
			}
			if strings.EqualFold(cur, p.Directory) && identityOf(info) != p.DirectoryID {
				fail()
				return nil, Error("STALE_WORKSPACE")
			}
		}
	}
	targets := []string{p.Executable}
	if p.ScriptHash != "" {
		targets = append(targets, p.Args[0])
	}
	for _, target := range projectTargets {
		if !target.directory {
			targets = append(targets, target.absolute)
		}
	}
	for _, path := range targets {
		name, e := windows.UTF16PtrFromString(path)
		if e != nil {
			return fail()
		}
		h, e := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if e != nil {
			return fail()
		}
		pins = append(pins, h)
		var info windows.ByHandleFileInformation
		if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
			return fail()
		}
		for _, target := range projectTargets {
			if target.absolute == path && info.NumberOfLinks != 1 {
				return fail()
			}
		}
	}
	hash, e := digest(p.Executable)
	if e != nil || hash != p.ExecutableHash {
		return fail()
	}
	if p.ScriptHash != "" {
		hash, e = digest(p.Args[0])
		if e != nil || hash != p.ScriptHash {
			return fail()
		}
	}
	return pins, nil
}

func resumeProcess(pid uint32) error {
	snapshot, e := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if e != nil {
		return e
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for e = windows.Thread32First(snapshot, &entry); e == nil; e = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID == pid {
			thread, e := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if e != nil {
				return e
			}
			_, e = windows.ResumeThread(thread)
			windows.CloseHandle(thread)
			return e
		}
	}
	return Error("COMMAND_START_FAILED")
}
