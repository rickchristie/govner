package docker

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/config"
)

// BarrelInfo holds status information about a running barrel container.
type BarrelInfo struct {
	Name         string
	Status       string
	WorkspaceDir string
}

// BarrelContainerName returns the container name for a barrel based on the
// workspace directory and tool name. The format is "barrel-{dirname}-{tool}".
// If a container with that name already exists for a different workspace path,
// a short hash of the absolute path is appended (e.g., "barrel-myproject-claude-a3f1").
func BarrelContainerName(workspaceDir, toolName string) string {
	base := filepath.Base(workspaceDir)
	name := BarrelNamePrefix() + base + "-" + toolName

	// Check if a container with this name already exists.
	absPath, _ := filepath.Abs(workspaceDir)
	existing := containerWorkspacePath(name)

	if existing == "" || existing == absPath {
		// No collision: either no existing container or same workspace.
		return name
	}

	// Collision detected: append short hash of absolute path.
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(absPath)))
	return name + "-" + hash[:4]
}

// containerWorkspacePath returns the workspace path label of an existing
// container, or empty string if the container does not exist.
func containerWorkspacePath(name string) string {
	cmd := exec.Command("docker", "inspect",
		"--format", "{{index .Config.Labels \"cooper.workspace\"}}",
		name,
	)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// StartBarrel creates and starts a barrel container for the given workspace and tool.
//
// The barrel runs on cooper-internal only (no internet access), with all
// traffic forced through the proxy. Security hardening includes dropping
// all capabilities, preventing privilege escalation, custom seccomp profile,
// and PID 1 init process.
//
// Multiple barrels for different tools can share the same workspace directory
// simultaneously. File ownership is consistent because all tool images inherit
// the same UID/GID from the base image.
//
// cooperDir is the path to ~/.cooper.
func StartBarrel(cfg *config.Config, workspaceDir, cooperDir, toolName string) error {
	name := BarrelContainerName(workspaceDir, toolName)
	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("resolve workspace path: %w", err)
	}

	// Create host directories that may not exist yet.
	if err := ensureBarrelMountDirs(toolName, cooperDir, name, cfg); err != nil {
		return fmt.Errorf("create mount directories: %w", err)
	}

	// Ensure seccomp profile is written to disk.
	seccompPath, err := EnsureSeccompProfile(cooperDir)
	if err != nil {
		return fmt.Errorf("ensure seccomp profile: %w", err)
	}

	// Remove existing container with the same name.
	_ = exec.Command("docker", "rm", "-f", name).Run()
	homeDir, _ := os.UserHomeDir()

	args := []string{
		"run", "-d",
		"--name", name,
		"--network", InternalNetworkName(),

		// Security hardening.
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
		"--security-opt", fmt.Sprintf("seccomp=%s", seccompPath),
		"--init",

		// Shared memory size for browser/Playwright workloads.
		"--shm-size", cfg.BarrelSHMSize,

		// Label for workspace path tracking (used by collision detection).
		"--label", fmt.Sprintf("cooper.workspace=%s", absWorkspace),
	}

	// Volume mounts.
	args = appendVolumeMounts(args, absWorkspace, homeDir, cfg, cooperDir, toolName, name)

	// Proxy environment variables -- all traffic goes through cooper-proxy.
	args = append(args,
		"-e", fmt.Sprintf("HTTP_PROXY=http://%s:%d", ProxyHost(), cfg.ProxyPort),
		"-e", fmt.Sprintf("HTTPS_PROXY=http://%s:%d", ProxyHost(), cfg.ProxyPort),
		"-e", "NO_PROXY=localhost,127.0.0.1",
		"-e", fmt.Sprintf("COOPER_PROXY_HOST=%s", ProxyHost()),
		"-e", fmt.Sprintf("COOPER_INTERNAL_NETWORK=%s", InternalNetworkName()),
	)

	// X11 display env vars — set for ALL barrels so Playwright and clipboard
	// bridge both have a consistent display. The entrypoint starts a shared
	// Xvfb instance that these point to.
	args = append(args,
		"-e", "DISPLAY=127.0.0.1:99",
		"-e", "XAUTHORITY=/home/user/.cooper-clipboard.xauth",
		"-e", "COOPER_CLIPBOARD_DISPLAY=127.0.0.1:99",
		"-e", "COOPER_CLIPBOARD_XAUTHORITY=/home/user/.cooper-clipboard.xauth",
	)

	// Playwright browser cache path.
	args = append(args,
		"-e", "PLAYWRIGHT_BROWSERS_PATH=/home/user/.cache/ms-playwright",
	)

	// Clipboard bridge env vars.
	args = append(args,
		"-e", "COOPER_CLIPBOARD_ENABLED=1",
		"-e", fmt.Sprintf("COOPER_CLIPBOARD_BRIDGE_URL=http://127.0.0.1:%d", cfg.BridgePort),
		"-e", "COOPER_CLIPBOARD_TOKEN_FILE=/etc/cooper/clipboard-token",
		"-e", "COOPER_CLIPBOARD_SHIMS=xclip,xsel",
	)

	// Working directory inside the container matches host workspace.
	args = append(args, "-w", absWorkspace)

	// Image and command — use tool-specific image.
	args = append(args, GetImageCLI(toolName), "sleep", "infinity")

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run %s failed: %w\n%s", name, err, string(output))
	}

	return nil
}

