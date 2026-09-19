//go:build linux

package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func (f fixture) migrate() {
	f.t.Helper()
	preview, err := f.service.PreviewMigration(context.Background(), "")
	if err != nil {
		f.t.Fatal(err)
	}
	if len(preview.Roots) == 0 {
		f.t.Fatal("migration has no roots")
	}
	if _, err := f.service.Migrate(context.Background(), ""); err != nil {
		f.t.Fatal(err)
	}
}

func TestManagedLiveSwitchDoesNotCopyHistory(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.write(".codex/sessions/first", "before migration")
	f.save("codex")
	f.migrate()
	host := filepath.Join(f.home, ".codex")
	if info, err := os.Lstat(host); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("host alias: %v %v", info, err)
	}
	f.write(".codex/sessions/live", "written through alias")
	if data, err := os.ReadFile(f.profilePath("Default", "codex-state", "sessions/live")); err != nil || string(data) != "written through alias" {
		t.Fatalf("live profile: %q %v", data, err)
	}
	f.load("codex", "Work")
	f.write(".codex/account", "work")
	f.save("codex")
	// A normal switch must not invoke any tree copy, even with a large session.
	sparse, err := os.Create(filepath.Join(host, "large-session"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sparse.Truncate(2 << 30); err != nil {
		t.Fatal(err)
	}
	sparse.Close()
	f.service.copy = func(context.Context, string, string) error { return errors.New("unexpected tree copy") }
	for _, name := range []string{"Default", "Work", "Default"} {
		f.load("codex", name)
	}
	if got := f.read(".codex/account"); got != "personal" {
		t.Fatal(got)
	}
	named, err := f.service.Select(context.Background(), "codex", "Default")
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := f.service.Select(context.Background(), "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if ordinary.ID != "" {
		t.Fatal("ordinary launch captured credentials")
	}
	if len(named.Paths.Mounts) != len(ordinary.Paths.Mounts) {
		t.Fatalf("mount count: %+v %+v", named, ordinary)
	}
	for i, mount := range named.Paths.Mounts {
		actual := ordinary.Paths.Mounts[i]
		if mount.Source != actual.Source || mount.Target != actual.Target || actual.Ownership != workload.ProfileState {
			t.Fatalf("mount mismatch: %+v %+v", mount, actual)
		}
		if strings.Contains(mount.Source, "/current/") {
			t.Fatal("runtime source follows selector")
		}
	}
	f.write(".codex/account", "unexpected")
	_, err = f.service.Save(context.Background(), SaveRequest{Harness: "codex"})
	requireIssue(t, err, AccountConflict)
	_, err = f.service.Select(context.Background(), "codex", "Default")
	requireIssue(t, err, AccountConflict)
}

func TestManagedStandaloneFileSurvivesReplacement(t *testing.T) {
	f := newFixture(t)
	f.write(".claude/account", "personal")
	f.write(".claude.json", "first")
	f.save("claude")
	f.migrate()
	f.write("new-settings", "replace by rename")
	if err := os.Rename(filepath.Join(f.home, "new-settings"), filepath.Join(f.home, ".claude.json")); err != nil {
		t.Fatal(err)
	}
	f.load("claude", "Work")
	if _, err := os.Stat(filepath.Join(f.home, ".claude.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fresh file remained", err)
	}
	f.write(".claude/account", "work")
	f.write(".claude.json", "work settings")
	f.save("claude")
	f.load("claude", "Default")
	if got := f.read(".claude.json"); got != "replace by rename" {
		t.Fatal(got)
	}
	f.load("claude", "Work")
	if got := f.read(".claude.json"); got != "work settings" {
		t.Fatal(got)
	}
}

func TestManagedMigrationRecovery(t *testing.T) {
	for _, stop := range []string{"journal", "root-0", "selector", "selector-sync", "finish"} {
		t.Run(stop, func(t *testing.T) {
			f := newFixture(t)
			f.write(".codex/account", "personal")
			f.save("codex")
			f.service.managedCheckpoint = func(step string) error {
				if step == stop {
					return errors.New("power cut")
				}
				return nil
			}
			if _, err := f.service.Migrate(context.Background(), ""); err == nil {
				t.Fatal("fault was not reached")
			}
			if err := CheckReady(f.service.options.CooperDir); err == nil {
				t.Fatal("unfinished state accepted")
			}
			f.service.managedCheckpoint = func(string) error { return nil }
			if err := f.service.Recover(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := f.service.Recover(context.Background()); err != nil {
				t.Fatal("recovery not repeatable", err)
			}
			if err := CheckReady(f.service.options.CooperDir); err != nil {
				t.Fatal(err)
			}
			if got := f.read(".codex/account"); got != "personal" {
				t.Fatal(got)
			}
			managed, err := f.service.usesManaged()
			if err != nil {
				t.Fatal(err)
			}
			if managed != (stop == "selector" || stop == "selector-sync" || stop == "finish") {
				t.Fatalf("wrong commit result: %s %v", stop, managed)
			}
		})
	}
}

func TestManagedAliasDriftStopsLaunch(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.migrate()
	if err := os.Remove(filepath.Join(f.home, ".codex")); err != nil {
		t.Fatal(err)
	}
	f.write(".codex/account", "replacement")
	if err := CheckReady(f.service.options.CooperDir); err == nil {
		t.Fatal("replaced alias accepted")
	}
	if _, err := f.service.Select(context.Background(), "codex", ""); err == nil {
		t.Fatal("detached state mounted")
	}
	if data, err := os.ReadFile(f.profilePath("Default", "codex-state", "account")); err != nil || string(data) != "personal" {
		t.Fatal("original lost", err)
	}
}

func TestManagedSharedRootsHaveOneHostOwner(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "codex")
	f.write(".grok/account", "grok")
	f.write(".agents/skills/test", "common")
	f.save("codex")
	f.save("grok")
	f.migrate()
	f.load("codex", "Default")
	list, err := f.service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.Harness == "grok" && !item.Mixed {
			t.Fatal("shared root ownership hidden", item)
		}
	}
	_, err = f.service.Save(context.Background(), SaveRequest{Harness: "grok"})
	requireIssue(t, err, MixedState)
	f.load("grok", "Default")
	if got := f.read(".agents/skills/test"); got != "common" {
		t.Fatal(got)
	}
	store, err := f.service.open(false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	view, err := profilelink.Read(store)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, binding := range view.Bindings {
		if binding.Path == filepath.Join(f.home, ".agents") {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate shared bindings", count)
	}
}

func TestManagedBackupAndDetachPreserveLatestWrites(t *testing.T) {
	f := newFixture(t)
	f.write(".claude/account", "personal")
	f.write(".claude.json", "initial")
	f.save("claude")
	f.migrate()
	f.write(".claude/sessions/new", "latest")
	f.write(".claude.json", "latest file")
	backup := filepath.Join(t.TempDir(), "backup")
	if err := f.service.Backup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	source, err := os.OpenRoot(backup)
	if err != nil {
		t.Fatal(err)
	}
	state, err := readIndex(source)
	source.Close()
	if err != nil {
		t.Fatal(err)
	}
	profile := state.byName("claude", "Default")
	data, err := os.ReadFile(filepath.Join(backup, dataPath(*profile), "roots", "claude-state", "sessions/new"))
	if err != nil || string(data) != "latest" {
		t.Fatal("backup", err)
	}
	if _, err := f.service.Detach(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(f.home, ".claude"))
	if err != nil || !info.IsDir() {
		t.Fatal("host not detached", err)
	}
	if got := f.read(".claude/sessions/new"); got != "latest" {
		t.Fatal(got)
	}
	if got := f.read(".claude.json"); got != "latest file" {
		t.Fatal(got)
	}
	managed, err := f.service.usesManaged()
	if err != nil || managed {
		t.Fatal("still managed", err)
	}
	f.load("claude", "Work")
	f.write(".claude/account", "work")
	f.save("claude")
	f.load("claude", "Default")
	if got := f.read(".claude/sessions/new"); got != "latest" {
		t.Fatal("copy-mode restore", got)
	}
}

func TestManagedDetachRecovery(t *testing.T) {
	for _, stop := range []string{"journal", "root-0", "selector", "selector-sync", "finish"} {
		t.Run(stop, func(t *testing.T) {
			f := newFixture(t)
			f.write(".codex/account", "personal")
			f.save("codex")
			f.migrate()
			f.write(".codex/new", "after migration")
			f.service.managedCheckpoint = func(step string) error {
				if step == stop {
					return errors.New("power cut")
				}
				return nil
			}
			if _, err := f.service.Detach(context.Background()); err == nil {
				t.Fatal("fault not reached")
			}
			f.service.managedCheckpoint = func(string) error { return nil }
			if err := f.service.Recover(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := f.service.Recover(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := f.read(".codex/new"); got != "after migration" {
				t.Fatal(got)
			}
			managed, err := f.service.usesManaged()
			if err != nil {
				t.Fatal(err)
			}
			if managed != (stop == "journal" || stop == "root-0") {
				t.Fatalf("wrong detach commit result %v", managed)
			}
		})
	}
}

func TestManagedPruneAndDelete(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.migrate()
	f.load("codex", "Work")
	f.write(".codex/account", "work")
	f.save("codex")
	if err := f.service.Delete(t.Context(), "codex", "Default"); err == nil {
		t.Fatal("deleted required recovery")
	}
	original := f.profilePath("Default", "codex-state", "")
	if err := f.service.PruneRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Delete(t.Context(), "codex", "Default"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(original); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted data remained", err)
	}
	if got := f.read(".codex/account"); got != "work" {
		t.Fatal("active state changed", got)
	}
	if _, err := f.service.Select(t.Context(), "codex", "Default"); err == nil {
		t.Fatal("deleted profile selected")
	}
	if err := f.service.PruneRecovery(t.Context()); err != nil {
		t.Fatal("repeat prune", err)
	}
}

func TestManagedEveryCatalogPreservesTargets(t *testing.T) {
	for _, harness := range []string{"claude", "codex", "copilot", "opencode", "grok", "antigravity"} {
		t.Run(harness, func(t *testing.T) {
			f := newFixture(t)
			f.service.options.Reader = ReaderFunc(func(context.Context, string, []workload.MountSpec, map[string]string) (Identity, error) {
				return Identity{Key: "fixture", Label: "Fixture"}, nil
			})
			scope, roots, err := f.service.hostScope(harness)
			if err != nil {
				t.Fatal(err)
			}
			for _, root := range roots {
				if root.Kind == workload.Directory {
					if err := os.MkdirAll(root.HostPath, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root.HostPath, "future"), []byte(root.ID), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			f.save(harness)
			f.migrate()
			for _, name := range []string{"", "Default"} {
				selection, err := f.service.Select(t.Context(), harness, name)
				if err != nil {
					t.Fatal(err)
				}
				for _, mount := range selection.Paths.Mounts {
					found := false
					for _, expected := range scope.Mounts {
						if expected.ID == mount.ID && expected.Target == mount.Target {
							found = true
						}
					}
					if !found {
						t.Fatal("target changed", mount)
					}
					if mount.Kind == workload.Directory {
						if mount.Ownership != workload.ProfileState {
							t.Fatal("live source not fixed", mount)
						}
						data, err := os.ReadFile(filepath.Join(mount.Source, "future"))
						if err != nil || string(data) != mount.ID {
							t.Fatal("complete state missing", err)
						}
					}
				}
			}
		})
	}
}

func TestManagedCustomPathsAndUserAlias(t *testing.T) {
	f := newFixture(t)
	actual := filepath.Join(f.home, "state space λ")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, filepath.Join(f.home, "codex-alias")); err != nil {
		t.Fatal(err)
	}
	f.service.options.Environment["CODEX_HOME"] = filepath.Join(f.home, "codex-alias")
	f.write("state space λ/account", "personal")
	f.save("codex")
	f.migrate()
	named, err := f.service.Select(t.Context(), "codex", "Default")
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := f.service.Select(t.Context(), "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if named.Paths.Mounts[0].Target != actual || ordinary.Paths.Mounts[0].Target != actual {
		t.Fatal("explicit CODEX_HOME target changed")
	}
	f.load("codex", "Work")
	f.write("state space λ/account", "work")
	f.save("codex")
	f.load("codex", "Default")
	if got := f.read("codex-alias/account"); got != "personal" {
		t.Fatal(got)
	}
	f.service.options.Environment["CODEX_HOME"] = filepath.Join(f.home, "other")
	if err := os.Mkdir(filepath.Join(f.home, "other"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work"}); err == nil {
		t.Fatal("changed path environment accepted")
	}
}

func TestManagedMigrationRejectsEscapingLinks(t *testing.T) {
	for _, link := range []string{"../outside", "/etc/passwd", "cycle"} {
		t.Run(link, func(t *testing.T) {
			f := newFixture(t)
			f.write(".codex/account", "personal")
			name := "linked"
			if link == "cycle" {
				name = "cycle"
			}
			if err := os.Symlink(link, filepath.Join(f.home, ".codex", name)); err != nil {
				t.Fatal(err)
			}
			f.save("codex")
			if _, err := f.service.PreviewMigration(t.Context(), ""); err == nil {
				t.Fatal("unsafe link accepted")
			}
			info, err := os.Lstat(filepath.Join(f.home, ".codex"))
			if err != nil || !info.IsDir() {
				t.Fatal("preview changed host", err)
			}
		})
	}
}

func TestManagedOrdinaryCredentialMismatch(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.migrate()
	f.write(".codex/account", "other")
	_, err := f.service.Select(t.Context(), "codex", "")
	requireIssue(t, err, AccountConflict)
}

func TestManagedRestoreRetainsUnexpectedAccount(t *testing.T) {
	f := newFixture(t)
	f.write(".claude/account", "personal")
	f.write(".claude.json", "original settings")
	f.save("claude")
	f.migrate()
	backup := filepath.Join(t.TempDir(), "independent")
	if err := f.service.Backup(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	old := f.profilePath("Default", "claude-state", "account")
	f.write(".claude/account", "unexpected account")
	f.write(".claude.json", "unexpected settings")
	if _, err := f.service.Save(t.Context(), SaveRequest{Harness: "claude"}); err == nil {
		t.Fatal("mismatch was accepted")
	}
	restored, err := f.service.Restore(t.Context(), "claude", "Default", backup)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.read(".claude/account"); got != "personal" {
		t.Fatal(got)
	}
	if got := f.read(".claude.json"); got != "original settings" {
		t.Fatal(got)
	}
	recovery, err := os.OpenRoot(restored.Recovery)
	if err != nil {
		t.Fatal(err)
	}
	var transaction managedTransaction
	err = readJSON(recovery, "managed.json", &transaction)
	recovery.Close()
	if err != nil {
		t.Fatal(err)
	}
	retained := false
	for position, change := range transaction.Changes {
		if change.Path == filepath.Dir(old) {
			path := filepath.Join(filepath.Dir(change.Path), transaction.sibling(position, "before"), "account")
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "unexpected account" {
				t.Fatal("current state was not retained", err)
			}
			retained = true
		}
	}
	if !retained {
		t.Fatal("replaced root is absent from recovery")
	}
	if data, err := os.ReadFile(old); err != nil || string(data) != "personal" {
		t.Fatal("native absolute path did not follow restore", err)
	}
	if _, err := f.service.Select(t.Context(), "claude", "Default"); err != nil {
		t.Fatal(err)
	}
}

func TestManagedRestoreKeepsMountPaths(t *testing.T) {
	f := newFixture(t)
	// A configured Cooper directory can itself be a link, as in the VM tests.
	// Journal entries for directories must use their registered physical path.
	physical := filepath.Join(f.home, ".cooper-data")
	if err := os.Mkdir(physical, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, f.service.options.CooperDir); err != nil {
		t.Fatal(err)
	}
	f.write(".claude/account", "personal")
	f.write(".claude.json", "personal settings")
	f.save("claude")
	f.migrate()
	f.load("claude", "Work")
	f.write(".claude/account", "work")
	f.write(".claude/session", "original")
	f.write(".claude.json", "work settings")
	f.save("claude")
	f.load("claude", "Default")
	before, err := f.service.Select(t.Context(), "claude", "Work")
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := f.service.Backup(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		for _, root := range before.Paths.Mounts {
			path := root.Source
			if root.Kind == workload.Directory {
				path = filepath.Join(path, "session")
			}
			if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.service.Restore(t.Context(), "claude", "Work", backup); err != nil {
			t.Fatal(err)
		}
		after, err := f.service.Select(t.Context(), "claude", "Work")
		if err != nil || !reflect.DeepEqual(before.Paths.Mounts, after.Paths.Mounts) {
			t.Fatalf("restore changed mount paths or alias count: %v", err)
		}
		for _, root := range after.Paths.Mounts {
			path, want := root.Source, "work settings"
			if root.Kind == workload.Directory {
				path, want = filepath.Join(path, "session"), "original"
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != want {
				t.Fatalf("restore did not replace %s: %q, %v", root.ID, data, err)
			}
		}
		if got := f.read(".claude.json"); got != "personal settings" {
			t.Fatal("inactive restore changed host settings", got)
		}
	}
}

func TestManagedBackupDoesNotReplaceConcurrentDestination(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.migrate()
	backup := filepath.Join(t.TempDir(), "independent")
	originalCopy := f.service.copy
	var created os.FileInfo
	f.service.copy = func(ctx context.Context, source, target string) error {
		if created == nil {
			if err := os.Mkdir(backup, 0700); err != nil {
				return err
			}
			var err error
			created, err = os.Stat(backup)
			if err != nil {
				return err
			}
		}
		return originalCopy(ctx, source, target)
	}
	if err := f.service.Backup(t.Context(), backup); !errors.Is(err, os.ErrExist) {
		t.Fatalf("concurrent destination was not refused: %v", err)
	}
	current, err := os.Stat(backup)
	if err != nil || !os.SameFile(created, current) {
		t.Fatal("concurrent destination was replaced", err)
	}
	entries, err := os.ReadDir(backup)
	if err != nil || len(entries) != 0 {
		t.Fatal("concurrent destination was changed", err)
	}
	if got := f.read(".codex/account"); got != "personal" {
		t.Fatal("live state changed", got)
	}
}

func TestManagedPruneRefusesChangedAliasAndWorkingDirectory(t *testing.T) {
	for _, fault := range []string{"changed-alias", "working-directory"} {
		t.Run(fault, func(t *testing.T) {
			f := newFixture(t)
			f.write(".codex/account", "personal")
			f.save("codex")
			original := filepath.Dir(f.profilePath("Default", "codex-state", "account"))
			f.migrate()
			if fault == "changed-alias" {
				host := filepath.Join(f.home, ".codex")
				if err := os.Remove(host); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(original, host); err != nil {
					t.Fatal(err)
				}
			} else {
				f.service.options.Workspace = original
			}
			if err := f.service.PruneRecovery(t.Context()); err == nil {
				t.Fatal("unsafe recovery removal was accepted")
			}
			data, err := os.ReadFile(filepath.Join(original, "account"))
			if err != nil || string(data) != "personal" {
				t.Fatal("recovery state was removed", err)
			}
		})
	}
}

func TestManagedAdoptAnotherHarness(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.write(".agents/skill", "common")
	f.save("codex")
	f.migrate()
	f.write(".grok/account", "grok")
	modified := time.Unix(1700000000, 0)
	for _, relative := range []string{".grok/account", ".grok"} {
		if err := os.Chtimes(filepath.Join(f.home, relative), modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	result := f.save("grok")
	if !result.Managed || !result.Created {
		t.Fatal(result)
	}
	for _, relative := range []string{".grok/account", ".grok"} {
		info, err := os.Stat(filepath.Join(f.home, relative))
		if err != nil || !info.ModTime().Equal(modified) {
			t.Fatalf("adoption changed modification time for %s: %v", relative, err)
		}
	}
	if got := f.read(".agents/skill"); got != "common" {
		t.Fatal(got)
	}
	if info, err := os.Lstat(filepath.Join(f.home, ".grok")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("not adopted", err)
	}
	if _, err := f.service.Select(t.Context(), "grok", "Default"); err != nil {
		t.Fatal(err)
	}
	list, err := f.service.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.Harness == "codex" && !item.Mixed {
			t.Fatal("shared root ownership hidden")
		}
	}
}

func TestManagedCopyAcrossFilesystems(t *testing.T) {
	f := newFixture(t)
	directory, err := os.MkdirTemp("/var/tmp", "cooper-managed-test-")
	if err != nil {
		t.Skip("second temporary filesystem unavailable", err)
	}
	defer os.RemoveAll(directory)
	source, err := os.Stat(f.home)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if source.Sys().(*syscall.Stat_t).Dev == target.Sys().(*syscall.Stat_t).Dev {
		t.Skip("fixture filesystems are identical")
	}
	f.service.options.CooperDir = directory
	f.write(".codex/account", "personal")
	f.save("codex")
	f.migrate()
	f.write(".codex/session", "new state")
	if _, err := f.service.Detach(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := f.read(".codex/session"); got != "new state" {
		t.Fatal(got)
	}
}

func TestManagedPreviewRejectsOverlappingCatalogs(t *testing.T) {
	f := newFixture(t)
	f.service.options.Reader = ReaderFunc(func(context.Context, string, []workload.MountSpec, map[string]string) (Identity, error) {
		return Identity{Key: "fixture", Label: "Fixture"}, nil
	})
	f.write(".codex/account", "fixture")
	f.save("codex")
	f.service.options.Environment["GROK_HOME"] = filepath.Join(f.home, ".codex", "nested")
	f.write(".codex/nested/account", "fixture")
	f.save("grok")
	if _, err := f.service.PreviewMigration(t.Context(), ""); err == nil {
		t.Fatal("overlapping roots accepted")
	}
	if info, err := os.Lstat(filepath.Join(f.home, ".codex")); err != nil || !info.IsDir() {
		t.Fatal("preview changed state", err)
	}
}
