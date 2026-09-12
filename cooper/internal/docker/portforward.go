package docker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/config"
)

const (
	liveConfigDir  = "live"
	socatRulesFile = "socat-rules.json"
)

// PortForwardConfig is the JSON file format for socat rules.
// Written below ~/.cooper/live and made visible through a directory mount.
// The directory mount keeps atomic replacements visible in running workloads.
type PortForwardConfig struct {
	BridgePort int                      `json:"bridge_port"`
	Rules      []config.PortForwardRule `json:"rules"`
}

// WritePortForwardConfig atomically writes the live socat rules file.
func WritePortForwardConfig(cooperDir string, bridgePort int, rules []config.PortForwardRule) error {
	dir := LiveConfigPath(cooperDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create Cooper live configuration directory %s: %w", dir, err)
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

	temporary, err := os.CreateTemp(dir, ".socat-rules-*.part")
	if err != nil {
		return fmt.Errorf("create temporary socat rules: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary socat rules: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("set socat rules mode: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync socat rules: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close socat rules: %w", err)
	}
	if err := os.Rename(temporaryPath, PortForwardConfigPath(cooperDir)); err != nil {
		return fmt.Errorf("replace socat rules: %w", err)
	}

	return nil
}

// LoadPortForwardConfig loads the live socat rules JSON.
// If the file does not exist, returns a default config with no rules.
func LoadPortForwardConfig(cooperDir string) (*PortForwardConfig, error) {
	path := PortForwardConfigPath(cooperDir)
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

// LiveConfigPath returns the public, non-secret live configuration directory.
func LiveConfigPath(cooperDir string) string {
	return filepath.Join(cooperDir, liveConfigDir)
}

// PortForwardConfigPath returns the live port-forward policy path.
func PortForwardConfigPath(cooperDir string) string {
	return filepath.Join(LiveConfigPath(cooperDir), socatRulesFile)
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
	previous, err := LoadPortForwardConfig(cooperDir)
	if err != nil {
		return fmt.Errorf("load current socat config: %w", err)
	}
	// 1. Write updated config file.
	if err := WritePortForwardConfig(cooperDir, bridgePort, rules); err != nil {
		return fmt.Errorf("write socat config: %w", err)
	}

	var errs []string

	// 2. Signal proxy container.
	running, err := IsProxyRunning()
	var signaled []string
	if err == nil && running {
		cmd := exec.Command("docker", "exec", ProxyContainerName(), "kill", "-HUP", "1")
		if output, execErr := cmd.CombinedOutput(); execErr != nil {
			errs = append(errs, fmt.Sprintf("signal proxy: %v (%s)", execErr, strings.TrimSpace(string(output))))
		} else {
			signaled = append(signaled, ProxyContainerName())
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
				} else {
					signaled = append(signaled, b.Name)
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("socat reload signal errors: %s", strings.Join(errs, "; "))
	}
	desired := append([]int{bridgePort}, forwardPorts(rules)...)
	removed := removedPorts(forwardPorts(previous.Rules), desired)
	if err := waitSocatReload(signaled, desired, removed, 10*time.Second); err != nil {
		return err
	}
	return nil
}

func forwardPorts(rules []config.PortForwardRule) []int {
	set := make(map[int]bool)
	for _, rule := range rules {
		end := rule.ContainerPort
		if rule.IsRange && rule.RangeEnd > end {
			end = rule.RangeEnd
		}
		for port := rule.ContainerPort; port <= end; port++ {
			set[port] = true
		}
	}
	ports := make([]int, 0, len(set))
	for port := range set {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

func removedPorts(previous, desired []int) []int {
	keep := make(map[int]bool, len(desired))
	for _, port := range desired {
		keep[port] = true
	}
	var removed []int
	for _, port := range previous {
		if !keep[port] {
			removed = append(removed, port)
		}
	}
	return removed
}

func waitSocatReload(containers []string, desired, removed []int, timeout time.Duration) error {
	if len(containers) == 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if socatStateMatches(containers, desired, removed) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("socat listeners did not apply the new port policy in %s", timeout)
}

func socatStateMatches(containers []string, desired, removed []int) bool {
	for _, container := range containers {
		for _, port := range desired {
			if !containerListens(container, port) {
				return false
			}
		}
		for _, port := range removed {
			if containerListens(container, port) {
				return false
			}
		}
	}
	return true
}

func containerListens(container string, port int) bool {
	command := fmt.Sprintf("exec 3<>/dev/tcp/127.0.0.1/%d", port)
	return exec.Command("docker", "exec", container, "timeout", "1", "bash", "-lc", command).Run() == nil
}