// appendVolumeMounts adds all volume mount flags to the docker run args.
// cooperDir provides Cooper-managed mounts such as language caches, CA,
// socat rules, clipboard shims/tokens, Playwright support dirs, and the
// per-barrel host-backed /tmp directory.
// toolName scopes which AI tool auth directories are mounted.
// containerName identifies per-barrel mounts such as the clipboard token
// file and ~/.cooper/tmp/{containerName}.
func appendVolumeMounts(args []string, absWorkspace, homeDir string, cfg *config.Config, cooperDir, toolName, containerName string) []string {
	// Workspace directory (read-write) -- symmetrical mount so IDE
	// integration (e.g. VS Code) can resolve paths correctly.
	args = append(args, "-v", fmt.Sprintf("%s:%s:rw", absWorkspace, absWorkspace))

	// .git/hooks overlay (read-only) to prevent hook injection.
	// Symmetrical mount so git inside the container finds hooks at the
	// expected path relative to the workspace.
	gitHooksDir := filepath.Join(absWorkspace, ".git", "hooks")
	if dirExists(gitHooksDir) {
		args = append(args, "-v", fmt.Sprintf("%s:%s:ro", gitHooksDir, gitHooksDir))
	}

	// Per-tool auth/config directories (read-write).
	// Only mount auth dirs for the specific tool.
	switch toolName {
	case "claude":
		mountRW(homeDir, ".claude", &args)
		claudeJSON := filepath.Join(homeDir, ".claude.json")
		if fileExists(claudeJSON) {
			args = append(args, "-v", fmt.Sprintf("%s:%s:rw", claudeJSON, filepath.Join(BarrelHomeDir, ".claude.json")))
		}
	case "copilot":
		mountRW(homeDir, ".copilot", &args)
	case "codex":
		mountRW(homeDir, ".codex", &args)
	case "opencode":
		mountRW(homeDir, filepath.Join(".cache", "opencode"), &args)
		mountRW(homeDir, filepath.Join(".config", "opencode"), &args)
		mountRW(homeDir, filepath.Join(".local", "share", "opencode"), &args)
		mountRW(homeDir, filepath.Join(".local", "state", "opencode"), &args)
		mountRW(homeDir, ".opencode", &args)
	}

	// Git config (read-only).
	gitconfig := filepath.Join(homeDir, ".gitconfig")
	if fileExists(gitconfig) {
		args = append(args, "-v", fmt.Sprintf("%s:%s:ro", gitconfig, filepath.Join(BarrelHomeDir, ".gitconfig")))
	}

	// Language-specific caches (Cooper-managed, under cooperDir/cache/).
	args = appendLanguageCacheMounts(args, cooperDir, cfg)

	// CA certificate for SSL bump trust. Volume-mounted so the barrel always
	// uses the same CA as the running proxy, even if the CA was regenerated
	// after the barrel image was built.
	caCert := filepath.Join(cooperDir, "ca", "cooper-ca.pem")
	if fileExists(caCert) {
		args = append(args, "-v", fmt.Sprintf("%s:/etc/cooper/cooper-ca.pem:ro", caCert))
	}

	// Socat port forwarding rules (live-reloadable via SIGHUP).
	socatRules := filepath.Join(cooperDir, socatRulesFile)
	if fileExists(socatRules) {
		args = append(args, "-v", fmt.Sprintf("%s:/etc/cooper/socat-rules.json:ro", socatRules))
	}

	// Mount clipboard token file if it exists.
	tokenFile := filepath.Join(cooperDir, "tokens", containerName)
	if fileExists(tokenFile) {
		args = append(args, "-v", tokenFile+":/etc/cooper/clipboard-token:ro")
	}

	// Mount clipboard shim scripts.
	shimsDir := filepath.Join(cooperDir, "base", "shims")
	if dirExists(shimsDir) {
		args = append(args, "-v", shimsDir+":/etc/cooper/shims:ro")
	}

	// Playwright support mounts: Cooper-managed fonts (read-only) and
	// Playwright browser cache (read-write).
	fontsDir := filepath.Join(cooperDir, "fonts")
	args = append(args, "-v", fontsDir+":"+BarrelFontsDir+":ro")

	pwCacheDir := filepath.Join(cooperDir, "cache", "ms-playwright")
	args = append(args, "-v", pwCacheDir+":"+BarrelPlaywrightCacheDir+":rw")

	// Per-barrel /tmp directory. Each barrel gets its own host-backed /tmp
	// under ~/.cooper/tmp/{containerName}/ to avoid collisions between
	// barrels. Cooper clears the shared tmp root when cooper up starts and
	// when it shuts down, so each control-plane session begins pristine.
	barrelTmpDir := filepath.Join(cooperDir, "tmp", containerName)
	args = append(args, "-v", barrelTmpDir+":/tmp:rw")

	return args
}

