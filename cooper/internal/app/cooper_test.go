package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/auth"
	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/names"
	"github.com/rickchristie/govner/cooper/internal/testdocker"

	"github.com/rickchristie/govner/cooper/internal/templates"
)

// testImagePrefix isolates test images from production Cooper images.
const testImagePrefix = testdocker.ImagePrefix

const (
	// Use Docker-network aliases instead of the public internet so proxy tests
	// exercise the real allow/deny rules against deterministic local targets.
	proxyAllowedTestDomain = "api.anthropic.com"
	proxyBlockedTestDomain = "example.com"
)

// skipIfNoDocker verifies the docker CLI is available for default test runs.
func skipIfNoDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("docker not available: %v", err)
	}
}

// skipIfNoProxyImage verifies the shared proxy image exists for default test runs.
func skipIfNoProxyImage(t *testing.T) {
	t.Helper()
	docker.SetImagePrefix(testImagePrefix)
	imageName := docker.GetImageProxy()
	exists, err := docker.ImageExists(imageName)
	if err != nil {
		t.Fatalf("cannot check image %s: %v", imageName, err)
	}
	if !exists {
		t.Fatalf("proxy image %s not found after test bootstrap", imageName)
	}
}

// setupCooperDir creates a full cooper directory using the real template
// rendering pipeline — the same code path as `cooper build` and `cooper up`.
// This ensures integration tests validate the actual generated configs
// (squid.conf, entrypoints, socat rules) against the real Squid/socat binaries.
func setupCooperDir(t *testing.T) (string, *config.Config) {
	t.Helper()
	cooperDir := t.TempDir()

	cfg := config.DefaultConfig()
	if err := testdocker.AssignDynamicPorts(cfg); err != nil {
		t.Fatalf("assign dynamic test ports: %v", err)
	}

	// Write config.json.
	cfgPath := filepath.Join(cooperDir, "config.json")
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Generate CA certificate.
	if _, _, err := config.EnsureCA(cooperDir); err != nil {
		t.Fatalf("ensure CA: %v", err)
	}

	// Render ALL templates using the real template pipeline.
	// This generates squid.conf, proxy-entrypoint.sh, cli entrypoint, etc.
	baseDir := filepath.Join(cooperDir, "base")
	cliDir := filepath.Join(cooperDir, "cli")
	proxyDir := filepath.Join(cooperDir, "proxy")
	for _, dir := range []string{baseDir, cliDir, proxyDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	// Render base + per-tool CLI templates (entrypoint.sh, base Dockerfile, per-tool Dockerfiles).
	if err := templates.WriteAllTemplates(baseDir, cliDir, cfg); err != nil {
		t.Fatalf("write cli templates: %v", err)
	}

	// Render proxy templates (squid.conf, proxy.Dockerfile, proxy-entrypoint.sh).
	if err := templates.WriteProxyTemplates(proxyDir, cfg); err != nil {
		t.Fatalf("write proxy templates: %v", err)
	}

	// Write socat rules config.
	if err := docker.WritePortForwardConfig(cooperDir, cfg.BridgePort, cfg.PortForwardRules); err != nil {
		t.Fatalf("write socat rules: %v", err)
	}

	// Create run and logs directories (must exist before Docker mounts them).
	// Use 0777 so Squid (running as user "squid" inside the container) can
	// write to the volume-mounted logs dir, and t.TempDir() can clean up.
	for _, dir := range []string{
		filepath.Join(cooperDir, "run"),
		filepath.Join(cooperDir, "logs"),
	} {
		if err := os.MkdirAll(dir, 0777); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		os.Chmod(dir, 0777)
	}

	// Register cleanup that fixes permissions on files created by container
	// processes (e.g., Squid's "squid" user), so t.TempDir() can remove them.
	t.Cleanup(func() {
		_ = testdocker.FixOwnership(cooperDir)
	})

	// Stage CA cert into proxy dir (proxy Dockerfile COPYs it from context,
	// but at runtime it's volume-mounted from cooperDir).
	caCert := filepath.Join(cooperDir, "ca", "cooper-ca.pem")
	caKey := filepath.Join(cooperDir, "ca", "cooper-ca-key.pem")
	copyFileForTest(t, caCert, filepath.Join(proxyDir, "cooper-ca.pem"))
	copyFileForTest(t, caKey, filepath.Join(proxyDir, "cooper-ca-key.pem"))

	return cooperDir, cfg
}

func copyFileForTest(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// cleanupDocker removes containers and networks created during tests.
func cleanupDocker(t *testing.T) {
	t.Helper()
	_ = docker.CleanupRuntime()
}

// fixCooperDirPermissions makes all files in cooperDir writable by the current
// user so t.TempDir() cleanup can remove them. Squid writes log files as the
// container's "squid" user (different UID), making them unremovable.
func fixCooperDirPermissions(t *testing.T, cooperDir string) {
	t.Helper()
	_ = testdocker.FixOwnership(cooperDir)
}

func startProxyHTTPSTarget(t *testing.T) *testdocker.HTTPSTarget {
	t.Helper()

	target, err := testdocker.StartHTTPSTarget(proxyAllowedTestDomain, proxyBlockedTestDomain)
	if err != nil {
		t.Fatalf("start local proxy HTTPS target: %v", err)
	}
	t.Cleanup(func() {
		if err := target.Remove(); err != nil {
			t.Logf("remove local proxy HTTPS target: %v", err)
		}
	})
	return target
}

func trustProxyHTTPSTarget(t *testing.T, target *testdocker.HTTPSTarget) {
	t.Helper()

	certPEM, err := exec.Command("docker", "exec", target.ContainerName, "cat", "/tmp/target.crt").Output()
	if err != nil {
		t.Fatalf("read HTTPS target certificate from %s: %v", target.ContainerName, err)
	}

	cmd := exec.Command(
		"docker", "exec", "-i", "-u", "root",
		docker.ProxyContainerName(),
		"sh", "-lc",
		"cat >/usr/local/share/ca-certificates/cooper-test-target.crt && update-ca-certificates >/tmp/cooper-test-target-ca.log 2>&1",
	)
	cmd.Stdin = bytes.NewReader(certPEM)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install HTTPS target certificate into proxy trust store: %v\n%s", err, string(out))
	}
}

func waitForCondition(t *testing.T, desc string, timeout, interval time.Duration, check func(attempt int) (bool, string, error)) string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	attempt := 0
	lastDetail := ""
	for {
		attempt++
		ok, detail, err := check(attempt)
		if detail == "" && err != nil {
			detail = err.Error()
		}
		if detail != "" {
			lastDetail = detail
		}
		if ok {
			if lastDetail != "" {
				t.Logf("%s ready on attempt=%d: %s", desc, attempt, lastDetail)
			}
			return lastDetail
		}

		if err != nil {
			t.Logf("%s pending on attempt=%d: %v", desc, attempt, err)
		} else if detail != "" {
			t.Logf("%s pending on attempt=%d: %s", desc, attempt, detail)
		} else {
			t.Logf("%s pending on attempt=%d", desc, attempt)
		}

		if time.Now().After(deadline) {
			if lastDetail == "" {
				lastDetail = "no detail"
			}
			t.Fatalf("%s did not become ready within %s (last detail: %s)", desc, timeout, lastDetail)
		}
		time.Sleep(interval)
	}
}

func waitForFileContains(t *testing.T, path string, timeout time.Duration, substrings ...string) string {
	t.Helper()

	var matchedContent string
	waitForCondition(t, "file "+path, timeout, 100*time.Millisecond, func(_ int) (bool, string, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return false, "", err
		}
		content := string(data)
		for _, want := range substrings {
			if !strings.Contains(content, want) {
				return false, fmt.Sprintf("waiting for %q", want), nil
			}
		}
		matchedContent = content
		return true, fmt.Sprintf("matched %d bytes", len(content)), nil
	})
	return matchedContent
}

func waitForDirNotEmpty(t *testing.T, dir string, timeout time.Duration) {
	t.Helper()

	waitForCondition(t, "directory "+dir, timeout, 100*time.Millisecond, func(_ int) (bool, string, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, "", err
		}
		if len(entries) == 0 {
			return false, "directory still empty", nil
		}
		return true, fmt.Sprintf("found %d entries", len(entries)), nil
	})
}

func waitForBarrelCommandMatch(t *testing.T, barrelName, desc, command string, timeout, interval time.Duration, match func(out string, err error) bool) string {
	t.Helper()

	var matched string
	waitForCondition(t, desc, timeout, interval, func(attempt int) (bool, string, error) {
		out, err := barrelExec(barrelName, command)
		trimmed := strings.TrimSpace(out)
		if match(trimmed, err) {
			matched = trimmed
			return true, trimmed, nil
		}
		return false, fmt.Sprintf("output=%q err=%v", trimmed, err), nil
	})
	return matched
}

func waitForProxyProcessPorts(t *testing.T, timeout time.Duration, ports ...int) string {
	t.Helper()

	var processes string
	waitForCondition(t, "proxy socat process list", timeout, 200*time.Millisecond, func(_ int) (bool, string, error) {
		out, err := exec.Command("docker", "exec", docker.ProxyContainerName(), "ps", "aux").CombinedOutput()
		if err != nil {
			return false, strings.TrimSpace(string(out)), err
		}
		processes = string(out)
		for _, port := range ports {
			if !strings.Contains(processes, fmt.Sprintf("TCP-LISTEN:%d", port)) {
				return false, fmt.Sprintf("waiting for TCP-LISTEN:%d", port), nil
			}
		}
		return true, fmt.Sprintf("found ports %v", ports), nil
	})
	return processes
}

// TestCooperApp_StartStop verifies that a CooperApp can start (creating
// networks, proxy container, ACL listener, bridge server) and stop
// (cleaning up all resources).
func TestCooperApp_StartStop(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	// Start the app.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var steps []string
	err := app.Start(ctx, func(step int, total int, name string, err error) {
		steps = append(steps, name)
		if err != nil {
			t.Logf("step %d/%d %q failed: %v", step, total, name, err)
		}
	})
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Verify progress callbacks were received for all steps.
	if len(steps) != totalSteps {
		t.Errorf("got %d progress steps, want %d; steps: %v", len(steps), totalSteps, steps)
	}

	// Verify proxy is running.
	if !app.IsProxyRunning() {
		t.Error("IsProxyRunning() = false after Start, want true")
	}

	// Verify ACL requests channel is non-nil and readable.
	select {
	case <-app.ACLRequests():
		// Got an unexpected request -- this is fine, channel works.
	case <-time.After(100 * time.Millisecond):
		// No request pending, expected.
	}

	// Stop the app.
	stopStart := time.Now()
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
	stopElapsed := time.Since(stopStart)
	t.Logf("app stop completed in %s", stopElapsed.Round(time.Millisecond))
	if stopElapsed > 5*time.Second {
		t.Fatalf("Stop() took too long: %s", stopElapsed)
	}

	// Verify proxy is no longer running.
	running, _ := docker.IsProxyRunning()
	if running {
		t.Error("proxy still running after Stop()")
	}
}

func TestCooperApp_StopWithBarrelReturnsQuickly(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	skipIfNoBarrelImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	app, barrelName := startAppAndBarrel(t, cfg, cooperDir)
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", barrelName).Run()
		cleanupDocker(t)
	})

	stopStart := time.Now()
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}
	stopElapsed := time.Since(stopStart)
	t.Logf("app stop with barrel completed in %s", stopElapsed.Round(time.Millisecond))
	if stopElapsed > 5*time.Second {
		t.Fatalf("Stop() with barrel took too long: %s", stopElapsed)
	}

	running, _ := docker.IsBarrelRunning(barrelName)
	if running {
		t.Errorf("barrel %s still running after Stop()", barrelName)
	}
	running, _ = docker.IsProxyRunning()
	if running {
		t.Error("proxy still running after Stop()")
	}
}

