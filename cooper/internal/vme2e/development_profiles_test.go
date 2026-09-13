package vme2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/launch"
	"github.com/rickchristie/govner/cooper/internal/profileauth"
	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/usercontext"
	"github.com/rickchristie/govner/cooper/internal/vm"
)

// One selected agent tests the shared profile mount contract. The local tests
// cover every catalog and identity adapter. A restart is the only second VM
// import: it must retain the selected profile from persisted runtime metadata.
func (f *developmentFixture) profiles() {
	account, err := usercontext.Current()
	if err != nil {
		f.t.Fatal(err)
	}
	account.Home = f.home
	service := profiles.New(profiles.Options{CooperDir: f.run.CooperDir, Workspace: f.run.Workspace, Account: account,
		Environment: map[string]string{}, CredentialNames: profileauth.CredentialNames, Reader: profileauth.Reader{},
		// The enclosing development VM mounts the repository and /tmp. Only
		// these fabricated fixture roots bypass that broad parent mount check.
		Guard: profiles.GuardFunc(func(context.Context, []string) error { return nil })})
	seed := func(name string) {
		for _, relative := range []string{".codex", ".agents", ".claude-plugin", ".cursor-plugin"} {
			writeFile(f.t, filepath.Join(f.home, relative, "profile-sentinel"), name)
		}
		writeFile(f.t, filepath.Join(f.home, ".codex", "auth.json"), fmt.Sprintf(`{"auth_mode":"apikey","OPENAI_API_KEY":"fake-%s-key"}`, name))
		writeFile(f.t, filepath.Join(f.home, ".codex", "session"), name)
	}
	seed("personal")
	if _, err := service.Save(f.ctx, profiles.SaveRequest{Harness: "codex"}); err != nil {
		f.t.Fatal(err)
	}
	if _, err := service.Load(f.ctx, profiles.LoadRequest{Harness: "codex", Name: "Work"}); err != nil {
		f.t.Fatal(err)
	}
	seed("work")
	if _, err := service.Save(f.ctx, profiles.SaveRequest{Harness: "codex"}); err != nil {
		f.t.Fatal(err)
	}
	if _, err := service.Load(f.ctx, profiles.LoadRequest{Harness: "codex", Name: "Default"}); err != nil {
		f.t.Fatal(err)
	}
	selection, err := service.Select(f.ctx, "codex", "Work")
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Setenv("OPENAI_API_KEY", "fake-host-key-must-not-replace-profile")
	name := docker.BarrelContainerNameForProfile(f.run.Workspace, f.tool, selection.ID)
	if err := docker.StartBarrelWithProfile(f.manifest.Config, f.run.Workspace, f.run.CooperDir, f.home, f.tool, selection.ID); err != nil {
		f.t.Fatal(err)
	}
	f.run.Objects = append(f.run.Objects, developmentObject{Type: "container", Name: name, ID: strings.TrimSpace(f.command("docker", "inspect", "--format", "{{.Id}}", name))})
	f.saveRun()
	checks := `set -eu
for root in .codex .agents .claude-plugin .cursor-plugin; do test "$(cat "$HOME/$root/profile-sentinel")" = work; done
test "${OPENAI_API_KEY-unset}" = unset
`
	command, env, closeSession := f.profileSession(name, selection, checks+`test "$(cat "$HOME/.codex/session")" = work; printf docker > "$HOME/.codex/session"`)
	args := []string{"exec"}
	for _, value := range env {
		args = append(args, "-e", value)
	}
	args = append(args, name)
	args = append(args, command...)
	output, err := exec.CommandContext(f.ctx, "docker", args...).CombinedOutput()
	closeSession()
	if err != nil {
		f.t.Fatalf("profile barrel: %v %s", err, output)
	}
	if err := docker.StopBarrel(name); err != nil {
		f.t.Fatal(err)
	}
	f.startVMWithProfile(selection.ID)
	f.execProfile(selection, checks+`test "$(cat "$HOME/.codex/session")" = docker; printf vm > "$HOME/.codex/session"`)
	original := f.state.ID
	f.state, err = f.manager.Restart(f.ctx, f.state.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	f.observeLifetime()
	if f.state.ID != original {
		f.t.Fatal("restart changed selected runtime identity")
	}
	f.execProfile(selection, checks+`test "$(cat "$HOME/.codex/session")" = vm`)
	infos, err := vm.ListInfo(f.ctx, f.run.Namespace, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	found := false
	for _, info := range infos {
		if info.ID == f.state.ID && info.ProfileID == selection.ID && info.ProfileName == "Work" {
			found = true
		}
	}
	if !found {
		f.t.Fatal("VM status omitted selected profile")
	}
	f.stopVM()
	if err := vm.RemoveCache(f.run.CooperDir); err != nil {
		f.t.Fatal(err)
	}
	if got := readFile(f.t, filepath.Join(f.home, ".codex/session")); got != "personal" {
		f.t.Fatalf("profile runtime changed host account: %q", got)
	}
	selected, err := service.Select(f.ctx, "codex", "Work")
	if err != nil {
		f.t.Fatal(err)
	}
	for _, root := range selected.Paths.Mounts {
		if root.ID == "codex-state" && readFile(f.t, filepath.Join(root.Source, "session")) != "vm" {
			f.t.Fatal("VM cleanup removed saved sessions")
		}
	}
	if f.starts != 2 || f.loads != 2 {
		f.t.Fatalf("profile check used %d starts and %d imports; want two", f.starts, f.loads)
	}
	f.report["checks"] = []string{"profile-source-only-mapping", "complete-selected-roots", "host-state-isolation", "credential-isolation", "Docker-to-VM-session", "profile-restart", "profile-status-labels", "cache-cleanup-preserves-profiles"}
}

func (f *developmentFixture) profileSession(id string, selection profiles.Selection, script string) ([]string, []string, func()) {
	session, _, err := launch.PrepareSession(launch.SessionRequest{Config: f.manifest.Config, CooperDir: f.run.CooperDir, RuntimeID: id, ToolName: f.tool, WorkspaceDir: f.run.Workspace, State: &selection, OneShot: script})
	if err != nil {
		f.t.Fatal(err)
	}
	return session.Command, session.Environment, func() {
		if err := session.Close(); err != nil {
			f.t.Error(err)
		}
	}
}

func (f *developmentFixture) execProfile(selection profiles.Selection, script string) {
	command, env, closeSession := f.profileSession(f.state.ID, selection, script)
	defer closeSession()
	var output bytes.Buffer
	if err := f.manager.ExecCommand(f.ctx, f.state, command, env, false, nil, &output, io.Discard); err != nil {
		f.t.Fatalf("profile VM command: %v %s", err, output.String())
	}
}
