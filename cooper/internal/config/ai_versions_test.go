package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIVersionsAlwaysUseTheHost(t *testing.T) {
	oldHost, oldLatest, oldValidate := HostVersionDetector, LatestVersionResolver, VersionValidator
	t.Cleanup(func() { HostVersionDetector, LatestVersionResolver, VersionValidator = oldHost, oldLatest, oldValidate })
	HostVersionDetector = func(string) (string, error) { return "2.3.4", nil }
	LatestVersionResolver = func(string) (string, error) { t.Fatal("AI latest lookup called"); return "", nil }
	VersionValidator = func(string, string) (bool, error) { t.Fatal("AI pin lookup called"); return false, nil }
	for _, mode := range []VersionMode{ModeMirror, ModePin, ModeLatest, ModeOff} {
		cfg := DefaultConfig()
		cfg.AITools = []ToolConfig{{Name: "claude", Enabled: true, Mode: mode, PinnedVersion: "1.0.0", HostVersion: "1.0.0", ContainerVersion: "1.0.0"}}
		notices, err := RefreshDesiredToolVersions(cfg, DesiredVersionRefreshOptions{})
		if err != nil {
			t.Fatal(err)
		}
		tool := cfg.AITools[0]
		if tool.Mode != ModeMirror || tool.PinnedVersion != "" || tool.HostVersion != "2.3.4" || tool.ContainerVersion != "1.0.0" {
			t.Fatalf("wrong version state: %+v", tool)
		}
		if (mode == ModePin || mode == ModeLatest) && len(notices) != 1 {
			t.Fatal("missing migration notice")
		}
	}
}

func TestAIHostDetectionCannotUseStaleFallback(t *testing.T) {
	old := HostVersionDetector
	t.Cleanup(func() { HostVersionDetector = old })
	HostVersionDetector = func(string) (string, error) { return "", errors.New("not installed") }
	cfg := DefaultConfig()
	cfg.AITools = []ToolConfig{{Name: "claude", Enabled: true, Mode: ModeMirror, HostVersion: "1.0.0"}}
	if _, err := RefreshDesiredToolVersions(cfg, DesiredVersionRefreshOptions{AllowStaleFallback: true}); err == nil {
		t.Fatal("stale AI host version accepted")
	}
}

func TestSaveConvertsAIModesWithoutChangingProgrammingTools(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AITools = []ToolConfig{{Name: "claude", Mode: ModePin, PinnedVersion: "1.0.0", ContainerVersion: "1.0.0"}}
	cfg.ProgrammingTools = []ToolConfig{{Name: "go", Mode: ModePin, PinnedVersion: "1.24.10"}}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"pinned_version": "1.0.0"`) {
		t.Fatal("saved obsolete AI pin")
	}
	saved, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.AITools[0].Mode != ModeMirror || saved.ProgrammingTools[0].PinnedVersion != "1.24.10" || cfg.AITools[0].Mode != ModePin {
		t.Fatal("wrong conversion scope")
	}
}
