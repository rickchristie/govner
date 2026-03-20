package locker

import (
	"context"
	"encoding/json"
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

	"github.com/rickchristie/govner/pgflock/internal/config"
)

const defaultDatabaseCount = 25
const testPassword = "testpassword"

func testConfig() *config.Config {
	return &config.Config{
		DockerNamePrefix:     "test",
		InstanceCount:        1,
		StartingPort:         5432,
		DatabasesPerInstance: defaultDatabaseCount,
		PGUsername:           "tester",
		Password:             testPassword,
		DatabasePrefix:       "tester",
		AutoUnlockMins:       30,
		Encoding:             "UTF8",
		LCCollate:            "en_US.UTF-8",
		LCCtype:              "en_US.UTF-8",
	}
}

// newTestHandler creates a handler for testing with DB reset skipped.
func newTestHandler() *Handler {
	return newTestHandlerWithCleanupInterval(1 * time.Minute)
}

// newTestHandlerWithCleanupInterval creates a handler with a configurable cleanup interval.
func newTestHandlerWithCleanupInterval(cleanupInterval time.Duration) *Handler {
	cfg := testConfig()

	testDatabases := make(map[string]bool)
	for _, port := range cfg.InstancePorts() {
		for i := 1; i <= cfg.DatabasesPerInstance; i++ {
			connString := fmt.Sprintf("postgresql://%s:%s@localhost:%d/%s%d",
				cfg.PGUsername, cfg.Password, port, cfg.DatabasePrefix, i)
			testDatabases[connString] = true
		}
	}

	h := &Handler{
		cfg:                   cfg,
		password:              cfg.Password,
		testDatabases:         testDatabases,
		cLockedDbConn:         make(chan string, len(testDatabases)),
		locks:                 make(map[string]*LockInfo),
		cleanupTickerInterval: cleanupInterval,
		autoUnlockDuration:    time.Duration(cfg.AutoUnlockMins) * time.Minute,
		stateUpdateChan:       nil,
		resetDatabase:         func(_ *config.Config, _ string) error { return nil },
	}

	for connStr := range testDatabases {
		h.cLockedDbConn <- connStr
	}

	go h.cleanupExpiredLocks()

	return h
}

// newStreamingTestServer creates a real HTTP test server using the handler,
// with DB reset skipped. Use this for tests that need real streaming connections.
func newStreamingTestServer(t *testing.T) (*Handler, *httptest.Server) {
	t.Helper()
	h := newTestHandler()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	return h, server
}

// newStreamingTestServerWithAutoUnlock creates a server whose handler auto-unlocks
// after the given duration (via context.WithTimeout inside handleLock).
func newStreamingTestServerWithAutoUnlock(t *testing.T, autoUnlock time.Duration) (*Handler, *httptest.Server) {
	t.Helper()
	h := newTestHandler()
	h.autoUnlockDuration = autoUnlock
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	return h, server
}

// lockStreaming acquires a lock via a real HTTP streaming connection.
// Returns the connStr and the response body (kept open to hold the lock).
// The caller must close the body to release the lock.
func lockStreaming(t *testing.T, serverURL, marker, password string) (connStr string, body io.ReadCloser) {
	t.Helper()
	reqURL := fmt.Sprintf("%s/lock?marker=%s&password=%s", serverURL, marker, password)

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := client.Get(reqURL)
	if err != nil {
		t.Fatalf("lock request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("lock returned status %d", resp.StatusCode)
	}

	buf := make([]byte, 256)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		resp.Body.Close()
		t.Fatalf("failed to read conn string: %v", err)
	}
	connStr = strings.TrimSpace(string(buf[:n]))
	return connStr, resp.Body
}

// handleLockNoReset is a non-streaming test version of handleLock that skips
// database reset. Used for unit tests that test the lock/unlock state machine
// without needing real HTTP connections.
func (h *Handler) handleLockNoReset(resp http.ResponseWriter, req *http.Request) {
	marker, valid := h.validateAuth(req)
	if !valid {
		http.Error(resp, "Invalid marker or password", http.StatusUnauthorized)
		return
	}

	h.waitingCount.Add(1)
	h.sendStateUpdate()

	select {
	case connStr := <-h.cLockedDbConn:
		h.waitingCount.Add(-1)
		h.sendStateUpdate()

		// Create a cancel func so external unlock operations (ForceUnlock, handleUnlock)
		// can signal this lock is gone. Nobody listens to this context in the
		// non-streaming path, but having it ensures the lock map is consistent.
		_, lockCancel := context.WithCancel(context.Background())

		h.withLocksLock(func() {
			h.locks[connStr] = &LockInfo{
				ConnString: connStr,
				Marker:     marker,
				LockedAt:   time.Now(),
				cancel:     lockCancel,
			}
		})

		resp.Header().Set("X-PGFlock-Version", serverVersion)
		_, err := resp.Write([]byte(connStr + "\n"))
		if err != nil {
			lockCancel()
			return
		}

		h.sendStateUpdate()

	case <-req.Context().Done():
		h.waitingCount.Add(-1)
		h.sendStateUpdate()
		http.Error(resp, "Request cancelled or timed out", http.StatusRequestTimeout)
	}
}

