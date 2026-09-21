package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupRestoreKeepsPathsAndIndependentData(t *testing.T) {
	f := newFixture(t)
	f.write(".claude/account", "personal")
	f.write(".claude/session", "first")
	f.write(".claude.json", "settings")
	f.save("claude")
	f.load("claude", "Work")
	f.write(".claude/account", "work")
	f.write(".claude/session", "work")
	f.save("claude")
	backup := filepath.Join(t.TempDir(), "backup")
	if err := f.service.Backup(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	path := f.profilePath("Default", "claude-state", "")
	old := inode(t, path)
	if err := os.WriteFile(filepath.Join(path, "session"), []byte("later"), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		_, err := f.service.Restore(t.Context(), RestoreRequest{Harness: "claude", Name: "Default", Backup: backup})
		requireIssue(t, err, ConfirmationRequired)
		result, err := f.service.Restore(t.Context(), RestoreRequest{Harness: "claude", Name: "Default", Backup: backup, Confirmed: true})
		if err != nil {
			t.Fatal(err)
		}
		if result.Recovery == "" || path != f.profilePath("Default", "claude-state", "") {
			t.Fatal("restore changed the mount source")
		}
		if readFile(t, filepath.Join(path, "session")) != "first" || inode(t, path) == old {
			t.Fatal("restore did not use an independent copy")
		}
		selection, err := f.service.Select(t.Context(), "claude", "Default")
		if err != nil || len(selection.Paths.Mounts) != 2 {
			t.Fatalf("restore added mounts: %v", err)
		}
	}
	if f.read(".claude/session") != "work" {
		t.Fatal("inactive restore changed active account")
	}
	f.load("claude", "Default")
	if f.read(".claude/session") != "first" || f.read(".claude.json") != "settings" {
		t.Fatal("restored profile did not load")
	}
	if err := f.service.PruneRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.read(".claude/session") != "first" {
		t.Fatal("cleanup removed current state")
	}
	if err := f.service.Backup(t.Context(), backup); err == nil {
		t.Fatal("backup overwrote a destination")
	}
}

func TestBackupFailureDoesNotPublishOrChangeState(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.write(".codex/session", "keep")
	f.save("codex")
	f.service.copy = func(context.Context, string, string) error { return errors.New("fixture copy failure") }
	backup := filepath.Join(t.TempDir(), "backup")
	if err := f.service.Backup(t.Context(), backup); err == nil {
		t.Fatal("copy failure ignored")
	}
	if _, err := os.Lstat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial backup was published")
	}
	if f.read(".codex/session") != "keep" {
		t.Fatal("failed copy changed live state")
	}
}

func TestRecoveryCleanupRejectsReplacementEntry(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	backup := filepath.Join(t.TempDir(), "backup")
	if err := f.service.Backup(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Restore(t.Context(), RestoreRequest{Harness: "codex", Name: "Default", Backup: backup, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(result.Recovery)
	id = id[:len(id)-len(".json")]
	path := filepath.Join(f.home, ".codex.cooper-recovery-"+id)
	if err := os.Rename(path, path+"-retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := f.service.PruneRecovery(t.Context()); err == nil {
		t.Fatal("cleanup accepted an unexpected directory")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("unexpected directory was removed")
	}
}
