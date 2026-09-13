// Package statelock coordinates profile replacement and workload startup.
// The lock is per host UID, across Cooper directories and runtime namespaces,
// because two configurations can refer to the same host agent roots.
package statelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Lock struct{ file *os.File }

// Acquire takes a shared startup lock or an exclusive mutation lock. Runtime
// startup releases it after the mounts exist; later use checks inspect those
// mounts, including idle containers. Human prompts never hold this lock.
func Acquire(ctx context.Context, exclusive bool) (*Lock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join("/tmp", fmt.Sprintf("cooper-profile-state-%d.lock", os.Getuid()))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open agent state lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		file.Close()
		return nil, errors.New("agent state lock has unsafe ownership or permissions")
	}
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		err = syscall.Flock(int(file.Fd()), mode|syscall.LOCK_NB)
		if err == nil {
			return &Lock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, fmt.Errorf("agent state is busy with another Cooper operation: %w", ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (l *Lock) Close() error { return l.file.Close() }
