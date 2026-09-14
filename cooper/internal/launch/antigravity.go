package launch

import (
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/auth"
	"github.com/rickchristie/govner/cooper/internal/profileauth"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func checkAntigravityAuth(request SessionRequest, tokens []auth.TokenResult) error {
	if request.ToolName != "antigravity" || request.State != nil && request.State.ID != "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	environment := map[string]string{"DBUS_SESSION_BUS_ADDRESS": os.Getenv("DBUS_SESSION_BUS_ADDRESS")}
	for _, token := range tokens {
		environment[token.Name] = token.Value
	}
	var mounts []workload.MountSpec
	if request.State != nil {
		mounts = request.State.Paths.Mounts
		for _, mount := range mounts {
			if mount.ID == "antigravity-state" {
				home = filepath.Dir(mount.Target)
			}
		}
	} else {
		paths, err := workload.ResolveAgentScope("antigravity", home, request.WorkspaceDir, environment)
		if err != nil {
			return err
		}
		mounts = paths.Mounts
	}
	return profileauth.CheckAntigravityLaunch(home, mounts, environment)
}
