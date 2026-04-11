package docker

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func skipIfDarwinHostRelayNoOp(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "darwin" {
		t.Skip("HostRelay is intentionally a no-op on macOS")
	}
}

func TestHostRelay_LoopbackService(t *testing.T) {
	skipIfDarwinHostRelayNoOp(t)

	// Start a loopback-only HTTP server on a high port.
	const port = 18931
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "relay-test-ok")
	})
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("start test server: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	// Discover a gateway IP to test with.
	gwIP, err := GetGatewayIP("bridge")
	if err != nil {
		t.Skipf("no Docker bridge gateway: %v", err)
	}

	// Verify gateway cannot reach loopback service before relay.
	conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", gwIP, port), 500*time.Millisecond)
	if dialErr == nil {
		conn.Close()
		t.Skipf("gateway %s already reaches port %d (service may bind wider)", gwIP, port)
	}

	// Start relay.
	logger := log.New(io.Discard, "", 0)
	rules := []config.PortForwardRule{{ContainerPort: port, HostPort: port, Description: "test"}}
	hr := NewHostRelay([]string{gwIP}, logger)
	hr.Start(rules)
	defer hr.Stop()

	// Wait for lazy scan to activate the relay.
	var active int
	for i := 0; i < 20; i++ {
		active = hr.ActiveCount()
		if active > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if active == 0 {
		t.Fatal("relay did not activate within timeout")
	}

	// Verify gateway CAN now reach the service.
	resp, err := http.Get(fmt.Sprintf("http://%s:%d/", gwIP, port))
	if err != nil {
		t.Fatalf("gateway request failed after relay: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "relay-test-ok\n" {
		t.Errorf("unexpected response: %q", string(body))
	}
}

func TestHostRelay_TearsDownOnServiceStop(t *testing.T) {
	skipIfDarwinHostRelayNoOp(t)

	// Verify that when the loopback service stops and a client tries to
	// connect through the relay, the relay tears itself down immediately
	// (freeing the gateway IP for a wider bind).
	const port = 18936

	gwIP, err := GetGatewayIP("bridge")
	if err != nil {
		t.Skipf("no Docker bridge gateway: %v", err)
	}

	// Start a loopback-only server so the relay activates.
	loopbackLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("start loopback server: %v", err)
	}
	go func() {
		for {
			c, err := loopbackLn.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	logger := log.New(io.Discard, "", 0)
	rules := []config.PortForwardRule{{ContainerPort: port, HostPort: port, Description: "test"}}
	hr := NewHostRelay([]string{gwIP}, logger)
	hr.Start(rules)
	defer hr.Stop()

	// Wait for relay to activate.
	for i := 0; i < 20; i++ {
		if hr.ActiveCount() > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if hr.ActiveCount() == 0 {
		t.Fatal("relay did not activate")
	}

	// Stop the loopback service.
	loopbackLn.Close()

	// Trigger a connection through the relay — this will fail to reach
	// 127.0.0.1:{port} and cause immediate teardown.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", gwIP, port), 2*time.Second)
	if err == nil {
		conn.Close()
	}
	// Give teardown a moment.
	time.Sleep(200 * time.Millisecond)

	if hr.ActiveCount() != 0 {
		t.Error("relay should have torn down after failed connection to stopped service")
	}

	// Now 0.0.0.0 bind should succeed — the gateway IP is free.
	widerLn, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		t.Fatalf("binding 0.0.0.0:%d failed after relay teardown: %v", port, err)
	}
	widerLn.Close()
}

func TestHostRelay_SkipsWhenGatewayReachable(t *testing.T) {
	skipIfDarwinHostRelayNoOp(t)

	// Start a server on 0.0.0.0 — gateway should already reach it.
	const port = 18932
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		t.Fatalf("start test server: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	defer ln.Close()

	gwIP, err := GetGatewayIP("bridge")
	if err != nil {
		t.Skipf("no Docker bridge gateway: %v", err)
	}

	logger := log.New(io.Discard, "", 0)
	rules := []config.PortForwardRule{{ContainerPort: port, HostPort: port, Description: "test"}}
	hr := NewHostRelay([]string{gwIP}, logger)
	hr.Start(rules)
	defer hr.Stop()

	// The scan should detect that the gateway already reaches the service
	// and not create a relay.
	time.Sleep(1 * time.Second)
	if hr.ActiveCount() != 0 {
		t.Errorf("expected 0 active relays (service on 0.0.0.0), got %d", hr.ActiveCount())
	}
}

func TestHostRelay_RemovesWhenServiceStops(t *testing.T) {
	skipIfDarwinHostRelayNoOp(t)

	const port = 18933

	gwIP, err := GetGatewayIP("bridge")
	if err != nil {
		t.Skipf("no Docker bridge gateway: %v", err)
	}

	// Start a loopback-only server.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("start test server: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	logger := log.New(io.Discard, "", 0)
	rules := []config.PortForwardRule{{ContainerPort: port, HostPort: port, Description: "test"}}
	hr := NewHostRelay([]string{gwIP}, logger)
	hr.Start(rules)
	defer hr.Stop()

	// Wait for relay to activate.
	for i := 0; i < 20; i++ {
		if hr.ActiveCount() > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if hr.ActiveCount() == 0 {
		t.Fatal("relay did not activate")
	}

	// Stop the server.
	ln.Close()

	// Wait for scan to detect the service is gone and remove the relay.
	for i := 0; i < 20; i++ {
		if hr.ActiveCount() == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if hr.ActiveCount() != 0 {
		t.Error("relay was not removed after service stopped")
	}
}

func TestHostRelay_UpdatePorts(t *testing.T) {
	skipIfDarwinHostRelayNoOp(t)

	const port1 = 18934
	const port2 = 18935

	gwIP, err := GetGatewayIP("bridge")
	if err != nil {
		t.Skipf("no Docker bridge gateway: %v", err)
	}

	// Start servers on both ports.
	for _, port := range []int{port1, port2} {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("start test server on %d: %v", port, err)
		}
		defer ln.Close()
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
	}

	logger := log.New(io.Discard, "", 0)
	rules1 := []config.PortForwardRule{{ContainerPort: port1, HostPort: port1, Description: "test1"}}
	hr := NewHostRelay([]string{gwIP}, logger)
	hr.Start(rules1)
	defer hr.Stop()

	// Wait for relay on port1.
	for i := 0; i < 20; i++ {
		if hr.ActiveCount() > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if hr.ActiveCount() != 1 {
		t.Fatalf("expected 1 active relay, got %d", hr.ActiveCount())
	}

	// Update to include port2 and remove port1.
	rules2 := []config.PortForwardRule{{ContainerPort: port2, HostPort: port2, Description: "test2"}}
	hr.UpdatePorts(rules2)

	// Wait for scan to pick up port2 and remove port1.
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		hr.mu.Lock()
		_, has1 := hr.active[port1]
		_, has2 := hr.active[port2]
		hr.mu.Unlock()
		if !has1 && has2 {
			break
		}
	}

	hr.mu.Lock()
	_, has1 := hr.active[port1]
	_, has2 := hr.active[port2]
	hr.mu.Unlock()

	if has1 {
		t.Error("port1 relay should have been removed after UpdatePorts")
	}
	if !has2 {
		t.Error("port2 relay should have been created after UpdatePorts")
	}
}

func TestHostRelay_DarwinNoOp(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only behavior")
	}

	logger := log.New(io.Discard, "", 0)
	hr := NewHostRelay([]string{"172.17.0.1"}, logger)
	hr.Start([]config.PortForwardRule{{ContainerPort: 5432, HostPort: 5432, Description: "postgres"}})
	defer hr.Stop()

	if hr.ActiveCount() != 0 {
		t.Fatalf("HostRelay should stay inactive on macOS, got %d active relays", hr.ActiveCount())
	}
}