// TestCooperApp_ACLFlow starts the app, subscribes to ACL channels, simulates
// an ACL request by connecting to the Unix socket, verifies the request appears,
// approves it, and verifies the decision event.
func TestCooperApp_ACLFlow(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	// Use a short timeout so the test doesn't hang on approval.
	cfg.MonitorTimeoutSecs = 5
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	// Connect to the ACL Unix socket and send a domain request.
	socketPath := filepath.Join(cooperDir, "run", "acl.sock")
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial ACL socket: %v", err)
	}

	// Protocol: "domain port source_ip\n"
	_, err = fmt.Fprintf(conn, "example.com 443 172.18.0.5\n")
	if err != nil {
		conn.Close()
		t.Fatalf("write to ACL socket: %v", err)
	}

	// Read the ACL request from the channel.
	var req ACLRequest
	select {
	case req = <-app.ACLRequests():
		// Got the request.
	case <-time.After(5 * time.Second):
		conn.Close()
		t.Fatal("timed out waiting for ACL request on channel")
	}

	if req.Domain != "example.com" {
		t.Errorf("ACLRequest.Domain = %q, want %q", req.Domain, "example.com")
	}
	if req.Port != "443" {
		t.Errorf("ACLRequest.Port = %q, want %q", req.Port, "443")
	}
	if req.SourceIP != "172.18.0.5" {
		t.Errorf("ACLRequest.SourceIP = %q, want %q", req.SourceIP, "172.18.0.5")
	}

	// Approve the request.
	app.ApproveRequest(req.ID)

	// Read the decision event from the channel.
	select {
	case evt := <-app.ACLDecisions():
		if evt.Decision != DecisionAllow {
			t.Errorf("DecisionEvent.Decision = %v, want DecisionAllow", evt.Decision)
		}
		if evt.Reason != "approved" {
			t.Errorf("DecisionEvent.Reason = %q, want %q", evt.Reason, "approved")
		}
		if evt.Request.Domain != "example.com" {
			t.Errorf("DecisionEvent.Request.Domain = %q, want %q", evt.Request.Domain, "example.com")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ACL decision on channel")
	}

	// Read the response from the socket (should be "OK\n").
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	conn.Close()
	if err != nil {
		t.Fatalf("read ACL response: %v", err)
	}
	response := string(buf[:n])
	if response != "OK\n" {
		t.Errorf("ACL response = %q, want %q", response, "OK\n")
	}
}

// TestCooperApp_BridgeHealth starts the app and makes an HTTP GET to the
// bridge health endpoint. Verifies it returns 200.
func TestCooperApp_BridgeHealth(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	// The bridge binds to 127.0.0.1:{BridgePort}.
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", cfg.BridgePort)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(healthURL)
	if err != nil {
		t.Fatalf("GET %s failed: %v", healthURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("bridge /health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestCooperApp_UpdateSettings starts the app, calls UpdateSettings with
// new values, and verifies the config was updated and persisted.
func TestCooperApp_UpdateSettings(t *testing.T) {
	cooperDir, cfg := setupCooperDir(t)

	app := NewCooperApp(cfg, cooperDir)

	// Original values from DefaultConfig.
	if cfg.MonitorTimeoutSecs != 30 {
		t.Fatalf("precondition: MonitorTimeoutSecs = %d, want 30", cfg.MonitorTimeoutSecs)
	}

	// Update settings.
	newTimeout := 15
	newBlocked := 200
	newAllowed := 300
	newBridgeLog := 100
	newClipboardTTL := 42
	newClipboardMaxBytes := 512
	if err := app.UpdateSettings(newTimeout, newBlocked, newAllowed, newBridgeLog, newClipboardTTL, newClipboardMaxBytes); err != nil {
		t.Fatalf("UpdateSettings() failed: %v", err)
	}

	// Verify in-memory config was updated.
	got := app.Config()
	if got.MonitorTimeoutSecs != newTimeout {
		t.Errorf("Config().MonitorTimeoutSecs = %d, want %d", got.MonitorTimeoutSecs, newTimeout)
	}
	if got.BlockedHistoryLimit != newBlocked {
		t.Errorf("Config().BlockedHistoryLimit = %d, want %d", got.BlockedHistoryLimit, newBlocked)
	}
	if got.AllowedHistoryLimit != newAllowed {
		t.Errorf("Config().AllowedHistoryLimit = %d, want %d", got.AllowedHistoryLimit, newAllowed)
	}
	if got.BridgeLogLimit != newBridgeLog {
		t.Errorf("Config().BridgeLogLimit = %d, want %d", got.BridgeLogLimit, newBridgeLog)
	}
	if got.ClipboardTTLSecs != newClipboardTTL {
		t.Errorf("Config().ClipboardTTLSecs = %d, want %d", got.ClipboardTTLSecs, newClipboardTTL)
	}
	if got.ClipboardMaxBytes != newClipboardMaxBytes {
		t.Errorf("Config().ClipboardMaxBytes = %d, want %d", got.ClipboardMaxBytes, newClipboardMaxBytes)
	}

	// Verify persisted config was updated.
	cfgPath := filepath.Join(cooperDir, "config.json")
	persisted, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}
	if persisted.MonitorTimeoutSecs != newTimeout {
		t.Errorf("persisted MonitorTimeoutSecs = %d, want %d", persisted.MonitorTimeoutSecs, newTimeout)
	}
	if persisted.BlockedHistoryLimit != newBlocked {
		t.Errorf("persisted BlockedHistoryLimit = %d, want %d", persisted.BlockedHistoryLimit, newBlocked)
	}
	if persisted.ClipboardTTLSecs != newClipboardTTL {
		t.Errorf("persisted ClipboardTTLSecs = %d, want %d", persisted.ClipboardTTLSecs, newClipboardTTL)
	}
	if persisted.ClipboardMaxBytes != newClipboardMaxBytes {
		t.Errorf("persisted ClipboardMaxBytes = %d, want %d", persisted.ClipboardMaxBytes, newClipboardMaxBytes)
	}

	snap, err := app.ClipboardManager().Stage(clipboard.ClipboardObject{
		Kind: clipboard.ClipboardKindText,
		Raw:  bytes.Repeat([]byte("x"), 32),
	}, 0)
	if err != nil {
		t.Fatalf("Stage() with updated max bytes should succeed: %v", err)
	}
	expectedExpiry := snap.CreatedAt.Add(time.Duration(newClipboardTTL) * time.Second)
	if snap.ExpiresAt.Sub(expectedExpiry).Abs() > time.Millisecond {
		t.Errorf("snapshot expiry = %v, want ~%v", snap.ExpiresAt, expectedExpiry)
	}

	if _, err := app.ClipboardManager().Stage(clipboard.ClipboardObject{
		Kind: clipboard.ClipboardKindText,
		Raw:  bytes.Repeat([]byte("x"), newClipboardMaxBytes+1),
	}, 0); err == nil {
		t.Fatal("expected updated clipboard max bytes to reject oversized stage")
	}
}

// TestCooperApp_UpdatePortForwards starts the app, calls UpdatePortForwards
// with new rules, and verifies that socat-rules.json was written correctly.
func TestCooperApp_UpdatePortForwards(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	// Define new port forward rules.
	rules := []config.PortForwardRule{
		{
			ContainerPort: 8080,
			HostPort:      8080,
			Description:   "test HTTP",
		},
		{
			ContainerPort: 9090,
			HostPort:      9090,
			Description:   "test metrics",
		},
	}

	if err := app.UpdatePortForwards(rules); err != nil {
		t.Fatalf("UpdatePortForwards() failed: %v", err)
	}

	// Verify socat-rules.json was written.
	socatPath := filepath.Join(cooperDir, "socat-rules.json")
	data, err := os.ReadFile(socatPath)
	if err != nil {
		t.Fatalf("read socat-rules.json: %v", err)
	}

	var socatCfg docker.PortForwardConfig
	if err := json.Unmarshal(data, &socatCfg); err != nil {
		t.Fatalf("parse socat-rules.json: %v", err)
	}

	if len(socatCfg.Rules) != 2 {
		t.Fatalf("socat-rules.json has %d rules, want 2", len(socatCfg.Rules))
	}
	if socatCfg.Rules[0].ContainerPort != 8080 {
		t.Errorf("rule[0].ContainerPort = %d, want 8080", socatCfg.Rules[0].ContainerPort)
	}
	if socatCfg.Rules[1].Description != "test metrics" {
		t.Errorf("rule[1].Description = %q, want %q", socatCfg.Rules[1].Description, "test metrics")
	}
	if socatCfg.BridgePort != cfg.BridgePort {
		t.Errorf("socat BridgePort = %d, want %d", socatCfg.BridgePort, cfg.BridgePort)
	}

	// Verify in-memory config was updated.
	if len(app.Config().PortForwardRules) != 2 {
		t.Errorf("Config().PortForwardRules has %d rules, want 2", len(app.Config().PortForwardRules))
	}

	// Verify persisted config.json was updated.
	cfgPath := filepath.Join(cooperDir, "config.json")
	persisted, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}
	if len(persisted.PortForwardRules) != 2 {
		t.Errorf("persisted PortForwardRules has %d rules, want 2", len(persisted.PortForwardRules))
	}
}

// TestCooperApp_ACLDenyFlow starts the app, sends an ACL request, denies it,
// and verifies the deny decision event and socket response.
func TestCooperApp_ACLDenyFlow(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	cfg.MonitorTimeoutSecs = 5
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	// Connect to the ACL Unix socket and send a domain request.
	socketPath := filepath.Join(cooperDir, "run", "acl.sock")
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial ACL socket: %v", err)
	}

	_, err = fmt.Fprintf(conn, "denied-domain.com 443 172.18.0.5\n")
	if err != nil {
		conn.Close()
		t.Fatalf("write to ACL socket: %v", err)
	}

	// Read the ACL request from the channel.
	var req ACLRequest
	select {
	case req = <-app.ACLRequests():
	case <-time.After(5 * time.Second):
		conn.Close()
		t.Fatal("timed out waiting for ACL request on channel")
	}

	if req.Domain != "denied-domain.com" {
		t.Errorf("ACLRequest.Domain = %q, want %q", req.Domain, "denied-domain.com")
	}

	// Deny the request.
	app.DenyRequest(req.ID)

	// Read the decision event from the channel.
	select {
	case evt := <-app.ACLDecisions():
		if evt.Decision != DecisionDeny {
			t.Errorf("DecisionEvent.Decision = %v, want DecisionDeny", evt.Decision)
		}
		if evt.Reason != "denied" {
			t.Errorf("DecisionEvent.Reason = %q, want %q", evt.Reason, "denied")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ACL decision on channel")
	}

	// Read the response from the socket (should be "ERR\n").
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	conn.Close()
	if err != nil {
		t.Fatalf("read ACL response: %v", err)
	}
	response := string(buf[:n])
	if response != "ERR\n" {
		t.Errorf("ACL response = %q, want %q", response, "ERR\n")
	}
}

// TestCooperApp_ACLTimeout starts the app with a short timeout, sends an ACL
// request, does NOT approve or deny, and verifies the request times out with
// fail-closed behavior (ERR response).
func TestCooperApp_ACLTimeout(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	cfg.MonitorTimeoutSecs = 1 // 1 second timeout for fast test
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	// Connect to the ACL Unix socket and send a domain request.
	socketPath := filepath.Join(cooperDir, "run", "acl.sock")
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial ACL socket: %v", err)
	}

	_, err = fmt.Fprintf(conn, "timeout-test.com 443 172.18.0.5\n")
	if err != nil {
		conn.Close()
		t.Fatalf("write to ACL socket: %v", err)
	}

	// Read the request from the channel to confirm it arrived.
	select {
	case <-app.ACLRequests():
		// Got the request; do NOT approve or deny.
	case <-time.After(5 * time.Second):
		conn.Close()
		t.Fatal("timed out waiting for ACL request on channel")
	}

	// Read the socket response — should be "ERR\n" after the 1s timeout.
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	conn.Close()
	if err != nil {
		t.Fatalf("read ACL response: %v", err)
	}
	response := string(buf[:n])
	if response != "ERR\n" {
		t.Errorf("ACL response = %q, want %q (auto-denied after timeout)", response, "ERR\n")
	}

	// Read the decision event from the channel.
	select {
	case evt := <-app.ACLDecisions():
		if evt.Decision != DecisionTimeout {
			t.Errorf("DecisionEvent.Decision = %v, want DecisionTimeout", evt.Decision)
		}
		if evt.Reason != "timeout" {
			t.Errorf("DecisionEvent.Reason = %q, want %q", evt.Reason, "timeout")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ACL decision on channel")
	}
}

// TestCooperApp_BridgeRouteExecution starts the app with a bridge route
// pointing to a test script, POSTs to the route, and verifies the response.
func TestCooperApp_BridgeRouteExecution(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)

	// Create a test script.
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "test-route.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho \"hello from bridge\"\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write test script: %v", err)
	}

	cfg.BridgeRoutes = []config.BridgeRoute{{APIPath: "/test-route", ScriptPath: scriptPath}}
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	client := &http.Client{Timeout: 10 * time.Second}

	// POST to the configured route.
	routeURL := fmt.Sprintf("http://127.0.0.1:%d/test-route", cfg.BridgePort)
	resp, err := client.Post(routeURL, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s failed: %v", routeURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST /test-route status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result struct {
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello from bridge") {
		t.Errorf("stdout = %q, want to contain %q", result.Stdout, "hello from bridge")
	}

	// Test error case: POST to nonexistent route should return 404.
	notFoundURL := fmt.Sprintf("http://127.0.0.1:%d/nonexistent", cfg.BridgePort)
	resp404, err := client.Post(notFoundURL, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s failed: %v", notFoundURL, err)
	}
	defer resp404.Body.Close()

	if resp404.StatusCode != http.StatusNotFound {
		t.Errorf("POST /nonexistent status = %d, want %d", resp404.StatusCode, http.StatusNotFound)
	}
}

