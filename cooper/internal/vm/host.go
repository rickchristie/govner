package vm

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// HostRequirements verifies the minimum physical or nested Linux host.
func HostRequirements(requireNested bool) error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("cooper VM requires Linux x86-64")
	}
	if err := validateHostIDs(os.Geteuid(), os.Getegid()); err != nil {
		return err
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("cooper VM requires the Docker command")
	}
	file, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("cooper VM needs read-write access to /dev/kvm: %w", err)
	}
	file.Close()
	if !requireNested {
		return nil
	}
	path := "/sys/module/kvm_amd/parameters/nested"
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		path = "/sys/module/kvm_intel/parameters/nested"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read nested KVM setting: %w", err)
	}
	value := strings.ToLower(strings.TrimSpace(string(data)))
	if value != "1" && value != "y" {
		return fmt.Errorf("nested KVM is disabled in %s", path)
	}
	return nil
}

func validateHostIDs(uid, gid int) error {
	if uid == 0 || gid == 0 {
		return errors.New("cooper VM must run as an unprivileged user outside the root user and group")
	}
	return nil
}

func currentIDs() (uid, gid, kvmGID int, err error) {
	uid = os.Getuid()
	gid = os.Getgid()
	info, err := os.Stat("/dev/kvm")
	if err != nil {
		return 0, 0, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0, errors.New("cannot read /dev/kvm group")
	}
	if stat.Gid == 0 {
		return 0, 0, 0, errors.New("cooper VM refuses /dev/kvm with the root group; configure a dedicated kvm group")
	}
	return uid, gid, int(stat.Gid), nil
}

// AvailableMemoryMiB returns MemAvailable from procfs.
func AvailableMemoryMiB() (int, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kib, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0, err
			}
			return kib / 1024, nil
		}
	}
	return 0, errors.New("MemAvailable is absent from /proc/meminfo")
}
