package docker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/config"
)

const socatRulesFile = "socat-rules.json"

// PortForwardConfig is the JSON file format for socat rules.
// Written to ~/.cooper/socat-rules.json and volume-mounted into containers
// at /etc/cooper/socat-rules.json.
type PortForwardConfig struct {
	BridgePort int                      `json:"bridge_port"`
	Rules      []config.PortForwardRule `json:"rules"`
}

// WritePortForwardConfig writes the socat rules JSON file to {cooperDir}/socat-rules.json.
func WritePortForwardConfig(cooperDir string, bridgePort int, rules []config.PortForwardRule) error {
	if err := os.MkdirAll(cooperDir, 0755); err != nil {
		return fmt.Errorf("create cooper directory %s: %w", cooperDir, err)
	}

	cfg := PortForwardConfig{
		BridgePort: bridgePort,
		Rules:      rules,
	}
	if cfg.Rules == nil {
		cfg.Rules = []config.PortForwardRule{}
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal socat rules: %w", err)
	}

	path := filepath.Join(cooperDir, socatRulesFile)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write socat rules: %w", err)
	}

	return nil
}

// LoadPortForwardConfig loads the socat rules JSON from {cooperDir}/socat-rules.json.
// If the file does not exist, returns a default config with no rules.
func LoadPortForwardConfig(cooperDir string) (*PortForwardConfig, error) {
	path := filepath.Join(cooperDir, socatRulesFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &PortForwardConfig{Rules: []config.PortForwardRule{}}, nil
		}
		return nil, fmt.Errorf("read socat rules: %w", err)
	}

	var cfg PortForwardConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse socat rules: %w", err)
	}

	return &cfg, nil
}

// ReloadSocat writes updated socat rules to the config file and signals all
// running containers to reload their socat processes via SIGHUP.
//
//  1. Write updated socat-rules.json
//  2. Signal proxy container: docker exec <runtime-proxy> kill -HUP 1
//  3. Signal each running barrel: docker exec barrel-X kill -HUP 1
//
// Returns an error describing any signal failures. The config file is always
// written first; signal errors are collected but do not prevent subsequent
// containers from being signaled.
func ReloadSocat(cooperDir string, bridgePort int, rules []config.PortForwardRule) error {
	// 1. Write updated config file.
	if err := WritePortForwardConfig(cooperDir, bridgePort, rules); err != nil {
		return fmt.Errorf("write socat config: %w", err)
	}

	var errs []string

	// 2. Signal proxy container.
	running, err := IsProxyRunning()
	if err == nil && running {
		cmd := exec.Command("docker", "exec", ProxyContainerName(), "kill", "-HUP", "1")
		if output, execErr := cmd.CombinedOutput(); execErr != nil {
			errs = append(errs, fmt.Sprintf("signal proxy: %v (%s)", execErr, strings.TrimSpace(string(output))))
		}
	}

	// 3. Signal each running barrel.
	barrels, err := ListBarrels()
	if err != nil {
		// Cannot list barrels — not fatal, but worth reporting.
		errs = append(errs, fmt.Sprintf("list barrels: %v", err))
	} else {
		for _, b := range barrels {
			if strings.HasPrefix(b.Name, BarrelNamePrefix()) {
				cmd := exec.Command("docker", "exec", b.Name, "kill", "-HUP", "1")
				if execErr := cmd.Run(); execErr != nil {
					errs = append(errs, fmt.Sprintf("signal %s: %v", b.Name, execErr))
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("socat reload signal errors: %s", strings.Join(errs, "; "))
	}
	return nil
}
