package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS != "linux" {
		t.Skip("profiles are Linux-only")
	}
	return fixtureAt(t, t.TempDir())
}

func fixtureAt(t testing.TB, base string) fixture {
	t.Helper()
	home, workspace := filepath.Join(base, "home"), filepath.Join(base, "workspace")
	for _, path := range []string{home, workspace} {
		if err := os.MkdirAll(path, 0700); err != nil {
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
		Account:     usercontext.Account{Name: "tester", Group: "tester", UID: os.Getuid(), GID: os.Getgid(), Home: home},
		Environment: map[string]string{}, CredentialNames: func(string) []string { return []string{"TEST_API_KEY"} },
		Reader: reader, Guard: GuardFunc(func(context.Context, []string) error { return nil })})
	return fixture{t, service, home}
}

func (f fixture) write(relative, content string) {
	f.t.Helper()
	path := filepath.Join(f.home, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		f.t.Fatal(err)
	}
}

func (f fixture) read(relative string) string {
	f.t.Helper()
	return readFile(f.t, filepath.Join(f.home, relative))
}

func readFile(t testing.TB, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
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
	result, err := f.service.Load(context.Background(), LoadRequest{Harness: harness, Name: name, Confirmed: true})
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

func (f fixture) profilePath(name, id, child string) string {
	f.t.Helper()
	state := f.state()
	for _, profile := range state.Profiles {
		if profile.Name != name {
			continue
		}
		for _, root := range profile.Roots {
			if root.ID == id {
				return filepath.Join(sourcePath(state, profile, root), child)
			}
		}
	}
	f.t.Fatal("profile root does not exist", name, id)
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

func inode(t testing.TB, path string) FileID {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fileID(info)
}

func TestSaveLoadPreservesCompleteRootsAndInodes(t *testing.T) {
	f := newFixture(t)
	f.write(".claude/account", "personal")
	f.write(".claude/sessions/first", "personal session")
	f.write(".claude.json", "personal settings")
	directory, history, settings := inode(t, filepath.Join(f.home, ".claude")), inode(t, filepath.Join(f.home, ".claude/sessions/first")), inode(t, filepath.Join(f.home, ".claude.json"))
	f.service.copy = func(context.Context, string, string) error { t.Fatal("save or switch copied state"); return nil }
	first := f.save("claude")
	if first.Saved != "Default" || !first.Created {
		t.Fatalf("first save: %+v", first)
	}
	if inode(t, filepath.Join(f.home, ".claude")) != directory {
		t.Fatal("registration replaced the root")
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
	if inode(t, f.profilePath("Default", "claude-state", "")) != directory {
		t.Fatal("outgoing directory was copied")
	}
	f.write(".claude/account", "enterprise")
	f.write(".claude/sessions/work", "work session")
	if f.save("claude").Saved != "Work" {
		t.Fatal("pending account was not bound")
	}
	f.load("claude", "default")
	if f.read(".claude/account") != "personal" || f.read(".claude/sessions/later") != "later personal session" || f.read(".claude.json") != "personal settings" {
		t.Fatal("personal state changed")
	}
	if inode(t, filepath.Join(f.home, ".claude/sessions/first")) != history || inode(t, filepath.Join(f.home, ".claude.json")) != settings {
		t.Fatal("switch replaced an inode")
	}
	f.load("claude", "WORK")
	if f.read(".claude/sessions/work") != "work session" {
		t.Fatal("work state changed")
	}
	if err := f.service.Delete(t.Context(), "claude", "Default"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Delete(t.Context(), "claude", "Work"); err == nil {
		t.Fatal("deleted selected profile")
	}
}

func TestConfirmationDoesNotHoldLockOrOverrideUse(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	before := readFile(t, filepath.Join(f.service.storePath(), "index.json"))
	_, err := f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work"})
	issue := requireIssue(t, err, ConfirmationRequired)
	if readFile(t, filepath.Join(f.service.storePath(), "index.json")) != before {
		t.Fatal("prompt changed the index")
	}
	f.load("codex", "Other") // Another command can proceed while a prompt is open.
	_, err = f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work", Confirmed: true, ExpectedProfileID: issue.ProfileID})
	requireIssue(t, err, ConfirmationRequired)
	f.service.options.Guard = GuardFunc(func(context.Context, []string) error { return &Issue{Kind: StateInUse, Message: "fixture busy"} })
	_, err = f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work", Confirmed: true})
	requireIssue(t, err, StateInUse)
}

func TestAccountChangesCannotOverwriteAnotherProfile(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.load("codex", "Work")
	f.write(".codex/account", "personal")
	_, err := f.service.Save(t.Context(), SaveRequest{Harness: "codex"})
	requireIssue(t, err, AccountConflict)
	f.write(".codex/account", "work")
	f.save("codex")
	f.write(".codex/account", "unexpected")
	_, err = f.service.Save(t.Context(), SaveRequest{Harness: "codex"})
	requireIssue(t, err, AccountConflict)
	_, err = f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Default", Confirmed: true})
	requireIssue(t, err, AccountConflict)
	if f.read(".codex/account") != "unexpected" {
		t.Fatal("mismatched state was changed")
	}
}

func TestGlobalAgentsDoNotSwitch(t *testing.T) {
	f := newFixture(t)
	f.write(".agents/skills/shared/SKILL.md", "global skill")
	global := inode(t, filepath.Join(f.home, ".agents"))
	for _, harness := range []string{"codex", "grok"} {
		f.write("."+harness+"/account", "personal")
		f.save(harness)
		f.load(harness, "Work")
		if inode(t, filepath.Join(f.home, ".agents")) != global {
			t.Fatal("global skills moved")
		}
		if f.read(".agents/skills/shared/SKILL.md") != "global skill" {
			t.Fatal("global skills changed")
		}
		selection, err := f.service.Select(t.Context(), harness, "Default")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, mount := range selection.Paths.Mounts {
			if mount.ID == "shared-agents" {
				found = true
				if mount.Source != filepath.Join(f.home, ".agents") || mount.Ownership != workload.HostState {
					t.Fatal("global mount is not host state")
				}
			}
		}
		if !found {
			t.Fatal("global mount missing")
		}
	}
}

func TestRootAndStoreGuards(t *testing.T) {
	for _, kind := range []string{"root-link", "workspace", "binary", "old-copy", "old-symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			harness := "codex"
			f.write(".codex/account", "personal")
			switch kind {
			case "root-link":
				path := filepath.Join(f.home, ".codex")
				if err := os.Rename(path, path+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+"-original", path); err != nil {
					t.Fatal(err)
				}
			case "workspace":
				f.service.options.Workspace = filepath.Join(f.home, ".codex")
			case "binary":
				harness = "opencode"
				f.write(".opencode/bin/opencode", "binary")
				if err := os.Chmod(filepath.Join(f.home, ".opencode/bin/opencode"), 0700); err != nil {
					t.Fatal(err)
				}
			case "old-copy":
				f.write(".cooper/profiles/index.json", `{"schema":1,"profiles":[],"hosts":{}}`)
			case "old-symlink":
				f.write(".cooper/profiles/managed.json", `{"schema":2}`)
			}
			if _, err := f.service.Save(t.Context(), SaveRequest{Harness: harness}); err == nil {
				t.Fatal("unsafe or old state was accepted")
			}
		})
	}
}

func TestProfileNamesRejectPathsAndControls(t *testing.T) {
	for _, name := range []string{"", "../Work", ".hidden", "Work/More", "Work\n", "bad name"} {
		if ValidateName(name) == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, name := range []string{"Default", "work-2", "Work_3"} {
		if err := ValidateName(name); err != nil {
			t.Fatal(err)
		}
	}
}
