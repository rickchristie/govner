// Package vmrelay provides the network side of the Cooper VM boundary. The
// relay has a proxy-only Docker network and no selected host file mounts.
package vmrelay

import (
	"context"
	"encoding/json"
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

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

const (
	maxConnections        = 256
	maxProxyConnections   = 192
	maxBridgeConnections  = 32
	maxForwardConnections = 32
	maxConnectionIdle     = 2 * time.Minute
	maxConnectionLifetime = 30 * time.Minute
)

// Policy contains the exact relay destinations. ProxyHost comes from Cooper,
// not from guest input.
type Policy struct {
	ProxyHost    string       `json:"proxy_host"`
	ProxyPort    int          `json:"proxy_port"`
	BridgePort   int          `json:"bridge_port"`
	ForwardPorts map[int]bool `json:"-"`
	Forwards     []int        `json:"forward_ports"`
}

// LoadPolicy reads and validates a relay policy.
func LoadPolicy(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("read VM relay policy: %w", err)
	}
	var policy Policy
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return Policy{}, fmt.Errorf("decode VM relay policy: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected data after JSON object")
		}
		return Policy{}, fmt.Errorf("decode VM relay policy: %w", err)
	}
	if len(policy.Forwards) > vmproto.MaxForwardPorts {
		return Policy{}, fmt.Errorf("VM relay has %d forward ports; maximum is %d", len(policy.Forwards), vmproto.MaxForwardPorts)
	}
	policy.ForwardPorts = make(map[int]bool, len(policy.Forwards))
	for _, port := range policy.Forwards {
		if policy.ForwardPorts[port] {
			return Policy{}, fmt.Errorf("VM relay forward port %d is duplicated", port)
		}
		policy.ForwardPorts[port] = true
	}
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

// Validate checks all fixed network destinations.
func (p Policy) Validate() error {
	if strings.TrimSpace(p.ProxyHost) == "" || len(p.ProxyHost) > 253 || strings.ContainsAny(p.ProxyHost, "\x00\r\n") {
		return errors.New("VM relay proxy host is required")
	}
	for name, port := range map[string]int{"proxy": p.ProxyPort, "bridge": p.BridgePort} {
		if port < 1 || port > 65535 {
			return fmt.Errorf("VM relay %s port %d is invalid", name, port)
		}
	}
	for port := range p.ForwardPorts {
		if port < 1 || port > 65535 {
			return fmt.Errorf("VM relay forward port %d is invalid", port)
		}
	}
	return nil
}

// Server accepts bounded Unix connections from the networkless supervisor.
type Server struct {
	SocketPath string
	PolicyPath string
	LogPath    string
	Logger     *log.Logger
	requests   *vmproto.ActiveRequests
	limits     *serviceLimits
}

type serviceLimits struct {
	mu     sync.Mutex
	active map[string]int
}

// Serve runs until the context ends or the listener fails.
func (s Server) Serve(ctx context.Context) error {
	if s.requests == nil {
		s.requests = &vmproto.ActiveRequests{}
	}
	if s.limits == nil {
		s.limits = &serviceLimits{active: make(map[string]int)}
	}
	if s.Logger == nil && s.LogPath != "" {
		if filepath.Clean(s.LogPath) != s.LogPath || !filepath.IsAbs(s.LogPath) {
			return errors.New("VM relay log path must be an absolute clean path")
		}
		file, err := os.OpenFile(s.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("open VM relay log: %w", err)
		}
		defer file.Close()
		s.Logger = log.New(file, "", log.LstdFlags|log.Lmicroseconds|log.LUTC)
	}
	s.log("relay starting")
	defer s.log("relay stopped")
	if strings.TrimSpace(s.SocketPath) == "" {
		return errors.New("VM relay socket path is required")
	}
	if _, err := LoadPolicy(s.PolicyPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.SocketPath), 0o700); err != nil {
		return fmt.Errorf("create VM relay socket directory: %w", err)
	}
	if err := os.Remove(s.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale VM relay socket: %w", err)
	}
	listener, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on VM relay socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(s.SocketPath)
	if err := os.Chmod(s.SocketPath, 0o600); err != nil {
		return fmt.Errorf("set VM relay socket mode: %w", err)
	}

	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	semaphore := make(chan struct{}, maxConnections)
	var connections sync.WaitGroup
	defer connections.Wait()
	for {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept VM relay connection: %w", acceptErr)
		}
		select {
		case semaphore <- struct{}{}:
			connections.Add(1)
			go func() {
				defer connections.Done()
				defer func() { <-semaphore }()
				s.handle(connection)
			}()
		default:
			connection.Close()
		}
	}
}

