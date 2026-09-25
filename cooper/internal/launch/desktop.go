package launch

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

// A custom desktop directory can be inside another selected state root. Use
// the public target to find its source even when mount deduplication removed
// the desktop root. A named profile must never fall back to live host state.
func desktopStateDirectory(paths workload.AgentPaths) (string, error) {
	var target string
	for _, value := range paths.Environment {
		if value.Name == "CODEX_ELECTRON_USER_DATA_PATH" && !value.Unset {
			target = value.Value
		}
	}
	if !filepath.IsAbs(target) {
		return "", errors.New("ChatGPT state path is missing from the selected environment")
	}
	var source string
	longest := 0
	for _, mount := range paths.Mounts {
		relative, err := filepath.Rel(mount.Target, target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || len(mount.Target) <= longest {
			continue
		}
		source = filepath.Join(mount.Source, relative)
		longest = len(mount.Target)
	}
	if source == "" {
		return "", errors.New("ChatGPT state path is outside the selected mounts")
	}
	return source, nil
}
