package docker

import (
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/launch"
)

func ValidateImageVersion(tool string) error {
	if !aitool.IsBuiltin(tool) {
		return nil
	}
	data, err := exec.Command("docker", "image", "inspect", "--format", "{{json .Config.Labels}}", GetImageCLI(tool)).Output()
	if err != nil {
		return fmt.Errorf("read %s image version: %w", tool, err)
	}
	var labels map[string]string
	if err := json.Unmarshal(data, &labels); err != nil {
		return fmt.Errorf("read image version labels: %w", err)
	}
	return launch.CheckImageVersion(tool, labels)
}
