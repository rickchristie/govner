package config

import (
	"strings"

	"github.com/rickchristie/govner/cooper/internal/antigravity"
)

// Install and ordinary account/API hosts only. Consumer and GCP restore
// selected the two Cloud Code hosts in native 1.2.2 probes. API-key auth and
// OAuth endpoints are documented by Google. Authenticated final tests still
// check this set. Telemetry, browser downloads, WIF, and custom MCP endpoints
// remain separate user choices; a compiled hostname is not a default grant.
func antigravityDefaultDomains() []DomainEntry {
	var entries []DomainEntry
	for _, host := range []string{
		antigravity.ManifestHost, "storage.googleapis.com", "antigravity.google",
		"accounts.google.com", "oauth2.googleapis.com", "www.googleapis.com",
		// Native 1.2.2 fails its account eligibility check if this profile
		// picture host is blocked. A real file-OAuth account verified this.
		"lh3.googleusercontent.com",
		"daily-cloudcode-pa.googleapis.com", "cloudcode-pa.googleapis.com",
		"generativelanguage.googleapis.com",
	} {
		entries = append(entries, DomainEntry{Domain: host, Source: "default"})
	}
	return entries
}

func (c *Config) reconcileToolDefaultDomains() {
	groups := []struct {
		tool    string
		entries []DomainEntry
	}{
		{"grok", grokDefaultDomains()},
		{"antigravity", antigravityDefaultDomains()},
	}
	enabled := map[string]bool{}
	for _, tool := range c.AITools {
		if tool.Enabled {
			enabled[strings.ToLower(tool.Name)] = true
		}
	}
	managed, wanted := map[string]bool{}, map[string]bool{}
	for _, entry := range defaultWhitelistedDomains() {
		wanted[strings.ToLower(entry.Domain)] = true
	}
	for _, group := range groups {
		for _, entry := range group.entries {
			host := strings.ToLower(entry.Domain)
			managed[host] = true
			if enabled[group.tool] {
				wanted[host] = true
			}
		}
	}
	kept := c.WhitelistedDomains[:0]
	for _, entry := range c.WhitelistedDomains {
		host := strings.ToLower(entry.Domain)
		if entry.Source == "default" && managed[host] && !wanted[host] {
			continue
		}
		kept = append(kept, entry)
	}
	c.WhitelistedDomains = kept
	for _, group := range groups {
		if !enabled[group.tool] {
			continue
		}
		for _, entry := range group.entries {
			if !hasDomainIgnoreCase(c.WhitelistedDomains, entry.Domain) {
				c.WhitelistedDomains = append(c.WhitelistedDomains, entry)
			}
		}
	}
}
