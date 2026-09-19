package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedFirstSaveRejectsWorkspaceInNewRoots(t *testing.T) {
	for _, location := range []string{"root", "worktree", "workspace-link", "state-link", "custom-root", "shared-root"} {
		t.Run(location, func(t *testing.T) {
			f := newFixture(t)
			f.write(".codex/account", "personal")
			if _, err := f.service.Migrate(t.Context(), ""); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(f.home, ".codex")
			switch location {
			case "state-link", "custom-root":
				root = filepath.Join(f.home, "codex-state")
				if err := os.Rename(filepath.Join(f.home, ".codex"), root); err != nil {
					t.Fatal(err)
				}
				if location == "state-link" {
					if err := os.Symlink(root, filepath.Join(f.home, ".codex")); err != nil {
						t.Fatal(err)
					}
				} else {
					f.service.options.Environment["CODEX_HOME"] = root
				}
			case "shared-root":
				root = filepath.Join(f.home, ".agents")
			}
			workspace := filepath.Join(root, "worktrees", "project")
			if location == "root" {
				workspace = root
			}
			if err := os.MkdirAll(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			physicalWorkspace := workspace
			if location == "workspace-link" {
				workspace = filepath.Join(f.home, "project-link")
				if err := os.Symlink(physicalWorkspace, workspace); err != nil {
					t.Fatal(err)
				}
			}
			t.Chdir(workspace)
			f.service.options.Workspace = workspace
			before, err := os.Stat(root)
			if err != nil {
				t.Fatal(err)
			}
			copies := 0
			copyTree := f.service.copy
			f.service.copy = func(ctx context.Context, source, target string) error {
				copies++
				return copyTree(ctx, source, target)
			}
			if _, err := f.service.Save(t.Context(), SaveRequest{Harness: "codex"}); err == nil || !strings.Contains(err.Error(), "working directory") {
				t.Errorf("first save must reject the working directory: %v", err)
			}
			if copies != 0 {
				t.Errorf("refused save copied %d roots", copies)
			}
			after, err := os.Stat(root)
			if err != nil || !os.SameFile(before, after) {
				t.Error("first save moved the working directory", err)
			}
			if err := os.WriteFile("after-save", []byte("relative write"), 0o600); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(physicalWorkspace, "after-save"))
			if err != nil || string(data) != "relative write" {
				t.Error("relative write left the live host root", err)
			}
			if _, err := os.Lstat(filepath.Join(f.service.storePath(), "harnesses")); !errors.Is(err, os.ErrNotExist) {
				t.Error("refused save created profile data", err)
			}
		})
	}
}
