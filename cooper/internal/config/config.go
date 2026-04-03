package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config holds all Cooper configuration. This is the central data type
// passed to Docker, template, TUI, proxy, and bridge packages.
type Config struct {
	ProgrammingTools  []ToolConfig      `json:"programming_tools"`
	AITools           []ToolConfig      `json:"ai_tools"`
	WhitelistedDomains []DomainEntry    `json:"whitelisted_domains"`
	PortForwardRules  []PortForwardRule `json:"port_forward_rules"`
	ProxyPort         int               `json:"proxy_port"`
	BridgePort        int               `json:"bridge_port"`
	MonitorTimeoutSecs int              `json:"monitor_timeout_secs"`
	BlockedHistoryLimit int             `json:"blocked_history_limit"`
	AllowedHistoryLimit int             `json:"allowed_history_limit"`
	BridgeLogLimit    int               `json:"bridge_log_limit"`
	BridgeRoutes      []BridgeRoute     `json:"bridge_routes"`
	ClipboardTTLSecs  int               `json:"clipboard_ttl_secs"`
	ClipboardMaxBytes int               `json:"clipboard_max_bytes"`
}

// LoadConfig loads configuration from a JSON file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Merge any new default domains added in code updates.
	cfg.MergeDefaultDomains()

	// Apply defaults for fields added in later versions that may be missing
	// from existing config files (zero-value means unset in JSON).
	cfg.applyMissingDefaults()

	return &cfg, nil
}

// applyMissingDefaults fills in zero-value fields with sensible defaults.
// This ensures existing config files written before new fields were added
// continue to validate and work correctly.
func (c *Config) applyMissingDefaults() {
	if c.ClipboardTTLSecs <= 0 {
		c.ClipboardTTLSecs = 300
	}
	if c.ClipboardMaxBytes <= 0 {
		c.ClipboardMaxBytes = 20971520 // 20 MiB
	}
}

// SaveConfig saves configuration to a JSON file with indentation.
func SaveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// DefaultConfig returns a configuration with sensible defaults.
// Proxy port 3128 (Squid standard), bridge port 4343, monitor timeout 5s,
// history limits 500 entries.
func DefaultConfig() *Config {
	return &Config{
		ProgrammingTools:   []ToolConfig{},
		AITools:            []ToolConfig{},
		WhitelistedDomains: defaultWhitelistedDomains(),
		PortForwardRules:   []PortForwardRule{},
		ProxyPort:          3128,
		BridgePort:         4343,
		MonitorTimeoutSecs: 5,
		BlockedHistoryLimit: 500,
		AllowedHistoryLimit: 500,
		BridgeLogLimit:     500,
		BridgeRoutes:       []BridgeRoute{},
		ClipboardTTLSecs:   300,
		ClipboardMaxBytes:  20971520, // 20 MiB
	}
}

// defaultWhitelistedDomains returns the default set of whitelisted domains
// for AI provider APIs, GitHub services, and other safe endpoints.
func defaultWhitelistedDomains() []DomainEntry {
	return []DomainEntry{
		{Domain: ".anthropic.com", IncludeSubdomains: true, Source: "default"},
		{Domain: "platform.claude.com", IncludeSubdomains: false, Source: "default"},
		{Domain: ".openai.com", IncludeSubdomains: true, Source: "default"},
		{Domain: ".chatgpt.com", IncludeSubdomains: true, Source: "default"},
		{Domain: "github.com", IncludeSubdomains: false, Source: "default"},
		{Domain: "api.github.com", IncludeSubdomains: false, Source: "default"},
		{Domain: ".githubcopilot.com", IncludeSubdomains: true, Source: "default"},
		{Domain: "copilot-proxy.githubusercontent.com", IncludeSubdomains: false, Source: "default"},
		{Domain: "origin-tracker.githubusercontent.com", IncludeSubdomains: false, Source: "default"},
		{Domain: "copilot-telemetry.githubusercontent.com", IncludeSubdomains: false, Source: "default"},
		{Domain: "collector.github.com", IncludeSubdomains: false, Source: "default"},
		{Domain: "default.exp-tas.com", IncludeSubdomains: false, Source: "default"},
		{Domain: "raw.githubusercontent.com", IncludeSubdomains: false, Source: "default"},
		{Domain: "statsig.anthropic.com", IncludeSubdomains: false, Source: "default"},
		{Domain: ".opencode.ai", IncludeSubdomains: true, Source: "default"},
	}
}

