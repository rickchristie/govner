package vmhost

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

func TestGatewayExecAndRelayPaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gatewaySocket := filepath.Join(root, "gateway.sock")
	controlSocket := filepath.Join(root, "control.sock")
	relaySocket := filepath.Join(root, "relay.sock")
	nonce := "test-nonce"
	startTestRelay(t, relaySocket)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	gateway := NewGateway(gatewaySocket, controlSocket, relaySocket, nonce)
	go func() { done <- gateway.Serve(ctx) }()
	waitForSocket(t, gatewaySocket)
	guestConnection, err := net.Dial("unix", gatewaySocket)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := yamux.Client(guestConnection, muxConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guest.Close() })

	hello, err := guest.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	helloHeader := vmproto.NewHeader(vmproto.ServiceHello, "hello")
	helloHeader.Nonce = nonce
	if err := vmproto.WriteHeader(hello, helloHeader); err != nil {
		t.Fatal(err)
	}
	if frame, err := vmproto.ReadFrame(hello); err != nil || string(frame.Data) != "ready" {
		t.Fatalf("hello response = %#v, %v", frame, err)
	}
	hello.Close()

	diagnostic, err := guest.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	diagnosticHeader := vmproto.NewHeader(vmproto.ServiceDiagnostic, "guest-failure")
	diagnosticHeader.Nonce = nonce
	if err := vmproto.WriteHeader(diagnostic, diagnosticHeader); err != nil {
		t.Fatal(err)
	}
	if err := vmproto.WriteFrame(diagnostic, vmproto.Frame{Type: vmproto.FrameError, Data: []byte("agent image has no disk space")}); err != nil {
		t.Fatal(err)
	}
	if frame, err := vmproto.ReadFrame(diagnostic); err != nil || frame.Type != vmproto.FrameExit {
		t.Fatalf("diagnostic acknowledgement = %#v, %v", frame, err)
	}
	diagnostic.Close()
	diagnosticData, err := os.ReadFile(filepath.Join(root, "guest-error.log"))
	if err != nil || strings.TrimSpace(string(diagnosticData)) != "agent image has no disk space" {
		t.Fatalf("saved guest diagnostic = %q, %v", diagnosticData, err)
	}

	execAccepted := make(chan vmproto.Header, 1)
	go func() {
		stream, acceptErr := guest.AcceptStream()
		if acceptErr != nil {
			return
		}
		defer stream.Close()
		header, readErr := vmproto.ReadHeader(stream)
		if readErr != nil {
			return
		}
		execAccepted <- header
		_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameStdout, Data: []byte("exec-ok")})
		_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(0)})
	}()

	control, err := net.Dial("unix", controlSocket)
	if err != nil {
		t.Fatal(err)
	}
	header := vmproto.NewHeader(vmproto.ServiceExec, "exec-1")
	header.Command = []string{"true"}
	if err := vmproto.WriteHeader(control, header); err != nil {
		t.Fatal(err)
	}
	frame, err := vmproto.ReadFrame(control)
	if err != nil || string(frame.Data) != "exec-ok" {
		t.Fatalf("exec frame = %#v, %v", frame, err)
	}
	if got := <-execAccepted; got.Nonce != nonce || got.Service != vmproto.ServiceExec {
		t.Fatalf("guest exec header = %#v", got)
	}
	control.Close()

	proxy, err := guest.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	proxyHeader := vmproto.NewHeader(vmproto.ServiceProxy, "proxy-1")
	proxyHeader.Nonce = nonce
	if err := vmproto.WriteHeader(proxy, proxyHeader); err != nil {
		t.Fatal(err)
	}
	if _, err := proxy.Write([]byte("relay-ok")); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("relay-ok"))
	if _, err := io.ReadFull(proxy, response); err != nil || string(response) != "relay-ok" {
		t.Fatalf("relay response = %q, %v", response, err)
	}
	proxy.Close()

	cancel()
	guest.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not stop")
	}
}

func TestGatewayRejectsWrongNonceAndUnknownGuestService(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gatewaySocket := filepath.Join(root, "gateway.sock")
	controlSocket := filepath.Join(root, "control.sock")
	relaySocket := filepath.Join(root, "relay.sock")
	relayListener, err := net.Listen("unix", relaySocket)
	if err != nil {
		t.Fatal(err)
	}
	defer relayListener.Close()
	relayAccepted := make(chan struct{}, 1)
	go func() {
		connection, acceptErr := relayListener.Accept()
		if acceptErr == nil {
			connection.Close()
			relayAccepted <- struct{}{}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	gateway := NewGateway(gatewaySocket, controlSocket, relaySocket, "correct-nonce")
	go func() { done <- gateway.Serve(ctx) }()
	waitForSocket(t, gatewaySocket)
	guestConnection, err := net.Dial("unix", gatewaySocket)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := yamux.Client(guestConnection, muxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer guest.Close()

	wrongNonce, err := guest.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	wrongHeader := vmproto.NewHeader(vmproto.ServiceProxy, "wrong-nonce")
	wrongHeader.Nonce = "wrong"
	if err := vmproto.WriteHeader(wrongNonce, wrongHeader); err != nil {
		t.Fatal(err)
	}
	_ = wrongNonce.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := vmproto.ReadFrame(wrongNonce); err == nil {
		t.Fatal("gateway accepted a guest stream with the wrong nonce")
	}
	wrongNonce.Close()

	unknown, err := guest.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	unknownHeader := vmproto.NewHeader("host:22", "unknown-service")
	unknownHeader.Nonce = "correct-nonce"
	if err := vmproto.WriteHeader(unknown, unknownHeader); err != nil {
		t.Fatal(err)
	}
	_ = unknown.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := unknown.Read(make([]byte, 1)); err == nil {
		t.Fatal("gateway kept an unknown guest service open")
	}
	unknown.Close()

	invalidForward, err := guest.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	forwardHeader := vmproto.NewHeader("forward:not-a-port", "invalid-forward")
	forwardHeader.Nonce = "correct-nonce"
	if err := vmproto.WriteHeader(invalidForward, forwardHeader); err != nil {
		t.Fatal(err)
	}
	_ = invalidForward.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := invalidForward.Read(make([]byte, 1)); err == nil {
		t.Fatal("gateway kept an invalid forward service open")
	}
	invalidForward.Close()

	select {
	case <-relayAccepted:
		t.Fatal("gateway sent a rejected guest stream to the network relay")
	case <-time.After(100 * time.Millisecond):
	}
	if !isRelayService("forward:8080") || isRelayService("forward:0") || isRelayService("forward:70000") || isRelayService("host:22") {
		t.Fatal("relay service classification is unsafe")
	}

	cancel()
	guest.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not stop")
	}
}

func TestMuxKeepAliveIsDisabledForGuestBootGap(t *testing.T) {
	t.Parallel()
	if muxConfig().EnableKeepAlive {
		t.Fatal("yamux keepalive can close the QEMU chardev before guest userspace starts")
	}
}

func startTestRelay(t *testing.T, path string) {
	t.Helper()
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer connection.Close()
				if _, readErr := vmproto.ReadHeader(connection); readErr != nil {
					return
				}
				_, _ = io.Copy(connection, connection)
			}()
		}
	}()
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s did not become ready", path)
}
