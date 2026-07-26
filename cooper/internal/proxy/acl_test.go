package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// tempSocketPath returns a unique socket path inside a temp directory.
func tempSocketPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "acl.sock")
}

// sendRequest connects to the Unix socket, sends a request line, and
// reads the response. Returns the trimmed response string and any error.
func sendRequest(socketPath, line string) (string, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return "", fmt.Errorf("dial failed: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	_, err = fmt.Fprintf(conn, "%s\n", line)
	if err != nil {
		return "", fmt.Errorf("write failed: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}

	return strings.TrimSpace(resp), nil
}

func TestSocketCreationAndCleanup(t *testing.T) {
	sockPath := tempSocketPath(t)

	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Socket file should exist after Start.
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Fatal("socket file does not exist after Start")
	}

	// Stop should remove the socket file.
	if err := listener.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Fatal("socket file still exists after Stop")
	}
}

func TestApproveFlow(t *testing.T) {
	sockPath := tempSocketPath(t)

	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	// Start a goroutine to approve the request once it appears on the channel.
	go func() {
		req := <-listener.RequestChan()
		if req.Domain != "example.com" {
			t.Errorf("expected domain example.com, got %s", req.Domain)
		}
		if req.Port != "443" {
			t.Errorf("expected port 443, got %s", req.Port)
		}
		if req.SourceIP != "10.0.0.2" {
			t.Errorf("expected source IP 10.0.0.2, got %s", req.SourceIP)
		}
		listener.Approve(req.ID)
	}()

	resp, err := sendRequest(sockPath, "example.com 443 10.0.0.2")
	if err != nil {
		t.Fatalf("sendRequest failed: %v", err)
	}

	if resp != "OK" {
		t.Fatalf("expected OK, got %q", resp)
	}

	// Verify the request appears in allowed history.
	allowed, blocked := listener.History()
	if len(allowed) != 1 {
		t.Fatalf("expected 1 allowed, got %d", len(allowed))
	}
	if len(blocked) != 0 {
		t.Fatalf("expected 0 blocked, got %d", len(blocked))
	}
	if allowed[0].Domain != "example.com" {
		t.Errorf("allowed domain mismatch: got %s", allowed[0].Domain)
	}
}

func TestSessionDomainAutoApprovesExactHostnameAndAudits(t *testing.T) {
	sockPath := tempSocketPath(t)
	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	firstResult := make(chan string, 1)
	firstErr := make(chan error, 1)
	go func() {
		response, err := sendRequest(sockPath, "Api.Example.com. 443 10.0.0.2")
		firstResult <- response
		firstErr <- err
	}()

	var first ACLRequest
	select {
	case first = <-listener.RequestChan():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first manual review request")
	}

	normalized, err := listener.AllowDomainForSession(first.Domain)
	if err != nil {
		t.Fatalf("AllowDomainForSession failed: %v", err)
	}
	if normalized != "api.example.com" {
		t.Fatalf("normalized domain = %q, want %q", normalized, "api.example.com")
	}
	if err := <-firstErr; err != nil {
		t.Fatalf("first sendRequest failed: %v", err)
	}
	if response := <-firstResult; response != "OK" {
		t.Fatalf("first response = %q, want OK", response)
	}

	select {
	case event := <-listener.DecisionChan():
		if event.Reason != "session" || event.Decision != DecisionAllow || !event.Prompted {
			t.Fatalf("first decision = (%v, %q, prompted=%t), want (allow, session, true)",
				event.Decision, event.Reason, event.Prompted)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first session decision event")
	}

	// Case and a DNS trailing dot normalize to the same exact hostname.
	response, err := sendRequest(sockPath, "API.EXAMPLE.COM. 443 10.0.0.3")
	if err != nil {
		t.Fatalf("automatic sendRequest failed: %v", err)
	}
	if response != "OK" {
		t.Fatalf("automatic response = %q, want OK", response)
	}
	select {
	case request := <-listener.RequestChan():
		t.Fatalf("automatically allowed request unexpectedly required review: %+v", request)
	case <-time.After(150 * time.Millisecond):
	}
	select {
	case event := <-listener.DecisionChan():
		if event.Reason != "session" || event.Request.Domain != "API.EXAMPLE.COM." || event.Prompted {
			t.Fatalf("automatic decision event = %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for automatic session decision event")
	}

	// Exact means exact: a subdomain still requires an independent decision.
	subdomainResult := make(chan string, 1)
	go func() {
		response, _ := sendRequest(sockPath, "child.api.example.com 443 10.0.0.4")
		subdomainResult <- response
	}()
	select {
	case request := <-listener.RequestChan():
		if request.Domain != "child.api.example.com" {
			t.Fatalf("review domain = %q, want child.api.example.com", request.Domain)
		}
		listener.Deny(request.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("exact session allowance incorrectly covered a subdomain")
	}
	if response := <-subdomainResult; response != "ERR" {
		t.Fatalf("subdomain response = %q, want ERR", response)
	}
}

func TestSessionDomainAllowanceResolvesAllAlreadyPendingRequests(t *testing.T) {
	sockPath := tempSocketPath(t)
	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	const requestCount = 4
	responses := make(chan string, requestCount)
	for i := 0; i < requestCount; i++ {
		go func(source int) {
			response, _ := sendRequest(sockPath, fmt.Sprintf(
				"burst.example.com 443 10.0.0.%d", source+10,
			))
			responses <- response
		}(i)
	}

	for i := 0; i < requestCount; i++ {
		select {
		case request := <-listener.RequestChan():
			if request.Domain != "burst.example.com" {
				t.Fatalf("pending domain = %q, want burst.example.com", request.Domain)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("received only %d/%d pending requests", i, requestCount)
		}
	}

	if _, err := listener.AllowDomainForSession("burst.example.com"); err != nil {
		t.Fatalf("AllowDomainForSession failed: %v", err)
	}
	for i := 0; i < requestCount; i++ {
		select {
		case response := <-responses:
			if response != "OK" {
				t.Fatalf("response %d = %q, want OK", i, response)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for response %d", i)
		}
	}
	for i := 0; i < requestCount; i++ {
		select {
		case event := <-listener.DecisionChan():
			if event.Decision != DecisionAllow || event.Reason != "session" || !event.Prompted {
				t.Fatalf("decision %d = (%v, %q, prompted=%t), want (allow, session, true)",
					i, event.Decision, event.Reason, event.Prompted)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for decision event %d", i)
		}
	}
}

func TestSessionDomainRevokeAndStopClearState(t *testing.T) {
	sockPath := tempSocketPath(t)
	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	for _, domain := range []string{"z.example.com", "a.example.com"} {
		if _, err := listener.AllowDomainForSession(domain); err != nil {
			t.Fatalf("allow %s: %v", domain, err)
		}
	}
	if got := listener.SessionAllowedDomains(); !equalStrings(got, []string{"a.example.com", "z.example.com"}) {
		t.Fatalf("session domains = %v, want sorted exact domains", got)
	}
	if !listener.RevokeDomainForSession("A.EXAMPLE.COM.") {
		t.Fatal("expected normalized revoke to remove a.example.com")
	}
	if listener.IsDomainAllowedForSession("a.example.com") {
		t.Fatal("revoked domain remained session-allowed")
	}

	result := make(chan string, 1)
	go func() {
		response, _ := sendRequest(sockPath, "a.example.com 443 10.0.0.8")
		result <- response
	}()
	select {
	case request := <-listener.RequestChan():
		listener.Deny(request.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("revoked domain did not return to manual review")
	}
	if response := <-result; response != "ERR" {
		t.Fatalf("revoked-domain response = %q, want ERR", response)
	}

	if err := listener.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if got := listener.SessionAllowedDomains(); len(got) != 0 {
		t.Fatalf("session domains after Stop = %v, want empty", got)
	}
	if _, err := listener.AllowDomainForSession("after-stop.example.com"); err == nil {
		t.Fatal("AllowDomainForSession succeeded after Stop")
	}
	if got := listener.SessionAllowedDomains(); len(got) != 0 {
		t.Fatalf("post-Stop allow repopulated session domains: %v", got)
	}
}

func TestSessionDomainValidationRejectsBroaderOrNonDNSInputs(t *testing.T) {
	listener := NewACLListener(tempSocketPath(t), time.Second)
	for _, domain := range []string{
		"",
		"*.example.com",
		".example.com",
		"example.com/path",
		"example.com:443",
		"127.0.0.1",
		"127.1",
		"2130706433",
		"[::1]",
		"-edge.example.com",
		"edge-.example.com",
		"example..com",
	} {
		t.Run(domain, func(t *testing.T) {
			if _, err := listener.AllowDomainForSession(domain); err == nil {
				t.Fatalf("AllowDomainForSession(%q) succeeded, want validation error", domain)
			}
		})
	}
	if got := listener.SessionAllowedDomains(); len(got) != 0 {
		t.Fatalf("invalid inputs changed session domains: %v", got)
	}
}

func TestDenyFlow(t *testing.T) {
	sockPath := tempSocketPath(t)

	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	go func() {
		req := <-listener.RequestChan()
		listener.Deny(req.ID)
	}()

	resp, err := sendRequest(sockPath, "evil.com 443 10.0.0.2")
	if err != nil {
		t.Fatalf("sendRequest failed: %v", err)
	}

	if resp != "ERR" {
		t.Fatalf("expected ERR, got %q", resp)
	}

	// Verify the request appears in blocked history.
	allowed, blocked := listener.History()
	if len(allowed) != 0 {
		t.Fatalf("expected 0 allowed, got %d", len(allowed))
	}
	if len(blocked) != 1 {
		t.Fatalf("expected 1 blocked, got %d", len(blocked))
	}
	if blocked[0].Domain != "evil.com" {
		t.Errorf("blocked domain mismatch: got %s", blocked[0].Domain)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestTimeoutFailClosed(t *testing.T) {
	sockPath := tempSocketPath(t)

	// Use a very short timeout so the test doesn't take long.
	listener := NewACLListener(sockPath, 300*time.Millisecond)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	// Don't approve or deny -- let it time out.
	// Drain the channel so the goroutine doesn't block.
	go func() {
		<-listener.RequestChan()
		// Intentionally do nothing -- let the request time out.
	}()

	start := time.Now()
	resp, err := sendRequest(sockPath, "timeout.com 443 10.0.0.2")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("sendRequest failed: %v", err)
	}

	if resp != "ERR" {
		t.Fatalf("expected ERR on timeout, got %q", resp)
	}

	// Verify the timeout happened roughly within the expected window.
	// Allow some slack for test execution overhead.
	if elapsed < 250*time.Millisecond {
		t.Errorf("timeout too fast: %v", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("timeout too slow: %v (expected ~300ms)", elapsed)
	}

	// Verify the request appears in blocked history (timeout = deny).
	_, blocked := listener.History()
	if len(blocked) != 1 {
		t.Fatalf("expected 1 blocked, got %d", len(blocked))
	}
}

func TestConcurrentRequests(t *testing.T) {
	sockPath := tempSocketPath(t)

	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	const numRequests = 10

	// Approve all requests as they come in.
	go func() {
		for i := 0; i < numRequests; i++ {
			req := <-listener.RequestChan()
			listener.Approve(req.ID)
		}
	}()

	var wg sync.WaitGroup
	results := make([]string, numRequests)
	errors := make([]error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			domain := fmt.Sprintf("domain%d.com", idx)
			results[idx], errors[idx] = sendRequest(sockPath, fmt.Sprintf("%s 443 10.0.0.%d", domain, idx))
		}(i)
	}

	wg.Wait()

	for i := 0; i < numRequests; i++ {
		if errors[i] != nil {
			t.Errorf("request %d failed: %v", i, errors[i])
		}
		if results[i] != "OK" {
			t.Errorf("request %d: expected OK, got %q", i, results[i])
		}
	}

	// Verify all requests appear in allowed history.
	allowed, _ := listener.History()
	if len(allowed) != numRequests {
		t.Errorf("expected %d allowed, got %d", numRequests, len(allowed))
	}
}

func TestMalformedRequest(t *testing.T) {
	sockPath := tempSocketPath(t)

	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	// Send a malformed request (not enough fields).
	resp, err := sendRequest(sockPath, "only-domain")
	if err != nil {
		t.Fatalf("sendRequest failed: %v", err)
	}

	if resp != "ERR" {
		t.Fatalf("expected ERR for malformed request, got %q", resp)
	}
}

func TestMissingSocketReturnsError(t *testing.T) {
	// Try to connect to a non-existent socket.
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	conn, err := net.Dial("unix", sockPath)
	if err == nil {
		conn.Close()
		t.Fatal("expected error connecting to missing socket, got nil")
	}
}

func TestSocketCleanupOnStop(t *testing.T) {
	sockPath := tempSocketPath(t)

	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify socket exists.
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Fatal("socket should exist after Start")
	}

	// Stop and verify cleanup.
	if err := listener.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Fatal("socket should not exist after Stop")
	}

	// Starting again after stop should work (no stale socket issues).
	listener2 := NewACLListener(sockPath, 5*time.Second)
	if err := listener2.Start(); err != nil {
		t.Fatalf("second Start failed: %v", err)
	}
	defer listener2.Stop()

	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Fatal("socket should exist after second Start")
	}
}

func TestPendingRequests(t *testing.T) {
	sockPath := tempSocketPath(t)

	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	// Hold a request in pending state while we check PendingRequests.
	reqReceived := make(chan ACLRequest, 1)
	checkDone := make(chan struct{})

	go func() {
		req := <-listener.RequestChan()
		reqReceived <- req
		<-checkDone
		listener.Approve(req.ID)
	}()

	// Send request in background.
	go sendRequest(sockPath, "pending.com 443 10.0.0.1")

	// Wait for request to be received.
	<-reqReceived

	// Give a brief moment for the pending map to be populated.
	time.Sleep(50 * time.Millisecond)

	pending := listener.PendingRequests()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].Request.Domain != "pending.com" {
		t.Errorf("pending domain mismatch: got %s", pending[0].Request.Domain)
	}

	// Release the approval.
	close(checkDone)
}

func TestHelperApproveFlow(t *testing.T) {
	sockPath := tempSocketPath(t)

	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	// Auto-approve everything.
	go func() {
		for req := range listener.RequestChan() {
			listener.Approve(req.ID)
		}
	}()

	// Run the helper with a single request on stdin.
	stdin := strings.NewReader("example.com 443 10.0.0.2\n")
	var stdout bytes.Buffer

	RunHelper(sockPath, stdin, &stdout)

	result := strings.TrimSpace(stdout.String())
	if result != "OK" {
		t.Fatalf("expected OK from helper, got %q", result)
	}
}

func TestHelperDenyFlow(t *testing.T) {
	sockPath := tempSocketPath(t)

	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	// Deny everything.
	go func() {
		for req := range listener.RequestChan() {
			listener.Deny(req.ID)
		}
	}()

	stdin := strings.NewReader("evil.com 443 10.0.0.2\n")
	var stdout bytes.Buffer

	RunHelper(sockPath, stdin, &stdout)

	result := strings.TrimSpace(stdout.String())
	if result != "ERR" {
		t.Fatalf("expected ERR from helper, got %q", result)
	}
}

func TestHelperSocketMissing(t *testing.T) {
	// Point to a non-existent socket -- fail closed.
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	stdin := strings.NewReader("example.com 443 10.0.0.2\n")
	var stdout bytes.Buffer

	RunHelper(sockPath, stdin, &stdout)

	result := strings.TrimSpace(stdout.String())
	if result != "ERR" {
		t.Fatalf("expected ERR for missing socket, got %q", result)
	}
}

func TestHelperMalformedInput(t *testing.T) {
	sockPath := tempSocketPath(t)

	// Don't even start a listener -- the malformed input should be rejected
	// before connecting.
	stdin := strings.NewReader("only-domain\n")
	var stdout bytes.Buffer

	RunHelper(sockPath, stdin, &stdout)

	result := strings.TrimSpace(stdout.String())
	if result != "ERR" {
		t.Fatalf("expected ERR for malformed input, got %q", result)
	}
}

func TestHelperEmptyInput(t *testing.T) {
	sockPath := tempSocketPath(t)

	stdin := strings.NewReader("\n")
	var stdout bytes.Buffer

	RunHelper(sockPath, stdin, &stdout)

	result := strings.TrimSpace(stdout.String())
	if result != "ERR" {
		t.Fatalf("expected ERR for empty input, got %q", result)
	}
}

func TestHelperMultipleRequests(t *testing.T) {
	sockPath := tempSocketPath(t)

	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	// Approve first, deny second.
	go func() {
		req1 := <-listener.RequestChan()
		listener.Approve(req1.ID)

		req2 := <-listener.RequestChan()
		listener.Deny(req2.ID)
	}()

	stdin := strings.NewReader("allow.com 443 10.0.0.2\ndeny.com 443 10.0.0.3\n")
	var stdout bytes.Buffer

	RunHelper(sockPath, stdin, &stdout)

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 response lines, got %d: %v", len(lines), lines)
	}
	if strings.TrimSpace(lines[0]) != "OK" {
		t.Errorf("first response: expected OK, got %q", lines[0])
	}
	if strings.TrimSpace(lines[1]) != "ERR" {
		t.Errorf("second response: expected ERR, got %q", lines[1])
	}
}

func TestHelperTimeoutFailClosed(t *testing.T) {
	sockPath := tempSocketPath(t)

	// Very short timeout so the test doesn't take long.
	listener := NewACLListener(sockPath, 300*time.Millisecond)
	if err := listener.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer listener.Stop()

	// Don't approve or deny -- let it time out.
	go func() {
		<-listener.RequestChan()
	}()

	stdin := strings.NewReader("timeout.com 443 10.0.0.2\n")
	var stdout bytes.Buffer

	RunHelper(sockPath, stdin, &stdout)

	result := strings.TrimSpace(stdout.String())
	if result != "ERR" {
		t.Fatalf("expected ERR on timeout, got %q", result)
	}
}

func TestHelperBrokenPipe(t *testing.T) {
	sockPath := tempSocketPath(t)

	// Create a listener that accepts and immediately closes the connection
	// (simulating a broken pipe).
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()
	defer os.Remove(sockPath)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Close immediately to simulate broken pipe.
			conn.Close()
		}
	}()

	stdin := strings.NewReader("example.com 443 10.0.0.2\n")
	var stdout bytes.Buffer

	RunHelper(sockPath, stdin, &stdout)

	result := strings.TrimSpace(stdout.String())
	if result != "ERR" {
		t.Fatalf("expected ERR on broken pipe, got %q", result)
	}
}

// TestBackpressureSlowConsumer verifies that when the TUI channel consumer is
// slow, requests are NOT dropped — they block until consumed, and the approval
// flow still works end-to-end. This is a regression test for the silent-drop
// bug where a non-blocking send caused requests to be denied without ever
// appearing in the Monitor.
func TestBackpressureSlowConsumer(t *testing.T) {
	sockPath := tempSocketPath(t)
	// Use a very small timeout so the test completes quickly.
	listener := NewACLListener(sockPath, 5*time.Second)
	if err := listener.Start(); err != nil {
		t.Fatalf("start listener: %v", err)
	}
	defer listener.Stop()

	requestCh := listener.RequestChan()

	// Send many requests concurrently WITHOUT consuming the channel.
	// This fills the channel buffer and tests backpressure behavior.
	const numRequests = 20
	var wg sync.WaitGroup
	responses := make([]string, numRequests)

	for i := range numRequests {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			line := fmt.Sprintf("domain%d.com 443 10.0.0.%d", idx, idx)
			resp, err := sendRequest(sockPath, line)
			if err != nil {
				responses[idx] = "error: " + err.Error()
				return
			}
			responses[idx] = resp
		}(i)
	}

	// Wait a moment for requests to queue up and backpressure to build.
	time.Sleep(200 * time.Millisecond)

	// Now start consuming — a slow consumer that processes one request every 50ms.
	// Every request that appears on the channel gets approved.
	consumed := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for consumed < numRequests {
			select {
			case req := <-requestCh:
				// Slow consumption — simulate a TUI that takes time to render.
				time.Sleep(50 * time.Millisecond)
				listener.Approve(req.ID)
				consumed++
			case <-time.After(10 * time.Second):
				// Safety timeout to prevent test hanging forever.
				return
			}
		}
	}()

	// Wait for all sender goroutines to finish.
	wg.Wait()

	// Wait for consumer to finish.
	<-done

	// Verify: every request must have been consumed (no drops).
	if consumed != numRequests {
		t.Errorf("expected %d requests consumed, got %d (requests were dropped!)", numRequests, consumed)
	}

	// Verify: every request must have received OK (approved through the channel).
	for i, resp := range responses {
		if resp != "OK" {
			t.Errorf("request %d: expected OK, got %q (request was denied without being shown to user)", i, resp)
		}
	}
}
