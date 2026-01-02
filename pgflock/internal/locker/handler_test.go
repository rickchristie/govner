package locker

import (
	"fmt"
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

// newTestHandler creates a handler for testing without database reset
func newTestHandler() *Handler {
	return newTestHandlerWithCleanupInterval(1 * time.Minute)
}

// newTestHandlerWithCleanupInterval creates a handler with configurable cleanup interval
func newTestHandlerWithCleanupInterval(cleanupInterval time.Duration) *Handler {
	cfg := testConfig()

	// Build test databases map from config
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
		stateUpdateChan:       nil, // No TUI updates in tests
	}

	// Initially all databases are available
	for connStr := range testDatabases {
		h.cLockedDbConn <- connStr
	}

	// Start cleanup routine for expired locks
	go h.cleanupExpiredLocks()

	return h
}

// handleLockNoReset is a test version of handleLock that skips database reset
func (h *Handler) handleLockNoReset(resp http.ResponseWriter, req *http.Request) {
	marker, valid := h.validateAuth(req)
	if !valid {
		http.Error(resp, "Invalid marker or password", http.StatusUnauthorized)
		return
	}

	// Increment waiting count
	h.waitingCount.Add(1)
	h.sendStateUpdate()
	defer func() {
		h.waitingCount.Add(-1)
		h.sendStateUpdate()
	}()

	// Wait for a database to be freed or request context to be cancelled
	select {
	case connStr := <-h.cLockedDbConn:
		// Skip database reset in tests

		// Record the lock
		h.withLocksLock(func() {
			h.locks[connStr] = &LockInfo{
				ConnString: connStr,
				Marker:     marker,
				LockedAt:   time.Now(),
			}
		})

		_, err := resp.Write([]byte(connStr))
		if err != nil {
			return
		}

		h.sendStateUpdate()

	case <-req.Context().Done():
		http.Error(resp, "Request cancelled or timed out", http.StatusRequestTimeout)
	}
}

