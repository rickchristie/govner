package launch

import (
	"testing"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

func TestDesktopStateDirectoryUsesSelectedSource(t *testing.T) {
	for _, test := range []struct{ name, target, want string }{
		{"root", "/home/user/.codex", "/selected/profile"},
		{"covered child", "/home/user/.codex/desktop", "/selected/profile/desktop"},
		{"another root", "/home/user/.codex-other", ""},
		{"missing", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := workload.AgentPaths{
				Mounts:      []workload.MountSpec{{ID: "codex-state", Source: "/selected/profile", Target: "/home/user/.codex"}},
				Environment: []workload.EnvVar{{Name: "CODEX_ELECTRON_USER_DATA_PATH", Value: test.target}},
			}
			got, err := desktopStateDirectory(paths)
			if got != test.want || (err != nil) != (test.want == "") {
				t.Fatalf("directory = %q, error = %v", got, err)
			}
		})
	}
}
