package launch

import (
	"errors"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestImageVersionMustMatchLiveHost(t *testing.T) {
	old := config.HostVersionDetector
	t.Cleanup(func() { config.HostVersionDetector = old })
	for _, test := range []struct{ name, tool, host, built, imageTool, want string }{
		{"match", "codex", "1.2.3", "1.2.3", "codex", ""},
		{"host updated", "codex", "1.2.4", "1.2.3", "codex", "cooper build"},
		{"image newer", "chatgpt", "26.917.71314", "26.928.20755", "chatgpt", "cooper build"},
		{"old image", "codex", "1.2.3", "", "", "cooper build"},
		{"wrong tool", "codex", "1.2.3", "1.2.3", "claude", "cooper build"},
		{"missing host", "codex", "", "1.2.3", "codex", "install it on the host"},
		{"custom image", "custom-tool", "", "", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			config.HostVersionDetector = func(string) (string, error) {
				if test.host == "" {
					return "", errors.New("not installed")
				}
				return test.host, nil
			}
			err := CheckImageVersion(test.tool, map[string]string{AIToolLabel: test.imageTool, AIVersionLabel: test.built})
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want %q", err, test.want)
			}
		})
	}
}
