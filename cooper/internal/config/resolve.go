package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// httpClient is the shared HTTP client for all version resolution requests.
// It uses a 10-second timeout since resolution happens at configure-time,
// not runtime -- the user can retry on failure.
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// npmPackageNames maps AI CLI tool names to their npm registry package names.
var npmPackageNames = map[string]string{
	"claude":   "@anthropic-ai/claude-code",
	"copilot":  "@github/copilot",
	"codex":    "@openai/codex",
	"opencode": "opencode-ai",
}

// LatestVersionResolver is the shared latest-version hook used by configure,
// build, update, and startup warning code paths. Tests override it to keep
// those flows deterministic.
var LatestVersionResolver = ResolveLatestVersion

// VersionValidator is the shared validation hook used by strict refresh flows.
// Tests override it to avoid live registry calls.
var VersionValidator = ValidateVersion

// ResolveLatestVersion dispatches to the tool-specific resolver and returns
// the latest stable version string for the given tool.
func ResolveLatestVersion(toolName string) (string, error) {
	switch toolName {
	case "go":
		return ResolveGoLatest()
	case "node":
		return ResolveNodeLatest()
	case "python":
		return ResolvePythonLatest()
	case "claude", "copilot", "codex", "opencode":
		pkg, ok := npmPackageNames[toolName]
		if !ok {
			return "", fmt.Errorf("no npm package mapping for tool %q", toolName)
		}
		return ResolveNPMPackageLatest(pkg)
	default:
		return "", fmt.Errorf("unknown tool for version resolution: %q", toolName)
	}
}

// ValidateVersion checks whether a specific version exists in the upstream
// registry for the given tool. Returns true if the version is found.
func ValidateVersion(toolName, version string) (bool, error) {
	switch toolName {
	case "go":
		return validateGoVersion(version)
	case "node":
		return validateNodeVersion(version)
	case "python":
		return validatePythonVersion(version)
	case "claude", "copilot", "codex", "opencode":
		pkg, ok := npmPackageNames[toolName]
		if !ok {
			return false, fmt.Errorf("no npm package mapping for tool %q", toolName)
		}
		return validateNPMVersion(pkg, version)
	default:
		return false, fmt.Errorf("unknown tool for version validation: %q", toolName)
	}
}

// --- Go version resolution ---

// goRelease represents a single release entry from https://go.dev/dl/?mode=json.
type goRelease struct {
	Version string `json:"version"` // e.g. "go1.22.5"
	Stable  bool   `json:"stable"`
}

// goLatestURL is the endpoint for Go release metadata.
var goLatestURL = "https://go.dev/dl/?mode=json"

// ResolveGoLatest fetches the latest stable Go version from go.dev.
func ResolveGoLatest() (string, error) {
	body, err := httpGet(goLatestURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Go versions: %w", err)
	}

	var releases []goRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("failed to parse Go version JSON: %w", err)
	}

	for _, r := range releases {
		if r.Stable {
			return strings.TrimPrefix(r.Version, "go"), nil
		}
	}

	return "", fmt.Errorf("no stable Go version found")
}

// validateGoVersion checks if a specific Go version exists in the release list.
func validateGoVersion(version string) (bool, error) {
	// The go.dev/dl/?mode=json API only returns the latest 2 major versions.
	// Older valid versions (e.g., 1.24.10 when 1.26 is out) won't be listed.
	// Instead, check if the specific version download URL exists (HTTP HEAD).
	// linux-amd64 is used here as a canonical existence check only. The actual
	// Cooper build installs Go from the multi-arch golang: Docker image, so the
	// version validation itself is not architecture-sensitive.
	url := fmt.Sprintf("https://go.dev/dl/go%s.linux-amd64.tar.gz", version)
	resp, err := httpClient.Head(url)
	if err != nil {
		return false, fmt.Errorf("failed to check Go version: %w", err)
	}
	resp.Body.Close()
	// 200 = exists, 404 = doesn't exist
	return resp.StatusCode == http.StatusOK, nil
}

// --- Node.js version resolution ---

// nodeRelease represents a single release entry from https://nodejs.org/dist/index.json.
type nodeRelease struct {
	Version string `json:"version"` // e.g. "v20.11.0"
	LTS     any    `json:"lts"`     // false or string like "Iron"
}

// nodeLatestURL is the endpoint for Node.js release metadata.
var nodeLatestURL = "https://nodejs.org/dist/index.json"

// ResolveNodeLatest fetches the latest LTS Node.js version from nodejs.org.
func ResolveNodeLatest() (string, error) {
	body, err := httpGet(nodeLatestURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Node.js versions: %w", err)
	}

	var releases []nodeRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("failed to parse Node.js version JSON: %w", err)
	}

	for _, r := range releases {
		if isNodeLTS(r.LTS) {
			return strings.TrimPrefix(r.Version, "v"), nil
		}
	}

	return "", fmt.Errorf("no LTS Node.js version found")
}

// isNodeLTS returns true if the LTS field is a non-false value.
// The nodejs.org API returns false for non-LTS and a codename string for LTS.
func isNodeLTS(lts any) bool {
	switch v := lts.(type) {
	case bool:
		return v
	case string:
		return v != ""
	default:
		return false
	}
}

