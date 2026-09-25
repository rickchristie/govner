package vme2e

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/chatgpt"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/templates"
)

func TestSelfHostProxyAllowsChatGPTPackagesWithCodexOnly(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AITools = []config.ToolConfig{{Name: "codex", Enabled: true}}
	appendSelfHostDomains(cfg)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	// Reload applies the selected-agent domain rules. The outer Codex VM must
	// retain the package host needed by the complete nested Go test suite.
	reloaded, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := templates.RenderSquidConf(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	packageURL, err := url.Parse(chatgpt.PackageBase)
	if err != nil {
		t.Fatal(err)
	}
	want := "\nacl allowed_domains dstdomain " + packageURL.Hostname() + "\n"
	if !strings.Contains(rules, want) {
		t.Fatalf("self-host proxy does not allow the ChatGPT package host %s", packageURL.Hostname())
	}
	if strings.Contains(rules, "\nacl allowed_domains dstdomain vm-blocked.cooper.test\n") {
		t.Fatal("self-host proxy allows the blocked test host")
	}
}
