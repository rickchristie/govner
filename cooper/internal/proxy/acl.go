package proxy

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"crypto/rand"
	"encoding/hex"
)

// ACLDecision represents the outcome of an ACL request.
type ACLDecision int

const (
	// DecisionPending means the request is awaiting user action.
	DecisionPending ACLDecision = iota
	// DecisionAllow means the request was approved.
	DecisionAllow
	// DecisionDeny means the request was denied by the user.
	DecisionDeny
	// DecisionTimeout means the request timed out (auto-deny, fail-closed).
	DecisionTimeout
)

// String returns a human-readable string for an ACLDecision.
func (d ACLDecision) String() string {
	switch d {
	case DecisionPending:
		return "pending"
	case DecisionAllow:
		return "allow"
	case DecisionDeny:
		return "deny"
	case DecisionTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// ACLRequest represents an incoming ACL query from the Squid external ACL helper.
type ACLRequest struct {
	ID        string
	Domain    string
	Port      string
	SourceIP  string
	Timestamp time.Time
}

// ACLResponse carries the decision for an ACL request back to the helper.
type ACLResponse struct {
	ID       string
	Decision ACLDecision
}

// PendingRequest tracks an in-flight ACL request awaiting user decision.
// The decision field is accessed atomically to avoid data races between
// the connection handler goroutine and the Approve/Deny callers.
type PendingRequest struct {
	Request  ACLRequest
	Deadline time.Time
	decision atomic.Int32
}

// GetDecision returns the current ACL decision atomically.
func (pr *PendingRequest) GetDecision() ACLDecision {
	return ACLDecision(pr.decision.Load())
}

// SetDecision sets the ACL decision atomically.
func (pr *PendingRequest) SetDecision(d ACLDecision) {
	pr.decision.Store(int32(d))
}

// ACLListener is the host-side Unix socket server that receives ACL queries
// from the Squid external ACL helper running inside the proxy container, and
// returns approve/deny decisions.
//
// The ACL helper connects, sends "domain port source_ip\n", and waits for
// "OK\n" or "ERR\n". The listener pushes requests to the TUI via requestCh,
// then polls for a decision until one is set or the deadline passes.
//
// Fail-closed: if no decision arrives before the deadline, the request is
// automatically denied.
// DecisionEvent is emitted when a request is resolved (approved, denied, or timed out).
// The TUI routes these to the Blocked or Allowed history tabs.
type DecisionEvent struct {
	Request  ACLRequest
	Decision ACLDecision
	Reason   string // "approved", "denied", "timeout"
}

type ACLListener struct {
	socketPath string
	timeoutNs  atomic.Int64 // approval timeout in nanoseconds (atomic for concurrent access)

	pending    sync.Map // id -> *PendingRequest
	requestCh  chan ACLRequest
	decisionIn chan DecisionEvent    // internal: handleConnection writes here
	decisionCh chan DecisionEvent    // external: TUI reads from here
	listener   net.Listener

	// sendMu protects channel sends against concurrent close in Stop().
	// handleConnection holds a read-lock to send; Stop() holds a write-lock to close.
	sendMu   sync.RWMutex
	stopped  bool // protected by sendMu

	// history is protected by historyMu.
	historyMu sync.Mutex
	allowed   []ACLRequest
	blocked   []ACLRequest
}

// NewACLListener creates a new ACLListener that will listen on the given
// Unix socket path with the given approval timeout.
func NewACLListener(socketPath string, timeout time.Duration) *ACLListener {
	l := &ACLListener{
		socketPath: socketPath,
		requestCh:  make(chan ACLRequest, 256),
		decisionIn: make(chan DecisionEvent, 256),
		decisionCh: make(chan DecisionEvent),
	}
	l.timeoutNs.Store(int64(timeout))
	// Unbounded buffer goroutine: drains decisionIn into an in-memory slice,
	// feeds decisionCh from the front. Never blocks the producer, never drops.
	go func() {
		var buf []DecisionEvent
		for {
			// If buffer is empty, block on input only.
			if len(buf) == 0 {
				evt, ok := <-l.decisionIn
				if !ok {
					close(l.decisionCh)
					return
				}
				buf = append(buf, evt)
				continue
			}
			// Buffer has items: try to send the front, or receive more.
			select {
			case l.decisionCh <- buf[0]:
				buf = buf[1:]
			case evt, ok := <-l.decisionIn:
				if !ok {
					// Drain remaining buffer.
					for _, e := range buf {
						l.decisionCh <- e
					}
					close(l.decisionCh)
					return
				}
				buf = append(buf, evt)
			}
		}
	}()
	return l
}

// Start creates the Unix domain socket and begins accepting connections
// from ACL helper processes. Each connection is handled in its own goroutine.
func (l *ACLListener) Start() error {
	// Remove stale socket file if it exists.
	if err := os.Remove(l.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", l.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket %s: %w", l.socketPath, err)
	}
	l.listener = ln

	go l.acceptLoop()
	return nil
}

// Stop closes the listener, removes the socket file, and closes channels
// so that forwarding goroutines (which range over RequestChan/DecisionChan)
// exit cleanly instead of leaking.
func (l *ACLListener) Stop() error {
	var firstErr error

	if l.listener != nil {
		if err := l.listener.Close(); err != nil {
			firstErr = err
		}
	}

	if err := os.Remove(l.socketPath); err != nil && !os.IsNotExist(err) {
		if firstErr == nil {
			firstErr = err
		}
	}

	// Hold write-lock to prevent handleConnection from sending while we close.
	// handleConnection holds a read-lock to send, so this is safe.
	l.sendMu.Lock()
	l.stopped = true
	close(l.requestCh)
	close(l.decisionIn)
	l.sendMu.Unlock()

	return firstErr
}

// Approve sets the decision for the given request ID to Allow.
func (l *ACLListener) Approve(id string) {
	if val, ok := l.pending.Load(id); ok {
		pr := val.(*PendingRequest)
		pr.SetDecision(DecisionAllow)
	}
}

// Deny sets the decision for the given request ID to Deny.
func (l *ACLListener) Deny(id string) {
	if val, ok := l.pending.Load(id); ok {
		pr := val.(*PendingRequest)
		pr.SetDecision(DecisionDeny)
	}
}

// RequestChan returns a read-only channel that emits new ACL requests
// for the TUI to display.
func (l *ACLListener) RequestChan() <-chan ACLRequest {
	return l.requestCh
}

// DecisionChan returns a read-only channel that emits decision events
// when requests are resolved. The TUI routes these to history tabs.
func (l *ACLListener) DecisionChan() <-chan DecisionEvent {
	return l.decisionCh
}

// PendingRequests returns a snapshot of all currently pending requests.
func (l *ACLListener) PendingRequests() []*PendingRequest {
	var result []*PendingRequest
	l.pending.Range(func(key, value any) bool {
		pr := value.(*PendingRequest)
		if pr.GetDecision() == DecisionPending {
			result = append(result, pr)
		}
		return true
	})
	return result
}

// History returns copies of the allowed and blocked request histories.
func (l *ACLListener) History() (allowed []ACLRequest, blocked []ACLRequest) {
	l.historyMu.Lock()
	defer l.historyMu.Unlock()

	a := make([]ACLRequest, len(l.allowed))
	copy(a, l.allowed)

	b := make([]ACLRequest, len(l.blocked))
	copy(b, l.blocked)

	return a, b
}

// acceptLoop accepts connections until the listener is closed.
func (l *ACLListener) acceptLoop() {
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			// If Stop() was called, the listener was intentionally closed.
			l.sendMu.RLock()
			isStopped := l.stopped
			l.sendMu.RUnlock()
			if isStopped {
				return
			}
			// Any other error is treated as transient (e.g. file descriptor
			// exhaustion, temporary network issues). Back off briefly and
			// retry rather than exiting permanently.
			time.Sleep(50 * time.Millisecond)
			continue
		}
		go l.handleConnection(conn)
	}
}