// TestCooperApp_UpdateBridgeRoutes starts the app with no routes, verifies
// 404 on a route, adds a route via UpdateBridgeRoutes, verifies 200, removes
// all routes, and verifies 404 again and that the routes file was written.
func TestCooperApp_UpdateBridgeRoutes(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	client := &http.Client{Timeout: 10 * time.Second}
	routeURL := fmt.Sprintf("http://127.0.0.1:%d/test-route", cfg.BridgePort)

	// Step 1: POST to /test-route with no routes — should be 404.
	resp, err := client.Post(routeURL, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s failed: %v", routeURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST /test-route (no routes) status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// Step 2: Create a test script and add the route.
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "dynamic-route.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho \"dynamic route works\"\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write test script: %v", err)
	}

	if err := app.UpdateBridgeRoutes([]config.BridgeRoute{{APIPath: "/test-route", ScriptPath: scriptPath}}); err != nil {
		t.Fatalf("UpdateBridgeRoutes() failed: %v", err)
	}

	// Step 3: POST to /test-route — should now be 200.
	resp, err = client.Post(routeURL, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s (after add) failed: %v", routeURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST /test-route (after add) status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result struct {
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(result.Stdout, "dynamic route works") {
		t.Errorf("stdout = %q, want to contain %q", result.Stdout, "dynamic route works")
	}

	// Step 4: Remove all routes.
	if err := app.UpdateBridgeRoutes([]config.BridgeRoute{}); err != nil {
		t.Fatalf("UpdateBridgeRoutes(empty) failed: %v", err)
	}

	// Step 5: POST to /test-route — should be 404 again.
	resp2, err := client.Post(routeURL, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s (after remove) failed: %v", routeURL, err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("POST /test-route (after remove) status = %d, want %d", resp2.StatusCode, http.StatusNotFound)
	}

	// Step 6: Verify bridge-routes.json was written.
	routesPath := filepath.Join(cooperDir, "bridge-routes.json")
	if _, err := os.Stat(routesPath); os.IsNotExist(err) {
		t.Errorf("bridge-routes.json was not written to %s", routesPath)
	}
}

// TestCooperApp_StartupFailure verifies that Start() returns an error when
// the bridge server cannot bind to its port (because another listener is
// already occupying it).
func TestCooperApp_StartupFailure(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	// Bind a TCP listener to the bridge port BEFORE starting the app.
	blocker, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.BridgePort))
	if err != nil {
		t.Fatalf("failed to pre-bind bridge port %d: %v", cfg.BridgePort, err)
	}
	defer blocker.Close()

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startErr := app.Start(ctx, nil)

	// Start() should fail because bridge can't bind.
	if startErr == nil {
		// Bridge somehow started despite port being taken; stop everything.
		app.Stop()
		t.Fatal("Start() succeeded but expected failure because bridge port is occupied")
	}

	// Verify the error message mentions the port.
	errMsg := startErr.Error()
	portStr := fmt.Sprintf("%d", cfg.BridgePort)
	if !strings.Contains(errMsg, portStr) {
		t.Errorf("error message %q does not mention port %s", errMsg, portStr)
	}

	// Clean up: proxy may have started before bridge failed.
	app.Stop()
}

// TestCooperApp_ACLLogging verifies that ACL requests and decisions are
// logged to disk in {cooperDir}/logs/acl.log.
func TestCooperApp_ACLLogging(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	cfg.MonitorTimeoutSecs = 5
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	// Connect to the ACL Unix socket and send a domain request.
	socketPath := filepath.Join(cooperDir, "run", "acl.sock")
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial ACL socket: %v", err)
	}

	_, err = fmt.Fprintf(conn, "logged-domain.com 443 10.0.0.1\n")
	if err != nil {
		conn.Close()
		t.Fatalf("write to ACL socket: %v", err)
	}

	// Read request from channel.
	var req ACLRequest
	select {
	case req = <-app.ACLRequests():
	case <-time.After(5 * time.Second):
		conn.Close()
		t.Fatal("timed out waiting for ACL request on channel")
	}

	// Approve the request.
	app.ApproveRequest(req.ID)

	// Read the decision from the channel so the pipeline completes.
	select {
	case <-app.ACLDecisions():
	case <-time.After(5 * time.Second):
		conn.Close()
		t.Fatal("timed out waiting for ACL decision on channel")
	}

	// Read and discard the socket response.
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	conn.Read(buf)
	conn.Close()

	logPath := filepath.Join(cooperDir, "logs", "acl.log")
	logContent := waitForFileContains(t, logPath, 3*time.Second, "domain=logged-domain.com", "decision=approved")

	// Verify request was logged.
	if !strings.Contains(logContent, "domain=logged-domain.com") {
		t.Errorf("acl.log does not contain request log; got:\n%s", logContent)
	}

	// Verify decision was logged.
	if !strings.Contains(logContent, "decision=approved") {
		t.Errorf("acl.log does not contain decision log; got:\n%s", logContent)
	}
}

// TestCooperApp_BridgeLogging verifies that bridge execution results are
// logged to disk in {cooperDir}/logs/bridge.log.
func TestCooperApp_BridgeLogging(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)

	// Create a test script.
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "log-test.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho \"bridge-test\"\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write test script: %v", err)
	}

	cfg.BridgeRoutes = []config.BridgeRoute{{APIPath: "/test-log", ScriptPath: scriptPath}}
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	// POST to the route.
	client := &http.Client{Timeout: 10 * time.Second}
	routeURL := fmt.Sprintf("http://127.0.0.1:%d/test-log", cfg.BridgePort)
	resp, err := client.Post(routeURL, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s failed: %v", routeURL, err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /test-log status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Drain the BridgeLogs channel so the forwarding goroutine processes the log entry.
	select {
	case <-app.BridgeLogs():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for bridge log on channel")
	}

	logPath := filepath.Join(cooperDir, "logs", "bridge.log")
	logContent := waitForFileContains(t, logPath, 3*time.Second, "route=/test-log", "exit=0")

	// Verify the log contains route and exit code.
	if !strings.Contains(logContent, "route=/test-log") {
		t.Errorf("bridge.log does not contain route=/test-log; got:\n%s", logContent)
	}
	if !strings.Contains(logContent, "exit=0") {
		t.Errorf("bridge.log does not contain exit=0; got:\n%s", logContent)
	}
}

// TestCooperApp_ContainerStats starts the app and calls ContainerStats.
// Verifies it returns without error. The result may contain only the proxy
// stats (no barrels are running in tests).
func TestCooperApp_ContainerStats(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	stats, err := app.ContainerStats()
	if err != nil {
		t.Fatalf("ContainerStats() failed: %v", err)
	}

	// The proxy container should appear in the stats.
	found := false
	for _, s := range stats {
		t.Logf("container stat: name=%s cpu=%s mem=%s", s.Name, s.CPUPercent, s.MemUsage)
		if s.Name == docker.ProxyContainerName() {
			found = true
		}
	}
	if !found {
		t.Error("ContainerStats() did not include the proxy container")
	}
}

// =====================================================================
// Barrel integration tests
// =====================================================================
//
// These tests require BOTH the shared proxy and barrel images to be built.

// skipIfNoBarrelImage verifies the shared barrel image exists for default test runs.
func skipIfNoBarrelImage(t *testing.T) {
	t.Helper()
	docker.SetImagePrefix(testImagePrefix)
	imageName := docker.GetImageCLI("claude")
	exists, err := docker.ImageExists(imageName)
	if err != nil {
		t.Fatalf("cannot check barrel image %s: %v", imageName, err)
	}
	if !exists {
		t.Fatalf("barrel image %s not found after test bootstrap", imageName)
	}
}

// barrelExec runs a command inside a barrel container and returns combined output.
func barrelExec(containerName string, cmd string) (string, error) {
	out, err := exec.Command("docker", "exec", containerName, "bash", "-c", cmd).CombinedOutput()
	return string(out), err
}

