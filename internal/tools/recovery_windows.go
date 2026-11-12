//go:build windows

package tools

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"unsafe"

	"asimi/internal/domain"
	"golang.org/x/sys/windows"
)

// ExecutionHooks records the flushed file identity durably before rename. Both
// hooks run with the registry locked. They must not acquire the service mutex or
// call the registry. Committed is only for isolated process fault injection.
type ExecutionHooks struct {
	Record    func(domain.FileWitness) error
	Committed func()
}

// Inspect reconciles an interrupted operation against its recorded native file
// identity. It never resumes an operation or changes any file. Expiry affects
// execution authority only, so it does not prevent inspection after restart.
func (r *Registry) Inspect(op domain.Operation, witness domain.FileWitness) (v domain.Verification, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v = expectedVerification(op)
	defer func() { setVerificationCode(&v, err) }()
	if r.closed {
		return v, toolError("REGISTRY_CLOSED")
	}
	if _, err = r.validateCanonicalOperation(op); err != nil {
		return v, err
	}
	if !validWitness(witness) {
		return v, toolError("WITNESS_MISMATCH")
	}
	return r.verify(context.Background(), op, witness, false)
}

func expectedVerification(op domain.Operation) domain.Verification {
	return domain.Verification{Path: op.NormalizedPath, ExpectedBytes: op.Bytes, ExpectedHash: op.ContentHash}
}

func setVerificationCode(v *domain.Verification, err error) {
	var e toolError
	if errors.As(err, &e) {
		v.Code = e.Code()
	}
}

func validWitness(witness domain.FileWitness) bool {
	name, prefix := strings.CutPrefix(witness.TempName, ".asimi-")
	if !prefix {
		name, prefix = strings.CutPrefix(witness.TempName, ".deskpilot-")
	}
	name, suffix := strings.CutSuffix(name, ".tmp")
	if !prefix || !suffix || len(name) != 32 || name != strings.ToLower(name) {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil && witness.VolumeSerial != 0 && witness.FileID != ([16]byte{}) && witness.CreationTime != 0
}

// FILE_ID_INFO contains a 64-bit volume serial and the filesystem's full
// 128-bit file ID. Creation time distinguishes a file record reused later.
type fileIDInfo struct {
	VolumeSerial uint64
	FileID       [16]byte
}

func identifyFile(h windows.Handle, tempName string) (windows.ByHandleFileInformation, domain.FileWitness, error) {
	var file windows.ByHandleFileInformation
	var id fileIDInfo
	if windows.GetFileInformationByHandle(h, &file) != nil {
		return file, domain.FileWitness{}, toolError("WITNESS_UNAVAILABLE")
	}
	if file.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || file.NumberOfLinks != 1 {
		return file, domain.FileWitness{}, toolError("WITNESS_MISMATCH")
	}
	if windows.GetFileInformationByHandleEx(h, windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id))) != nil {
		return file, domain.FileWitness{}, toolError("WITNESS_UNAVAILABLE")
	}
	witness := domain.FileWitness{
		VolumeSerial: id.VolumeSerial,
		FileID:       id.FileID,
		CreationTime: uint64(file.CreationTime.HighDateTime)<<32 | uint64(file.CreationTime.LowDateTime),
		TempName:     tempName,
	}
	if !validWitness(witness) {
		return file, domain.FileWitness{}, toolError("WITNESS_UNAVAILABLE")
	}
	return file, witness, nil
}

func callbackError(err error, fallback toolError) toolError {
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		code := coded.Code()
		if suffix, ok := strings.CutPrefix(code, "STORAGE_"); ok && len(suffix) > 0 && len(code) <= 48 &&
			strings.IndexFunc(suffix, func(c rune) bool { return c != '_' && (c < 'A' || c > 'Z') }) == -1 {
			return toolError(code)
		}
	}
	return fallback
}
