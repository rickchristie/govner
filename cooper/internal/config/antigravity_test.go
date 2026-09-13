package config

import (
	"errors"
	"reflect"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/antigravity"
)

func TestAntigravityRefreshFreezesBothPlatforms(t *testing.T) {
	oldHost, oldLatest := HostVersionDetector, LatestVersionResolver
	t.Cleanup(func() { HostVersionDetector, LatestVersionResolver = oldHost, oldLatest })
	HostVersionDetector = func(string) (string, error) { return "1.2.2", nil }
	LatestVersionResolver = func(string) (string, error) { return "1.2.2", nil }
	for _, mode := range []VersionMode{ModePin, ModeMirror, ModeLatest} {
		cfg := &Config{AITools: []ToolConfig{{Name: "antigravity", Enabled: true, Mode: mode, PinnedVersion: "1.2.2"}}}
		if _, err := RefreshDesiredToolVersions(cfg, DesiredVersionRefreshOptions{}); err != nil {
			t.Fatal(err)
		}
		records := cfg.AITools[0].AntigravityReleases
		if len(records) != 2 || records[0].Arch != "amd64" || records[1].Arch != "arm64" {
			t.Fatalf("platform records: %#v", records)
		}
		copy := CloneConfig(cfg)
		copy.AITools[0].AntigravityReleases[0].URL = "changed"
		if cfg.AITools[0].AntigravityReleases[0].URL == "changed" {
			t.Fatal("config clone shares release records")
		}
	}
}

func TestAntigravityFailedPlatformLookupPreservesSavedRecords(t *testing.T) {
	old := AntigravityReleaseResolver
	t.Cleanup(func() { AntigravityReleaseResolver = old })
	saved := []antigravity.Release{{Version: "1.2.2", Arch: "amd64", URL: "unchanged"}}
	tool := ToolConfig{Name: "antigravity", Enabled: true, Mode: ModePin, PinnedVersion: "1.2.2", AntigravityReleases: saved}
	AntigravityReleaseResolver = func(version, arch string, records []antigravity.Release) (antigravity.Release, error) {
		if !reflect.DeepEqual(records, saved) {
			t.Fatal("saved records were not supplied")
		}
		if arch == "arm64" {
			return antigravity.Release{}, errors.New("unavailable")
		}
		return antigravity.Release{Version: version, Arch: arch}, nil
	}
	if err := resolveAntigravityReleases(&tool); err == nil {
		t.Fatal("partial lookup succeeded")
	}
	if !reflect.DeepEqual(tool.AntigravityReleases, saved) {
		t.Fatal("partial lookup replaced saved records")
	}
}

func TestAntigravityManagedDomainsPreserveUserAndSharedRules(t *testing.T) {
	cfg := &Config{AITools: []ToolConfig{{Name: "antigravity", Enabled: true}, {Name: "grok", Enabled: true}}, WhitelistedDomains: []DomainEntry{
		{Domain: "ACCOUNTS.GOOGLE.COM", Source: "user"},
	}}
	cfg.MergeDefaultDomains()
	if !hasDomainIgnoreCase(cfg.WhitelistedDomains, "lh3.googleusercontent.com") {
		t.Fatal("Google account eligibility requires the profile picture host")
	}
	first := append([]DomainEntry(nil), cfg.WhitelistedDomains...)
	cfg.MergeDefaultDomains()
	if !reflect.DeepEqual(first, cfg.WhitelistedDomains) {
		t.Fatal("domain merge is not stable")
	}
	for _, entry := range antigravityDefaultDomains() {
		if entry.IncludeSubdomains || !hasDomainIgnoreCase(cfg.WhitelistedDomains, entry.Domain) {
			t.Fatalf("missing exact host %s", entry.Domain)
		}
	}
	cfg.AITools[0].Enabled = false
	cfg.MergeDefaultDomains()
	for _, host := range []string{"daily-cloudcode-pa.googleapis.com", "lh3.googleusercontent.com"} {
		if hasDomainIgnoreCase(cfg.WhitelistedDomains, host) {
			t.Fatal("disabled tool retained its private default")
		}
	}
	if !hasDomainIgnoreCase(cfg.WhitelistedDomains, "ACCOUNTS.GOOGLE.COM") || !hasDomainIgnoreCase(cfg.WhitelistedDomains, "auth.x.ai") {
		t.Fatal("user rule or other enabled tool host removed")
	}
	if cfg.WhitelistedDomains[0].Source != "user" {
		t.Fatal("user rule was changed to managed")
	}
}
