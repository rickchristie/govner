// Package client provides a Go client for interacting with the pgflock locker server.
//
// For full documentation, CLI usage, configuration options, and HTTP API reference,
// see the main pgflock package: https://pkg.go.dev/github.com/rickchristie/govner/pgflock
//
// pgflock manages a pool of PostgreSQL test databases. This client enables your tests
// to acquire exclusive locks on databases from the pool, ensuring test isolation when
// running tests in parallel.
//
// # Basic Usage
//
// In your test code, acquire a database lock before the test runs and release it after:
//
//	func TestSomething(t *testing.T) {
//	    connStr, err := client.Lock(9191, "TestSomething", "pgflock")
//	    if err != nil {
//	        t.Fatal(err)
//	    }
//	    defer client.Unlock(9191, "pgflock", connStr)
//
//	    db, err := sql.Open("postgres", connStr)
//	    if err != nil {
//	        t.Fatal(err)
//	    }
//	    defer db.Close()
//
//	    // Run your database tests...
//	}
//
// # Auto-unlock on process death
//
// The client keeps the HTTP connection to the server open for the duration of the
// lock. This open connection IS the lock: when the process dies (panic, kill, timeout),
// all connections are closed by the OS, and the server releases all locks immediately.
// There is no heartbeat polling — connection death equals lock release.
//
// # CloseAll
//
// Call CloseAll in TestMain — after m.Run() returns — to release all connections
// cleanly. This is required for tools like goleak that check for leaked goroutines.
// Do NOT call CloseAll from individual tests or t.Cleanup; it closes every open
// connection in the process, which will break other tests running in parallel.
//
//	func TestMain(m *testing.M) {
//	    code := m.Run()
//	    client.CloseAll()
//	    os.Exit(code)
//	}
//
// # Prerequisites
//
// Before using this client, you must have pgflock v2 running:
//
//	pgflock up
//
// This starts the PostgreSQL containers and the locker server on the configured port
// (default: 9191).
//
// # Configuration
//
// The lockerPort parameter should match the locker_port setting in your
// .pgflock/config.yaml (default: 9191).
//
// The password parameter must match the password setting in your config (default: "pgflock").
//
// # Thread Safety
//
// All functions in this package are safe for concurrent use. Multiple goroutines
// can call Lock simultaneously; each will receive a different database from the pool.
package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// requiredServerVersion is the X-PGFlock-Version header value this client requires.
const requiredServerVersion = "2"

// lockClient is a dedicated HTTP client for streaming lock connections.
// It has no timeout: lock connections are intentionally long-lived, and the
// server enforces the auto-unlock duration via its own WriteTimeout.
var lockClient = &http.Client{
	Transport: &http.Transport{
		// Each lock is a dedicated long-lived connection. Disable keep-alive
		// pooling so connections are not reused across locks.
		DisableKeepAlives: true,
	},
}

var (
	connMu    sync.Mutex
	openConns = make(map[string]io.Closer) // connStr -> open response body
)

// Lock acquires an exclusive lock on a database from the pool and returns its connection string.
//
// This function blocks until a database becomes available. When a database is acquired,
// it is automatically reset (DROP + CREATE from template) before the connection string
// is returned, ensuring a clean state for each test.
//
// The lock is held by keeping an HTTP streaming connection open to the server.
// When the process dies for any reason (panic, kill, timeout), the OS closes all
// connections and the server releases all locks immediately — no heartbeat required.
//
// Parameters:
//   - lockerPort: The port where the locker server is running (default: 9191)
//   - marker: An identifier for this lock, typically the test name. Shown in the TUI
//     to help identify which test holds each database.
//   - password: The locker password from your pgflock configuration
//
// Returns the PostgreSQL connection string (e.g., "postgres://user:pass@localhost:5432/db")
// that can be used with sql.Open or any PostgreSQL driver.
//
// The returned connection string must be passed to [Unlock] when the test completes.
//
// If the locker server is not running, not reachable, or is not pgflock v2, an
// error is returned immediately.
func Lock(lockerPort int, marker string, password string) (string, error) {
	reqURL := fmt.Sprintf("http://localhost:%d/lock?marker=%s&password=%s",
		lockerPort, url.QueryEscape(marker), url.QueryEscape(password))

	resp, err := lockClient.Get(reqURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to locker: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("lock failed: %s", strings.TrimSpace(string(body)))
	}

	// Verify the server is v2. A v1 server returns the conn string and closes the
	// connection immediately, so the open-connection unlock mechanism won't work.
	serverVersion := resp.Header.Get("X-PGFlock-Version")
	if serverVersion != requiredServerVersion {
		resp.Body.Close()
		if serverVersion == "" {
			return "", fmt.Errorf(
				"pgflock server v2 required but got a v1 server: run 'pgflock up' to upgrade")
		}
		return "", fmt.Errorf(
			"pgflock server version mismatch: client requires v%s, server reported v%s",
			requiredServerVersion, serverVersion)
	}

	// Read the connection string from the first line. The body is intentionally
	// left open — the open connection holds the lock.
	reader := bufio.NewReader(resp.Body)
	connStr, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		resp.Body.Close()
		return "", fmt.Errorf("failed to read connection string: %w", err)
	}
	connStr = strings.TrimSpace(connStr)
	if connStr == "" {
		resp.Body.Close()
		return "", fmt.Errorf("locker returned empty connection string")
	}

	connMu.Lock()
	openConns[connStr] = resp.Body
	connMu.Unlock()

	return connStr, nil
}