// MergeDefaultDomains ensures all default whitelisted domains are present
// in the config. New defaults added in code updates are merged into existing
// configs so users don't have to reconfigure to pick up new tool domains.
func (c *Config) MergeDefaultDomains() {
	existing := make(map[string]bool)
	for _, d := range c.WhitelistedDomains {
		existing[d.Domain] = true
	}
	for _, d := range defaultWhitelistedDomains() {
		if !existing[d.Domain] {
			c.WhitelistedDomains = append(c.WhitelistedDomains, d)
		}
	}
}

// Validate checks if the configuration is valid. Returns an error if any
// validation rule is violated.
func (c *Config) Validate() error {
	// Validate proxy port range
	if c.ProxyPort <= 0 || c.ProxyPort > 65535 {
		return fmt.Errorf("proxy port %d is out of valid range (1-65535)", c.ProxyPort)
	}

	// Validate bridge port range
	if c.BridgePort <= 0 || c.BridgePort > 65535 {
		return fmt.Errorf("bridge port %d is out of valid range (1-65535)", c.BridgePort)
	}

	// Proxy port must not equal bridge port
	if c.ProxyPort == c.BridgePort {
		return fmt.Errorf("proxy port (%d) and bridge port (%d) must be different", c.ProxyPort, c.BridgePort)
	}

	// Check port forwarding rules don't collide with proxy or bridge ports
	reservedPorts := map[int]string{
		c.ProxyPort:  "proxy",
		c.BridgePort: "bridge",
	}

	for _, rule := range c.PortForwardRules {
		// Validate container port range
		if rule.ContainerPort <= 0 || rule.ContainerPort > 65535 {
			return fmt.Errorf("port forward rule %q: container port %d is out of valid range (1-65535)",
				rule.Description, rule.ContainerPort)
		}
		// Validate host port range
		if rule.HostPort <= 0 || rule.HostPort > 65535 {
			return fmt.Errorf("port forward rule %q: host port %d is out of valid range (1-65535)",
				rule.Description, rule.HostPort)
		}

		if rule.IsRange {
			if rule.RangeEnd <= rule.ContainerPort {
				return fmt.Errorf("port forward rule %q: range end (%d) must be greater than container port (%d)",
					rule.Description, rule.RangeEnd, rule.ContainerPort)
			}
			if rule.RangeEnd > 65535 {
				return fmt.Errorf("port forward rule %q: range end %d is out of valid range (1-65535)",
					rule.Description, rule.RangeEnd)
			}
			// Check each port in the range against reserved ports
			rangeSize := rule.RangeEnd - rule.ContainerPort + 1
			for i := 0; i < rangeSize; i++ {
				port := rule.ContainerPort + i
				if usage, ok := reservedPorts[port]; ok {
					return fmt.Errorf("port forward rule %q: container port %d collides with %s port",
						rule.Description, port, usage)
				}
			}
		} else {
			// Check single port against reserved ports
			if usage, ok := reservedPorts[rule.ContainerPort]; ok {
				return fmt.Errorf("port forward rule %q: container port %d collides with %s port",
					rule.Description, rule.ContainerPort, usage)
			}
		}
	}

	// Validate monitor timeout
	if c.MonitorTimeoutSecs <= 0 {
		return fmt.Errorf("monitor timeout must be positive, got %d", c.MonitorTimeoutSecs)
	}

	// Validate history limits
	if c.BlockedHistoryLimit <= 0 {
		return fmt.Errorf("blocked history limit must be positive, got %d", c.BlockedHistoryLimit)
	}
	if c.AllowedHistoryLimit <= 0 {
		return fmt.Errorf("allowed history limit must be positive, got %d", c.AllowedHistoryLimit)
	}
	if c.BridgeLogLimit <= 0 {
		return fmt.Errorf("bridge log limit must be positive, got %d", c.BridgeLogLimit)
	}

	// Validate clipboard settings
	if c.ClipboardTTLSecs <= 0 {
		return fmt.Errorf("clipboard TTL must be positive, got %d", c.ClipboardTTLSecs)
	}
	if c.ClipboardMaxBytes <= 0 {
		return fmt.Errorf("clipboard max bytes must be positive, got %d", c.ClipboardMaxBytes)
	}

	return nil
}

// HasEnabledAITool returns true if at least one AI tool is enabled.
// This is a warning check, not a validation error -- Cooper can run
// without AI tools, but it's unusual.
func (c *Config) HasEnabledAITool() bool {
	for _, tool := range c.AITools {
		if tool.Enabled {
			return true
		}
	}
	return false
}
