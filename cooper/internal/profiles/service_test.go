package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/usercontext"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

type fixture struct {
	t       testing.TB
	service *Service
	home    string
}

func newFixture(t testing.TB) fixture {
	t.Helper()
	base := t.TempDir()
	home, workspace := filepath.Join(base, "home"), filepath.Join(base, "workspace")
	for _, path := range []string{home, workspace} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	reader := ReaderFunc(func(ctx context.Context, harness string, mounts []workload.MountSpec, env map[string]string) (Identity, error) {
		for _, mount := range mounts {
			if mount.ID != harness+"-state" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(mount.Source, "account"))
			if err != nil {
				return Identity{}, err
			}
			return Identity{Key: string(data), Label: string(data)}, nil
		}
		return Identity{}, errors.New("no identity root")
	})
	service := New(Options{CooperDir: filepath.Join(home, ".cooper"), Workspace: workspace,
		Account:     usercontext.Account{Name: "tester", Group: "tester", UID: 1000, GID: 1000, Home: home},
		Environment: map[string]string{}, CredentialNames: func(string) []string { return []string{"TEST_API_KEY"} },
		Reader: reader, Guard: GuardFunc(func(context.Context, []string) error { return nil })})
	return fixture{t: t, service: service, home: home}
}

func (f fixture) write(relative, content string) {
	f.t.Helper()
	path := filepath.Join(f.home, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f fixture) read(relative string) string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.home, relative))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f fixture) save(harness string) Result {
	f.t.Helper()
	result, err := f.service.Save(context.Background(), SaveRequest{Harness: harness})
	if err != nil {
		f.t.Fatal(err)
	}
	return result
}

func (f fixture) load(harness, name string) Result {
	f.t.Helper()
	result, err := f.service.Load(context.Background(), LoadRequest{Harness: harness, Name: name})
	if err != nil {
		f.t.Fatal(err)
	}
	return result
}

func (f fixture) state() index {
	f.t.Helper()
	root, err := f.service.open(false)
	if err != nil {
		f.t.Fatal(err)
	}
	defer root.Close()
	state, err := readIndex(root)
	if err != nil {
		f.t.Fatal(err)
	}
	return state
}

func (f fixture) profilePath(name, root, child string) string {
	f.t.Helper()
	state := f.state()
	for _, profile := range state.Profiles {
		if profile.Name == name {
			return filepath.Join(f.service.storePath(), dataPath(profile), "roots", root, child)
		}
	}
	f.t.Fatal("profile does not exist", name)
	return ""
}

func requireIssue(t *testing.T, err error, kind IssueKind) *Issue {
	t.Helper()
	var issue *Issue
	if !errors.As(err, &issue) || issue.Kind != kind {
		t.Fatalf("want %s, got %v", kind, err)
	}
	return issue
}