// startAppAndBarrel is a helper that starts the full Cooper app (proxy + ACL +
// bridge) and a barrel container. It returns the barrel container name.
// The caller's t.Cleanup handles removing the barrel and Docker resources.
func startAppAndBarrel(t *testing.T, cfg *config.Config, cooperDir string) (*CooperApp, string) {
	t.Helper()

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("starting Cooper app runtime")
	if err := app.Start(ctx, func(step int, total int, name string, err error) {
		if err != nil {
			t.Logf("startup step %d/%d %q failed: %v", step, total, name, err)
			return
		}
		t.Logf("startup step %d/%d complete: %s", step+1, total, name)
	}); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Create a workspace directory for the barrel.
	workspaceDir := t.TempDir()
	barrelName := docker.BarrelContainerName(workspaceDir, "claude")

	// Start the barrel container.
	t.Logf("starting barrel container %s", barrelName)
	if err := docker.StartBarrel(cfg, workspaceDir, cooperDir, "claude"); err != nil {
		app.Stop()
		t.Fatalf("StartBarrel() failed: %v", err)
	}

	// Wait for barrel container to be running.
	t.Logf("waiting for barrel container %s to report running", barrelName)
	waitForContainer(t, barrelName, 10*time.Second)
	t.Logf("barrel container %s is running", barrelName)

	// Wait for proxy to be reachable from inside the barrel.
	// The barrel's entrypoint needs time to start, and Docker DNS
	// needs time to propagate the "cooper-proxy" name.
	t.Logf("waiting for barrel %s to reach proxy %s:%d", barrelName, docker.ProxyHost(), cfg.ProxyPort)
	waitForProxyFromBarrel(t, barrelName, cfg.ProxyPort, 15*time.Second)
	t.Logf("barrel %s can reach proxy %s:%d", barrelName, docker.ProxyHost(), cfg.ProxyPort)

	return app, barrelName
}

// waitForContainer polls until the named container is running or the timeout
// expires. This avoids races between docker run returning and the container
// being ready to accept exec commands.
func waitForContainer(t *testing.T, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		running, err := docker.IsBarrelRunning(name)
		t.Logf("waitForContainer attempt=%d container=%s running=%t err=%v", attempt, name, running, err)
		if running {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("container %s did not become running within %v", name, timeout)
}

// waitForProxyFromBarrel polls until the proxy is reachable from inside the
// barrel container. This waits for Docker DNS propagation and the barrel's
// entrypoint to finish initializing.
func waitForProxyFromBarrel(t *testing.T, barrelName string, proxyPort int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		proxyAddr := fmt.Sprintf("http://%s:%d", docker.ProxyHost(), proxyPort)
		// Use a separate curl call that only outputs the HTTP status code.
		// Avoid `|| echo 000` which concatenates with curl's output on failure.
		out, _ := exec.Command("docker", "exec", barrelName, "bash", "-c",
			fmt.Sprintf("curl -s -o /dev/null -w '%%{http_code}' --connect-timeout 2 --max-time 3 -x %s http://example.com 2>/dev/null", proxyAddr)).CombinedOutput()
		status := strings.TrimSpace(string(out))

		// A valid HTTP status is exactly 3 digits and not "000".
		if len(status) == 3 && status != "000" && status >= "100" && status <= "599" {
			t.Logf("proxy reachable from barrel (status=%s, attempt=%d)", status, attempt)
			return
		}

		// Check if proxy container is running.
		proxyState, _ := exec.Command("docker", "inspect", "--format", "{{.State.Status}}", docker.ProxyContainerName()).CombinedOutput()
		// Check if both are on the same network.
		netMembers, _ := exec.Command("docker", "network", "inspect", docker.InternalNetworkName(),
			"--format", "{{range .Containers}}{{.Name}} {{end}}").CombinedOutput()
		t.Logf("waitForProxy attempt=%d status=%q proxy=%s internal_members=[%s]",
			attempt, status, strings.TrimSpace(string(proxyState)), strings.TrimSpace(string(netMembers)))

		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("proxy not reachable from barrel %s within %v", barrelName, timeout)
}

// TestCooperApp_ProxyRuntimeScenarios shares one started app/barrel runtime
// across the proxy-path integration checks. These assertions are independent,
// but paying full Docker startup for each one dominated package time.
func TestCooperApp_ProxyRuntimeScenarios(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	skipIfNoBarrelImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	cfg.MonitorTimeoutSecs = 1
	app, barrelName := startAppAndBarrel(t, cfg, cooperDir)
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", barrelName).Run()
		app.Stop()
		cleanupDocker(t)
	})

	target := startProxyHTTPSTarget(t)
	trustProxyHTTPSTarget(t, target)
	t.Logf("local proxy HTTPS target %s ready at %s for domains %s", target.ContainerName, target.IP, strings.Join(target.Domains, ", "))

	t.Run("WhitelistedDomainPassthrough", func(t *testing.T) {
		out, err := barrelExec(barrelName,
			fmt.Sprintf("curl -k -s -o /dev/null -w '%%{http_code}' --connect-timeout 2 --max-time 5 -x http://%s:%d https://%s",
				docker.ProxyHost(), cfg.ProxyPort, proxyAllowedTestDomain))
		if err != nil {
			t.Fatalf("curl failed: %v\noutput: %s", err, out)
		}

		statusCode := strings.TrimSpace(out)
		t.Logf("whitelisted domain status code: %s", statusCode)
		if statusCode == "403" {
			t.Errorf("expected proxy to allow %s, got HTTP 403 (denied)", proxyAllowedTestDomain)
		}
		if statusCode == "000" || len(statusCode) != 3 {
			t.Errorf("unexpected status code format: %q", statusCode)
		}
	})

	t.Run("BlockedDomainDenied", func(t *testing.T) {
		out, err := barrelExec(barrelName,
			fmt.Sprintf("curl -k -s -o /dev/null -w '%%{http_code}' --connect-timeout 2 --max-time 4 -x http://%s:%d https://%s",
				docker.ProxyHost(), cfg.ProxyPort, proxyBlockedTestDomain))

		statusCode := strings.TrimSpace(out)
		t.Logf("blocked domain status code: %s (err: %v)", statusCode, err)
		if statusCode == "403" {
			return
		}
		if err != nil {
			t.Logf("curl failed with error (expected for blocked domain): %v", err)
			return
		}
		if len(statusCode) == 3 && (statusCode[0] == '2' || statusCode[0] == '3') {
			t.Errorf("expected proxy to block %s, but got HTTP %s (allowed)", proxyBlockedTestDomain, statusCode)
		}
	})

	t.Run("SSLBumpWorks", func(t *testing.T) {
		out, err := barrelExec(barrelName,
			"curl --cacert /usr/local/share/ca-certificates/cooper-ca.crt "+
				fmt.Sprintf("-s -o /dev/null -w '%%{http_code}' --connect-timeout 2 --max-time 5 -x http://%s:%d https://%s",
					docker.ProxyHost(), cfg.ProxyPort, proxyAllowedTestDomain))
		if err != nil {
			t.Fatalf("curl with CA cert failed: %v\noutput: %s", err, out)
		}

		statusCode := strings.TrimSpace(out)
		t.Logf("SSL bump status code: %s", statusCode)
		if statusCode == "000" {
			t.Errorf("curl returned 000 — likely a certificate error; SSL bump may not be working")
		}
		if statusCode == "403" {
			t.Errorf("proxy denied the request (403) — %s should be whitelisted", proxyAllowedTestDomain)
		}
	})

	t.Run("DirectEgressBlocked", func(t *testing.T) {
		out, err := barrelExec(barrelName,
			fmt.Sprintf("curl --resolve %s:443:%s --noproxy '*' --connect-timeout 2 --max-time 4 -s -o /dev/null -w '%%{http_code}' https://%s 2>&1",
				proxyBlockedTestDomain, target.IP, proxyBlockedTestDomain))

		t.Logf("direct egress output: %q (err: %v)", strings.TrimSpace(out), err)
		if err == nil {
			statusCode := strings.TrimSpace(out)
			if len(statusCode) == 3 && statusCode[0] >= '1' && statusCode[0] <= '5' && statusCode != "000" {
				t.Errorf("direct egress succeeded with HTTP %s — network isolation is broken", statusCode)
			}
		}
	})

	t.Run("BarrelReachesProxy", func(t *testing.T) {
		out, err := barrelExec(barrelName,
			fmt.Sprintf("curl -s -o /dev/null -w '%%{http_code}' --connect-timeout 2 --max-time 3 -x http://%s:%d http://%s",
				docker.ProxyHost(), cfg.ProxyPort, proxyAllowedTestDomain))
		if err != nil {
			t.Logf("curl returned error: %v (output: %s)", err, out)
		}

		statusCode := strings.TrimSpace(out)
		t.Logf("barrel-to-proxy status code: %s", statusCode)
		if statusCode == "000" || statusCode == "" {
			t.Errorf("barrel could not reach proxy: status=%q — DNS resolution or TCP connectivity failed", statusCode)
		}
	})
}

// TestCooperApp_SocatPortForwarding verifies the two-hop socat relay:
// barrel container -> cooper-proxy -> host.docker.internal -> host listener.
//
// A TCP listener on the host writes "hello-from-host\n" to any connection.
// A port forward rule maps containerPort=19999 -> hostPort=19999. The barrel
// connects to localhost:19999, which socat inside the barrel forwards to
// cooper-proxy, which socat inside the proxy forwards to host.docker.internal,
// which Docker resolves to the host machine where our listener is running.
func TestCooperApp_SocatPortForwarding(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	skipIfNoBarrelImage(t)
	docker.SetImagePrefix(testImagePrefix)

	// Start a TCP listener on the host on a test port.
	const testPort = 19999
	const testMessage = "hello-from-host"

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", testPort))
	if err != nil {
		t.Fatalf("failed to start TCP listener on port %d: %v", testPort, err)
	}
	t.Cleanup(func() { listener.Close() })

	// Accept connections in a goroutine and write the test message.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // listener closed
			}
			conn.Write([]byte(testMessage + "\n"))
			conn.Close()
		}
	}()

	// Configure a port forward rule.
	cooperDir, cfg := setupCooperDir(t)
	cfg.PortForwardRules = []config.PortForwardRule{
		{
			ContainerPort: testPort,
			HostPort:      testPort,
			Description:   "test socat relay",
		},
	}

	// Re-write the config and socat rules with the port forward.
	cfgPath := filepath.Join(cooperDir, "config.json")
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save config with port forward: %v", err)
	}
	if err := docker.WritePortForwardConfig(cooperDir, cfg.BridgePort, cfg.PortForwardRules); err != nil {
		t.Fatalf("write socat rules: %v", err)
	}

	app, barrelName := startAppAndBarrel(t, cfg, cooperDir)
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", barrelName).Run()
		app.Stop()
		cleanupDocker(t)
	})

	waitForCondition(t, "socat forwarding to host", 8*time.Second, 200*time.Millisecond, func(_ int) (bool, string, error) {
		out, err := barrelExec(barrelName,
			fmt.Sprintf("bash -c 'timeout 3 bash -c \"cat < /dev/tcp/localhost/%d\" 2>&1 || echo SOCAT_FAIL'", testPort))
		trimmed := strings.TrimSpace(out)
		if strings.Contains(trimmed, testMessage) {
			return true, fmt.Sprintf("primary output=%q", trimmed), nil
		}

		out2, err2 := barrelExec(barrelName,
			fmt.Sprintf("curl -s --connect-timeout 2 --max-time 3 telnet://localhost:%d", testPort))
		trimmed2 := strings.TrimSpace(out2)
		if strings.Contains(trimmed2, testMessage) {
			return true, fmt.Sprintf("fallback output=%q", trimmed2), nil
		}

		return false, fmt.Sprintf("primary=%q err=%v fallback=%q err=%v", trimmed, err, trimmed2, err2), nil
	})
}

// =====================================================================
// Additional integration tests
// =====================================================================