// Unlock releases a database lock by closing the streaming connection to the server.
//
// Closing the connection signals the server to release the lock immediately.
// No separate HTTP request is made.
//
// The lockerPort and password parameters are accepted for API compatibility but
// are unused in v2 — the lock is identified by connString alone.
//
// Parameters:
//   - lockerPort: Unused in v2, kept for API compatibility
//   - password: Unused in v2, kept for API compatibility
//   - connString: The connection string returned by [Lock]
func Unlock(lockerPort int, password string, connString string) error {
	connMu.Lock()
	body, exists := openConns[connString]
	if exists {
		delete(openConns, connString)
	}
	connMu.Unlock()

	if !exists {
		return fmt.Errorf("no active lock found for connection: %s", connString)
	}

	// Closing the response body drops the TCP connection. The server's request
	// context is cancelled, waking the streaming handler which releases the lock.
	return body.Close()
}

// CloseAll closes all open lock connections, releasing all held locks.
//
// This does not prevent future Lock calls — new locks can be acquired after CloseAll.
//
// CloseAll must only be called from TestMain after m.Run() returns — that is,
// after every test in the package has finished. Calling it from an individual test
// (including inside t.Cleanup) will close connections held by other tests that may
// still be running in parallel, causing them to lose their database locks.
//
//	func TestMain(m *testing.M) {
//	    code := m.Run()
//	    client.CloseAll()
//	    os.Exit(code)
//	}
//
// If you use [go.uber.org/goleak], place CloseAll before VerifyTestMain or use
// goleak.VerifyTestMain with a custom wrapper — both have the same constraint
// of running after all tests complete.
//
// Individual tests should release their locks with [Unlock]. If a test forgets
// to unlock, the server's auto-unlock timeout will release it eventually, and
// the OS will close all connections when the test process exits.
func CloseAll() {
	connMu.Lock()
	defer connMu.Unlock()

	for connStr, body := range openConns {
		body.Close()
		delete(openConns, connStr)
	}
}

// HealthCheck verifies that the locker server is running and responsive.
//
// This can be used in test setup to ensure pgflock is available before
// running tests, providing a clearer error message than a Lock timeout.
//
// Parameters:
//   - lockerPort: The port where the locker server is running (default: 9191)
//
// Returns nil if the locker is healthy, or an error if it's not reachable.
func HealthCheck(lockerPort int) error {
	reqURL := fmt.Sprintf("http://localhost:%d/health-check", lockerPort)

	resp, err := http.Get(reqURL)
	if err != nil {
		return fmt.Errorf("locker not responding: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("locker unhealthy: status %d", resp.StatusCode)
	}

	return nil
}

// LockInfo contains information about a locked database.
type LockInfo struct {
	ConnString      string `json:"conn_string"`
	Marker          string `json:"marker"`
	LockedAt        string `json:"locked_at"`
	DurationSeconds int64  `json:"duration_seconds"`
}

// Status contains the full state of the locker server.
type Status struct {
	Status            string     `json:"status"`
	TotalDatabases    int        `json:"total"`
	LockedDatabases   int        `json:"locked"`
	FreeDatabases     int        `json:"free"`
	WaitingRequests   int        `json:"waiting"`
	AutoUnlockMinutes int        `json:"auto_unlock_minutes"`
	Locks             []LockInfo `json:"locks"`
}

// GetStatus returns the full state of the locker server, including details about
// all locked databases.
//
// This is useful for monitoring and debugging, especially for AI agents that need
// to understand why tests might be slow or blocked.
//
// Parameters:
//   - lockerPort: The port where the locker server is running (default: 9191)
//
// The returned Status includes:
//   - Total, locked, free, and waiting database counts
//   - Auto-unlock timeout configuration
//   - List of all locked databases with marker, timestamp, and duration
func GetStatus(lockerPort int) (*Status, error) {
	reqURL := fmt.Sprintf("http://localhost:%d/health-check", lockerPort)

	resp, err := http.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("locker not responding: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("locker unhealthy: status %d", resp.StatusCode)
	}

	var status Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &status, nil
}

// Restart triggers a full restart of the database pool.
//
// This unlocks all databases and restarts the PostgreSQL containers. Use this
// to recover from stuck tests or when the database pool is in an inconsistent state.
//
// This function blocks until the restart is complete, which may take 30+ seconds
// depending on the number of instances.
//
// Parameters:
//   - lockerPort: The port where the locker server is running (default: 9191)
//   - password: The locker password from your pgflock configuration
//
// Note: This is a disruptive operation that will interrupt any running tests.
func Restart(lockerPort int, password string) error {
	reqURL := fmt.Sprintf("http://localhost:%d/restart?marker=client&password=%s",
		lockerPort, url.QueryEscape(password))

	resp, err := http.Post(reqURL, "text/plain", nil)
	if err != nil {
		return fmt.Errorf("failed to connect to locker: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("restart failed: %s", string(body))
	}

	return nil
}

// UnlockAll releases all locked databases without restarting containers.
//
// This is a less disruptive alternative to [Restart] when you just need to
// release stuck locks but the containers are healthy.
//
// Parameters:
//   - lockerPort: The port where the locker server is running (default: 9191)
//   - password: The locker password from your pgflock configuration
//
// Returns the number of databases that were unlocked.
func UnlockAll(lockerPort int, password string) (int, error) {
	reqURL := fmt.Sprintf("http://localhost:%d/unlock-all?marker=client&password=%s",
		lockerPort, url.QueryEscape(password))

	resp, err := http.Post(reqURL, "text/plain", nil)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to locker: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("unlock-all failed: %s", string(body))
	}

	var result struct {
		Status   string `json:"status"`
		Unlocked int    `json:"unlocked"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Unlocked, nil
}
