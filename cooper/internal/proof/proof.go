package proof

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/config"
)

// ProofResult holds the outcome of a single diagnostic check.
type ProofResult struct {
	Name   string // Human-readable check name.
	Status string // "OK", "FAIL", or "INFO".
	Detail string // Explanation of what was found.
}

const (
	StatusOK   = "OK"
	StatusFAIL = "FAIL"
	StatusINFO = "INFO"
)

// RunAllChecks executes every diagnostic check inside the given barrel
// container and returns the collected results. Each check is independent;
// a failure in one does not prevent the others from running.
func RunAllChecks(containerName string, cfg *config.Config) ([]ProofResult, error) {
	if containerName == "" {
		return nil, fmt.Errorf("container name must not be empty")
	}

	var results []ProofResult

	// Collect whitelisted domain strings for the domain reachability check.
	var domains []string
	for _, d := range cfg.WhitelistedDomains {
		// Skip wildcard-prefix entries (e.g. ".anthropic.com") -- use the
		// canonical hostname instead so curl has something to resolve.
		domain := d.Domain
		if strings.HasPrefix(domain, ".") {
			domain = "api" + domain // e.g. ".anthropic.com" -> "api.anthropic.com"
		}
		domains = append(domains, domain)
	}

	// Collect tool names from enabled AI tools.
	var aiTools []string
	for _, t := range cfg.AITools {
		if t.Enabled {
			aiTools = append(aiTools, t.Name)
		}
	}

	// Collect tool names from enabled programming tools.
	var progTools []string
	for _, t := range cfg.ProgrammingTools {
		if t.Enabled {
			progTools = append(progTools, t.Name)
		}
	}

	proxyAddr := fmt.Sprintf("cooper-proxy:%d", cfg.ProxyPort)
	bridgePort := strconv.Itoa(cfg.BridgePort)

	// --- Run all checks ---

	results = append(results, checkProxyConnectivity(containerName, proxyAddr))
	results = append(results, checkSSLBump(containerName, proxyAddr, domains))
	results = append(results, checkWhitelistedDomains(containerName, proxyAddr, domains)...)
	results = append(results, checkBlockedDomains(containerName, proxyAddr))
	results = append(results, checkDirectEgress(containerName))
	results = append(results, checkPortForwarding(containerName, cfg.PortForwardRules)...)
	results = append(results, checkProgrammingToolVersions(containerName, progTools)...)
	results = append(results, checkToolInstallations(containerName, aiTools)...)
	results = append(results, checkAuth(containerName))
	results = append(results, checkBridgeReachability(containerName, bridgePort))

	return results, nil
}

