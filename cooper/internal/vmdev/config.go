package vmdev

import (
	"fmt"

	"github.com/rickchristie/govner/cooper/internal/config"
)

const TestAgent = "vm-test-agent"

// These pins are test inputs. Change one to check an agent release without
// resolving or building every other supported agent.
var AgentVersions = map[string]string{
	"claude": "2.1.87", "copilot": "1.0.12", "codex": "0.117.0", "opencode": "1.3.7", "grok": "1.0.4", "antigravity": "1.2.2",
}

func ConfigFor(tool string) (*config.Config, error) {
	cfg := config.DefaultConfig()
	if tool != TestAgent {
		version, ok := AgentVersions[tool]
		if !ok {
			return nil, fmt.Errorf("unsupported development agent %q", tool)
		}
		cfg.AITools = []config.ToolConfig{{Name: tool, Enabled: true, Mode: config.ModePin, PinnedVersion: version}}
	}
	cfg.VM = config.VMConfig{CPUs: 4, MemoryMiB: 4096, DiskGiB: 24, MaxDepth: 2, StartTimeoutS: 600, StopTimeoutS: 5}
	cfg.MonitorTimeoutSecs = 1
	cfg.WhitelistedDomains = append(cfg.WhitelistedDomains, config.DomainEntry{Domain: "vm-allowed.cooper.test", Source: "user"})
	return cfg, nil
}
