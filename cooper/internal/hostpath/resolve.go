// Package hostpath resolves host aliases before Cooper creates or replaces
// files. Host launchers and workload state must use the same overlap rules.
package hostpath

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Resolve follows existing symlinks, including links to absent targets.
// Missing children retain their resolved parent path for creation checks.
func Resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	volume := filepath.VolumeName(abs)
	resolved := volume + string(filepath.Separator)
	pending := strings.Split(strings.TrimPrefix(abs, volume), string(filepath.Separator))
	links := 0
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, part)
		info, err := os.Lstat(next)
		if errors.Is(err, os.ErrNotExist) {
			resolved = next
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			if len(pending) > 0 && !info.IsDir() {
				return "", &os.PathError{Op: "resolve", Path: next, Err: syscall.ENOTDIR}
			}
			resolved = next
			continue
		}
		links++
		if links > 255 {
			return "", &os.PathError{Op: "resolve", Path: path, Err: syscall.ELOOP}
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(target) {
			volume = filepath.VolumeName(target)
			resolved = volume + string(filepath.Separator)
			target = strings.TrimPrefix(target, volume)
		}
		pending = append(strings.Split(target, string(filepath.Separator)), pending...)
	}
	return resolved, nil
}
