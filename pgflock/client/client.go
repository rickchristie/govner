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
// # Prerequisites
//
// Before using this client, you must have pgflock running:
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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Lock acquires an exclusive lock on a database from the pool and returns its connection string.
//
// This function blocks until a database becomes available. When a database is acquired,
// it is automatically reset (DROP + CREATE from template) before the connection string
// is returned, ensuring a clean state for each test.
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
// If the locker server is not running or unreachable, an error is returned immediately.
func Lock(lockerPort int, marker string, password string) (string, error) {
	reqURL := fmt.Sprintf("http://localhost:%d/lock?marker=%s&password=%s",
		lockerPort, url.QueryEscape(marker), url.QueryEscape(password))

	resp, err := http.Get(reqURL)
	if err != nil {
		return "", fmt.Errorf("failed to connect to locker: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("lock failed: %s", string(body))
	}

	connStr, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(connStr), nil
}

// Unlock releases a database lock, returning it to the pool for other tests.
//
// This function should be called when a test completes (typically via defer).
// After unlocking, the database becomes available for other tests to acquire.
//
// Parameters:
//   - lockerPort: The port where the locker server is running (default: 9191)
//   - password: The locker password from your pgflock configuration
//   - connString: The connection string returned by [Lock]
//
// Note: If you forget to call Unlock, the database will be automatically
// unlocked after the auto_unlock_minutes duration (default: 5 minutes).
func Unlock(lockerPort int, password string, connString string) error {
	reqURL := fmt.Sprintf("http://localhost:%d/unlock?marker=unlock&password=%s",
		lockerPort, url.QueryEscape(password))

	resp, err := http.Post(reqURL, "text/plain", strings.NewReader(connString))
	if err != nil {
		return fmt.Errorf("failed to connect to locker: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unlock failed: %s", string(body))
	}

	return nil
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