// mountRW appends a read-write volume mount for a directory relative to home.
func mountRW(homeDir, relPath string, args *[]string) {
	hostPath := filepath.Join(homeDir, relPath)
	containerPath := filepath.Join(BarrelHomeDir, relPath)
	*args = append(*args, "-v", fmt.Sprintf("%s:%s:rw", hostPath, containerPath))
}

// appendLanguageCacheMounts adds Cooper-managed cache volume mounts based
// on which programming tools are enabled. All caches live under
// cooperDir/cache/ and are mounted read-write — no host caches are used.
func appendLanguageCacheMounts(args []string, cooperDir string, cfg *config.Config) []string {
	for _, spec := range languageCacheSpecs(cooperDir, cfg) {
		args = append(args, "-v", fmt.Sprintf("%s:%s:rw", spec.HostPath, spec.ContainerPath))
	}
	return args
}

// ensureBarrelMountDirs creates directories on the host that must exist
// before Docker can bind-mount them into a barrel. The directory list
// comes from barrelMountDirs (pure helper); this function is the thin
// I/O wrapper that calls os.MkdirAll.
func ensureBarrelMountDirs(toolName, cooperDir, containerName string, cfg *config.Config) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	for _, dir := range barrelMountDirs(homeDir, toolName, cooperDir, containerName, cfg) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	return nil
}

// StopBarrel stops and removes a barrel container by name.
func StopBarrel(name string) error {
	return stopAndRemoveContainer(name)
}

