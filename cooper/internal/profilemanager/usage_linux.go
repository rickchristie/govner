package profilemanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

func processOwner(info os.FileInfo) int { return int(info.Sys().(*syscall.Stat_t).Uid) }

func processUsage(ctx context.Context, roots []string) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		pid, valid := parsePID(entry.Name())
		if !valid {
			continue
		}
		info, err := entry.Info()
		if err != nil || !ownedProcess(info) {
			continue
		}
		base := filepath.Join("/proc", entry.Name())
		command, err := os.ReadFile(filepath.Join(base, "cmdline"))
		if err != nil {
			continue
		} // The process may have exited.
		harness := harnessName(strings.Split(string(command), "\x00"))
		if harness == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(base, "environ"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return processInUse(pid, harness)
		}
		env := processFields(data)
		home, workspace := env["HOME"], env["PWD"]
		if current, err := os.Readlink(filepath.Join(base, "cwd")); err == nil {
			workspace = current
		}
		if !filepath.IsAbs(home) || !filepath.IsAbs(workspace) {
			return processInUse(pid, harness)
		}
		paths, err := workload.ResolveAgentScope(harness, home, workspace, env)
		if err != nil {
			return processInUse(pid, harness)
		}
		for _, mount := range paths.Mounts {
			path, err := workload.ResolvedPath(mount.Source)
			if err != nil {
				return processInUse(pid, harness)
			}
			if overlapsAny(path, roots) {
				return processInUse(pid, harness)
			}
		}
	}
	return nil
}
