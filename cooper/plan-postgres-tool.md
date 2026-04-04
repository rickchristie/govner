# Plan: Add PostgreSQL as a Programming Tool

## Handoff Note

This document is meant to be handed to another AI session for implementation.
It is intentionally explicit. Follow the decisions here exactly unless the
codebase proves one of them impossible. If that happens, update this plan in
the implementation PR/patch notes and explain the deviation.

The goal is not just "install PostgreSQL". The goal is:

- PostgreSQL appears as a first-class programming tool in `cooper configure`
- version handling is correct for a major-only tool
- barrel startup yields a working local PostgreSQL service
- existing Cooper behavior around forwarded `5432` is not broken
- tests verify real connectivity, not just `psql --version`

## Non-Negotiable Decisions

These are the decisions that must not drift during implementation:

1. Cooper-managed PostgreSQL binds `127.0.0.1:15432`, not `5432`.
2. PostgreSQL version semantics are `major only`.
3. Programming-tool rules come from shared metadata, not new scattered
   PostgreSQL-specific conditionals.
4. `cooper configure` save must still render valid templates when PostgreSQL is
   in `ModeLatest` and no major has been resolved yet.
5. The entrypoint must use a stable PostgreSQL bin path and must not depend on
   a templated `/usr/lib/postgresql/<major>/bin` path.
6. Entry-point startup must fail clearly if PostgreSQL is enabled but does not
   become ready.
7. Verification must include `pg_isready` and `psql -c 'select 1'`.

## Assumption Verification

The previous writer already verified the assumptions below while analyzing the
current Cooper codebase and the existing PostgreSQL tool draft.

Treat these as verified inputs for implementation, not open questions to
reinterpret mid-way through the change.

All 10 assumptions below have already been verified.

### Verified Assumptions

1. Cooper commonly uses forwarded container port `5432` already.
   This is present in the shipped test fixtures and end-to-end flows, so a
   Cooper-managed PostgreSQL service cannot safely claim `127.0.0.1:5432`
   without breaking existing behavior.

2. The current barrel entrypoint starts port forwarding before other runtime
   services.
   That ordering exists today and should be preserved. The fix is to move
   PostgreSQL to `15432`, not to redesign the startup sequence around `5432`.

3. `cooper configure` save renders templates before `cooper build` resolves
   `ModeLatest` to a concrete version.
   Therefore the PostgreSQL template path must work when no concrete major has
   been resolved yet.

4. The current template/render path already supports "empty concrete version"
   semantics for some tools.
   Using an empty PostgreSQL version to mean "install generic latest package"
   is compatible with the existing render architecture.

5. The current configure UI uses a single shared text input placeholder for all
   programming tools.
   PostgreSQL needs tool-specific placeholder/help text, so the input widget
   must support placeholder mutation instead of introducing a PostgreSQL-only UI
   branch.

6. `cooper proof` currently verifies programming tools mostly as binaries, not
   as services.
   PostgreSQL is different: a correct implementation must prove both binary
   presence and live connectivity.

7. PostgreSQL host detection via `psql --version` is sufficient for Cooper's
   needs.
   Cooper only needs the installed host major for mirror-mode comparison, not
   cluster introspection or server process inspection on the host.

8. PostgreSQL package installs on Debian/PGDG are major-oriented.
   Cooper should store and compare PostgreSQL versions as major-only values,
   not exact patch versions.

9. Persistent PostgreSQL storage is not required for this feature.
   Ephemeral data under `/home/user/pgdata` matches the intended "scratch
   database inside the barrel" use case for this change.

10. The implementor should still verify behavior after coding, but should not
    reopen these architecture decisions unless the codebase contradicts them.
    If one of these assumptions proves false in implementation, document the
    contradiction and update the plan/patch notes explicitly.

## Problem Statement

Cooper currently treats programming tools like simple binary installs. That is
fine for Go, Node, and Python, but PostgreSQL is different:

- it has a client and a server
- its install packaging is major-version-oriented
- it needs startup lifecycle management
- it can collide with Cooper's existing port-forwarding model

The previous draft had three material flaws:

1. it tried to bind local PostgreSQL to `5432`, which conflicts with common
   forwarded-port rules
2. it made the entrypoint depend on a concrete PostgreSQL major
3. it only verified install-time presence, not runtime usability

This plan fixes those issues.

## Scope

In scope:

- PostgreSQL as a built-in programming tool
- host detection via `psql --version`
- major-only pin/mirror/latest behavior
- Cooper-managed local PostgreSQL service inside the barrel
- config validation for PostgreSQL-specific rules
- `cooper configure`, `cooper proof`, Docker templates, fixture updates
- unit tests plus runtime integration checks

Out of scope:

- persistent PostgreSQL data across barrel recreation
- user-configurable PostgreSQL local port
- database authentication beyond local `trust`
- supporting multiple PostgreSQL majors in the same image
- changing AI tool metadata architecture

## High-Level Design

### PostgreSQL Service Model

When PostgreSQL is enabled as a programming tool:

- the base image installs PostgreSQL server + client
- the entrypoint initializes a cluster under `/home/user/pgdata` on first run
- the entrypoint starts PostgreSQL on `127.0.0.1:15432`
- the image exports:
  - `PGHOST=127.0.0.1`
  - `PGPORT=15432`
  - `PGUSER=user`
  - `PGDATABASE=postgres`
  - `DATABASE_URL=postgresql://user@127.0.0.1:15432/postgres?sslmode=disable`

This makes bare `psql`, migration tools, and app defaults work without extra
flags.

### Version Model

PostgreSQL uses major-only version semantics:

| Field | Example | Meaning |
|---|---|---|
| `host_version` | `"17"` | major extracted from `psql --version` |
| `pinned_version` | `"16"` | user enters major only |
| `container_version` | `"17"` | what the Cooper image built |
| resolved latest | `"18"` | latest supported current major |