// handleConnection processes a single connection from the ACL helper.
// Protocol: helper sends "domain port source_ip\n", listener responds
// with "OK\n" or "ERR\n".
func (l *ACLListener) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Set a read deadline so we don't hang forever waiting for the helper
	// to send data. Use 2x the approval timeout as an outer bound.
	conn.SetReadDeadline(time.Now().Add(time.Duration(l.timeoutNs.Load()) * 2))

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		// Failed to read from helper -- write ERR and return.
		conn.Write([]byte("ERR\n"))
		return
	}

	line := strings.TrimSpace(scanner.Text())
	parts := strings.Fields(line)
	if len(parts) < 2 {
		// Malformed request -- fail closed.
		conn.Write([]byte("ERR\n"))
		return
	}

	id := generateID()
	now := time.Now()

	// Squid sends: %DST %SRC (domain and source IP).
	// Port defaults to 443 (HTTPS CONNECT tunnels).
	port := "443"
	sourceIP := parts[1]
	if len(parts) >= 3 {
		// If 3 fields provided, middle is port.
		port = parts[1]
		sourceIP = parts[2]
	}

	req := ACLRequest{
		ID:        id,
		Domain:    parts[0],
		Port:      port,
		SourceIP:  sourceIP,
		Timestamp: now,
	}

	pr := &PendingRequest{
		Request:  req,
		Deadline: now.Add(time.Duration(l.timeoutNs.Load())),
	}
	// decision field defaults to zero value (DecisionPending).

	l.pending.Store(id, pr)

	// Send to TUI under read-lock. Stop() holds write-lock when closing channels.
	l.sendMu.RLock()
	if l.stopped {
		l.sendMu.RUnlock()
		conn.Write([]byte("ERR\n"))
		return
	}
	l.requestCh <- req
	l.sendMu.RUnlock()

	// Poll for a decision until one is made or the deadline passes.
	decision := l.waitForDecision(pr)

	// Record in history.
	l.recordHistory(req, decision)

	// Emit decision event for the TUI history tabs.
	reason := "approved"
	if decision == DecisionDeny {
		reason = "denied"
	} else if decision == DecisionTimeout {
		reason = "timeout"
	}
	// Send under read-lock to prevent race with Stop() closing the channel.
	l.sendMu.RLock()
	if !l.stopped {
		l.decisionIn <- DecisionEvent{Request: req, Decision: decision, Reason: reason}
	}
	l.sendMu.RUnlock()

	// Clean up from pending map.
	l.pending.Delete(id)

	// Write response to the helper.
	if decision == DecisionAllow {
		conn.Write([]byte("OK\n"))
	} else {
		conn.Write([]byte("ERR\n"))
	}
}

// waitForDecision polls the PendingRequest for a decision at 100ms intervals.
// If the deadline passes with no decision, it sets the decision to Timeout
// (fail-closed) and returns.
func (l *ACLListener) waitForDecision(pr *PendingRequest) ACLDecision {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if d := pr.GetDecision(); d != DecisionPending {
			return d
		}

		if time.Now().After(pr.Deadline) {
			pr.SetDecision(DecisionTimeout)
			return DecisionTimeout
		}

		<-ticker.C
	}
}

// recordHistory adds the request to the appropriate history list.
func (l *ACLListener) recordHistory(req ACLRequest, decision ACLDecision) {
	l.historyMu.Lock()
	defer l.historyMu.Unlock()

	if decision == DecisionAllow {
		l.allowed = append(l.allowed, req)
	} else {
		l.blocked = append(l.blocked, req)
	}
}

// SetTimeout updates the approval timeout used for new requests. This is
// called at runtime when the user changes the timeout in the settings tab.
func (l *ACLListener) SetTimeout(d time.Duration) {
	l.timeoutNs.Store(int64(d))
}



// generateID returns a random hex string suitable for identifying requests.
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
