package vme2e

import (
	"bytes"
	"context"
	"encoding/base64"
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
	roots, stateRoot, credential := []string{".codex", ".agents", ".claude-plugin", ".cursor-plugin"}, ".codex", "OPENAI_API_KEY"
	if f.tool == "antigravity" {
		roots, stateRoot, credential = []string{".gemini"}, ".gemini", "GEMINI_API_KEY"
	}
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
		for _, relative := range roots {
			writeFile(f.t, filepath.Join(f.home, relative, "profile-sentinel"), name)
		}
		if f.tool == "antigravity" {
			// Fabricated Google subjects test the local identity adapter without
			// sending a token or prompt to an external service.
			claims := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"iss":"https://accounts.google.com","sub":%q,"aud":"cooper-fixture"}`, name)))
			token := fmt.Sprintf(`{"auth_method":"consumer","id_token":"fake.%s.fake","token":{"access_token":"fake-access","refresh_token":"fake-refresh","expiry":"2099-01-01T00:00:00Z"}}`, claims)
			writeFile(f.t, filepath.Join(f.home, stateRoot, "antigravity-cli/antigravity-oauth-token"), token)
		} else {
			writeFile(f.t, filepath.Join(f.home, stateRoot, "auth.json"), fmt.Sprintf(`{"auth_mode":"apikey","OPENAI_API_KEY":"fake-%s-key"}`, name))
		}
		writeFile(f.t, filepath.Join(f.home, stateRoot, "session"), name)
	}
	seed("personal")
	if _, err := service.Save(f.ctx, profiles.SaveRequest{Harness: f.tool}); err != nil {
		f.t.Fatal(err)
	}
	if _, err := service.Load(f.ctx, profiles.LoadRequest{Harness: f.tool, Name: "Work"}); err != nil {
		f.t.Fatal(err)
	}
	seed("work")
	if _, err := service.Save(f.ctx, profiles.SaveRequest{Harness: f.tool}); err != nil {
		f.t.Fatal(err)
	}
	if _, err := service.Load(f.ctx, profiles.LoadRequest{Harness: f.tool, Name: "Default"}); err != nil {
		f.t.Fatal(err)
	}
	selection, err := service.Select(f.ctx, f.tool, "Work")
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Setenv(credential, "fake-host-key-must-not-replace-profile")
	name := docker.BarrelContainerNameForProfile(f.run.Workspace, f.tool, selection.ID)
	if err := docker.StartBarrelWithProfile(f.manifest.Config, f.run.Workspace, f.run.CooperDir, f.home, f.tool, selection.ID); err != nil {
		f.t.Fatal(err)
	}
	f.run.Objects = append(f.run.Objects, developmentObject{Type: "container", Name: name, ID: strings.TrimSpace(f.command("docker", "inspect", "--format", "{{.Id}}", name))})
	f.saveRun()
	checks := fmt.Sprintf(`set -eu
for root in %s; do test "$(cat "$HOME/$root/profile-sentinel")" = work; done
test "${%s-unset}" = unset
session="$HOME/%s/session"
`, strings.Join(roots, " "), credential, stateRoot)
	dockerWrite := checks + `test "$(cat "$session")" = work; printf docker > "$session"`
	if f.tool == "antigravity" {
		dockerWrite += antigravityRefreshScript("fake-access", "docker-refreshed-access")
	}
	command, env, closeSession := f.profileSession(name, selection, dockerWrite)
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
	vmWrite := checks + `test "$(cat "$session")" = docker; printf vm > "$session"`
	if f.tool == "antigravity" {
		vmWrite += antigravityRefreshScript("docker-refreshed-access", "vm-refreshed-access")
	}
	f.execProfile(selection, vmWrite)
	original := f.state.ID
	f.state, err = f.manager.Restart(f.ctx, f.state.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	f.observeLifetime()
	if f.state.ID != original {
		f.t.Fatal("restart changed selected runtime identity")
	}
	restartCheck := checks + `test "$(cat "$session")" = vm`
	if f.tool == "antigravity" {
		restartCheck += antigravityRefreshScript("vm-refreshed-access", "vm-refreshed-access")
	}
	f.execProfile(selection, restartCheck)
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
	if got := readFile(f.t, filepath.Join(f.home, stateRoot, "session")); got != "personal" {
		f.t.Fatalf("profile runtime changed host account: %q", got)
	}
	selected, err := service.Select(f.ctx, f.tool, "Work")
	if err != nil {
		f.t.Fatal(err)
	}
	for _, root := range selected.Paths.Mounts {
		if root.ID == f.tool+"-state" && readFile(f.t, filepath.Join(root.Source, "session")) != "vm" {
			f.t.Fatal("VM cleanup removed saved sessions")
		}
	}
	if f.tool == "antigravity" {
		if _, err := service.Load(f.ctx, profiles.LoadRequest{Harness: f.tool, Name: "Work"}); err != nil {
			f.t.Fatal(err)
		}
		stored := readFile(f.t, filepath.Join(f.home, ".gemini", "antigravity-cli", "antigravity-oauth-token"))
		if !strings.Contains(stored, "vm-refreshed-access") {
			f.t.Fatal("host load lost the profile token written in the VM")
		}
	}
	if f.starts != 2 || f.loads != 2 {
		f.t.Fatalf("profile check used %d starts and %d imports; want two", f.starts, f.loads)
	}
	f.report["checks"] = []string{"profile-source-only-mapping", "complete-selected-roots", "host-state-isolation", "credential-isolation", "Docker-to-VM-session", "profile-restart", "profile-status-labels", "cache-cleanup-preserves-profiles"}
	if f.tool == "antigravity" {
		f.report["auth_checks"] = []string{"atomic-token-replacement", "Docker-to-VM-refreshed-token", "refreshed-token-after-restart", "load-refreshed-profile-on-host"}
	}
}

// A fabricated refresh replaces the whole file, as native clients can do.
// Directory mounts must carry the new inode across Docker, VM, and host load.
// This does not claim a successful OAuth request to a real provider.
func antigravityRefreshScript(before, after string) string {
	return fmt.Sprintf(`
node - <<'JS'
const fs = require('fs');
const path = process.env.HOME + '/.gemini/antigravity-cli/antigravity-oauth-token';
const token = JSON.parse(fs.readFileSync(path, 'utf8'));
if (token.token.access_token !== %q) throw new Error('profile refresh was not shared');
token.token.access_token = %q;
token.token.refresh_token = %q;
fs.writeFileSync(path + '.next', JSON.stringify(token), {mode: 0o600});
fs.renameSync(path + '.next', path);
JS
`, before, after, after+"-refresh")
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