// TestCooperApp_CLIRuntimeScenarios shares one started barrel across the CLI
// integration checks. They all validate runtime container behavior, so the
// extra startup cost was redundant.
func TestCooperApp_CLIRuntimeScenarios(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	skipIfNoBarrelImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	cfg.AITools = []config.ToolConfig{
		{Name: "claude", Enabled: true, Mode: config.ModeLatest},
		{Name: "codex", Enabled: true, Mode: config.ModeLatest},
	}

	app, barrelName := startAppAndBarrel(t, cfg, cooperDir)
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", barrelName).Run()
		app.Stop()
		cleanupDocker(t)
	})

	t.Run("CLIOneShot", func(t *testing.T) {
		out, err := barrelExec(barrelName, "echo hello")
		if err != nil {
			t.Fatalf("one-shot exec failed: %v\noutput: %s", err, out)
		}

		trimmed := strings.TrimSpace(out)
		if trimmed != "hello" {
			t.Errorf("one-shot output = %q, want %q", trimmed, "hello")
		}
	})

	t.Run("CLITokenForwarding", func(t *testing.T) {
		envArgs := []string{
			"-e", "OPENAI_API_KEY=test-openai-key-12345",
			"-e", "GH_TOKEN=test-gh-token-67890",
		}

		args := append([]string{"exec"}, envArgs...)
		args = append(args, barrelName, "bash", "-c", "echo OPENAI=$OPENAI_API_KEY GH=$GH_TOKEN")
		cmd := exec.Command("docker", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("exec with env args failed: %v\noutput: %s", err, output)
		}

		out := strings.TrimSpace(string(output))
		if !strings.Contains(out, "OPENAI=test-openai-key-12345") {
			t.Errorf("OPENAI_API_KEY not forwarded; output: %s", out)
		}
		if !strings.Contains(out, "GH=test-gh-token-67890") {
			t.Errorf("GH_TOKEN not forwarded; output: %s", out)
		}
	})

	t.Run("CLIClaudeCodeNotForwarded", func(t *testing.T) {
		t.Setenv("CLAUDECODE", "1")
		t.Log("set host CLAUDECODE=1 for token resolution")

		workspaceDir := t.TempDir()
		enabledTools := []string{"claude", "codex"}
		t.Logf("resolving auth tokens for enabled tools: %s", strings.Join(enabledTools, ", "))
		tokens, err := auth.ResolveTokens(workspaceDir, cooperDir, enabledTools)
		if err != nil {
			t.Fatalf("ResolveTokens() failed: %v", err)
		}
		t.Logf("resolved %d auth tokens", len(tokens))

		for _, tok := range tokens {
			if tok.Name == "CLAUDECODE" {
				t.Errorf("CLAUDECODE was resolved as a token but should be excluded")
			}
		}

		t.Logf("inspecting barrel environment inside %s", barrelName)
		out, err := barrelExec(barrelName, "env")
		if err != nil {
			t.Fatalf("env command failed: %v\noutput: %s", err, out)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "CLAUDECODE=") {
				t.Errorf("CLAUDECODE found in barrel environment: %s", line)
			}
		}
	})

	t.Run("CLIReusesContainer", func(t *testing.T) {
		running, err := docker.IsBarrelRunning(barrelName)
		if err != nil {
			t.Fatalf("IsBarrelRunning() failed: %v", err)
		}
		if !running {
			t.Fatal("barrel should be running after startAppAndBarrel")
		}

		wsOut, err := exec.Command("docker", "inspect",
			"--format", "{{index .Config.Labels \"cooper.workspace\"}}",
			barrelName).Output()
		if err != nil {
			t.Fatalf("inspect barrel workspace label: %v", err)
		}
		workspaceDir := strings.TrimSpace(string(wsOut))

		secondName := docker.BarrelContainerName(workspaceDir, "claude")
		if secondName != barrelName {
			t.Errorf("BarrelContainerName returned %q on second call, want %q (reuse)", secondName, barrelName)
		}

		if err := docker.StartBarrel(cfg, workspaceDir, cooperDir, "claude"); err != nil {
			t.Fatalf("second StartBarrel() failed: %v", err)
		}

		running, err = docker.IsBarrelRunning(barrelName)
		if err != nil {
			t.Fatalf("IsBarrelRunning() after second start: %v", err)
		}
		if !running {
			t.Error("barrel should be running after second StartBarrel call")
		}

		barrels, err := docker.ListBarrels()
		if err != nil {
			t.Fatalf("ListBarrels() failed: %v", err)
		}
		count := 0
		for _, b := range barrels {
			if b.WorkspaceDir == workspaceDir {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected 1 barrel for workspace %s, found %d", workspaceDir, count)
		}
	})
}

// TestCooperApp_CLIChecksProxyRunning verifies that without starting the
// proxy, IsProxyRunning returns false. This mirrors the check that
// `cooper cli` performs before starting a barrel.
func TestCooperApp_CLIChecksProxyRunning(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)
	t.Cleanup(func() { cleanupDocker(t) })

	// Ensure proxy is not running (clean state).
	_ = exec.Command("docker", "rm", "-f", docker.ProxyContainerName()).Run()

	running, err := docker.IsProxyRunning()
	if err != nil {
		t.Fatalf("IsProxyRunning() returned error: %v", err)
	}
	if running {
		t.Error("IsProxyRunning() = true, want false when proxy is not started")
	}
}

// TestCooperApp_SocatLiveReload starts the app with port forward rules,
// calls ReloadSocat with new rules, and verifies the socat-rules.json is
// updated on disk. Then it verifies socat processes are running inside the
// proxy container for the new rules.
func TestCooperApp_SocatLiveReload(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	skipIfNoBarrelImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)

	// Start with one port forward rule.
	cfg.PortForwardRules = []config.PortForwardRule{
		{ContainerPort: 15000, HostPort: 15000, Description: "initial rule"},
	}
	if err := config.SaveConfig(filepath.Join(cooperDir, "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := docker.WritePortForwardConfig(cooperDir, cfg.BridgePort, cfg.PortForwardRules); err != nil {
		t.Fatalf("write initial socat rules: %v", err)
	}

	app, barrelName := startAppAndBarrel(t, cfg, cooperDir)
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", barrelName).Run()
		app.Stop()
		cleanupDocker(t)
	})

	waitForProxyProcessPorts(t, 5*time.Second, 15000)

	// Now reload with NEW rules (different port).
	newRules := []config.PortForwardRule{
		{ContainerPort: 16000, HostPort: 16000, Description: "reloaded rule"},
		{ContainerPort: 16001, HostPort: 16001, Description: "second reloaded rule"},
	}
	if err := docker.ReloadSocat(cooperDir, cfg.BridgePort, newRules); err != nil {
		// ReloadSocat may return signal errors if containers don't support
		// SIGHUP, but the config file should still be written.
		t.Logf("ReloadSocat returned (non-fatal): %v", err)
	}

	// Verify socat-rules.json was updated.
	socatPath := filepath.Join(cooperDir, "socat-rules.json")
	data, err := os.ReadFile(socatPath)
	if err != nil {
		t.Fatalf("read socat-rules.json: %v", err)
	}

	var socatCfg docker.PortForwardConfig
	if err := json.Unmarshal(data, &socatCfg); err != nil {
		t.Fatalf("parse socat-rules.json: %v", err)
	}

	if len(socatCfg.Rules) != 2 {
		t.Fatalf("socat-rules.json has %d rules, want 2", len(socatCfg.Rules))
	}
	if socatCfg.Rules[0].ContainerPort != 16000 {
		t.Errorf("rule[0].ContainerPort = %d, want 16000", socatCfg.Rules[0].ContainerPort)
	}
	if socatCfg.Rules[1].ContainerPort != 16001 {
		t.Errorf("rule[1].ContainerPort = %d, want 16001", socatCfg.Rules[1].ContainerPort)
	}

	psStr := waitForProxyProcessPorts(t, 5*time.Second, 16000, 16001)
	t.Logf("proxy processes:\n%s", psStr)
}

// TestCooperApp_BridgeBindAddress starts the app and verifies the bridge
// server is reachable on 127.0.0.1:port. It also verifies that the bridge
// is NOT reachable on 0.0.0.0 from a different interface (if possible).
func TestCooperApp_BridgeBindAddress(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	client := &http.Client{Timeout: 5 * time.Second}

	// Verify bridge is reachable on 127.0.0.1.
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", cfg.BridgePort)
	resp, err := client.Get(healthURL)
	if err != nil {
		t.Fatalf("GET %s failed: %v", healthURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("bridge /health on 127.0.0.1 status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Attempt to verify bridge is NOT reachable from a non-loopback address.
	// We try to connect on a non-loopback IP; if the bridge is correctly bound
	// to 127.0.0.1, it should refuse connections from other addresses.
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Logf("cannot enumerate interfaces: %v", err)
		return
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}

			// Try connecting to the bridge on a non-loopback IP.
			externalURL := fmt.Sprintf("http://%s:%d/health", ip.String(), cfg.BridgePort)
			externalResp, externalErr := client.Get(externalURL)
			if externalErr == nil {
				externalResp.Body.Close()
				if externalResp.StatusCode == http.StatusOK {
					t.Errorf("bridge reachable on non-loopback address %s (should only bind to 127.0.0.1)", ip.String())
				}
			}
			// Connection refused or timeout is expected -- bridge is bound to localhost only.
			return // Only need to test one non-loopback address.
		}
	}
}

// TestCooperApp_HistoryTabsReceiveEvents starts the app, sends an ACL
// request, approves it, and verifies the DecisionChan receives a
// DecisionEvent with DecisionAllow.
func TestCooperApp_HistoryTabsReceiveEvents(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	cfg.MonitorTimeoutSecs = 10
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	// Connect to the ACL Unix socket and send a domain request.
	socketPath := filepath.Join(cooperDir, "run", "acl.sock")
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial ACL socket: %v", err)
	}
	defer conn.Close()

	_, err = fmt.Fprintf(conn, "history-test.com 443 172.18.0.10\n")
	if err != nil {
		t.Fatalf("write to ACL socket: %v", err)
	}

	// Read the ACL request from the channel.
	var req ACLRequest
	select {
	case req = <-app.ACLRequests():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ACL request on channel")
	}

	if req.Domain != "history-test.com" {
		t.Errorf("ACLRequest.Domain = %q, want %q", req.Domain, "history-test.com")
	}

	// Approve the request.
	app.ApproveRequest(req.ID)

	// Verify the decision event appears on the decisions channel.
	select {
	case evt := <-app.ACLDecisions():
		if evt.Decision != DecisionAllow {
			t.Errorf("DecisionEvent.Decision = %v, want DecisionAllow", evt.Decision)
		}
		if evt.Reason != "approved" {
			t.Errorf("DecisionEvent.Reason = %q, want %q", evt.Reason, "approved")
		}
		if evt.Request.Domain != "history-test.com" {
			t.Errorf("DecisionEvent.Request.Domain = %q, want %q", evt.Request.Domain, "history-test.com")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ACL decision on channel")
	}

	// Read and discard socket response.
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	conn.Read(buf)
}

