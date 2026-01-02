package client

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Lock acquires a database lock and returns the connection string.
// Blocks until a database is available.
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

// Unlock releases a database lock.
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

// HealthCheck checks if the locker is running
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
