package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const defaultDatabaseCount = 25

func TestMain(m *testing.M) {
	// Initialize with default config for tests
	InitFromConfig(DefaultConfig())
	os.Exit(m.Run())
}

// Await waits for an event to occur within the timeout duration
func Await(timeoutDuration time.Duration, event func() bool) error {
	now := time.Now()
	timeout := time.After(timeoutDuration)
	for {
		select {
		case <-timeout:
			return fmt.Errorf("waiting for an event that did not arrive :(")
		default:
			if event() {
				log.Println("Event received after: " + time.Since(now).String())
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func TestAuthValidation_LockUnlock(t *testing.T) {
	h := NewHandler()

	tests := []struct {
		name     string
		username string
		password string
		expected bool
	}{
		{"Valid credentials", "testuser", dbLockerPassword, true},
		{"Empty username", "", dbLockerPassword, false},
		{"Wrong password", "testuser", "wrongpassword", false},
		{"Both wrong", "testuser", "wrongpassword", false},
		{"Empty both", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name+" (lock and unlock)", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/lock?username="+tt.username+"&password="+tt.password, nil)
			username, valid := h.validateAuth(req)

			if valid != tt.expected {
				t.Errorf("validateAuth() = %v, want %v", valid, tt.expected)
			}

			if valid && username != tt.username {
				t.Errorf("Expected username %s, got %s", tt.username, username)
			}

			// Also test unlock endpoint with same credentials
			req = httptest.NewRequest("GET", "/unlock?username="+tt.username+"&password="+tt.password+"&conn=someconn", nil)
			username, valid = h.validateAuth(req)

			if valid != tt.expected {
				t.Errorf("validateAuth() for unlock = %v, want %v", valid, tt.expected)
			}

			if valid && username != tt.username {
				t.Errorf("Expected username %s for unlock, got %s", tt.username, username)
			}
		})
	}
}

func TestLockUnlockFlow(t *testing.T) {
	h := NewHandler()

	// Test lock with valid credentials
	req := httptest.NewRequest("GET", "/lock?username=testuser&password="+dbLockerPassword, nil)
	rr := httptest.NewRecorder()

	h.handleLock(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	connStr := strings.TrimSpace(rr.Body.String())
	if connStr == "" {
		t.Error("Expected connection string, got empty response")
	}

	// Test unlock with the same connection string
	unlockURL := "/unlock?username=testuser&password=" + dbLockerPassword
	req = httptest.NewRequest("POST", unlockURL, strings.NewReader(connStr))
	rr = httptest.NewRecorder()

	h.handleUnlock(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestAdminAuth_NoCookie(t *testing.T) {
	h := NewHandler()

	// First, lock a database to have some state to verify
	lockReq := httptest.NewRequest("GET", "/lock?username=testuser&password="+dbLockerPassword, nil)
	lockRr := httptest.NewRecorder()
	h.handleLock(lockRr, lockReq)
	lockedConnStr := strings.TrimSpace(lockRr.Body.String())

	// Test admin page without login
	req := httptest.NewRequest("GET", "/admin", nil)
	rr := httptest.NewRecorder()

	h.handleAdmin(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200 (login page), got %d", rr.Code)
	}

	// Check that response contains login form
	body := rr.Body.String()
	if !strings.Contains(body, "Password:") || !strings.Contains(body, "Login") {
		t.Error("Expected login form in response")
	}

	// Also test that force unlock API cannot be accessed without cookie
	req = httptest.NewRequest("POST", "/admin/force-unlock", strings.NewReader("conn=someconn"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr = httptest.NewRecorder()

	h.handleAdminForceUnlock(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 for force unlock without cookie, got %d", rr.Code)
	}

	// Assert that handler state hasn't changed - the previously locked connection should still be locked
	h.withLocksRLock(func() {
		if _, exists := h.locks[lockedConnStr]; !exists {
			t.Error("Expected previously locked connection to still be locked after failed admin requests")
		}
	})
}

func TestAdminAuth_InvalidCookie(t *testing.T) {
	h := NewHandler()

	// First, lock a database to have some state to verify
	lockReq := httptest.NewRequest("GET", "/lock?username=testuser&password="+dbLockerPassword, nil)
	lockRr := httptest.NewRecorder()
	h.handleLock(lockRr, lockReq)
	lockedConnStr := strings.TrimSpace(lockRr.Body.String())

	// Test admin page with invalid session ID
	req := httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: "invalid-session-id"})
	rr := httptest.NewRecorder()

	h.handleAdmin(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200 (login page), got %d", rr.Code)
	}

	// Check that response contains login form (since session ID is invalid)
	body := rr.Body.String()
	if !strings.Contains(body, "Password:") || !strings.Contains(body, "Login") {
		t.Error("Expected login form in response for invalid session")
	}

	// Also test that force unlock API cannot be accessed with invalid cookie
	req = httptest.NewRequest("POST", "/admin/force-unlock", strings.NewReader("conn=someconn"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: "invalid-session-id"})
	rr = httptest.NewRecorder()

	h.handleAdminForceUnlock(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 for force unlock with invalid cookie, got %d", rr.Code)
	}

	// Assert that handler state hasn't changed - the previously locked connection should still be locked
	h.withLocksRLock(func() {
		if _, exists := h.locks[lockedConnStr]; !exists {
			t.Error("Expected previously locked connection to still be locked after failed admin requests")
		}
	})
}

func TestAdminLoginPost(t *testing.T) {
	h := NewHandler()

	// Test login with correct password
	form := url.Values{}
	form.Set("password", dbLockerPassword)

	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h.handleAdminLogin(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303 (redirect), got %d", rr.Code)
	}

	// Check for session cookie
	cookies := rr.Header()["Set-Cookie"]
	var sessionCookie string
	found := false
	for _, cookie := range cookies {
		if strings.Contains(cookie, "admin_session=") {
			found = true
			// Extract the session value
			parts := strings.Split(cookie, "=")
			if len(parts) > 1 {
				sessionValue := strings.Split(parts[1], ";")[0]
				sessionCookie = sessionValue
			}
			break
		}
	}
	if !found {
		t.Error("Expected admin_session cookie to be set")
	}

	// Test that with the cookie, we can now view /admin page
	req = httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: sessionCookie})
	rr = httptest.NewRecorder()

	h.handleAdmin(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200 for admin page with valid cookie, got %d", rr.Code)
	}

	// Check that response contains admin dashboard (not login form)
	body := rr.Body.String()
	if !strings.Contains(body, "DB Locker (") {
		t.Error("Expected admin dashboard in response")
	}
	if strings.Contains(body, "login") {
		t.Error("Expected admin dashboard to not contain login form")
	}

	// Test that with the cookie, we can trigger force unlock
	// First, lock a database for testing
	lockReq := httptest.NewRequest("GET", "/lock?username=testuser&password="+dbLockerPassword, nil)
	lockRr := httptest.NewRecorder()
	h.handleLock(lockRr, lockReq)

	if lockRr.Code != http.StatusOK {
		t.Errorf("Expected lock to succeed, got status %d", lockRr.Code)
	}

	lockedConnStr := strings.TrimSpace(lockRr.Body.String())

	// Test force unlock with valid cookie
	forceUnlockForm := url.Values{}
	forceUnlockForm.Set("conn", lockedConnStr)
	req = httptest.NewRequest("POST", "/admin/force-unlock", strings.NewReader(forceUnlockForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: sessionCookie})
	rr = httptest.NewRecorder()

	h.handleAdminForceUnlock(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303 for force unlock with valid cookie, got %d", rr.Code)
	}

	// Verify the database was actually unlocked
	h.withLocksRLock(func() {
		if _, exists := h.locks[lockedConnStr]; exists {
			t.Error("Expected database to be unlocked after force unlock")
		}
	})
}

func TestAutoUnlockAfterTimeout(t *testing.T) {
	// Create handler with faster cleanup interval for testing (3 seconds)
	h := NewHandlerWithCleanupInterval(3 * time.Second)

	// Lock a database
	req := httptest.NewRequest("GET", "/lock?username=testuser&password="+dbLockerPassword, nil)
	rr := httptest.NewRecorder()
	h.handleLock(rr, req)

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
	h := NewHandler()

	// Lock all available databases
	var lockedConnections []string
	for i := 0; i < defaultDatabaseCount; i++ {
		req := httptest.NewRequest("GET", "/lock?username=testuser&password="+dbLockerPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLock(rr, req)

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
		req := httptest.NewRequest("GET", "/lock?username=otheruser&password="+dbLockerPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLock(rr, req)

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

	unlockURL := "/unlock?username=testuser&password=" + dbLockerPassword
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
	// cannot use goleak until we update handler to have graceful shutdown (sync).
	//defer goleak.VerifyNone(t)

	h := NewHandler()
	numGoroutines := 50 * defaultDatabaseCount // 50x the default database count

	var wg sync.WaitGroup
	errorsChan := make(chan error, numGoroutines)

	// As with all race condition tests, there is no guaranteed way to ensure that race conditions will occur. However,
	// we can set up a stress test that has a high chance of triggering race condition.
	//
	// We don't generally want to track lock ownership in detail, as doing so would likely require us to serialize the
	// lock and unlock operation, which would defeat the purpose of this test. We have these requirements:
	//	- Detect when a connection lock is held by multiple goroutines, at the same time.
	//	- Detection must not block each other, so all goroutines are not serialized by anything other than the server
	//	  that we're testing.
	//
	// What we want to detect are these scenarios:
	//
	//   goroutine 1: L(A)------------------------------------------------------UL(A)
	//   goroutine 2:       L(A)-------------------------------------UL(A)
	//   time: ------------------------------------------------------------------------------------------------------>
	//	 Legend: L(A) lock successfully acquired on connection A, UL(A) unlock connection A successful.
	//
	// We want to ensure that L(A) and UL(A) are always interleaving and serialized, to illustrate:
	//   L(A) --> UL(A) --> L(A) --> UL(A)
	//
	// This is wrong (another lock happens before unlock on the same resource):
	//   L(A) --> L(A) --> UL(A) --> UL(A)
	//
	// We can have a check that verifies that no two goroutines hold the same connection string at the same time with
	// a counter. Each database connection string has a counter incremented after a goroutine locks it and
	// decremented before the goroutine unlocks it.
	//
	// With serial operations, the counter should always be 1 after a lock and 0 before unlocked. To illustrate:
	//
	// The operation becomes:
	//   L(A) --> INC(1) --> hold-lock --> DEC(1) --> UL(A)
	//
	// When lock is given when still held, the value after increment and decrement will be different:
	//
	//   goroutine 1: L(A)-INC(A,1,1)--------------------------------------------------DEC(A,-1,1)-UL(A)
	//   goroutine 2:         L(A)-INC(A,1,2)-----------------------------------------------------DEC(A,-1,0)--UL(A)
	//   time: ------------------------------------------------------------------------------------------------------>
	//	 Legend: L(A) lock successfully acquired on connection A, UL(A) unlock connection A successful.
	//			 INC(A,1,2) increment connection A counter by 1, result after increment is 2.
	//			 DEC(A,-1,0) decrement connection A counter by -1, result after decrement is 1.
	//
	// In the graph above, when INC(A,1,2) happened, we know that another goroutine already holds the lock on A.
	// Also, when DEC(A,-1,1) happened, we know that the lock is also held by another goroutine.
	//
	// We can use map because we're not going to write to it concurrently, only read the value of the pointers, which
	// will never change.
	counters := make(map[string]*atomic.Int32)
	for connStr := range testDatabases {
		counters[connStr] = &atomic.Int32{}
		counters[connStr].Store(0)
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			// Lock a database (this will block if all databases are locked).
			// When we get the response, no other goroutine should have the same connection string.
			req := httptest.NewRequest("GET", fmt.Sprintf("/lock?username=user%d&password=%s", goroutineID, dbLockerPassword), nil)
			rr := httptest.NewRecorder()
			h.handleLock(rr, req)

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
			// We can't decrement after unlocking, because after the unlock and before the decrement, another goroutine
			// might have already secured the lock on the same connection string. To prevent false positives, we
			// decrement the counter before unlocking.
			if counters[connStr].Add(-1) != 0 {
				errorsChan <- fmt.Errorf("goroutine %d: connection %s counter is not 0 after decrement, expected 0", goroutineID, connStr)
				return
			}

			// Release the lock
			unlockURL := fmt.Sprintf("/unlock?username=user%d&password=%s", goroutineID, dbLockerPassword)
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

func TestAdminUnlockByUsername(t *testing.T) {
	h := NewHandler()

	// First, log in to admin
	form := url.Values{}
	form.Set("password", dbLockerPassword)
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.handleAdminLogin(rr, req)

	// Extract session cookie
	cookies := rr.Header()["Set-Cookie"]
	var sessionCookie string
	for _, cookie := range cookies {
		if strings.Contains(cookie, "admin_session=") {
			parts := strings.Split(cookie, "=")
			if len(parts) > 1 {
				sessionCookie = strings.Split(parts[1], ";")[0]
			}
			break
		}
	}

	// Lock 5 databases with username "alice"
	var aliceConnections []string
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/lock?username=alice&password="+dbLockerPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLock(rr, req)
		if rr.Code == http.StatusOK {
			aliceConnections = append(aliceConnections, strings.TrimSpace(rr.Body.String()))
		}
	}

	// Lock 3 databases with username "bob"
	var bobConnections []string
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/lock?username=bob&password="+dbLockerPassword, nil)
		rr := httptest.NewRecorder()
		h.handleLock(rr, req)
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

	// Use admin to unlock all databases locked by "alice"
	unlockForm := url.Values{}
	unlockForm.Set("username", "alice")
	req = httptest.NewRequest("POST", "/admin/unlock-by-username", strings.NewReader(unlockForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: sessionCookie})
	rr = httptest.NewRecorder()
	h.handleAdminUnlockByUsername(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303 for unlock-by-username, got %d", rr.Code)
	}

	// Verify that only bob's locks remain (3 locks)
	h.withLocksRLock(func() {
		if len(h.locks) != 3 {
			t.Errorf("Expected 3 locks remaining (bob's), got %d", len(h.locks))
		}

		// Verify all remaining locks are bob's
		for _, lockInfo := range h.locks {
			if lockInfo.Username != "bob" {
				t.Errorf("Expected all remaining locks to be bob's, found lock owned by %s", lockInfo.Username)
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

	// Test unlocking by username when no databases are locked by that user
	unlockForm = url.Values{}
	unlockForm.Set("username", "charlie")
	req = httptest.NewRequest("POST", "/admin/unlock-by-username", strings.NewReader(unlockForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: sessionCookie})
	rr = httptest.NewRecorder()
	h.handleAdminUnlockByUsername(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303 for unlock-by-username with no matches, got %d", rr.Code)
	}

	// Verify bob's locks are still there (no change)
	h.withLocksRLock(func() {
		if len(h.locks) != 3 {
			t.Errorf("Expected 3 locks remaining after unlocking non-existent user, got %d", len(h.locks))
		}
	})

	// Test that unlock-by-username requires admin authentication
	unlockForm = url.Values{}
	unlockForm.Set("username", "bob")
	req = httptest.NewRequest("POST", "/admin/unlock-by-username", strings.NewReader(unlockForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// No session cookie
	rr = httptest.NewRecorder()
	h.handleAdminUnlockByUsername(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 for unlock-by-username without auth, got %d", rr.Code)
	}
}

// TestLock_MassiveRaceConditionStressTest bombards the server with thousands of concurrent requests
// to ensure no race conditions exist. This test specifically validates:
// 1. No database connection is ever given to multiple goroutines simultaneously
// 2. The locks map and channel stay in sync
// 3. Admin force-unlock operations don't cause race conditions
// 4. All databases are properly returned to the pool after the test
func TestLock_MassiveRaceConditionStressTest(t *testing.T) {
	h := NewHandler()

	// 5000 goroutines competing for 25 databases = 200x contention ratio
	numGoroutines := 5000
	// Each goroutine will do multiple lock/unlock cycles
	cyclesPerGoroutine := 3

	var wg sync.WaitGroup
	errorsChan := make(chan error, numGoroutines*cyclesPerGoroutine)

	// Track ownership of each connection using atomic counters.
	// If a connection is given to two goroutines, the counter will exceed 1.
	counters := make(map[string]*atomic.Int32)
	for connStr := range testDatabases {
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
					fmt.Sprintf("/lock?username=user%d&password=%s", goroutineID, dbLockerPassword), nil)
				rr := httptest.NewRecorder()
				h.handleLock(rr, req)

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
				unlockURL := fmt.Sprintf("/unlock?username=user%d&password=%s", goroutineID, dbLockerPassword)
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
				t.Errorf("  Remaining lock: %s by %s", connStr, lockInfo.Username)
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

// TestLock_RaceWithAdminForceUnlock tests that admin force-unlock doesn't corrupt system state.
// Note: Admin force-unlock intentionally "steals" locks from workers, so we don't check for
// exclusive access here. Instead we verify:
// 1. System state remains consistent (locked + available = total)
// 2. No panics or unexpected errors
// 3. Workers handle force-unlock gracefully (their unlock returns 400)
func TestLock_RaceWithAdminForceUnlock(t *testing.T) {
	h := NewHandler()

	// Create admin session
	form := url.Values{}
	form.Set("password", dbLockerPassword)
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.handleAdminLogin(rr, req)

	var sessionCookie string
	for _, cookie := range rr.Header()["Set-Cookie"] {
		if strings.Contains(cookie, "admin_session=") {
			parts := strings.Split(cookie, "=")
			if len(parts) > 1 {
				sessionCookie = strings.Split(parts[1], ";")[0]
			}
			break
		}
	}

	numWorkers := 200
	var wg sync.WaitGroup
	errorsChan := make(chan error, numWorkers*2)

	// Track connections that are currently locked by workers (for admin to target)
	activeConnections := sync.Map{}

	var forceUnlockCount atomic.Int64
	var workerUnlockSuccess atomic.Int64
	var workerUnlockFailed atomic.Int64

	t.Logf("Starting admin force-unlock consistency test with %d workers", numWorkers)

	// Start worker goroutines that lock/unlock
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			// Lock
			req := httptest.NewRequest("GET",
				fmt.Sprintf("/lock?username=worker%d&password=%s", goroutineID, dbLockerPassword), nil)
			rr := httptest.NewRecorder()
			h.handleLock(rr, req)

			if rr.Code != http.StatusOK {
				errorsChan <- fmt.Errorf("worker %d: lock failed with status %d", goroutineID, rr.Code)
				return
			}

			connStr := strings.TrimSpace(rr.Body.String())

			// Register as active (admin might force-unlock this)
			activeConnections.Store(connStr, goroutineID)

			// Hold for random time
			time.Sleep(time.Duration(rand.Intn(30)) * time.Millisecond)

			activeConnections.Delete(connStr)

			// Try to unlock - might fail if admin already force-unlocked
			unlockURL := fmt.Sprintf("/unlock?username=worker%d&password=%s", goroutineID, dbLockerPassword)
			req = httptest.NewRequest("POST", unlockURL, strings.NewReader(connStr))
			rr = httptest.NewRecorder()
			h.handleUnlock(rr, req)

			if rr.Code == http.StatusOK {
				workerUnlockSuccess.Add(1)
			} else if rr.Code == http.StatusBadRequest {
				// Admin already force-unlocked this - expected
				workerUnlockFailed.Add(1)
			} else {
				errorsChan <- fmt.Errorf("worker %d: unlock got unexpected status %d", goroutineID, rr.Code)
			}
		}(i)
	}

	// Start admin goroutines that randomly force-unlock
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

				// Force unlock it
				forceUnlockForm := url.Values{}
				forceUnlockForm.Set("conn", targetConn)
				req := httptest.NewRequest("POST", "/admin/force-unlock", strings.NewReader(forceUnlockForm.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.AddCookie(&http.Cookie{Name: "admin_session", Value: sessionCookie})
				rr := httptest.NewRecorder()
				h.handleAdminForceUnlock(rr, req)

				if rr.Code == http.StatusSeeOther {
					forceUnlockCount.Add(1)
				} else {
					errorsChan <- fmt.Errorf("admin %d: force-unlock got status %d", adminID, rr.Code)
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
	h.withLocksLock(func() {
		for connStr := range h.locks {
			delete(h.locks, connStr)
			h.cLockedDbConn <- connStr
		}
	})

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
	h := NewHandler()

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
	for connStr := range testDatabases {
		counters[connStr] = &atomic.Int32{}
	}

	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Lock
			req := httptest.NewRequest("GET",
				fmt.Sprintf("/lock?username=user%d&password=%s", id, dbLockerPassword), nil)
			rr := httptest.NewRecorder()
			h.handleLock(rr, req)

			if rr.Code != http.StatusOK {
				return
			}

			connStr := strings.TrimSpace(rr.Body.String())
			counters[connStr].Add(1)

			time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)

			counters[connStr].Add(-1)

			// Unlock
			req = httptest.NewRequest("POST",
				fmt.Sprintf("/unlock?username=user%d&password=%s", id, dbLockerPassword),
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
