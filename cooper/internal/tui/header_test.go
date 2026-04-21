package tui

import (
	"strings"
	"testing"

	cooperapp "github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestHeaderBar_ShowsHealthBadges(t *testing.T) {
	mockApp := cooperapp.NewMockApp(&config.Config{}, t.TempDir())
	mockApp.HeaderHealthVal = cooperapp.HeaderHealth{Proxy: true, Socat: false, Bridge: true}

	model := NewModel(mockApp)
	header := model.headerBar(200)

	for _, want := range []string{"Proxy ✓", "Socat ✗", "Bridge ✓"} {
		if !strings.Contains(header, want) {
			t.Fatalf("expected header to contain %q:\n%s", want, header)
		}
	}
	if strings.Contains(header, "barrel-proof") {
		t.Fatalf("header should not include removed barrel-proof tagline:\n%s", header)
	}
}

func TestHeaderBar_ShowsMirrorMismatchWarningOnlyForMirrorTools(t *testing.T) {
	mockApp := cooperapp.NewMockApp(&config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true, Mode: config.ModeMirror},
			{Name: "node", Enabled: true, Mode: config.ModePin},
		},
		AITools: []config.ToolConfig{{Name: "claude", Enabled: true, Mode: config.ModeMirror}},
	}, t.TempDir())
	mockApp.SetStartupWarnings([]string{
		"go: container=1.24.0, expected=1.24.1 (mirror mode)",
		"node: container=22.10.0, expected=22.12.0 (pin mode)",
		"typescript-language-server (for node): container=1.0.0, expected=1.0.1",
	})

	model := NewModel(mockApp)
	header := model.headerBar(200)

	if !strings.Contains(header, "tool mismatch") {
		t.Fatalf("expected mirror mismatch warning in header:\n%s", header)
	}
	if strings.Contains(header, "2 tool mismatches") {
		t.Fatalf("expected only mirror top-level mismatches to count:\n%s", header)
	}
}

func TestTopLevelToolMismatchCount(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{{Name: "go", Enabled: true, Mode: config.ModeMirror}},
		AITools:          []config.ToolConfig{{Name: "claude", Enabled: true, Mode: config.ModeMirror}},
	}
	count := topLevelToolMismatchCount(cfg, []string{
		"go: container=1.24.0, expected=1.24.1 (mirror mode)",
		"claude: container=1.0.0, expected=1.0.1 (mirror mode)",
		"gopls (for go): container=v0.20.0, expected=v0.21.1",
		"Font sync failed: no fonts",
	})
	if count != 2 {
		t.Fatalf("topLevelToolMismatchCount() = %d, want 2", count)
	}
}
