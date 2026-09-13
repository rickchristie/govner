package docker

import (
	"context"
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
	"github.com/rickchristie/govner/cooper/internal/profilemanager"
	"github.com/rickchristie/govner/cooper/internal/runtimefs"
	"github.com/rickchristie/govner/cooper/internal/statelock"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// BarrelInfo holds status information about a running barrel container.
type BarrelInfo struct {
	Name         string
	Status       string
	WorkspaceDir string
	ToolName     string
	ProfileID    string
	ProfileName  string
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

func BarrelContainerNameForProfile(workspaceDir, toolName, profileID string) string {
	if profileID == "" {
		return BarrelContainerName(workspaceDir, toolName)
	}
	return BarrelContainerName(workspaceDir, toolName+"-p-"+profileID)
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
	return StartBarrelWithProfile(cfg, workspaceDir, cooperDir, homeDir, toolName, "")
}

func StartBarrelWithProfile(cfg *config.Config, workspaceDir, cooperDir, homeDir, toolName, profileID string) error {
	lock, err := statelock.Acquire(context.Background(), false)
	if err != nil {
		return err
	}
	defer lock.Close()
	if !filepath.IsAbs(homeDir) {
		return errors.New("barrel host home directory must be absolute")
	}
	if err := ValidateImageAccount(GetImageCLI(toolName), homeDir); err != nil {
		return err
	}
	selection, err := profilemanager.SelectID(context.Background(), cooperDir, workspaceDir, homeDir, toolName, profileID)
	if err != nil {
		return err
	}
	name := BarrelContainerNameForProfile(workspaceDir, toolName, selection.ID)
	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("resolve workspace path: %w", err)
	}
	input := barrelMountInput(absWorkspace, homeDir, cfg, cooperDir, toolName, name)
	input.Agent = &selection.Paths
	mountInput, err := workload.ResolveMountInput(input)
	if err != nil {
		return err
	}
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
	environment := append(workload.RuntimeEnvironment(cfg, ProxyHost(), InternalNetworkName()), mountInput.Agent.Environment...)
	digest, err := workload.RuntimeDigest(mounts, environment)
	if err != nil {
		return err
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
		"--label", "cooper.mount-plan=" + digest,
	}
	if selection.ID != "" {
		args = append(args, "--label", "cooper.profile-id="+selection.ID, "--label", "cooper.profile="+selection.Name)
	}

	// Volume mounts.
	args = appendDockerMounts(args, mounts)

	// Render the shared non-secret environment with this back end's proxy.
	for _, value := range workload.RenderEnvironment(environment) {
		args = append(args, "-e", value)
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
		value := workload.DockerBindMount(mount.Source, mount.Target, mount.Access == workload.ReadOnly)
		args = append(args, "--mount", value)
	}
	return args
}

func barrelMountInput(absWorkspace, homeDir string, cfg *config.Config, cooperDir, toolName, containerName string) workload.MountInput {
	return workload.MountInput{
		WorkspaceDir: absWorkspace,
		HomeDir:      homeDir,
		CooperDir:    cooperDir,
		RuntimeID:    containerName,
		ToolName:     toolName,
		Environment:  workload.HostPathEnvironment(),
		Config:       cfg,
	}
}

// BarrelMatchesHost rejects reuse after an image, state-root, or path-variable
// change. All agents use this check; new state roots need no new reuse branch.
func BarrelMatchesHost(name string, cfg *config.Config, workspace, cooperDir, home, tool string) (bool, error) {
	return BarrelMatchesProfile(name, cfg, workspace, cooperDir, home, tool, "")
}

func BarrelMatchesProfile(name string, cfg *config.Config, workspace, cooperDir, home, tool, profileID string) (bool, error) {
	if err := ValidateImageAccount(GetImageCLI(tool), home); err != nil {
		return false, err
	}
	selection, err := profilemanager.SelectID(context.Background(), cooperDir, workspace, home, tool, profileID)
	if err != nil {
		return false, err
	}
	if containerLabel(name, "cooper.profile-id") != selection.ID {
		return false, nil
	}
	raw := barrelMountInput(workspace, home, cfg, cooperDir, tool, name)
	raw.Agent = &selection.Paths
	input, err := workload.ResolveMountInput(raw)
	if err != nil {
		return false, err
	}
	if err := workload.EnsureDirectories(input); err != nil {
		return false, err
	}
	mounts, err := workload.BuildMountPlan(input)
	if err != nil {
		return false, err
	}
	environment := append(workload.RuntimeEnvironment(cfg, ProxyHost(), InternalNetworkName()), input.Agent.Environment...)
	digest, err := workload.RuntimeDigest(mounts, environment)
	if err != nil {
		return false, err
	}
	if containerLabel(name, "cooper.mount-plan") != digest {
		return false, nil
	}
	current, err := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", GetImageCLI(tool)).Output()
	if err != nil {
		return false, err
	}
	built, err := exec.Command("docker", "inspect", "--format", "{{.Image}}", name).Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(current)) == strings.TrimSpace(string(built)), nil
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
			ProfileID:    containerLabel(name, "cooper.profile-id"),
			ProfileName:  containerLabel(name, "cooper.profile"),
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
