package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestWriteAllTemplates_GrokRemovesObsoletePolicyAndPreservesOtherFiles(t *testing.T) {
	cfg := &config.Config{
		AITools: []config.ToolConfig{
			{Name: "grok", Enabled: true, Mode: config.ModePin, PinnedVersion: "1.0.4"},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}
	baseDir := filepath.Join(t.TempDir(), "base")
	cliDir := filepath.Join(t.TempDir(), "cli")

	if err := WriteAllTemplates(baseDir, cliDir, cfg, nil); err != nil {
		t.Fatalf("WriteAllTemplates: %v", err)
	}
	reqPath := filepath.Join(cliDir, "grok", "requirements.toml")
	if _, err := os.Stat(reqPath); !os.IsNotExist(err) {
		t.Fatalf("requirements.toml must not be generated: %v", err)
	}

	extra := filepath.Join(cliDir, "grok", "notes.txt")
	if err := os.WriteFile(extra, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reqPath, []byte("[memory]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAllTemplates(baseDir, cliDir, cfg, nil); err != nil {
		t.Fatalf("refresh generated grok dir: %v", err)
	}
	kept, err := os.ReadFile(extra)
	if err != nil || string(kept) != "keep me" {
		t.Fatalf("unrelated file was not preserved: %v %q", err, kept)
	}
	if _, err := os.Stat(reqPath); !os.IsNotExist(err) {
		t.Fatalf("obsolete requirements.toml was not removed: %v", err)
	}
}

func TestWriteAllTemplates_RemovesObsoleteGrokPolicyWhenDisabled(t *testing.T) {
	cfg := &config.Config{
		AITools:    []config.ToolConfig{{Name: "grok", Enabled: true, Mode: config.ModePin, PinnedVersion: "1.0.4"}},
		ProxyPort:  3128,
		BridgePort: 4343,
	}
	baseDir := filepath.Join(t.TempDir(), "base")
	cliDir := filepath.Join(t.TempDir(), "cli")
	if err := WriteAllTemplates(baseDir, cliDir, cfg, nil); err != nil {
		t.Fatal(err)
	}
	reqPath := filepath.Join(cliDir, "grok", "requirements.toml")
	if err := os.WriteFile(reqPath, []byte("[memory]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.AITools[0].Enabled = false
	if err := WriteAllTemplates(baseDir, cliDir, cfg, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reqPath); !os.IsNotExist(err) {
		t.Fatalf("obsolete requirements.toml was not removed: %v", err)
	}
}

func TestValidateGrokOutputDirUnmanagedConflict(t *testing.T) {
	cliDir := t.TempDir()
	grokDir := filepath.Join(cliDir, "grok")
	if err := os.MkdirAll(grokDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(grokDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AITools: []config.ToolConfig{{Name: "grok", Enabled: true, Mode: config.ModePin, PinnedVersion: "1.0.4"}}}
	err := ValidateGrokOutputDir(cliDir)
	if err == nil {
		t.Fatal("expected unmanaged grok conflict")
	}
	if !strings.Contains(err.Error(), "Rename") || !strings.Contains(err.Error(), "grok-custom") {
		t.Fatalf("conflict error missing rename guidance: %v", err)
	}

	baseDir := filepath.Join(t.TempDir(), "base")
	if err := WriteAllTemplates(baseDir, cliDir, cfg, nil); err == nil {
		t.Fatal("WriteAllTemplates should refuse unmanaged grok dir")
	}
	data, err := os.ReadFile(filepath.Join(grokDir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "FROM scratch\n" {
		t.Fatalf("unmanaged Dockerfile was rewritten: %q", data)
	}
}

func TestValidateGrokOutputDirNonDockerfileConflict(t *testing.T) {
	cliDir := t.TempDir()
	grokDir := filepath.Join(cliDir, "grok")
	if err := os.MkdirAll(grokDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(grokDir, "notes.txt"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGrokOutputDir(cliDir); err == nil {
		t.Fatal("expected conflict for grok dir without generated Dockerfile")
	}
}

func TestRenderSquidConf_GrokPolicyUnconditional(t *testing.T) {
	cfg := noToolsConfig()
	result, err := RenderSquidConf(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertGrokPathPolicy(t, result)
}
