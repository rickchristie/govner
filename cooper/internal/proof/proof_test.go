package proof

import (
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestFirstBarrelUsesConfiguredToolOrder(t *testing.T) {
	ctx := ProofContext{
		Cfg: &config.Config{AITools: []config.ToolConfig{
			{Name: "grok", Enabled: true},
			{Name: "claude", Enabled: true},
		}},
		barrels: map[string]string{
			"claude": "barrel-claude",
			"grok":   "barrel-grok",
		},
	}

	toolName, barrelName := ctx.firstBarrel()
	if toolName != "grok" || barrelName != "barrel-grok" {
		t.Fatalf("firstBarrel() = (%q, %q), want (grok, barrel-grok)", toolName, barrelName)
	}
}
