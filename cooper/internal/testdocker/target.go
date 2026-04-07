package testdocker

import (
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rickchristie/govner/cooper/internal/docker"
)

var httpsTargetSeq uint64

// HTTPSTarget is a deterministic upstream HTTPS server for Docker-backed tests.
// It lives only on the Cooper external network, so proxy tests can exercise
// Squid's allow/deny and SSL-bump paths without depending on the public internet.
type HTTPSTarget struct {
	ContainerName string
	Domains       []string
	IP            string
}

// StartHTTPSTarget starts a simple HTTPS server container on the current Cooper
// external network and publishes each requested domain as a Docker DNS alias.
// The target serves a self-signed certificate with SANs covering every alias.
func StartHTTPSTarget(domains ...string) (*HTTPSTarget, error) {
	cleanDomains, err := cleanTargetDomains(domains)
	if err != nil {
		return nil, err
	}

	target := &HTTPSTarget{
		ContainerName: fmt.Sprintf("%s-https-target-%03d", docker.RuntimeNamespace(), atomic.AddUint64(&httpsTargetSeq, 1)),
		Domains:       cleanDomains,
	}

	args := []string{
		"run", "-d",
		"--name", target.ContainerName,
		"--network", docker.ExternalNetworkName(),
		"--user", "root",
	}
	for _, domain := range cleanDomains {
		args = append(args, "--network-alias", domain)
	}
	args = append(args,
		"--entrypoint", "bash",
		docker.GetImageProxy(),
		"-lc", renderHTTPSTargetScript(cleanDomains),
	)

	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("start HTTPS target %s failed: %w\n%s", target.ContainerName, err, string(out))
	}

	if err := target.WaitReady(10 * time.Second); err != nil {
		_ = target.Remove()
		return nil, err
	}

	ip, err := inspectContainerIP(target.ContainerName)
	if err != nil {
		_ = target.Remove()
		return nil, err
	}
	target.IP = ip
	return target, nil
}

// WaitReady waits until the target accepts TCP connections on port 443.
func (t *HTTPSTarget) WaitReady(timeout time.Duration) error {
	if t == nil {
		return fmt.Errorf("wait HTTPS target: nil target")
	}

	deadline := time.Now().Add(timeout)
	var lastErr string
	for time.Now().Before(deadline) {
		out, err := exec.Command(
			"docker", "exec", t.ContainerName,
			"bash", "-lc", "exec 3<>/dev/tcp/127.0.0.1/443",
		).CombinedOutput()
		if err == nil {
			return nil
		}
		lastErr = strings.TrimSpace(string(out))
		if lastErr == "" {
			lastErr = err.Error()
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("HTTPS target %s did not become ready within %s (last error: %s)", t.ContainerName, timeout, lastErr)
}

// Remove force-removes the target container.
func (t *HTTPSTarget) Remove() error {
	if t == nil || strings.TrimSpace(t.ContainerName) == "" {
		return nil
	}
	out, err := exec.Command("docker", "rm", "-f", t.ContainerName).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "No such container") {
			return nil
		}
		return fmt.Errorf("remove HTTPS target %s failed: %w\n%s", t.ContainerName, err, string(out))
	}
	return nil
}

func cleanTargetDomains(domains []string) ([]string, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("start HTTPS target: no domains provided")
	}

	seen := make(map[string]struct{}, len(domains))
	clean := make([]string, 0, len(domains))
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			return nil, fmt.Errorf("start HTTPS target: empty domain")
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		clean = append(clean, domain)
	}
	return clean, nil
}

func inspectContainerIP(containerName string) (string, error) {
	out, err := exec.Command(
		"docker", "inspect",
		"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}",
		containerName,
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect IP for %s failed: %w\n%s", containerName, err, string(out))
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("inspect IP for %s returned no addresses", containerName)
	}
	return fields[0], nil
}

func renderHTTPSTargetScript(domains []string) string {
	var sanEntries strings.Builder
	for i, domain := range domains {
		fmt.Fprintf(&sanEntries, "DNS.%d = %s\n", i+1, domain)
	}

	return fmt.Sprintf(`set -eu
cat >/tmp/openssl.cnf <<'EOF'
[req]
distinguished_name = req_distinguished_name
x509_extensions = v3_req
prompt = no
[req_distinguished_name]
CN = %s
[v3_req]
subjectAltName = @alt_names
[alt_names]
%sEOF
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -keyout /tmp/target.key \
  -out /tmp/target.crt \
  -config /tmp/openssl.cnf >/tmp/target-cert.log 2>&1
exec openssl s_server -quiet -accept 443 -cert /tmp/target.crt -key /tmp/target.key -www
`, domains[0], sanEntries.String())
}
