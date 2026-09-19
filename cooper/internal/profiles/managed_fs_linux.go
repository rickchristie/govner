package profiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Initial conversion refuses layouts whose ownership or extended attributes
// the existing copy format cannot preserve. No host entry has moved yet.
func checkManagedAttributes(path string, info fs.FileInfo) error {
	stat := info.Sys().(*syscall.Stat_t)
	if stat.Uid != uint32(os.Getuid()) || stat.Gid != uint32(os.Getgid()) {
		return errors.New("state has another owner or group; correct ownership before conversion")
	}
	size, err := unix.Llistxattr(path, nil)
	if errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	if err != nil {
		return err
	}
	if size > 0 {
		return errors.New("state has ACLs or extended attributes; managed conversion does not support this layout")
	}
	return nil
}

func checkManagedMounts(path string) error {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mount := filepath.Clean(unescape.Replace(fields[4]))
		if containsPath(path, mount) {
			return fmt.Errorf("state root contains mount point %s; unmount it before conversion", mount)
		}
	}
	return nil
}

func checkManagedSpace(path string, required int64) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return err
	}
	available := uint64(stat.Bavail) * uint64(stat.Bsize)
	if uint64(required) > available {
		return fmt.Errorf("profile conversion needs at least %d bytes; only %d are free", required, available)
	}
	return nil
}

// A different process can create the destination while the backup copies.
// Publish without replacing even an empty directory created in that interval.
func publishBackup(parent *os.Root, from, to string) error {
	directory, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return unix.Renameat2(int(directory.Fd()), from, int(directory.Fd()), to, unix.RENAME_NOREPLACE)
}