PostgreSQL is the first built-in programming tool with `major-only` semantics.
That rule must live in shared tool metadata.

### Port Model

Cooper already forwards user-requested container ports via `socat`. Many
existing configs forward `5432` to the host. Therefore:

- do not bind Cooper-managed PostgreSQL to `5432`
- reserve `15432` for Cooper-managed local PostgreSQL
- reject config that tries to use `15432` as a forwarded container port when
  PostgreSQL is enabled

## Implementation Order

Implement in this order to minimize churn and avoid partial broken states:

1. Add shared programming-tool metadata in `internal/config/tooldefs.go`
2. Update detection/normalization/validation in `internal/config`
3. Update `internal/app/configure.go` to consume metadata
4. Update `internal/configure/textinput.go` and
   `internal/configure/programming.go` for per-tool placeholder/help
5. Update `internal/proof/proof.go`
6. Update template data structs and render helpers
7. Update `base.Dockerfile.tmpl`
8. Update `entrypoint.sh.tmpl`
9. Update fixtures
10. Add/adjust unit tests
11. Add runtime checks in `test-docker-build.sh`
12. Add runtime checks in `test-e2e.sh`
13. Run verification commands from the final section of this plan

Do not start with the Dockerfile or entrypoint. The metadata and validation
layer should exist first, otherwise the UI and config behavior will drift.

## Files To Modify

| # | File | Change |
|---|---|---|
| 1 | `internal/config/tooldefs.go` | New shared programming-tool metadata registry |
| 2 | `internal/config/tooldefs_test.go` | New tests for metadata helpers and format validation |
| 3 | `internal/config/versions.go` | Use shared metadata for detect command and normalization |
| 4 | `internal/config/resolve.go` | Add PostgreSQL latest/version validation with numeric fallback |
| 5 | `internal/config/config.go` | Validate version shape; reserve PostgreSQL local port |
| 6 | `internal/config/config_test.go` | Add PostgreSQL detect and config-validation tests |
| 7 | `internal/config/resolve_test.go` | Add PostgreSQL resolution/validation tests |
| 8 | `internal/app/configure.go` | Replace hardcoded programming tool list with metadata |
| 9 | `internal/app/configure_test.go` | Expect PostgreSQL in host tool detection/listing |
| 10 | `internal/configure/textinput.go` | Add `SetPlaceholder` helper |
| 11 | `internal/configure/programming.go` | Build from metadata; show per-tool pin help/placeholder |
| 12 | `internal/configure/configure_test.go` | Add PostgreSQL UI expectations |
| 13 | `internal/proof/proof.go` | PostgreSQL-aware proof command and connectivity proof |
| 14 | `internal/templates/templates.go` | Add PostgreSQL fields to render data |
| 15 | `internal/templates/base.Dockerfile.tmpl` | Install PostgreSQL, stable bin symlink, PG envs |
| 16 | `internal/templates/entrypoint.sh.tmpl` | Add `start_postgres()` helper and startup call |
| 17 | `internal/templates/templates_test.go` | Add PostgreSQL rendering tests |
| 18 | `.testfiles/config-pinned.json` | Add PostgreSQL programming tool |
| 19 | `.testfiles/config-latest.json` | Add PostgreSQL programming tool |
| 20 | `.testfiles/config-mirror.json` | Add PostgreSQL programming tool |
| 21 | `test-docker-build.sh` | Add install checks and runtime PostgreSQL checks |
| 22 | `test-e2e.sh` | Add local PostgreSQL runtime checks and coexistence check |

## Step 1: Add Shared Programming-Tool Metadata

Add a new file: `internal/config/tooldefs.go`

This file becomes the single source of truth for built-in programming tools.
Do not leave a separate hardcoded programming-tool registry in
`internal/app/configure.go` or `internal/configure/programming.go`.

### Exact Types To Add

```go
package config

import (
	"fmt"
	"regexp"
	"strings"
)

type VersionShape int

const (
	VersionShapeExact VersionShape = iota
	VersionShapeMajorOnly
)

const PostgreSQLLocalPort = 15432

type ProgrammingToolSpec struct {
	Name           string
	DisplayName    string
	VersionShape   VersionShape
	PinPlaceholder string
	PinHelp        string
	DetectCommand  []string
	ProofCommand   string
	LocalPort      int
}

var semverLikeVersionRE = regexp.MustCompile(`^\d+\.\d+(?:\.\d+)?$`)
var majorOnlyVersionRE = regexp.MustCompile(`^\d+$`)
```

### Exact Built-In Programming Tool List

Use a lowercase-name keyed registry.

```go
var programmingToolSpecs = []ProgrammingToolSpec{
	{
		Name:           "go",
		DisplayName:    "Go",
		VersionShape:   VersionShapeExact,
		PinPlaceholder: "e.g., 1.24.10",
		PinHelp:        "Specify an exact Go version.",
		DetectCommand:  []string{"go", "version"},
		ProofCommand:   "go version",
	},
	{
		Name:           "node",
		DisplayName:    "Node.js",
		VersionShape:   VersionShapeExact,
		PinPlaceholder: "e.g., 22.12.0",
		PinHelp:        "Specify an exact Node.js version.",
		DetectCommand:  []string{"node", "--version"},
		ProofCommand:   "node --version",
	},
	{
		Name:           "python",
		DisplayName:    "Python",
		VersionShape:   VersionShapeExact,
		PinPlaceholder: "e.g., 3.12.1",
		PinHelp:        "Specify an exact Python version.",
		DetectCommand:  []string{"python3", "--version"},
		ProofCommand:   "python3 --version 2>/dev/null || python --version",
	},
	{
		Name:           "postgresql",
		DisplayName:    "PostgreSQL",
		VersionShape:   VersionShapeMajorOnly,
		PinPlaceholder: "e.g., 17",
		PinHelp:        "Specify a supported PostgreSQL major version.",
		DetectCommand:  []string{"psql", "--version"},
		ProofCommand:   "psql --version",
		LocalPort:      PostgreSQLLocalPort,
	},
}
```

