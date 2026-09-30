package launch

import (
	"fmt"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/config"
)

const AIToolLabel = "cooper.ai-tool"
const AIVersionLabel = "cooper.ai-version"

// CheckImageVersion runs before a workload can write shared host state. Image
// labels record the build input; saved config can describe a different image.
func CheckImageVersion(tool string, labels map[string]string) error {
	if !aitool.IsBuiltin(tool) {
		return nil
	}
	host, err := config.HostVersionDetector(tool)
	if err != nil {
		return fmt.Errorf("cannot detect host %s version; install it on the host before launch: %w", tool, err)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("host %s version is empty; install it on the host before launch", tool)
	}
	built := labels[AIVersionLabel]
	if labels[AIToolLabel] != tool || built == "" {
		return fmt.Errorf("%s image has no matching version record; run 'cooper build'", tool)
	}
	if built != host {
		return fmt.Errorf("%s host version is %s, but the image contains %s; run 'cooper build' before launch", tool, host, built)
	}
	return nil
}
