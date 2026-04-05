package docker

import (
	"fmt"
	"os/exec"
	"strings"
)

// ContainerStat holds resource usage statistics for a running container.
type ContainerStat struct {
	Name       string
	CPUPercent string
	MemUsage   string
}

// ContainerStats returns CPU and memory statistics for the named container.
// Uses docker stats --no-stream for a single snapshot.
func ContainerStats(name string) (*ContainerStat, error) {
	cmd := exec.Command("docker", "stats", "--no-stream",
		"--format", "{{.CPUPerc}}\t{{.MemUsage}}",
		name,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker stats %s failed: %w\n%s", name, err, string(output))
	}

	line := strings.TrimSpace(string(output))
	if line == "" {
		return nil, fmt.Errorf("no stats output for container %s", name)
	}

	parts := strings.SplitN(line, "\t", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected stats format for container %s: %q", name, line)
	}

	return &ContainerStat{
		Name:       name,
		CPUPercent: strings.TrimSpace(parts[0]),
		MemUsage:   strings.TrimSpace(parts[1]),
	}, nil
}

// AllContainerStats returns resource statistics for the proxy container
// and all running barrel containers.
func AllContainerStats() ([]ContainerStat, error) {
	var stats []ContainerStat

	// Collect proxy stats if running.
	proxyRunning, err := IsProxyRunning()
	if err != nil {
		return nil, fmt.Errorf("check proxy status: %w", err)
	}
	if proxyRunning {
		s, err := ContainerStats(ProxyContainerName())
		if err != nil {
			return nil, fmt.Errorf("proxy stats: %w", err)
		}
		stats = append(stats, *s)
	}

	// Collect barrel stats.
	barrels, err := ListBarrels()
	if err != nil {
		return nil, fmt.Errorf("list barrels: %w", err)
	}
	for _, b := range barrels {
		s, err := ContainerStats(b.Name)
		if err != nil {
			// Skip barrels that stopped between listing and stats collection.
			continue
		}
		stats = append(stats, *s)
	}

	return stats, nil
}
