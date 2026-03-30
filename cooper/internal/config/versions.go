package config

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// VersionMode controls how a tool's version is resolved.
type VersionMode int

const (
	// ModeOff means the tool is not included in the Dockerfile.
	ModeOff VersionMode = iota
	// ModeMirror means mirror the version from the host machine.
	ModeMirror
	// ModeLatest means use the latest available version.
	ModeLatest
	// ModePin means use a user-specified pinned version.
	ModePin
)

// versionModeStrings maps VersionMode values to their JSON string representation.
var versionModeStrings = map[VersionMode]string{
	ModeOff:    "off",
	ModeMirror: "mirror",
	ModeLatest: "latest",
	ModePin:    "pin",
}

// versionModeFromString maps JSON string representation to VersionMode values.
var versionModeFromString = map[string]VersionMode{
	"off":    ModeOff,
	"mirror": ModeMirror,
	"latest": ModeLatest,
	"pin":    ModePin,
}

// String returns the string representation of a VersionMode.
func (m VersionMode) String() string {
	if s, ok := versionModeStrings[m]; ok {
		return s
	}
	return "unknown"
}

// MarshalJSON implements json.Marshaler for VersionMode.
// Stores as a JSON string: "off", "mirror", "latest", "pin".
func (m VersionMode) MarshalJSON() ([]byte, error) {
	s, ok := versionModeStrings[m]
	if !ok {
		return nil, fmt.Errorf("unknown VersionMode: %d", m)
	}
	return json.Marshal(s)
}

// UnmarshalJSON implements json.Unmarshaler for VersionMode.
func (m *VersionMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("VersionMode must be a string: %w", err)
	}
	mode, ok := versionModeFromString[s]
	if !ok {
		return fmt.Errorf("unknown VersionMode string: %q", s)
	}
	*m = mode
	return nil
}

// ToolConfig describes a programming tool or AI CLI tool and its
// version management settings.
type ToolConfig struct {
	Name             string      `json:"name"`
	Enabled          bool        `json:"enabled"`
	Mode             VersionMode `json:"mode"`
	PinnedVersion    string      `json:"pinned_version,omitempty"`
	HostVersion      string      `json:"host_version,omitempty"`
	ContainerVersion string      `json:"container_version,omitempty"`
	InstallCmd       string      `json:"install_cmd,omitempty"`
}

// RefreshContainerVersion sets ContainerVersion based on the version mode
// to reflect what was built into the container image.
//   - Mirror: uses HostVersion (the version mirrored from the host)
//   - Pin: uses PinnedVersion (the explicitly pinned version)
//   - Latest: uses PinnedVersion if set (resolved by resolveLatestVersions),
//     falls back to HostVersion
//   - Off/disabled: no-op
func (t *ToolConfig) RefreshContainerVersion() {
	if !t.Enabled {
		return
	}
	switch t.Mode {
	case ModeMirror:
		t.ContainerVersion = t.HostVersion
	case ModePin:
		t.ContainerVersion = t.PinnedVersion
	case ModeLatest:
		if t.PinnedVersion != "" {
			t.ContainerVersion = t.PinnedVersion
		} else if t.HostVersion != "" {
			t.ContainerVersion = t.HostVersion
		}
	}
}

// DomainEntry represents a whitelisted domain in the proxy configuration.
type DomainEntry struct {
	Domain            string `json:"domain"`
	IncludeSubdomains bool   `json:"include_subdomains"`
	Source            string `json:"source"` // "default" or "user"
}

// PortForwardRule describes a port forwarding rule from the CLI container
// to the host machine via the proxy container's socat relay.
type PortForwardRule struct {
	ContainerPort int    `json:"container_port"`
	HostPort      int    `json:"host_port"`
	Description   string `json:"description"`
	IsRange       bool   `json:"is_range"`
	RangeEnd      int    `json:"range_end,omitempty"`
}

// BridgeRoute maps an API path to a host script for the execution bridge.
type BridgeRoute struct {
	APIPath    string `json:"api_path"`
	ScriptPath string `json:"script_path"`
}

// VersionStatus indicates the result of a version comparison.
type VersionStatus int

const (
	// VersionMatch means the container version matches the expected version.
	VersionMatch VersionStatus = iota
	// VersionMismatch means the container version does not match the expected version.
	VersionMismatch
	// VersionUnknown means the version could not be determined.
	VersionUnknown
)

// String returns a human-readable string for VersionStatus.
func (s VersionStatus) String() string {
	switch s {
	case VersionMatch:
		return "match"
	case VersionMismatch:
		return "mismatch"
	case VersionUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// semverRegex matches semantic version strings (e.g., "1.22.5", "20.11.0", "3.12.1").
var semverRegex = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)

// toolVersionCommands maps tool names to the command and args used to detect
// the host version.
var toolVersionCommands = map[string][]string{
	"go":      {"go", "version"},
	"node":    {"node", "--version"},
	"python":  {"python3", "--version"},
	"rust":    {"rustc", "--version"},
	"claude":  {"claude", "--version"},
	"copilot": {"copilot", "--version"},
	"codex":   {"codex", "--version"},
	"opencode": {"opencode", "--version"},
}

// execCommand is a package-level variable to allow test mocking.
var execCommand = exec.Command

// DetectHostVersion runs the appropriate command to detect the installed
// version of a tool on the host machine. Returns the parsed semver string.
func DetectHostVersion(toolName string) (string, error) {
	args, ok := toolVersionCommands[toolName]
	if !ok {
		return "", fmt.Errorf("unknown tool: %q", toolName)
	}

	cmd := execCommand(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to detect %s version: %w", toolName, err)
	}

	return parseVersion(string(output))
}

// parseVersion extracts a semver-like version string from command output.
func parseVersion(output string) (string, error) {
	match := semverRegex.FindString(strings.TrimSpace(output))
	if match == "" {
		return "", fmt.Errorf("no version found in output: %q", strings.TrimSpace(output))
	}
	return match, nil
}

// CompareVersions compares a container version against an expected version
// using the given VersionMode.
//
//   - ModeOff: always returns VersionMatch (tool is disabled, nothing to compare).
//   - ModeMirror: compares container version against expected (host) version.
//   - ModePin: compares container version against expected (pinned) version.
//   - ModeLatest: compares container version against expected (resolved latest) version.
func CompareVersions(container, expected string, mode VersionMode) VersionStatus {
	switch mode {
	case ModeOff:
		return VersionMatch

	case ModeMirror, ModePin:
		if container == "" || expected == "" {
			return VersionUnknown
		}
		if container == expected {
			return VersionMatch
		}
		return VersionMismatch

	case ModeLatest:
		// Compare container version against expected (the resolved latest).
		// If expected is empty, the caller hasn't resolved yet — return Unknown.
		if container == "" || expected == "" {
			return VersionUnknown
		}
		if container == expected {
			return VersionMatch
		}
		return VersionMismatch

	default:
		return VersionUnknown
	}
}
