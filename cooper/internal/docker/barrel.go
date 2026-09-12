package docker

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/runtimefs"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// BarrelInfo holds status information about a running barrel container.
type BarrelInfo struct {
	Name         string
	Status       string
	WorkspaceDir string
	ToolName     string
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
	return containerLabel(name, "cooper.workspace")
}

func containerLabel(name, label string) string {
	cmd := exec.Command("docker", "inspect",
		"--format", fmt.Sprintf("{{index .Config.Labels %q}}", label),
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
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}
	return StartBarrelWithHomeDir(cfg, workspaceDir, cooperDir, homeDir, toolName)
}

// StartBarrelWithHomeDir starts a barrel with an explicit host home. Runtime
// tests use a temporary home that is visible to both the Docker client and its
// daemon. Production callers use StartBarrel and the real user home.
func StartBarrelWithHomeDir(cfg *config.Config, workspaceDir, cooperDir, homeDir, toolName string) error {
	if !filepath.IsAbs(homeDir) {
		return errors.New("barrel host home directory must be absolute")
	}
	name := BarrelContainerName(workspaceDir, toolName)
	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("resolve workspace path: %w", err)
	}
	mountInput := barrelMountInput(absWorkspace, homeDir, cfg, cooperDir, toolName, name)
	// Create only the known host state, cache, and runtime directories before
	// the pure plan detects optional directory mounts such as live config.
	if err := workload.EnsureDirectories(mountInput); err != nil {
		return fmt.Errorf("create mount directories: %w", err)
	}
	// Write the startup timezone snapshot before the mount plan checks which
	// optional files exist. This order makes the host timezone snapshot part of
	// every new barrel instead of only barrels that reuse an old snapshot.
	if _, err := runtimefs.SyncTimezoneFile(cooperDir, name); err != nil {
		return fmt.Errorf("sync barrel timezone: %w", err)
	}
	mounts, err := workload.BuildMountPlan(mountInput)
	if err != nil {
		return fmt.Errorf("build mount plan: %w", err)
	}
	clipboardMode, err := ToolClipboardMode(toolName)
	if err != nil {
		return err
	}

	// Ensure seccomp profile is written to disk.
	seccompPath, err := EnsureSeccompProfile(cooperDir)
	if err != nil {
		return fmt.Errorf("ensure seccomp profile: %w", err)
	}

	// Remove existing container with the same name.
	_ = exec.Command("docker", "rm", "-f", name).Run()
	_ = os.Remove(filepath.Join(cooperDir, "tmp", name, filepath.Base(workload.EntrypointReadyPath)))

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
		"--label", "cooper.kind=cli",
		"--label", "cooper.runtime-id=" + name,
		"--label", "cooper.tool=" + toolName,
		"--label", "cooper.clipboard-mode=" + clipboardMode,
	}

	// Volume mounts.
	args = appendDockerMounts(args, mounts)

	// Render the shared non-secret environment with this back end's proxy.
	for _, environment := range workload.RenderEnvironment(workload.RuntimeEnvironment(cfg, ProxyHost(), InternalNetworkName())) {
		args = append(args, "-e", environment)
	}
	args = append(args, "-e", "COOPER_CLIPBOARD_MODE="+clipboardMode)

	// Working directory inside the container matches host workspace.
	args = append(args, "-w", absWorkspace)

	// Image and command — use tool-specific image.
	args = append(args, GetImageCLI(toolName), "sleep", "infinity")

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run %s failed: %w\n%s", name, err, string(output))
	}
	if err := WaitBarrelReady(name, 60*time.Second); err != nil {
		logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
		_ = StopBarrel(name)
		return fmt.Errorf("start barrel %s: %w\n%s", name, err, strings.TrimSpace(string(logs)))
	}
	return nil
}

// ToolClipboardMode returns the effective clipboard mode of an agent image.
// Built-in tools use the reviewed catalog value. A custom image can select a
// stricter mode through COOPER_CLIPBOARD_MODE in its image environment.
func ToolClipboardMode(toolName string) (string, error) {
	if _, ok := aitool.Lookup(toolName); ok {
		return aitool.ClipboardMode(toolName), nil
	}

	output, err := exec.Command("docker", "image", "inspect", "--format", "{{json .Config.Env}}", GetImageCLI(toolName)).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect clipboard mode for custom tool %s: %w: %s", toolName, err, strings.TrimSpace(string(output)))
	}
	var environment []string
	if err := json.Unmarshal(output, &environment); err != nil {
		return "", fmt.Errorf("parse clipboard mode for custom tool %s: %w", toolName, err)
	}
	mode, err := clipboardModeFromEnvironment(environment)
	if err != nil {
		return "", fmt.Errorf("custom tool %s: %w", toolName, err)
	}
	return mode, nil
}

