package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/statelock"
)

func (f fixture) oldCopyStore() string {
	f.t.Helper()
	data := strings.ReplaceAll(readFile(f.t, "testdata/old-copy-index.json"), "/old-home", f.home)
	f.write(".cooper/profiles/index.json", data)
	f.write(".cooper/profiles/harnesses/antigravity/snapshot/session", "saved session")
	f.write(".cooper/profiles/recovery/retained/session", "recovery session")
	f.write(".gemini/session", "live session")
	f.write(".codex/account", "personal")
	return data
}

func TestRetireOldCopiesPreservesLiveStateAndAllowsLaunch(t *testing.T) {
	f := newFixture(t)
	indexData := f.oldCopyStore()
	storeID := inode(t, f.service.storePath())
	geminiID := inode(t, filepath.Join(f.home, ".gemini"))
	codexID := inode(t, filepath.Join(f.home, ".codex"))
	if err := CheckReady(f.service.options.CooperDir); err == nil {
		t.Fatal("old store did not reproduce the launch error")
	}
	backup, err := f.service.RetireOldCopies(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(backup) != f.service.options.CooperDir || !strings.HasPrefix(filepath.Base(backup), copyBackupPrefix) || inode(t, backup) != storeID {
		t.Fatal("reset did not retain the complete store in place")
	}
	for path, want := range map[string]string{
		"index.json":                             indexData,
		"harnesses/antigravity/snapshot/session": "saved session",
		"recovery/retained/session":              "recovery session",
	} {
		if readFile(t, filepath.Join(backup, path)) != want {
			t.Fatal("retained data changed", path)
		}
	}
	if inode(t, filepath.Join(f.home, ".gemini")) != geminiID || inode(t, filepath.Join(f.home, ".codex")) != codexID || f.read(".gemini/session") != "live session" {
		t.Fatal("reset changed live state")
	}
	if err := CheckReady(f.service.options.CooperDir); err != nil {
		t.Fatal(err)
	}
	selection, err := f.service.SelectID(t.Context(), "codex", "")
	if err != nil || selection.ID != "" || selection.Paths.Mounts[0].Source != filepath.Join(f.home, ".codex") {
		t.Fatalf("ordinary launch did not select live Codex state: %+v %v", selection, err)
	}
	if _, err := f.service.Select(t.Context(), "antigravity", "Default"); err == nil {
		t.Fatal("old named profile silently selected live state")
	}
	if CanRemoveUnusedStore(f.service.options.CooperDir) {
		t.Fatal("cleanup could delete the retained store")
	}
	if next, err := f.service.RetireOldCopies(t.Context()); err != nil || next != "" {
		t.Fatalf("repeat reset: %q %v", next, err)
	}
	f.save("codex")
	if f.state().Schema != Schema || inode(t, filepath.Join(f.home, ".codex")) != codexID || readFile(t, filepath.Join(backup, "index.json")) != indexData {
		t.Fatal("new registration changed live or retained state")
	}
}

func TestRetireOldCopiesRejectsUnsafeStores(t *testing.T) {
	for _, kind := range []string{"journal", "managed", "unknown", "invalid-json", "trailing-json", "unknown-field", "public-index", "index-link", "store-link", "live-link", "recorded-link"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			data := f.oldCopyStore()
			switch kind {
			case "journal":
				f.write(".cooper/profiles/transaction.json", `{}`)
			case "managed":
				f.write(".cooper/profiles/managed.json", `{"schema":2}`)
			case "unknown":
				f.write(".cooper/profiles/unknown", "retain")
			case "invalid-json":
				f.write(".cooper/profiles/index.json", `{"schema":1,`)
			case "trailing-json":
				f.write(".cooper/profiles/index.json", data+`{}`)
			case "unknown-field":
				f.write(".cooper/profiles/index.json", strings.Replace(data, `"schema": 1`, `"unknown": true, "schema": 1`, 1))
			case "public-index":
				if err := os.Chmod(filepath.Join(f.service.storePath(), "index.json"), 0644); err != nil {
					t.Fatal(err)
				}
			case "index-link":
				path := filepath.Join(f.service.storePath(), "index.json")
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("index.json.original", path); err != nil {
					t.Fatal(err)
				}
			case "store-link":
				path := f.service.storePath()
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("profiles.original", path); err != nil {
					t.Fatal(err)
				}
			case "live-link", "recorded-link":
				name := ".codex"
				if kind == "recorded-link" {
					name = ".gemini"
				}
				path := filepath.Join(f.home, name)
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(f.service.storePath(), "harnesses/antigravity/snapshot"), path); err != nil {
					t.Fatal(err)
				}
			}
			before := readFile(t, filepath.Join(f.service.storePath(), "index.json"))
			if backup, err := f.service.RetireOldCopies(t.Context()); err == nil || backup != "" {
				t.Fatalf("unsafe reset succeeded: %q %v", backup, err)
			}
			if readFile(t, filepath.Join(f.service.storePath(), "index.json")) != before || f.read(".cooper/profiles/recovery/retained/session") != "recovery session" {
				t.Fatal("failed reset changed the old store")
			}
		})
	}
}

func TestRetireOldCopiesLeavesOtherSchemasUnchanged(t *testing.T) {
	for _, data := range []string{"", `{"schema":2}`, `{"schema":3,"profiles":[],"hosts":{}}`, `{"schema":4,"future":true}`} {
		f := newFixture(t)
		if data != "" {
			f.write(".cooper/profiles/index.json", data)
		}
		if backup, err := f.service.RetireOldCopies(t.Context()); err != nil || backup != "" {
			t.Fatalf("changed another format: %q %v", backup, err)
		}
		if data != "" && f.read(".cooper/profiles/index.json") != data {
			t.Fatal("index changed")
		}
	}
}

func TestRetireOldCopiesWaitsForStartupAndPreservesFailedRename(t *testing.T) {
	f := newFixture(t)
	data := f.oldCopyStore()
	lock, err := statelock.Acquire(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	_, resetErr := f.service.RetireOldCopies(ctx)
	lock.Close()
	if !errors.Is(resetErr, context.DeadlineExceeded) {
		t.Fatalf("reset crossed startup lock: %v", resetErr)
	}
	failure := errors.New("rename failed")
	f.service.rename = func(*os.Root, string, string) error { return failure }
	if _, err := f.service.RetireOldCopies(t.Context()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if f.read(".cooper/profiles/index.json") != data {
		t.Fatal("failed reset changed the index")
	}
	f.service.rename = renameEntry
	if _, err := f.service.RetireOldCopies(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRetireOldCopiesContinuesAfterInterruptedRename(t *testing.T) {
	f := newFixture(t)
	data := f.oldCopyStore()
	var backup string
	f.service.rename = func(parent *os.Root, from, to string) error {
		if err := renameEntry(parent, from, to); err != nil {
			return err
		}
		backup = filepath.Join(f.service.options.CooperDir, to)
		return errors.New("interrupted after rename")
	}
	if _, err := f.service.RetireOldCopies(t.Context()); err == nil {
		t.Fatal("reset did not stop at the interruption")
	}
	f.service.rename = renameEntry
	if next, err := f.service.RetireOldCopies(t.Context()); err != nil || next != "" {
		t.Fatalf("reset could not continue: %q %v", next, err)
	}
	if readFile(t, filepath.Join(backup, "index.json")) != data {
		t.Fatal("interrupted reset lost the old index")
	}
	if err := CheckReady(f.service.options.CooperDir); err != nil {
		t.Fatal(err)
	}
}