### Exact Helper Functions To Add

```go
func ProgrammingToolSpecs() []ProgrammingToolSpec {
	return append([]ProgrammingToolSpec(nil), programmingToolSpecs...)
}

func ProgrammingToolSpecByName(name string) (ProgrammingToolSpec, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, spec := range programmingToolSpecs {
		if spec.Name == name {
			return spec, true
		}
	}
	return ProgrammingToolSpec{}, false
}

func NormalizeDetectedVersion(toolName, version string) string {
	spec, ok := ProgrammingToolSpecByName(toolName)
	if !ok {
		return version
	}
	switch spec.VersionShape {
	case VersionShapeMajorOnly:
		if i := strings.Index(version, "."); i >= 0 {
			return version[:i]
		}
	}
	return version
}

func ValidatePinnedVersionFormat(toolName, version string) error {
	spec, ok := ProgrammingToolSpecByName(toolName)
	if !ok {
		return nil
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("%s pinned version cannot be empty", spec.DisplayName)
	}
	switch spec.VersionShape {
	case VersionShapeMajorOnly:
		if !majorOnlyVersionRE.MatchString(version) {
			return fmt.Errorf("%s requires a major version only (example: 17)", spec.DisplayName)
		}
	case VersionShapeExact:
		if !semverLikeVersionRE.MatchString(version) {
			return fmt.Errorf("%s requires an exact version (example: %s)", spec.DisplayName, spec.PinPlaceholder)
		}
	}
	return nil
}
```

### Notes

- `ProgrammingToolSpecs()` must return a copy, not the backing slice.
- `ValidatePinnedVersionFormat()` is for built-in programming tools only.
- Unknown tool names should return `nil` from `ValidatePinnedVersionFormat()` so
  future custom tools are not blocked accidentally.

## Step 2: Add Metadata Tests

Add `internal/config/tooldefs_test.go`.

It should test:

- `ProgrammingToolSpecByName("postgresql")` returns the expected metadata
- `NormalizeDetectedVersion("postgresql", "17.2") == "17"`
- `NormalizeDetectedVersion("go", "1.24.10") == "1.24.10"`
- `ValidatePinnedVersionFormat("postgresql", "17")` succeeds
- `ValidatePinnedVersionFormat("postgresql", "17.2")` fails
- `ValidatePinnedVersionFormat("go", "1.24.10")` succeeds
- `ValidatePinnedVersionFormat("go", "17")` fails

Recommended test shape:

```go
func TestValidatePinnedVersionFormat_PostgreSQLMajorOnly(t *testing.T) {
	if err := ValidatePinnedVersionFormat("postgresql", "17"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidatePinnedVersionFormat("postgresql", "17.2"); err == nil {
		t.Fatal("expected error for 17.2")
	}
}
```

## Step 3: Update Host Version Detection

Modify `internal/config/versions.go`.

### Replace Programming Tool Command Duplication

Do not keep PostgreSQL in a second standalone `toolVersionCommands` map.

The current file can keep AI CLI detect commands as a small local map, but
programming-tool detect commands should come from `ProgrammingToolSpec`.

### Recommended Structure

Keep AI tool commands:

```go
var aiToolVersionCommands = map[string][]string{
	"claude":   {"claude", "--version"},
	"copilot":  {"copilot", "--version"},
	"codex":    {"codex", "--version"},
	"opencode": {"opencode", "--version"},
}
```

Add a helper:

```go
func versionCommand(toolName string) ([]string, bool) {
	if spec, ok := ProgrammingToolSpecByName(toolName); ok {
		return spec.DetectCommand, true
	}
	args, ok := aiToolVersionCommands[toolName]
	return args, ok
}
```

Then rewrite `DetectHostVersion()` to use it:

```go
func DetectHostVersion(toolName string) (string, error) {
	args, ok := versionCommand(toolName)
	if !ok {
		return "", fmt.Errorf("unknown tool: %q", toolName)
	}

	cmd := execCommand(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to detect %s version: %w", toolName, err)
	}

	version, err := parseVersion(string(output))
	if err != nil {
		return "", err
	}

	return NormalizeDetectedVersion(toolName, version), nil
}
```

### DetectHostVersion Test To Add

Add to `internal/config/config_test.go`:

```go
func TestDetectHostVersionPostgreSQL(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", "psql (PostgreSQL) 17.2")
	}

	version, err := DetectHostVersion("postgresql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "17" {
		t.Errorf("got %q, want \"17\"", version)
	}
}
```

## Step 4: Add PostgreSQL Latest Resolution

Modify `internal/config/resolve.go`.

### Add Types And URL

```go
var postgresqlLatestURL = "https://www.postgresql.org/versions.json"

type postgresqlRelease struct {
	Major       string `json:"major"`
	LatestMinor string `json:"latestMinor"`
	Supported   bool   `json:"supported"`
	Current     bool   `json:"current"`
}
```

### Add Resolver

Use `current && supported` if present. If not, choose the numerically highest
supported major.

```go
func ResolvePostgreSQLLatest() (string, error) {
	body, err := httpGet(postgresqlLatestURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch PostgreSQL versions: %w", err)
	}

	var releases []postgresqlRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("failed to parse PostgreSQL version JSON: %w", err)
	}

	for _, r := range releases {
		if r.Supported && r.Current {
			return r.Major, nil
		}
	}

	best := ""
	bestMajor := -1
	for _, r := range releases {
		if !r.Supported {
			continue
		}
		major, err := strconv.Atoi(r.Major)
		if err != nil {
			continue
		}
		if major > bestMajor {
			bestMajor = major
			best = r.Major
		}
	}
	if best != "" {
		return best, nil
	}

	return "", fmt.Errorf("no supported PostgreSQL version found")
}
```

