package vm

import (
	"context"
	"fmt"
	"strings"
)

const dockerOwnershipFormat = `{{.Id}}	{{index .Config.Labels "cooper.kind"}}	{{index .Config.Labels "cooper.runtime-id"}}`
const dockerNetworkOwnershipFormat = `{{.Id}}	{{index .Labels "cooper.kind"}}	{{index .Labels "cooper.runtime-id"}}`

// inspectOwnedContainer returns an immutable Docker ID only when both Cooper
// ownership labels match. A matching name is not ownership evidence.
func inspectOwnedContainer(ctx context.Context, runner CommandRunner, name, kind, runtimeID string) (string, bool, error) {
	return inspectOwnedObject(ctx, runner, "container", name, kind, runtimeID)
}

// inspectOwnedNetwork applies the same label rule to one relay network.
func inspectOwnedNetwork(ctx context.Context, runner CommandRunner, name, kind, runtimeID string) (string, bool, error) {
	return inspectOwnedObject(ctx, runner, "network", name, kind, runtimeID)
}

func inspectOwnedObject(ctx context.Context, runner CommandRunner, objectType, name, kind, runtimeID string) (string, bool, error) {
	format := dockerOwnershipFormat
	arguments := []string{"container", "inspect", "--format", format, name}
	if objectType == "network" {
		format = dockerNetworkOwnershipFormat
		arguments = []string{"network", "inspect", "--format", format, name}
	}
	output, err := runnerOrSystem(runner).Output(ctx, "docker", arguments...)
	if err != nil {
		if isMissingDockerObject(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("inspect VM %s %s: %w", objectType, name, err)
	}
	fields := strings.Split(strings.TrimSpace(string(output)), "\t")
	if len(fields) != 3 || !validDockerID(fields[0]) {
		return "", false, fmt.Errorf("VM %s %s returned invalid ownership data", objectType, name)
	}
	if fields[1] != kind || fields[2] != runtimeID {
		return "", false, fmt.Errorf("refuse %s %s: ownership labels are %q and %q, want %q and %q", objectType, name, fields[1], fields[2], kind, runtimeID)
	}
	return fields[0], true, nil
}

func validDockerID(value string) bool {
	if len(value) < 12 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}
