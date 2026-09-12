package vmguest

import (
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

func TestLocalRelayReloadAddsAndRemovesListeners(t *testing.T) {
	t.Parallel()
	proxyPort := reserveTCPPort(t)
	bridgePort := reserveTCPPort(t)
	oldPort := reserveTCPPort(t)
	newPort := reserveTCPPort(t)

	hostTransport, guestTransport := net.Pipe()
	hostSession, err := yamux.Server(hostTransport, testMuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	guestSession, err := yamux.Client(guestTransport, testMuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		hostSession.Close()
		guestSession.Close()
	})
	go func() {
		for {
			stream, err := hostSession.AcceptStream()
			if err != nil {
				return
			}
			_, _ = vmproto.ReadHeader(stream)
			_ = stream.Close()
		}
	}()

	manifest := vmproto.Manifest{
		Nonce: "test", ControlGateway: "127.0.0.1",
		ProxyPort: proxyPort, BridgePort: bridgePort,
		ForwardPorts: []vmproto.PortForward{{Port: oldPort}},
	}
	relay := newLocalRelay(guestSession, manifest)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.serve(ctx) }()
	waitForTCPListener(t, oldPort)
	if err := relay.reload([]int{newPort}); err != nil {
		t.Fatalf("reload() failed: %v", err)
	}
	waitForTCPListener(t, newPort)
	waitForTCPClosed(t, oldPort)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func reserveTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForTCPListener(t *testing.T, port int) {
	t.Helper()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp4", address, 50*time.Millisecond)
		if err == nil {
			_, _ = io.WriteString(connection, "test")
			_ = connection.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("listener %s did not start", address)
}

func waitForTCPClosed(t *testing.T, port int) {
	t.Helper()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp4", address, 50*time.Millisecond)
		if err != nil {
			return
		}
		_ = connection.Close()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("listener %s did not stop", address)
}

func testMuxConfig() *yamux.Config {
	config := yamux.DefaultConfig()
	config.EnableKeepAlive = false
	config.LogOutput = io.Discard
	return config
}
