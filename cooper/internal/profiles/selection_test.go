package profiles

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

func TestSelectionUsesCompleteCatalogWithSameTargets(t *testing.T) {
	for _, harness := range []string{"claude", "codex", "copilot", "opencode", "grok"} {
		t.Run(harness, func(t *testing.T) {
			f := newFixture(t)
			f.service.options.Reader = ReaderFunc(func(context.Context, string, []workload.MountSpec, map[string]string) (Identity, error) {
				return Identity{Key: "fake-account", Label: "Fixture"}, nil
			})
			scope, roots, err := f.service.hostScope(harness)
			if err != nil {
				t.Fatal(err)
			}
			for _, root := range roots {
				if err := os.MkdirAll(filepath.Dir(root.HostPath), 0700); err != nil {
					t.Fatal(err)
				}
				if root.Kind == workload.Directory {
					if err := os.MkdirAll(root.HostPath, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root.HostPath, "future-state"), []byte(root.ID), 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(root.HostPath, []byte(root.ID), 0600); err != nil {
					t.Fatal(err)
				}
			}
			f.service.options.Environment["TEST_API_KEY"] = "profile-secret"
			f.save(harness)
			selection, err := f.service.Select(t.Context(), harness, "default")
			if err != nil {
				t.Fatal(err)
			}
			if len(selection.Paths.Mounts) != len(scope.Mounts) || !reflect.DeepEqual(selection.Paths.Environment, scope.Environment) {
				t.Fatal("selection differs from shared path catalog")
			}
			for index, mount := range selection.Paths.Mounts {
				host := scope.Mounts[index]
				if mount.ID != host.ID || mount.Target != host.Target || mount.Ownership != workload.ProfileState || mount.Access != workload.ReadWrite || !strings.HasPrefix(mount.Source, f.service.storePath()+"/") {
					t.Fatalf("invalid selected root: %+v", mount)
				}
				if mount.Kind == workload.Directory {
					if err := os.WriteFile(filepath.Join(mount.Source, "runtime-change"), []byte("new"), 0600); err != nil {
						t.Fatal(err)
					}
					if _, err := os.Stat(filepath.Join(host.Source, "runtime-change")); !os.IsNotExist(err) {
						t.Fatal("profile writes leaked to host state")
					}
				}
			}
			f.service.options.Environment["TEST_API_KEY"] = "different-host-secret"
			selected, err := f.service.SelectID(t.Context(), harness, selection.ID)
			if err != nil || selected.Credentials[0].Value != "profile-secret" {
				t.Fatal("host credentials replaced saved credentials")
			}
			hostSelection, err := f.service.Select(t.Context(), harness, "")
			if err != nil || hostSelection.ID != "" || hostSelection.Paths.Mounts[0].Source != scope.Mounts[0].Source {
				t.Fatal("ordinary launch stopped using live host state")
			}
		})
	}
}

func TestSelectionRejectsMissingPendingAndSymlinkedProfiles(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	if _, err := f.service.Select(t.Context(), "codex", "Missing"); err == nil {
		t.Fatal("missing profile fell back to host")
	}
	f.load("codex", "Work")
	if _, err := f.service.Select(t.Context(), "codex", "Work"); err == nil {
		t.Fatal("pending profile launched before login binding")
	}
	source := f.profilePath("Default", "codex-state", "")
	moved := source + "-old"
	if err := os.Rename(source, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, source); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Select(t.Context(), "codex", "Default"); err == nil {
		t.Fatal("root symlink escaped private profile storage")
	}
}