// Await polls until event() returns true or the timeout elapses.
func Await(timeoutDuration time.Duration, event func() bool) error {
	timeout := time.After(timeoutDuration)
	for {
		select {
		case <-timeout:
			return fmt.Errorf("timed out waiting for event")
		default:
			if event() {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// ---------------------------------------------------------------------------
// Unit tests — use ResponseRecorder + handleLockNoReset (non-streaming)
// ---------------------------------------------------------------------------

func TestAuthValidation_LockUnlock(t *testing.T) {
	h := newTestHandler()

	tests := []struct {
		name     string
		marker   string
		password string
		expected bool
	}{
		{"Valid credentials", "testuser", testPassword, true},
		{"Empty marker", "", testPassword, false},
		{"Wrong password", "testuser", "wrongpassword", false},
		{"Both wrong", "testuser", "wrongpassword", false},
		{"Empty both", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name+" (lock and unlock)", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/lock?marker="+tt.marker+"&password="+tt.password, nil)
			marker, valid := h.validateAuth(req)

			if valid != tt.expected {
				t.Errorf("validateAuth() = %v, want %v", valid, tt.expected)
			}
			if valid && marker != tt.marker {
				t.Errorf("Expected marker %s, got %s", tt.marker, marker)
			}

			req = httptest.NewRequest("GET", "/unlock?marker="+tt.marker+"&password="+tt.password+"&conn=someconn", nil)
			marker, valid = h.validateAuth(req)

			if valid != tt.expected {
				t.Errorf("validateAuth() for unlock = %v, want %v", valid, tt.expected)
			}
			if valid && marker != tt.marker {
				t.Errorf("Expected marker %s for unlock, got %s", tt.marker, marker)
			}
		})
	}
}

func TestLockUnlockFlow(t *testing.T) {
	h := newTestHandler()

	req := httptest.NewRequest("GET", "/lock?marker=testuser&password="+testPassword, nil)
	rr := httptest.NewRecorder()
	h.handleLockNoReset(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	connStr := strings.TrimSpace(rr.Body.String())
	if connStr == "" {
		t.Fatal("Expected connection string, got empty response")
	}

	unlockURL := "/unlock?marker=testuser&password=" + testPassword
	req = httptest.NewRequest("POST", unlockURL, strings.NewReader(connStr))
	rr = httptest.NewRecorder()
	h.handleUnlock(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestVersionHeader(t *testing.T) {
	h := newTestHandler()

	req := httptest.NewRequest("GET", "/lock?marker=testuser&password="+testPassword, nil)
	rr := httptest.NewRecorder()
	h.handleLockNoReset(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("X-PGFlock-Version"); got != serverVersion {
		t.Errorf("Expected X-PGFlock-Version: %s, got %q", serverVersion, got)
	}
}

func TestAutoUnlockSafetyNet(t *testing.T) {
	// Safety-net cleanup only triggers for locks with cancel=nil.
	h := newTestHandlerWithCleanupInterval(100 * time.Millisecond)
	h.autoUnlockDuration = 200 * time.Millisecond

	// Insert a lock with no cancel func to simulate a legacy/edge-case lock.
	var capturedConn string
	h.withLocksRLock(func() {
		for k := range h.testDatabases {
			capturedConn = k
			break
		}
	})
	// Remove it from the pool manually (simulating it being locked)
	<-h.cLockedDbConn
	h.withLocksLock(func() {
		h.locks[capturedConn] = &LockInfo{
			ConnString: capturedConn,
			Marker:     "ghost",
			LockedAt:   time.Now().Add(-300 * time.Millisecond),
			cancel:     nil, // no cancel — safety-net should clean this up
		}
	})

	err := Await(2*time.Second, func() bool {
		var exists bool
		h.withLocksRLock(func() {
			_, exists = h.locks[capturedConn]
		})
		return !exists
	})
	if err != nil {
		t.Errorf("Safety-net auto-unlock did not fire: %v", err)
	}
}

func TestLock_BlockWhenExhausted(t *testing.T) {
	h := newTestHandler()

	var lockedConnections []string
	for i := 0; i < defaultDatabaseCount; i++ {
		req := httptest.NewRequest("GET", "/lock?marker=testuser&password="+testPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLockNoReset(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("Expected lock %d to succeed, got status %d", i+1, rr.Code)
		}
		lockedConnections = append(lockedConnections, strings.TrimSpace(rr.Body.String()))
	}

	var otherLockerResponse *httptest.ResponseRecorder
	var otherLockerDone bool
	var otherLockerMu sync.Mutex

	go func() {
		req := httptest.NewRequest("GET", "/lock?marker=otheruser&password="+testPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLockNoReset(rr, req)

		otherLockerMu.Lock()
		otherLockerResponse = rr
		otherLockerDone = true
		otherLockerMu.Unlock()
	}()

	// Confirm the request is blocked.
	err := Await(300*time.Millisecond, func() bool {
		otherLockerMu.Lock()
		defer otherLockerMu.Unlock()
		return otherLockerDone
	})
	if err == nil {
		t.Fatal("Expected lock to be blocked when all databases exhausted, but it completed")
	}

	// Unlock one database.
	selectedConnStr := lockedConnections[rand.Intn(len(lockedConnections))]
	req := httptest.NewRequest("POST", "/unlock?marker=testuser&password="+testPassword,
		strings.NewReader(selectedConnStr))
	rr := httptest.NewRecorder()
	h.handleUnlock(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Unlock failed: %d", rr.Code)
	}

	// Blocked request should now complete.
	err = Await(5*time.Second, func() bool {
		otherLockerMu.Lock()
		defer otherLockerMu.Unlock()
		return otherLockerDone
	})
	if err != nil {
		t.Errorf("Expected blocked request to complete after unlock: %v", err)
	}

	otherLockerMu.Lock()
	defer otherLockerMu.Unlock()
	if otherLockerResponse.Code != http.StatusOK {
		t.Errorf("Expected otherLocker to get status 200, got %d", otherLockerResponse.Code)
	}
	if returnedConn := strings.TrimSpace(otherLockerResponse.Body.String()); returnedConn != selectedConnStr {
		t.Errorf("Expected otherLocker to get %s, got %s", selectedConnStr, returnedConn)
	}
}

func TestLock_RaceConditionStressTest(t *testing.T) {
	h := newTestHandler()
	numGoroutines := 50 * defaultDatabaseCount

	var wg sync.WaitGroup
	errorsChan := make(chan error, numGoroutines)

	counters := make(map[string]*atomic.Int32)
	for connStr := range h.testDatabases {
		counters[connStr] = &atomic.Int32{}
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := httptest.NewRequest("GET",
				fmt.Sprintf("/lock?marker=user%d&password=%s", id, testPassword), nil)
			rr := httptest.NewRecorder()
			h.handleLockNoReset(rr, req)

			if rr.Code != http.StatusOK {
				errorsChan <- fmt.Errorf("goroutine %d: lock failed status %d", id, rr.Code)
				return
			}

			connStr := strings.TrimSpace(rr.Body.String())
			if counters[connStr].Add(1) != 1 {
				errorsChan <- fmt.Errorf("goroutine %d: RACE — %s held by multiple goroutines", id, connStr)
			}

			time.Sleep(time.Duration(rand.Intn(500)) * time.Millisecond)

			if counters[connStr].Add(-1) != 0 {
				errorsChan <- fmt.Errorf("goroutine %d: counter for %s not 0 after decrement", id, connStr)
			}

			req = httptest.NewRequest("POST",
				fmt.Sprintf("/unlock?marker=user%d&password=%s", id, testPassword),
				strings.NewReader(connStr))
			rr = httptest.NewRecorder()
			h.handleUnlock(rr, req)

			if rr.Code != http.StatusOK {
				errorsChan <- fmt.Errorf("goroutine %d: unlock failed status %d", id, rr.Code)
			}
		}(i)
	}

	wg.Wait()
	close(errorsChan)

	for err := range errorsChan {
		t.Error(err)
	}

	h.withLocksRLock(func() {
		if len(h.locks) != 0 {
			t.Errorf("Expected all locks released, %d remain", len(h.locks))
		}
	})
	if got := len(h.cLockedDbConn); got != defaultDatabaseCount {
		t.Errorf("Expected %d databases available, got %d", defaultDatabaseCount, got)
	}
}

func TestLock_MassiveRaceConditionStressTest(t *testing.T) {
	h := newTestHandler()

	numGoroutines := 5000
	cyclesPerGoroutine := 3

	var wg sync.WaitGroup
	errorsChan := make(chan error, numGoroutines*cyclesPerGoroutine)

	counters := make(map[string]*atomic.Int32)
	for connStr := range h.testDatabases {
		counters[connStr] = &atomic.Int32{}
	}

	var totalLocks, totalUnlocks atomic.Int64
	var seenConnections sync.Map

	t.Logf("Stress test: %d goroutines × %d cycles = %d total lock attempts",
		numGoroutines, cyclesPerGoroutine, numGoroutines*cyclesPerGoroutine)
	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for cycle := 0; cycle < cyclesPerGoroutine; cycle++ {
				req := httptest.NewRequest("GET",
					fmt.Sprintf("/lock?marker=user%d&password=%s", id, testPassword), nil)
				rr := httptest.NewRecorder()
				h.handleLockNoReset(rr, req)

				if rr.Code != http.StatusOK {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: lock status %d", id, cycle, rr.Code)
					return
				}

				connStr := strings.TrimSpace(rr.Body.String())
				totalLocks.Add(1)
				seenConnections.Store(connStr, true)

				if v := counters[connStr].Add(1); v != 1 {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: RACE on %s (counter=%d)", id, cycle, connStr, v)
				}

				time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)

				if v := counters[connStr].Add(-1); v != 0 {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: counter for %s = %d after decrement", id, cycle, connStr, v)
				}

				req = httptest.NewRequest("POST",
					fmt.Sprintf("/unlock?marker=user%d&password=%s", id, testPassword),
					strings.NewReader(connStr))
				rr = httptest.NewRecorder()
				h.handleUnlock(rr, req)

				if rr.Code != http.StatusOK {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: unlock status %d", id, cycle, rr.Code)
					return
				}
				totalUnlocks.Add(1)
			}
		}(i)
	}

	wg.Wait()
	close(errorsChan)

	t.Logf("Completed in %v — locks: %d, unlocks: %d",
		time.Since(start), totalLocks.Load(), totalUnlocks.Load())

	errCount := 0
	for err := range errorsChan {
		t.Error(err)
		errCount++
		if errCount > 10 {
			t.Errorf("... (truncated, %d more errors)", len(errorsChan))
			break
		}
	}

	h.withLocksRLock(func() {
		if len(h.locks) != 0 {
			t.Errorf("Expected all locks released, %d remain", len(h.locks))
		}
	})
	if got := len(h.cLockedDbConn); got != defaultDatabaseCount {
		t.Errorf("Expected %d databases available, got %d", defaultDatabaseCount, got)
	}
	for connStr, c := range counters {
		if v := c.Load(); v != 0 {
			t.Errorf("Counter for %s = %d, expected 0", connStr, v)
		}
	}
	seenCount := 0
	seenConnections.Range(func(_, _ interface{}) bool { seenCount++; return true })
	if seenCount != defaultDatabaseCount {
		t.Errorf("Saw %d unique connections, expected %d", seenCount, defaultDatabaseCount)
	}
}

func TestLock_RaceWithForceUnlock(t *testing.T) {
	h := newTestHandler()

	numWorkers := 200
	var wg sync.WaitGroup
	errorsChan := make(chan error, numWorkers*2)

	activeConnections := sync.Map{}
	var forceUnlockCount, workerUnlockSuccess, workerUnlockFailed atomic.Int64

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := httptest.NewRequest("GET",
				fmt.Sprintf("/lock?marker=worker%d&password=%s", id, testPassword), nil)
			rr := httptest.NewRecorder()
			h.handleLockNoReset(rr, req)

			if rr.Code != http.StatusOK {
				errorsChan <- fmt.Errorf("worker %d: lock status %d", id, rr.Code)
				return
			}

			connStr := strings.TrimSpace(rr.Body.String())
			activeConnections.Store(connStr, id)
			time.Sleep(time.Duration(rand.Intn(30)) * time.Millisecond)
			activeConnections.Delete(connStr)

			req = httptest.NewRequest("POST",
				fmt.Sprintf("/unlock?marker=worker%d&password=%s", id, testPassword),
				strings.NewReader(connStr))
			rr = httptest.NewRecorder()
			h.handleUnlock(rr, req)

			switch rr.Code {
			case http.StatusOK:
				workerUnlockSuccess.Add(1)
			case http.StatusBadRequest:
				workerUnlockFailed.Add(1) // already force-unlocked
			default:
				errorsChan <- fmt.Errorf("worker %d: unlock unexpected status %d", id, rr.Code)
			}
		}(i)
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				var target string
				activeConnections.Range(func(k, _ interface{}) bool {
					target = k.(string)
					return false
				})
				if target != "" && h.ForceUnlock(target) {
					forceUnlockCount.Add(1)
				}
				time.Sleep(time.Duration(rand.Intn(3)) * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	close(errorsChan)

	for err := range errorsChan {
		t.Error(err)
	}

	t.Logf("Force unlocks: %d, worker success: %d, worker failed (already force-unlocked): %d",
		forceUnlockCount.Load(), workerUnlockSuccess.Load(), workerUnlockFailed.Load())

	h.withLocksRLock(func() {
		locked := len(h.locks)
		available := len(h.cLockedDbConn)
		if locked+available != defaultDatabaseCount {
			t.Errorf("Inconsistent state: %d locked + %d available = %d (want %d)",
				locked, available, locked+available, defaultDatabaseCount)
		}
	})

	h.UnlockAll()

	if got := len(h.cLockedDbConn); got != defaultDatabaseCount {
		t.Errorf("After cleanup: expected %d available, got %d", defaultDatabaseCount, got)
	}

	seen := make(map[string]bool)
	for i := 0; i < defaultDatabaseCount; i++ {
		c := <-h.cLockedDbConn
		if seen[c] {
			t.Errorf("Duplicate connection in pool: %s", c)
		}
		seen[c] = true
	}
}

func TestUnlockByMarker(t *testing.T) {
	h := newTestHandler()

	var aliceConns, bobConns []string
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/lock?marker=alice&password="+testPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLockNoReset(rr, req)
		if rr.Code == http.StatusOK {
			aliceConns = append(aliceConns, strings.TrimSpace(rr.Body.String()))
		}
	}
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/lock?marker=bob&password="+testPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLockNoReset(rr, req)
		if rr.Code == http.StatusOK {
			bobConns = append(bobConns, strings.TrimSpace(rr.Body.String()))
		}
	}

	h.withLocksRLock(func() {
		if len(h.locks) != 8 {
			t.Errorf("Expected 8 locks, got %d", len(h.locks))
		}
	})

	if count := h.UnlockByMarker("alice"); count != 5 {
		t.Errorf("Expected 5 unlocked for alice, got %d", count)
	}

	h.withLocksRLock(func() {
		if len(h.locks) != 3 {
			t.Errorf("Expected 3 locks remaining, got %d", len(h.locks))
		}
		for _, lockInfo := range h.locks {
			if lockInfo.Marker != "bob" {
				t.Errorf("Unexpected lock marker %s (expected bob)", lockInfo.Marker)
			}
		}
	})

	if count := h.UnlockByMarker("charlie"); count != 0 {
		t.Errorf("Expected 0 unlocked for charlie, got %d", count)
	}

	for _, c := range bobConns {
		h.ForceUnlock(c)
	}
}

func TestLock_VerifyNoDuplicateInChannel(t *testing.T) {
	h := newTestHandler()

	// Drain and verify initial state has no duplicates.
	seen := make(map[string]bool)
	available := len(h.cLockedDbConn)
	for i := 0; i < available; i++ {
		select {
		case c := <-h.cLockedDbConn:
			if seen[c] {
				t.Errorf("Duplicate in initial pool: %s", c)
			}
			seen[c] = true
		default:
			t.Error("Pool had fewer items than expected")
		}
	}
	if len(seen) != defaultDatabaseCount {
		t.Errorf("Expected %d unique connections, got %d", defaultDatabaseCount, len(seen))
	}
	for c := range seen {
		h.cLockedDbConn <- c
	}

	// Stress: concurrent lock/unlock cycles.
	numOps := 2000
	counters := make(map[string]*atomic.Int32)
	for c := range h.testDatabases {
		counters[c] = &atomic.Int32{}
	}

	var wg sync.WaitGroup
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := httptest.NewRequest("GET",
				fmt.Sprintf("/lock?marker=user%d&password=%s", id, testPassword), nil)
			rr := httptest.NewRecorder()
			h.handleLockNoReset(rr, req)
			if rr.Code != http.StatusOK {
				return
			}
			c := strings.TrimSpace(rr.Body.String())
			counters[c].Add(1)
			time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
			counters[c].Add(-1)

			req = httptest.NewRequest("POST",
				fmt.Sprintf("/unlock?marker=user%d&password=%s", id, testPassword),
				strings.NewReader(c))
			rr = httptest.NewRecorder()
			h.handleUnlock(rr, req)
		}(i)
	}
	wg.Wait()

	for c, ctr := range counters {
		if v := ctr.Load(); v != 0 {
			t.Errorf("Counter for %s = %d after stress test", c, v)
		}
	}

	// Re-drain and verify no duplicates.
	seen = make(map[string]bool)
	available = len(h.cLockedDbConn)
	for i := 0; i < available; i++ {
		c := <-h.cLockedDbConn
		if seen[c] {
			t.Errorf("Duplicate in pool after stress test: %s", c)
		}
		seen[c] = true
	}
	if len(seen) != defaultDatabaseCount {
		t.Errorf("Expected %d connections after stress test, got %d", defaultDatabaseCount, len(seen))
	}
}

func TestHealthCheck(t *testing.T) {
	h := newTestHandler()

	markers := []string{"test-alpha", "test-beta", "test-gamma"}
	for _, marker := range markers {
		req := httptest.NewRequest("GET", "/lock?marker="+marker+"&password="+testPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLockNoReset(rr, req)
	}

	req := httptest.NewRequest("GET", "/health-check", nil)
	rr := httptest.NewRecorder()
	h.handleHealthCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}

	var response HealthCheckResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if response.Status != "ok" {
		t.Errorf("Expected status 'ok', got %s", response.Status)
	}
	if response.LockedDatabases != 3 {
		t.Errorf("Expected 3 locked, got %d", response.LockedDatabases)
	}
	if response.TotalDatabases != defaultDatabaseCount {
		t.Errorf("Expected %d total, got %d", defaultDatabaseCount, response.TotalDatabases)
	}
	if response.FreeDatabases != defaultDatabaseCount-3 {
		t.Errorf("Expected %d free, got %d", defaultDatabaseCount-3, response.FreeDatabases)
	}
	if len(response.Locks) != 3 {
		t.Fatalf("Expected 3 lock entries, got %d", len(response.Locks))
	}
	for _, lock := range response.Locks {
		if lock.ConnString == "" {
			t.Error("Lock missing conn_string")
		}
		if lock.Marker == "" {
			t.Error("Lock missing marker")
		}
		if lock.LockedAt == "" {
			t.Error("Lock missing locked_at")
		}
	}
}

func TestUnlockAllEndpoint(t *testing.T) {
	h := newTestHandler()

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET",
			fmt.Sprintf("/lock?marker=user%d&password=%s", i, testPassword), nil)
		rr := httptest.NewRecorder()
		h.handleLockNoReset(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("Lock %d failed", i)
		}
	}

	req := httptest.NewRequest("POST", "/unlock-all?marker=admin&password="+testPassword, nil)
	rr := httptest.NewRecorder()
	h.handleUnlockAll(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unlock-all: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var response struct {
		Status   string `json:"status"`
		Unlocked int    `json:"unlocked"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if response.Unlocked != 5 {
		t.Errorf("Expected 5 unlocked, got %d", response.Unlocked)
	}

	h.withLocksRLock(func() {
		if len(h.locks) != 0 {
			t.Errorf("Expected 0 locks after unlock-all, got %d", len(h.locks))
		}
	})
}

func TestRestartEndpoint(t *testing.T) {
	h := newTestHandler()

	req := httptest.NewRequest("POST", "/restart?marker=admin&password="+testPassword, nil)
	rr := httptest.NewRecorder()
	h.handleRestart(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503 when no channel configured, got %d", rr.Code)
	}

	restartChan := make(chan RestartRequest)
	h.SetRestartRequestChan(restartChan)

	restartCalled := make(chan bool, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r := <-restartChan
		restartCalled <- true
		r.ResponseChan <- nil
	}()

	time.Sleep(10 * time.Millisecond)

	req = httptest.NewRequest("POST", "/restart?marker=admin&password="+testPassword, nil)
	rr = httptest.NewRecorder()
	h.handleRestart(rr, req)
	<-done

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	select {
	case <-restartCalled:
	default:
		t.Error("Restart request not received")
	}
}

func TestRestartEndpointAuthRequired(t *testing.T) {
	h := newTestHandler()
	h.SetRestartRequestChan(make(chan RestartRequest))

	req := httptest.NewRequest("POST", "/restart?marker=admin", nil)
	rr := httptest.NewRecorder()
	h.handleRestart(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rr.Code)
	}

	req = httptest.NewRequest("POST", "/restart?marker=admin&password=wrongpass", nil)
	rr = httptest.NewRecorder()
	h.handleRestart(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 with wrong password, got %d", rr.Code)
	}

	req = httptest.NewRequest("GET", "/restart?marker=admin&password="+testPassword, nil)
	rr = httptest.NewRecorder()
	h.handleRestart(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for GET, got %d", rr.Code)
	}
}

func TestRestartEndpointConflict(t *testing.T) {
	h := newTestHandler()
	h.SetRestartRequestChan(make(chan RestartRequest)) // nobody listening

	req := httptest.NewRequest("POST", "/restart?marker=admin&password="+testPassword, nil)
	rr := httptest.NewRecorder()
	h.handleRestart(rr, req)
	if rr.Code != http.StatusConflict {
		t.Errorf("Expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetState(t *testing.T) {
	h := newTestHandler()

	state := h.GetState()
	if state.TotalDatabases != defaultDatabaseCount {
		t.Errorf("Expected total %d, got %d", defaultDatabaseCount, state.TotalDatabases)
	}
	if state.LockedDatabases != 0 {
		t.Errorf("Expected 0 locked, got %d", state.LockedDatabases)
	}

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET",
			fmt.Sprintf("/lock?marker=user%d&password=%s", i, testPassword), nil)
		rr := httptest.NewRecorder()
		h.handleLockNoReset(rr, req)
	}

	state = h.GetState()
	if state.LockedDatabases != 5 {
		t.Errorf("Expected 5 locked, got %d", state.LockedDatabases)
	}
	if len(state.Locks) != 5 {
		t.Errorf("Expected 5 lock infos, got %d", len(state.Locks))
	}
}

// ---------------------------------------------------------------------------
// Integration tests — use httptest.Server for real streaming connections
// ---------------------------------------------------------------------------

// TestStreaming_ConnectionCloseReleasesLock verifies that closing the HTTP
// connection immediately releases the lock on the server side.
func TestStreaming_ConnectionCloseReleasesLock(t *testing.T) {
	h, server := newStreamingTestServer(t)

	connStr, body := lockStreaming(t, server.URL, "test-marker", testPassword)

	// Verify it's locked.
	h.withLocksRLock(func() {
		if _, exists := h.locks[connStr]; !exists {
			t.Error("Expected lock to be recorded")
		}
	})

	// Close the connection — this should release the lock.
	body.Close()

	err := Await(2*time.Second, func() bool {
		var exists bool
		h.withLocksRLock(func() {
			_, exists = h.locks[connStr]
		})
		return !exists
	})
	if err != nil {
		t.Errorf("Expected lock to be released after connection close: %v", err)
	}

	// Database should be back in the pool.
	if err := Await(2*time.Second, func() bool {
		return len(h.cLockedDbConn) == defaultDatabaseCount
	}); err != nil {
		t.Errorf("Expected database back in pool: %v", err)
	}
}

// TestStreaming_ForceUnlockCancelsConnection verifies that ForceUnlock wakes the
// streaming handler and cleanly releases the lock without double-returning to pool.
func TestStreaming_ForceUnlockCancelsConnection(t *testing.T) {
	h, server := newStreamingTestServer(t)

	connStr, body := lockStreaming(t, server.URL, "test-marker", testPassword)
	defer body.Close()

	if !h.ForceUnlock(connStr) {
		t.Fatal("ForceUnlock returned false")
	}

	// Lock should be gone from map.
	h.withLocksRLock(func() {
		if _, exists := h.locks[connStr]; exists {
			t.Error("Lock still in map after ForceUnlock")
		}
	})

	// Pool should be fully restored — no double-send.
	if err := Await(2*time.Second, func() bool {
		return len(h.cLockedDbConn) == defaultDatabaseCount
	}); err != nil {
		t.Errorf("Pool not restored after ForceUnlock: %v", err)
	}
}

// TestStreaming_UnlockAllCancelsAllConnections verifies that UnlockAll wakes all
// streaming handlers and restores the full pool.
func TestStreaming_UnlockAllCancelsAllConnections(t *testing.T) {
	h, server := newStreamingTestServer(t)

	const numLocks = 10
	bodies := make([]io.ReadCloser, numLocks)
	for i := 0; i < numLocks; i++ {
		_, body := lockStreaming(t, server.URL, fmt.Sprintf("marker-%d", i), testPassword)
		bodies[i] = body
	}
	defer func() {
		for _, b := range bodies {
			b.Close()
		}
	}()

	if got := h.UnlockAll(); got != numLocks {
		t.Errorf("UnlockAll returned %d, want %d", got, numLocks)
	}

	if err := Await(2*time.Second, func() bool {
		return len(h.cLockedDbConn) == defaultDatabaseCount
	}); err != nil {
		t.Errorf("Pool not fully restored after UnlockAll: %v", err)
	}

	h.withLocksRLock(func() {
		if len(h.locks) != 0 {
			t.Errorf("Expected 0 locks after UnlockAll, got %d", len(h.locks))
		}
	})
}

// TestStreaming_ConcurrentLocksAndConnectionCloses is a race condition stress test
// for streaming connections — goroutines acquire locks, hold them briefly, then
// close the connection to release.
func TestStreaming_ConcurrentLocksAndConnectionCloses(t *testing.T) {
	h, server := newStreamingTestServer(t)

	numGoroutines := 200
	cyclesPerGoroutine := 5

	var wg sync.WaitGroup
	errorsChan := make(chan error, numGoroutines*cyclesPerGoroutine)

	counters := make(map[string]*atomic.Int32)
	for c := range h.testDatabases {
		counters[c] = &atomic.Int32{}
	}

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for cycle := 0; cycle < cyclesPerGoroutine; cycle++ {
				reqURL := fmt.Sprintf("%s/lock?marker=user%d&password=%s",
					server.URL, id, testPassword)
				resp, err := client.Get(reqURL)
				if err != nil {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: GET error: %v", id, cycle, err)
					return
				}
				if resp.StatusCode != http.StatusOK {
					resp.Body.Close()
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: lock status %d", id, cycle, resp.StatusCode)
					return
				}

				buf := make([]byte, 256)
				n, _ := resp.Body.Read(buf)
				connStr := strings.TrimSpace(string(buf[:n]))

				if ctr := counters[connStr]; ctr != nil {
					if v := ctr.Add(1); v != 1 {
						errorsChan <- fmt.Errorf("goroutine %d cycle %d: RACE on %s (counter=%d)", id, cycle, connStr, v)
					}
				}

				time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)

				if ctr := counters[connStr]; ctr != nil {
					ctr.Add(-1)
				}

				// Close the connection — this is the unlock.
				resp.Body.Close()
			}
		}(i)
	}

	wg.Wait()
	close(errorsChan)

	errCount := 0
	for err := range errorsChan {
		t.Error(err)
		errCount++
		if errCount > 10 {
			break
		}
	}

	if err := Await(5*time.Second, func() bool {
		return len(h.cLockedDbConn) == defaultDatabaseCount
	}); err != nil {
		t.Errorf("Pool not fully restored after stress test: %v (available=%d, want=%d)",
			err, len(h.cLockedDbConn), defaultDatabaseCount)
	}

	h.withLocksRLock(func() {
		if len(h.locks) != 0 {
			t.Errorf("Expected 0 locks, got %d", len(h.locks))
		}
	})

	for c, ctr := range counters {
		if v := ctr.Load(); v != 0 {
			t.Errorf("Counter for %s = %d, expected 0", c, v)
		}
	}
}

// TestStreaming_AutoUnlockTimeoutReleasesLock verifies that handleLock's internal
// context.WithTimeout releases the lock after autoUnlockDuration even if the
// client never closes the connection.
func TestStreaming_AutoUnlockTimeoutReleasesLock(t *testing.T) {
	h, server := newStreamingTestServerWithAutoUnlock(t, 300*time.Millisecond)

	_, body := lockStreaming(t, server.URL, "timeout-test", testPassword)
	defer body.Close()

	h.withLocksRLock(func() {
		if len(h.locks) != 1 {
			t.Errorf("Expected 1 lock, got %d", len(h.locks))
		}
	})

	// Wait for the auto-unlock timeout to fire and release the lock.
	if err := Await(3*time.Second, func() bool {
		return len(h.cLockedDbConn) == defaultDatabaseCount
	}); err != nil {
		t.Errorf("Lock not released after auto-unlock timeout: %v", err)
	}

	h.withLocksRLock(func() {
		if len(h.locks) != 0 {
			t.Errorf("Expected 0 locks after timeout, got %d", len(h.locks))
		}
	})
}

// TestStreaming_ForceUnlockAndClientCloseRace stresses the race between a client
// closing its connection and a concurrent ForceUnlock to ensure we never
// double-return a database to the pool.
func TestStreaming_ForceUnlockAndClientCloseRace(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		h, server := newStreamingTestServer(t)

		connStr, body := lockStreaming(t, server.URL, "race-test", testPassword)

		// Race: client closes AND server force-unlocks at the same time.
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Add(-1); body.Close() }()
		go func() { defer wg.Add(-1); h.ForceUnlock(connStr) }()
		wg.Wait()

		// Pool must have exactly defaultDatabaseCount — no duplicates.
		if err := Await(2*time.Second, func() bool {
			return len(h.cLockedDbConn) == defaultDatabaseCount
		}); err != nil {
			t.Fatalf("iteration %d: pool count wrong after race: available=%d want=%d",
				iteration, len(h.cLockedDbConn), defaultDatabaseCount)
		}

		// Drain and check for duplicates.
		seen := make(map[string]bool)
		for i := 0; i < defaultDatabaseCount; i++ {
			c := <-h.cLockedDbConn
			if seen[c] {
				t.Fatalf("iteration %d: DUPLICATE in pool: %s", iteration, c)
			}
			seen[c] = true
		}

		server.Close()
	}
}
