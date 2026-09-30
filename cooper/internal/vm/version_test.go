package vm

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/launch"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func TestAgentImageInspectionRequiresLiveHostVersion(t *testing.T) {
	old := config.HostVersionDetector
	t.Cleanup(func() { config.HostVersionDetector = old })
	config.HostVersionDetector = func(string) (string, error) { return "2.0.0", nil }
	imageID := "sha256:" + strings.Repeat("a", 64)
	for _, built := range []string{"1.0.0", "2.0.0", ""} {
		runner := &recordingRunner{output: func(command string) ([]byte, error) {
			if command != "docker image inspect "+imageID {
				t.Fatalf("unexpected runtime action: %s", command)
			}
			return []byte(fmt.Sprintf(`[{"Id":%q,"Config":{"Labels":{%q:%q,%q:"codex",%q:%q}}}]`,
				imageID, workload.VMImageContractLabel, workload.VMImageContractVersion,
				launch.AIToolLabel, launch.AIVersionLabel, built)), nil
		}}
		got, err := inspectToolImageID(context.Background(), imageID, "codex", runner)
		if built == "2.0.0" {
			if err != nil || got != imageID {
				t.Fatalf("matching host version rejected: %v", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "cooper build") {
			t.Fatalf("image version %q did not require a rebuild: %v", built, err)
		}
	}
}
