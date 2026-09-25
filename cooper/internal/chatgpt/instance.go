package chatgpt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// CheckHostInstance refuses a live host app and removes only a proven stale
// lock from this host. Chromium protects locks from other hosts and runtimes.
func CheckHostInstance(directory string) error {
	host, err := os.Hostname()
	if err != nil {
		return err
	}
	return checkHostInstance(directory, host, func(pid int) error { return syscall.Kill(pid, 0) })
}

func checkHostInstance(directory, host string, exists func(int) error) error {
	path := filepath.Join(directory, "SingletonLock")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return errors.New("ChatGPT instance lock is not a symbolic link; inspect the selected app state")
	}
	target, err := os.Readlink(path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(target, host+"-") {
		return nil
	}
	pid, err := strconv.Atoi(strings.TrimPrefix(target, host+"-"))
	if err != nil || pid <= 0 {
		return errors.New("ChatGPT host instance lock has an invalid process ID")
	}
	if !errors.Is(exists(pid), syscall.ESRCH) {
		return fmt.Errorf("close ChatGPT on the host before opening its state in Cooper (process %d)", pid)
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) {
		return errors.New("ChatGPT instance changed during startup; try again")
	}
	return os.Remove(path)
}
