package profiles

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSwitchCrashChild(t *testing.T) {
	base := os.Getenv("COOPER_RENAME_CRASH_BASE")
	if base == "" {
		t.Skip("subprocess fixture")
	}
	f := fixtureAt(t, base)
	f.service.checkpoint = func(point string) error {
		if point == os.Getenv("COOPER_RENAME_CRASH_POINT") {
			os.Exit(73)
		}
		return nil
	}
	if os.Getenv("COOPER_RENAME_CRASH_OPERATION") == "restore" {
		if _, err := f.service.Restore(t.Context(), RestoreRequest{Harness: "claude", Name: "Default", Backup: filepath.Join(base, "backup"), Confirmed: true}); err != nil {
			t.Fatal(err)
		}
	} else {
		f.load("claude", "Work")
	}
	t.Fatal("crash checkpoint was not reached")
}

func TestRestoreRecoversEveryDurableBoundary(t *testing.T) {
	for _, point := range []string{"journal", "move-0", "sync-0", "move-1", "sync-1", "move-2", "sync-2", "move-3", "sync-3", "index"} {
		t.Run(point, func(t *testing.T) {
			f := newFixture(t)
			f.write(".claude/account", "personal")
			f.write(".claude/session", "backup")
			f.write(".claude.json", "backup")
			f.save("claude")
			if err := f.service.Backup(t.Context(), filepath.Join(filepath.Dir(f.home), "backup")); err != nil {
				t.Fatal(err)
			}
			f.write(".claude/session", "current")
			f.write(".claude.json", "current")
			cmd := exec.Command(os.Args[0], "-test.run=^TestSwitchCrashChild$")
			cmd.Env = append(os.Environ(), "COOPER_RENAME_CRASH_BASE="+filepath.Dir(f.home), "COOPER_RENAME_CRASH_POINT="+point, "COOPER_RENAME_CRASH_OPERATION=restore")
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("crash child: %v %s", err, output)
			}
			if err := f.service.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			want := "current"
			if point == "index" {
				want = "backup"
			}
			if f.read(".claude/session") != want || f.read(".claude.json") != want {
				t.Fatal("restore recovery chose mixed state")
			}
			if err := f.service.PruneRecovery(t.Context()); err != nil {
				t.Fatal(err)
			}
			if f.read(".claude/session") != want {
				t.Fatal("cleanup removed selected state")
			}
		})
	}
}

func TestIndexWriteFailureUsesDiskCommit(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-publication", true: "after-publication"}[after], func(t *testing.T) {
			f := newFixture(t)
			f.write(".claude/account", "personal")
			f.save("claude")
			f.service.publish = func(root *os.Root, state index) error {
				if state.LastTransaction == "" {
					return writeJSON(root, "index.json", state)
				}
				if after {
					if err := writeJSON(root, "index.json", state); err != nil {
						return err
					}
				}
				return errors.New("fixture index failure")
			}
			if _, err := f.service.Load(t.Context(), LoadRequest{Harness: "claude", Name: "Work", Confirmed: true}); err == nil {
				t.Fatal("index failure ignored")
			}
			state := f.state()
			want := "Default"
			if after {
				want = "Work"
			}
			if state.byID(state.Hosts["claude"].ProfileID).Name != want {
				t.Fatal("recovery ignored the disk commit")
			}
			if readFile(t, f.profilePath("Default", "claude-state", "account")) != "personal" {
				t.Fatal("index failure lost state")
			}
		})
	}
}

