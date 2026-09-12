// Package vmdev contains the local, testable parts of the VM development gate.
// It has no Docker or QEMU dependency. Cache owners hold one daemon-scoped
// lease through preparation, assertions, and cleanup.
package vmdev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// SourceDigest includes dirty and new source, templates, and embedded payloads.
// Documentation and generated test state do not change the tested program.
func SourceDigest(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative != "." && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "__pycache__") {
				return filepath.SkipDir
			}
			return nil
		}
		if !SourceFile(filepath.ToSlash(relative)) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source must be a regular file: %s", path)
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(relative), info.Mode().Perm())
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(hash, file)
		file.Close()
		if err != nil {
			return err
		}
		hash.Write([]byte{0})
		return nil
	})
	return hex.EncodeToString(hash.Sum(nil)), err
}

func SourceFile(relative string) bool {
	if strings.HasSuffix(relative, ".md") || strings.HasSuffix(relative, ".pyc") {
		return false
	}
	if strings.HasPrefix(relative, "internal/") || strings.HasPrefix(relative, "cmd/") || strings.HasPrefix(relative, "meta/") || strings.HasPrefix(relative, "dev/") {
		return true
	}
	return strings.HasSuffix(relative, ".go") || relative == "go.mod" || relative == "go.sum" || relative == "test-vm-dev.sh" || relative == "test-vm.sh"
}

func FileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// CacheKey separates accounts and Docker daemons that share a workspace.
func CacheKey(daemonID string, uid int) string {
	return fmt.Sprintf("%d-%x", uid, sha256.Sum256([]byte(daemonID)))[:20]
}

// Lease never unlinks its lock file. Removing a locked inode would let another
// process take a different lock for the same cache.
type Lease struct{ file *os.File }

func Acquire(ctx context.Context, path string) (*Lease, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &Lease{file}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, fmt.Errorf("VM development cache is in use: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func (l *Lease) Close() error { return l.file.Close() }

// WriteJSON publishes complete records only. Failed preparation leaves no new
// manifest, and a source change during preparation must prevent publication.
func WriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".record-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// Stamp allows cheap warm validation after preparation has checked full data.
// It is a test cache freshness record, not an integrity boundary against its owner.
type Stamp struct {
	Size       int64  `json:"size"`
	ModifiedNS int64  `json:"modified_ns"`
	Mode       uint32 `json:"mode"`
}

func FileStamp(path string) (Stamp, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Stamp{}, err
	}
	if !info.Mode().IsRegular() {
		return Stamp{}, fmt.Errorf("not a regular file: %s", path)
	}
	return Stamp{info.Size(), info.ModTime().UnixNano(), uint32(info.Mode().Perm())}, nil
}

// Stage first tries a hard link. Copy only on a filesystem boundary or a host
// that forbids links. Never chmod or chown the result: a link shares its inode.
func Stage(source, target string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		return "", fmt.Errorf("stage destination already exists: %s", target)
	}
	if err := os.Link(source, target); err == nil {
		return "hardlink", nil
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		os.Remove(target)
		return "", copyErr
	}
	if closeErr != nil {
		os.Remove(target)
		return "", closeErr
	}
	return "copy", nil
}
