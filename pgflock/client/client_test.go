package client

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeLockerServer is a minimal in-process locker that mimics the real server's
// streaming lock behaviour without any Postgres dependency.
type fakeLockerServer struct {
	mu       sync.Mutex
	pool     []string            // available connection strings
	locked   map[string]struct{} // currently locked
	cancels  map[string]func()   // per-lock cancel to wake handler
	password string
}

func newFakeLocker(password string, dbCount int) *fakeLockerServer {
	pool := make([]string, dbCount)
	for i := range pool {
		pool[i] = fmt.Sprintf("postgresql://tester:pass@localhost:5432/db%d", i+1)
	}
	return &fakeLockerServer{
		pool:     pool,
		locked:   make(map[string]struct{}),
		cancels:  make(map[string]func()),
		password: password,
	}
}

func (f *fakeLockerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/lock":
		f.handleLock(w, r)
	case "/health-check":
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeLockerServer) handleLock(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("password") != f.password {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Acquire a database from the pool (simple polling — fine for tests).
	var connStr string
	for {
		select {
		case <-r.Context().Done():
			http.Error(w, "cancelled", http.StatusRequestTimeout)
			return
		default:
		}

		f.mu.Lock()
		if len(f.pool) > 0 {
			connStr = f.pool[0]
			f.pool = f.pool[1:]
			f.locked[connStr] = struct{}{}
			f.mu.Unlock()
			break
		}
		f.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}

	lockCtx, lockCancel := newCancelContext(r.Context())
	f.mu.Lock()
	f.cancels[connStr] = lockCancel
	f.mu.Unlock()

	w.Header().Set("X-PGFlock-Version", "2")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "%s\n", connStr)
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}

	// Hold until client disconnects or external force-release.
	<-lockCtx.Done()

	f.mu.Lock()
	if _, exists := f.locked[connStr]; exists {
		delete(f.locked, connStr)
		delete(f.cancels, connStr)
		f.pool = append(f.pool, connStr)
	}
	f.mu.Unlock()
}

// forceRelease simulates a server-side force-unlock (like TUI force-unlock).
func (f *fakeLockerServer) forceRelease(connStr string) bool {
	f.mu.Lock()
	cancel, exists := f.cancels[connStr]
	if exists {
		delete(f.locked, connStr)
		delete(f.cancels, connStr)
		f.pool = append(f.pool, connStr)
	}
	f.mu.Unlock()
	if exists && cancel != nil {
		cancel()
	}
	return exists
}

func (f *fakeLockerServer) lockedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.locked)
}

func (f *fakeLockerServer) availableCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pool)
}

// newCancelContext returns a context that is cancelled when either parent is
// cancelled or the returned cancel func is called.
func newCancelContext(parent interface{ Done() <-chan struct{} }) (interface{ Done() <-chan struct{} }, func()) {
	// We need a real context here.
	// Use context package indirectly via a channel.
	ch := make(chan struct{})
	var once sync.Once
	cancel := func() { once.Do(func() { close(ch) }) }

	// Cancel when parent is cancelled.
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-ch:
		}
	}()

	return &chanCtx{ch: ch}, cancel
}

type chanCtx struct{ ch chan struct{} }

func (c *chanCtx) Done() <-chan struct{} { return c.ch }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const testClientPassword = "clienttestpw"
const testClientDBCount = 10

func newTestClientServer(t *testing.T) (*fakeLockerServer, *httptest.Server, int) {
	t.Helper()
	fake := newFakeLocker(testClientPassword, testClientDBCount)
	srv := httptest.NewServer(fake)
	t.Cleanup(func() {
		// CloseAll must come first: closing client connections unblocks the streaming
		// handlers on the server, allowing srv.Close()'s wg.Wait() to complete.
		CloseAll()
		srv.Close()
	})

	// Extract port from server URL.
	addr := srv.Listener.Addr().String()
	var port int
	fmt.Sscanf(addr[strings.LastIndex(addr, ":")+1:], "%d", &port)

	// The client hard-codes "localhost", so we use a custom transport that
	// redirects localhost:<port> to the test server.
	lockClient = &http.Client{
		Transport: &redirectTransport{target: srv.URL},
	}

	return fake, srv, port
}

// redirectTransport rewrites every request's host to the target server.
type redirectTransport struct {
	target string
	inner  http.RoundTripper
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	// Replace scheme+host with test server.
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(rt.target, "http://")
	transport := rt.inner
	if transport == nil {
		transport = &http.Transport{DisableKeepAlives: true}
	}
	return transport.RoundTrip(clone)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestClientLock_ServerVersionMismatch_NoHeader(t *testing.T) {
	// Server that returns 200 but no X-PGFlock-Version header.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lock" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "postgresql://user:pass@localhost:5432/db1")
		}
	}))
	defer srv.Close()
	defer CloseAll()

	lockClient = &http.Client{Transport: &redirectTransport{target: srv.URL}}

	_, err := Lock(9191, "test", "any")
	if err == nil {
		t.Fatal("Expected error for missing version header")
	}
	if !strings.Contains(err.Error(), "v2 required") {
		t.Errorf("Expected 'v2 required' in error, got: %v", err)
	}
}

