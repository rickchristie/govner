package bridge

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rickchristie/govner/cooper/internal/config"
)

// ExecutionLog captures the result of a single bridge script execution.
// Sent on the log channel for the TUI to display.
type ExecutionLog struct {
	Timestamp  time.Time
	Route      string
	ScriptPath string
	ExitCode   int
	Stdout     string
	Stderr     string
	Duration   time.Duration
	Error      string
}

// BridgeServer is the execution bridge HTTP API server. It binds to
// specific addresses (localhost + Docker gateway IP) and dispatches
// incoming requests to configured script routes.
type BridgeServer struct {
	routes    []config.BridgeRoute
	bindAddrs []string
	logCh     chan ExecutionLog
	servers   []*http.Server

	mu sync.RWMutex
}

// NewBridgeServer creates a BridgeServer that will bind to 127.0.0.1:{port}
// plus each provided gateway IP on the same port. It does NOT bind to 0.0.0.0
// — only the explicit addresses are used. Multiple gateway IPs are needed
// because host.docker.internal resolves to the default bridge gateway, which
// may differ from the cooper-external network gateway.
func NewBridgeServer(routes []config.BridgeRoute, port int, gatewayIPs []string) *BridgeServer {
	addrs := []string{
		fmt.Sprintf("127.0.0.1:%d", port),
	}
	seen := map[string]bool{"127.0.0.1": true}
	for _, ip := range gatewayIPs {
		if ip != "" && !seen[ip] {
			seen[ip] = true
			addrs = append(addrs, fmt.Sprintf("%s:%d", ip, port))
		}
	}

	return &BridgeServer{
		routes:    routes,
		bindAddrs: addrs,
		logCh:     make(chan ExecutionLog, 256),
	}
}

// Start launches HTTP listeners on each configured bind address. All
// listeners share the same handler (the BridgeServer itself). If any
// listener fails to bind, all already-started listeners are shut down
// and an error is returned.
func (s *BridgeServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.servers = nil
	var listeners []net.Listener

	// Pre-bind all addresses so we fail fast before starting goroutines.
	for _, addr := range s.bindAddrs {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			// Clean up listeners already opened.
			for _, l := range listeners {
				l.Close()
			}
			return fmt.Errorf("bridge: failed to listen on %s: %w", addr, err)
		}
		listeners = append(listeners, ln)
	}

	// Start serving on each listener.
	for _, ln := range listeners {
		srv := &http.Server{
			Handler: s,
		}
		s.servers = append(s.servers, srv)
		go srv.Serve(ln)
	}

	return nil
}

// Stop gracefully shuts down all HTTP listeners with a 5-second timeout,
// then closes the log channel so the forwarding goroutine in main.go exits.
func (s *BridgeServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var firstErr error
	for _, srv := range s.servers {
		if err := srv.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	s.servers = nil

	// Close the log channel so consumers (e.g. the forwarding goroutine
	// that ranges over LogChan()) exit cleanly instead of leaking.
	close(s.logCh)

	return firstErr
}

// LogChan returns a read-only channel that receives execution logs.
// The TUI consumes this to display bridge activity.
func (s *BridgeServer) LogChan() <-chan ExecutionLog {
	return s.logCh
}

// UpdateRoutes hot-swaps the configured routes while the server is
// running. The new routes take effect on the next incoming request.
func (s *BridgeServer) UpdateRoutes(routes []config.BridgeRoute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes = make([]config.BridgeRoute, len(routes))
	copy(s.routes, routes)
}

// getRoutes returns a snapshot of the current routes (safe for reads).
func (s *BridgeServer) getRoutes() []config.BridgeRoute {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]config.BridgeRoute, len(s.routes))
	copy(out, s.routes)
	return out
}
