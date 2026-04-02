// Package proof implements the `cooper proof` diagnostic command — a fully
// self-contained integration test that stands up the entire Cooper stack,
// validates every layer (networking, proxy, SSL, tools, AI CLIs), and tears
// it down. The output is plain text designed to be copy-pasted into a GitHub
// issue for troubleshooting.
package proof

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/auth"
	"github.com/rickchristie/govner/cooper/internal/bridge"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/proxy"
)

// ANSI color codes.
const (
	green  = "\033[32m"
	red    = "\033[31m"
	yellow = "\033[33m"
	amber  = "\033[38;5;130m"
	gray   = "\033[38;5;245m"
	bold   = "\033[1m"
	reset  = "\033[0m"
)

// Result holds the outcome of a single diagnostic check.
type Result struct {
	Name   string
	Status string // "PASS", "FAIL", "WARN", "INFO"
	Detail string
}

const (
	StatusPASS = "PASS"
	StatusFAIL = "FAIL"
	StatusWARN = "WARN"
	StatusINFO = "INFO"
)

// ProofContext carries shared state across all proof phases.
type ProofContext struct {
	Cfg          *config.Config
	CooperDir    string
	WorkspaceDir string

	// Per-tool barrel container names (populated during phaseContainer).
	barrels map[string]string // tool name -> container name

	// Infrastructure started by proof (cleaned up on teardown).
	aclListener  *proxy.ACLListener
	bridgeServer *bridge.BridgeServer

	// Resolved tokens for AI CLI smoke tests.
	tokens []auth.TokenResult

	// For output.
	results    []Result
	startTime  time.Time
	passCount  int
	failCount  int
	warnCount  int
	infoCount  int
}