// TestCooperApp_ACLBackpressure starts the app, floods 50 ACL requests
// simultaneously without consuming the channel, then consumes all of them
// and verifies none were dropped.
func TestCooperApp_ACLBackpressure(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	cfg.MonitorTimeoutSecs = 30 // Long timeout so requests don't auto-deny.
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	socketPath := filepath.Join(cooperDir, "run", "acl.sock")

	const numRequests = 50

	// Send 50 ACL requests concurrently WITHOUT consuming the channel first.
	var wg sync.WaitGroup
	conns := make([]net.Conn, numRequests)
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			conn, err := net.DialTimeout("unix", socketPath, 10*time.Second)
			if err != nil {
				t.Errorf("dial ACL socket [%d]: %v", idx, err)
				return
			}
			conns[idx] = conn
			domain := fmt.Sprintf("backpressure-%d.example.com", idx)
			if _, err := fmt.Fprintf(conn, "%s 443 172.18.0.%d\n", domain, idx+10); err != nil {
				t.Errorf("write to ACL socket [%d]: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	// Now consume all requests from the channel.
	received := make(map[string]bool)
	timeout := time.After(30 * time.Second)
	for len(received) < numRequests {
		select {
		case req := <-app.ACLRequests():
			received[req.Domain] = true
			// Approve each request so the connection completes.
			app.ApproveRequest(req.ID)
		case <-timeout:
			t.Fatalf("timed out after receiving %d/%d requests", len(received), numRequests)
		}
	}

	// Verify all 50 domains were received.
	for i := 0; i < numRequests; i++ {
		domain := fmt.Sprintf("backpressure-%d.example.com", i)
		if !received[domain] {
			t.Errorf("request for %s was dropped", domain)
		}
	}

	// Clean up connections.
	for _, conn := range conns {
		if conn != nil {
			conn.Close()
		}
	}
}

// TestCooperApp_ContainerNamingCollision creates a barrel for a "myproject"
// workspace, then creates a barrel for a different path also named "myproject",
// and verifies the second gets a hash suffix to avoid collision.
func TestCooperApp_ContainerNamingCollision(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	skipIfNoBarrelImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	app := NewCooperApp(cfg, cooperDir)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Create two workspace directories with the same base name "myproject".
	ws1 := filepath.Join(t.TempDir(), "myproject")
	ws2 := filepath.Join(t.TempDir(), "myproject")
	if err := os.MkdirAll(ws1, 0755); err != nil {
		t.Fatalf("mkdir ws1: %v", err)
	}
	if err := os.MkdirAll(ws2, 0755); err != nil {
		t.Fatalf("mkdir ws2: %v", err)
	}

	// Start first barrel.
	if err := docker.StartBarrel(cfg, ws1, cooperDir, "claude"); err != nil {
		app.Stop()
		t.Fatalf("StartBarrel(ws1) failed: %v", err)
	}
	name1 := docker.BarrelContainerName(ws1, "claude")
	t.Logf("barrel 1: name=%s workspace=%s", name1, ws1)

	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", name1).Run()
	})

	waitForContainer(t, name1, 15*time.Second)

	// Start second barrel with a different absolute path but same dir name.
	if err := docker.StartBarrel(cfg, ws2, cooperDir, "claude"); err != nil {
		app.Stop()
		t.Fatalf("StartBarrel(ws2) failed: %v", err)
	}
	name2 := docker.BarrelContainerName(ws2, "claude")
	t.Logf("barrel 2: name=%s workspace=%s", name2, ws2)

	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", name2).Run()
		app.Stop()
		cleanupDocker(t)
	})

	// The two names must be different (second should have a hash suffix).
	if name1 == name2 {
		t.Errorf("both barrels have the same name %q; expected hash suffix on the second", name1)
	}

	basePrefix := docker.BarrelNamePrefix() + "myproject"
	// The first should be "<barrel-prefix>myproject", the second adds a hash suffix.
	if !strings.HasPrefix(name1, basePrefix) {
		t.Errorf("name1 = %q, want prefix %q", name1, basePrefix)
	}
	if !strings.HasPrefix(name2, basePrefix+"-") {
		t.Errorf("name2 = %q, want prefix %q with hash suffix", name2, basePrefix+"-")
	}
}

// TestCooperApp_Cleanup starts the app with proxy and a barrel, then calls
// the cleanup functions and verifies all containers are stopped and removed.
func TestCooperApp_Cleanup(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	skipIfNoBarrelImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	app, barrelName := startAppAndBarrel(t, cfg, cooperDir)
	// Do NOT use the standard cleanup because we are testing cleanup itself.

	// Verify proxy and barrel are running before cleanup.
	proxyRunning, err := docker.IsProxyRunning()
	if err != nil {
		t.Fatalf("IsProxyRunning() before cleanup: %v", err)
	}
	if !proxyRunning {
		t.Fatal("proxy should be running before cleanup")
	}

	barrelRunning, err := docker.IsBarrelRunning(barrelName)
	if err != nil {
		t.Fatalf("IsBarrelRunning() before cleanup: %v", err)
	}
	if !barrelRunning {
		t.Fatal("barrel should be running before cleanup")
	}

	// Perform cleanup: stop all barrels, then stop proxy.
	barrels, err := docker.ListBarrels()
	if err != nil {
		t.Fatalf("ListBarrels() failed: %v", err)
	}
	for _, b := range barrels {
		if err := docker.StopBarrel(b.Name); err != nil {
			t.Errorf("StopBarrel(%s) failed: %v", b.Name, err)
		}
	}

	if err := docker.StopProxy(); err != nil {
		t.Errorf("StopProxy() failed: %v", err)
	}

	// Also stop via the app to close loggers and clean up resources.
	app.Stop()

	// Verify barrel is no longer running.
	barrelRunning, err = docker.IsBarrelRunning(barrelName)
	if err != nil {
		t.Fatalf("IsBarrelRunning() after cleanup: %v", err)
	}
	if barrelRunning {
		t.Error("barrel still running after cleanup")
	}

	// Verify proxy is no longer running.
	proxyRunning, err = docker.IsProxyRunning()
	if err != nil {
		t.Fatalf("IsProxyRunning() after cleanup: %v", err)
	}
	if proxyRunning {
		t.Error("proxy still running after cleanup")
	}

	// Verify the containers were actually removed (not just stopped).
	inspectBarrel := exec.Command("docker", "inspect", barrelName)
	if err := inspectBarrel.Run(); err == nil {
		t.Errorf("barrel container %s still exists after cleanup (expected removal)", barrelName)
	}

	inspectProxy := exec.Command("docker", "inspect", docker.ProxyContainerName())
	if err := inspectProxy.Run(); err == nil {
		t.Errorf("proxy container still exists after cleanup (expected removal)")
	}

	// Final cleanup of networks.
	cleanupDocker(t)
}

// TestCooperApp_PerToolDockerfiles verifies that the multi-image architecture
// generates separate Dockerfiles for each tool that reference the base image
// and do not contain CACHE_BUST args (removed concept).
func TestCooperApp_PerToolDockerfiles(t *testing.T) {
	docker.SetImagePrefix(testImagePrefix)

	cfg := config.DefaultConfig()

	// Render per-tool Dockerfiles and verify they reference the base image.
	for _, tool := range []string{"claude", "copilot", "codex", "opencode"} {
		df, err := templates.RenderCLIToolDockerfile(cfg, tool)
		if err != nil {
			t.Fatalf("RenderCLIToolDockerfile(%s) failed: %v", tool, err)
		}
		if !strings.Contains(df, "FROM "+docker.GetImageBase()) {
			t.Errorf("%s Dockerfile missing FROM %s", tool, docker.GetImageBase())
		}
		if strings.Contains(df, "CACHE_BUST") {
			t.Errorf("%s Dockerfile should not contain CACHE_BUST (removed concept)", tool)
		}
		if !strings.Contains(df, "COOPER_CLI_TOOL="+tool) {
			t.Errorf("%s Dockerfile missing COOPER_CLI_TOOL=%s", tool, tool)
		}
	}

	// Render base Dockerfile and verify no AI tools leak in.
	base, err := templates.RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}
	if strings.Contains(base, "CACHE_BUST") {
		t.Error("base Dockerfile should not contain CACHE_BUST")
	}
	for _, tool := range []string{"claude", "copilot", "codex", "opencode"} {
		if strings.Contains(base, "COOPER_CLI_TOOL="+tool) {
			t.Errorf("base Dockerfile should not contain COOPER_CLI_TOOL=%s", tool)
		}
	}
}

// Note: TestCooperApp_ProofDiagnostics was removed because proof.Run is now
// a self-contained lifecycle test that creates its own infrastructure.
// Use `cooper proof` to run the full integration test.

// TestCooperApp_LoginShellPATH verifies that interactive login shells have the
// correct PATH including npm-global/bin and .local/bin. This catches the bug
// where Debian login shells reset PATH, making tools invisible to users.
func TestCooperApp_LoginShellPATH(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app, barrelName := startAppAndBarrel(t, cfg, cooperDir)
	defer app.Stop()

	// Run 'echo $PATH' in a login shell (bash -l), not a regular shell.
	loginPath, err := barrelExec(barrelName, "bash -lc 'echo $PATH'")
	if err != nil {
		t.Fatalf("login shell PATH check failed: %v", err)
	}

	if !strings.Contains(loginPath, ".npm-global/bin") {
		t.Errorf("login shell PATH missing .npm-global/bin: %s", loginPath)
	}
	if !strings.Contains(loginPath, ".local/bin") {
		t.Errorf("login shell PATH missing .local/bin: %s", loginPath)
	}

	// Verify enabled AI tools are found in login shell.
	for _, tool := range cfg.AITools {
		if !tool.Enabled {
			continue
		}
		which, err := barrelExec(barrelName, "bash -lc 'which "+tool.Name+"'")
		if err != nil || strings.TrimSpace(which) == "" {
			t.Errorf("login shell: '%s' not found (enabled but not in login PATH)", tool.Name)
		} else {
			t.Logf("login shell: %s found at %s", tool.Name, strings.TrimSpace(which))
		}
	}
}

// TestCooperApp_MountedVolumeOwnership verifies that files created by
// the proxy and barrel containers in mounted volumes are owned by the
// host user's UID/GID, not by root or a random container UID.
func TestCooperApp_MountedVolumeOwnership(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app, barrelName := startAppAndBarrel(t, cfg, cooperDir)
	defer app.Stop()

	expectedUID := fmt.Sprintf("%d", os.Getuid())

	waitForDirNotEmpty(t, filepath.Join(cooperDir, "logs"), 3*time.Second)

	// Check proxy-created files in mounted volumes.
	checkPaths := []struct {
		path string
		desc string
	}{
		{filepath.Join(cooperDir, "run"), "run/ directory"},
		{filepath.Join(cooperDir, "logs"), "logs/ directory"},
	}

	for _, cp := range checkPaths {
		entries, err := os.ReadDir(cp.path)
		if err != nil {
			t.Logf("could not read %s: %v", cp.desc, err)
			continue
		}
		for _, entry := range entries {
			fullPath := filepath.Join(cp.path, entry.Name())
			info, err := os.Stat(fullPath)
			if err != nil {
				continue
			}
			_ = info
			// Get owner UID via exec since os.Stat doesn't expose UID portably.
			out, err := exec.Command("stat", "-c", "%u", fullPath).Output()
			if err != nil {
				continue
			}
			actualUID := strings.TrimSpace(string(out))
			if actualUID != expectedUID {
				t.Errorf("%s/%s owned by UID %s, expected %s", cp.desc, entry.Name(), actualUID, expectedUID)
			} else {
				t.Logf("%s/%s owned by UID %s (correct)", cp.desc, entry.Name(), actualUID)
			}
		}
	}

	// Check barrel-created file in workspace.
	workspaceDir := filepath.Join(cooperDir, "test-workspace")
	testFile := filepath.Join(workspaceDir, "ownership-test")
	_, err := barrelExec(barrelName, fmt.Sprintf("touch %s", testFile))
	if err != nil {
		t.Logf("barrel could not create test file: %v", err)
	} else {
		out, err := exec.Command("stat", "-c", "%u", testFile).Output()
		if err == nil {
			actualUID := strings.TrimSpace(string(out))
			if actualUID != expectedUID {
				t.Errorf("barrel-created file owned by UID %s, expected %s", actualUID, expectedUID)
			} else {
				t.Logf("barrel-created file owned by UID %s (correct)", actualUID)
			}
		}
		os.Remove(testFile)
	}
}