// RestartBarrel restarts a barrel container by name. This is a simple
// docker restart which preserves the container (unlike StopBarrel which
// also removes it).
func RestartBarrel(name string) error {
	cmd := exec.Command("docker", "restart", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "No such container") {
			return fmt.Errorf("docker restart %s failed: %w\n%s", name, err, string(output))
		}
	}
	return nil
}

// ExecBarrel executes a command inside a running barrel container.
// When interactive is true, stdin/stdout/stderr are attached for
// terminal passthrough (e.g., launching an interactive shell).
// envArgs are passed as additional -e flags to docker exec.
func ExecBarrel(containerName string, cmd []string, envArgs []string, interactive bool) error {
	args := []string{"exec"}

	if interactive {
		args = append(args, "-it")
	}

	for _, env := range envArgs {
		args = append(args, "-e", env)
	}

	args = append(args, containerName)
	args = append(args, cmd...)

	c := exec.Command("docker", args...)
	// Always wire stdout/stderr so command output is visible.
	// Only wire stdin for interactive sessions (shells).
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if interactive {
		c.Stdin = os.Stdin
	}

	err := c.Run()
	if err != nil && !interactive {
		return fmt.Errorf("docker exec %s failed: %w", containerName, err)
	}
	// For interactive sessions, don't treat shell exit codes as errors.
	// The exit code is just the status of the last command the user ran
	// (or from profile scripts like .bash_logout).
	return nil
}

// ListBarrels returns information about all running barrel containers.
// Barrel containers are identified by the "barrel-" name prefix.
func ListBarrels() ([]BarrelInfo, error) {
	cmd := exec.Command("docker", "ps",
		"--format", "{{.Names}}\t{{.Status}}",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w\n%s", err, string(output))
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return nil, nil
	}

	var barrels []BarrelInfo
	prefix := BarrelNamePrefix()
	for _, line := range strings.Split(result, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		status := strings.TrimSpace(parts[1])
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		// Look up workspace path from container label.
		workspace := containerWorkspacePath(name)

		barrels = append(barrels, BarrelInfo{
			Name:         name,
			Status:       status,
			WorkspaceDir: workspace,
		})
	}
	return barrels, nil
}

// IsBarrelRunning checks whether a barrel container with the given name
// is currently running.
func IsBarrelRunning(name string) (bool, error) {
	cmd := exec.Command("docker", "inspect",
		"--format", "{{.State.Running}}",
		name,
	)
	output, err := cmd.Output()
	if err != nil {
		// Container doesn't exist.
		return false, nil
	}
	return strings.TrimSpace(string(output)) == "true", nil
}

// clipboardModeForTool returns the clipboard mode for a given tool.
// Built-in tools have known modes; custom tools default to "auto".
//
// Image paste support requires two different strategies depending on how
// the AI CLI reads the clipboard:
//
//   - "shim": The CLI shells out to helper binaries (xclip, xsel, wl-paste)
//     to read clipboard data. Cooper installs wrapper scripts earlier in PATH
//     that intercept image-read calls and serve the staged image from the
//     bridge. Claude and OpenCode both work this way — their binaries contain
//     explicit references to these helper tools.
//
//   - "x11": The CLI reads the clipboard in-process via native X11 APIs.
//     A helper-binary shim cannot intercept this. Instead, Cooper starts
//     Xvfb and runs cooper-x11-bridge as the X11 CLIPBOARD selection owner.
//     Codex uses arboard (Rust, in-process X11); Copilot uses
//     @teddyzhu/clipboard (native Node module) — both verified by runtime
//     inspection and live Xvfb experiments.
//
// Custom cooper-cli-* barrels default to "auto" (both shim and X11 plumbing
// installed) so they work without the user having to manually classify the
// CLI's clipboard strategy. Barrels can opt out with COOPER_CLIPBOARD_MODE=off.
func clipboardModeForTool(toolName string) string {
	switch toolName {
	case "claude", "opencode":
		return "shim"
	case "codex", "copilot":
		return "x11"
	default:
		return "auto"
	}
}

// dirExists returns true if the path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// fileExists returns true if the path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
