package docker

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/usercontext"
)

// ValidateImageAccount checks the build identity before a workload can write
// host state. A new account needs a rebuild, not a runtime ownership change.
func ValidateImageAccount(imageName, homeDir string) error {
	account, err := usercontext.Current()
	if err != nil {
		return err
	}
	account.Home = homeDir
	if err := account.Validate(); err != nil {
		return err
	}
	output, err := exec.Command("docker", "image", "inspect", "--format", `{{index .Config.Labels "cooper.account"}}`, imageName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect image account: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return account.CheckLabel(strings.TrimSpace(string(output)))
}
