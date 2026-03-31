package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/config"
)

// helper: create a temporary script that writes to stdout/stderr and exits
// with the given code.
func writeTempScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sh")
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("failed to write temp script: %v", err)
	}
	return path
}

// helper: build a BridgeServer with the given routes, using httptest
// infrastructure (no real listener needed for handler-level tests).
func newTestServer(routes []config.BridgeRoute) *BridgeServer {
	return NewBridgeServer(routes, 0, nil)
}

// --- Health endpoint ---

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

// --- Routes endpoint ---

func TestRoutesEndpoint(t *testing.T) {
	routes := []config.BridgeRoute{
		{APIPath: "/deploy", ScriptPath: "/scripts/deploy.sh"},
		{APIPath: "/restart", ScriptPath: "/scripts/restart.sh"},
	}
	srv := newTestServer(routes)

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body []routeInfo
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(body))
	}
	if body[0].APIPath != "/deploy" {
		t.Errorf("expected /deploy, got %q", body[0].APIPath)
	}
	if body[1].ScriptPath != "/scripts/restart.sh" {
		t.Errorf("expected /scripts/restart.sh, got %q", body[1].ScriptPath)
	}
}

func TestRoutesEndpointEmpty(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body []routeInfo
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("expected 0 routes, got %d", len(body))
	}
}

// --- Valid route execution ---

func TestValidRouteExecutesScript(t *testing.T) {
	script := writeTempScript(t, `#!/bin/bash
echo "hello from stdout"
echo "hello from stderr" >&2
exit 0
`)
	routes := []config.BridgeRoute{
		{APIPath: "/greet", ScriptPath: script},
	}
	srv := newTestServer(routes)

	req := httptest.NewRequest(http.MethodPost, "/greet", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body execResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", body.ExitCode)
	}
	if body.Stdout != "hello from stdout\n" {
		t.Errorf("unexpected stdout: %q", body.Stdout)
	}
	if body.Stderr != "hello from stderr\n" {
		t.Errorf("unexpected stderr: %q", body.Stderr)
	}
	if body.DurationMs < 0 {
		t.Errorf("expected non-negative duration, got %d", body.DurationMs)
	}
}

func TestValidRouteNonZeroExit(t *testing.T) {
	script := writeTempScript(t, `#!/bin/bash
echo "partial output"
exit 42
`)
	routes := []config.BridgeRoute{
		{APIPath: "/fail", ScriptPath: script},
	}
	srv := newTestServer(routes)

	req := httptest.NewRequest(http.MethodPost, "/fail", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (script ran, just non-zero exit), got %d", w.Code)
	}

	var body execResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", body.ExitCode)
	}
	if body.Stdout != "partial output\n" {
		t.Errorf("unexpected stdout: %q", body.Stdout)
	}
}

// --- Unknown route returns 404 ---

func TestUnknownRouteReturns404(t *testing.T) {
	routes := []config.BridgeRoute{
		{APIPath: "/deploy", ScriptPath: "/scripts/deploy.sh"},
	}
	srv := newTestServer(routes)

	req := httptest.NewRequest(http.MethodPost, "/nonexistent", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	var body errorResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(body.AvailableRoutes) != 1 || body.AvailableRoutes[0] != "/deploy" {
		t.Errorf("expected available routes [/deploy], got %v", body.AvailableRoutes)
	}
}

// --- Script not found ---

func TestScriptNotFoundReturnsError(t *testing.T) {
	routes := []config.BridgeRoute{
		{APIPath: "/missing", ScriptPath: "/nonexistent/path/script.sh"},
	}
	srv := newTestServer(routes)

	req := httptest.NewRequest(http.MethodPost, "/missing", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}

	var body errorResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// --- Script timeout ---

func TestScriptTimeout(t *testing.T) {
	script := writeTempScript(t, `#!/bin/bash
sleep 30
`)

	// Use executeScriptCtx directly with a very short timeout so the
	// test completes quickly. This exercises the same code path that
	// executeScript uses with the 5-minute constant.
	stdout, stderr, exitCode, _, err := executeScriptCtx(
		context.Background(), script, 200*time.Millisecond,
	)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if exitCode != -1 {
		t.Errorf("expected exit code -1 on timeout, got %d", exitCode)
	}
	// stdout and stderr may be empty or partial -- that's fine.
	_ = stdout
	_ = stderr
}

// --- Concurrent executions ---

func TestConcurrentExecutions(t *testing.T) {
	script := writeTempScript(t, `#!/bin/bash
echo "concurrent"
exit 0
`)
	routes := []config.BridgeRoute{
		{APIPath: "/concurrent", ScriptPath: script},
	}
	srv := newTestServer(routes)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)

	errors := make(chan string, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/concurrent", nil)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				errors <- fmt.Sprintf("expected 200, got %d", w.Code)
				return
			}
			var body execResponse
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				errors <- fmt.Sprintf("failed to decode: %v", err)
				return
			}
			if body.ExitCode != 0 {
				errors <- fmt.Sprintf("expected exit 0, got %d", body.ExitCode)
			}
		}()
	}

	wg.Wait()
	close(errors)
	for e := range errors {
		t.Error(e)
	}
}