// validateNodeVersion checks if a specific Node.js version exists in the release list.
func validateNodeVersion(version string) (bool, error) {
	body, err := httpGet(nodeLatestURL)
	if err != nil {
		return false, fmt.Errorf("failed to fetch Node.js versions: %w", err)
	}

	var releases []nodeRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return false, fmt.Errorf("failed to parse Node.js version JSON: %w", err)
	}

	for _, r := range releases {
		v := strings.TrimPrefix(r.Version, "v")
		if v == version {
			return true, nil
		}
	}

	return false, nil
}

// --- Python version resolution ---

// pythonRelease represents a single release entry from https://endoflife.date/api/python.json.
type pythonRelease struct {
	Cycle  string `json:"cycle"`  // e.g. "3.12"
	Latest string `json:"latest"` // e.g. "3.12.1"
	EOL    any    `json:"eol"`    // false or date string "2028-10-31"
}

// pythonLatestURL is the endpoint for Python release metadata.
var pythonLatestURL = "https://endoflife.date/api/python.json"

// ResolvePythonLatest fetches the latest non-EOL Python version from endoflife.date.
func ResolvePythonLatest() (string, error) {
	body, err := httpGet(pythonLatestURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Python versions: %w", err)
	}

	var releases []pythonRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("failed to parse Python version JSON: %w", err)
	}

	for _, r := range releases {
		if !isPythonEOL(r.EOL) {
			return r.Latest, nil
		}
	}

	return "", fmt.Errorf("no active Python version found")
}

// isPythonEOL returns true if the EOL field indicates the release has reached end-of-life.
// The endoflife.date API returns false for active releases and a date string for EOL releases.
func isPythonEOL(eol any) bool {
	switch v := eol.(type) {
	case bool:
		return v
	case string:
		// A non-empty date string means a concrete EOL date has been set.
		// We parse it and check if it is in the past.
		if v == "" {
			return false
		}
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			// If we can't parse the date, treat as not EOL to be safe.
			return false
		}
		return time.Now().After(t)
	default:
		return false
	}
}

// validatePythonVersion checks if a specific Python version exists in the release list.
func validatePythonVersion(version string) (bool, error) {
	body, err := httpGet(pythonLatestURL)
	if err != nil {
		return false, fmt.Errorf("failed to fetch Python versions: %w", err)
	}

	var releases []pythonRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return false, fmt.Errorf("failed to parse Python version JSON: %w", err)
	}

	requestedCycle := version
	if parts := strings.Split(version, "."); len(parts) >= 2 {
		requestedCycle = strings.Join(parts[:2], ".")
	}
	for _, r := range releases {
		if r.Latest == version || r.Cycle == requestedCycle {
			return true, nil
		}
	}

	return false, nil
}

// --- npm registry resolution ---

// npmRegistryURL is the base URL for the npm registry.
var npmRegistryURL = "https://registry.npmjs.org"

// npmPackageResponse represents the subset of the npm registry response we need.
type npmPackageResponse struct {
	DistTags map[string]string          `json:"dist-tags"`
	Versions map[string]json.RawMessage `json:"versions"`
}

// NPMPackageMetadata is the subset of version-specific npm metadata used by
// implicit tooling compatibility checks.
type NPMPackageMetadata struct {
	Version string `json:"version"`
	Engines struct {
		Node string `json:"node"`
	} `json:"engines"`
}

// ResolveNPMPackageLatest fetches the latest version of an npm package from the registry.
func ResolveNPMPackageLatest(packageName string) (string, error) {
	url := npmRegistryURL + "/" + packageName
	body, err := httpGet(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch npm package %q: %w", packageName, err)
	}

	var pkg npmPackageResponse
	if err := json.Unmarshal(body, &pkg); err != nil {
		return "", fmt.Errorf("failed to parse npm registry response for %q: %w", packageName, err)
	}

	latest, ok := pkg.DistTags["latest"]
	if !ok || latest == "" {
		return "", fmt.Errorf("no 'latest' dist-tag found for npm package %q", packageName)
	}

	return latest, nil
}

// ResolveNPMPackageMetadata fetches version-specific npm metadata.
func ResolveNPMPackageMetadata(packageName, version string) (NPMPackageMetadata, error) {
	url := npmRegistryURL + "/" + packageName + "/" + version
	body, err := httpGet(url)
	if err != nil {
		return NPMPackageMetadata{}, fmt.Errorf("failed to fetch npm package %q version %q: %w", packageName, version, err)
	}

	var meta NPMPackageMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return NPMPackageMetadata{}, fmt.Errorf("failed to parse npm metadata for %q version %q: %w", packageName, version, err)
	}

	return meta, nil
}

// validateNPMVersion checks if a specific version exists for an npm package.
func validateNPMVersion(packageName, version string) (bool, error) {
	url := npmRegistryURL + "/" + packageName
	body, err := httpGet(url)
	if err != nil {
		return false, fmt.Errorf("failed to fetch npm package %q: %w", packageName, err)
	}

	var pkg npmPackageResponse
	if err := json.Unmarshal(body, &pkg); err != nil {
		return false, fmt.Errorf("failed to parse npm registry response for %q: %w", packageName, err)
	}

	_, exists := pkg.Versions[version]
	return exists, nil
}

// --- HTTP helper ---

// httpGet performs a GET request and returns the response body.
// It returns a clear error on network failure so the user can retry.
func httpGet(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed for %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from %s: %w", url, err)
	}

	return body, nil
}