### Add Validator

Validate local format first, then remote support:

```go
func validatePostgreSQLVersion(version string) (bool, error) {
	if err := ValidatePinnedVersionFormat("postgresql", version); err != nil {
		return false, err
	}

	body, err := httpGet(postgresqlLatestURL)
	if err != nil {
		return false, fmt.Errorf("failed to fetch PostgreSQL versions: %w", err)
	}

	var releases []postgresqlRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return false, fmt.Errorf("failed to parse PostgreSQL version JSON: %w", err)
	}

	for _, r := range releases {
		if r.Major == version && r.Supported {
			return true, nil
		}
	}
	return false, nil
}
```

### Wire Dispatchers

Add PostgreSQL cases to:

- `ResolveLatestVersion()`
- `ValidateVersion()`

Exact pattern:

```go
case "postgresql":
	return ResolvePostgreSQLLatest()
```

and

```go
case "postgresql":
	return validatePostgreSQLVersion(version)
```

### Resolve Tests To Add

Add to `internal/config/resolve_test.go`:

- `TestResolvePostgreSQLLatest`
- `TestResolvePostgreSQLLatestFallbackHighestSupported`
- `TestResolvePostgreSQLLatestInvalidJSON`
- `TestValidatePostgreSQLVersion`
- `TestResolveLatestVersionDispatch_PostgreSQL`

Important: include one fallback test where JSON order is unsorted, e.g. `17`,
`15`, `16`, so the implementation must compare numerically.

Example:

