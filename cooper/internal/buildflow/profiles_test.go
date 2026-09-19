package buildflow

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/profileauth"
	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/usercontext"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func profileBuildFixture(t *testing.T, saved bool) *Prepared {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("live profiles require Linux")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("COOPER_VM_CONTEXT", filepath.Join(t.TempDir(), "absent-context"))
	t.Setenv("COOPER_CLI_TOOL", "")
	for _, name := range []string{"CODEX_HOME", "OPENAI_API_KEY", "CODEX_API_KEY", "OPENAI_BASE_URL"} {
		t.Setenv(name, "")
	}
	bin := t.TempDir()
	// The fixture has no runtime users. Unit checks must not inspect or stop
	// the real VM that runs this test suite.
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\n[ \"$1\" = ps ] || exit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	cooperDir := filepath.Join(home, ".cooper")
	if err := os.MkdirAll(filepath.Join(home, ".codex", "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte(`{"OPENAI_API_KEY":"build-profile-fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "sessions", "first"), []byte("saved conversation"), 0o600); err != nil {
		t.Fatal(err)
	}
	if saved {
		account, err := usercontext.Current()
		if err != nil {
			t.Fatal(err)
		}
		workspace, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		environment := workload.HostPathEnvironment()
		for _, name := range profileauth.CredentialNames("codex") {
			t.Setenv(name, "")
			environment[name] = ""
		}
		service := profiles.New(profiles.Options{CooperDir: cooperDir, Workspace: workspace, Account: account,
			Environment: environment, CredentialNames: profileauth.CredentialNames, Reader: profileauth.Reader{},
			Guard: profiles.GuardFunc(func(context.Context, []string) error { return nil })})
		if _, err := service.Save(t.Context(), profiles.SaveRequest{Harness: "codex"}); err != nil {
			t.Fatal(err)
		}
	}
	return &Prepared{cfg: config.DefaultConfig(), cooperDir: cooperDir, configPath: filepath.Join(cooperDir, "config.json"),
		baseDir: filepath.Join(cooperDir, "base"), proxyDir: filepath.Join(cooperDir, "proxy"), cliDir: filepath.Join(cooperDir, "cli")}
}

func successfulProfileBuild(name, dockerfilePath, contextDir string, args map[string]string, noCache bool) (<-chan string, <-chan error) {
	lines, result := make(chan string), make(chan error, 1)
	close(lines)
	result <- nil
	close(result)
	return lines, result
}

func TestBuildAlwaysSetsUpProfilesBeforeImages(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[saved], func(t *testing.T) {
			prepared := profileBuildFixture(t, saved)
			var output bytes.Buffer
			calls := 0
			builder := func(name, file, dir string, args map[string]string, clean bool) (<-chan string, <-chan error) {
				store, err := os.OpenRoot(filepath.Join(prepared.cooperDir, "profiles"))
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				if managed, err := profilelink.Managed(store); err != nil || !managed {
					t.Fatalf("image build started before live storage: %v %v", managed, err)
				}
				calls++
				return successfulProfileBuild(name, file, dir, args, clean)
			}
			for range 2 {
				if err := prepared.Build(Options{Out: &output, imageBuild: builder}); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 4 || strings.Count(output.String(), profileStepName) != 2 {
				t.Fatalf("build skipped profile setup: %d calls, %s", calls, output.String())
			}
			if saved {
				if info, err := os.Lstat(filepath.Join(os.Getenv("HOME"), ".codex")); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("existing profile was not converted: %v %v", info, err)
				}
				if !strings.Contains(output.String(), "Original state is retained") {
					t.Fatal("build omitted recovery output")
				}
			}
		})
	}
}

func TestProfileFailureStopsBuildBeforeImages(t *testing.T) {
	prepared := profileBuildFixture(t, true)
	if err := os.WriteFile(filepath.Join(prepared.cooperDir, "profiles", "index.json"), []byte("invalid metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	failed := -1
	err := prepared.Build(Options{imageBuild: func(string, string, string, map[string]string, bool) (<-chan string, <-chan error) {
		t.Fatal("an image build started after profile setup failed")
		return nil, nil
	}, OnProgress: func(step, total int, name string, err error) {
		if err != nil && name == profileStepName {
			failed = step
		}
	}})
	if err == nil || failed != 0 {
		t.Fatalf("profile failure was not reported as the first step: %v, step %d", err, failed)
	}
	if data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".codex", "sessions", "first")); err != nil || string(data) != "saved conversation" {
		t.Fatal("failed setup changed host state")
	}
}

func TestInnerBuildCannotConvertExistingProfiles(t *testing.T) {
	prepared := profileBuildFixture(t, true)
	t.Setenv("COOPER_CLI_TOOL", "codex")
	err := prepared.prepareProfiles(Options{}, os.Getenv("HOME"))
	if err == nil || !strings.Contains(err.Error(), "physical host") {
		t.Fatalf("inner conversion: %v", err)
	}
	if info, err := os.Lstat(filepath.Join(os.Getenv("HOME"), ".codex")); err != nil || !info.IsDir() {
		t.Fatal("inner build changed the host root")
	}
}

func TestInnerBuildCanPrepareAnEmptyStore(t *testing.T) {
	prepared := profileBuildFixture(t, false)
	t.Setenv("COOPER_CLI_TOOL", "codex")
	if err := prepared.prepareProfiles(Options{}, os.Getenv("HOME")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(os.Getenv("HOME"), ".codex")); err != nil || !info.IsDir() {
		t.Fatal("empty inner build changed a host root")
	}
}
