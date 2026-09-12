package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StopAll stops every VM resource in this namespace, including stale relay
// networks and runtime directories left after an interrupted process.
func (m Manager) StopAll(ctx context.Context) error {
	ids, err := m.runtimeIDs(ctx)
	if err != nil {
		return err
	}
	var failures []string
	for _, id := range ids {
		controlDir := ControlDir(m.CooperDir, id)
		runtime := Runtime{
			ID: id, ContainerName: id,
			RelayName: RelayContainerName(id), RelayNetwork: RelayNetworkName(id),
			RuntimeDir:    RuntimeDir(m.CooperDir, id),
			ControlDir:    controlDir,
			ControlSocket: ControlSocketPath(m.CooperDir, id),
		}
		if err := m.Stop(ctx, runtime); err != nil {
			failures = append(failures, fmt.Sprintf("stop %s: %v", id, err))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func (m Manager) runtimeIDs(ctx context.Context) ([]string, error) {
	prefix := cleanNamePart(m.Namespace) + "-vm-"
	ids := make(map[string]bool)
	runner := runnerOrSystem(m.Runner)
	for _, command := range [][]string{
		{"ps", "-a", "--filter", "label=cooper.runtime-id", "--format", `{{.Label "cooper.runtime-id"}}`},
		{"network", "ls", "--filter", "label=cooper.runtime-id", "--format", `{{.Label "cooper.runtime-id"}}`},
	} {
		output, err := runner.Output(ctx, "docker", command...)
		if err != nil {
			return nil, fmt.Errorf("list Cooper VM resources: %w", err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			id := strings.TrimSpace(line)
			if strings.HasPrefix(id, prefix) && cleanNamePart(id) == id {
				ids[id] = true
			}
		}
	}
	runRoot := filepath.Join(m.CooperDir, "vm", "run")
	entries, err := os.ReadDir(runRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("list VM runtime directories: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) && cleanNamePart(entry.Name()) == entry.Name() {
			ids[entry.Name()] = true
		}
	}
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result, nil
}