func TestClientLock_ServerVersionMismatch_WrongVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lock" {
			w.Header().Set("X-PGFlock-Version", "1")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "postgresql://user:pass@localhost:5432/db1")
		}
	}))
	defer srv.Close()
	defer CloseAll()

	lockClient = &http.Client{Transport: &redirectTransport{target: srv.URL}}

	_, err := Lock(9191, "test", "any")
	if err == nil {
		t.Fatal("Expected error for wrong version header")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("Expected 'mismatch' in error, got: %v", err)
	}
}

func TestClientLock_Success(t *testing.T) {
	fake, _, port := newTestClientServer(t)

	connStr, err := Lock(port, "test-marker", testClientPassword)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}
	if connStr == "" {
		t.Fatal("Expected non-empty connection string")
	}

	if fake.lockedCount() != 1 {
		t.Errorf("Expected 1 locked, got %d", fake.lockedCount())
	}

	// Connection must be stored in package map.
	connMu.Lock()
	_, stored := openConns[connStr]
	connMu.Unlock()
	if !stored {
		t.Error("Expected connection stored in openConns map")
	}
}

func TestClientUnlock_ClosesConnectionAndReleasesLock(t *testing.T) {
	fake, _, port := newTestClientServer(t)

	connStr, err := Lock(port, "test-marker", testClientPassword)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	if err := Unlock(port, testClientPassword, connStr); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	// Connection should be removed from map.
	connMu.Lock()
	_, stored := openConns[connStr]
	connMu.Unlock()
	if stored {
		t.Error("Expected connection removed from openConns after Unlock")
	}

	// Server should release the lock.
	if err := awaitClient(2*time.Second, func() bool {
		return fake.lockedCount() == 0
	}); err != nil {
		t.Errorf("Server did not release lock after Unlock: %v", err)
	}

	if fake.availableCount() != testClientDBCount {
		t.Errorf("Expected %d available, got %d", testClientDBCount, fake.availableCount())
	}
}

func TestClientUnlock_UnknownConnString(t *testing.T) {
	_, _, port := newTestClientServer(t)

	err := Unlock(port, testClientPassword, "postgresql://unknown")
	if err == nil {
		t.Fatal("Expected error for unknown connection string")
	}
	if !strings.Contains(err.Error(), "no active lock") {
		t.Errorf("Expected 'no active lock' in error, got: %v", err)
	}
}

func TestClientCloseAll_ReleasesAllLocks(t *testing.T) {
	fake, _, port := newTestClientServer(t)

	const numLocks = 5
	var connStrs []string
	for i := 0; i < numLocks; i++ {
		c, err := Lock(port, fmt.Sprintf("marker-%d", i), testClientPassword)
		if err != nil {
			t.Fatalf("Lock %d failed: %v", i, err)
		}
		connStrs = append(connStrs, c)
	}

	if fake.lockedCount() != numLocks {
		t.Errorf("Expected %d locked, got %d", numLocks, fake.lockedCount())
	}

	CloseAll()

	// All connections removed from map.
	connMu.Lock()
	remaining := len(openConns)
	connMu.Unlock()
	if remaining != 0 {
		t.Errorf("Expected openConns to be empty after CloseAll, got %d", remaining)
	}

	// Server releases all locks.
	if err := awaitClient(2*time.Second, func() bool {
		return fake.lockedCount() == 0
	}); err != nil {
		t.Errorf("Server did not release all locks after CloseAll: %v", err)
	}
}

func TestClientCloseAll_AllowsNewLocks(t *testing.T) {
	fake, _, port := newTestClientServer(t)

	c1, err := Lock(port, "first", testClientPassword)
	if err != nil {
		t.Fatalf("First lock failed: %v", err)
	}
	_ = c1

	CloseAll()

	if err := awaitClient(2*time.Second, func() bool {
		return fake.lockedCount() == 0
	}); err != nil {
		t.Fatalf("Lock not released after CloseAll: %v", err)
	}

	// New lock should succeed after CloseAll.
	c2, err := Lock(port, "second", testClientPassword)
	if err != nil {
		t.Fatalf("Lock after CloseAll failed: %v", err)
	}
	defer Unlock(port, testClientPassword, c2)

	if fake.lockedCount() != 1 {
		t.Errorf("Expected 1 lock after second Lock, got %d", fake.lockedCount())
	}
}