// Await waits for an event to occur within the timeout duration
func Await(timeoutDuration time.Duration, event func() bool) error {
	timeout := time.After(timeoutDuration)
	for {
		select {
		case <-timeout:
			return fmt.Errorf("waiting for an event that did not arrive")
		default:
			if event() {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

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

			// Also test unlock endpoint with same credentials
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

	// Test lock with valid credentials
	req := httptest.NewRequest("GET", "/lock?marker=testuser&password="+testPassword, nil)
	rr := httptest.NewRecorder()

	h.handleLockNoReset(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	connStr := strings.TrimSpace(rr.Body.String())
	if connStr == "" {
		t.Error("Expected connection string, got empty response")
	}

	// Test unlock with the same connection string
	unlockURL := "/unlock?marker=testuser&password=" + testPassword
	req = httptest.NewRequest("POST", unlockURL, strings.NewReader(connStr))
	rr = httptest.NewRecorder()

	h.handleUnlock(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestAutoUnlockAfterTimeout(t *testing.T) {
	// Create handler with faster cleanup interval for testing (3 seconds)
	h := newTestHandlerWithCleanupInterval(3 * time.Second)
	// Override auto-unlock duration for test
	h.autoUnlockDuration = 30 * time.Minute

	// Lock a database
	req := httptest.NewRequest("GET", "/lock?marker=testuser&password="+testPassword, nil)
	rr := httptest.NewRecorder()
	h.handleLockNoReset(rr, req)

	connStr := strings.TrimSpace(rr.Body.String())

	// Simulate the lock being old by modifying the timestamp
	h.withLocksLock(func() {
		if lockInfo, exists := h.locks[connStr]; exists {
			lockInfo.LockedAt = time.Now().Add(-31 * time.Minute) // 31 minutes ago
		}
	})

	// Use Await to wait for the automatic cleanup to remove the lock
	err := Await(10*time.Second, func() bool {
		var exists bool
		h.withLocksRLock(func() {
			_, exists = h.locks[connStr]
		})
		return !exists // Return true when lock is removed
	})

	if err != nil {
		t.Errorf("Expected lock to be automatically removed after 30 minutes, but timeout occurred: %v", err)
	}
}

func TestLock_BlockWhenExhausted(t *testing.T) {
	h := newTestHandler()

	// Lock all available databases
	var lockedConnections []string
	for i := 0; i < defaultDatabaseCount; i++ {
		req := httptest.NewRequest("GET", "/lock?marker=testuser&password="+testPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLockNoReset(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("Expected lock %d to succeed, got status %d", i+1, rr.Code)
		}

		connStr := strings.TrimSpace(rr.Body.String())
		lockedConnections = append(lockedConnections, connStr)
	}

	// Spawn a "otherLocker" goroutine that tries to lock another database
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

	// Assert using Await that after waiting for 5 seconds, the request is blocked and doesn't actually return
	err := Await(5*time.Second, func() bool {
		otherLockerMu.Lock()
		defer otherLockerMu.Unlock()
		return otherLockerDone
	})

	if err == nil {
		t.Error("Expected otherLocker request to be blocked when all databases are exhausted, but it completed")
	}

	// Randomly select one locked database connection string, and call /unlock to unlock the connection
	selectedIndex := rand.Intn(len(lockedConnections))
	selectedConnStr := lockedConnections[selectedIndex]

	unlockURL := "/unlock?marker=testuser&password=" + testPassword
	req := httptest.NewRequest("POST", unlockURL, strings.NewReader(selectedConnStr))
	rr := httptest.NewRecorder()
	h.handleUnlock(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected unlock to succeed, got status %d", rr.Code)
	}

	// Assert using Await that the "otherLocker" request finally gets a response
	err = Await(10*time.Second, func() bool {
		otherLockerMu.Lock()
		defer otherLockerMu.Unlock()
		return otherLockerDone
	})

	if err != nil {
		t.Errorf("Expected otherLocker request to complete after unlock, but it timed out: %v", err)
	}

	// Verify the returned database connection is the one we chose to unlock randomly
	otherLockerMu.Lock()
	defer otherLockerMu.Unlock()

	if otherLockerResponse.Code != http.StatusOK {
		t.Errorf("Expected otherLocker to get status 200, got %d", otherLockerResponse.Code)
	}

	returnedConnStr := strings.TrimSpace(otherLockerResponse.Body.String())
	if returnedConnStr != selectedConnStr {
		t.Errorf("Expected otherLocker to get connection %s, but got %s", selectedConnStr, returnedConnStr)
	}
}

func TestLock_RaceConditionStressTest(t *testing.T) {
	h := newTestHandler()
	numGoroutines := 50 * defaultDatabaseCount // 50x the default database count

	var wg sync.WaitGroup
	errorsChan := make(chan error, numGoroutines)

	// Track ownership of each connection using atomic counters.
	counters := make(map[string]*atomic.Int32)
	for connStr := range h.testDatabases {
		counters[connStr] = &atomic.Int32{}
		counters[connStr].Store(0)
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			// Lock a database (this will block if all databases are locked).
			req := httptest.NewRequest("GET", fmt.Sprintf("/lock?marker=user%d&password=%s", goroutineID, testPassword), nil)
			rr := httptest.NewRecorder()
			h.handleLockNoReset(rr, req)

			if rr.Code != http.StatusOK {
				errorsChan <- fmt.Errorf("goroutine %d: lock failed with status %d", goroutineID, rr.Code)
				return
			}

			connStr := strings.TrimSpace(rr.Body.String())

			// Check if this connection is already held by another goroutine.
			ret := counters[connStr].Add(1)
			if ret != 1 {
				errorsChan <- fmt.Errorf("goroutine %d: connection %s is already held by another goroutine", goroutineID, connStr)
				return
			}

			// Hold the lock for a randomized time (0-500ms)
			holdTime := time.Duration(rand.Intn(500)) * time.Millisecond
			time.Sleep(holdTime)

			// Decrement the counter before unlocking.
			if counters[connStr].Add(-1) != 0 {
				errorsChan <- fmt.Errorf("goroutine %d: connection %s counter is not 0 after decrement, expected 0", goroutineID, connStr)
				return
			}

			// Release the lock
			unlockURL := fmt.Sprintf("/unlock?marker=user%d&password=%s", goroutineID, testPassword)
			req = httptest.NewRequest("POST", unlockURL, strings.NewReader(connStr))
			rr = httptest.NewRecorder()
			h.handleUnlock(rr, req)

			if rr.Code != http.StatusOK {
				errorsChan <- fmt.Errorf("goroutine %d: unlock failed with status %d", goroutineID, rr.Code)
				return
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errorsChan)

	// Check for any errors
	for err := range errorsChan {
		t.Error(err)
	}

	// Assert that at the end of the test, after all goroutines have released their locks, all databases are unlocked
	h.withLocksRLock(func() {
		if len(h.locks) != 0 {
			t.Errorf("Expected all databases to be unlocked at end of test, but %d locks remain", len(h.locks))
		}
	})

	// Verify that all databases are back in the available pool
	availableCount := len(h.cLockedDbConn)
	if availableCount != defaultDatabaseCount {
		t.Errorf("Expected %d databases to be available, but got %d", defaultDatabaseCount, availableCount)
	}
}

func TestUnlockByMarker(t *testing.T) {
	h := newTestHandler()

	// Lock 5 databases with marker "alice"
	var aliceConnections []string
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/lock?marker=alice&password="+testPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLockNoReset(rr, req)
		if rr.Code == http.StatusOK {
			aliceConnections = append(aliceConnections, strings.TrimSpace(rr.Body.String()))
		}
	}

	// Lock 3 databases with marker "bob"
	var bobConnections []string
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/lock?marker=bob&password="+testPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLockNoReset(rr, req)
		if rr.Code == http.StatusOK {
			bobConnections = append(bobConnections, strings.TrimSpace(rr.Body.String()))
		}
	}

	// Verify that we have 8 locks total
	h.withLocksRLock(func() {
		if len(h.locks) != 8 {
			t.Errorf("Expected 8 locks, got %d", len(h.locks))
		}
	})

	// Use UnlockByMarker to unlock all databases locked by "alice"
	count := h.UnlockByMarker("alice")
	if count != 5 {
		t.Errorf("Expected to unlock 5 databases, unlocked %d", count)
	}

	// Verify that only bob's locks remain (3 locks)
	h.withLocksRLock(func() {
		if len(h.locks) != 3 {
			t.Errorf("Expected 3 locks remaining (bob's), got %d", len(h.locks))
		}

		// Verify all remaining locks are bob's
		for _, lockInfo := range h.locks {
			if lockInfo.Marker != "bob" {
				t.Errorf("Expected all remaining locks to be bob's, found lock owned by %s", lockInfo.Marker)
			}
		}
	})

	// Verify alice's connections are back in the pool
	for _, connStr := range aliceConnections {
		h.withLocksRLock(func() {
			if _, exists := h.locks[connStr]; exists {
				t.Errorf("Expected alice's connection %s to be unlocked", connStr)
			}
		})
	}

	// Test unlocking by marker when no databases are locked by that user
	count = h.UnlockByMarker("charlie")
	if count != 0 {
		t.Errorf("Expected to unlock 0 databases for charlie, unlocked %d", count)
	}

	// Verify bob's locks are still there (no change)
	h.withLocksRLock(func() {
		if len(h.locks) != 3 {
			t.Errorf("Expected 3 locks remaining after unlocking non-existent user, got %d", len(h.locks))
		}
	})

	// Clean up bob's connections
	for _, connStr := range bobConnections {
		h.ForceUnlock(connStr)
	}
}

// TestLock_MassiveRaceConditionStressTest bombards the server with thousands of concurrent requests
func TestLock_MassiveRaceConditionStressTest(t *testing.T) {
	h := newTestHandler()

	// 5000 goroutines competing for 25 databases = 200x contention ratio
	numGoroutines := 5000
	// Each goroutine will do multiple lock/unlock cycles
	cyclesPerGoroutine := 3

	var wg sync.WaitGroup
	errorsChan := make(chan error, numGoroutines*cyclesPerGoroutine)

	// Track ownership of each connection using atomic counters.
	counters := make(map[string]*atomic.Int32)
	for connStr := range h.testDatabases {
		counters[connStr] = &atomic.Int32{}
	}

	// Track total successful locks and unlocks for final verification
	var totalLocks atomic.Int64
	var totalUnlocks atomic.Int64

	// Also track unique connections seen to ensure we're not getting duplicates
	seenConnections := sync.Map{}

	t.Logf("Starting massive stress test: %d goroutines x %d cycles = %d total lock attempts",
		numGoroutines, cyclesPerGoroutine, numGoroutines*cyclesPerGoroutine)

	startTime := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for cycle := 0; cycle < cyclesPerGoroutine; cycle++ {
				// Lock a database
				req := httptest.NewRequest("GET",
					fmt.Sprintf("/lock?marker=user%d&password=%s", goroutineID, testPassword), nil)
				rr := httptest.NewRecorder()
				h.handleLockNoReset(rr, req)

				if rr.Code != http.StatusOK {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: lock failed with status %d",
						goroutineID, cycle, rr.Code)
					return
				}

				connStr := strings.TrimSpace(rr.Body.String())
				totalLocks.Add(1)

				// Verify this connection is not already held
				counter := counters[connStr]
				if counter == nil {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: got unknown connection %s",
						goroutineID, cycle, connStr)
					return
				}

				// Increment counter - must be exactly 1 after increment
				if val := counter.Add(1); val != 1 {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: RACE DETECTED! connection %s counter is %d (expected 1)",
						goroutineID, cycle, connStr, val)
					// Don't return - still try to clean up
				}

				// Record that we've seen this connection
				seenConnections.Store(connStr, true)

				// Hold the lock for a very short random time (0-10ms) to maximize contention
				holdTime := time.Duration(rand.Intn(10)) * time.Millisecond
				time.Sleep(holdTime)

				// Decrement counter before unlock - must be exactly 0 after decrement
				if val := counter.Add(-1); val != 0 {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: RACE DETECTED! connection %s counter is %d after decrement (expected 0)",
						goroutineID, cycle, connStr, val)
				}

				// Unlock the database
				unlockURL := fmt.Sprintf("/unlock?marker=user%d&password=%s", goroutineID, testPassword)
				req = httptest.NewRequest("POST", unlockURL, strings.NewReader(connStr))
				rr = httptest.NewRecorder()
				h.handleUnlock(rr, req)

				if rr.Code != http.StatusOK {
					errorsChan <- fmt.Errorf("goroutine %d cycle %d: unlock failed with status %d",
						goroutineID, cycle, rr.Code)
					return
				}

				totalUnlocks.Add(1)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errorsChan)

	elapsed := time.Since(startTime)
	t.Logf("Stress test completed in %v", elapsed)
	t.Logf("Total locks: %d, Total unlocks: %d", totalLocks.Load(), totalUnlocks.Load())

	// Check for any errors
	errorCount := 0
	for err := range errorsChan {
		t.Error(err)
		errorCount++
		if errorCount > 10 {
			t.Errorf("... and %d more errors (truncated)", len(errorsChan))
			break
		}
	}

	// Verify all databases are unlocked
	h.withLocksRLock(func() {
		if len(h.locks) != 0 {
			t.Errorf("Expected all databases to be unlocked, but %d locks remain", len(h.locks))
			for connStr, lockInfo := range h.locks {
				t.Errorf("  Remaining lock: %s by %s", connStr, lockInfo.Marker)
			}
		}
	})

	// Verify all databases are back in the pool
	availableCount := len(h.cLockedDbConn)
	if availableCount != defaultDatabaseCount {
		t.Errorf("Expected %d databases available, got %d", defaultDatabaseCount, availableCount)
	}

	// Verify all counters are back to zero
	for connStr, counter := range counters {
		if val := counter.Load(); val != 0 {
			t.Errorf("Counter for %s is %d, expected 0", connStr, val)
		}
	}

	// Verify we saw all databases (they were all used at some point)
	seenCount := 0
	seenConnections.Range(func(key, value interface{}) bool {
		seenCount++
		return true
	})
	if seenCount != defaultDatabaseCount {
		t.Errorf("Only saw %d unique connections, expected %d", seenCount, defaultDatabaseCount)
	}
}

// TestLock_RaceWithForceUnlock tests that force-unlock doesn't corrupt system state.
func TestLock_RaceWithForceUnlock(t *testing.T) {
	h := newTestHandler()

	numWorkers := 200
	var wg sync.WaitGroup
	errorsChan := make(chan error, numWorkers*2)

	// Track connections that are currently locked by workers (for force-unlock to target)
	activeConnections := sync.Map{}

	var forceUnlockCount atomic.Int64
	var workerUnlockSuccess atomic.Int64
	var workerUnlockFailed atomic.Int64

	t.Logf("Starting force-unlock consistency test with %d workers", numWorkers)

	// Start worker goroutines that lock/unlock
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			// Lock
			req := httptest.NewRequest("GET",
				fmt.Sprintf("/lock?marker=worker%d&password=%s", goroutineID, testPassword), nil)
			rr := httptest.NewRecorder()
			h.handleLockNoReset(rr, req)

			if rr.Code != http.StatusOK {
				errorsChan <- fmt.Errorf("worker %d: lock failed with status %d", goroutineID, rr.Code)
				return
			}

			connStr := strings.TrimSpace(rr.Body.String())

			// Register as active (might be force-unlocked)
			activeConnections.Store(connStr, goroutineID)

			// Hold for random time
			time.Sleep(time.Duration(rand.Intn(30)) * time.Millisecond)

			activeConnections.Delete(connStr)

			// Try to unlock - might fail if already force-unlocked
			unlockURL := fmt.Sprintf("/unlock?marker=worker%d&password=%s", goroutineID, testPassword)
			req = httptest.NewRequest("POST", unlockURL, strings.NewReader(connStr))
			rr = httptest.NewRecorder()
			h.handleUnlock(rr, req)

			if rr.Code == http.StatusOK {
				workerUnlockSuccess.Add(1)
			} else if rr.Code == http.StatusBadRequest {
				// Already force-unlocked - expected
				workerUnlockFailed.Add(1)
			} else {
				errorsChan <- fmt.Errorf("worker %d: unlock got unexpected status %d", goroutineID, rr.Code)
			}
		}(i)
	}

	// Start goroutines that randomly force-unlock
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(adminID int) {
			defer wg.Done()

			for j := 0; j < 30; j++ {
				// Pick a connection to force-unlock
				var targetConn string
				activeConnections.Range(func(key, value interface{}) bool {
					targetConn = key.(string)
					return false // Stop after first one
				})

				if targetConn == "" {
					time.Sleep(time.Millisecond)
					continue
				}

				// Force unlock it using the handler method
				if h.ForceUnlock(targetConn) {
					forceUnlockCount.Add(1)
				}

				time.Sleep(time.Duration(rand.Intn(3)) * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	close(errorsChan)

	// Check for errors
	for err := range errorsChan {
		t.Error(err)
	}

	t.Logf("Force unlocks: %d, Worker unlocks success: %d, Worker unlocks failed (force-unlocked): %d",
		forceUnlockCount.Load(), workerUnlockSuccess.Load(), workerUnlockFailed.Load())

	// Final state verification - this is the critical check
	// The sum of locked + available must equal total databases
	h.withLocksRLock(func() {
		lockedCount := len(h.locks)
		availableCount := len(h.cLockedDbConn)
		total := lockedCount + availableCount

		if total != defaultDatabaseCount {
			t.Errorf("CRITICAL: Inconsistent state! %d locked + %d available = %d (expected %d)",
				lockedCount, availableCount, total, defaultDatabaseCount)
		}
	})

	// Clean up any remaining locks
	h.UnlockAll()

	// Verify all connections are back
	if len(h.cLockedDbConn) != defaultDatabaseCount {
		t.Errorf("After cleanup: expected %d available, got %d", defaultDatabaseCount, len(h.cLockedDbConn))
	}

	// Verify no duplicates in channel
	seen := make(map[string]bool)
	for i := 0; i < defaultDatabaseCount; i++ {
		connStr := <-h.cLockedDbConn
		if seen[connStr] {
			t.Errorf("CRITICAL: Duplicate connection in channel after test: %s", connStr)
		}
		seen[connStr] = true
	}
}

// TestLock_VerifyNoDuplicateInChannel verifies the channel never contains duplicate connection strings
func TestLock_VerifyNoDuplicateInChannel(t *testing.T) {
	h := newTestHandler()

	// Drain the channel and verify no duplicates
	seen := make(map[string]bool)
	available := len(h.cLockedDbConn)

	for i := 0; i < available; i++ {
		select {
		case connStr := <-h.cLockedDbConn:
			if seen[connStr] {
				t.Errorf("Duplicate connection string in channel: %s", connStr)
			}
			seen[connStr] = true
		default:
			t.Errorf("Channel had fewer items than expected")
		}
	}

	if len(seen) != defaultDatabaseCount {
		t.Errorf("Expected %d unique connections, got %d", defaultDatabaseCount, len(seen))
	}

	// Put them back
	for connStr := range seen {
		h.cLockedDbConn <- connStr
	}

	// Now run a stress test and verify again
	numOps := 2000
	var wg sync.WaitGroup

	counters := make(map[string]*atomic.Int32)
	for connStr := range h.testDatabases {
		counters[connStr] = &atomic.Int32{}
	}

	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Lock
			req := httptest.NewRequest("GET",
				fmt.Sprintf("/lock?marker=user%d&password=%s", id, testPassword), nil)
			rr := httptest.NewRecorder()
			h.handleLockNoReset(rr, req)

			if rr.Code != http.StatusOK {
				return
			}

			connStr := strings.TrimSpace(rr.Body.String())
			counters[connStr].Add(1)

			time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)

			counters[connStr].Add(-1)

			// Unlock
			req = httptest.NewRequest("POST",
				fmt.Sprintf("/unlock?marker=user%d&password=%s", id, testPassword),
				strings.NewReader(connStr))
			rr = httptest.NewRecorder()
			h.handleUnlock(rr, req)
		}(i)
	}

	wg.Wait()

	// Verify all counters are 0
	for connStr, counter := range counters {
		if val := counter.Load(); val != 0 {
			t.Errorf("Counter for %s is %d after test, expected 0", connStr, val)
		}
	}

	// Drain and verify no duplicates again
	seen = make(map[string]bool)
	available = len(h.cLockedDbConn)

	for i := 0; i < available; i++ {
		connStr := <-h.cLockedDbConn
		if seen[connStr] {
			t.Errorf("Duplicate connection string after stress test: %s", connStr)
		}
		seen[connStr] = true
	}

	if len(seen) != defaultDatabaseCount {
		t.Errorf("Expected %d connections after stress test, got %d", defaultDatabaseCount, len(seen))
	}
}

func TestHealthCheck(t *testing.T) {
	h := newTestHandler()

	// Lock a few databases
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/lock?marker=testuser&password="+testPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLockNoReset(rr, req)
	}

	// Check health endpoint
	req := httptest.NewRequest("GET", "/health-check", nil)
	rr := httptest.NewRecorder()
	h.handleHealthCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("Expected status ok in response, got %s", body)
	}
	if !strings.Contains(body, `"locked":3`) {
		t.Errorf("Expected locked:3 in response, got %s", body)
	}
}

func TestGetState(t *testing.T) {
	h := newTestHandler()

	// Initial state
	state := h.GetState()
	if state.TotalDatabases != defaultDatabaseCount {
		t.Errorf("Expected total %d, got %d", defaultDatabaseCount, state.TotalDatabases)
	}
	if state.LockedDatabases != 0 {
		t.Errorf("Expected 0 locked, got %d", state.LockedDatabases)
	}

	// Lock some databases
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", fmt.Sprintf("/lock?marker=user%d&password=%s", i, testPassword), nil)
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
