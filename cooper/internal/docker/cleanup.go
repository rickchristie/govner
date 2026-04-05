package docker

import (
	"fmt"
	"os/exec"
	"strings"
)

// CleanupRuntime removes runtime resources for the current Docker namespace.
// It stops all namespace-scoped barrels, stops/removes the proxy container,
// and removes the namespace-scoped Docker networks.
func CleanupRuntime() error {
	var errs []string

	barrelNames, err := listAllRuntimeBarrelNames()
	if err != nil {
		errs = append(errs, fmt.Sprintf("list barrels: %v", err))
	} else {
		for _, barrelName := range barrelNames {
			if err := StopBarrel(barrelName); err != nil {
				errs = append(errs, fmt.Sprintf("stop barrel %s: %v", barrelName, err))
			}
		}
	}

	if err := StopProxy(); err != nil {
		errs = append(errs, fmt.Sprintf("stop proxy: %v", err))
	}
	if err := RemoveNetworks(); err != nil {
		errs = append(errs, fmt.Sprintf("remove networks: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup runtime: %s", strings.Join(errs, "; "))
	}
	return nil
}

func listAllRuntimeBarrelNames() ([]string, error) {
	cmd := exec.Command("docker", "ps", "-a", "--format", "{{.Names}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps -a failed: %w\n%s", err, string(output))
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return nil, nil
	}

	prefix := BarrelNamePrefix()
	var names []string
	for _, line := range strings.Split(result, "\n") {
		name := strings.TrimSpace(line)
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return names, nil
}
