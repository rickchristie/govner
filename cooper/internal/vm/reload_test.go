package vm

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/vmproto"
	"github.com/rickchristie/govner/cooper/internal/vmrelay"
)

func TestReloadPortForwardsUpdatesPolicyAndGuest(t *testing.T) {
	t.Parallel()
	cooperDir := t.TempDir()
	id := "unit-vm-project-codex-aabbccddeeff"
	runtimeDir := RuntimeDir(cooperDir, id)
	if err := os.MkdirAll(filepath.Join(runtimeDir, "live"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := vmrelay.Policy{ProxyHost: "unit-proxy", ProxyPort: 3128, BridgePort: 4343, Forwards: []int{7000}}
	if err := writeJSON(relayPolicyPath(runtimeDir), old, 0o444); err != nil {
		t.Fatal(err)
	}
	controlDir := ControlDir(cooperDir, id)
	if err := os.MkdirAll(controlDir, 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", ControlSocketPath(cooperDir, id))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	received := make(chan []int, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		header, err := vmproto.ReadHeader(connection)
		if err != nil {
			return
		}
		received <- header.ForwardPorts
		_ = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(0)})
	}()
	runner := &recordingRunner{output: func(command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "docker ps -a"):
			return []byte(id + "\n"), nil
		case strings.Contains(command, "docker network ls"):
			return nil, nil
		case strings.Contains(command, "docker inspect --format"):
			return []byte("true\n"), nil
		default:
			return nil, errors.New("unexpected command: " + command)
		}
	}}
	manager := Manager{CooperDir: cooperDir, Namespace: "unit", Runner: runner}
	rules := []config.PortForwardRule{{ContainerPort: 8000}, {ContainerPort: 9000, IsRange: true, RangeEnd: 9001}}
	if err := manager.ReloadPortForwards(context.Background(), rules); err != nil {
		t.Fatal(err)
	}
	want := []int{8000, 9000, 9001}
	if got := <-received; !slices.Equal(got, want) {
		t.Fatalf("guest ports = %v, want %v", got, want)
	}
	policy, err := vmrelay.LoadPolicy(relayPolicyPath(runtimeDir))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(policy.Forwards, want) {
		t.Fatalf("policy ports = %v, want %v", policy.Forwards, want)
	}
}
