package workload

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestChatGPTKeepsCompleteSelectedStateAtPublicPaths(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	for _, test := range []struct {
		env                   map[string]string
		desktop, codex, cache string
	}{
		{nil, filepath.Join(home, ".config/Codex"), filepath.Join(home, ".codex"), filepath.Join(home, ".cache/Codex")},
		{map[string]string{"XDG_CONFIG_HOME": "settings", "XDG_CACHE_HOME": "cache"}, filepath.Join(workspace, "settings/Codex"), filepath.Join(home, ".codex"), filepath.Join(workspace, "cache/Codex")},
		{map[string]string{"CODEX_ELECTRON_USER_DATA_PATH": " app-state ", "CODEX_HOME": "core-state"}, filepath.Join(workspace, "app-state"), filepath.Join(workspace, "core-state"), ""},
		{map[string]string{"CODEX_ELECTRON_USER_DATA_PATH": filepath.Join(home, ".config/Work")}, filepath.Join(home, ".config/Work"), filepath.Join(home, ".codex"), filepath.Join(home, ".cache/Work")},
	} {
		paths, err := ResolveAgentScope("chatgpt", home, workspace, test.env)
		if err != nil {
			t.Fatal(err)
		}
		roots := map[string]string{}
		for _, mount := range paths.Mounts {
			if mount.Source != mount.Target || mount.Access != ReadWrite {
				t.Fatalf("state path changed: %+v", mount)
			}
			roots[mount.ID] = mount.Source
		}
		count := 5
		if test.cache != "" {
			count++
		}
		if len(roots) != count || roots["chatgpt-desktop"] != test.desktop || roots["codex-state"] != test.codex || roots["chatgpt-cache"] != test.cache {
			t.Fatalf("wrong selected roots: %v", roots)
		}
		if !strings.Contains(strings.Join(RenderEnvironment(paths.Environment), "\n"), "CODEX_ELECTRON_USER_DATA_PATH="+test.desktop) {
			t.Fatal("desktop override lost")
		}
	}
}

func TestDesktopHostnameIsStableAndSeparate(t *testing.T) {
	first := DesktopHostname("cooper-vm-work-chatgpt")
	if first != DesktopHostname("cooper-vm-work-chatgpt") || first == DesktopHostname("cooper-barrel-work-chatgpt") || len(first) > 63 {
		t.Fatal("desktop lock namespace is not stable and separate")
	}
}
