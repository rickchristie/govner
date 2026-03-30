package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const scriptTimeout = 5 * time.Minute

// execResponse is the JSON body returned after a successful script execution.
type execResponse struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
}

// errorResponse is the JSON body returned for errors (404, bad method, etc.).
type errorResponse struct {
	Error           string   `json:"error"`
	AvailableRoutes []string `json:"available_routes,omitempty"`
}

// routeInfo is one entry in the /routes listing response.
type routeInfo struct {
	APIPath    string `json:"api_path"`
	ScriptPath string `json:"script_path"`
}

// ServeHTTP dispatches incoming HTTP requests:
//   - GET  /health  -> health check
//   - GET  /routes  -> list configured routes
//   - POST /{path}  -> execute the matching bridge script
//   - anything else -> 404 with available routes
func (s *BridgeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path

	// Health check.
	if path == "/health" && r.Method == http.MethodGet {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	// Route listing.
	if path == "/routes" && r.Method == http.MethodGet {
		routes := s.getRoutes()
		info := make([]routeInfo, len(routes))
		for i, rt := range routes {
			info[i] = routeInfo{APIPath: rt.APIPath, ScriptPath: rt.ScriptPath}
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(info)
		return
	}

	// Route execution — POST only.
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed, use POST", s.routePaths())
		return
	}

	routes := s.getRoutes()
	for _, rt := range routes {
		if rt.APIPath == path {
			stdout, stderr, exitCode, duration, err := executeScript(rt.ScriptPath)

			// Build and send the execution log (non-blocking).
			logEntry := ExecutionLog{
				Timestamp:  time.Now(),
				Route:      rt.APIPath,
				ScriptPath: rt.ScriptPath,
				ExitCode:   exitCode,
				Stdout:     stdout,
				Stderr:     stderr,
				Duration:   duration,
			}
			if err != nil {
				logEntry.Error = err.Error()
			}
			// Blocking send: bridge executions are infrequent (seconds each),
			// so briefly waiting for channel space is negligible.
			s.logCh <- logEntry

			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error(), nil)
				return
			}

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(execResponse{
				ExitCode:   exitCode,
				Stdout:     stdout,
				Stderr:     stderr,
				DurationMs: duration.Milliseconds(),
			})
			return
		}
	}

	// No matching route.
	writeError(w, http.StatusNotFound, fmt.Sprintf("no route matches path %q", path), s.routePaths())
}

// executeScript runs the given script with bash and captures output.
// It enforces a 5-minute timeout per execution.
func executeScript(scriptPath string) (stdout, stderr string, exitCode int, duration time.Duration, err error) {
	return executeScriptCtx(context.Background(), scriptPath, scriptTimeout)
}

// executeScriptCtx is the core implementation with a configurable timeout.
func executeScriptCtx(parent context.Context, scriptPath string, timeout time.Duration) (stdout, stderr string, exitCode int, duration time.Duration, err error) {
	// Check that the script file exists before running bash.
	if _, statErr := os.Stat(scriptPath); statErr != nil {
		return "", "", -1, 0, fmt.Errorf("script not found: %s", scriptPath)
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", scriptPath)

	// Run in its own process group so we can kill all children on timeout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// On context cancellation, kill the entire process group (not just bash).
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	// Give child processes a brief window to die after the signal.
	cmd.WaitDelay = 2 * time.Second

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	runErr := cmd.Run()
	duration = time.Since(start)

	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if runErr != nil {
		// Check if the context deadline was exceeded (timeout).
		if ctx.Err() == context.DeadlineExceeded {
			return stdout, stderr, -1, duration, fmt.Errorf("script timed out after %s", timeout)
		}

		// Try to extract the exit code from the error.
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return stdout, stderr, status.ExitStatus(), duration, nil
			}
		}

		// Script not found or other exec failure.
		return stdout, stderr, -1, duration, fmt.Errorf("script execution failed: %w", runErr)
	}

	return stdout, stderr, 0, duration, nil
}

// routePaths returns the API paths of all configured routes.
func (s *BridgeServer) routePaths() []string {
	routes := s.getRoutes()
	paths := make([]string, len(routes))
	for i, rt := range routes {
		paths[i] = rt.APIPath
	}
	return paths
}

// writeError writes a JSON error response with the given status code.
func writeError(w http.ResponseWriter, statusCode int, msg string, availableRoutes []string) {
	w.WriteHeader(statusCode)
	resp := errorResponse{
		Error: msg,
	}
	if len(availableRoutes) > 0 {
		resp.AvailableRoutes = availableRoutes
	}
	json.NewEncoder(w).Encode(resp)
}

// normalizeAPIPath ensures the path starts with a slash and has no trailing slash.
// Exported for use by callers that build BridgeRoute values.
func NormalizeAPIPath(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(path, "/")
}
