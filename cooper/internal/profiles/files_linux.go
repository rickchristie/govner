package profiles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Do not overwrite a destination created after validation, even if empty.
// Refusing unsupported filesystems keeps a move from becoming a copy.
func renameEntry(parent *os.Root, from, to string) error {
	directory, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return unix.Renameat2(int(directory.Fd()), from, int(directory.Fd()), to, unix.RENAME_NOREPLACE)
}

func checkRootMounts(path string) error {
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
			return fmt.Errorf("profile root contains mount point %s; unmount it before changing profiles", mount)
		}
	}
	return nil
}
