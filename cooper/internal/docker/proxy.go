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
	proxyName := ProxyContainerName()
	_ = exec.Command("docker", "rm", "-f", proxyName).Run()

	squidConf := filepath.Join(cooperDir, "proxy", "squid.conf")
	caCert := filepath.Join(cooperDir, "ca", "cooper-ca.pem")
	caKey := filepath.Join(cooperDir, "ca", "cooper-ca-key.pem")
	aclSocketDir := filepath.Join(cooperDir, "run")
	logDir := filepath.Join(cooperDir, "logs")

	if err := prepareProxyMountDirs(aclSocketDir, logDir); err != nil {
		return err
	}
	socatRules := filepath.Join(cooperDir, socatRulesFile)

	args := []string{
		"run", "-d",
		"--name", proxyName,
		"--network", ExternalNetworkName(),
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
		return fmt.Errorf("docker run %s failed: %w\n%s", proxyName, err, string(output))
	}

	// Connect to internal network so barrel containers can reach us
	// via Docker DNS as "cooper-proxy".
	if err := ConnectContainer(proxyName, InternalNetworkName()); err != nil {
		// If this fails, stop the container to avoid a half-configured proxy.
		_ = exec.Command("docker", "rm", "-f", proxyName).Run()
		return fmt.Errorf("connect proxy to internal network: %w", err)
	}

	return nil
}

func prepareProxyMountDirs(aclSocketDir, logDir string) error {
	// The socket directory is transient and may contain stale socket files from a
	// previous run. The log directory must be preserved: cooper up opens its own
	// command log before proxy startup, so deleting the directory unlinks up.log
	// while the process is still writing the shutdown reason.
	if err := os.RemoveAll(aclSocketDir); err != nil {
		return fmt.Errorf("remove stale mount dir %s: %w", aclSocketDir, err)
	}
	if err := os.MkdirAll(aclSocketDir, 0755); err != nil {
		return fmt.Errorf("create mount dir %s: %w", aclSocketDir, err)
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create mount dir %s: %w", logDir, err)
	}
	return nil
}

// StopProxy stops and removes the proxy container.
func StopProxy() error {
	return stopAndRemoveContainer(ProxyContainerName())
}

// IsProxyRunning checks whether the proxy container is currently running.
func IsProxyRunning() (bool, error) {
	proxyName := ProxyContainerName()
	cmd := exec.Command("docker", "ps",
		"--filter", fmt.Sprintf("name=^/%s$", proxyName),
		"--format", "{{.Names}}",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("docker ps failed: %w\n%s", err, string(output))
	}
	return strings.TrimSpace(string(output)) == proxyName, nil
}

// ReconfigureSquid validates squid.conf and sends SIGHUP to the supervised
// Squid process. Squid runs with -N and does not keep pid_filename, so
// `squid -k reconfigure` cannot find it even while the process is healthy.
// Docker can report the container as running before its entrypoint starts
// Squid. The polls cover that startup interval and wait until Squid confirms
// that the new listener is active.
func ReconfigureSquid() error {
	const script = `set -u
if ! squid -k parse; then
    echo "Squid configuration validation failed." >&2
    exit 1
fi
pid=""
port="$(awk '$1 == "http_port" { print $2; exit }' /etc/squid/squid.conf)"
ready=0
attempt=0
while [ "$attempt" -lt 50 ]; do
    pid="$(pgrep squid | head -n 1)"
    if [ -n "$pid" ] && [ -n "$port" ] && nc -z -w 1 127.0.0.1 "$port"; then
        ready=1
        break
    fi
    attempt=$((attempt + 1))
    sleep 0.1
done
if [ "$ready" != 1 ]; then
    echo "The supervised Squid process did not become ready." >&2
    exit 1
fi
cache_log=/var/log/squid/cache.log
log_size=0
if [ -f "$cache_log" ]; then
    log_size="$(wc -c < "$cache_log")"
fi
if ! kill -HUP "$pid"; then
    echo "Cannot send SIGHUP to Squid process $pid." >&2
    exit 1
fi
attempt=0
while [ "$attempt" -lt 100 ]; do
    if ! kill -0 "$pid"; then
        echo "Squid process $pid stopped during configuration reload." >&2
        exit 1
    fi
    current_size=0
    if [ -f "$cache_log" ]; then
        current_size="$(wc -c < "$cache_log")"
    fi
    log_offset=$((log_size + 1))
    if [ "$current_size" -lt "$log_size" ]; then
        log_offset=1
    fi
    if tail -c +"$log_offset" "$cache_log" 2>/dev/null | grep -Fq "Accepting SSL bumped HTTP Socket connections"; then
        exit 0
    fi
    attempt=$((attempt + 1))
    sleep 0.1
done
echo "Squid did not confirm the configuration reload." >&2
exit 1`
	cmd := exec.Command("docker", "exec", ProxyContainerName(), "sh", "-c", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validate and signal Squid reconfigure: %w\n%s", err, string(output))
	}
	return nil
}
