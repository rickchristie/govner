package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const upLockFileName = "up.lock"

type upLock struct {
	file *os.File
	path string
}

type upAlreadyRunningError struct {
	PID      int
	LockPath string
}

func (e *upAlreadyRunningError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("cooper up is already running (pid %d); run 'cooper down' before starting it again", e.PID)
	}
	return "cooper up is already running; run 'cooper down' before starting it again"
}

func upLockPath(cooperDir string) string {
	return filepath.Join(cooperDir, upLockFileName)
}

// acquireUpLock serializes cooper up/down for one Cooper config directory.
// The OS lock, not the file contents, is the source of truth so a SIGKILL or
// host crash cannot leave a stale pid file that blocks future starts.
func acquireUpLock(cooperDir string) (*upLock, error) {
	path := upLockPath(cooperDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create cooper up lock directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open cooper up lock %s: %w", path, err)
	}

	if err := tryLockUpFile(file); err != nil {
		pid := readPIDFromUpLock(file)
		_ = file.Close()
		if isLockBusy(err) {
			return nil, &upAlreadyRunningError{PID: pid, LockPath: path}
		}
		return nil, fmt.Errorf("lock cooper up runtime: %w", err)
	}

	lock := &upLock{file: file, path: path}
	if err := lock.writePID(os.Getpid()); err != nil {
		_ = lock.Release()
		return nil, err
	}
	return lock, nil
}

func (l *upLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}

	var errs []string
	if err := clearUpLockFile(l.file); err != nil {
		errs = append(errs, err.Error())
	}
	if err := unlockUpFile(l.file); err != nil {
		errs = append(errs, fmt.Sprintf("unlock cooper up runtime: %v", err))
	}
	if err := l.file.Close(); err != nil {
		errs = append(errs, fmt.Sprintf("close cooper up lock %s: %v", l.path, err))
	}
	l.file = nil

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (l *upLock) writePID(pid int) error {
	if l == nil || l.file == nil {
		return fmt.Errorf("cooper up lock is not open")
	}
	if err := l.file.Truncate(0); err != nil {
		return fmt.Errorf("truncate cooper up lock %s: %w", l.path, err)
	}
	if _, err := l.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek cooper up lock %s: %w", l.path, err)
	}
	if _, err := fmt.Fprintf(l.file, "pid=%d\n", pid); err != nil {
		return fmt.Errorf("write cooper up lock %s: %w", l.path, err)
	}
	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("sync cooper up lock %s: %w", l.path, err)
	}
	return nil
}

func tryLockUpFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlockUpFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

func isLockBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}

func readPIDFromUpLock(file *os.File) int {
	if file == nil {
		return 0
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0
	}
	data, err := io.ReadAll(io.LimitReader(file, 1024))
	if err != nil {
		return 0
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "pid=")
		pid, err := strconv.Atoi(line)
		if err != nil || pid <= 0 {
			return 0
		}
		return pid
	}
	return 0
}

func clearUpLockFile(file *os.File) error {
	if file == nil {
		return nil
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncate cooper up lock: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek cooper up lock: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync cooper up lock: %w", err)
	}
	return nil
}