func (s Server) handle(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	header, err := vmproto.ReadHeader(connection)
	if err != nil {
		s.log("reject header: %v", err)
		return
	}
	if !s.requests.Acquire(header.RequestID) {
		s.log("reject duplicate request %q", header.RequestID)
		return
	}
	defer s.requests.Release(header.RequestID)
	// Read the policy for each new stream. The host replaces the file through
	// a mounted directory, so one accepted update applies without restarting
	// the relay. An invalid or unavailable policy rejects the stream.
	policy, err := LoadPolicy(s.PolicyPath)
	if err != nil {
		s.log("reject service %q: reload policy: %v", header.Service, err)
		return
	}
	destination, err := policy.destination(header.Service)
	if err != nil {
		s.log("reject service %q: %v", header.Service, err)
		return
	}
	if !s.limits.acquire(header.Service) {
		s.log("reject service %q: connection quota reached", header.Service)
		return
	}
	defer s.limits.release(header.Service)
	s.log("allow service %q", header.Service)
	deadline := time.Now().Add(maxConnectionLifetime)
	_ = connection.SetDeadline(deadline)
	target, err := net.DialTimeout("tcp", destination, 10*time.Second)
	if err != nil {
		s.log("dial service %q: %v", header.Service, err)
		return
	}
	defer target.Close()
	copyBounded(connection, target, maxConnectionIdle, deadline)
}

func copyBounded(left, right net.Conn, idle time.Duration, expires time.Time) {
	activity := &connectionActivity{left: left, right: right, idle: idle, expires: expires}
	activity.refresh()
	copyBoth(&boundedConnection{Conn: left, activity: activity}, &boundedConnection{Conn: right, activity: activity})
}

// Both directions share an idle deadline. A large download can keep receiving
// data without sending another request; its blocked request read must stay
// open. The fixed lifetime still limits streams with continuous traffic.
type connectionActivity struct {
	mu          sync.Mutex
	left, right net.Conn
	idle        time.Duration
	expires     time.Time
}

func (a *connectionActivity) refresh() {
	// Serialize both deadline updates so an older activity event cannot
	// replace a newer deadline while the copy directions run concurrently.
	a.mu.Lock()
	defer a.mu.Unlock()
	deadline := time.Now().Add(a.idle)
	if deadline.After(a.expires) {
		deadline = a.expires
	}
	_ = a.left.SetDeadline(deadline)
	_ = a.right.SetDeadline(deadline)
}

type boundedConnection struct {
	net.Conn
	activity *connectionActivity
}

func (c *boundedConnection) Read(data []byte) (int, error) {
	count, err := c.Conn.Read(data)
	if count > 0 {
		c.activity.refresh()
	}
	return count, err
}

func (c *boundedConnection) Write(data []byte) (int, error) {
	count, err := c.Conn.Write(data)
	if count > 0 {
		c.activity.refresh()
	}
	return count, err
}

func (c *boundedConnection) CloseWrite() error {
	if closer, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return closer.CloseWrite()
	}
	return c.Conn.Close()
}

func (l *serviceLimits) acquire(service string) bool {
	class, maximum := serviceClass(service)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active[class] >= maximum {
		return false
	}
	l.active[class]++
	return true
}

func (l *serviceLimits) release(service string) {
	class, _ := serviceClass(service)
	l.mu.Lock()
	if l.active[class] > 0 {
		l.active[class]--
	}
	l.mu.Unlock()
}

func serviceClass(service string) (string, int) {
	switch service {
	case vmproto.ServiceProxy:
		return vmproto.ServiceProxy, maxProxyConnections
	case vmproto.ServiceBridge:
		return vmproto.ServiceBridge, maxBridgeConnections
	default:
		return "forward", maxForwardConnections
	}
}

func (p Policy) destination(service string) (string, error) {
	port := 0
	switch service {
	case vmproto.ServiceProxy:
		port = p.ProxyPort
	case vmproto.ServiceBridge:
		port = p.BridgePort
	default:
		value, ok := strings.CutPrefix(service, "forward:")
		if !ok {
			return "", fmt.Errorf("service is not allowed")
		}
		parsed, err := strconv.Atoi(value)
		if err != nil || !p.ForwardPorts[parsed] {
			return "", fmt.Errorf("forward port is not allowed")
		}
		port = parsed
	}
	return net.JoinHostPort(p.ProxyHost, strconv.Itoa(port)), nil
}

func (s Server) log(format string, values ...any) {
	if s.Logger != nil {
		s.Logger.Printf(format, values...)
	}
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
