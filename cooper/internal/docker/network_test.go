//go:build integration

package docker

import (
	"os/exec"
	"strings"
	"testing"
)

// These tests require Docker and will create/remove real Docker networks.
// Run with: go test -tags=integration ./internal/docker/...

func removeNetworkIfExists(name string) {
	_ = exec.Command("docker", "network", "rm", name).Run()
}

func TestEnsureNetworks(t *testing.T) {
	// Clean up before and after.
	removeNetworkIfExists(NetworkExternal)
	removeNetworkIfExists(NetworkInternal)
	t.Cleanup(func() {
		removeNetworkIfExists(NetworkExternal)
		removeNetworkIfExists(NetworkInternal)
	})

	if err := EnsureNetworks(); err != nil {
		t.Fatalf("EnsureNetworks() failed: %v", err)
	}

	// Verify both networks exist.
	for _, name := range []string{NetworkExternal, NetworkInternal} {
		exists, err := NetworkExists(name)
		if err != nil {
			t.Fatalf("NetworkExists(%q) error: %v", name, err)
		}
		if !exists {
			t.Errorf("NetworkExists(%q) = false, want true", name)
		}
	}

	// Calling EnsureNetworks again should be idempotent.
	if err := EnsureNetworks(); err != nil {
		t.Fatalf("EnsureNetworks() second call failed: %v", err)
	}
}

func TestNetworkExists(t *testing.T) {
	// A network that definitely does not exist.
	exists, err := NetworkExists("cooper-nonexistent-test-network")
	if err != nil {
		t.Fatalf("NetworkExists() error: %v", err)
	}
	if exists {
		t.Error("NetworkExists() = true for non-existent network, want false")
	}

	// Create a network and verify it exists.
	removeNetworkIfExists(NetworkExternal)
	t.Cleanup(func() {
		removeNetworkIfExists(NetworkExternal)
	})

	cmd := exec.Command("docker", "network", "create", NetworkExternal)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test network: %v\n%s", err, string(output))
	}

	exists, err = NetworkExists(NetworkExternal)
	if err != nil {
		t.Fatalf("NetworkExists(%q) error: %v", NetworkExternal, err)
	}
	if !exists {
		t.Errorf("NetworkExists(%q) = false, want true", NetworkExternal)
	}
}

func TestGetGatewayIP(t *testing.T) {
	// Create the external network (regular bridge, should have a gateway).
	removeNetworkIfExists(NetworkExternal)
	t.Cleanup(func() {
		removeNetworkIfExists(NetworkExternal)
	})

	cmd := exec.Command("docker", "network", "create", NetworkExternal)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test network: %v\n%s", err, string(output))
	}

	ip, err := GetGatewayIP(NetworkExternal)
	if err != nil {
		t.Fatalf("GetGatewayIP(%q) error: %v", NetworkExternal, err)
	}
	if ip == "" {
		t.Fatal("GetGatewayIP() returned empty string")
	}
	// Sanity check: should look like an IP address.
	if !strings.Contains(ip, ".") {
		t.Errorf("GetGatewayIP() = %q, does not look like an IPv4 address", ip)
	}
}

func TestGetGatewayIP_InternalHasNoGateway(t *testing.T) {
	// The --internal network should have no gateway.
	removeNetworkIfExists(NetworkInternal)
	t.Cleanup(func() {
		removeNetworkIfExists(NetworkInternal)
	})

	cmd := exec.Command("docker", "network", "create", "--internal", NetworkInternal)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test network: %v\n%s", err, string(output))
	}

	_, err := GetGatewayIP(NetworkInternal)
	if err == nil {
		t.Error("GetGatewayIP() on --internal network should return error (no gateway), got nil")
	}
}

func TestRemoveNetworks(t *testing.T) {
	// Create both networks first.
	removeNetworkIfExists(NetworkExternal)
	removeNetworkIfExists(NetworkInternal)

	for _, args := range [][]string{
		{"network", "create", NetworkExternal},
		{"network", "create", "--internal", NetworkInternal},
	} {
		cmd := exec.Command("docker", args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to create network: %v\n%s", err, string(output))
		}
	}

	if err := RemoveNetworks(); err != nil {
		t.Fatalf("RemoveNetworks() failed: %v", err)
	}

	// Verify both are gone.
	for _, name := range []string{NetworkExternal, NetworkInternal} {
		exists, err := NetworkExists(name)
		if err != nil {
			t.Fatalf("NetworkExists(%q) error: %v", name, err)
		}
		if exists {
			t.Errorf("NetworkExists(%q) = true after RemoveNetworks(), want false", name)
		}
	}
}
