package clipboard

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

var inspectContainerSession = inspectContainerSessionDocker

type dockerContainerInspect struct {
	Config struct {
		Env   []string `json:"Env"`
		Image string   `json:"Image"`
	} `json:"Config"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
}

func inspectContainerSessionDocker(containerName string) (*BarrelSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "inspect", containerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("inspect container %s: %w", containerName, err)
	}

	var inspected []dockerContainerInspect
	if err := json.Unmarshal(output, &inspected); err != nil {
		return nil, fmt.Errorf("parse docker inspect for %s: %w", containerName, err)
	}
	if len(inspected) == 0 {
		return nil, fmt.Errorf("container %s not found", containerName)
	}

	info := inspected[0]
	if !info.State.Running {
		return nil, nil
	}

	env := envMap(info.Config.Env)
	toolName := strings.TrimSpace(env["COOPER_CLI_TOOL"])
	if toolName == "" {
		toolName = toolNameFromImage(info.Config.Image)
	}

	mode := normalizeClipboardMode(env["COOPER_CLIPBOARD_MODE"])
	return &BarrelSession{
		ContainerName: containerName,
		ToolName:      toolName,
		ClipboardMode: mode,
		Eligible:      mode != "off",
	}, nil
}

func envMap(entries []string) map[string]string {
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		values[key] = value
	}
	return values
}

func toolNameFromImage(image string) string {
	const marker = "cooper-cli-"
	idx := strings.LastIndex(image, marker)
	if idx < 0 {
		return ""
	}
	return image[idx+len(marker):]
}

func normalizeClipboardMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "off":
		return "off"
	case "shim":
		return "shim"
	case "x11":
		return "x11"
	case "auto":
		return "auto"
	default:
		return "auto"
	}
}
