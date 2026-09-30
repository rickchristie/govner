package vmdev

import (
	"fmt"

	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/config"
)

const TestAgent = "vm-test-agent"

func ConfigFor(tool string) (*config.Config, error) {
	cfg := config.DefaultConfig()
	if tool != TestAgent {
		if !aitool.IsBuiltin(tool) {
			return nil, fmt.Errorf("unsupported development agent %q", tool)
		}
		cfg.AITools = []config.ToolConfig{{Name: tool, Enabled: true, Mode: config.ModeMirror}}
	}
	cfg.VM = config.VMConfig{CPUs: 4, MemoryMiB: 4096, DiskGiB: 24, MaxDepth: 2, StartTimeoutS: 600, StopTimeoutS: 5}
	cfg.MonitorTimeoutSecs = 1
	cfg.WhitelistedDomains = append(cfg.WhitelistedDomains, config.DomainEntry{Domain: "vm-allowed.cooper.test", Source: "user"})
	return cfg, nil
}
