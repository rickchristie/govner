package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

func TestInterruptedLoadRecoversMissingSymlinkTarget(t *testing.T) {
	for _, linkType := range []string{"absolute", "relative", "changed"} {
		t.Run(linkType, func(t *testing.T) {
			f := newFixture(t)
			f.write("state/account", "personal")
			target := filepath.Join(f.home, "state")
			if linkType == "relative" {
				target = "state"
			}
			link := filepath.Join(f.home, ".codex")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			f.save("codex")
			f.service.rename = func(root *os.Root, from, to string) error {
				if err := root.Rename(from, to); err != nil {
					return err
				}
				panic("simulated process exit after moving the link target")
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Error("process exit was not injected")
					}
				}()
				_, _ = f.service.Load(context.Background(), LoadRequest{Harness: "codex", Name: "Work"})
			}()
			if _, err := os.Stat(link); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected a missing link target after interruption: %v", err)
			}
			f.service = New(f.service.options)
			if linkType == "changed" {
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("other-state", link); err != nil {
					t.Fatal(err)
				}
				if _, err := f.service.Save(context.Background(), SaveRequest{Harness: "codex"}); err == nil {
					t.Fatal("recovery accepted a changed symlink")
				}
				if _, err := os.Stat(filepath.Join(f.service.storePath(), transactionFile)); err != nil {
					t.Fatalf("rejected recovery lost its journal: %v", err)
				}
				return
			}
			f.save("codex")
			if f.read(".codex/account") != "personal" {
				t.Fatal("recovery lost account state")
			}
			if got, err := os.Readlink(link); err != nil || got != target {
				t.Fatalf("recovery changed the symlink: %q, %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(f.service.storePath(), transactionFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("journal remained after recovery: %v", err)
			}
		})
	}
}

func TestOpenCodeLoadProtectsHostExecutable(t *testing.T) {
	for _, install := range []string{"state", "external"} {
		t.Run(install, func(t *testing.T) {
			f := newFixture(t)
			f.service.options.Reader = ReaderFunc(func(context.Context, string, []workload.MountSpec, map[string]string) (Identity, error) {
				return Identity{Key: "personal", Label: "personal"}, nil
			})
			f.write(".opencode/session", "personal session")
			binary := ".opencode/bin/opencode"
			if install == "external" {
				binary = ".local/bin/opencode"
			}
			f.write(binary, "#!/bin/sh\nexit 0\n")
			if err := os.Chmod(filepath.Join(f.home, binary), 0o700); err != nil {
				t.Fatal(err)
			}
			f.save("opencode")
			f.write(".opencode/session", "new personal session")
			_, err := f.service.Load(context.Background(), LoadRequest{Harness: "opencode", Name: "Work"})
			if install == "external" {
				if err != nil {
					t.Fatal(err)
				}
				if entries, err := os.ReadDir(filepath.Join(f.home, ".opencode")); err != nil || len(entries) != 0 {
					t.Fatalf("fresh profile retained old state: %v %v", entries, err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "executable") {
					t.Fatalf("expected installation error before replacement: %v", err)
				}
				state := f.state()
				if f.read(".opencode/session") != "new personal session" || state.byName("opencode", "Work") != nil {
					t.Fatal("rejected load changed the host or created Work")
				}
				data, err := os.ReadFile(f.profilePath("Default", "opencode-compat", "session"))
				if err != nil || string(data) != "personal session" {
					t.Fatalf("rejected load changed the saved profile: %q %v", data, err)
				}
			}
			if _, err := os.Stat(filepath.Join(f.home, binary)); err != nil {
				t.Fatalf("host executable was removed: %v", err)
			}
		})
	}
}
