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
	name := "barrel-" + base + "-" + toolName

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
// cooperDir is the path to ~/.cooper.
func StartBarrel(cfg *config.Config, workspaceDir, cooperDir, toolName string) error {
	name := BarrelContainerName(workspaceDir, toolName)
	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("resolve workspace path: %w", err)
	}

	// Create host directories that may not exist yet.
	if err := ensureBarrelHostDirs(absWorkspace, toolName); err != nil {
		return fmt.Errorf("create host directories: %w", err)
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
		"--network", NetworkInternal,

		// Security hardening.
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
		"--security-opt", fmt.Sprintf("seccomp=%s", seccompPath),
		"--init",

		// Label for workspace path tracking (used by collision detection).
		"--label", fmt.Sprintf("cooper.workspace=%s", absWorkspace),
	}

	// Volume mounts.
	args = appendVolumeMounts(args, absWorkspace, homeDir, cfg, cooperDir, toolName)

	// Proxy environment variables -- all traffic goes through cooper-proxy.
	args = append(args,
		"-e", fmt.Sprintf("HTTP_PROXY=http://cooper-proxy:%d", cfg.ProxyPort),
		"-e", fmt.Sprintf("HTTPS_PROXY=http://cooper-proxy:%d", cfg.ProxyPort),
		"-e", "NO_PROXY=localhost,127.0.0.1",
	)

	// If Go is enabled, set GOFLAGS=-mod=readonly to prevent the AI from
	// modifying go.mod/go.sum inside the container. Dependencies must be
	// installed on the host.
	if isGoEnabled(cfg) {
		args = append(args, "-e", "GOFLAGS=-mod=readonly")
	}

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

// containerHome is the home directory of the unprivileged user inside barrel
// containers. The barrel Dockerfile creates this user as "user" with home
// /home/user, so all auth/config/cache mounts must target paths under this
// directory rather than the host user's home.
const containerHome = "/home/user"

// appendVolumeMounts adds all volume mount flags to the docker run args.
// cooperDir is used to locate the socat-rules.json config file.
// toolName scopes which AI tool auth directories are mounted.
func appendVolumeMounts(args []string, absWorkspace, homeDir string, cfg *config.Config, cooperDir, toolName string) []string {
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
			args = append(args, "-v", fmt.Sprintf("%s:%s:rw", claudeJSON, filepath.Join(containerHome, ".claude.json")))
		}
	case "copilot":
		mountRW(homeDir, ".copilot", &args)
	case "codex":
		mountRW(homeDir, ".codex", &args)
	case "opencode":
		mountRW(homeDir, filepath.Join(".config", "opencode"), &args)
		mountRW(homeDir, filepath.Join(".local", "share", "opencode"), &args)
	}

	// Git config (read-only).
	gitconfig := filepath.Join(homeDir, ".gitconfig")
	if fileExists(gitconfig) {
		args = append(args, "-v", fmt.Sprintf("%s:%s:ro", gitconfig, filepath.Join(containerHome, ".gitconfig")))
	}

	// Language-specific caches (based on enabled programming tools).
	args = appendLanguageCacheMounts(args, homeDir, cfg)

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

	return args
}

// mountRW appends a read-write volume mount for a directory relative to home.
func mountRW(homeDir, relPath string, args *[]string) {
	hostPath := filepath.Join(homeDir, relPath)
	containerPath := filepath.Join(containerHome, relPath)
	*args = append(*args, "-v", fmt.Sprintf("%s:%s:rw", hostPath, containerPath))
}

// appendLanguageCacheMounts adds cache volume mounts based on which
// programming tools are enabled in the configuration. Container-side
// paths use containerHome so the barrel user can find caches.
func appendLanguageCacheMounts(args []string, homeDir string, cfg *config.Config) []string {
	for _, tool := range cfg.ProgrammingTools {
		if !tool.Enabled {
			continue
		}
		switch tool.Name {
		case "go":
			gopath := os.Getenv("GOPATH")
			if gopath == "" {
				gopath = filepath.Join(homeDir, "go")
			}
			hostModCache := filepath.Join(gopath, "pkg", "mod")
			hostBuildCache := filepath.Join(homeDir, ".cache", "go-build")
			containerModCache := filepath.Join(containerHome, "go", "pkg", "mod")
			containerBuildCache := filepath.Join(containerHome, ".cache", "go-build")
			args = append(args,
				"-v", fmt.Sprintf("%s:%s:ro", hostModCache, containerModCache),
				"-v", fmt.Sprintf("%s:%s:rw", hostBuildCache, containerBuildCache),
			)
		case "node":
			hostNpm := filepath.Join(homeDir, ".npm")
			containerNpm := filepath.Join(containerHome, ".npm")
			args = append(args, "-v", fmt.Sprintf("%s:%s:ro", hostNpm, containerNpm))
		case "python":
			hostPip := filepath.Join(homeDir, ".cache", "pip")
			containerPip := filepath.Join(containerHome, ".cache", "pip")
			args = append(args, "-v", fmt.Sprintf("%s:%s:ro", hostPip, containerPip))
		}
	}
	return args
}

// ensureBarrelHostDirs creates directories on the host that must exist
// before Docker can mount them as volumes.
func ensureBarrelHostDirs(absWorkspace, toolName string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(homeDir, "go")
	}

	// Tool-specific auth dirs.
	var dirs []string
	switch toolName {
	case "claude":
		dirs = append(dirs, filepath.Join(homeDir, ".claude"))
	case "copilot":
		dirs = append(dirs, filepath.Join(homeDir, ".copilot"))
	case "codex":
		dirs = append(dirs, filepath.Join(homeDir, ".codex"))
	case "opencode":
		dirs = append(dirs,
			filepath.Join(homeDir, ".config", "opencode"),
			filepath.Join(homeDir, ".local", "share", "opencode"),
		)
	}

	// Language cache dirs (always needed).
	dirs = append(dirs,
		filepath.Join(homeDir, ".npm"),
		filepath.Join(homeDir, ".cache", "pip"),
		filepath.Join(homeDir, ".cache", "go-build"),
		filepath.Join(gopath, "pkg", "mod"),
	)

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	return nil
}

// StopBarrel stops and removes a barrel container by name.
func StopBarrel(name string) error {
	cmd := exec.Command("docker", "stop", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "No such container") &&
			!strings.Contains(string(output), "is not running") {
			return fmt.Errorf("docker stop %s failed: %w\n%s", name, err, string(output))
		}
	}

	cmd = exec.Command("docker", "rm", "-f", name)
	output, err = cmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "No such container") {
			return fmt.Errorf("docker rm %s failed: %w\n%s", name, err, string(output))
		}
	}

	return nil
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
		"--filter", "name=barrel-",
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
	for _, line := range strings.Split(result, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		status := strings.TrimSpace(parts[1])

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

// isGoEnabled checks if Go is enabled in the programming tools config.
func isGoEnabled(cfg *config.Config) bool {
	for _, t := range cfg.ProgrammingTools {
		if strings.EqualFold(t.Name, "go") && t.Enabled {
			return true
		}
	}
	return false
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
