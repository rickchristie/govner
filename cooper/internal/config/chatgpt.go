package config

import (
	"fmt"
	"runtime"

	"github.com/rickchristie/govner/cooper/internal/chatgpt"
)

var ChatGPTReleaseResolver = chatgpt.NewClient().Resolve

func resolveChatGPTLatest() (string, error) {
	release, err := chatgpt.NewClient().Latest(runtime.GOARCH)
	return release.Version, err
}

func validateChatGPTVersion(version string) (bool, error) {
	_, err := ChatGPTReleaseResolver(version, runtime.GOARCH, nil)
	return err == nil, err
}

func resolveChatGPTReleases(tool *ToolConfig) error {
	if tool.Name != "chatgpt" || !tool.Enabled || tool.Mode == ModeOff {
		return nil
	}
	version := tool.PinnedVersion
	if tool.Mode == ModeMirror {
		version = tool.HostVersion
	}
	var releases []chatgpt.Release
	for _, arch := range []string{"amd64", "arm64"} {
		release, err := ChatGPTReleaseResolver(version, arch, tool.ChatGPTReleases)
		if err != nil {
			return fmt.Errorf("resolve ChatGPT %s package: %w", arch, err)
		}
		releases = append(releases, release)
	}
	tool.ChatGPTReleases = releases
	return nil
}
