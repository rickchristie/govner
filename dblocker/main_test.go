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