func TestSwitchRecoversEveryDurableBoundary(t *testing.T) {
	for _, point := range []string{"journal", "move-0", "sync-0", "move-1", "sync-1", "move-2", "sync-2", "index"} {
		t.Run(point, func(t *testing.T) {
			f := newFixture(t)
			f.write(".claude/account", "personal")
			f.write(".claude/session", "keep")
			f.write(".claude.json", "settings")
			f.save("claude")
			dir := inode(t, filepath.Join(f.home, ".claude"))
			cmd := exec.Command(os.Args[0], "-test.run=^TestSwitchCrashChild$")
			cmd.Env = append(os.Environ(), "COOPER_RENAME_CRASH_BASE="+filepath.Dir(f.home), "COOPER_RENAME_CRASH_POINT="+point)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("crash child: %v %s", err, output)
			}
			if _, err := f.service.SelectID(t.Context(), "claude", ""); err == nil {
				t.Fatal("ordinary startup crossed unfinished journal")
			}
			if err := f.service.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := f.service.Recover(t.Context()); err != nil {
				t.Fatal("recovery was not repeatable", err)
			}
			path := f.profilePath("Default", "claude-state", "")
			if inode(t, path) != dir || readFile(t, filepath.Join(path, "session")) != "keep" {
				t.Fatal("recovery changed outgoing data")
			}
			state := f.state()
			selected := state.byID(state.Hosts["claude"].ProfileID).Name
			want := "Default"
			if point == "index" {
				want = "Work"
			}
			if selected != want {
				t.Fatalf("selected %s, want %s", selected, want)
			}
			f.load("claude", "Default")
			if f.read(".claude.json") != "settings" {
				t.Fatal("standalone file was lost")
			}
		})
	}
}

func TestRenameFailureRollsBackWithoutCopy(t *testing.T) {
	for fail := range 3 {
		t.Run(string(rune('0'+fail)), func(t *testing.T) {
			f := newFixture(t)
			f.write(".claude/account", "personal")
			f.write(".claude.json", "settings")
			f.save("claude")
			calls := 0
			f.service.rename = func(parent *os.Root, from, to string) error {
				calls++
				if calls == fail+1 {
					return errors.New("fixture rename failure")
				}
				return renameEntry(parent, from, to)
			}
			_, err := f.service.Load(t.Context(), LoadRequest{Harness: "claude", Name: "Work", Confirmed: true})
			if err == nil {
				t.Fatal("rename failure ignored")
			}
			if f.read(".claude/account") != "personal" || f.read(".claude.json") != "settings" {
				t.Fatal("failed switch changed active state")
			}
			if err := CheckReady(f.service.options.CooperDir); err != nil {
				t.Fatal("completed rollback left a journal", err)
			}
		})
	}
}

func TestRecoveryRejectsForeignEntryAndChangedParent(t *testing.T) {
	for _, changed := range []string{"entry", "parent", "journal"} {
		t.Run(changed, func(t *testing.T) {
			f := newFixture(t)
			f.write(".claude/account", "personal")
			f.save("claude")
			f.service.checkpoint = func(point string) error {
				if point == "move-0" {
					return errors.New("interrupted")
				}
				return nil
			}
			_, err := f.service.Load(t.Context(), LoadRequest{Harness: "claude", Name: "Work", Confirmed: true})
			if err == nil {
				t.Fatal("checkpoint did not interrupt")
			}
			switch changed {
			case "entry":
				f.write(".claude/account", "unrelated new writer")
			case "parent":
				if err := os.Rename(f.home, f.home+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(f.home, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(f.home+"-old", ".cooper"), filepath.Join(f.home, ".cooper")); err != nil {
					t.Fatal(err)
				}
			case "journal":
				path := filepath.Join(f.service.storePath(), transactionFile)
				data := readFile(t, path)
				data = strings.Replace(data, `"Name": ".claude"`, `"Name": "../unrelated"`, 1)
				// Entry fields use explicit lowercase JSON names.
				data = strings.Replace(data, `"name": ".claude"`, `"name": "../unrelated"`, 1)
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.service.Recover(t.Context()); err == nil {
				t.Fatal("recovery accepted changed authority")
			}
			if _, err := os.Stat(filepath.Join(f.service.storePath(), transactionFile)); err != nil {
				t.Fatal("failed recovery removed its record")
			}
		})
	}
}
