package vmguest

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/hashicorp/yamux"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

type localRelay struct {
	session             *yamux.Session
	nonce               string
	host                string
	proxyPort           int
	bridgePort          int
	initialForwardPorts []int
	nextID              atomic.Uint64

	mu        sync.Mutex
	context   context.Context
	listeners map[int]net.Listener
	streams   chan struct{}
}

func newLocalRelay(session *yamux.Session, manifest vmproto.Manifest) *localRelay {
	forwardPorts := make([]int, 0, len(manifest.ForwardPorts))
	for _, forward := range manifest.ForwardPorts {
		forwardPorts = append(forwardPorts, forward.Port)
	}
	return &localRelay{
		session: session, nonce: manifest.Nonce, host: manifest.ControlGateway,
		proxyPort: manifest.ProxyPort, bridgePort: manifest.BridgePort, initialForwardPorts: forwardPorts,
		listeners: make(map[int]net.Listener), streams: make(chan struct{}, 256),
	}
}

func (r *localRelay) serve(ctx context.Context) error {
	r.mu.Lock()
	r.context = ctx
	r.mu.Unlock()
	if err := r.reload(r.initialForwardPorts); err != nil {
		return err
	}
	<-ctx.Done()
	r.mu.Lock()
	for port, listener := range r.listeners {
		_ = listener.Close()
		delete(r.listeners, port)
	}
	r.context = nil
	r.mu.Unlock()
	return nil
}

// reload changes only the enabled forward ports. Fixed proxy and bridge
// listeners stay present. It opens all new listeners before it closes removed
// listeners, so a failed update keeps the last complete policy.
func (r *localRelay) reload(forwardPorts []int) error {
	desired := map[int]string{
		r.proxyPort:  vmproto.ServiceProxy,
		r.bridgePort: vmproto.ServiceBridge,
	}
	for _, port := range forwardPorts {
		if port < 1 || port > 65535 {
			return fmt.Errorf("VM relay forward port %d is invalid", port)
		}
		if _, reserved := desired[port]; reserved {
			return fmt.Errorf("VM relay forward port %d is reserved", port)
		}
		desired[port] = "forward:" + strconv.Itoa(port)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.context == nil {
		return fmt.Errorf("VM relay is not running")
	}
	opened := make(map[int]net.Listener)
	for port, service := range desired {
		if _, exists := r.listeners[port]; exists {
			continue
		}
		listener, err := net.Listen("tcp4", net.JoinHostPort(r.host, strconv.Itoa(port)))
		if err != nil {
			for _, pending := range opened {
				_ = pending.Close()
			}
			return fmt.Errorf("listen for VM %s relay: %w", service, err)
		}
		opened[port] = listener
	}
	for port, listener := range r.listeners {
		if _, keep := desired[port]; keep {
			continue
		}
		_ = listener.Close()
		delete(r.listeners, port)
	}
	for port, listener := range opened {
		r.listeners[port] = listener
		go r.accept(r.context, listener, desired[port])
	}
	return nil
}

func (r *localRelay) accept(ctx context.Context, listener net.Listener, service string) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		select {
		case r.streams <- struct{}{}:
			go func() {
				defer func() { <-r.streams }()
				r.forward(ctx, connection, service)
			}()
		default:
			_ = connection.Close()
		}
	}
}

func (r *localRelay) forward(ctx context.Context, connection net.Conn, service string) {
	defer connection.Close()
	stream, err := r.session.OpenStream()
	if err != nil {
		return
	}
	defer stream.Close()
	header := vmproto.NewHeader(service, fmt.Sprintf("relay-%d", r.nextID.Add(1)))
	header.Nonce = r.nonce
	if err := vmproto.WriteHeader(stream, header); err != nil {
		return
	}
	copyConnections(connection, stream)
}

func copyConnections(left, right net.Conn) {
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, _ = io.Copy(left, right)
		closeConnectionWrite(left)
	}()
	go func() {
		defer wait.Done()
		_, _ = io.Copy(right, left)
		closeConnectionWrite(right)
	}()
	wait.Wait()
}

func closeConnectionWrite(connection net.Conn) {
	if closer, ok := connection.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
		return
	}
	_ = connection.Close()
}
