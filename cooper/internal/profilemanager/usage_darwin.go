package profilemanager

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func processOwner(info os.FileInfo) int { return int(info.Sys().(*syscall.Stat_t).Uid) }

func processUsage(ctx context.Context, roots []string) error {
	data, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,uid=,comm=").Output()
	if err != nil {
		return errors.New("cannot check host harness processes")
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		pid, valid := parsePID(parts[0])
		uid, err := strconv.Atoi(parts[1])
		if !valid || err != nil || uid != os.Getuid() {
			continue
		}
		if name := harnessName(parts[2:]); name != "" {
			return processInUse(pid, name)
		}
	}
	return nil
}