func clipboardModeFromEnvironment(environment []string) (string, error) {
	mode := aitool.ClipboardAuto
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if ok && name == "COOPER_CLIPBOARD_MODE" {
			mode = strings.ToLower(strings.TrimSpace(value))
		}
	}
	switch mode {
	case "off", aitool.ClipboardShim, aitool.ClipboardX11, aitool.ClipboardAuto:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid COOPER_CLIPBOARD_MODE %q", mode)
	}
}

func appendDockerMounts(args []string, mounts []workload.MountSpec) []string {
	for _, mount := range mounts {
		value := fmt.Sprintf("type=bind,src=%s,dst=%s", mount.Source, mount.Target)
		if mount.Access == workload.ReadOnly {
			value += ",readonly"
		}
		args = append(args, "--mount", value)
	}
	return args
}

func barrelMountInput(absWorkspace, homeDir string, cfg *config.Config, cooperDir, toolName, containerName string) workload.MountInput {
	return workload.MountInput{
		WorkspaceDir:  absWorkspace,
		HomeDir:       homeDir,
		CooperDir:     cooperDir,
		RuntimeID:     containerName,
		ToolName:      toolName,
		GrokStateRoot: GrokHostStateRoot(homeDir),
		Config:        cfg,
	}
}

// StopBarrel stops and removes a barrel container by name.
func StopBarrel(name string) error {
	return stopAndRemoveContainer(name)
}

// RestartBarrel restarts a barrel container by name. This is a simple
// docker restart which preserves the container (unlike StopBarrel which
// also removes it).
func RestartBarrel(name string) error {
	_ = exec.Command("docker", "exec", name, "rm", "-f", workload.EntrypointReadyPath).Run()
	cmd := exec.Command("docker", "restart", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "No such container") {
			return nil
		}
		return fmt.Errorf("docker restart %s failed: %w\n%s", name, err, string(output))
	}
	return WaitBarrelReady(name, 60*time.Second)
}

// WaitBarrelReady waits until the shared entrypoint has installed all runtime
// support. Docker's running state alone is too early for an exec session.
func WaitBarrelReady(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := exec.Command("docker", "exec", name, "test", "-f", workload.EntrypointReadyPath).Run(); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("barrel %s did not become ready in %s", name, timeout)
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
			ToolName:     containerLabel(name, "cooper.tool"),
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

// BarrelHasSessionMount reports whether the running barrel includes the
// read-only session mount introduced for host-controlled runtime files.
func BarrelHasSessionMount(name string) (bool, error) {
	cmd := exec.Command("docker", "inspect",
		"--format", "{{range .Mounts}}{{println .Destination}}{{end}}",
		name,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("inspect barrel mounts for %s: %w\n%s", name, err, string(output))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) == workload.SessionContainerDir {
			return true, nil
		}
	}
	return false, nil
}

// BarrelHasGrokStateMount reports whether a running barrel has the expected
// complete Grok state root as one read-write mount. A legacy barrel or a
// barrel created with a different GROK_HOME must be recreated.
func BarrelHasGrokStateMount(name, expectedHostRoot string) (bool, error) {
	cmd := exec.Command("docker", "inspect",
		"--format", `{{range .Mounts}}{{printf "%s\t%s\t%t\n" .Source .Destination .RW}}{{end}}`,
		name,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("inspect barrel mounts for %s: %w\n%s", name, err, string(output))
	}
	return hasGrokStateMount(string(output), expectedHostRoot), nil
}

func hasGrokStateMount(inspectOutput, expectedHostRoot string) bool {
	stateMounts := 0
	expectedMount := false
	for _, line := range strings.Split(strings.TrimSpace(inspectOutput), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			continue
		}
		destination := filepath.Clean(fields[1])
		if destination != BarrelGrokStateRoot && !strings.HasPrefix(destination, BarrelGrokStateRoot+string(filepath.Separator)) {
			continue
		}
		stateMounts++
		if destination == BarrelGrokStateRoot && fields[2] == "true" && sameHostPath(fields[0], expectedHostRoot) {
			expectedMount = true
		}
	}
	return stateMounts == 1 && expectedMount
}

func sameHostPath(left, right string) bool {
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	leftResolved, leftErr := filepath.EvalSymlinks(left)
	rightResolved, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftResolved) == filepath.Clean(rightResolved)
}