// TestCooperApp_MultipleToolBarrels starts two barrels for the same workspace
// (claude and codex) and verifies they run simultaneously, mount the same
// workspace, and use the correct images.
func TestCooperApp_MultipleToolBarrels(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	skipIfNoBarrelImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)

	app := NewCooperApp(cfg, cooperDir)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	t.Cleanup(func() {
		app.Stop()
		cleanupDocker(t)
	})

	workspaceDir := t.TempDir()

	// Start claude barrel.
	if err := docker.StartBarrel(cfg, workspaceDir, cooperDir, "claude"); err != nil {
		t.Fatalf("StartBarrel(claude) failed: %v", err)
	}
	claudeBarrel := docker.BarrelContainerName(workspaceDir, "claude")
	t.Cleanup(func() { docker.StopBarrel(claudeBarrel) })
	waitForContainer(t, claudeBarrel, 15*time.Second)

	// Start codex barrel for the same workspace.
	codexImage := docker.GetImageCLI("codex")
	codexExists, _ := docker.ImageExists(codexImage)
	if !codexExists {
		t.Skipf("codex image %s not found", codexImage)
	}
	if err := docker.StartBarrel(cfg, workspaceDir, cooperDir, "codex"); err != nil {
		t.Fatalf("StartBarrel(codex) failed: %v", err)
	}
	codexBarrel := docker.BarrelContainerName(workspaceDir, "codex")
	t.Cleanup(func() { docker.StopBarrel(codexBarrel) })
	waitForContainer(t, codexBarrel, 15*time.Second)

	// Both should be running.
	claudeRunning, _ := docker.IsBarrelRunning(claudeBarrel)
	codexRunning, _ := docker.IsBarrelRunning(codexBarrel)
	if !claudeRunning {
		t.Error("claude barrel not running")
	}
	if !codexRunning {
		t.Error("codex barrel not running")
	}

	// Both should have different names.
	if claudeBarrel == codexBarrel {
		t.Errorf("barrel names should differ: claude=%s, codex=%s", claudeBarrel, codexBarrel)
	}

	// Both see the same workspace: claude writes a file, codex reads it.
	testFile := filepath.Join(workspaceDir, "multi-barrel-test.txt")
	if _, err := barrelExec(claudeBarrel, fmt.Sprintf("echo hello > %s", testFile)); err != nil {
		t.Fatalf("claude barrel failed to write file: %v", err)
	}
	out, err := barrelExec(codexBarrel, fmt.Sprintf("cat %s", testFile))
	if err != nil {
		t.Fatalf("codex barrel failed to read file: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("codex barrel read %q, expected 'hello'", out)
	}
}

// TestCooperApp_ToolBarrelIsolation verifies that each tool barrel contains
// only its own AI tool binary and has the correct COOPER_CLI_TOOL env var.
func TestCooperApp_ToolBarrelIsolation(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	skipIfNoBarrelImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)

	app := NewCooperApp(cfg, cooperDir)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	t.Cleanup(func() {
		app.Stop()
		cleanupDocker(t)
	})

	workspaceDir := t.TempDir()

	// Start a claude barrel.
	if err := docker.StartBarrel(cfg, workspaceDir, cooperDir, "claude"); err != nil {
		t.Fatalf("StartBarrel(claude) failed: %v", err)
	}
	barrelName := docker.BarrelContainerName(workspaceDir, "claude")
	t.Cleanup(func() { docker.StopBarrel(barrelName) })
	waitForContainer(t, barrelName, 15*time.Second)

	// COOPER_CLI_TOOL should be "claude".
	out, err := barrelExec(barrelName, "echo $COOPER_CLI_TOOL")
	if err != nil {
		t.Fatalf("failed to check COOPER_CLI_TOOL: %v", err)
	}
	if strings.TrimSpace(out) != "claude" {
		t.Errorf("COOPER_CLI_TOOL = %q, want %q", strings.TrimSpace(out), "claude")
	}

	// Other AI tool binaries should NOT be present.
	for _, other := range []string{"copilot", "codex"} {
		out, err := barrelExec(barrelName, "which "+other+" 2>/dev/null")
		if err == nil && strings.TrimSpace(out) != "" {
			t.Errorf("%s binary found in claude barrel at %s (should not be present)", other, strings.TrimSpace(out))
		}
	}
}

// TestCooperApp_ToolBarrelAuthMounts verifies that each tool barrel only
// mounts its own auth directory, not other tools' auth directories.
func TestCooperApp_ToolBarrelAuthMounts(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	skipIfNoBarrelImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)

	app := NewCooperApp(cfg, cooperDir)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	t.Cleanup(func() {
		app.Stop()
		cleanupDocker(t)
	})

	workspaceDir := t.TempDir()

	// Start a claude barrel.
	if err := docker.StartBarrel(cfg, workspaceDir, cooperDir, "claude"); err != nil {
		t.Fatalf("StartBarrel(claude) failed: %v", err)
	}
	barrelName := docker.BarrelContainerName(workspaceDir, "claude")
	t.Cleanup(func() { docker.StopBarrel(barrelName) })
	waitForContainer(t, barrelName, 15*time.Second)

	// Claude barrel should have .claude mounted.
	out, err := barrelExec(barrelName, "mount | grep '/home/user/.claude'")
	if err != nil || strings.TrimSpace(out) == "" {
		t.Error("claude barrel should have /home/user/.claude mounted")
	}

	// Claude barrel should NOT have .copilot or .codex mounted.
	for _, dir := range []string{".copilot", ".codex"} {
		out, err := barrelExec(barrelName, "mount | grep '/home/user/"+dir+"'")
		if err == nil && strings.TrimSpace(out) != "" {
			t.Errorf("claude barrel should NOT have /home/user/%s mounted", dir)
		}
	}
}

