package vmdev

import (
	"github.com/rickchristie/govner/cooper/internal/config"
	"testing"
)

func TestPreparationSelectsExactlyOneAgent(t *testing.T) {
	for agent, version := range AgentVersions {
		cfg, err := ConfigFor(agent)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.ProgrammingTools) != 0 || len(cfg.AITools) != 1 {
			t.Fatalf("%s would prepare unrelated tools: %#v", agent, cfg)
		}
		selected := cfg.AITools[0]
		if selected.Name != agent || !selected.Enabled || selected.Mode != config.ModePin || selected.PinnedVersion != version {
			t.Fatalf("selected agent was not pinned: %#v", selected)
		}
	}
	cfg, err := ConfigFor(TestAgent)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ProgrammingTools) != 0 || len(cfg.AITools) != 0 {
		t.Fatal("small fixture enables provider or language tools")
	}
	if _, err := ConfigFor("unreviewed"); err == nil {
		t.Fatal("unknown agent accepted")
	}
}