// dockerExec runs a command inside the container via "docker exec" and returns
// combined stdout+stderr and the exit error (nil on success).
func dockerExec(container, shellCmd string) (string, error) {
	cmd := exec.Command("docker", "exec", container, "bash", "-c", shellCmd)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ---------------------------------------------------------------------------
// Individual check functions
// ---------------------------------------------------------------------------

// checkProxyConnectivity verifies that the Squid proxy port inside the
// cooper-internal network is reachable from the barrel container.
func checkProxyConnectivity(container, proxyAddr string) ProofResult {
	// Use curl to hit the proxy; any response (even 400) means it's up.
	shellCmd := fmt.Sprintf(
		`curl -so /dev/null -w '%%{http_code}' --connect-timeout 5 -x http://%s http://connectivity-check.invalid 2>&1 || true`,
		proxyAddr,
	)
	out, _ := dockerExec(container, shellCmd)

	// Squid returns a status code (e.g. 503 for denied, 407 for auth) when
	// reachable. A completely empty or "000" response means no connectivity.
	if out != "" && out != "000" {
		return ProofResult{
			Name:   "Proxy Connectivity",
			Status: StatusOK,
			Detail: fmt.Sprintf("Squid proxy reachable at %s (HTTP %s)", proxyAddr, out),
		}
	}
	return ProofResult{
		Name:   "Proxy Connectivity",
		Status: StatusFAIL,
		Detail: fmt.Sprintf("Squid proxy NOT reachable at %s", proxyAddr),
	}
}

// checkSSLBump makes an HTTPS request through the proxy to a whitelisted
// domain and verifies no certificate errors occur. This is the critical
// end-to-end validation of the CA chain: generated -> injected into
// container -> trusted by system CA store -> SSL bump decryption works.
func checkSSLBump(container, proxyAddr string, whitelistedDomains []string) ProofResult {
	// Pick a whitelisted domain to test against. Prefer an API endpoint
	// that is likely to return quickly.
	target := "https://api.anthropic.com"
	for _, d := range whitelistedDomains {
		if strings.Contains(d, "anthropic.com") {
			target = "https://" + d
			break
		}
	}

	// The key here: we do NOT pass --insecure. If the CA chain is broken,
	// curl will fail with a certificate error.
	shellCmd := fmt.Sprintf(
		`curl -so /dev/null -w '%%{http_code}' --connect-timeout 10 -x http://%s %s 2>&1`,
		proxyAddr, target,
	)
	out, err := dockerExec(container, shellCmd)

	if err == nil && out != "" && out != "000" {
		return ProofResult{
			Name:   "SSL Bump (CA Chain)",
			Status: StatusOK,
			Detail: fmt.Sprintf("HTTPS through proxy succeeded without cert errors (%s -> HTTP %s)", target, out),
		}
	}

	detail := fmt.Sprintf("HTTPS through proxy FAILED for %s", target)
	if out != "" {
		detail += fmt.Sprintf(" (output: %s)", out)
	}
	return ProofResult{
		Name:   "SSL Bump (CA Chain)",
		Status: StatusFAIL,
		Detail: detail,
	}
}

// checkWhitelistedDomains curls each whitelisted domain through the proxy
// and verifies it is reachable (any 2xx/3xx/4xx from the origin counts).
func checkWhitelistedDomains(container, proxyAddr string, domains []string) []ProofResult {
	if len(domains) == 0 {
		return []ProofResult{{
			Name:   "Whitelisted Domains",
			Status: StatusINFO,
			Detail: "No whitelisted domains configured",
		}}
	}

	var results []ProofResult
	for _, domain := range domains {
		url := "https://" + domain
		shellCmd := fmt.Sprintf(
			`curl -so /dev/null -w '%%{http_code}' --connect-timeout 10 -x http://%s %s 2>&1`,
			proxyAddr, url,
		)
		out, _ := dockerExec(container, shellCmd)

		name := fmt.Sprintf("Whitelist: %s", domain)

		// A real HTTP status (2xx-5xx) from the origin means the proxy
		// allowed the connection through.
		if out != "" && out != "000" {
			code := 0
			fmt.Sscanf(out, "%d", &code)
			if code >= 200 && code < 600 {
				results = append(results, ProofResult{
					Name:   name,
					Status: StatusOK,
					Detail: fmt.Sprintf("Reachable (HTTP %s)", out),
				})
				continue
			}
		}

		results = append(results, ProofResult{
			Name:   name,
			Status: StatusFAIL,
			Detail: fmt.Sprintf("Not reachable (got: %s)", out),
		})
	}
	return results
}

// checkBlockedDomains verifies that non-whitelisted domains are blocked by
// the proxy (expecting HTTP 403 from Squid).
func checkBlockedDomains(container, proxyAddr string) ProofResult {
	blocked := []string{"example.com", "google.com"}
	var failures []string

	for _, domain := range blocked {
		url := "https://" + domain
		// Fetch the HTTP status code through the proxy -- Squid should
		// return 403 for blocked domains.
		shellCmd := fmt.Sprintf(
			`curl -so /dev/null -w '%%{http_code}' --connect-timeout 10 -x http://%s %s 2>&1`,
			proxyAddr, url,
		)
		out, _ := dockerExec(container, shellCmd)

		// Parse the HTTP status code. Squid returns 403 for denied
		// domains. Any 2xx/3xx response means the request leaked through.
		code := 0
		fmt.Sscanf(out, "%d", &code)
		if code >= 200 && code < 400 {
			failures = append(failures, fmt.Sprintf("%s (HTTP %d)", domain, code))
		}
		// 403, 000 (connection refused), or other 4xx/5xx are all fine
		// -- the domain was blocked.
	}

	if len(failures) == 0 {
		return ProofResult{
			Name:   "Blocked Domains",
			Status: StatusOK,
			Detail: fmt.Sprintf("Non-whitelisted domains correctly blocked (%s)", strings.Join(blocked, ", ")),
		}
	}
	return ProofResult{
		Name:   "Blocked Domains",
		Status: StatusFAIL,
		Detail: fmt.Sprintf("LEAK: the following domains were accessible: %s", strings.Join(failures, ", ")),
	}
}

// checkDirectEgress attempts to reach the internet directly (bypassing the
// proxy). On the cooper-internal network this MUST fail with "no route to
// host" or similar -- NOT a proxy error. This validates the --internal
// network has no gateway.
func checkDirectEgress(container string) ProofResult {
	// --noproxy '*' forces curl to ignore proxy env vars and connect directly.
	shellCmd := `curl -so /dev/null --noproxy '*' --connect-timeout 5 https://example.com 2>&1`
	out, err := dockerExec(container, shellCmd)

	if err != nil {
		// Any failure is expected. Distinguish between network-level failure
		// (good) and proxy-level failure (bad -- means proxy vars leaked).
		lower := strings.ToLower(out)
		if strings.Contains(lower, "no route to host") ||
			strings.Contains(lower, "network is unreachable") ||
			strings.Contains(lower, "connection timed out") ||
			strings.Contains(lower, "couldn't connect to server") ||
			strings.Contains(lower, "connection refused") {
			return ProofResult{
				Name:   "Direct Egress Blocked",
				Status: StatusOK,
				Detail: "Direct internet access correctly blocked (no route from internal network)",
			}
		}
		// Some other failure -- still blocked, but report what we saw.
		return ProofResult{
			Name:   "Direct Egress Blocked",
			Status: StatusOK,
			Detail: fmt.Sprintf("Direct internet access blocked (%s)", truncate(out, 120)),
		}
	}

	// If curl succeeded, direct egress is possible -- this is a security leak.
	return ProofResult{
		Name:   "Direct Egress Blocked",
		Status: StatusFAIL,
		Detail: "LEAK: direct internet access succeeded bypassing proxy (network isolation broken)",
	}
}

// checkPortForwarding tests connectivity for each configured port forwarding
// rule from inside the container. Ports are forwarded via socat through the
// proxy container. For range rules, a sample of ports is probed (first,
// middle, last) rather than every port in the range.
func checkPortForwarding(container string, rules []config.PortForwardRule) []ProofResult {
	if len(rules) == 0 {
		return []ProofResult{{
			Name:   "Port Forwarding",
			Status: StatusINFO,
			Detail: "No port forwarding rules configured",
		}}
	}

	var results []ProofResult
	for _, rule := range rules {
		// For range rules, probe a sample: first, middle, and last port.
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
			name := fmt.Sprintf("Port Forward: %d", port)
			if rule.Description != "" {
				name = fmt.Sprintf("Port Forward: %d (%s)", port, rule.Description)
			}

			shellCmd := fmt.Sprintf(
				`bash -c 'echo > /dev/tcp/localhost/%d' 2>&1`,
				port,
			)
			_, err := dockerExec(container, shellCmd)

			if err == nil {
				results = append(results, ProofResult{
					Name:   name,
					Status: StatusOK,
					Detail: fmt.Sprintf("Port %d reachable from inside container", port),
				})
			} else {
				results = append(results, ProofResult{
					Name:   name,
					Status: StatusFAIL,
					Detail: fmt.Sprintf("Port %d NOT reachable from inside container", port),
				})
			}
		}
	}
	return results
}