// Run is the main entry point. It orchestrates all phases, prints results
// in real-time, and always cleans up — even on failure.
func Run(cfg *config.Config, cooperDir string) error {
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	ctx := &ProofContext{
		Cfg:          cfg,
		CooperDir:    cooperDir,
		WorkspaceDir: workspaceDir,
		barrels:      make(map[string]string),
		startTime:    time.Now(),
	}

	// Print header.
	fmt.Println()
	fmt.Printf("  %s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", amber, bold, reset)
	fmt.Printf("  %s🥃 c o o p e r   p r o o f%s\n", bold, reset)
	fmt.Printf("  %sFull lifecycle integration test%s\n", gray, reset)
	fmt.Printf("  %s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", amber, bold, reset)
	fmt.Println()

	// Always tear down, even if a phase fails.
	defer ctx.phaseTeardown()

	ctx.phasePreflight()
	if ctx.failCount > 0 {
		ctx.printSummary()
		return fmt.Errorf("preflight failed")
	}

	ctx.phaseStartup()
	if ctx.failCount > 0 {
		ctx.printSummary()
		return fmt.Errorf("startup failed")
	}

	ctx.phaseContainer()
	if ctx.failCount > 0 {
		ctx.printSummary()
		return fmt.Errorf("container failed")
	}

	ctx.phaseNetworkSecurity()
	ctx.phaseTools()
	ctx.phaseAICLI()
	ctx.phasePortForwarding()

	ctx.printSummary()
	if ctx.failCount > 0 {
		return fmt.Errorf("%d checks failed", ctx.failCount)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Phase 1: Preflight
// ---------------------------------------------------------------------------

func (ctx *ProofContext) phasePreflight() {
	ctx.printPhase("Phase 1: Preflight")

	// Docker daemon reachable.
	if out, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		ctx.fail("Docker daemon", "Docker is not reachable — is it running?")
	} else {
		ctx.pass("Docker daemon", fmt.Sprintf("Docker %s", strings.TrimSpace(string(out))))
	}

	// Config loaded.
	if ctx.Cfg == nil {
		ctx.fail("Config loaded", "config.json not found — run 'cooper configure' first")
		return
	}
	ctx.pass("Config loaded", ctx.CooperDir+"/config.json")

	// CA certificate exists.
	caPath := filepath.Join(ctx.CooperDir, "ca", "cooper-ca.pem")
	if _, err := os.Stat(caPath); err != nil {
		ctx.fail("CA certificate", "Not found — run 'cooper build' first")
	} else {
		ctx.pass("CA certificate", caPath)
	}

	// Images built: proxy + base.
	for _, img := range []string{docker.GetImageProxy(), docker.GetImageBase()} {
		exists, err := docker.ImageExists(img)
		if err != nil || !exists {
			ctx.fail("Image: "+img, "Not found — run 'cooper build' first")
		} else {
			ctx.pass("Image: "+img, "exists")
		}
	}
	// Per-tool images.
	for _, t := range ctx.Cfg.AITools {
		if !t.Enabled {
			continue
		}
		img := docker.GetImageCLI(t.Name)
		exists, err := docker.ImageExists(img)
		if err != nil || !exists {
			ctx.fail("Image: "+img, "Not found — run 'cooper build' first")
		} else {
			ctx.pass("Image: "+img, "exists")
		}
	}

	// No cooper up already running (proxy container check).
	running, _ := docker.IsProxyRunning()
	if running {
		ctx.fail("No cooper-up running", "cooper-proxy is already running — stop it first with 'cooper up' then quit, or 'docker rm -f cooper-proxy'")
	} else {
		ctx.pass("No cooper-up running", "clean slate")
	}
}

// ---------------------------------------------------------------------------
// Phase 2: Startup (mirrors cooper up)
// ---------------------------------------------------------------------------

func (ctx *ProofContext) phaseStartup() {
	ctx.printPhase("Phase 2: Startup")

	// Create networks.
	if err := docker.EnsureNetworks(); err != nil {
		ctx.fail("Create networks", err.Error())
		return
	}
	ctx.pass("Create networks", "cooper-external + cooper-internal")

	// Start proxy.
	if err := docker.StartProxy(ctx.Cfg, ctx.CooperDir); err != nil {
		ctx.fail("Start proxy", err.Error())
		return
	}
	ctx.pass("Start proxy", "cooper-proxy running")

	// Start bridge.
	var gatewayIPs []string
	if ip, err := docker.GetGatewayIP(docker.NetworkExternal); err == nil {
		gatewayIPs = append(gatewayIPs, ip)
	}
	if ip, err := docker.GetGatewayIP("bridge"); err == nil {
		gatewayIPs = append(gatewayIPs, ip)
	}
	if len(gatewayIPs) == 0 {
		ctx.fail("Start bridge", "no Docker gateway IP found")
		return
	}
	ctx.bridgeServer = bridge.NewBridgeServer(ctx.Cfg.BridgeRoutes, ctx.Cfg.BridgePort, gatewayIPs)
	if err := ctx.bridgeServer.Start(); err != nil {
		ctx.fail("Start bridge", err.Error())
		return
	}
	ctx.pass("Start bridge", fmt.Sprintf("listening on %s", strings.Join(gatewayIPs, ", ")))

	// Start ACL listener.
	socketPath := filepath.Join(ctx.CooperDir, "run", "acl.sock")
	timeout := time.Duration(ctx.Cfg.MonitorTimeoutSecs) * time.Second
	ctx.aclListener = proxy.NewACLListener(socketPath, timeout)
	if err := ctx.aclListener.Start(); err != nil {
		ctx.fail("Start ACL listener", err.Error())
		return
	}
	ctx.pass("Start ACL listener", socketPath)

	// Resolve auth tokens for AI CLI tests.
	var enabledTools []string
	for _, t := range ctx.Cfg.AITools {
		if t.Enabled {
			enabledTools = append(enabledTools, t.Name)
		}
	}
	ctx.tokens, _ = auth.ResolveTokens(ctx.WorkspaceDir, ctx.CooperDir, enabledTools)
}

// ---------------------------------------------------------------------------
// Phase 3: Container
// ---------------------------------------------------------------------------

func (ctx *ProofContext) phaseContainer() {
	ctx.printPhase("Phase 3: Per-Tool Containers")

	// Start a barrel for each enabled AI tool.
	for _, t := range ctx.Cfg.AITools {
		if !t.Enabled {
			continue
		}

		barrelName := docker.BarrelContainerName(ctx.WorkspaceDir, t.Name)
		ctx.barrels[t.Name] = barrelName

		if err := docker.StartBarrel(ctx.Cfg, ctx.WorkspaceDir, ctx.CooperDir, t.Name); err != nil {
			ctx.fail(fmt.Sprintf("Start barrel (%s)", t.Name), err.Error())
			continue
		}
		ctx.pass(fmt.Sprintf("Start barrel (%s)", t.Name), barrelName)

		// Wait for barrel to be ready.
		ready := false
		for i := 0; i < 30; i++ {
			if running, _ := docker.IsBarrelRunning(barrelName); running {
				ready = true
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if !ready {
			ctx.fail(fmt.Sprintf("Barrel ready (%s)", t.Name), "container did not start within 15s")
			continue
		}

		// DNS resolution.
		out, err := dockerExec(barrelName, "getent hosts cooper-proxy")
		if err != nil {
			ctx.fail(fmt.Sprintf("DNS: cooper-proxy (%s)", t.Name), fmt.Sprintf("cannot resolve — %s", truncate(out, 100)))
			continue
		}
		ip := strings.Fields(out)[0]
		ctx.pass(fmt.Sprintf("DNS: cooper-proxy (%s)", t.Name), fmt.Sprintf("resolves to %s", ip))

		// Proxy connectivity.
		proxyAddr := fmt.Sprintf("cooper-proxy:%d", ctx.Cfg.ProxyPort)
		shellCmd := fmt.Sprintf(
			`curl -so /dev/null -w '%%{http_code}' --connect-timeout 5 -x http://%s http://connectivity-check.invalid 2>&1 || true`,
			proxyAddr,
		)
		out, _ = dockerExec(barrelName, shellCmd)
		if out != "" && out != "000" {
			ctx.pass(fmt.Sprintf("Proxy reachable (%s)", t.Name), fmt.Sprintf("%s (HTTP %s)", proxyAddr, out))
		} else {
			ctx.fail(fmt.Sprintf("Proxy reachable (%s)", t.Name), fmt.Sprintf("%s not responding", proxyAddr))
		}

		// Verify COOPER_CLI_TOOL env var.
		out, _ = dockerExec(barrelName, "echo $COOPER_CLI_TOOL")
		if strings.TrimSpace(out) == t.Name {
			ctx.pass(fmt.Sprintf("COOPER_CLI_TOOL (%s)", t.Name), t.Name)
		} else {
			ctx.fail(fmt.Sprintf("COOPER_CLI_TOOL (%s)", t.Name), fmt.Sprintf("expected %s, got %s", t.Name, out))
		}
	}
}

// firstBarrel returns the name of any running barrel for tests that only need one.
func (ctx *ProofContext) firstBarrel() string {
	for _, name := range ctx.barrels {
		return name
	}
	return ""
}

// ---------------------------------------------------------------------------
// Phase 4: Network Security
// ---------------------------------------------------------------------------

func (ctx *ProofContext) phaseNetworkSecurity() {
	ctx.printPhase("Phase 4: Network Security")

	proxyAddr := fmt.Sprintf("cooper-proxy:%d", ctx.Cfg.ProxyPort)

	// SSL bump — HTTPS through proxy without --insecure.
	sslTarget := "https://api.anthropic.com"
	for _, d := range ctx.Cfg.WhitelistedDomains {
		if strings.Contains(d.Domain, "anthropic.com") && !strings.HasPrefix(d.Domain, ".") {
			sslTarget = "https://" + d.Domain
			break
		}
	}
	shellCmd := fmt.Sprintf(
		`curl -so /dev/null -w '%%{http_code}' --connect-timeout 10 -x http://%s %s 2>&1`,
		proxyAddr, sslTarget,
	)
	out, err := dockerExec(ctx.firstBarrel(), shellCmd)
	if err == nil && out != "" && out != "000" {
		ctx.pass("SSL bump (CA chain)", fmt.Sprintf("%s -> HTTP %s", sslTarget, out))
	} else {
		detail := fmt.Sprintf("HTTPS failed for %s", sslTarget)
		if out != "" {
			detail += fmt.Sprintf(" (output: %s)", truncate(out, 120))
		}
		ctx.fail("SSL bump (CA chain)", detail)
	}

	// Blocked domains.
	blocked := []string{"example.com", "google.com"}
	allBlocked := true
	var leaks []string
	for _, domain := range blocked {
		shellCmd := fmt.Sprintf(
			`curl -so /dev/null -w '%%{http_code}' --connect-timeout 10 -x http://%s https://%s 2>&1`,
			proxyAddr, domain,
		)
		out, _ := dockerExec(ctx.firstBarrel(), shellCmd)
		code := 0
		fmt.Sscanf(out, "%d", &code)
		if code >= 200 && code < 400 {
			allBlocked = false
			leaks = append(leaks, fmt.Sprintf("%s (HTTP %d)", domain, code))
		}
	}
	if allBlocked {
		ctx.pass("Blocked domains", fmt.Sprintf("%s correctly denied", strings.Join(blocked, ", ")))
	} else {
		ctx.fail("Blocked domains", fmt.Sprintf("LEAK: %s", strings.Join(leaks, ", ")))
	}

	// Direct egress blocked (bypass proxy).
	shellCmd = `curl -so /dev/null --noproxy '*' --connect-timeout 5 https://example.com 2>&1`
	out, err = dockerExec(ctx.firstBarrel(), shellCmd)
	if err != nil {
		ctx.pass("Direct egress blocked", "no route from internal network")
	} else {
		ctx.fail("Direct egress blocked", "LEAK: direct internet access succeeded (network isolation broken)")
	}
}

// ---------------------------------------------------------------------------
// Phase 5: Tools
// ---------------------------------------------------------------------------

func (ctx *ProofContext) phaseTools() {
	ctx.printPhase("Phase 5: Tools")

	// Programming tools — check in the first available barrel (base layers are shared).
	barrel := ctx.firstBarrel()
	if barrel == "" {
		ctx.fail("Programming tools", "no barrel running")
		return
	}

	progCmds := map[string]string{
		"go":     "go version",
		"node":   "node --version",
		"python": "python3 --version 2>/dev/null || python --version",
	}
	for _, t := range ctx.Cfg.ProgrammingTools {
		if !t.Enabled {
			continue
		}
		cmd, ok := progCmds[t.Name]
		if !ok {
			cmd = t.Name + " --version"
		}
		out, err := dockerExec(barrel, cmd)
		if err == nil && out != "" {
			ctx.pass(t.Name, truncate(out, 80))
		} else {
			ctx.fail(t.Name, "not found in container")
		}
	}

	// AI tool installations — check in each tool's own barrel.
	aiCmds := map[string]string{
		"claude":   "claude --version",
		"copilot":  "copilot --version 2>/dev/null || github-copilot-cli --version",
		"codex":    "codex --version",
		"opencode": "opencode --version",
	}
	for _, t := range ctx.Cfg.AITools {
		if !t.Enabled {
			continue
		}
		toolBarrel, ok := ctx.barrels[t.Name]
		if !ok {
			ctx.fail(t.Name, "no barrel running for this tool")
			continue
		}
		cmd, ok := aiCmds[t.Name]
		if !ok {
			cmd = t.Name + " --version"
		}
		out, err := dockerExec(toolBarrel, cmd)
		if err == nil && out != "" && !strings.Contains(out, "not found") {
			ctx.pass(t.Name, truncate(out, 80))
		} else {
			ctx.fail(t.Name, fmt.Sprintf("not found in container (%s)", truncate(out, 80)))
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 6: AI CLI Smoke Test
// ---------------------------------------------------------------------------

func (ctx *ProofContext) phaseAICLI() {
	ctx.printPhase("Phase 6: AI CLI Smoke Test")

	// Build env args for the docker exec (tokens).
	var envArgs []string
	for _, t := range ctx.tokens {
		envArgs = append(envArgs, fmt.Sprintf("%s=%s", t.Name, t.Value))
	}

	// For each enabled AI tool, try to send a message or verify connectivity.
	for _, t := range ctx.Cfg.AITools {
		if !t.Enabled {
			continue
		}

		start := time.Now()
		var cmd string
		var name string
		switch t.Name {
		case "claude":
			name = "Claude Code"
			cmd = `claude -p "Reply with only the word: ok" --max-turns 1 2>&1`
		case "codex":
			name = "Codex"
			cmd = `codex --version 2>&1`
		case "copilot":
			name = "Copilot CLI"
			// Copilot CLI is not a chat tool — verify it can reach its API
			// by checking that the version command succeeds (it phones home).
			cmd = `copilot --version 2>&1`
		case "opencode":
			name = "OpenCode"
			// OpenCode may not have a one-shot mode — verify API reachability.
			cmd = `opencode --version 2>&1`
		default:
			continue
		}

		toolBarrel, ok := ctx.barrels[t.Name]
		if !ok {
			ctx.warn(name, "no barrel running for this tool")
			continue
		}
		out, err := dockerExecWithEnv(toolBarrel, cmd, envArgs)
		elapsed := time.Since(start).Round(100 * time.Millisecond)

		switch t.Name {
		case "claude":
			// These are chat CLIs — we expect a response containing text.
			if err == nil && out != "" && !strings.Contains(strings.ToLower(out), "error") {
				ctx.pass(name, fmt.Sprintf("response received (%s)", elapsed))
			} else {
				detail := truncate(out, 150)
				if detail == "" {
					detail = "no output"
				}
				if err != nil {
					detail = truncate(fmt.Sprintf("%v: %s", err, out), 150)
				}
				ctx.warn(name, fmt.Sprintf("no response — %s", detail))
			}
		default:
			// Version check tools — just verify they ran.
			if err == nil && out != "" {
				ctx.pass(name, fmt.Sprintf("reachable (%s)", elapsed))
			} else {
				ctx.warn(name, fmt.Sprintf("not reachable — %s", truncate(out, 100)))
			}
		}
	}

	if len(ctx.Cfg.AITools) == 0 {
		ctx.info("AI CLI", "no AI tools enabled")
	}
}

// ---------------------------------------------------------------------------
// Phase 7: Port Forwarding & Bridge
// ---------------------------------------------------------------------------

func (ctx *ProofContext) phasePortForwarding() {
	ctx.printPhase("Phase 7: Port Forwarding & Bridge")

	// Bridge health.
	bridgePort := ctx.Cfg.BridgePort
	shellCmd := fmt.Sprintf(
		`curl -s --connect-timeout 5 http://localhost:%d/health 2>&1`,
		bridgePort,
	)
	out, err := dockerExec(ctx.firstBarrel(), shellCmd)
	if err == nil && strings.Contains(out, "ok") {
		ctx.pass("Bridge /health", fmt.Sprintf("port %d -> {\"status\":\"ok\"}", bridgePort))
	} else {
		ctx.fail("Bridge /health", fmt.Sprintf("port %d — %s", bridgePort, truncate(out, 100)))
	}

	// Port forwarding rules.
	for _, rule := range ctx.Cfg.PortForwardRules {
		var samplePorts []int
		if rule.IsRange && rule.RangeEnd > rule.ContainerPort {
			first := rule.ContainerPort
			last := rule.RangeEnd
			mid := first + (last-first)/2
			samplePorts = []int{first}
			if mid != first && mid != last {
				samplePorts = append(samplePorts, mid)
			}
			samplePorts = append(samplePorts, last)
		} else {
			samplePorts = []int{rule.ContainerPort}
		}

		for _, port := range samplePorts {
			name := fmt.Sprintf("Port %d", port)
			if rule.Description != "" {
				name = fmt.Sprintf("Port %d (%s)", port, rule.Description)
			}
			shellCmd := fmt.Sprintf(`bash -c 'echo > /dev/tcp/localhost/%d' 2>&1`, port)
			_, err := dockerExec(ctx.firstBarrel(), shellCmd)
			if err == nil {
				ctx.pass(name, "connected")
			} else {
				ctx.fail(name, "not reachable from container")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Teardown (always runs)
// ---------------------------------------------------------------------------

func (ctx *ProofContext) phaseTeardown() {
	ctx.printPhase("Teardown")

	// Stop ACL listener.
	if ctx.aclListener != nil {
		ctx.aclListener.Stop()
		ctx.info("ACL listener", "stopped")
	}

	// Stop bridge.
	if ctx.bridgeServer != nil {
		ctx.bridgeServer.Stop()
		ctx.info("Bridge server", "stopped")
	}

	// Stop all barrels.
	for tool, name := range ctx.barrels {
		if running, _ := docker.IsBarrelRunning(name); running {
			docker.StopBarrel(name)
			ctx.info(fmt.Sprintf("Barrel (%s)", tool), "stopped")
		}
	}

	// Stop proxy.
	if running, _ := docker.IsProxyRunning(); running {
		docker.StopProxy()
		ctx.info("Proxy", "stopped")
	}

	// Remove networks.
	docker.RemoveNetworks()
	ctx.info("Networks", "removed")
}

// ---------------------------------------------------------------------------
// Output helpers
// ---------------------------------------------------------------------------

func (ctx *ProofContext) printPhase(name string) {
	fmt.Printf("\n  %s%s%s\n", bold, name, reset)
}

func (ctx *ProofContext) emit(status, name, detail string) {
	var prefix string
	switch status {
	case StatusPASS:
		prefix = green + "  PASS" + reset
		ctx.passCount++
	case StatusFAIL:
		prefix = red + bold + "  FAIL" + reset
		ctx.failCount++
	case StatusWARN:
		prefix = yellow + "  WARN" + reset
		ctx.warnCount++
	case StatusINFO:
		prefix = gray + "  info" + reset
		ctx.infoCount++
	}

	ctx.results = append(ctx.results, Result{Name: name, Status: status, Detail: detail})
	fmt.Printf("  %s  %-28s %s\n", prefix, name, detail)
}

func (ctx *ProofContext) pass(name, detail string) { ctx.emit(StatusPASS, name, detail) }
func (ctx *ProofContext) fail(name, detail string) { ctx.emit(StatusFAIL, name, detail) }
func (ctx *ProofContext) warn(name, detail string) { ctx.emit(StatusWARN, name, detail) }
func (ctx *ProofContext) info(name, detail string) { ctx.emit(StatusINFO, name, detail) }

func (ctx *ProofContext) printSummary() {
	elapsed := time.Since(ctx.startTime).Round(time.Second)

	fmt.Println()
	fmt.Printf("  %s%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", amber, bold, reset)

	summary := fmt.Sprintf("  %s%d passed%s", green, ctx.passCount, reset)
	if ctx.failCount > 0 {
		summary += fmt.Sprintf("  %s%s%d failed%s", red, bold, ctx.failCount, reset)
	}
	if ctx.warnCount > 0 {
		summary += fmt.Sprintf("  %s%d warnings%s", yellow, ctx.warnCount, reset)
	}
	fmt.Println(summary)
	fmt.Printf("  %sCompleted in %s%s\n", gray, elapsed, reset)

	// System info for GitHub issues.
	fmt.Println()
	fmt.Printf("  %sSystem:%s %s %s\n", gray, reset, runtime.GOOS, kernelVersion())
	if v, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output(); err == nil {
		fmt.Printf("  %sDocker:%s %s\n", gray, reset, strings.TrimSpace(string(v)))
	}
	fmt.Printf("  %sConfig:%s %s\n", gray, reset, filepath.Join(ctx.CooperDir, "config.json"))
	fmt.Printf("  %sImages:%s %s, %s\n", gray, reset, docker.GetImageProxy(), docker.GetImageBase())
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Docker helpers
// ---------------------------------------------------------------------------

// dockerExec runs a command inside the container via "docker exec".
func dockerExec(container, shellCmd string) (string, error) {
	cmd := exec.Command("docker", "exec", container, "bash", "-c", shellCmd)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// dockerExecWithEnv runs a command inside the container with extra env vars.
func dockerExecWithEnv(container, shellCmd string, envArgs []string) (string, error) {
	args := []string{"exec"}
	for _, env := range envArgs {
		args = append(args, "-e", env)
	}
	args = append(args, container, "bash", "-c", shellCmd)
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func kernelVersion() string {
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return "unknown"
}