func TestClientLock_BlocksWhenPoolExhausted(t *testing.T) {
	_, _, port := newTestClientServer(t)

	// Lock all databases.
	bodies := make([]string, testClientDBCount)
	for i := 0; i < testClientDBCount; i++ {
		c, err := Lock(port, fmt.Sprintf("marker-%d", i), testClientPassword)
		if err != nil {
			t.Fatalf("Lock %d failed: %v", i, err)
		}
		bodies[i] = c
	}

	// Next lock should block.
	var extraDone bool
	var extraMu sync.Mutex
	go func() {
		_, _ = Lock(port, "extra", testClientPassword)
		extraMu.Lock()
		extraDone = true
		extraMu.Unlock()
	}()

	// Confirm it's blocked.
	time.Sleep(200 * time.Millisecond)
	extraMu.Lock()
	blocked := !extraDone
	extraMu.Unlock()
	if !blocked {
		t.Fatal("Expected Lock to block when pool exhausted")
	}

	// Release one lock — the blocked goroutine should proceed.
	if err := Unlock(port, testClientPassword, bodies[0]); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	if err := awaitClient(5*time.Second, func() bool {
		extraMu.Lock()
		defer extraMu.Unlock()
		return extraDone
	}); err != nil {
		t.Errorf("Blocked Lock did not unblock after Unlock: %v", err)
	}

	CloseAll()
}

// TestClientConcurrentLockUnlock_RaceConditionStress runs many goroutines
// concurrently locking and unlocking to catch data races in the client map.
func TestClientConcurrentLockUnlock_RaceConditionStress(t *testing.T) {
	_, _, port := newTestClientServer(t)

	numGoroutines := 200
	cyclesPerGoroutine := 5

	var wg sync.WaitGroup
	errorsChan := make(chan error, numGoroutines*cyclesPerGoroutine)
	var totalLocks, totalUnlocks atomic.Int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for cycle := 0; cycle < cyclesPerGoroutine; cycle++ {
				connStr, err := Lock(port, fmt.Sprintf("user%d", id), testClientPassword)
				if err != nil {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: Lock: %v", id, cycle, err)
					return
				}
				totalLocks.Add(1)

				// Verify it's in the map.
				connMu.Lock()
				_, ok := openConns[connStr]
				connMu.Unlock()
				if !ok {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: connStr not in openConns after Lock", id, cycle)
				}

				time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)

				if err := Unlock(port, testClientPassword, connStr); err != nil {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: Unlock: %v", id, cycle, err)
					return
				}
				totalUnlocks.Add(1)

				// Verify it's out of the map.
				connMu.Lock()
				_, ok = openConns[connStr]
				connMu.Unlock()
				if ok {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: connStr still in openConns after Unlock", id, cycle)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errorsChan)

	t.Logf("Total locks: %d, total unlocks: %d", totalLocks.Load(), totalUnlocks.Load())

	for err := range errorsChan {
		t.Error(err)
	}

	connMu.Lock()
	remaining := len(openConns)
	connMu.Unlock()
	if remaining != 0 {
		t.Errorf("Expected openConns empty after all goroutines done, got %d entries", remaining)
	}
}

// TestClientCloseAll_RaceWithConcurrentLocks stresses CloseAll being called
// while concurrent goroutines are locking and unlocking, ensuring no panics or
// data corruption.
func TestClientCloseAll_RaceWithConcurrentLocks(t *testing.T) {
	_, _, port := newTestClientServer(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				c, err := Lock(port, fmt.Sprintf("user%d", id), testClientPassword)
				if err != nil {
					// CloseAll may have raced and the server pool may be empty/connection closed
					select {
					case <-stop:
						return
					default:
						time.Sleep(time.Millisecond)
						continue
					}
				}
				time.Sleep(time.Duration(rand.Intn(3)) * time.Millisecond)
				Unlock(port, testClientPassword, c) //nolint
			}
		}(i)
	}

	// Call CloseAll several times while goroutines are running.
	for i := 0; i < 5; i++ {
		time.Sleep(20 * time.Millisecond)
		CloseAll()
	}

	close(stop)
	wg.Wait()
	CloseAll() // final cleanup
}

// TestClientServerForceRelease verifies that when the server force-releases a
// lock (simulating TUI force-unlock), the client's streaming read returns an
// error/EOF, which is the expected result of the connection being dropped.
func TestClientServerForceRelease(t *testing.T) {
	fake, _, port := newTestClientServer(t)

	connStr, err := Lock(port, "force-release-test", testClientPassword)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	// Grab the response body to observe when it closes.
	connMu.Lock()
	body := openConns[connStr]
	connMu.Unlock()

	// Server force-releases the lock (simulates TUI action).
	if !fake.forceRelease(connStr) {
		t.Fatal("forceRelease returned false")
	}

	// The body should become readable (EOF/error) since the server closed the connection.
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1)
		body.(io.Reader).Read(buf) //nolint — we just want to detect EOF
	}()

	select {
	case <-done:
		// Good — connection was closed by server
	case <-time.After(3 * time.Second):
		t.Error("Body did not return EOF after server force-release within 3s")
	}

	// Clean up our side.
	Unlock(port, testClientPassword, connStr) //nolint
}

// awaitClient polls until fn() returns true or timeout elapses.
func awaitClient(timeout time.Duration, fn func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out")
}

