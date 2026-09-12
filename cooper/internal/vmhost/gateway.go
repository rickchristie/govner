// Package vmhost owns the networkless supervisor side of the VM control
// channel. It can reach the relay only through one private Unix socket.
package vmhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

const maxConcurrentStreams = 256

// Gateway links local Cooper clients and the guest yamux session.
type Gateway struct {
	GatewaySocket string
	ControlSocket string
	RelaySocket   string
	Nonce         string
	Logger        *log.Logger

	mu              sync.RWMutex
	session         *yamux.Session
	ready           chan struct{}
	once            sync.Once
	guestRequests   vmproto.ActiveRequests
	controlRequests vmproto.ActiveRequests
}

// NewGateway creates a gateway with an unopened ready signal.
func NewGateway(gatewaySocket, controlSocket, relaySocket, nonce string) *Gateway {
	return &Gateway{
		GatewaySocket: gatewaySocket,
		ControlSocket: controlSocket,
		RelaySocket:   relaySocket,
		Nonce:         nonce,
		ready:         make(chan struct{}),
	}
}

// Serve accepts one guest connection and local control clients until the
// context ends or the guest disconnects.
func (g *Gateway) Serve(ctx context.Context) error {
	if err := g.validate(); err != nil {
		return err
	}
	for _, path := range []string{g.GatewaySocket, g.ControlSocket} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create VM host socket directory: %w", err)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale VM host socket %s: %w", path, err)
		}
	}
	gatewayListener, err := net.Listen("unix", g.GatewaySocket)
	if err != nil {
		return fmt.Errorf("listen for VM guest: %w", err)
	}
	defer gatewayListener.Close()
	defer os.Remove(g.GatewaySocket)
	controlListener, err := net.Listen("unix", g.ControlSocket)
	if err != nil {
		return fmt.Errorf("listen for VM control: %w", err)
	}
	defer controlListener.Close()
	defer os.Remove(g.ControlSocket)
	for _, path := range []string{g.GatewaySocket, g.ControlSocket} {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("set VM host socket mode: %w", err)
		}
	}

	controlErrors := make(chan error, 1)
	go func() { controlErrors <- g.serveControl(ctx, controlListener) }()
	go func() {
		<-ctx.Done()
		gatewayListener.Close()
		controlListener.Close()
	}()

	connection, err := gatewayListener.Accept()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("accept VM guest: %w", err)
	}
	defer connection.Close()
	session, err := yamux.Server(connection, muxConfig())
	if err != nil {
		return fmt.Errorf("start VM host multiplexer: %w", err)
	}
	defer session.Close()
	g.mu.Lock()
	g.session = session
	g.mu.Unlock()

	streamErrors := make(chan error, 1)
	go func() { streamErrors <- g.acceptGuestStreams(ctx, session) }()
	select {
	case <-ctx.Done():
		return nil
	case err := <-controlErrors:
		return err
	case err := <-streamErrors:
		return err
	case <-session.CloseChan():
		if ctx.Err() != nil {
			return nil
		}
		return errors.New("VM guest control channel closed")
	}
}

func (g *Gateway) validate() error {
	for name, value := range map[string]string{
		"guest socket":   g.GatewaySocket,
		"control socket": g.ControlSocket,
		"relay socket":   g.RelaySocket,
		"nonce":          g.Nonce,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("VM host %s is required", name)
		}
	}
	return nil
}

func (g *Gateway) acceptGuestStreams(ctx context.Context, session *yamux.Session) error {
	semaphore := make(chan struct{}, maxConcurrentStreams)
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			if ctx.Err() != nil || session.IsClosed() {
				return nil
			}
			return fmt.Errorf("accept VM guest stream: %w", err)
		}
		select {
		case semaphore <- struct{}{}:
			go func() {
				defer func() { <-semaphore }()
				g.handleGuestStream(stream)
			}()
		default:
			stream.Close()
		}
	}
}

