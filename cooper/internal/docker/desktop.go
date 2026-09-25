package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/desktop"
	"github.com/rickchristie/govner/cooper/internal/launch"
	"github.com/rickchristie/govner/cooper/internal/profilemanager"
)

// StartDesktop refreshes the session environment after a barrel restart.
// Secrets are resolved at launch and are never kept in container labels.
func StartDesktop(ctx context.Context, cfg *config.Config, cooperDir, name string) error {
	tool := containerLabel(name, "cooper.tool")
	if !aitool.IsDesktop(tool) {
		return nil
	}
	if _, err := desktopContainerID(ctx, name); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	workspace := containerWorkspacePath(name)
	selection, err := profilemanager.SelectID(ctx, cooperDir, workspace, home, tool, containerLabel(name, "cooper.profile-id"))
	if err != nil {
		return err
	}
	session, warnings, err := launch.PrepareSession(launch.SessionRequest{
		Config: cfg, CooperDir: cooperDir, RuntimeID: name, ToolName: tool,
		WorkspaceDir: workspace, OneShot: "cooper-desktop-start", State: &selection,
	})
	if err != nil {
		return err
	}
	defer session.Close()
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, warning)
	}
	return ExecBarrel(name, session.Command, session.Environment, false)
}

func desktopContainerID(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "docker", "inspect", name).Output()
	if err != nil {
		return "", err
	}
	var values []struct {
		ID     string `json:"Id"`
		State  struct{ Running bool }
		Config struct{ Labels map[string]string }
	}
	if err := json.Unmarshal(data, &values); err != nil {
		return "", err
	}
	if len(values) != 1 {
		return "", errors.New("desktop barrel was not found")
	}
	value := values[0]
	if !value.State.Running || value.Config.Labels["cooper.kind"] != "cli" || value.Config.Labels["cooper.runtime-id"] != name || !aitool.IsDesktop(value.Config.Labels["cooper.tool"]) {
		return "", errors.New("this container is not a running Cooper desktop")
	}
	return value.ID, nil
}

func desktopViewerDir(cooperDir, name string) (string, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return "", errors.New("desktop barrel name is invalid")
	}
	return filepath.Join(cooperDir, "desktop", "cli", name), nil
}

func OpenDesktopViewer(ctx context.Context, name, executable, cooperDir string) (string, error) {
	if _, err := desktopContainerID(ctx, name); err != nil {
		return "", err
	}
	directory, err := desktopViewerDir(cooperDir, name)
	if err != nil {
		return "", err
	}
	return desktop.OpenViewer(ctx, directory, executable, []string{"--config", cooperDir, "__desktop-viewer", "cli", name})
}

func ServeDesktopViewer(ctx context.Context, cooperDir, name string) error {
	directory, err := desktopViewerDir(cooperDir, name)
	if err != nil {
		return err
	}
	id, err := desktopContainerID(ctx, name)
	if err != nil {
		return err
	}
	return desktop.ServeViewer(ctx, directory, func(request context.Context) (net.Conn, error) {
		// Docker exec also works when a desktop Docker VM hides its network
		// from the host. It exposes no published or arbitrary guest port.
		return desktop.DialCommand(request, "docker", "exec", "-i", id, "socat", "STDIO", "TCP:127.0.0.1:6080,connect-timeout=5")
	}, func() bool { current, err := desktopContainerID(ctx, name); return err == nil && current == id })
}
