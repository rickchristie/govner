package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/config"
)

const (
	// ContainerProxy is the Docker container name for the proxy.
	ContainerProxy = "cooper-proxy"
)

// StartProxy starts the proxy container with full dual-network topology.
//
// The proxy is created on cooper-external (regular bridge with internet),
// then connected to cooper-internal (isolated, no gateway) so CLI containers
// can reach it via Docker DNS as "cooper-proxy".
//
// cooperDir is the path to ~/.cooper (contains squid.conf, CA cert, run dir, logs).
func StartProxy(cfg *config.Config, cooperDir string) error {
	// Write socat-rules.json before starting so the volume mount has content.
	if err := WritePortForwardConfig(cooperDir, cfg.BridgePort, cfg.PortForwardRules); err != nil {
		return fmt.Errorf("write socat rules: %w", err)
	}

	// Remove any existing proxy container first.
	_ = exec.Command("docker", "rm", "-f", ContainerProxy).Run()

	squidConf := filepath.Join(cooperDir, "proxy", "squid.conf")
	caCert := filepath.Join(cooperDir, "ca", "cooper-ca.pem")
	caKey := filepath.Join(cooperDir, "ca", "cooper-ca-key.pem")
	aclSocketDir := filepath.Join(cooperDir, "run")
	logDir := filepath.Join(cooperDir, "logs")

	// Create mount directories as the current user BEFORE docker run.
	// If these don't exist, Docker creates them as root, making them
	// inaccessible to the user process (e.g., ACL socket listener).
	for _, dir := range []string{aclSocketDir, logDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create mount dir %s: %w", dir, err)
		}
	}
	socatRules := filepath.Join(cooperDir, socatRulesFile)

	args := []string{
		"run", "-d",
		"--name", ContainerProxy,
		"--network", NetworkExternal,
		"--add-host=host.docker.internal:host-gateway",
		"--restart", "unless-stopped",

		// Volume mounts: squid config (hot-reloadable), CA cert/key, ACL socket dir, logs.
		"-v", fmt.Sprintf("%s:/etc/squid/squid.conf:ro", squidConf),
		"-v", fmt.Sprintf("%s:/etc/squid/cooper-ca.pem:ro", caCert),
		"-v", fmt.Sprintf("%s:/etc/squid/cooper-ca-key.pem:ro", caKey),
		"-v", fmt.Sprintf("%s:/var/run/cooper:rw", aclSocketDir),
		"-v", fmt.Sprintf("%s:/var/log/squid:rw", logDir),

		// Socat port forwarding rules (live-reloadable via SIGHUP).
		"-v", fmt.Sprintf("%s:/etc/cooper/socat-rules.json:ro", socatRules),

		// Publish proxy port on localhost only for host access.
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", cfg.ProxyPort, cfg.ProxyPort),
	}

	// Port forwarding rules are handled by socat relays inside the proxy and
	// barrel containers (see entrypoint templates), not by Docker -p publishing.
	// Only the Squid proxy port is published for host TUI access.

	args = append(args, GetImageProxy())

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run %s failed: %w\n%s", ContainerProxy, err, string(output))
	}

	// Connect to internal network so barrel containers can reach us
	// via Docker DNS as "cooper-proxy".
	if err := ConnectContainer(ContainerProxy, NetworkInternal); err != nil {
		// If this fails, stop the container to avoid a half-configured proxy.
		_ = exec.Command("docker", "rm", "-f", ContainerProxy).Run()
		return fmt.Errorf("connect proxy to internal network: %w", err)
	}

	return nil
}

// StopProxy stops and removes the proxy container.
func StopProxy() error {
	cmd := exec.Command("docker", "stop", ContainerProxy)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If already stopped or doesn't exist, that's fine.
		if !strings.Contains(string(output), "No such container") &&
			!strings.Contains(string(output), "is not running") {
			return fmt.Errorf("docker stop %s failed: %w\n%s", ContainerProxy, err, string(output))
		}
	}

	cmd = exec.Command("docker", "rm", "-f", ContainerProxy)
	output, err = cmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "No such container") {
			return fmt.Errorf("docker rm %s failed: %w\n%s", ContainerProxy, err, string(output))
		}
	}

	return nil
}

// IsProxyRunning checks whether the proxy container is currently running.
func IsProxyRunning() (bool, error) {
	cmd := exec.Command("docker", "ps",
		"--filter", fmt.Sprintf("name=^/%s$", ContainerProxy),
		"--format", "{{.Names}}",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("docker ps failed: %w\n%s", err, string(output))
	}
	return strings.TrimSpace(string(output)) == ContainerProxy, nil
}

// ProxyExec executes a command inside the running proxy container and
// returns its combined stdout/stderr output.
func ProxyExec(cmd string) (string, error) {
	// Split the command string into args for exec.
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}

	args := append([]string{"exec", ContainerProxy}, parts...)
	c := exec.Command("docker", args...)
	output, err := c.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("docker exec %s %q failed: %w\n%s",
			ContainerProxy, cmd, err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

// ReconfigureSquid sends the reconfigure signal to Squid inside the proxy
// container. This causes Squid to reload squid.conf without restarting,
// enabling hot-reload of whitelist and ACL changes.
func ReconfigureSquid() error {
	_, err := ProxyExec("squid -k reconfigure")
	if err != nil {
		return fmt.Errorf("squid reconfigure: %w", err)
	}
	return nil
}
