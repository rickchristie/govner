package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type runtimeLock struct {
	file *os.File
}

func acquireRuntimeLock(cooperDir, runtimeID string) (*runtimeLock, error) {
	if cleanNamePart(runtimeID) != runtimeID || runtimeID == "" {
		return nil, fmt.Errorf("invalid Cooper VM runtime ID %q", runtimeID)
	}
	dir := filepath.Join(cooperDir, "vm", "locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create VM lock directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, runtimeID+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open VM runtime lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock VM runtime: %w", err)
	}
	return &runtimeLock{file: file}, nil
}

func (l *runtimeLock) release() {
	if l == nil || l.file == nil {
		return
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
}
