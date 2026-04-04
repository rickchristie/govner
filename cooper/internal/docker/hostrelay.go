package docker

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/rickchristie/govner/cooper/internal/config"
)

// HostRelay manages lazy TCP relays on the host that forward connections
// from the Docker gateway IP to 127.0.0.1. This bridges the gap for host
// services that bind only to loopback — Docker containers reach
// host.docker.internal (the gateway IP), which can't reach 127.0.0.1.
//
// The relay is lazy: it periodically scans forwarded ports and only creates
// a relay when a loopback-only service is detected (127.0.0.1:{port} is
// listening but {gatewayIP}:{port} is not). When the service stops, the
// relay is removed either by the next scan (~3s) or immediately when a
// proxied connection fails to reach the destination. In the uncommon case
// where a service stops and restarts on 0.0.0.0 within one scan interval
// with no traffic in between, the wider bind may briefly conflict with the
// relay listener until the next scan clears it.
type HostRelay struct {
	mu         sync.Mutex
	gatewayIPs []string
	ports      []int // all forwarded host ports (expanded from rules)
	active     map[int]relayEntry // port → listeners
	stopCh     chan struct{}
	logger     *log.Logger
}

// NewHostRelay creates a host relay manager. Call Start to begin the
// periodic scan loop and UpdatePorts when rules change.
func NewHostRelay(gatewayIPs []string, logger *log.Logger) *HostRelay {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &HostRelay{
		gatewayIPs: gatewayIPs,
		active:     make(map[int]relayEntry),
		stopCh:     make(chan struct{}),
		logger:     logger,
	}
}

// Start begins the periodic scan loop that creates/removes relays as needed.
func (hr *HostRelay) Start(rules []config.PortForwardRule) {
	hr.mu.Lock()
	hr.ports = expandPorts(rules)
	hr.mu.Unlock()

	// Run an immediate scan, then periodically.
	hr.scan()
	go hr.loop()
}

// UpdatePorts updates the forwarded port list and triggers an immediate rescan.
// Relays for removed ports are torn down; new ports are picked up.
func (hr *HostRelay) UpdatePorts(rules []config.PortForwardRule) {
	hr.mu.Lock()
	hr.ports = expandPorts(rules)
	hr.mu.Unlock()
	hr.scan()
}

// Stop closes all relay listeners and stops the scan loop.
func (hr *HostRelay) Stop() {
	close(hr.stopCh)
	hr.mu.Lock()
	defer hr.mu.Unlock()
	for port, entry := range hr.active {
		for _, ln := range entry.listeners {
			ln.Close()
		}
		delete(hr.active, port)
	}
}

// ActiveCount returns the number of active relay listeners.
func (hr *HostRelay) ActiveCount() int {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	return len(hr.active)
}

func (hr *HostRelay) loop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-hr.stopCh:
			return
		case <-ticker.C:
			hr.scan()
		}
	}
}

