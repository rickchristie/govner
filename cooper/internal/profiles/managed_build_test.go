//go:build linux

package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildInitializesLiveStorageBeforeFirstSave(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.write(".codex/sessions/first", "host conversation")
	before, err := os.Stat(filepath.Join(f.home, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := f.service.PreviewMigration(t.Context(), "")
	if err != nil || preview.Managed || len(preview.Roots) != 0 {
		t.Fatalf("empty preview: %+v %v", preview, err)
	}
	if _, err := os.Lstat(f.service.storePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview created profile state: %v", err)
	}
	result, err := f.service.Migrate(t.Context(), "")
	if err != nil || !result.Managed {
		t.Fatalf("empty store setup: %+v %v", result, err)
	}
	after, err := os.Stat(filepath.Join(f.home, ".codex"))
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("empty store setup changed host state")
	}
	result, err = f.service.Migrate(t.Context(), "")
	if err != nil || !result.Unchanged {
		t.Fatalf("repeat empty setup: %+v %v", result, err)
	}
	if result := f.save("codex"); !result.Managed {
		t.Fatal("first save used copy mode")
	}
	if info, err := os.Lstat(filepath.Join(f.home, ".codex")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("first save did not install a live root: %v %v", info, err)
	}
	f.load("codex", "Work")
	f.write(".codex/account", "work")
	f.save("codex")
	f.load("codex", "Default")
	if f.read(".codex/sessions/first") != "host conversation" {
		t.Fatal("first host conversation was not retained")
	}
}

func TestRepeatBuildChecksLinksWithoutCopyOrUseChecks(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.migrate()
	selector := filepath.Join(f.service.storePath(), "current")
	before, err := os.Readlink(selector)
	if err != nil {
		t.Fatal(err)
	}
	f.service.copy = func(context.Context, string, string) error { return errors.New("unexpected copy") }
	f.service.options.Guard = GuardFunc(func(context.Context, []string) error { return errors.New("active agent") })
	result, err := f.service.Migrate(t.Context(), "")
	if err != nil || !result.Unchanged {
		t.Fatalf("repeat build: %+v %v", result, err)
	}
	after, err := os.Readlink(selector)
	if err != nil || before != after {
		t.Fatal("repeat build changed the selector")
	}
	host := filepath.Join(f.home, ".codex")
	if err := os.Remove(host); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(host, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Migrate(t.Context(), ""); err == nil {
		t.Fatal("repeat build accepted a replaced host link")
	}
}

func TestBuildStoreCleanupRequiresExactUnusedLayout(t *testing.T) {
	for _, change := range []string{"none", "extra-file", "saved-account", "corrupt-metadata", "store-link"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t)
			if _, err := f.service.Migrate(t.Context(), ""); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "extra-file":
				f.write(".cooper/profiles/keep-this", "user data")
			case "saved-account":
				f.write(".codex/account", "personal")
				f.save("codex")
			case "corrupt-metadata":
				f.write(".cooper/profiles/index.json", "invalid metadata")
			case "store-link":
				store := f.service.storePath()
				if err := os.Rename(store, store+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(store+"-original", store); err != nil {
					t.Fatal(err)
				}
			}
			if removable := CanRemoveUnusedStore(f.service.options.CooperDir); removable != (change == "none") {
				t.Fatalf("cleanup permission = %v", removable)
			}
		})
	}
}

func TestManagedRecoverRejectsMissingDescriptorWithoutJournal(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Migrate(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.service.storePath(), "index.json")); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Recover(t.Context()); err == nil {
		t.Fatal("recovery reported success for a store without its descriptor")
	}
	if _, err := os.Lstat(filepath.Join(f.service.storePath(), "index.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("recovery created a descriptor without an authoritative catalog", err)
	}
}
