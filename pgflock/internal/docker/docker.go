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

	"github.com/rickchristie/govner/pgflock/internal/config"
)

// BuildImage builds the PostgreSQL Docker image
func BuildImage(cfg *config.Config, configDir string) error {
	imageName := cfg.ImageName()

	cmd := exec.Command("docker", "build", "--no-cache", "-t", imageName, configDir)
	cmd.Stdout = nil // Will be set by caller if needed
	cmd.Stderr = nil

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker build failed: %w\n%s", err, string(output))
	}

	return nil
}

// BuildImageWithOutput builds the image and streams output live
func BuildImageWithOutput(cfg *config.Config, configDir string) error {
	imageName := cfg.ImageName()

	cmd := exec.Command("docker", "build", "--no-cache", "-t", imageName, configDir)
	cmd.Stdout = os.Stdout

	// Stream stderr live while also capturing it for error reporting
	var stderrBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w\n%s", err, stderrBuf.String())
	}

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
			"-e", fmt.Sprintf("NUM_TEST_DBS=%d", cfg.DatabasesPerInstance),
			"-e", fmt.Sprintf("PGPORT=%d", port),
			imageName,
			"postgres", "-c", fmt.Sprintf("port=%d", port),
			"-c", "config_file=/etc/postgresql/postgresql.conf",
		}

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
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		cmd := exec.Command("pg_isready",
			"-h", "localhost",
			"-p", fmt.Sprintf("%d", port),
			"-U", cfg.PGUsername,
		)

		if err := cmd.Run(); err == nil {
			return nil
		}

		time.Sleep(500 * time.Millisecond)
	}
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