// scan checks each forwarded port and creates/removes relays as needed.
func (hr *HostRelay) scan() {
	hr.mu.Lock()
	currentPorts := make([]int, len(hr.ports))
	copy(currentPorts, hr.ports)
	hr.mu.Unlock()

	// Build set of desired ports for quick lookup.
	desired := make(map[int]bool, len(currentPorts))
	for _, p := range currentPorts {
		desired[p] = true
	}

	hr.mu.Lock()
	defer hr.mu.Unlock()

	// Remove relays for ports no longer in the forwarding rules.
	for port, entry := range hr.active {
		if !desired[port] {
			for _, ln := range entry.listeners {
				ln.Close()
			}
			delete(hr.active, port)
			hr.logger.Printf("[host-relay] removed relay for port %d (rule removed)", port)
		}
	}

	// For each desired port, decide whether a relay is needed.
	for _, port := range currentPorts {
		loopbackUp := isListening("127.0.0.1", port)
		_, alreadyRelaying := hr.active[port]

		if !loopbackUp {
			// No loopback service — remove relay if active.
			if alreadyRelaying {
				entry := hr.active[port]
				for _, ln := range entry.listeners {
					ln.Close()
				}
				delete(hr.active, port)
				hr.logger.Printf("[host-relay] removed relay for port %d (service stopped)", port)
			}
			continue
		}

		if alreadyRelaying {
			// Relay is active and loopback service is still up — keep it.
			// A service cannot switch to 0.0.0.0 while our relay holds the
			// gateway IP. The transition path is: service stops → scan tears
			// down relay (loopback down) → service restarts on 0.0.0.0 →
			// scan sees gateway reachable → no relay created.
			continue
		}

		// Loopback service exists but no relay yet. Check if gateway can
		// already reach it (service binds 0.0.0.0) — if so, no relay needed.
		gatewayReachable := false
		for _, gw := range hr.gatewayIPs {
			if isListening(gw, port) {
				gatewayReachable = true
				break
			}
		}
		if gatewayReachable {
			continue // service is on 0.0.0.0 — already reachable
		}

		// Need a relay: loopback-only service, not reachable on gateway.
		hr.startRelay(port)
	}
}

// relayEntry tracks listeners for a single relayed port.
type relayEntry struct {
	listeners []net.Listener
}

// startRelay creates TCP relays on all gateway IPs for a single port.
// Must be called with mu held.
func (hr *HostRelay) startRelay(port int) {
	dst := fmt.Sprintf("127.0.0.1:%d", port)
	var entry relayEntry
	for _, gw := range hr.gatewayIPs {
		bindAddr := fmt.Sprintf("%s:%d", gw, port)
		ln, err := net.Listen("tcp", bindAddr)
		if err != nil {
			hr.logger.Printf("[host-relay] cannot bind %s: %v", bindAddr, err)
			continue
		}
		entry.listeners = append(entry.listeners, ln)
		hr.logger.Printf("[host-relay] relaying %s → %s", bindAddr, dst)
		go hr.relayAcceptLoop(ln, dst, port)
	}
	if len(entry.listeners) > 0 {
		hr.active[port] = entry
	}
}

// isListening probes whether a TCP port is accepting connections on the
// given host. Uses a fast connect+close with a short timeout.
func isListening(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// expandPorts collects all host ports from forwarding rules (expanding ranges).
func expandPorts(rules []config.PortForwardRule) []int {
	var ports []int
	for _, rule := range rules {
		if rule.IsRange && rule.RangeEnd > rule.ContainerPort {
			for p := rule.HostPort; p <= rule.HostPort+(rule.RangeEnd-rule.ContainerPort); p++ {
				ports = append(ports, p)
			}
		} else {
			ports = append(ports, rule.HostPort)
		}
	}
	return ports
}

// relayAcceptLoop accepts connections on ln and relays each to dst.
// If the dst becomes unreachable (service stopped), it tears down the
// relay immediately so the gateway IP is freed for services that may
// restart on 0.0.0.0.
func (hr *HostRelay) relayAcceptLoop(ln net.Listener, dst string, port int) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		dstConn, err := net.DialTimeout("tcp", dst, 2*time.Second)
		if err != nil {
			conn.Close()
			// Destination unreachable — service stopped. Tear down immediately.
			hr.teardownPort(port)
			return
		}
		go relayConnPair(conn, dstConn)
	}
}

// teardownPort closes all listeners for a port and removes it from active.
func (hr *HostRelay) teardownPort(port int) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	entry, ok := hr.active[port]
	if !ok {
		return
	}
	for _, ln := range entry.listeners {
		ln.Close()
	}
	delete(hr.active, port)
	hr.logger.Printf("[host-relay] removed relay for port %d (destination unreachable)", port)
}

// relayConnPair copies data bidirectionally between two established connections.
func relayConnPair(src, dst net.Conn) {
	defer src.Close()
	defer dst.Close()

	done := make(chan struct{})
	go func() {
		io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		close(done)
	}()
	io.Copy(src, dst)
	<-done
}
