// Package buildlog keeps the complete output of the current and previous build.
package buildlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Log owns one build log and its lock. One build must not replace another
// build's output while that build still runs.
type Log struct {
	Path string
	mu   sync.Mutex
	file *os.File
	lock *os.File
	err  error
}

func Open(cooperDir string) (*Log, error) {
	dir := filepath.Join(cooperDir, "logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create build log directory: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, "build.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open build log lock: %w", err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("another build uses %s: %w", dir, err)
	}
	path := filepath.Join(dir, "build.log")
	if err := os.Rename(path, filepath.Join(dir, "build.previous.log")); err != nil && !os.IsNotExist(err) {
		lock.Close()
		return nil, fmt.Errorf("retain previous build log: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		lock.Close()
		return nil, fmt.Errorf("open build log: %w", err)
	}
	log := &Log{Path: path, file: file, lock: lock}
	log.Line("Build started: " + time.Now().UTC().Format(time.RFC3339))
	return log, nil
}

func (l *Log) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.file.Write(data)
	if err != nil {
		l.err = err
	}
	return n, err
}

func (l *Log) Line(line string) { fmt.Fprintln(l, line) }

// Finish reports file errors as well as build errors. A failed log write must
// not silently produce an incomplete support report.
func (l *Log) Finish(buildErr error) error {
	if buildErr != nil {
		l.Line("Build failed: " + buildErr.Error())
	} else {
		l.Line("Build complete.")
	}
	l.Line("Build finished: " + time.Now().UTC().Format(time.RFC3339))
	err := errors.Join(buildErr, l.err, l.file.Close(), l.lock.Close())
	if err != nil {
		return fmt.Errorf("%w\nBuild log: %s", err, l.Path)
	}
	return nil
}
