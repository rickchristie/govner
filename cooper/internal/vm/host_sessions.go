package vm

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/chatgpt"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// CheckHostSessions runs before launch preparation and again before VM start.
// The desktop instance lock alone cannot detect a separate host Codex CLI.
func CheckHostSessions(ctx context.Context, tool string) error {
	if !strings.EqualFold(strings.TrimSpace(tool), "chatgpt") {
		return nil
	}
	return chatgpt.CheckHostProcesses(ctx)
}

// A test can register only an empty private directory. Keep its identity in
// this process, not in a file or an environment variable that a normal Cooper
// launch could inherit. Go sets testing.Testing only in test executables.
var isolatedTestHomes sync.Map

// RegisterIsolatedTestHome permits VM tests to use new state while the host
// app stays open. The caller must release the registration before it removes
// the home. This does not change CheckHostSessions or the CLI launch guard.
func RegisterIsolatedTestHome(home string) (func(), error) {
	if !testing.Testing() {
		return nil, fmt.Errorf("isolated VM state is available only in Go tests")
	}
	info, err := privateTestHome(home)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil, err
	}
	if len(entries) != 0 {
		return nil, fmt.Errorf("isolated VM test home must be empty: %s", home)
	}
	if _, loaded := isolatedTestHomes.LoadOrStore(home, info); loaded {
		return nil, fmt.Errorf("isolated VM test home is already registered: %s", home)
	}
	return func() { isolatedTestHomes.CompareAndDelete(home, info) }, nil
}

func privateTestHome(home string) (os.FileInfo, error) {
	if !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return nil, fmt.Errorf("isolated VM test home must be a clean absolute path")
	}
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		return nil, err
	}
	if resolved != home {
		return nil, fmt.Errorf("isolated VM test home cannot use symlinks: %s", home)
	}
	info, err := os.Lstat(home)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || int(info.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
		return nil, fmt.Errorf("isolated VM test home must be a private owned directory: %s", home)
	}
	return info, nil
}

func (m Manager) isolatedTestHome() (os.FileInfo, bool) {
	if !testing.Testing() {
		return nil, false
	}
	info, ok := isolatedTestHomes.Load(m.HomeDir)
	if !ok {
		return nil, false
	}
	return info.(os.FileInfo), true
}

func (m Manager) checkHostSessions(ctx context.Context, request StartRequest) error {
	if !strings.EqualFold(strings.TrimSpace(request.ToolName), "chatgpt") {
		return nil
	}
	registered, ok := m.isolatedTestHome()
	if !ok {
		return CheckHostSessions(ctx, request.ToolName)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := privateTestHome(m.HomeDir)
	if err != nil {
		return err
	}
	if !os.SameFile(registered, current) {
		return fmt.Errorf("isolated VM test home was replaced: %s", m.HomeDir)
	}
	if request.ProfileID != "" {
		return fmt.Errorf("isolated VM tests cannot use named account profiles")
	}
	if err := m.checkTestPath(request.WorkspaceDir); err != nil {
		return err
	}
	paths, err := workload.ResolveAgentScope("chatgpt", m.HomeDir, request.WorkspaceDir, workload.HostPathEnvironment())
	if err != nil {
		return err
	}
	if err := m.checkTestStateMounts(request.ToolName, paths.Mounts); err != nil {
		return err
	}
	// Test state is small and starts empty. Check its children as well as root
	// paths so a later symlink or hard link cannot expose a live host database.
	return filepath.WalkDir(m.HomeDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("isolated VM test state cannot contain symlinks: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && info.Sys().(*syscall.Stat_t).Nlink != 1 {
			return fmt.Errorf("isolated VM test state cannot contain hard links: %s", path)
		}
		return nil
	})
}

func (m Manager) checkTestPath(path string) error {
	relative, err := filepath.Rel(m.HomeDir, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("isolated VM test path must stay below its private home: %s", path)
	}
	return nil
}

func (m Manager) checkTestStateMounts(tool string, mounts []workload.MountSpec) error {
	if !strings.EqualFold(strings.TrimSpace(tool), "chatgpt") {
		return nil
	}
	if _, ok := m.isolatedTestHome(); !ok {
		return nil
	}
	for _, mount := range mounts {
		if err := m.checkTestPath(mount.Source); err != nil {
			return err
		}
		if err := m.checkTestPath(mount.Target); err != nil {
			return err
		}
	}
	return nil
}