// TestCooperApp_CustomToolImage creates a custom Dockerfile in cli/my-custom/,
// verifies it's picked up by DiscoverCLITools-style logic, and can be built.
func TestCooperApp_CustomToolImage(t *testing.T) {
	skipIfNoDocker(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	// Create a custom tool directory with a Dockerfile.
	customDir := filepath.Join(cooperDir, "cli", "my-custom")
	if err := os.MkdirAll(customDir, 0755); err != nil {
		t.Fatalf("mkdir custom dir: %v", err)
	}
	dockerfile := fmt.Sprintf("FROM %s\nRUN echo custom-test > /tmp/custom-marker.txt\nENV COOPER_CLI_TOOL=my-custom\n", docker.GetImageBase())
	if err := os.WriteFile(filepath.Join(customDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		t.Fatalf("write custom Dockerfile: %v", err)
	}

	// Verify the custom dir is discoverable by scanning cli/.
	cliDir := filepath.Join(cooperDir, "cli")
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		t.Fatalf("ReadDir cli: %v", err)
	}
	builtinNames := map[string]bool{"claude": true, "copilot": true, "codex": true, "opencode": true}
	found := false
	for _, e := range entries {
		if e.IsDir() && !builtinNames[e.Name()] && e.Name() == "my-custom" {
			found = true
		}
	}
	if !found {
		t.Error("my-custom not discovered in cli/ directory")
	}

	// Verify the custom image name follows convention.
	imageName := docker.GetImageCLI("my-custom")
	expected := testImagePrefix + "cooper-cli-my-custom"
	if imageName != expected {
		t.Errorf("GetImageCLI(my-custom) = %q, want %q", imageName, expected)
	}

	// Build the custom image (requires base image to exist).
	baseExists, _ := docker.ImageExists(docker.GetImageBase())
	if !baseExists {
		t.Fatal("base image not found after test bootstrap")
	}
	if err := docker.BuildImage(imageName, filepath.Join(customDir, "Dockerfile"), customDir, nil, false); err != nil {
		t.Fatalf("BuildImage(my-custom) failed: %v", err)
	}
	t.Cleanup(func() { docker.RemoveImage(imageName) })

	// Verify the image exists and has custom content.
	exists, _ := docker.ImageExists(imageName)
	if !exists {
		t.Fatal("custom image should exist after build")
	}

	out, err := exec.Command("docker", "run", "--rm", "--entrypoint", "", imageName, "cat", "/tmp/custom-marker.txt").CombinedOutput()
	if err != nil {
		t.Fatalf("custom image run failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "custom-test") {
		t.Errorf("custom image content = %q, want 'custom-test'", string(out))
	}

	// Verify COOPER_CLI_TOOL env.
	out, err = exec.Command("docker", "run", "--rm", "--entrypoint", "", imageName, "bash", "-c", "echo $COOPER_CLI_TOOL").CombinedOutput()
	if err != nil {
		t.Fatalf("custom image env check failed: %v", err)
	}
	if strings.TrimSpace(string(out)) != "my-custom" {
		t.Errorf("COOPER_CLI_TOOL = %q, want 'my-custom'", strings.TrimSpace(string(out)))
	}

	_ = cfg // cfg used by setupCooperDir
}

func TestCooperApp_CustomToolClipboardModeOffRespected(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	offImage := docker.GetImageCLI(testdocker.SharedClipboardOffToolName)
	offExists, _ := docker.ImageExists(offImage)
	if !offExists {
		t.Fatalf("shared clipboard-off image %s not found after test bootstrap", offImage)
	}

	app := startClipboardApp(t, cfg, cooperDir)
	defer app.Stop()

	workspaceDir := t.TempDir()
	barrelName := docker.BarrelContainerName(workspaceDir, testdocker.SharedClipboardOffToolName)
	token, err := clipboard.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken() failed: %v", err)
	}
	if _, err := clipboard.WriteTokenFile(cooperDir, barrelName, token); err != nil {
		t.Fatalf("WriteTokenFile() failed: %v", err)
	}
	t.Cleanup(func() { _ = clipboard.RemoveTokenFile(cooperDir, barrelName) })

	if err := docker.StartBarrel(cfg, workspaceDir, cooperDir, testdocker.SharedClipboardOffToolName); err != nil {
		t.Fatalf("StartBarrel(%s) failed: %v", testdocker.SharedClipboardOffToolName, err)
	}
	t.Cleanup(func() { _ = docker.StopBarrel(barrelName) })
	waitForContainer(t, barrelName, 15*time.Second)

	out, err := barrelExec(barrelName, `printf "%s" "$COOPER_CLIPBOARD_MODE"`)
	if err != nil {
		t.Fatalf("read COOPER_CLIPBOARD_MODE: %v", err)
	}
	if strings.TrimSpace(out) != "off" {
		t.Fatalf("COOPER_CLIPBOARD_MODE = %q, want off", strings.TrimSpace(out))
	}

	resp := clipboardGet(t, cfg.BridgePort, "/clipboard/type", token)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("custom off barrel clipboard status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// =====================================================================
// Clipboard integration tests
// =====================================================================

// startClipboardApp creates a CooperApp with the clipboard reader disabled
// (since integration tests don't have host clipboard tools), starts it,
// and returns the running app. The clipboard manager and HTTP handler are
// fully functional; only the host-reader prerequisite check is bypassed.
func startClipboardApp(t *testing.T, cfg *config.Config, cooperDir string) *CooperApp {
	t.Helper()
	app := NewCooperApp(cfg, cooperDir)
	// Disable the clipboard reader so Start() does not check for host
	// clipboard tools (wl-paste/xclip). The clipboard HTTP endpoints
	// are driven by the Manager, not the Reader.
	app.clipboardReader = nil

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	return app
}

// writeTestToken writes a token file to {cooperDir}/tokens/{name} and
// returns the token string.
func writeTestToken(t *testing.T, cooperDir, name string) string {
	t.Helper()
	token, err := clipboard.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken() failed: %v", err)
	}
	if _, err := clipboard.WriteTokenFile(cooperDir, name, token); err != nil {
		t.Fatalf("WriteTokenFile() failed: %v", err)
	}
	return token
}

func registerTestBarrelSession(t *testing.T, app *CooperApp, session clipboard.BarrelSession) string {
	t.Helper()
	if err := app.ClipboardManager().RegisterBarrel(session); err != nil {
		t.Fatalf("RegisterBarrel() failed: %v", err)
	}
	for _, s := range app.ClipboardManager().ActiveSessions() {
		if s.ContainerName == session.ContainerName {
			return s.Token
		}
	}
	t.Fatalf("could not find token for registered barrel %s", session.ContainerName)
	return ""
}

// clipboardGet performs a GET request to the clipboard endpoint with optional
// bearer token and returns the response.
func clipboardGet(t *testing.T, port int, path, token string) *http.Response {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest(%s) failed: %v", url, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	return resp
}

// TestCooperApp_ClipboardEndpointAuth verifies that clipboard endpoints
// require valid bearer token authentication. Valid tokens get 200, invalid
// tokens and missing auth both get 401.
func TestCooperApp_ClipboardEndpointAuth(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app := startClipboardApp(t, cfg, cooperDir)
	defer app.Stop()

	validToken := registerTestBarrelSession(t, app, clipboard.BarrelSession{
		ContainerName: "barrel-auth-test",
		ToolName:      "claude",
		ClipboardMode: "shim",
		Eligible:      true,
	})

	// GET /clipboard/type with valid token -> 200.
	resp := clipboardGet(t, cfg.BridgePort, "/clipboard/type", validToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("valid token: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// GET /clipboard/type with invalid token -> 401.
	resp = clipboardGet(t, cfg.BridgePort, "/clipboard/type", "bogus-invalid-token")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("invalid token: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	// GET /clipboard/type with no auth -> 401.
	resp = clipboardGet(t, cfg.BridgePort, "/clipboard/type", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no auth: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestCooperApp_ClipboardStageAndFetch verifies the full clipboard lifecycle:
// stage an image via the Manager, fetch metadata via /clipboard/type, fetch
// image bytes via /clipboard/image, clear, and verify 204 after clear.
func TestCooperApp_ClipboardStageAndFetch(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app := startClipboardApp(t, cfg, cooperDir)
	defer app.Stop()

	token := registerTestBarrelSession(t, app, clipboard.BarrelSession{
		ContainerName: "barrel-stage-test",
		ToolName:      "claude",
		ClipboardMode: "shim",
		Eligible:      true,
	})

	// Stage a clipboard object via the Manager directly.
	// Use a minimal valid PNG (1x1 pixel) to simulate a real clipboard capture.
	pngBytes := minimalPNG()
	obj := clipboard.ClipboardObject{
		Kind:    clipboard.ClipboardKindImage,
		MIME:    "image/png",
		Raw:     pngBytes,
		RawSize: int64(len(pngBytes)),
		Variants: map[string]clipboard.ClipboardVariant{
			"image/png": {
				MIME:  "image/png",
				Bytes: pngBytes,
				Size:  int64(len(pngBytes)),
			},
		},
	}
	ttl := time.Duration(cfg.ClipboardTTLSecs) * time.Second
	if _, err := app.ClipboardManager().Stage(obj, ttl); err != nil {
		t.Fatalf("Stage() failed: %v", err)
	}

	// GET /clipboard/type -> 200, state=staged, kind=image.
	resp := clipboardGet(t, cfg.BridgePort, "/clipboard/type", token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/clipboard/type status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var typeResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&typeResp); err != nil {
		t.Fatalf("decode /clipboard/type response: %v", err)
	}
	if typeResp["state"] != "staged" {
		t.Errorf("state = %v, want %q", typeResp["state"], "staged")
	}
	if typeResp["kind"] != "image" {
		t.Errorf("kind = %v, want %q", typeResp["kind"], "image")
	}

	// GET /clipboard/image -> 200 with PNG bytes matching what was staged.
	imgResp := clipboardGet(t, cfg.BridgePort, "/clipboard/image", token)
	defer imgResp.Body.Close()
	if imgResp.StatusCode != http.StatusOK {
		t.Fatalf("/clipboard/image status = %d, want %d", imgResp.StatusCode, http.StatusOK)
	}
	if imgResp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("Content-Type = %q, want %q", imgResp.Header.Get("Content-Type"), "image/png")
	}

	var imgBuf bytes.Buffer
	if _, err := imgBuf.ReadFrom(imgResp.Body); err != nil {
		t.Fatalf("read /clipboard/image body: %v", err)
	}
	if !bytes.Equal(imgBuf.Bytes(), pngBytes) {
		t.Errorf("image bytes mismatch: got %d bytes, want %d bytes", imgBuf.Len(), len(pngBytes))
	}

	// Clear clipboard.
	app.ClearClipboard()

	// GET /clipboard/image -> 204 after clear.
	clearedResp := clipboardGet(t, cfg.BridgePort, "/clipboard/image", token)
	clearedResp.Body.Close()
	if clearedResp.StatusCode != http.StatusNoContent {
		t.Errorf("after clear: /clipboard/image status = %d, want %d", clearedResp.StatusCode, http.StatusNoContent)
	}
}

// TestCooperApp_ClipboardIneligibleBarrel verifies that a barrel registered
// with Eligible=false receives 403 Forbidden when accessing clipboard endpoints.
func TestCooperApp_ClipboardIneligibleBarrel(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app := startClipboardApp(t, cfg, cooperDir)
	defer app.Stop()

	// Register a barrel with Eligible=false via the Manager.
	mgr := app.ClipboardManager()
	if err := mgr.RegisterBarrel(clipboard.BarrelSession{
		ContainerName: "barrel-ineligible",
		ToolName:      "test-tool",
		ClipboardMode: "off",
		Eligible:      false,
	}); err != nil {
		t.Fatalf("RegisterBarrel() failed: %v", err)
	}

	// Retrieve the token assigned to this barrel session.
	sessions := mgr.ActiveSessions()
	var token string
	for _, s := range sessions {
		if s.ContainerName == "barrel-ineligible" {
			token = s.Token
			break
		}
	}
	if token == "" {
		t.Fatal("could not find token for barrel-ineligible session")
	}

	// GET /clipboard/type -> 403.
	resp := clipboardGet(t, cfg.BridgePort, "/clipboard/type", token)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("ineligible barrel: status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// TestCooperApp_ClipboardTokenFromDisk verifies the supported disk-token path:
// a real running barrel with a token file mounted from {cooperDir}/tokens/.
func TestCooperApp_ClipboardTokenFromDisk(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	skipIfNoBarrelImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app := startClipboardApp(t, cfg, cooperDir)
	defer app.Stop()

	workspaceDir := t.TempDir()
	barrelName := docker.BarrelContainerName(workspaceDir, "claude")
	token := writeTestToken(t, cooperDir, barrelName)
	t.Cleanup(func() { _ = clipboard.RemoveTokenFile(cooperDir, barrelName) })
	if err := docker.StartBarrel(cfg, workspaceDir, cooperDir, "claude"); err != nil {
		t.Fatalf("StartBarrel(claude) failed: %v", err)
	}
	t.Cleanup(func() { _ = docker.StopBarrel(barrelName) })
	waitForContainer(t, barrelName, 15*time.Second)

	// GET /clipboard/type with the disk-based token -> 200.
	resp := clipboardGet(t, cfg.BridgePort, "/clipboard/type", token)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("disk-based token: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestCooperApp_ClipboardTTLExpiry stages an image with a very short TTL,
// waits for it to expire, and verifies the clipboard becomes inaccessible.
func TestCooperApp_ClipboardTTLExpiry(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	t.Cleanup(func() { cleanupDocker(t) })

	app := startClipboardApp(t, cfg, cooperDir)
	defer app.Stop()

	token := registerTestBarrelSession(t, app, clipboard.BarrelSession{
		ContainerName: "barrel-ttl-test",
		ToolName:      "claude",
		ClipboardMode: "shim",
		Eligible:      true,
	})

	// Stage with a very short TTL (1 second).
	pngBytes := minimalPNG()
	obj := clipboard.ClipboardObject{
		Kind:    clipboard.ClipboardKindImage,
		MIME:    "image/png",
		Raw:     pngBytes,
		RawSize: int64(len(pngBytes)),
		Variants: map[string]clipboard.ClipboardVariant{
			"image/png": {
				MIME:  "image/png",
				Bytes: pngBytes,
				Size:  int64(len(pngBytes)),
			},
		},
	}
	if _, err := app.ClipboardManager().Stage(obj, 1*time.Second); err != nil {
		t.Fatalf("Stage() failed: %v", err)
	}

	// Immediately: should be staged.
	resp := clipboardGet(t, cfg.BridgePort, "/clipboard/image", token)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("before expiry: /clipboard/image status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	waitForCondition(t, "clipboard TTL expiry", 3*time.Second, 100*time.Millisecond, func(_ int) (bool, string, error) {
		expiredResp := clipboardGet(t, cfg.BridgePort, "/clipboard/image", token)
		expiredResp.Body.Close()
		if expiredResp.StatusCode == http.StatusNoContent {
			return true, fmt.Sprintf("/clipboard/image status=%d", expiredResp.StatusCode), nil
		}
		return false, fmt.Sprintf("/clipboard/image status=%d", expiredResp.StatusCode), nil
	})

	// After expiry: /clipboard/type should return state=empty.
	expiredTypeResp := clipboardGet(t, cfg.BridgePort, "/clipboard/type", token)
	defer expiredTypeResp.Body.Close()
	if expiredTypeResp.StatusCode != http.StatusOK {
		t.Fatalf("after expiry: /clipboard/type status = %d, want %d", expiredTypeResp.StatusCode, http.StatusOK)
	}
	var typeResp map[string]interface{}
	if err := json.NewDecoder(expiredTypeResp.Body).Decode(&typeResp); err != nil {
		t.Fatalf("decode /clipboard/type response: %v", err)
	}
	if typeResp["state"] != "empty" {
		t.Errorf("after expiry: state = %v, want %q", typeResp["state"], "empty")
	}
}

// minimalPNG returns a valid 1x1 pixel white PNG image. This is the smallest
// valid PNG and avoids importing image/png just for test data.
func minimalPNG() []byte {
	// 1x1 white pixel PNG, manually constructed.
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, // 8-bit RGB
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, // IDAT chunk
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc,
		0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, // IEND chunk
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
}

// Ensure imported packages are used. These variables exist solely to prevent
// "imported and not used" compilation errors for packages that are used
// conditionally or in specific test helpers above.
var (
	_ = auth.ResolveTokens
	_ = names.Generate
)
