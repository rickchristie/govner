package vm

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Info is the runtime-neutral identity shown by the application boundary.
type Info struct {
	ID           string
	Status       string
	WorkspaceDir string
	ToolName     string
	ProfileID    string
	ProfileName  string
	Depth        int
}

// List returns active or stopped VM supervisor IDs in the selected namespace.
func List(ctx context.Context, namespace string, runner CommandRunner) ([]string, error) {
	infos, err := ListInfo(ctx, namespace, runner)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(infos))
	for _, info := range infos {
		result = append(result, info.ID)
	}
	return result, nil
}

// ListInfo returns VM supervisors from exact labels in one namespace. Stopped
// supervisors remain visible so the TUI can diagnose or restart a crashed VM.
func ListInfo(ctx context.Context, namespace string, runner CommandRunner) ([]Info, error) {
	output, err := runnerOrSystem(runner).Output(ctx, "docker", "ps", "-a",
		"--filter", "label=cooper.kind=vm-supervisor",
		"--format", `{{.Names}}\t{{.Status}}\t{{.Label "cooper.workspace"}}\t{{.Label "cooper.tool"}}\t{{.Label "cooper.depth"}}\t{{.Label "cooper.profile-id"}}\t{{.Label "cooper.profile"}}`)
	if err != nil {
		return nil, fmt.Errorf("list Cooper VMs: %w", err)
	}
	prefix := cleanNamePart(namespace) + "-vm-"
	var result []Info
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 && len(fields) != 7 {
			continue
		}
		id := strings.TrimSpace(fields[0])
		depth, parseErr := strconv.Atoi(strings.TrimSpace(fields[4]))
		profileID, profileName := "", ""
		if len(fields) == 7 {
			profileID, profileName = fields[5], fields[6]
		}
		if parseErr == nil && id != "" && strings.HasPrefix(id, prefix) && depth >= 1 && depth <= 2 && strings.TrimSpace(fields[3]) != "" {
			result = append(result, Info{
				ID: id, Status: strings.TrimSpace(fields[1]), WorkspaceDir: strings.TrimSpace(fields[2]),
				ToolName: strings.TrimSpace(fields[3]), Depth: depth,
				ProfileID: profileID, ProfileName: profileName,
			})
		}
	}
	return result, nil
}