func TestSaveLoadRoundTrip(t *testing.T) {
	f := newFixture(t)
	f.write(".claude/account", "personal")
	f.write(".claude/sessions/first", "personal session")
	f.write(".claude.json", "personal settings")
	first := f.save("claude")
	if first.Saved != "Default" || !first.Created {
		t.Fatalf("first save: %+v", first)
	}
	f.write(".claude/sessions/later", "later personal session")
	loaded := f.load("claude", "Work")
	if loaded.Saved != "Default" || !loaded.Pending || !loaded.Created {
		t.Fatalf("fresh load: %+v", loaded)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".claude.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old optional settings remained: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(f.home, ".claude")); err != nil || len(entries) != 0 {
		t.Fatalf("fresh state: %v %v", entries, err)
	}
	if !f.load("claude", "work").Unchanged {
		t.Fatal("same empty profile was replaced")
	}
	f.write(".claude/account", "enterprise")
	f.write(".claude/sessions/work", "work session")
	if result := f.save("claude"); result.Saved != "Work" {
		t.Fatalf("pending save: %+v", result)
	}
	f.write(".claude/sessions/later", "later work session")
	f.load("claude", "default")
	if f.read(".claude/account") != "personal" || f.read(".claude/sessions/later") != "later personal session" || f.read(".claude.json") != "personal settings" {
		t.Fatal("personal state was not restored")
	}
	if _, err := os.Stat(filepath.Join(f.home, ".claude/sessions/work")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("work-only file remained")
	}
	f.load("claude", "WORK")
	if f.read(".claude/account") != "enterprise" || f.read(".claude/sessions/later") != "later work session" {
		t.Fatal("work state was not restored")
	}
	if err := f.service.Delete(context.Background(), "claude", "Default"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Delete(context.Background(), "claude", "Work"); err == nil {
		t.Fatal("deleted host selection")
	}
}

func TestAccountMappingCannotSelectWrongDestination(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.load("codex", "Work")
	f.write(".codex/account", "personal")
	_, err := f.service.Save(context.Background(), SaveRequest{Harness: "codex"})
	requireIssue(t, err, AccountConflict)
	f.write(".codex/account", "enterprise")
	f.save("codex")
	_, err = f.service.Save(context.Background(), SaveRequest{Harness: "codex", NewName: "Default"})
	if err == nil {
		t.Fatal("explicit name overwrote another profile")
	}
	f.write(".codex/account", "third-account")
	_, err = f.service.Save(context.Background(), SaveRequest{Harness: "codex"})
	requireIssue(t, err, NameRequired)
	_, err = f.service.Save(context.Background(), SaveRequest{Harness: "codex", NewName: "default"})
	if err == nil {
		t.Fatal("case-insensitive duplicate accepted")
	}
	result, err := f.service.Save(context.Background(), SaveRequest{Harness: "codex", NewName: "Playground"})
	if err != nil || result.Saved != "Playground" {
		t.Fatalf("new account: %+v %v", result, err)
	}
	state := f.state()
	if state.byName("codex", "Default").Identity.Key != "personal" {
		t.Fatal("Default mapping changed")
	}
}

func TestUnverifiedLoginPreservesState(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	if err := os.Remove(filepath.Join(f.home, ".codex/account")); err != nil {
		t.Fatal(err)
	}
	f.write(".codex/session", "session after logout")
	_, err := f.service.Load(context.Background(), LoadRequest{Harness: "codex", Name: "Work"})
	issue := requireIssue(t, err, IdentityUnknown)
	if issue.Recovery == "" {
		t.Fatal("unverified state was not preserved")
	}
	data, err := os.ReadFile(filepath.Join(issue.Recovery, "roots", "codex-state", "session"))
	if err != nil || string(data) != "session after logout" {
		t.Fatalf("recovery: %q %v", data, err)
	}
	if f.read(".codex/session") != "session after logout" {
		t.Fatal("host was replaced")
	}
}

func TestSavedRuntimeChangesAndConflicts(t *testing.T) {
	for _, both := range []bool{false, true} {
		t.Run(fmt.Sprint("both-", both), func(t *testing.T) {
			f := newFixture(t)
			f.write(".codex/account", "personal")
			f.write(".codex/session", "base")
			f.save("codex")
			profileSession := f.profilePath("Default", "codex-state", "session")
			if err := os.WriteFile(profileSession, []byte("runtime change"), 0o600); err != nil {
				t.Fatal(err)
			}
			if both {
				f.write(".codex/session", "host change")
			}
			_, err := f.service.Load(context.Background(), LoadRequest{Harness: "codex", Name: "Default"})
			if both {
				requireIssue(t, err, StateConflict)
				if f.read(".codex/session") != "host change" {
					t.Fatal("host conflict was overwritten")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.read(".codex/session") != "runtime change" {
				t.Fatal("saved runtime change was lost")
			}
		})
	}
}

func TestLoadRollsBackEveryRenameFailure(t *testing.T) {
	for step := 1; step <= 8; step++ {
		for _, after := range []bool{false, true} {
			t.Run(fmt.Sprintf("step-%d-after-%v", step, after), func(t *testing.T) {
				f := newFixture(t)
				for _, relative := range []string{".codex/account", ".agents/state", ".claude-plugin/state", ".cursor-plugin/state"} {
					f.write(relative, "personal")
				}
				f.save("codex")
				_, roots, err := f.service.hostScope("codex")
				if err != nil {
					t.Fatal(err)
				}
				before, err := digestRoots(context.Background(), roots, hostSource, f.service.credentials("codex"))
				if err != nil {
					t.Fatal(err)
				}
				calls := 0
				f.service.rename = func(root *os.Root, from, to string) error {
					calls++
					if calls == step && !after {
						return errors.New("injected rename failure")
					}
					if err := root.Rename(from, to); err != nil {
						return err
					}
					if calls == step && after {
						return errors.New("injected rename failure")
					}
					return nil
				}
				_, err = f.service.Load(context.Background(), LoadRequest{Harness: "codex", Name: "Work"})
				if err == nil {
					t.Fatal("fault did not stop load")
				}
				actual, err := digestRoots(context.Background(), roots, hostSource, f.service.credentials("codex"))
				if err != nil || before != actual {
					t.Fatalf("rollback changed state: %s %s %v", before, actual, err)
				}
				state := f.state()
				if state.byID(state.Hosts["codex"].ProfileID).Name != "Default" {
					t.Fatal("failed load changed selected profile")
				}
			})
		}
	}
}

func TestInterruptedLoadRecoversOnNextCommand(t *testing.T) {
	for step := 1; step <= 8; step++ {
		t.Run(fmt.Sprint(step), func(t *testing.T) {
			f := newFixture(t)
			for _, relative := range []string{".codex/account", ".agents/state", ".claude-plugin/state", ".cursor-plugin/state"} {
				f.write(relative, "personal")
			}
			f.service.options.Environment["CODEX_HOME"] = filepath.Join(f.home, ".codex")
			f.save("codex")
			calls := 0
			f.service.rename = func(root *os.Root, from, to string) error {
				if err := root.Rename(from, to); err != nil {
					return err
				}
				calls++
				if calls == step {
					panic("simulated process exit")
				}
				return nil
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Error("process exit was not injected")
					}
				}()
				_, _ = f.service.Load(context.Background(), LoadRequest{Harness: "codex", Name: "Work"})
			}()
			f.service = New(f.service.options)
			f.save("codex")
			if f.read(".codex/account") != "personal" || f.read(".agents/state") != "personal" {
				t.Fatal("interrupted load lost outgoing state")
			}
			if _, err := os.Stat(filepath.Join(f.service.storePath(), transactionFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("journal remained after recovery")
			}
		})
	}
}

func TestLoadChecksEnvironmentAndUsageBeforeReplacement(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.service.options.Environment["TEST_API_KEY"] = "do-not-print-this-secret"
	_, err := f.service.Load(context.Background(), LoadRequest{Harness: "codex", Name: "Work"})
	if err == nil || !strings.Contains(err.Error(), "unset TEST_API_KEY") || strings.Contains(err.Error(), "do-not-print") {
		t.Fatalf("credential check: %v", err)
	}
	delete(f.service.options.Environment, "TEST_API_KEY")
	f.service.options.Guard = GuardFunc(func(context.Context, []string) error {
		return &Issue{Kind: StateInUse, Message: "fixture runtime uses this state"}
	})
	_, err = f.service.Load(context.Background(), LoadRequest{Harness: "codex", Name: "Work"})
	requireIssue(t, err, StateInUse)
	if f.read(".codex/account") != "personal" {
		t.Fatal("busy host was replaced")
	}
}

func TestProfileNamesRejectPathsAndControls(t *testing.T) {
	for _, name := range []string{"", "../work", "/tmp/work", "work/personal", "a\nb", "a\x00b", "1work", strings.Repeat("a", 41)} {
		if ValidateName(name) == nil {
			t.Errorf("accepted %q", name)
		}
	}
}
