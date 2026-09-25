package config

import (
	"errors"
	"reflect"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/chatgpt"
)

func TestChatGPTRefreshFreezesBothPlatforms(t *testing.T) {
	oldHost, oldLatest, oldRelease := HostVersionDetector, LatestVersionResolver, ChatGPTReleaseResolver
	t.Cleanup(func() {
		HostVersionDetector, LatestVersionResolver, ChatGPTReleaseResolver = oldHost, oldLatest, oldRelease
	})
	HostVersionDetector = func(string) (string, error) { return "26.917.71314", nil }
	LatestVersionResolver = func(string) (string, error) { return "26.917.71314", nil }
	ChatGPTReleaseResolver = func(version, arch string, _ []chatgpt.Release) (chatgpt.Release, error) {
		return chatgpt.Release{Version: version, Arch: arch}, nil
	}
	for _, mode := range []VersionMode{ModePin, ModeMirror, ModeLatest} {
		cfg := &Config{AITools: []ToolConfig{{Name: "chatgpt", Enabled: true, Mode: mode, PinnedVersion: "26.917.71314"}}}
		if _, err := RefreshDesiredToolVersions(cfg, DesiredVersionRefreshOptions{}); err != nil {
			t.Fatal(err)
		}
		records := cfg.AITools[0].ChatGPTReleases
		if len(records) != 2 || records[0].Arch != "amd64" || records[1].Arch != "arm64" || records[0].Version != "26.917.71314" {
			t.Fatalf("records: %#v", records)
		}
		copy := CloneConfig(cfg)
		copy.AITools[0].ChatGPTReleases[0].URL = "changed"
		if cfg.AITools[0].ChatGPTReleases[0].URL == "changed" {
			t.Fatal("config clone shares package records")
		}
	}
}

func TestChatGPTFailedLookupPreservesSavedRecords(t *testing.T) {
	old := ChatGPTReleaseResolver
	t.Cleanup(func() { ChatGPTReleaseResolver = old })
	saved := []chatgpt.Release{{Version: "26.917.71314", Arch: "amd64", URL: "unchanged"}}
	tool := ToolConfig{Name: "chatgpt", Enabled: true, Mode: ModePin, PinnedVersion: "26.917.71314", ChatGPTReleases: saved}
	ChatGPTReleaseResolver = func(version, arch string, previous []chatgpt.Release) (chatgpt.Release, error) {
		if !reflect.DeepEqual(previous, saved) {
			t.Fatal("saved records not supplied")
		}
		if arch == "arm64" {
			return chatgpt.Release{}, errors.New("unavailable")
		}
		return chatgpt.Release{Version: version, Arch: arch}, nil
	}
	if err := resolveChatGPTReleases(&tool); err == nil {
		t.Fatal("partial resolution succeeded")
	}
	if !reflect.DeepEqual(tool.ChatGPTReleases, saved) {
		t.Fatal("partial resolution replaced saved records")
	}
}

func TestChatGPTDomainsPreserveUserRules(t *testing.T) {
	cfg := &Config{AITools: []ToolConfig{{Name: "chatgpt", Enabled: true}}, WhitelistedDomains: []DomainEntry{{Domain: "persistent.oaistatic.com", Source: "user"}}}
	cfg.MergeDefaultDomains()
	if !hasDomainIgnoreCase(cfg.WhitelistedDomains, "cdn.oaistatic.com") {
		t.Fatal("desktop assets are missing")
	}
	cfg.AITools[0].Enabled = false
	cfg.MergeDefaultDomains()
	if hasDomainIgnoreCase(cfg.WhitelistedDomains, "cdn.oaistatic.com") {
		t.Fatal("disabled desktop retained its managed domain")
	}
	if !hasDomainIgnoreCase(cfg.WhitelistedDomains, "persistent.oaistatic.com") || cfg.WhitelistedDomains[0].Source != "user" {
		t.Fatal("user domain rule changed")
	}
}
