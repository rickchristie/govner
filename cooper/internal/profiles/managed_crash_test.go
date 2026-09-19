//go:build linux

package profiles

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestManagedCrashProcess(t *testing.T) {
	if base := os.Getenv("COOPER_MANAGED_CRASH_HOME"); base != "" {
		f := newFixture(t)
		f.home = base
		f.service.options.Account.Home = base
		f.service.options.CooperDir = filepath.Join(base, ".cooper")
		f.service.options.Workspace = filepath.Join(filepath.Dir(base), "workspace")
		f.service.managedCheckpoint = func(step string) error {
			if step == os.Getenv("COOPER_MANAGED_CRASH_STEP") {
				os.Exit(73)
			}
			return nil
		}
		var err error
		switch os.Getenv("COOPER_MANAGED_CRASH_OPERATION") {
		case "restore":
			_, err = f.service.Restore(t.Context(), "claude", "Default", filepath.Join(filepath.Dir(base), "backup"))
		case "detach":
			_, err = f.service.Detach(t.Context())
		default:
			_, err = f.service.Migrate(t.Context(), "")
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Fatal("crash point not reached")
	}
	for _, step := range []string{"journal", "old-root-0", "new-root-0", "root-0", "old-root-1", "new-root-1", "root-1", "old-root-2", "new-root-2", "root-2", "old-root-3", "new-root-3", "root-3", "selector", "selector-sync", "finish"} {
		t.Run(step, func(t *testing.T) {
			f := newFixture(t)
			f.write(".codex/account", "personal")
			f.save("codex")
			child := exec.Command(os.Args[0], "-test.run=^TestManagedCrashProcess$")
			child.Env = append(os.Environ(), "COOPER_MANAGED_CRASH_HOME="+f.home, "COOPER_MANAGED_CRASH_STEP="+step)
			output, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("child did not stop at %s: %v %s", step, err, output)
			}
			if err := f.service.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := f.read(".codex/account"); got != "personal" {
				t.Fatal(got)
			}
			if err := CheckReady(f.service.options.CooperDir); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// The first build has no saved catalog. A process exit before the journal
// must still leave enough metadata for recovery and a successful retry.
func TestManagedInitialBuildCrashRecovery(t *testing.T) {
	for _, step := range []string{"migration-index", "view-directories", "view", "journal", "selector", "selector-sync", "finish"} {
		t.Run(step, func(t *testing.T) {
			f := newFixture(t)
			f.write(".codex/account", "personal")
			host := filepath.Join(f.home, ".codex")
			before, err := os.Stat(host)
			if err != nil {
				t.Fatal(err)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestManagedCrashProcess$")
			child.Env = append(os.Environ(), "COOPER_MANAGED_CRASH_HOME="+f.home, "COOPER_MANAGED_CRASH_STEP="+step)
			output, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("child did not stop at %s: %v %s", step, err, output)
			}
			for range 2 {
				if err := f.service.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			result, err := f.service.Migrate(t.Context(), "")
			if err != nil || !result.Managed {
				t.Fatalf("build cannot retry after recovery: %+v %v", result, err)
			}
			after, err := os.Stat(host)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("empty setup moved host state", err)
			}
			if result := f.save("codex"); !result.Managed {
				t.Fatal("first save after recovery did not use live storage")
			}
			if got := f.read(".codex/account"); got != "personal" {
				t.Fatal("first save changed the account", got)
			}
		})
	}
}

// Restore and detach replace physical directories that a native database can
// reference. Stop between each rename, without running deferred rollback.
func TestManagedCanonicalCrashRecovery(t *testing.T) {
	for _, operation := range []string{"restore", "detach"} {
		changes := 2
		if operation == "restore" {
			changes = 3 // Stored directory, stored file, and active host file.
		}
		steps := []string{"journal"}
		for position := range changes {
			for _, prefix := range []string{"old-root-", "new-root-", "root-"} {
				steps = append(steps, prefix+strconv.Itoa(position))
			}
		}
		steps = append(steps, "selector", "selector-sync", "finish")
		for _, step := range steps {
			t.Run(operation+"/"+step, func(t *testing.T) {
				f := newFixture(t)
				f.write(".claude/account", "personal")
				f.write(".claude/session", "original")
				f.write(".claude.json", "original")
				f.save("claude")
				f.migrate()
				canonical, err := filepath.EvalSymlinks(filepath.Join(f.home, ".claude"))
				if err != nil {
					t.Fatal(err)
				}
				if operation == "restore" {
					if err := f.service.Backup(t.Context(), filepath.Join(filepath.Dir(f.home), "backup")); err != nil {
						t.Fatal(err)
					}
				}
				f.write(".claude/session", "latest")
				f.write(".claude.json", "latest")
				child := exec.Command(os.Args[0], "-test.run=^TestManagedCrashProcess$")
				child.Env = append(os.Environ(), "COOPER_MANAGED_CRASH_HOME="+f.home, "COOPER_MANAGED_CRASH_STEP="+step, "COOPER_MANAGED_CRASH_OPERATION="+operation)
				output, err := child.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 73 {
					t.Fatalf("child did not stop at %s: %v %s", step, err, output)
				}
				for range 2 {
					if err := f.service.Recover(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				committed := step == "selector" || step == "selector-sync" || step == "finish"
				want := "latest"
				if operation == "restore" && committed {
					want = "original"
				}
				for _, path := range []string{filepath.Join(f.home, ".claude/session"), filepath.Join(canonical, "session"), filepath.Join(f.home, ".claude.json")} {
					data, err := os.ReadFile(path)
					if err != nil || string(data) != want {
						t.Fatalf("recovered %s: %q, want %q: %v", path, data, want, err)
					}
				}
				managed, err := f.service.usesManaged()
				if err != nil || managed != (operation != "detach" || !committed) {
					t.Fatalf("wrong storage mode after recovery: managed=%v, error=%v", managed, err)
				}
				if _, err := f.service.Select(t.Context(), "claude", "Default"); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestManagedSelectorFailureRestoresFiles(t *testing.T) {
	f := newFixture(t)
	f.write(".claude/account", "personal")
	f.write(".claude.json", "personal file")
	f.save("claude")
	f.migrate()
	f.load("claude", "Work")
	f.write(".claude/account", "work")
	f.write(".claude.json", "work file")
	f.save("claude")
	rename := f.service.rename
	f.service.rename = func(root *os.Root, from, to string) error {
		if to == "current" {
			return syscall.ENOSPC
		}
		return rename(root, from, to)
	}
	_, err := f.service.Load(t.Context(), LoadRequest{Harness: "claude", Name: "Default"})
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatal("wrong failure", err)
	}
	if got := f.read(".claude/account"); got != "work" {
		t.Fatal(got)
	}
	if got := f.read(".claude.json"); got != "work file" {
		t.Fatal(got)
	}
	if err := CheckReady(f.service.options.CooperDir); err != nil {
		t.Fatal(err)
	}
}

func TestManagedRecoveryRejectsChangedParent(t *testing.T) {
	f := newFixture(t)
	parent := filepath.Join(f.home, "custom")
	f.service.options.Environment["CODEX_HOME"] = filepath.Join(parent, "codex")
	f.write("custom/codex/account", "personal")
	f.save("codex")
	f.service.managedCheckpoint = func(step string) error {
		if step == "journal" {
			return errors.New("stop")
		}
		return nil
	}
	if _, err := f.service.Migrate(context.Background(), ""); err == nil {
		t.Fatal("fault missing")
	}
	if err := os.Rename(parent, parent+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	f.service.managedCheckpoint = func(string) error { return nil }
	if err := f.service.Recover(t.Context()); err == nil {
		t.Fatal("changed parent accepted")
	}
	if got := f.read("custom-original/codex/account"); got != "personal" {
		t.Fatal(got)
	}
}