```go
func TestResolvePostgreSQLLatestFallbackHighestSupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"major":"15","supported":true,"current":false},
			{"major":"17","supported":true,"current":false},
			{"major":"16","supported":true,"current":false}
		]`)
	}))
	defer server.Close()

	origURL := postgresqlLatestURL
	postgresqlLatestURL = server.URL
	defer func() { postgresqlLatestURL = origURL }()

	v, err := ResolvePostgreSQLLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "17" {
		t.Errorf("got %q, want \"17\"", v)
	}
}
```

## Step 5: Validate Config For PostgreSQL Rules

Modify `internal/config/config.go`.

### Add Programming Tool Validation Helper

Add a helper called near the end of `Validate()`:

```go
func (c *Config) validateProgrammingToolRules() error {
	postgresEnabled := false

	for _, tool := range c.ProgrammingTools {
		if !tool.Enabled {
			continue
		}

		spec, ok := ProgrammingToolSpecByName(tool.Name)
		if !ok {
			continue
		}

		switch tool.Mode {
		case ModePin:
			if err := ValidatePinnedVersionFormat(tool.Name, tool.PinnedVersion); err != nil {
				return err
			}
		case ModeMirror:
			if strings.TrimSpace(tool.HostVersion) != "" {
				if err := ValidatePinnedVersionFormat(tool.Name, tool.HostVersion); err != nil {
					return fmt.Errorf("%s host version is invalid: %w", spec.DisplayName, err)
				}
			}
		}

		if spec.Name == "postgresql" {
			postgresEnabled = true
		}
	}

	if !postgresEnabled {
		return nil
	}

	if c.BridgePort == PostgreSQLLocalPort {
		return fmt.Errorf("bridge port (%d) conflicts with Cooper-managed PostgreSQL local port", PostgreSQLLocalPort)
	}

	for _, rule := range c.PortForwardRules {
		if rule.IsRange {
			for port := rule.ContainerPort; port <= rule.RangeEnd; port++ {
				if port == PostgreSQLLocalPort {
					return fmt.Errorf("port forward rule %q: container port %d conflicts with Cooper-managed PostgreSQL local port",
						rule.Description, port)
				}
			}
			continue
		}
		if rule.ContainerPort == PostgreSQLLocalPort {
			return fmt.Errorf("port forward rule %q: container port %d conflicts with Cooper-managed PostgreSQL local port",
				rule.Description, rule.ContainerPort)
		}
	}

	return nil
}
```

Then call it from `Validate()`:

```go
if err := c.validateProgrammingToolRules(); err != nil {
	return err
}
```

### Notes

- do not reserve `PostgreSQLLocalPort` against `ProxyPort`; that proxy port is
  not bound inside the barrel
- do reserve it against `BridgePort` and forwarded container ports, because
  those bind inside the barrel

### Config Tests To Add

Add to `internal/config/config_test.go`:

- PostgreSQL pin `17` validates
- PostgreSQL pin `17.2` fails
- PostgreSQL enabled + `BridgePort=15432` fails
- PostgreSQL enabled + forward rule `container_port=15432` fails
- PostgreSQL enabled + range covering `15432` fails

Example:

```go
func TestValidatePostgreSQLBridgePortConflict(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BridgePort = PostgreSQLLocalPort
	cfg.ProgrammingTools = []ToolConfig{
		{Name: "postgresql", Enabled: true, Mode: ModePin, PinnedVersion: "17"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}
```

## Step 6: Replace Hardcoded Programming-Tool Lists In App Layer

Modify `internal/app/configure.go`.

### Remove Hardcoded `programmingToolDefs`

Delete the current hardcoded slice:

```go
var programmingToolDefs = []struct {
	name        string
	displayName string
}{ ... }
```

### Replace `DetectHostTools()`

Implement directly from `config.ProgrammingToolSpecs()`:

```go
func (a *ConfigureApp) DetectHostTools() []config.ToolConfig {
	specs := config.ProgrammingToolSpecs()
	result := make([]config.ToolConfig, len(specs))
	for i, spec := range specs {
		tc := config.ToolConfig{Name: spec.Name}
		v, err := config.DetectHostVersion(spec.Name)
		if err == nil && v != "" {
			tc.HostVersion = v
			tc.Enabled = true
			tc.Mode = config.ModeMirror
		}
		result[i] = tc
	}
	return result
}
```

Do not keep a separate helper that reconstructs programming tool names.

### App Test Update

Update `internal/app/configure_test.go` expectations so
`DetectHostTools()` includes:

- `go`
- `node`
- `python`
- `postgresql`

## Step 7: Support Per-Tool Pin Placeholder In TUI

### 7a. Modify `internal/configure/textinput.go`

Add:

```go
func (t *textInput) SetPlaceholder(s string) {
	t.placeholder = s
}
```

This is needed because PostgreSQL pin mode must show `e.g., 17` instead of the
current global placeholder.

### 7b. Modify `internal/configure/programming.go`

Replace the static `defaultProgrammingTools` slice with entries built from
metadata.

#### Update `toolEntry`

Add the spec to each entry:

```go
type toolEntry struct {
	spec             config.ProgrammingToolSpec
	name             string
	displayName      string
	enabled          bool
	mode             config.VersionMode
	hostVersion      string
	pinVersion       string
	containerVersion string
}
```

#### Add builder

```go
func defaultProgrammingToolEntries() []toolEntry {
	specs := config.ProgrammingToolSpecs()
	tools := make([]toolEntry, len(specs))
	for i, spec := range specs {
		tools[i] = toolEntry{
			spec:        spec,
			name:        spec.Name,
			displayName: spec.DisplayName,
		}
	}
	return tools
}
```

Use it from `newProgrammingModel()` instead of copying
`defaultProgrammingTools`.

#### Set placeholder on detail entry

In the `"enter"` handling path where the detail view opens, add:

```go
m.pinInput.SetPlaceholder(m.tools[m.cursor].spec.PinPlaceholder)
```

Also initialize the shared input with a sane default:

```go
pinInput: newTextInput("e.g., 1.24.10", 30),
```

#### Add pin help text to detail view

In the `Pin` mode rendering block, show tool-specific help text before the
input box:

```go
if mode.mode == config.ModePin {
	pinMargin := 11
	helpIndent := lipgloss.NewStyle().MarginLeft(pinMargin).Foreground(theme.ColorDusty)
	inner += "\n" + helpIndent.Render(t.spec.PinHelp)
	inner += "\n" + m.pinInput.viewWithMargin(pinMargin)
	if m.pinError != "" {
		errIndent := lipgloss.NewStyle().MarginLeft(pinMargin)
		inner += "\n" + errIndent.Render(lipgloss.NewStyle().Foreground(theme.ColorFlame).Render(m.pinError))
	}
}
```

#### Leave validation call as generic

Do not add a PostgreSQL-only branch in the TUI. This existing pattern stays:

```go
valid, err := config.ValidateVersion(tool.name, v)
```

The tool-specific format and remote validation now happen inside `config`.

### TUI Tests To Update

In `internal/configure/configure_test.go`, update/add tests for:

- PostgreSQL appears in the programming tool list
- opening PostgreSQL detail sets placeholder to `e.g., 17`
- PostgreSQL pin help text is rendered
- invalid PostgreSQL pin like `17.2` produces an error

If the current test structure makes render-string assertions awkward, it is
acceptable to assert the model state and the presence of the help string in the
detail view output.

## Step 8: Update `cooper proof`

Modify `internal/proof/proof.go`.

### Use Metadata For Programming Tool Version Command

Instead of the current hardcoded map plus `t.Name + " --version"` fallback for
unknown programming tools, use the metadata when available:

```go
for _, t := range ctx.Cfg.ProgrammingTools {
	if !t.Enabled {
		continue
	}
	cmd := t.Name + " --version"
	if spec, ok := config.ProgrammingToolSpecByName(t.Name); ok && spec.ProofCommand != "" {
		cmd = spec.ProofCommand
	}
	out, err := dockerExec(barrel, cmd)
	if err == nil && out != "" {
		ctx.pass(t.Name, truncate(out, 80))
	} else {
		ctx.fail(t.Name, "not found in container")
	}
}
```

### Add PostgreSQL Connectivity Proof

After programming tool version checks, add a dedicated runtime check if
PostgreSQL is enabled:

```go
postgresEnabled := false
for _, t := range ctx.Cfg.ProgrammingTools {
	if t.Enabled && strings.EqualFold(t.Name, "postgresql") {
		postgresEnabled = true
		break
	}
}

if postgresEnabled {
	out, err := dockerExec(barrel, `pg_isready -h 127.0.0.1 -p 15432 -U user`)
	if err == nil && strings.Contains(out, "accepting connections") {
		ctx.pass("postgresql-ready", truncate(out, 80))
	} else {
		ctx.fail("postgresql-ready", truncate(out, 80))
	}

	out, err = dockerExec(barrel, `PGCONNECT_TIMEOUT=2 psql -c "select 1"`)
	if err == nil && strings.Contains(out, "1 row") {
		ctx.pass("postgresql-query", "select 1 succeeded")
	} else {
		ctx.fail("postgresql-query", truncate(out, 80))
	}
}
```

Do not rely on `psql --version` alone in `cooper proof`.

## Step 9: Update Template Data Plumbing

Modify `internal/templates/templates.go`.

### Exact Struct Changes

Add to `baseDockerfileData`:

```go
HasPostgreSQL     bool
PostgreSQLVersion string
PostgreSQLPort    int
```

Add to `entrypointData`:

```go
HasPostgreSQL  bool
PostgreSQLPort int
```

Do not add `PostgreSQLVersion` to `entrypointData`. The entrypoint must not
need it.

### Build Data Helpers

Update `buildBaseDockerfileData()`:

```go
func buildBaseDockerfileData(cfg *config.Config) baseDockerfileData {
	return baseDockerfileData{
		HasGo:             isToolEnabled(cfg.ProgrammingTools, "go"),
		GoVersion:         getToolVersion(cfg.ProgrammingTools, "go"),
		HasNode:           isToolEnabled(cfg.ProgrammingTools, "node"),
		NodeVersion:       getToolVersion(cfg.ProgrammingTools, "node"),
		HasPython:         isToolEnabled(cfg.ProgrammingTools, "python"),
		HasPostgreSQL:     isToolEnabled(cfg.ProgrammingTools, "postgresql"),
		PostgreSQLVersion: getToolVersion(cfg.ProgrammingTools, "postgresql"),
		PostgreSQLPort:    config.PostgreSQLLocalPort,
		HasCodex:          isToolEnabled(cfg.AITools, "codex"),
		HasOpenCode:       isToolEnabled(cfg.AITools, "opencode"),
		ProxyPort:         cfg.ProxyPort,
	}
}
```

Update `RenderEntrypoint()`:

```go
data := entrypointData{
	HasGo:            isToolEnabled(cfg.ProgrammingTools, "go"),
	HasPostgreSQL:    isToolEnabled(cfg.ProgrammingTools, "postgresql"),
	PostgreSQLPort:   config.PostgreSQLLocalPort,
	BridgePort:       cfg.BridgePort,
	ClipboardEnabled: anyAIToolEnabled(cfg.AITools),
}
```

### Important Semantics

`getToolVersion(cfg.ProgrammingTools, "postgresql")` is intentionally allowed
to return `""` for unresolved `ModeLatest`.

That must mean:

- Dockerfile installs generic `postgresql` packages
- entrypoint still works because it uses a stable symlink

## Step 10: Update `base.Dockerfile.tmpl`

Modify `internal/templates/base.Dockerfile.tmpl`.

### PostgreSQL Install Block

Insert after the Python block and before the Codex bubblewrap block.

Use exactly this behavior:

- add PGDG apt repo
- disable auto cluster creation
- install versioned packages only when `.PostgreSQLVersion` is non-empty
- otherwise install generic `postgresql` and `postgresql-client`
- create a stable symlink to the installed bin dir

Recommended template block:

```dockerfile
{{- if .HasPostgreSQL}}
# ============================================================================
# PostgreSQL installation via PGDG
# ============================================================================
RUN apt-get update && apt-get install -y --no-install-recommends gnupg && \
    curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
      | gpg --dearmor -o /etc/apt/trusted.gpg.d/pgdg.gpg && \
    echo "deb https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
      > /etc/apt/sources.list.d/pgdg.list && \
    mkdir -p /etc/postgresql-common && \
    echo "create_main_cluster = false" > /etc/postgresql-common/createcluster.conf && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
      postgresql{{if .PostgreSQLVersion}}-{{.PostgreSQLVersion}}{{end}} \
      postgresql-client{{if .PostgreSQLVersion}}-{{.PostgreSQLVersion}}{{end}} && \
    PG_BIN_DIR="$(find /usr/lib/postgresql -mindepth 1 -maxdepth 1 -type d | sort -V | tail -1)/bin" && \
    mkdir -p /usr/local/lib && \
    ln -sfn "$PG_BIN_DIR" /usr/local/lib/cooper-postgres-bin && \
    rm -rf /var/lib/apt/lists/*
{{- end}}
```

### User Directory

Add `/home/user/pgdata` to the user dir creation when PostgreSQL is enabled:

```dockerfile
RUN mkdir -p /home/user/.npm-global /home/user/.config /home/user/.local/bin \
{{- if .HasPostgreSQL}}
    /home/user/pgdata \
{{- end}}
    && chown -R user:user /home/user
```

### PATH

Update PATH:

```dockerfile
ENV PATH=/home/user/.local/bin:/home/user/.npm-global/bin:/home/user/.opencode/bin{{- if .HasPostgreSQL}}:/usr/local/lib/cooper-postgres-bin{{- end}}:$PATH
```

### Export PG Defaults

Add conditional env exports:

```dockerfile
{{- if .HasPostgreSQL}}
ENV PGHOST=127.0.0.1
ENV PGPORT={{.PostgreSQLPort}}
ENV PGUSER=user
ENV PGDATABASE=postgres
ENV DATABASE_URL=postgresql://user@127.0.0.1:{{.PostgreSQLPort}}/postgres?sslmode=disable
{{- end}}
```

### Do Not Do These

- do not add `/usr/lib/postgresql/{{.PostgreSQLVersion}}/bin` directly to PATH
- do not make the entrypoint figure out the version with shell `ls`
- do not install only the client package

## Step 11: Update `entrypoint.sh.tmpl`

Modify `internal/templates/entrypoint.sh.tmpl`.

### Update `COOPER_PATH_LINE`

Add the stable PostgreSQL bin path:

```bash
COOPER_PATH_LINE='export PATH="/home/user/.local/bin:/home/user/.npm-global/bin:/home/user/.opencode/bin{{- if .HasGo}}:/usr/local/go/bin:/go/bin{{- end}}{{- if .HasPostgreSQL}}:/usr/local/lib/cooper-postgres-bin{{- end}}:$PATH"'
```

### Add `start_postgres()`

Place it near the other startup helper functions, before the final startup
sequence.

Use this exact shape:

```bash
start_postgres() {
  local pg_data="/home/user/pgdata"
  local pg_bin="/usr/local/lib/cooper-postgres-bin"

  if [ ! -f "$pg_data/PG_VERSION" ]; then
    echo "Initializing PostgreSQL data directory..."
    "$pg_bin/initdb" \
      -D "$pg_data" \
      --auth=trust \
      --no-locale \
      --encoding=UTF8 \
      -U user >/tmp/pg-initdb.log 2>&1 || return 1
  fi

  echo "Starting PostgreSQL server..."
  "$pg_bin/pg_ctl" \
    -D "$pg_data" \
    -l /tmp/postgresql.log \
    -o "-h 127.0.0.1 -p {{.PostgreSQLPort}} -k /tmp" \
    start >/tmp/pg-start.log 2>&1 || return 1

  for _i in $(seq 1 50); do
    if "$pg_bin/pg_isready" -h 127.0.0.1 -p {{.PostgreSQLPort}} -U user >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.1
  done

  echo "PostgreSQL failed to become ready. See /tmp/postgresql.log" >&2
  tail -n 100 /tmp/postgresql.log >&2 || true
  return 1
}
```

### Add Startup Call

In the startup sequence, keep the current order:

1. `start_socat_from_config`
2. PostgreSQL startup
3. Playwright runtime
4. Xvfb

Exact insertion:

```bash
echo "Starting port forwarding..."
start_socat_from_config

{{- if .HasPostgreSQL}}
start_postgres || exit 1
{{- end}}

ensure_playwright_runtime
start_shared_xvfb
```

### Why This Order Is Correct

- port forwarding still starts first
- PostgreSQL no longer collides with forwarded `5432`
- entrypoint exits clearly if PostgreSQL is required but broken

### Do Not Do These

- do not bind PostgreSQL to `5432`
- do not use only Unix sockets without exporting matching env
- do not swallow startup failure and continue boot
- do not background PostgreSQL startup without a readiness loop

## Step 12: Update Rendering Tests

Modify `internal/templates/templates_test.go`.

### Update `testConfig()`

Add PostgreSQL:

```go
ProgrammingTools: []config.ToolConfig{
	{Name: "go", Enabled: true, PinnedVersion: "1.24.10"},
	{Name: "node", Enabled: true, PinnedVersion: "22.12.0"},
	{Name: "python", Enabled: true, PinnedVersion: "3.12.1"},
	{Name: "postgresql", Enabled: true, PinnedVersion: "17"},
},
```

### Add Dockerfile Tests

Add:

1. PostgreSQL pinned version renders versioned packages
2. PostgreSQL empty version renders generic packages
3. stable symlink path exists
4. PG env vars render
5. `pgdata` dir render exists

Example pinned test:

```go
func TestRenderBaseDockerfile_PostgreSQLPinned(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "postgresql", Enabled: true, PinnedVersion: "17"},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "postgresql-17")
	assertContains(t, result, "postgresql-client-17")
	assertContains(t, result, "/usr/local/lib/cooper-postgres-bin")
	assertContains(t, result, "ENV PGHOST=127.0.0.1")
	assertContains(t, result, "ENV PGPORT=15432")
}
```

Example latest-save test:

```go
func TestRenderBaseDockerfile_PostgreSQLLatestUnresolvedUsesGenericPackages(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "postgresql", Enabled: true, Mode: config.ModeLatest},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "postgresql{{if .PostgreSQLVersion}}")
	_ = result
}
```

Do not use that exact template assertion above in code. For the real test,
assert the rendered output contains:

- `postgresql \`
- `postgresql-client \`

and does not contain:

- `postgresql-17`
- `postgresql-client-17`

### Add Entrypoint Tests

Add:

- PostgreSQL startup helper rendered
- `pg_isready` rendered
- startup call rendered
- path uses `/usr/local/lib/cooper-postgres-bin`
- port `15432` rendered
- no version-specific `/usr/lib/postgresql/17/bin` in entrypoint

Example:

```go
func TestRenderEntrypoint_PostgreSQL(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "postgresql", Enabled: true, PinnedVersion: "17"},
		},
		BridgePort: 4343,
	}

	result, err := RenderEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderEntrypoint failed: %v", err)
	}

	assertContains(t, result, "start_postgres()")
	assertContains(t, result, "pg_isready")
	assertContains(t, result, "/usr/local/lib/cooper-postgres-bin")
	assertContains(t, result, "127.0.0.1 -p 15432")
	assertNotContains(t, result, "/usr/lib/postgresql/17/bin")
}
```

## Step 13: Update Fixture Files

Update:

- `.testfiles/config-pinned.json`
- `.testfiles/config-latest.json`
- `.testfiles/config-mirror.json`

Add PostgreSQL entry to `programming_tools`.

Pinned:

```json
{"name": "postgresql", "enabled": true, "mode": "pin", "pinned_version": "17"}
```

Latest:

```json
{"name": "postgresql", "enabled": true, "mode": "latest"}
```

Mirror:

```json
{"name": "postgresql", "enabled": true, "mode": "mirror", "host_version": "17"}
```

Do not add a port forward rule for `15432` to any fixture.

## Step 14: Update `test-docker-build.sh`

This script needs both install checks and runtime checks.

### 14a. Keep install checks in Phase 5

Add PostgreSQL binary checks to the existing programming-tools phase.

Recommended install check:

```bash
local pg_version
pg_version=$(get_tool_version programming_tools postgresql)
if [ "$(is_tool_enabled programming_tools postgresql)" = "true" ]; then
    local actual_pg
    actual_pg=$(base_run psql --version 2>&1 || true)
    if [ -n "$pg_version" ]; then
        if echo "$actual_pg" | grep -q "PostgreSQL) ${pg_version}\."; then
            pass "${mode}: PostgreSQL ${pg_version} installed"
        else
            fail "${mode}: PostgreSQL ${pg_version} expected, got: ${actual_pg}"
        fi
    else
        assert_version "PostgreSQL" "" "$actual_pg"
    fi
fi
```

### 14b. Add a new runtime phase

Add a new phase after base-image install checks and before per-tool AI image
checks.

Use the normal image entrypoint. Do not override `--entrypoint` here.

Recommended phase:

```bash
info "${mode}: Asserting PostgreSQL runtime..."

if [ "$(is_tool_enabled programming_tools postgresql)" = "true" ]; then
    local pg_runtime
    pg_runtime=$(docker run --rm "$base_image" bash -lc '
        echo "PGHOST=$PGHOST"
        echo "PGPORT=$PGPORT"
        pg_isready -h "$PGHOST" -p "$PGPORT" -U "$PGUSER"
        psql -c "select 1"
    ' 2>&1 || true)

    if echo "$pg_runtime" | grep -q "PGHOST=127.0.0.1"; then
        pass "${mode}: PostgreSQL runtime exports PGHOST"
    else
        fail "${mode}: PostgreSQL runtime missing PGHOST (got: ${pg_runtime})"
    fi

    if echo "$pg_runtime" | grep -q "PGPORT=15432"; then
        pass "${mode}: PostgreSQL runtime exports PGPORT"
    else
        fail "${mode}: PostgreSQL runtime missing PGPORT (got: ${pg_runtime})"
    fi

    if echo "$pg_runtime" | grep -q "accepting connections"; then
        pass "${mode}: PostgreSQL accepts connections"
    else
        fail "${mode}: PostgreSQL not accepting connections (got: ${pg_runtime})"
    fi

    if echo "$pg_runtime" | grep -q "1 row"; then
        pass "${mode}: PostgreSQL query succeeded"
    else
        fail "${mode}: PostgreSQL query failed (got: ${pg_runtime})"
    fi
fi
```

## Step 15: Update `test-e2e.sh`

### 15a. Update generated config

Add PostgreSQL to the generated `programming_tools` list in the test config.

Use pinned version `17`.

### 15b. Update programming tool version phase

Add:

```bash
expected_pg=$(get_tool_version programming_tools postgresql)
actual_pg=$(barrel_exec 'psql --version 2>&1 || echo notfound')
if echo "$actual_pg" | grep -q "PostgreSQL) ${expected_pg}\."; then
    pass "PostgreSQL ${expected_pg} installed"
else
    fail "PostgreSQL ${expected_pg} expected, got: ${actual_pg}"
fi
```

### 15c. Add a new runtime phase for local PostgreSQL

Recommended checks inside the active barrel:

```bash
section "Phase 11d: Local PostgreSQL Runtime"

pg_runtime=$(barrel_exec '
  echo "PGHOST=$PGHOST"
  echo "PGPORT=$PGPORT"
  pg_isready -h "$PGHOST" -p "$PGPORT" -U "$PGUSER"
  psql -c "select 1"
')

if echo "$pg_runtime" | grep -q "PGHOST=127.0.0.1"; then
    pass "Local PostgreSQL exports PGHOST"
else
    fail "Local PostgreSQL missing PGHOST (got: ${pg_runtime})"
fi

if echo "$pg_runtime" | grep -q "PGPORT=15432"; then
    pass "Local PostgreSQL exports PGPORT"
else
    fail "Local PostgreSQL missing PGPORT (got: ${pg_runtime})"
fi

if echo "$pg_runtime" | grep -q "accepting connections"; then
    pass "Local PostgreSQL accepting connections"
else
    fail "Local PostgreSQL not accepting connections (got: ${pg_runtime})"
fi

if echo "$pg_runtime" | grep -q "1 row"; then
    pass "Local PostgreSQL query succeeded"
else
    fail "Local PostgreSQL query failed (got: ${pg_runtime})"
fi
```

### 15d. Preserve forwarded `5432` checks

Do not remove the existing `5432` forwarding assertions. The point of the new
design is that:

- forwarded `5432` keeps working
- local PostgreSQL lives on `15432`

Both must coexist.

## Step 16: Final Verification Commands

Run these after implementation. Per repo instructions, send output to `/tmp`.

From `/home/ricky/Personal/govner/cooper`:

```bash
go test ./internal/config ./internal/templates ./internal/app ./internal/configure > /tmp/cooper-postgres-go-test.txt 2>&1
tail -n 200 /tmp/cooper-postgres-go-test.txt
```

Then:

```bash
./test-docker-build.sh pinned > /tmp/cooper-postgres-docker-build.txt 2>&1
tail -n 200 /tmp/cooper-postgres-docker-build.txt
```

Then:

```bash
./test-e2e.sh > /tmp/cooper-postgres-e2e.txt 2>&1
tail -n 200 /tmp/cooper-postgres-e2e.txt
```

If any of those fail, do not stop at code inspection. Fix the implementation or
update the plan with a justified deviation.

## Do Not Do These

These are explicit anti-goals:

- Do not bind PostgreSQL to `5432`
- Do not add a new PostgreSQL-only branch in the TUI if shared metadata can
  express the behavior
- Do not make the entrypoint compute the installed PostgreSQL version using
  shell `ls` or `find` at runtime
- Do not depend on a versioned `/usr/lib/postgresql/<major>/bin` path in
  `entrypoint.sh.tmpl`
- Do not require network resolution during `cooper configure` save
- Do not verify only `psql --version`
- Do not add persistent PostgreSQL storage in this change
- Do not add `15432` to any forwarded-port fixture or test rule

## Acceptance Criteria

The implementation is done when all of the following are true:

1. `cooper configure` shows PostgreSQL as a programming tool.
2. PostgreSQL pin mode shows `e.g., 17` and PostgreSQL-specific help text.
3. Invalid PostgreSQL pin values like `17.2` are rejected before build.
4. `DetectHostVersion("postgresql")` returns a major-only value.
5. `ResolveLatestVersion("postgresql")` returns the latest supported major.
6. Saving a config with PostgreSQL in unresolved `ModeLatest` still renders a
   valid Dockerfile.
7. The base image exports `PGHOST`, `PGPORT`, `PGUSER`, `PGDATABASE`, and
   `DATABASE_URL` when PostgreSQL is enabled.
8. The entrypoint starts PostgreSQL on `127.0.0.1:15432`.
9. Entry-point startup exits non-zero if PostgreSQL is enabled but cannot
   become ready.
10. Existing forwarded `5432` behavior still works.
11. `cooper proof` verifies both PostgreSQL version presence and live
   connectivity.
12. `test-docker-build.sh` verifies both PostgreSQL install and runtime.
13. `test-e2e.sh` verifies both local PostgreSQL on `15432` and coexistence
   with forwarded `5432`.