func (g *Gateway) handleGuestStream(stream net.Conn) {
	defer stream.Close()
	_ = stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	header, err := vmproto.ReadHeader(stream)
	if err != nil {
		g.log("reject guest header: %v", err)
		return
	}
	if header.Nonce != g.Nonce {
		g.log("reject guest stream %s: nonce mismatch", header.RequestID)
		return
	}
	if !g.guestRequests.Acquire(header.RequestID) {
		g.log("reject duplicate guest request %s", header.RequestID)
		return
	}
	defer g.guestRequests.Release(header.RequestID)
	_ = stream.SetReadDeadline(time.Time{})
	if header.Service == vmproto.ServiceHello {
		g.once.Do(func() { close(g.ready) })
		_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameStdout, Data: []byte("ready")})
		_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(0)})
		return
	}
	if header.Service == vmproto.ServiceDiagnostic {
		frame, readErr := vmproto.ReadFrame(stream)
		if readErr != nil || frame.Type != vmproto.FrameError || len(frame.Data) == 0 {
			g.log("reject invalid VM guest diagnostic")
			return
		}
		path := filepath.Join(filepath.Dir(g.ControlSocket), "guest-error.log")
		if err := os.WriteFile(path, append(frame.Data, '\n'), 0o600); err != nil {
			g.log("write VM guest diagnostic: %v", err)
			return
		}
		_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(0)})
		return
	}
	if !isRelayService(header.Service) {
		g.log("reject guest service %q", header.Service)
		return
	}
	relay, err := net.DialTimeout("unix", g.RelaySocket, 5*time.Second)
	if err != nil {
		g.log("connect VM relay: %v", err)
		return
	}
	defer relay.Close()
	if err := vmproto.WriteHeader(relay, header); err != nil {
		g.log("write VM relay header: %v", err)
		return
	}
	copyBoth(stream, relay)
}

func (g *Gateway) serveControl(ctx context.Context, listener net.Listener) error {
	semaphore := make(chan struct{}, maxConcurrentStreams)
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept VM control client: %w", err)
		}
		select {
		case semaphore <- struct{}{}:
			go func() {
				defer func() { <-semaphore }()
				g.handleControl(ctx, connection)
			}()
		default:
			connection.Close()
		}
	}
}

func (g *Gateway) handleControl(ctx context.Context, connection net.Conn) {
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	header, err := vmproto.ReadHeader(connection)
	if err != nil {
		return
	}
	if !g.controlRequests.Acquire(header.RequestID) {
		g.log("reject duplicate control request %s", header.RequestID)
		return
	}
	defer g.controlRequests.Release(header.RequestID)
	_ = connection.SetReadDeadline(time.Time{})
	if header.Service == vmproto.ServiceHealth {
		select {
		case <-g.ready:
		default:
			g.writeStartingHealth(connection)
			return
		}
	}
	if header.Service != vmproto.ServiceHealth && header.Service != vmproto.ServiceExec && header.Service != vmproto.ServiceReload && header.Service != vmproto.ServiceDoctor && header.Service != vmproto.ServiceShutdown {
		_ = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameError, Data: []byte("control service is not allowed")})
		return
	}
	select {
	case <-g.ready:
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Minute):
		_ = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameError, Data: []byte("VM guest is not ready")})
		return
	}
	session := g.currentSession()
	if session == nil || session.IsClosed() {
		_ = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameError, Data: []byte("VM guest is not connected")})
		return
	}
	stream, err := session.OpenStream()
	if err != nil {
		_ = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameError, Data: []byte("cannot open VM guest stream")})
		return
	}
	defer stream.Close()
	header.Nonce = g.Nonce
	if err := vmproto.WriteHeader(stream, header); err != nil {
		return
	}
	copyBoth(connection, stream)
}

func (g *Gateway) writeStartingHealth(connection io.Writer) {
	_ = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameError, Data: []byte("starting")})
	_ = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(1)})
}

func (g *Gateway) currentSession() *yamux.Session {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.session
}

func (g *Gateway) log(format string, values ...any) {
	if g.Logger != nil {
		g.Logger.Printf(format, values...)
	}
}

func isRelayService(service string) bool {
	if service == vmproto.ServiceProxy || service == vmproto.ServiceBridge {
		return true
	}
	value, ok := strings.CutPrefix(service, "forward:")
	if !ok {
		return false
	}
	port, err := strconv.Atoi(value)
	return err == nil && port >= 1 && port <= 65535
}

func muxConfig() *yamux.Config {
	config := yamux.DefaultConfig()
	config.AcceptBacklog = 64
	// QEMU connects the chardev before guest userspace opens the virtio port.
	// A yamux keepalive during that boot gap closes a healthy transport.
	config.EnableKeepAlive = false
	config.MaxStreamWindowSize = 256 * 1024
	config.KeepAliveInterval = 15 * time.Second
	config.ConnectionWriteTimeout = 15 * time.Second
	config.StreamOpenTimeout = 15 * time.Second
	config.StreamCloseTimeout = 30 * time.Second
	config.LogOutput = io.Discard
	return config
}

func copyBoth(left, right net.Conn) {
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, _ = io.Copy(left, right)
		closeWrite(left)
	}()
	go func() {
		defer wait.Done()
		_, _ = io.Copy(right, left)
		closeWrite(right)
	}()
	wait.Wait()
}

func closeWrite(connection net.Conn) {
	if closer, ok := connection.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
		return
	}
	_ = connection.Close()
}
