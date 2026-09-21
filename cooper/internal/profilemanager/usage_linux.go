package profilemanager

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/rickchristie/govner/cooper/internal/profiles"
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
		if !valid || pid == os.Getpid() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !ownedProcess(info) {
			continue
		}
		base := filepath.Join("/proc", entry.Name())
		if err := openStateUse(ctx, base, pid, roots); err != nil {
			return err
		}
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

// Scripts and database clients can write state without a known harness name.
// Existing open files therefore also block switches. Native process discovery
// below still catches a known harness before it opens its first state file.
func openStateUse(ctx context.Context, base string, pid int, roots []string) error {
	if path, err := os.Readlink(filepath.Join(base, "cwd")); err == nil && stateContains(roots, path) {
		return processInUse(pid, "working-directory")
	}
	if err := mappedStateUse(ctx, base, pid, roots); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(base, "fd"))
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if errors.Is(err, os.ErrPermission) {
		// Unrelated non-dumpable services can hide their fd table. The known
		// harness check still refuses unreadable environments below. Open-file
		// discovery is an extra check, not a lock on arbitrary native writers.
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect process %d files: %w", pid, err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := os.Readlink(filepath.Join(base, "fd", entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return processInUse(pid, "unreadable")
		}
		path = strings.TrimSuffix(path, " (deleted)")
		if !filepath.IsAbs(path) {
			continue
		}
		if stateContains(roots, path) {
			return &profiles.Issue{Kind: profiles.StateInUse, Message: fmt.Sprintf("process %d has profile state open; stop it before changing profiles", pid)}
		}
	}
	return nil
}

func stateContains(roots []string, path string) bool {
	path = strings.TrimSuffix(path, " (deleted)")
	if !filepath.IsAbs(path) {
		return false
	}
	for _, root := range roots {
		if contains(root, path) {
			return true
		}
	}
	return false
}

// A database can keep a mapping after it closes the file descriptor. Check
// mappings as well as descriptors, but do not claim to detect idle apps or
// processes hidden by /proc permissions. User confirmation is still required.
func mappedStateUse(ctx context.Context, base string, pid int, roots []string) error {
	file, err := os.Open(filepath.Join(base, "maps"))
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()
		for range 5 {
			end := strings.IndexAny(line, " \t")
			if end < 0 {
				line = ""
				break
			}
			line = strings.TrimLeft(line[end:], " \t")
		}
		if stateContains(roots, line) {
			return processInUse(pid, "memory-mapped")
		}
	}
	return scanner.Err()
}
