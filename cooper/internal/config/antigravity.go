package config

import (
	"fmt"
	"runtime"

	"github.com/rickchristie/govner/cooper/internal/antigravity"
)

// Keep this network boundary injectable with the other version resolvers.
var AntigravityReleaseResolver = antigravity.NewClient().Resolve

func resolveAntigravityLatest() (string, error) {
	release, err := antigravity.NewClient().Latest(runtime.GOARCH)
	return release.Version, err
}

func validateAntigravityVersion(version string) (bool, error) {
	_, err := AntigravityReleaseResolver(version, runtime.GOARCH, nil)
	return err == nil, err
}

func resolveAntigravityReleases(tool *ToolConfig) error {
	if tool.Name != "antigravity" || !tool.Enabled || tool.Mode == ModeOff {
		return nil
	}
	version := tool.PinnedVersion
	if tool.Mode == ModeMirror {
		version = tool.HostVersion
	}
	releases := make([]antigravity.Release, 0, 2)
	for _, arch := range []string{"amd64", "arm64"} {
		release, err := AntigravityReleaseResolver(version, arch, tool.AntigravityReleases)
		if err != nil {
			return fmt.Errorf("resolve Antigravity %s archive: %w", arch, err)
		}
		releases = append(releases, release)
	}
	// Publish both architectures together; a failed second lookup must not
	// replace a valid prior record with an incomplete build input.
	tool.AntigravityReleases = releases
	return nil
}
