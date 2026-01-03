package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/rickchristie/govner/pgflock/internal/config"
)

// BuildImage builds the PostgreSQL Docker image
func BuildImage(cfg *config.Config, configDir string) error {
	imageName := cfg.ImageName()

	// Delete existing image first (like testdb's build-docker.sh)
	_ = exec.Command("docker", "rmi", imageName).Run()

	cmd := exec.Command("docker", "build", "--no-cache", "-t", imageName, configDir)
	cmd.Stdout = nil // Will be set by caller if needed
	cmd.Stderr = nil

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker build failed: %w\n%s", err, string(output))
	}

	// Clean up dangling images after build
	_ = exec.Command("docker", "system", "prune", "-f").Run()

	return nil
}

// BuildImageWithOutput builds the image and streams output live
func BuildImageWithOutput(cfg *config.Config, configDir string) error {
	imageName := cfg.ImageName()

	// Delete existing image first (like testdb's build-docker.sh)
	fmt.Println("Removing existing image...")
	_ = exec.Command("docker", "rmi", imageName).Run()

	cmd := exec.Command("docker", "build", "--no-cache", "-t", imageName, configDir)
	cmd.Stdout = os.Stdout

	// Stream stderr live while also capturing it for error reporting
	var stderrBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w\n%s", err, stderrBuf.String())
	}

	// Clean up dangling images after build
	fmt.Println("Cleaning up dangling images...")
	_ = exec.Command("docker", "system", "prune", "-f").Run()

	return nil
}

// RunContainers starts all PostgreSQL containers
func RunContainers(cfg *config.Config) error {
	imageName := cfg.ImageName()

	for _, port := range cfg.InstancePorts() {
		containerName := cfg.ContainerName(port)

		// Remove existing container if any
		_ = exec.Command("docker", "rm", "-f", containerName).Run()

		args := []string{
			"run", "-d",
			"--name", containerName,
			"--net=host",
			"--tmpfs", fmt.Sprintf("/var/lib/postgresql/data:rw,noexec,nosuid,size=%s", cfg.TmpfsSize),
			"--shm-size", cfg.ShmSize,
		}

		// Add CPU limit if configured
		if cfg.CPULimit != "" {
			args = append(args, "--cpus", cfg.CPULimit)
		}

		args = append(args,
			"-e", fmt.Sprintf("NUM_TEST_DBS=%d", cfg.DatabasesPerInstance),
			"-e", fmt.Sprintf("PGPORT=%d", port),
			imageName,
			"postgres", "-c", fmt.Sprintf("port=%d", port),
			"-c", "config_file=/etc/postgresql/postgresql.conf",
		)

		cmd := exec.Command("docker", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to start container %s: %w\n%s", containerName, err, string(output))
		}
	}

	return nil
}

// StopContainers stops all PostgreSQL containers
func StopContainers(cfg *config.Config) error {
	var errs []string

	for _, port := range cfg.InstancePorts() {
		containerName := cfg.ContainerName(port)

		cmd := exec.Command("docker", "stop", containerName)
		if err := cmd.Run(); err != nil {
			errs = append(errs, fmt.Sprintf("failed to stop %s: %v", containerName, err))
			continue
		}

		// Remove the container
		cmd = exec.Command("docker", "rm", containerName)
		_ = cmd.Run() // Ignore error on rm
	}

	// Clean up dangling containers and images (like testdb's stop-docker.sh)
	_ = exec.Command("docker", "system", "prune", "-f").Run()

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping containers:\n%s", strings.Join(errs, "\n"))
	}

	return nil
}

// WaitForPostgres waits for all PostgreSQL instances to be ready
func WaitForPostgres(ctx context.Context, cfg *config.Config, timeout time.Duration) error {
	for _, port := range cfg.InstancePorts() {
		if err := WaitForPostgresOnPort(ctx, cfg, port); err != nil {
			return err
		}
	}
	return nil
}