// --- Bind address verification ---

func TestBindAddressVerification(t *testing.T) {
	port := getFreePort(t)

	routes := []config.BridgeRoute{
		{APIPath: "/test", ScriptPath: "/dev/null"},
	}
	srv := NewBridgeServer(routes, port, []string{"127.0.0.2"})

	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	// Give the servers a moment to start accepting connections.
	time.Sleep(50 * time.Millisecond)

	// Should be reachable on 127.0.0.1:{port}.
	resp1, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		t.Fatalf("expected server reachable on 127.0.0.1:%d, got error: %v", port, err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("expected 200 on 127.0.0.1:%d, got %d", port, resp1.StatusCode)
	}

	// Should be reachable on 127.0.0.2:{port} (gateway IP).
	resp2, err := http.Get(fmt.Sprintf("http://127.0.0.2:%d/health", port))
	if err != nil {
		t.Fatalf("expected server reachable on 127.0.0.2:%d, got error: %v", port, err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 on 127.0.0.2:%d, got %d", port, resp2.StatusCode)
	}
}

// --- UpdateRoutes hot-swap ---

func TestUpdateRoutesHotSwap(t *testing.T) {
	script := writeTempScript(t, `#!/bin/bash
echo "v1"
`)
	routes := []config.BridgeRoute{
		{APIPath: "/v1", ScriptPath: script},
	}
	srv := newTestServer(routes)

	// Before update: /v1 exists, /v2 does not.
	req := httptest.NewRequest(http.MethodPost, "/v1", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for /v1, got %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v2", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for /v2, got %d", w.Code)
	}

	// Hot-swap: replace /v1 with /v2.
	script2 := writeTempScript(t, `#!/bin/bash
echo "v2"
`)
	srv.UpdateRoutes([]config.BridgeRoute{
		{APIPath: "/v2", ScriptPath: script2},
	})

	// After update: /v2 exists, /v1 does not.
	req = httptest.NewRequest(http.MethodPost, "/v2", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for /v2, got %d", w.Code)
	}
	var body execResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if body.Stdout != "v2\n" {
		t.Errorf("expected v2 output, got %q", body.Stdout)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for /v1 after swap, got %d", w.Code)
	}
}

// --- Method not allowed ---

func TestMethodNotAllowed(t *testing.T) {
	routes := []config.BridgeRoute{
		{APIPath: "/deploy", ScriptPath: "/scripts/deploy.sh"},
	}
	srv := newTestServer(routes)

	req := httptest.NewRequest(http.MethodGet, "/deploy", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// --- Execution log channel ---

func TestExecutionLogSent(t *testing.T) {
	script := writeTempScript(t, `#!/bin/bash
echo "logged"
`)
	routes := []config.BridgeRoute{
		{APIPath: "/log-test", ScriptPath: script},
	}
	srv := newTestServer(routes)

	req := httptest.NewRequest(http.MethodPost, "/log-test", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	select {
	case log := <-srv.LogChan():
		if log.Route != "/log-test" {
			t.Errorf("expected route /log-test, got %q", log.Route)
		}
		if log.Stdout != "logged\n" {
			t.Errorf("expected stdout 'logged\\n', got %q", log.Stdout)
		}
		if log.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", log.ExitCode)
		}
		if log.Error != "" {
			t.Errorf("expected no error, got %q", log.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for execution log")
	}
}

// --- Helper functions ---

func getFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}
