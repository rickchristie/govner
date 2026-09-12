package vmrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

var relayTestRequestID atomic.Uint64

func TestRelayForwardsOnlyNamedServices(t *testing.T) {
	t.Parallel()
	target := startEchoServer(t)
	_, portText, _ := net.SplitHostPort(target.Addr().String())
	port, _ := net.LookupPort("tcp", portText)
	root := t.TempDir()
	policyPath := filepath.Join(root, "policy.json")
	writePolicy(t, policyPath, Policy{ProxyHost: "127.0.0.1", ProxyPort: port, BridgePort: port, Forwards: []int{port}})
	socket := filepath.Join(root, "relay.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (Server{SocketPath: socket, PolicyPath: policyPath}).Serve(ctx) }()
	waitForSocket(t, socket)

	for index, service := range []string{vmproto.ServiceProxy, vmproto.ServiceBridge, "forward:" + portText} {
		connection, err := net.Dial("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		if err := vmproto.WriteHeader(connection, vmproto.NewHeader(service, "request-"+strconv.Itoa(index))); err != nil {
			t.Fatal(err)
		}
		if _, err := connection.Write([]byte("ping")); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, 4)
		if _, err := io.ReadFull(connection, got); err != nil || string(got) != "ping" {
			t.Fatalf("service %q response = %q, %v", service, got, err)
		}
		connection.Close()
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRelayRejectsUnknownAndDisabledDestinations(t *testing.T) {
	t.Parallel()
	policy := Policy{ProxyHost: "proxy", ProxyPort: 3128, BridgePort: 4343, ForwardPorts: map[int]bool{8080: true}}
	for _, service := range []string{"host:22", "forward:22", "forward:not-a-port", ""} {
		if _, err := policy.destination(service); err == nil {
			t.Fatalf("destination(%q) succeeded", service)
		}
	}
}

func TestLoadPolicyRejectsUnknownFieldsAndPorts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	unknown := filepath.Join(root, "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"proxy_host":"proxy","proxy_port":3128,"bridge_port":4343,"unknown":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicy(unknown); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("LoadPolicy() error = %v", err)
	}
	bad := filepath.Join(root, "bad.json")
	writePolicy(t, bad, Policy{ProxyHost: "proxy", ProxyPort: 0, BridgePort: 4343})
	if _, err := LoadPolicy(bad); err == nil {
		t.Fatal("LoadPolicy() accepted port 0")
	}
}

func TestServiceLimitsKeepCapacityForEachServiceClass(t *testing.T) {
	t.Parallel()
	limits := &serviceLimits{active: make(map[string]int)}
	for index := 0; index < maxBridgeConnections; index++ {
		if !limits.acquire(vmproto.ServiceBridge) {
			t.Fatalf("bridge connection %d was rejected before its quota", index)
		}
	}
	if limits.acquire(vmproto.ServiceBridge) {
		t.Fatal("bridge quota accepted one excess connection")
	}
	if !limits.acquire(vmproto.ServiceProxy) || !limits.acquire("forward:8080") {
		t.Fatal("one full service class blocked a different service class")
	}
	limits.release(vmproto.ServiceBridge)
	if !limits.acquire(vmproto.ServiceBridge) {
		t.Fatal("released bridge quota was not reusable")
	}
}

func TestRelayLogsPolicyDecisionsWithoutPayload(t *testing.T) {
	t.Parallel()
	target := startEchoServer(t)
	root := t.TempDir()
	policyPath := filepath.Join(root, "policy.json")
	writePolicy(t, policyPath, Policy{ProxyHost: "127.0.0.1", ProxyPort: listenerPort(t, target), BridgePort: listenerPort(t, target)})
	socket := filepath.Join(root, "relay.sock")
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- (Server{SocketPath: socket, PolicyPath: policyPath, Logger: log.New(&logs, "", 0)}).Serve(ctx)
	}()
	waitForSocket(t, socket)
	connection, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := vmproto.WriteHeader(connection, vmproto.NewHeader(vmproto.ServiceProxy, "logged-request")); err != nil {
		t.Fatal(err)
	}
	secretPayload := "payload-must-not-enter-the-log"
	if _, err := connection.Write([]byte(secretPayload)); err != nil {
		t.Fatal(err)
	}
	echo := make([]byte, len(secretPayload))
	if _, err := io.ReadFull(connection, echo); err != nil {
		t.Fatal(err)
	}
	if string(echo) != secretPayload {
		t.Fatalf("relay echo = %q, want %q", echo, secretPayload)
	}
	connection.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `allow service "proxy"`) {
		t.Fatalf("relay log has no allow decision: %q", logs.String())
	}
	if strings.Contains(logs.String(), secretPayload) {
		t.Fatalf("relay log contains stream payload: %q", logs.String())
	}
}

func TestRelayReadsAnAtomicPolicyReplacementForEachStream(t *testing.T) {
	t.Parallel()
	first := startEchoServer(t)
	second := startEchoServer(t)
	firstPort := listenerPort(t, first)
	secondPort := listenerPort(t, second)
	root := t.TempDir()
	policyPath := filepath.Join(root, "policy.json")
	writePolicy(t, policyPath, Policy{ProxyHost: "127.0.0.1", ProxyPort: firstPort, BridgePort: firstPort, Forwards: []int{firstPort}})
	socket := filepath.Join(root, "relay.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (Server{SocketPath: socket, PolicyPath: policyPath}).Serve(ctx) }()
	waitForSocket(t, socket)
	assertRelayEcho(t, socket, "forward:"+strconv.Itoa(firstPort), true)

	temporary := filepath.Join(root, "policy.next")
	writePolicy(t, temporary, Policy{ProxyHost: "127.0.0.1", ProxyPort: secondPort, BridgePort: secondPort, Forwards: []int{secondPort}})
	if err := os.Rename(temporary, policyPath); err != nil {
		t.Fatal(err)
	}
	assertRelayEcho(t, socket, "forward:"+strconv.Itoa(firstPort), false)
	assertRelayEcho(t, socket, "forward:"+strconv.Itoa(secondPort), true)

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func listenerPort(t *testing.T, listener net.Listener) int {
	t.Helper()
	return listener.Addr().(*net.TCPAddr).Port
}

func assertRelayEcho(t *testing.T, socket, service string, wantEcho bool) {
	t.Helper()
	connection, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	requestID := "policy-reload-" + strconv.FormatUint(relayTestRequestID.Add(1), 10)
	if err := vmproto.WriteHeader(connection, vmproto.NewHeader(service, requestID)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte("ping")); err != nil {
		if !wantEcho {
			return
		}
		t.Fatal(err)
	}
	got := make([]byte, 4)
	_, err = io.ReadFull(connection, got)
	if wantEcho && (err != nil || string(got) != "ping") {
		t.Fatalf("service %s response = %q, %v", service, got, err)
	}
	if !wantEcho && err == nil {
		t.Fatalf("disabled service %s was forwarded", service)
	}
}

func startEchoServer(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
			}()
		}
	}()
	return listener
}

func writePolicy(t *testing.T, path string, policy Policy) {
	t.Helper()
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear", path)
}
