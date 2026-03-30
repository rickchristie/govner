// Package logging provides a simple file logger with rotation for Cooper.
// It writes timestamped lines to log files and rotates when files exceed
// the configured maximum size.
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger writes timestamped lines to a log file with automatic rotation.
type Logger struct {
	dir      string
	prefix   string
	maxSize  int64
	maxFiles int

	mu   sync.Mutex
	file *os.File
	size int64
}

// NewLogger creates a new Logger that writes to files named {prefix}.log in
// the given directory. When the file exceeds maxSize bytes, it is rotated.
// Up to maxFiles rotated files are kept (named {prefix}.1.log, {prefix}.2.log, etc.).
func NewLogger(dir, prefix string, maxSize int64, maxFiles int) *Logger {
	return &Logger{
		dir:      dir,
		prefix:   prefix,
		maxSize:  maxSize,
		maxFiles: maxFiles,
	}
}

// Log writes a timestamped entry to the log file. The file is opened lazily
// on first write. If the file exceeds maxSize, it is rotated before writing.
func (l *Logger) Log(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.ensureOpen(); err != nil {
		return
	}

	if l.size >= l.maxSize {
		l.rotate()
		if err := l.ensureOpen(); err != nil {
			return
		}
	}

	line := fmt.Sprintf("%s %s\n", time.Now().UTC().Format(time.RFC3339), entry)
	n, err := l.file.WriteString(line)
	if err == nil {
		l.size += int64(n)
	}
}

// Close closes the underlying file handle.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// ensureOpen opens the log file if it is not already open. Must be called
// with l.mu held.
func (l *Logger) ensureOpen() error {
	if l.file != nil {
		return nil
	}

	if err := os.MkdirAll(l.dir, 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	path := l.currentPath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat log file: %w", err)
	}

	l.file = f
	l.size = info.Size()
	return nil
}

// rotate closes the current file and shifts existing rotated files.
// Must be called with l.mu held.
func (l *Logger) rotate() {
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}

	// Remove oldest file if it would exceed maxFiles.
	oldest := l.rotatedPath(l.maxFiles)
	os.Remove(oldest)

	// Shift existing rotated files: N-1 -> N, N-2 -> N-1, ..., 1 -> 2.
	for i := l.maxFiles - 1; i >= 1; i-- {
		src := l.rotatedPath(i)
		dst := l.rotatedPath(i + 1)
		os.Rename(src, dst)
	}

	// Move current file to .1.log.
	os.Rename(l.currentPath(), l.rotatedPath(1))
	l.size = 0
}

// currentPath returns the path to the active log file.
func (l *Logger) currentPath() string {
	return filepath.Join(l.dir, l.prefix+".log")
}

// rotatedPath returns the path to a rotated log file with the given index.
func (l *Logger) rotatedPath(index int) string {
	return filepath.Join(l.dir, fmt.Sprintf("%s.%d.log", l.prefix, index))
}