// checkProgrammingToolVersions verifies each enabled programming tool is
// installed in the container and reports its version.
func checkProgrammingToolVersions(container string, tools []string) []ProofResult {
	if len(tools) == 0 {
		return []ProofResult{{
			Name:   "Programming Tools",
			Status: StatusINFO,
			Detail: "No programming tools enabled",
		}}
	}

	// Map tool names to their version-check commands.
	versionCmds := map[string]string{
		"go":     "go version",
		"node":   "node --version",
		"python": "python3 --version 2>/dev/null || python --version 2>/dev/null || echo notfound",
	}

	var results []ProofResult
	for _, tool := range tools {
		cmd, ok := versionCmds[tool]
		if !ok {
			cmd = fmt.Sprintf("%s --version 2>/dev/null || echo notfound", tool)
		}

		out, err := dockerExec(container, cmd)
		name := fmt.Sprintf("Prog Tool: %s", tool)

		if err == nil && !strings.Contains(out, "notfound") && out != "" {
			results = append(results, ProofResult{
				Name:   name,
				Status: StatusOK,
				Detail: fmt.Sprintf("Installed (%s)", truncate(out, 80)),
			})
		} else {
			results = append(results, ProofResult{
				Name:   name,
				Status: StatusFAIL,
				Detail: fmt.Sprintf("%s not found in container", tool),
			})
		}
	}
	return results
}

// checkToolInstallations verifies each enabled AI tool is installed and
// responds to --version.
func checkToolInstallations(container string, tools []string) []ProofResult {
	if len(tools) == 0 {
		return []ProofResult{{
			Name:   "Tool Installations",
			Status: StatusINFO,
			Detail: "No AI tools enabled",
		}}
	}

	// Map tool names to their version-check commands.
	versionCmds := map[string]string{
		"claude":  "claude --version",
		"copilot": "github-copilot-cli --version 2>/dev/null || copilot --version 2>/dev/null || echo notfound",
		"codex":   "codex --version 2>/dev/null || echo notfound",
		"opencode": "opencode --version 2>/dev/null || echo notfound",
	}

	var results []ProofResult
	for _, tool := range tools {
		cmd, ok := versionCmds[tool]
		if !ok {
			// Unknown tool -- just try "<tool> --version".
			cmd = fmt.Sprintf("%s --version 2>/dev/null || echo notfound", tool)
		}

		out, err := dockerExec(container, cmd)
		name := fmt.Sprintf("Tool: %s", tool)

		if err == nil && !strings.Contains(out, "notfound") && out != "" {
			results = append(results, ProofResult{
				Name:   name,
				Status: StatusOK,
				Detail: fmt.Sprintf("Installed (%s)", truncate(out, 80)),
			})
		} else {
			results = append(results, ProofResult{
				Name:   name,
				Status: StatusFAIL,
				Detail: fmt.Sprintf("%s not found in container", tool),
			})
		}
	}
	return results
}

