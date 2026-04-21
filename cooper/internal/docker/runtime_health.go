package docker

import (
	"fmt"
	"os/exec"
	"strings"
)

// IsProxySocatHealthy reports whether the proxy container is accepting
// connections on the configured bridge relay port, which is managed by socat.
func IsProxySocatHealthy(port int) bool {
	proxyName := ProxyContainerName()
	cmd := exec.Command("docker", "exec", proxyName, "bash", "-lc", fmt.Sprintf("echo >/dev/tcp/127.0.0.1/%d", port))
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if strings.Contains(trimmed, "No such container") || strings.Contains(trimmed, "is not running") {
			return false
		}
		return false
	}
	return true
}