// WaitForPostgresOnPort waits for a specific PostgreSQL instance to be ready
func WaitForPostgresOnPort(ctx context.Context, cfg *config.Config, port int) error {
	containerName := cfg.ContainerName(port)
	log.Info().Int("port", port).Str("container", containerName).Msg("WaitForPostgresOnPort: starting")

	// First, wait for the container logs to show PostgreSQL is ready.
	// This is foolproof because "database system is ready to accept connections"
	// only appears after PostgreSQL successfully binds to the TCP port.
	if err := waitForPostgresLogs(ctx, containerName, port); err != nil {
		return err
	}

	// Then verify with pg_isready via Unix socket
	cmd := exec.Command("docker", "exec", containerName,
		"pg_isready",
		"-h", "/var/run/postgresql",
		"-p", fmt.Sprintf("%d", port),
		"-U", cfg.PGUsername,
	)
	if err := cmd.Run(); err != nil {
		log.Error().Int("port", port).Err(err).Msg("WaitForPostgresOnPort: pg_isready failed after logs showed ready")
		return fmt.Errorf("pg_isready failed for container %s: %w", containerName, err)
	}

	log.Info().Int("port", port).Msg("WaitForPostgresOnPort: ready")
	return nil
}

// waitForPostgresLogs waits for the PostgreSQL ready message in container logs
func waitForPostgresLogs(ctx context.Context, containerName string, port int) error {
	const initCompleteMsg = "PostgreSQL init process complete"
	const readyMsg = "database system is ready to accept connections"
	const bindErrorMsg = "Address already in use"

	attempt := 0
	for {
		select {
		case <-ctx.Done():
			log.Error().Int("port", port).Int("attempts", attempt).Err(ctx.Err()).Msg("waitForPostgresLogs: context cancelled")
			return ctx.Err()
		default:
		}

		attempt++

		// Check container logs
		cmd := exec.Command("docker", "logs", containerName)
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Debug().Int("port", port).Err(err).Msg("waitForPostgresLogs: failed to get logs")
			time.Sleep(500 * time.Millisecond)
			continue
		}

		logs := string(output)

		// Find where init completes - we only care about messages after this point
		initCompleteIdx := strings.Index(logs, initCompleteMsg)
		if initCompleteIdx == -1 {
			// Init not complete yet, keep waiting
			log.Debug().Int("port", port).Int("attempt", attempt).Msg("waitForPostgresLogs: init not complete yet")
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Only check logs after init complete
		postInitLogs := logs[initCompleteIdx:]

		// Check for bind error (this appears before ready message if port is taken)
		if strings.Contains(postInitLogs, bindErrorMsg) {
			log.Error().Int("port", port).Msg("waitForPostgresLogs: port already in use")
			return fmt.Errorf("port %d is already in use by another process", port)
		}

		// Check for success after init
		if strings.Contains(postInitLogs, readyMsg) {
			log.Debug().Int("port", port).Int("attempts", attempt).Msg("waitForPostgresLogs: found ready message after init")
			return nil
		}

		// Check if container exited
		if !isContainerRunning(containerName) {
			log.Error().Int("port", port).Msg("waitForPostgresLogs: container exited")
			return fmt.Errorf("container %s exited unexpectedly", containerName)
		}

		log.Debug().Int("port", port).Int("attempt", attempt).Msg("waitForPostgresLogs: waiting for ready after init...")
		time.Sleep(500 * time.Millisecond)
	}
}

// isContainerRunning checks if a container is currently running
func isContainerRunning(containerName string) bool {
	cmd := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// ContainerInfo holds status information for a container
type ContainerInfo struct {
	Name    string
	Port    int
	Running bool
	Status  string
}

// ContainerStatus returns the status of each container
func ContainerStatus(cfg *config.Config) ([]ContainerInfo, error) {
	ports := cfg.InstancePorts()
	infos := make([]ContainerInfo, len(ports))

	for i, port := range ports {
		containerName := cfg.ContainerName(port)
		infos[i] = ContainerInfo{
			Name: containerName,
			Port: port,
		}

		cmd := exec.Command("docker", "inspect", "--format", "{{.State.Status}}", containerName)
		output, err := cmd.Output()
		if err != nil {
			infos[i].Status = "not found"
			infos[i].Running = false
			continue
		}

		status := strings.TrimSpace(string(output))
		infos[i].Status = status
		infos[i].Running = status == "running"
	}

	return infos, nil
}

// PostgresStatus checks if PostgreSQL is responding on a port
func PostgresStatus(cfg *config.Config, port int) bool {
	cmd := exec.Command("pg_isready",
		"-h", "localhost",
		"-p", fmt.Sprintf("%d", port),
		"-U", cfg.PGUsername,
	)
	return cmd.Run() == nil
}
