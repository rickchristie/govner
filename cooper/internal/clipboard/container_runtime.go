package clipboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
	"github.com/rickchristie/govner/cooper/internal/vmstate"
)

var inspectRuntimeSession = inspectRuntimeSessionDocker

type dockerContainerInspect struct {
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
}

func inspectRuntimeSessionDocker(cooperDir, containerName string) (*RuntimeSession, error) {
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

	labels := info.Config.Labels
	runtimeID := strings.TrimSpace(labels["cooper.runtime-id"])
	toolName := strings.TrimSpace(labels["cooper.tool"])
	mode := strings.TrimSpace(labels["cooper.clipboard-mode"])
	kind := ""
	switch strings.TrimSpace(labels["cooper.kind"]) {
	case RuntimeCLI:
		kind = RuntimeCLI
	case "vm-supervisor":
		kind = RuntimeVM
	}
	if runtimeID != containerName || toolName == "" || kind == "" || !validClipboardMode(mode) {
		return nil, nil
	}
	if kind == RuntimeVM && !vmControlHealthy(cooperDir, runtimeID) {
		return nil, nil
	}
	return &RuntimeSession{
		RuntimeID:     containerName,
		RuntimeKind:   kind,
		ToolName:      toolName,
		ClipboardMode: mode,
		Eligible:      mode != "off",
	}, nil
}

func vmControlHealthy(cooperDir, runtimeID string) bool {
	if strings.TrimSpace(cooperDir) == "" {
		return false
	}
	socket := vmstate.ControlSocketPath(cooperDir, runtimeID)
	connection, err := net.DialTimeout("unix", socket, 250*time.Millisecond)
	if err != nil {
		return false
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(750 * time.Millisecond))
	if err := vmproto.WriteHeader(connection, vmproto.NewHeader(vmproto.ServiceHealth, "clipboard-health")); err != nil {
		return false
	}
	frame, err := vmproto.ReadFrame(connection)
	return err == nil && frame.Type == vmproto.FrameStdout && string(frame.Data) == "ready"
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

func validClipboardMode(mode string) bool {
	switch mode {
	case "off", "shim", "x11", "auto":
		return true
	default:
		return false
	}
}
