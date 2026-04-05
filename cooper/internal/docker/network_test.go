package docker

import (
	"os/exec"
	"strings"
	"testing"
)

// These tests require Docker and will create/remove real Docker networks.
// They run in the default `go test` flow.

func removeNetworkIfExists(name string) {
	_ = exec.Command("docker", "network", "rm", name).Run()
}

func TestEnsureNetworks(t *testing.T) {
	externalName := ExternalNetworkName()
	internalName := InternalNetworkName()

	// Clean up before and after.
	removeNetworkIfExists(externalName)
	removeNetworkIfExists(internalName)
	t.Cleanup(func() {
		removeNetworkIfExists(externalName)
		removeNetworkIfExists(internalName)
	})

	if err := EnsureNetworks(); err != nil {
		t.Fatalf("EnsureNetworks() failed: %v", err)
	}

	// Verify both networks exist.
	for _, name := range []string{externalName, internalName} {
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
	externalName := ExternalNetworkName()
	removeNetworkIfExists(externalName)
	t.Cleanup(func() {
		removeNetworkIfExists(externalName)
	})

	cmd := exec.Command("docker", "network", "create", externalName)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test network: %v\n%s", err, string(output))
	}

	exists, err = NetworkExists(externalName)
	if err != nil {
		t.Fatalf("NetworkExists(%q) error: %v", externalName, err)
	}
	if !exists {
		t.Errorf("NetworkExists(%q) = false, want true", externalName)
	}
}

func TestGetGatewayIP(t *testing.T) {
	// Create the external network (regular bridge, should have a gateway).
	externalName := ExternalNetworkName()
	removeNetworkIfExists(externalName)
	t.Cleanup(func() {
		removeNetworkIfExists(externalName)
	})

	cmd := exec.Command("docker", "network", "create", externalName)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test network: %v\n%s", err, string(output))
	}

	ip, err := GetGatewayIP(externalName)
	if err != nil {
		t.Fatalf("GetGatewayIP(%q) error: %v", externalName, err)
	}
	if ip == "" {
		t.Fatal("GetGatewayIP() returned empty string")
	}
	// Sanity check: should look like an IP address.
	if !strings.Contains(ip, ".") {
		t.Errorf("GetGatewayIP() = %q, does not look like an IPv4 address", ip)
	}
}

func TestInternalNetworkIsMarkedInternal(t *testing.T) {
	internalName := InternalNetworkName()
	removeNetworkIfExists(internalName)
	t.Cleanup(func() {
		removeNetworkIfExists(internalName)
	})

	cmd := exec.Command("docker", "network", "create", "--internal", internalName)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test network: %v\n%s", err, string(output))
	}

	cmd = exec.Command("docker", "network", "inspect", "--format", "{{.Internal}}", internalName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to inspect internal flag: %v\n%s", err, string(output))
	}
	if strings.TrimSpace(string(output)) != "true" {
		t.Fatalf("network %s internal flag = %q, want true", internalName, strings.TrimSpace(string(output)))
	}
}

func TestRemoveNetworks(t *testing.T) {
	externalName := ExternalNetworkName()
	internalName := InternalNetworkName()

	// Create both networks first.
	removeNetworkIfExists(externalName)
	removeNetworkIfExists(internalName)

	for _, args := range [][]string{
		{"network", "create", externalName},
		{"network", "create", "--internal", internalName},
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
	for _, name := range []string{externalName, internalName} {
		exists, err := NetworkExists(name)
		if err != nil {
			t.Fatalf("NetworkExists(%q) error: %v", name, err)
		}
		if exists {
			t.Errorf("NetworkExists(%q) = true after RemoveNetworks(), want false", name)
		}
	}
}
