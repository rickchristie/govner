package profiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

func fileID(info os.FileInfo) FileID {
	stat := info.Sys().(*syscall.Stat_t)
	return FileID{Device: uint64(stat.Dev), Inode: stat.Ino}
}

func rootParent(root Root) (*os.Root, error) {
	resolved, err := workload.ResolvedPath(filepath.Dir(root.HostPath))
	if err != nil {
		return nil, err
	}
	if resolved != filepath.Dir(root.HostPath) {
		return nil, errors.New("profile parent path changed; state was retained")
	}
	parent, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, err
	}
	info, err := parent.Stat(".")
	if err != nil || fileID(info) != root.Parent {
		parent.Close()
		return nil, errors.New("profile parent identity changed; state was retained")
	}
	return parent, nil
}

func entryID(parent *os.Root, name string) (FileID, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return FileID{}, nil
	}
	if err != nil {
		return FileID{}, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return FileID{}, errors.New("profile root must be a regular file or directory; root links are unsupported")
	}
	return fileID(info), nil
}

func inspectRoot(root Root, path string) (Root, error) {
	parent, err := rootParent(root)
	if err != nil {
		return root, err
	}
	defer parent.Close()
	info, err := parent.Lstat(filepath.Base(path))
	if errors.Is(err, os.ErrNotExist) {
		if root.Kind == workload.Directory && root.Present {
			return root, fmt.Errorf("profile root %s is missing; state was retained", path)
		}
		root.Present, root.Entry = false, FileID{}
		return root, nil
	}
	if err != nil {
		return root, err
	}
	if (root.Kind == workload.Directory && !info.IsDir()) || (root.Kind == workload.File && !info.Mode().IsRegular()) {
		return root, fmt.Errorf("profile root %s changed its file type", path)
	}
	if info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return root, fmt.Errorf("profile root %s has another owner", path)
	}
	if root.Kind == workload.Directory && root.Present && root.Entry != fileID(info) {
		return root, fmt.Errorf("profile directory %s was replaced; state was retained", path)
	}
	root.Present, root.Entry = true, fileID(info)
	return root, nil
}

func inspectProfile(state index, profile Manifest) (Manifest, error) {
	profile.Roots = append([]Root(nil), profile.Roots...)
	for position, root := range profile.Roots {
		updated, err := inspectRoot(root, sourcePath(state, profile, root))
		if err != nil {
			return profile, err
		}
		profile.Roots[position] = updated
	}
	return profile, nil
}

// Recursive removal is confined to an already opened parent and an exact
// registered entry. It never follows child links into another account.
func removeTree(parent *os.Root, name string) error {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return parent.Remove(name)
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return err
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		return errors.New("directory changed before removal")
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := removeTree(root, entry.Name()); err != nil {
			return err
		}
	}
	actual, err = parent.Lstat(name)
	if err != nil || !os.SameFile(info, actual) {
		return errors.New("directory changed during removal")
	}
	return parent.Remove(name)
}
