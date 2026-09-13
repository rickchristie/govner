package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestFailedCopyKeepsHostAndPublishedProfile(t *testing.T) {
	for _, operation := range []string{"save", "load"} {
		t.Run(operation, func(t *testing.T) {
			f := newFixture(t)
			f.write(".codex/account", "personal")
			f.write(".codex/session", "before")
			f.save("codex")
			before := f.state().Profiles[0]
			f.service.copy = func(ctx context.Context, source, target string) error {
				if err := os.Mkdir(target, 0700); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(target, "partial"), []byte("partial"), 0600); err != nil {
					return err
				}
				return syscall.ENOSPC
			}
			var err error
			if operation == "save" {
				f.write(".codex/session", "host change")
				_, err = f.service.Save(t.Context(), SaveRequest{Harness: "codex"})
			} else {
				_, err = f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work"})
			}
			if !errors.Is(err, syscall.ENOSPC) {
				t.Fatalf("copy fault: %v", err)
			}
			state := f.state()
			if state.byName("codex", "Default").Generation != before.Generation || f.read(".codex/account") != "personal" {
				t.Fatal("failed copy changed authority or host account")
			}
			entries, err := filepath.Glob(filepath.Join(f.home, ".cooper-profile-*-next"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("untracked staging copies: %v %v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(f.service.storePath(), transactionFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("copy failure published a journal: %v", err)
			}
		})
	}
}

func TestMetadataCommitFailureUsesDiskAuthority(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[committed], func(t *testing.T) {
			f := newFixture(t)
			f.write(".codex/account", "personal")
			f.save("codex")
			f.service.publish = func(root *os.Root, state index) error {
				if state.LastTransaction == "" {
					return writeJSON(root, "index.json", state)
				}
				if committed {
					if err := writeJSON(root, "index.json", state); err != nil {
						return err
					}
				}
				return errors.New("injected metadata sync failure")
			}
			if _, err := f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work"}); err == nil {
				t.Fatal("commit fault was not reached")
			}
			state := f.state()
			want := "Default"
			if committed {
				want = "Work"
			}
			if state.byID(state.Hosts["codex"].ProfileID).Name != want {
				t.Fatal("disk authority was not respected")
			}
			if !committed && f.read(".codex/account") != "personal" {
				t.Fatal("uncommitted host was not restored")
			}
			if committed {
				if _, err := os.Stat(filepath.Join(f.home, ".codex/account")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("committed host was rolled back")
				}
			}
			if err := CheckReady(f.service.options.CooperDir); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLateWriterKeepsJournalAndBothVersions(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	installs := 0
	f.service.rename = func(root *os.Root, from, to string) error {
		if err := root.Rename(from, to); err != nil {
			return err
		}
		if strings.HasSuffix(from, "-next") {
			installs++
			if installs == 4 {
				f.write(".codex/new-session", "late writer")
			}
		}
		return nil
	}
	if _, err := f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work"}); err == nil {
		t.Fatal("late writer was overwritten")
	}
	if f.read(".codex/new-session") != "late writer" {
		t.Fatal("late writer state lost")
	}
	if err := CheckReady(f.service.options.CooperDir); err == nil {
		t.Fatal("unfinished transaction permitted runtime startup")
	}
	backups, err := filepath.Glob(filepath.Join(f.home, ".cooper-profile-*-codex-state-before", "account"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("original backup: %v %v", backups, err)
	}
	data, err := os.ReadFile(backups[0])
	if err != nil || string(data) != "personal" {
		t.Fatal("original state was lost")
	}
}

func TestConflictChoicesKeepAnIndependentRecovery(t *testing.T) {
	for _, choice := range []ConflictChoice{KeepHost, KeepSaved} {
		t.Run(string(choice), func(t *testing.T) {
			f := newFixture(t)
			f.write(".codex/account", "personal")
			f.write(".codex/session", "base")
			f.save("codex")
			previous := f.profilePath("Default", "codex-state", "session")
			if err := os.WriteFile(previous, []byte("saved change"), 0600); err != nil {
				t.Fatal(err)
			}
			f.write(".codex/session", "host change")
			result, err := f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Default", ConflictChoice: choice})
			if err != nil {
				t.Fatal(err)
			}
			want := "host change"
			if choice == KeepSaved {
				want = "saved change"
			}
			if f.read(".codex/session") != want {
				t.Fatal("wrong conflict choice loaded")
			}
			data, err := os.ReadFile(previous)
			if err != nil || string(data) != "saved change" {
				t.Fatal("saved version lost")
			}
			if result.Recovery == "" && result.Warning == "" {
				t.Fatal("host recovery was not reported")
			}
		})
	}
}

func TestManualSavedAccountChangeCannotBeOverwritten(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	target := f.profilePath("Default", "codex-state", "account")
	if err := os.WriteFile(target, []byte("enterprise"), 0600); err != nil {
		t.Fatal(err)
	}
	f.write(".codex/session", "new host state")
	_, err := f.service.Save(t.Context(), SaveRequest{Harness: "codex", ConflictChoice: KeepHost})
	requireIssue(t, err, AccountConflict)
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "enterprise" {
		t.Fatal("saved account was overwritten")
	}
}

func TestRootPolicyRefreshKeepsPreviousGeneration(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	state := f.state()
	oldGeneration := state.Profiles[0].Generation
	state.Profiles[0].Policy = strings.Repeat("a", 64)
	root, err := f.service.open(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := writeJSON(root, "index.json", state); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Select(t.Context(), "codex", "Default"); err == nil {
		t.Fatal("old policy launched")
	}
	f.write(".agents/future-state", "future catalog data")
	f.save("codex")
	current := f.state().Profiles[0]
	if current.PreviousGeneration != oldGeneration || current.Policy == state.Profiles[0].Policy {
		t.Fatal("refresh lost its recovery or old policy remained")
	}
	if _, err := f.service.Select(t.Context(), "codex", "Default"); err != nil {
		t.Fatal(err)
	}
}

func TestNewCredentialVariableRequiresAndPermitsRefresh(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.service.options.CredentialNames = func(string) []string { return []string{"NEW_API_KEY", "TEST_API_KEY"} }
	f.service.options.Environment["NEW_API_KEY"] = "new-fixture-key"
	if _, err := f.service.Select(t.Context(), "codex", "Default"); err == nil {
		t.Fatal("new credential silently fell back to host")
	}
	f.save("codex")
	selected, err := f.service.Select(t.Context(), "codex", "Default")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Credentials) != 2 || selected.Credentials[0].Value != "new-fixture-key" {
		t.Fatal("credential scope did not refresh")
	}
}

func TestTamperedJournalCannotReplaceAnUnrelatedPath(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.service.rename = func(root *os.Root, from, to string) error {
		if err := root.Rename(from, to); err != nil {
			return err
		}
		panic("fixture process exit")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("crash was not reached")
			}
		}()
		_, _ = f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work"})
	}()
	root, err := f.service.open(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var txn transaction
	if err := readJSON(root, transactionFile, &txn); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	txn.Roots[0].Root.HostPath = outside
	if err := writeJSON(root, transactionFile, txn); err != nil {
		t.Fatal(err)
	}
	f.service = New(f.service.options)
	if _, err := f.service.Save(t.Context(), SaveRequest{Harness: "codex"}); err == nil {
		t.Fatal("tampered journal was applied")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
		t.Fatal("unrelated data changed")
	}
	if err := CheckReady(f.service.options.CooperDir); err == nil {
		t.Fatal("unresolved journal was discarded")
	}
}
