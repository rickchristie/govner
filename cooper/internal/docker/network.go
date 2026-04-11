package docker

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
)

const (
	// NetworkExternal is the regular bridge network with internet access.
	// Only the proxy container should be attached to this network.
	NetworkExternal = "cooper-external"

	// NetworkInternal is the isolated network created with --internal.
	// It has NO default gateway and NO route to the internet.
	// CLI containers and the proxy container are both on this network.
	NetworkInternal = "cooper-internal"
)

// EnsureNetworks creates both cooper networks if they don't already exist.
// cooper-external is a regular bridge network (has internet access).
// cooper-internal is created with --internal (no gateway, no internet).
func EnsureNetworks() error {
	if err := ensureNetwork(ExternalNetworkName(), false); err != nil {
		return fmt.Errorf("ensure external network: %w", err)
	}
	if err := ensureNetwork(InternalNetworkName(), true); err != nil {
		return fmt.Errorf("ensure internal network: %w", err)
	}
	return nil
}

// ensureNetwork creates a network if it does not already exist.
// If internal is true, the --internal flag is passed to disable the default gateway.
func ensureNetwork(name string, internal bool) error {
	exists, err := NetworkExists(name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	args := []string{"network", "create"}
	if internal {
		args = append(args, "--internal")
	}
	args = append(args, name)

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "already exists") {
			return nil
		}
		return fmt.Errorf("docker network create %s failed: %w\n%s", name, err, string(output))
	}
	return nil
}

// RemoveNetworks removes both cooper networks.
// Errors from individual removals are collected and returned together.
func RemoveNetworks() error {
	var errs []string
	for _, name := range []string{InternalNetworkName(), ExternalNetworkName()} {
		cmd := exec.Command("docker", "network", "rm", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			if strings.Contains(string(output), "No such network") ||
				strings.Contains(string(output), "not found") {
				continue
			}
			errs = append(errs, fmt.Sprintf("docker network rm %s: %v\n%s", name, err, string(output)))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors removing networks:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// NetworkExists checks whether a Docker network with the given name exists.
func NetworkExists(name string) (bool, error) {
	cmd := exec.Command("docker", "network", "inspect", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// "No such network" means it doesn't exist — not an error.
		if strings.Contains(string(output), "No such network") ||
			strings.Contains(string(output), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("docker network inspect %s failed: %w\n%s", name, err, string(output))
	}
	return true, nil
}

// GetGatewayIP returns the gateway IP address of the named Docker network.
// This is used to discover the Docker bridge gateway IP for the execution bridge bind address.
func GetGatewayIP(networkName string) (string, error) {
	cmd := exec.Command("docker", "network", "inspect",
		"--format", "{{range .IPAM.Config}}{{.Gateway}}{{end}}",
		networkName,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker network inspect %s failed: %w\n%s", networkName, err, string(output))
	}

	ip := strings.TrimSpace(string(output))
	if ip == "" {
		return "", fmt.Errorf("no gateway IP found for network %s", networkName)
	}
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("invalid gateway IP %q for network %s", ip, networkName)
	}
	return ip, nil
}

// BridgeGatewayIPs returns the extra host addresses that the execution bridge
// should bind to in addition to 127.0.0.1.
//
// On Linux this includes the cooper-external and default bridge gateway IPs so
// barrels can reach the host bridge directly. On macOS Docker Desktop tunnels
// host.docker.internal to the host loopback, so no extra bind addresses are
// needed or valid.
func BridgeGatewayIPs() ([]string, error) {
	return bridgeGatewayIPsForOS(runtime.GOOS)
}

func bridgeGatewayIPsForOS(goos string) ([]string, error) {
	if goos == "darwin" {
		return nil, nil
	}

	var gatewayIPs []string
	for _, networkName := range []string{ExternalNetworkName(), "bridge"} {
		if ip, err := GetGatewayIP(networkName); err == nil {
			gatewayIPs = append(gatewayIPs, ip)
		}
	}
	gatewayIPs = uniqueStrings(gatewayIPs)
	if len(gatewayIPs) == 0 {
		return nil, fmt.Errorf("could not discover any Docker gateway IP")
	}
	return gatewayIPs, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// ConnectContainer connects a container to a Docker network.
func ConnectContainer(containerName, networkName string) error {
	cmd := exec.Command("docker", "network", "connect", networkName, containerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network connect %s %s failed: %w\n%s", networkName, containerName, err, string(output))
	}
	return nil
}

// DisconnectContainer disconnects a container from a Docker network.
func DisconnectContainer(containerName, networkName string) error {
	cmd := exec.Command("docker", "network", "disconnect", networkName, containerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network disconnect %s %s failed: %w\n%s", networkName, containerName, err, string(output))
	}
	return nil
}
