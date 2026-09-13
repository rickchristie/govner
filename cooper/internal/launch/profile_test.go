package launch

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func TestNamedSessionUsesCapturedCredentialsAndPreservesUnset(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "host-key-must-not-leak")
	t.Setenv("ANTHROPIC_API_KEY", "host-claude-must-not-leak")
	cfg := config.DefaultConfig()
	cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "ANTHROPIC_API_KEY", Value: "config-key-must-not-leak"}, {Name: "PROFILE_TEST", Value: "custom config kept"}}
	session, _, err := PrepareSession(SessionRequest{Config: cfg, CooperDir: t.TempDir(), RuntimeID: "profile-test", ToolName: "codex", WorkspaceDir: t.TempDir(),
		OneShot: `printf '%s|%s|%s' "$OPENAI_API_KEY" "${ANTHROPIC_API_KEY-unset}" "$PROFILE_TEST"`,
		State:   &profiles.Selection{ID: "fixture", Name: "Work", Credentials: []workload.EnvVar{{Name: "OPENAI_API_KEY", Value: "profile-key", Secret: true}, {Name: "ANTHROPIC_API_KEY", Secret: true, Unset: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	argv := append([]string(nil), session.Command...)
	// Use the real generated wrapper, with its runtime file path mapped to
	// the host fixture. This needs no container or credential provider.
	for index, arg := range argv {
		if index > 0 && argv[index-1] == "cooper-env-wrapper" {
			argv[index] = session.envPath
		}
		if strings.Contains(arg, "host-key-must-not-leak") {
			t.Fatal("host credential in command")
		}
	}
	command := exec.Command(argv[0], argv[1:]...)
	command.Env = append(os.Environ(), session.Environment...)
	output, err := command.CombinedOutput()
	if err != nil || string(output) != "profile-key|unset|custom config kept" {
		t.Fatalf("profile wrapper: %q %v", output, err)
	}
	if !strings.Contains(session.Title, "Work") {
		t.Fatal("session title omitted account profile")
	}
}
