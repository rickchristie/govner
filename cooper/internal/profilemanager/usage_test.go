package profilemanager

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func TestDockerUseChecksActualMountsAcrossNamespaces(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\ncase $1 in\nps) printf 'unrelated-namespace-container\\n';;\ninspect) printf '%s\\n' \"$PROFILE_FIXTURE_MOUNTS\";;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	root := filepath.Join(t.TempDir(), "state")
	for _, test := range []struct {
		name, source string
		busy         bool
	}{
		{"same", root, true}, {"parent", filepath.Dir(root), true}, {"child", filepath.Join(root, "sessions"), true},
		{"prefix only", root + "-other", false}, {"no host source", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal([]map[string]string{{"Source": test.source}})
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("PROFILE_FIXTURE_MOUNTS", string(data))
			err = dockerUsage(context.Background(), []string{root})
			var issue *profiles.Issue
			busy := errors.As(err, &issue) && issue.Kind == profiles.StateInUse
			if busy != test.busy || (!busy && err != nil) {
				t.Fatalf("guard: %v", err)
			}
		})
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 23\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := dockerUsage(t.Context(), []string{root}); err == nil {
		t.Fatal("unknown daemon state was treated as safe")
	}
}

func TestHarnessNamesRecognizeNativeAndNodeExecutables(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"/opt/codex/bin/codex"}, "codex"},
		{[]string{"node", "/opt/npm/node_modules/@anthropic-ai/claude-code/cli.js"}, "claude"},
		{[]string{"node", "/opt/npm/node_modules/@github/copilot/index.js"}, "copilot"},
		{[]string{"cooper", "cli", "codex"}, "codex"},
		{[]string{"/opt/cooper/bin/agy"}, "antigravity"},
		{[]string{"/opt/antigravity/antigravity"}, "antigravity"},
		{[]string{"node", "/opt/npm/node_modules/@google/gemini-cli/index.js"}, "antigravity"},
		{[]string{"/bin/sleep", "30"}, ""},
	} {
		if got := harnessName(test.args); got != test.want {
			t.Fatalf("harness %v = %s", test.args, got)
		}
	}
}

func TestDefaultSessionBusCannotBeMistakenForFileOnlyAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bus")
	environment := map[string]string{}
	noteSessionBus(environment, path)
	if environment["DBUS_SESSION_BUS_ADDRESS"] != "" {
		t.Fatal("absent bus changed the auth observation")
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	noteSessionBus(environment, path)
	if environment["DBUS_SESSION_BUS_ADDRESS"] != "unix:path="+path {
		t.Fatal("default keyring source was missed")
	}
	environment["DBUS_SESSION_BUS_ADDRESS"] = "explicit"
	noteSessionBus(environment, path)
	if environment["DBUS_SESSION_BUS_ADDRESS"] != "explicit" {
		t.Fatal("explicit bus selection changed")
	}
}