// checkAuth verifies that authentication credentials are present inside the
// container for the various AI tools. Returns FAIL if no credentials are
// found at all, OK if at least one credential source is configured.
func checkAuth(container string) ProofResult {
	var findings []string
	anyFound := false

	// OPENAI_API_KEY
	out, _ := dockerExec(container, `bash -c 'test -n "$OPENAI_API_KEY" && echo set || echo unset'`)
	if strings.Contains(out, "set") {
		findings = append(findings, "OPENAI_API_KEY: set")
		anyFound = true
	} else {
		findings = append(findings, "OPENAI_API_KEY: not set")
	}

	// Copilot PAT (~/.copilot/.gh_token or GH_TOKEN/GITHUB_TOKEN env)
	out, _ = dockerExec(container, `bash -c '
		if [ -n "$GH_TOKEN" ] || [ -n "$GITHUB_TOKEN" ]; then
			echo "env"
		elif [ -f ~/.copilot/.gh_token ]; then
			echo "file"
		else
			echo "missing"
		fi
	'`)
	switch {
	case strings.Contains(out, "env"):
		findings = append(findings, "Copilot PAT: set via env")
		anyFound = true
	case strings.Contains(out, "file"):
		findings = append(findings, "Copilot PAT: ~/.copilot/.gh_token")
		anyFound = true
	default:
		findings = append(findings, "Copilot PAT: not configured")
	}

	// Claude credentials (~/.claude directory)
	out, _ = dockerExec(container, `test -d ~/.claude && echo exists || echo missing`)
	if strings.Contains(out, "exists") {
		findings = append(findings, "~/.claude: present")
		anyFound = true
	} else {
		findings = append(findings, "~/.claude: missing")
	}

	status := StatusFAIL
	if anyFound {
		status = StatusOK
	}
	return ProofResult{
		Name:   "Auth Credentials",
		Status: status,
		Detail: strings.Join(findings, "; "),
	}
}

// checkBridgeReachability curls the execution bridge health endpoint from
// inside the container.
func checkBridgeReachability(container, bridgePort string) ProofResult {
	shellCmd := fmt.Sprintf(
		`curl -so /dev/null -w '%%{http_code}' --connect-timeout 5 http://localhost:%s/health 2>&1`,
		bridgePort,
	)
	out, err := dockerExec(container, shellCmd)

	if err == nil && out != "" && out != "000" {
		return ProofResult{
			Name:   "Bridge Reachability",
			Status: StatusOK,
			Detail: fmt.Sprintf("Bridge health endpoint reachable on port %s (HTTP %s)", bridgePort, out),
		}
	}
	return ProofResult{
		Name:   "Bridge Reachability",
		Status: StatusFAIL,
		Detail: fmt.Sprintf("Bridge health endpoint NOT reachable on port %s", bridgePort),
	}
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

// FormatResults renders the proof results as a human-readable string with
// colored OK/FAIL/INFO prefixes using ANSI escape codes.
func FormatResults(results []ProofResult) string {
	if len(results) == 0 {
		return "No diagnostic checks were run."
	}

	// ANSI color codes for terminal output.
	const (
		green  = "\033[32m"
		red    = "\033[31m"
		yellow = "\033[33m"
		bold   = "\033[1m"
		reset  = "\033[0m"
	)

	var b strings.Builder
	b.WriteString(bold + "=== Cooper Proof ===" + reset + "\n\n")

	okCount, failCount, infoCount := 0, 0, 0

	for _, r := range results {
		var prefix string
		switch r.Status {
		case StatusOK:
			prefix = green + bold + "  OK" + reset
			okCount++
		case StatusFAIL:
			prefix = red + bold + "FAIL" + reset
			failCount++
		case StatusINFO:
			prefix = yellow + bold + "INFO" + reset
			infoCount++
		default:
			prefix = "  ??"
		}
		b.WriteString(fmt.Sprintf("  %s  %-30s %s\n", prefix, r.Name, r.Detail))
	}

	b.WriteString("\n")
	summary := fmt.Sprintf("  %s%d passed%s", green, okCount, reset)
	if failCount > 0 {
		summary += fmt.Sprintf("  %s%d failed%s", red, failCount, reset)
	}
	if infoCount > 0 {
		summary += fmt.Sprintf("  %s%d info%s", yellow, infoCount, reset)
	}
	b.WriteString(summary + "\n")

	return b.String()
}

// truncate shortens s to at most maxLen characters, appending "..." if cut.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
