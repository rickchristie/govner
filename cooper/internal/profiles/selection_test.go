package profiles

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

func TestSelectionUsesCompleteCatalogWithPublicTargets(t *testing.T) {
	for _, harness := range []string{"claude", "codex", "copilot", "opencode", "grok", "antigravity"} {
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
			delete(f.service.options.Environment, "TEST_API_KEY")
			f.load(harness, "Work")
			selection, err := f.service.Select(t.Context(), harness, "default")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(selection.Paths.Environment, scope.Environment) {
				t.Fatal("path environment changed")
			}
			if selection.Credentials[0].Value != "profile-secret" {
				t.Fatal("saved credential was lost")
			}
			state := f.state()
			profile := state.byName(harness, "Default")
			count := 0
			for _, mount := range selection.Paths.Mounts {
				if mount.ID == "shared-agents" {
					continue
				}
				host, root := scope.Mounts[count], profile.Roots[count]
				count++
				if mount.ID != host.ID || mount.Target != host.Target || mount.Source != sibling(root, profile.ID) || mount.Ownership != workload.ProfileState || mount.Access != workload.ReadWrite {
					t.Fatalf("invalid selected root: %+v", mount)
				}
				if err := workload.ValidateProfileSource(mount, f.service.options.CooperDir); err != nil {
					t.Fatal(err)
				}
				if mount.Kind == workload.Directory {
					if err := os.WriteFile(filepath.Join(mount.Source, "runtime-change"), []byte("new"), 0600); err != nil {
						t.Fatal(err)
					}
					if _, err := os.Stat(filepath.Join(host.Source, "runtime-change")); !os.IsNotExist(err) {
						t.Fatal("named profile writes leaked to host state")
					}
				}
			}
			if count != len(scope.Mounts) {
				t.Fatal("selection omitted a complete root")
			}
			f.service.options.Environment["TEST_API_KEY"] = "different-host-secret"
			selected, err := f.service.SelectID(t.Context(), harness, selection.ID)
			if err != nil || selected.Credentials[0].Value != "profile-secret" {
				t.Fatalf("host credentials replaced saved credentials: %v", err)
			}
			host, err := f.service.SelectID(t.Context(), harness, "")
			if err != nil || host.ID != "" {
				t.Fatalf("ordinary launch failed: %v", err)
			}
		})
	}
}

func TestSelectionRejectsPendingMissingAndChangedRoots(t *testing.T) {
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
	if err := os.Rename(source, source+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source+"-old", source); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Select(t.Context(), "codex", "Default"); err == nil {
		t.Fatal("root symlink accepted")
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Select(t.Context(), "codex", "Default"); err == nil {
		t.Fatal("replaced root accepted")
	}
}

func TestCredentialAndPathChangesStopHostSwitch(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.service.options.Environment["TEST_API_KEY"] = "personal-secret"
	f.save("codex")
	_, err := f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work", Confirmed: true})
	if err == nil {
		t.Fatal("host credential was carried into a fresh account")
	}
	delete(f.service.options.Environment, "TEST_API_KEY")
	f.service.options.Environment["CODEX_HOME"] = filepath.Join(f.home, ".another-codex")
	_, err = f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work", Confirmed: true})
	if err == nil {
		t.Fatal("path change moved another state directory")
	}
}

func TestCurrentStandaloneFileCanBeAtomicallyReplaced(t *testing.T) {
	f := newFixture(t)
	f.write(".claude/account", "personal")
	f.write(".claude.json", "old")
	f.save("claude")
	f.write(".claude.json.next", "new")
	if err := os.Rename(filepath.Join(f.home, ".claude.json.next"), filepath.Join(f.home, ".claude.json")); err != nil {
		t.Fatal(err)
	}
	id := inode(t, filepath.Join(f.home, ".claude.json"))
	f.load("claude", "Work")
	f.load("claude", "Default")
	if f.read(".claude.json") != "new" || inode(t, filepath.Join(f.home, ".claude.json")) != id {
		t.Fatal("atomic file update was lost")
	}
}
