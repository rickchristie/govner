package profiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// treeDigest records content, names, kinds, and permission bits. It excludes
// access times, inode numbers, and process transport. An atomic replacement
// with identical bytes and permissions does not create a state conflict.
func treeDigest(ctx context.Context, path string) (string, error) {
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	defer parent.Close()
	return treeDigestAt(ctx, parent, filepath.Base(path))
}

func treeDigestAt(ctx context.Context, parent *os.Root, name string) (string, error) {
	hash := sha256.New()
	err := visitTree(ctx, parent, name, ".", func(root *os.Root, name, relative string, info fs.FileInfo) error {
		fmt.Fprintf(hash, "%s\x00%d\x00", relative, info.Mode())
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := root.Readlink(name)
			if err != nil {
				return err
			}
			fmt.Fprint(hash, target)
		} else if info.Mode().IsRegular() {
			file, err := openRegular(root, name, info)
			if err != nil {
				return err
			}
			// Use a fixed-size content hash. Raw binary file bytes can contain
			// tree record separators and must not disguise added or lost files.
			content := sha256.New()
			_, readErr := io.Copy(content, &contextReader{ctx: ctx, reader: file})
			if err := errors.Join(readErr, file.Close()); err != nil {
				return err
			}
			hash.Write(content.Sum(nil))
		}
		hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// copyTree uses anchored roots and never follows a child symlink. No mutable
// profile file shares an inode with the host or a different profile.
func copyTree(ctx context.Context, source, target string) error {
	input, err := os.OpenRoot(filepath.Dir(source))
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenRoot(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer output.Close()
	base := filepath.Base(target)
	type directory struct {
		name string
		mode fs.FileMode
	}
	var directories []directory
	err = visitTree(ctx, input, filepath.Base(source), ".", func(root *os.Root, name, relative string, info fs.FileInfo) error {
		destination := filepath.Join(base, relative)
		switch {
		case info.IsDir():
			if err := output.Mkdir(destination, 0o700); err != nil {
				return err
			}
			directories = append(directories, directory{destination, permissionMode(info.Mode())})
			return nil
		case info.Mode()&os.ModeSymlink != 0:
			link, err := root.Readlink(name)
			if err != nil {
				return err
			}
			return output.Symlink(link, destination)
		default:
			return copyRegular(ctx, root, name, info, output, destination)
		}
	})
	if err != nil {
		return err
	}
	// Delay directory modes until children exist. Read-only source directories
	// are valid state and must not make a complete copy fail halfway through.
	for index := len(directories) - 1; index >= 0; index-- {
		dir := directories[index]
		if err := output.Chmod(dir.name, dir.mode); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Join(output.Name(), dir.name)); err != nil {
			return err
		}
	}
	return syncDirectory(filepath.Dir(target))
}

func copyRegular(ctx context.Context, input *os.Root, name string, info fs.FileInfo, output *os.Root, destination string) error {
	source, err := openRegular(input, name, info)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := output.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, &contextReader{ctx: ctx, reader: source})
	modeErr := target.Chmod(permissionMode(info.Mode()))
	syncErr := target.Sync()
	return errors.Join(copyErr, modeErr, syncErr, target.Close())
}

type treeVisitor func(*os.Root, string, string, fs.FileInfo) error

func permissionMode(mode fs.FileMode) fs.FileMode {
	return mode & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
}

func visitTree(ctx context.Context, root *os.Root, name, relative string, visit treeVisitor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if info.Mode()&(os.ModeSocket|os.ModeNamedPipe) != 0 {
		return nil // Process transport cannot be restored as durable state.
	}
	if !info.IsDir() && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("state contains unsupported special file %s", relative)
	}
	if err := visit(root, name, relative, info); err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	directory, err := root.OpenRoot(name)
	if err != nil {
		return err
	}
	defer directory.Close()
	actual, err := directory.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		return fmt.Errorf("state directory changed while opening %s", relative)
	}
	entries, err := fs.ReadDir(directory.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := visitTree(ctx, directory, entry.Name(), filepath.Join(relative, entry.Name()), visit); err != nil {
			return err
		}
	}
	return nil
}

func openRegular(root *os.Root, name string, expected fs.FileInfo) (*os.File, error) {
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(expected, actual) {
		file.Close()
		return nil, fmt.Errorf("state file changed while opening %s", name)
	}
	return file, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
