# LSP Auto-Install Plan

## Goal

Implement automatic installation of the standard language-server tooling for Cooper programming tools, with full Cooper feature support.

This means:

- Go installs `gopls`.
- Node.js / TypeScript installs `typescript-language-server`, plus `typescript` because the server depends on the TypeScript language service.
- Python installs both `pyright` and `python-lsp-server`.
- The feature is fully integrated with:
  - `cooper configure`
  - `cooper build`
  - `cooper update`
  - `cooper up` startup mismatch warnings
  - the About tab
  - `cooper proof`
  - `doctor.sh`
  - `test-e2e.sh`
  - docs

TypeScript remains bundled under the existing `node` programming tool. It is not a new top-level programming tool.

## Scope

This plan covers:

- version-aware implicit LSP selection
- base-image installation logic
- persisted built-state metadata for implicit tools
- startup/update mismatch detection
- About tab visibility
- proof/doctor/e2e coverage
- docs and configure-copy updates

This plan does not create new user-facing version controls for LSPs. LSPs remain implicit defaults attached to the language tool.

## Non-Goals

- Do not add a new top-level `typescript` programming tool.
- Do not add new TUI controls for enabling/disabling/pinning LSPs independently.
- Do not redesign Cooper's overall programming-tool model.
- Do not attempt a broad generic "base image template drift" redesign beyond what is needed for implicit-tool correctness.
- Do not attempt to fix Cooper's broader Python runtime pinning model in this change. That is a pre-existing issue and is called out explicitly below.

## Current State

The current implementation only models three built-in programming tools:

- `go`
- `node`
- `python`

Verified code references:

- `cooper/internal/configure/programming.go:52-56`
- `cooper/internal/app/configure.go:27-35`

The generated base image is driven by:

- `cooper/internal/templates/templates.go:27-38`
- `cooper/internal/templates/templates.go:131-159`
- `cooper/internal/templates/base.Dockerfile.tmpl`

There are currently no LSP installs anywhere in the Cooper codebase. A repo-wide search for `gopls`, `pyright`, `typescript-language-server`, `pylsp`, `pyright-langserver`, and `tsserver` in Go sources returned no matches.

The current base Dockerfile installs:

- Go by choosing `FROM golang:<version>-bookworm` when Go is enabled
- Node from the official tarball
- Python from Debian packages

Verified code references:

- `cooper/internal/templates/base.Dockerfile.tmpl:12-16`
- `cooper/internal/templates/base.Dockerfile.tmpl:79-88`
- `cooper/internal/templates/base.Dockerfile.tmpl:89-100`

The current runtime/About/version-check flow only knows about top-level programming tools and AI tools. It does not model implicit language servers.

Verified code references:

- `cooper/internal/tui/about/model.go:31-35`
- `cooper/internal/tui/about/model.go:57-60`
- `cooper/internal/tui/about/model.go:169-180`
- `cooper/internal/tui/about/model.go:205-249`

The current startup/update verification only checks top-level tool versions. It does not check implicit language servers.

Verified code references:

- `cooper/main.go:1350-1415`
- `cooper/internal/app/cooper.go:785-837`

The current proof/doctor/e2e checks only verify language runtimes, not language servers.

Verified code references:

- `cooper/internal/proof/proof.go:413-470`
- `cooper/internal/templates/doctor.sh:268-285`
- `cooper/test-e2e.sh:1540-1628`

## Important External Context

Cooper requirements already say version resolution happens at `configure` time and `update` time via HTTP APIs from Go, not by shelling out to package managers on the host.

Verified requirements reference:

- `cooper/REQUIREMENTS.md:105-112`

Cooper also already expects the About tab to show tool/version state and startup warnings:

- `cooper/REQUIREMENTS.md:229-233`
- `cooper/REQUIREMENTS.md:289-292`

The base image is Debian bookworm based. Node is always installed in the base image even when the `node` programming tool is disabled, because npm-based AI tools need it.

Verified code references:

- `cooper/internal/templates/base.Dockerfile.tmpl:79-88`

That matters because Python's `pyright` can rely on a base Node runtime even when the `node` programming tool is off.

## Design Decisions

### 1. Implicit Tooling Model

Add a new persisted config field for built implicit tools.

Use an explicit type, not `ToolConfig`, because host/mode semantics do not apply to implicit tooling.

Recommended type:

```go
type ImplicitToolConfig struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"`        // "lsp" or "support"
	ParentTool       string `json:"parent_tool"` // "go", "node", "python"
	Binary           string `json:"binary,omitempty"`
	ContainerVersion string `json:"container_version,omitempty"`
}
```

Recommended config addition:

```go
type Config struct {
	ProgrammingTools  []ToolConfig         `json:"programming_tools"`
	AITools           []ToolConfig         `json:"ai_tools"`
	ImplicitTools     []ImplicitToolConfig `json:"implicit_tools,omitempty"`
	// existing fields...
}
```

Use `Kind == "lsp"` for user-visible rows in the About tab.

Use `Kind == "support"` for hidden support packages that still affect build/update correctness.

Recommended built implicit tool set:

- Go enabled:
  - `gopls` (`kind=lsp`, `binary=gopls`, `parent_tool=go`)
- Node enabled:
  - `typescript-language-server` (`kind=lsp`, `binary=typescript-language-server`, `parent_tool=node`)
  - `typescript` (`kind=support`, `binary=tsc`, `parent_tool=node`)
- Python enabled:
  - `pyright` (`kind=lsp`, `binary=pyright-langserver`, `parent_tool=python`)
  - `python-lsp-server` (`kind=lsp`, `binary=pylsp`, `parent_tool=python`)

Do not show `typescript` in the About tab. Persist it anyway so `build`/`update` can detect drift.

### 2. Built-State Semantics

`Config.ImplicitTools` must represent the last successfully built implicit tool versions, not the desired target.

That matches how `ToolConfig.ContainerVersion` works today for top-level tools.

Important clarification: built-state persistence for this feature must be stage-based, not all-or-nothing across the entire `cooper build` or `cooper update` command.

Reason:

- the base image can succeed and materially change the real system state before later AI CLI or custom image builds run
- if a later child-image build fails, leaving `config.json` completely unchanged would make the saved state lie about what is already built
- future `cooper update`, startup warnings, About, proof, and doctor behavior all depend on `config.json` reflecting the last successfully built base state

Behavior by command:

- `cooper configure` / save-only:
  - may resolve target implicit versions in memory for template generation
  - must not overwrite `cfg.ImplicitTools`, because no image was built yet
- `cooper build`:
  - resolve target implicit versions
  - use them in the generated base Dockerfile
  - after successful base build, immediately persist the new base built state:
    - refresh and save `ProgrammingTools[].ContainerVersion`
    - assign and save `cfg.ImplicitTools`
  - if a later built-in AI CLI image succeeds, immediately persist that tool's `ContainerVersion`
  - if a later built-in AI CLI image fails, return an error but keep already-persisted successful base state and any already-persisted successful earlier AI tool state
  - custom image builds have no config entries; if a custom image fails after the base succeeded, return an error and leave the already-persisted base state intact
- `cooper update`:
  - refresh desired top-level tool versions first
  - resolve target implicit versions from the refreshed desired tool state
  - compare target vs `cfg.ImplicitTools`
  - if different, mark base rebuild required
  - after successful base rebuild, immediately persist the new base built state:
    - refresh and save `ProgrammingTools[].ContainerVersion`
    - assign and save `cfg.ImplicitTools`
  - if later built-in AI CLI rebuilds succeed, persist their `ContainerVersion` incrementally
  - if a later child rebuild fails, return an error but keep already-persisted successful base state and any already-persisted successful earlier AI tool state

Migration behavior:

- old configs will load with `ImplicitTools == nil`
- first `cooper update` after this feature lands should treat missing built implicit state as mismatch and rebuild the base image

### 3. Resolve Concrete Versions Before Template Generation

Cooper requirements already say latest resolution happens at configure-time and update-time.

Today `runBuild` resolves latest top-level tool versions before template generation:

- `cooper/main.go:294-299`

But `ConfigureApp.Save()` does not:

- `cooper/internal/app/configure.go:165-199`

This is already inconsistent with requirements and becomes more problematic once implicit LSP versions depend on the selected language versions.

Required change:

- extract current `resolveLatestVersions` logic out of `main.go`
- move it to a shared helper in `internal/config`
- call it from:
  - `ConfigureApp.Save()`
  - `runBuild`
  - `runUpdate`

Recommended helper shape:

```go
type DesiredVersionRefreshOptions struct {
	AllowStaleFallback bool
}

func RefreshDesiredToolVersions(cfg *Config, opts DesiredVersionRefreshOptions) error

func RefreshDesiredToolVersionsBestEffort(cfg *Config, timeout time.Duration) map[string]error
```

Behavior:

- for `ModeLatest`, always resolve and write concrete `PinnedVersion` for every enabled tool in latest mode, even when `ContainerVersion` already matches
- for `ModeMirror`, always detect and write fresh `HostVersion` for every enabled tool in mirror mode, even when `ContainerVersion` already matches
- for `ModePin`, keep `PinnedVersion` as the selected version source and validate it is present for enabled pinned tools
- do not touch `ContainerVersion`
- when `AllowStaleFallback == false`, any enabled latest/mirror tool that cannot be freshly resolved/detected causes an error
- when `AllowStaleFallback == true`, refresh failures may fall back to already-saved concrete desired state if and only if that fallback is sufficient to render truthful, buildable templates:
  - latest mode may fall back to an already-present concrete `PinnedVersion`
  - mirror mode may fall back to an already-present `HostVersion`
  - if no usable fallback exists for an enabled tool, return an error
- `RefreshDesiredToolVersionsBestEffort` is for startup warnings only:
  - it mutates only the provided config value, which should be a deep copy of the loaded config
  - it writes freshly resolved `PinnedVersion` / `HostVersion` for tools that were successfully refreshed
  - it returns a per-tool error map for tools that could not be refreshed within the warning timeout budget
  - it never persists config and never blocks startup

This helper is a full refresh pass for desired tool state. Callers must not rely on mismatch-detection side effects to populate `PinnedVersion` or `HostVersion`.

This makes generated templates truthful during `cooper configure`, not only during `cooper build`.

## Version Selection Rules

## Effective Runtime Version Rules

Use the configured/resolved programming-tool version, not the built container version, to choose implicit tooling.

Recommended helper:

```go
func EffectiveProgrammingToolVersion(cfg *Config, toolName string) (string, bool, error)
```

Behavior:

- disabled or missing tool: `("", false, nil)`
- `ModeMirror`: use `HostVersion`
- `ModePin`: use `PinnedVersion`
- `ModeLatest`: use resolved `PinnedVersion`
- if enabled but no effective version is available: return error

Node needs one extra helper because Node exists in the base even when the `node` programming tool is off.

Extract the current hardcoded base default from `cooper/internal/templates/base.Dockerfile.tmpl:81-85`.

Recommended shared constant:

```go
const DefaultBaseNodeVersion = "22.12.0"
```

Recommended helper:

```go
func EffectiveBaseNodeVersion(cfg *Config) (string, error)
```

Behavior:

- if `node` programming tool is enabled, use its effective configured version
- otherwise use `DefaultBaseNodeVersion`

## Go / gopls Rules

Use the official gopls compatibility policy.

Verified source:

- `https://go.dev/gopls/`
- `https://proxy.golang.org/golang.org/x/tools/gopls/@latest`

Rules:

- Go `>= 1.21`: use latest stable `gopls`
- Go `1.20`: use `gopls v0.15.3`
- Go `1.18` or `1.19`: use `gopls v0.14.2`
- Go `1.17`: use `gopls v0.11.0`
- Go `1.15` or `1.16`: use `gopls v0.9.5`
- Go `1.12` to `1.14`: use `gopls v0.7.5`
- Go `< 1.12`: fail with a clear error

Recommended helper:

```go
func ResolveGoplsVersion(goVersion string) (string, error)
```

For latest resolution, use the Go module proxy:

- latest endpoint: `https://proxy.golang.org/golang.org/x/tools/gopls/@latest`
- version info endpoint: `https://proxy.golang.org/golang.org/x/tools/gopls/@v/<tag>.info`

Do not shell out to `go list` on the host.

Store gopls versions with the leading `v`, for example `v0.21.1`.

## Node / TypeScript Rules

TypeScript remains part of the `node` programming tool.

Install two packages when `node` is enabled:

- `typescript-language-server`
- `typescript`

Verified npm metadata:

- latest `typescript-language-server` requires `node >=20`
- `typescript-language-server@4.4.1` requires `node >=18`
- `typescript-language-server@3.3.2` requires `node >=14.17`
- verified from registry payloads and version-specific metadata

Rules for `typescript-language-server`:

- Node `>=20`: resolve latest stable from npm and use it
- Node `>=18 && <20`: use `4.4.1`
- Node `>=14.17 && <18`: use `3.3.2`
- Node `<14.17`: fail with a clear error

Recommended helper:

```go
func ResolveTypeScriptLanguageServerVersion(nodeVersion string) (string, error)
```

Rules for `typescript`:

- resolve latest stable from npm
- validate its `engines.node` against the effective Node version
- if latest is compatible, use it
- if latest is incompatible, fall back to verified-good `5.8.3`
- if effective Node still does not satisfy the fallback, fail with a clear error

Recommended helper:

```go
func ResolveTypeScriptPackageVersion(nodeVersion string) (string, error)
```

This keeps `typescript` reasonably current without making it a separate user-configurable tool.

## Python Rules

Install two packages when `python` is enabled:

- `pyright`
- `python-lsp-server`

`pyright` is npm-based and depends on the effective base Node version.

Verified npm metadata:

- latest `pyright` requires `node >=14.0.0`
- package exposes both `pyright` and `pyright-langserver` binaries

Rules for `pyright`:

- resolve latest stable from npm
- validate its `engines.node` against effective base Node version
- if latest is compatible, use it
- else fall back to verified-good `1.1.408`
- if effective Node `<14.0.0`, fail with a clear error

Recommended helper:

```go
func ResolvePyrightVersion(nodeVersion string) (string, error)
```

Rules for `python-lsp-server`:

- Python `>=3.9`: resolve latest stable from PyPI and use it
- Python `==3.8.*`: use `1.12.2`
- Python `==3.7.*`: use `1.7.4`
- Python `==3.6.*`: use `1.3.3`
- Python `<3.6`: fail with a clear error

Recommended helper:

```go
func ResolvePythonLSPServerVersion(pythonVersion string) (string, error)
```

Use the PyPI JSON API:

- latest metadata: `https://pypi.org/pypi/python-lsp-server/json`
- version metadata: `https://pypi.org/pypi/python-lsp-server/<version>/json`

## Pre-Existing Python Runtime Caveat

Cooper currently does not actually pin Python to the configured version in the base image. It installs Debian's default `python3` instead.

Verified code reference:

- `cooper/internal/templates/base.Dockerfile.tmpl:93-100`

This plan does not replace Python installation strategy.

For this LSP feature, it is still acceptable to select `python-lsp-server` based on the configured effective Python version because the verified older `python-lsp-server` releases only declare lower-bound `requires_python` constraints and therefore remain installable on newer actual interpreters.

This caveat must be called out in docs/comments so the implementor does not confuse “configured Python version” with “actual container Python version.”

## Resolver and Comparison API

Create a new file:

- `cooper/internal/config/lsp.go`

Recommended contents:

- `DefaultBaseNodeVersion`
- `ImplicitToolConfig` type
- effective-version helpers
- Node/Python/Go version comparison helpers
- latest-package metadata helpers
- `ResolveImplicitTools(cfg *Config) ([]ImplicitToolConfig, error)`
- `CompareImplicitTools(built, target []ImplicitToolConfig) []string`
- `VisibleImplicitLSPs(tools []ImplicitToolConfig) []ImplicitToolConfig`

Recommended resolver output order:

- stable and deterministic
- sort by `ParentTool`, then `Kind`, then `Name`

Example target resolver output:

```go
[]config.ImplicitToolConfig{
	{
		Name:             "gopls",
		Kind:             "lsp",
		ParentTool:       "go",
		Binary:           "gopls",
		ContainerVersion: "v0.21.1",
	},
	{
		Name:             "typescript-language-server",
		Kind:             "lsp",
		ParentTool:       "node",
		Binary:           "typescript-language-server",
		ContainerVersion: "5.1.3",
	},
	{
		Name:             "typescript",
		Kind:             "support",
		ParentTool:       "node",
		Binary:           "tsc",
		ContainerVersion: "6.0.2",
	},
	{
		Name:             "pyright",
		Kind:             "lsp",
		ParentTool:       "python",
		Binary:           "pyright-langserver",
		ContainerVersion: "1.1.408",
	},
	{
		Name:             "python-lsp-server",
		Kind:             "lsp",
		ParentTool:       "python",
		Binary:           "pylsp",
		ContainerVersion: "1.14.0",
	},
}
```

Recommended comparison warning format:

```text
gopls (for go): container=v0.15.3, expected=v0.21.1
typescript-language-server (for node): container=4.4.1, expected=5.1.3
python-lsp-server (for python): not built, expected=1.14.0
typescript (for node): built but no longer expected
```

## Template Changes

## Signatures

Change the base-template render path to accept already-resolved implicit tools explicitly.

Recommended signature changes:

```go
func RenderBaseDockerfile(cfg *config.Config, implicit []config.ImplicitToolConfig) (string, error)
func WriteAllTemplates(baseDir, cliDir string, cfg *config.Config, implicit []config.ImplicitToolConfig) error
```

Do not hide implicit resolution inside the templates package. Keep network/version resolution outside the renderer so tests stay deterministic.

Update all call sites:

- `cooper/main.go`
- `cooper/internal/app/configure.go`
- `cooper/internal/testdocker/bootstrap.go`
- `cooper/internal/testdriver/driver.go`
- any tests calling these functions directly

## Template Data

Extend `baseDockerfileData` in `cooper/internal/templates/templates.go`.

Recommended fields:

```go
type baseDockerfileData struct {
	HasGo                bool
	GoVersion            string
	HasNode              bool
	NodeVersion          string
	HasPython            bool
	HasCodex             bool
	HasOpenCode          bool
	ProxyPort            int

	GoLSPVersion          string
	NodeTSLSPVersion      string
	NodeTypeScriptVersion string
	PythonPyrightVersion  string
	PythonPylspVersion    string
}
```

Populate those fields from the passed `implicit []config.ImplicitToolConfig`.

## Dockerfile Placement

Important placement rules in `cooper/internal/templates/base.Dockerfile.tmpl`:

- keep system package installation at the top
- keep Node tarball install where it is
- keep Python apt install where it is
- keep user creation before user-scoped installs
- add implicit tool installs after:
  - `USER user`
  - `ENV NPM_CONFIG_PREFIX=...`
  - `ENV PATH=...`
- add implicit tool installs before runtime proxy env vars:
  - `ENV HTTP_PROXY=http://cooper-proxy:...`
  - `ENV HTTPS_PROXY=http://cooper-proxy:...`

This avoids the build-time proxy problem, because `cooper-proxy` does not exist during Docker build.

Relevant existing references:

- base user creation: `cooper/internal/templates/base.Dockerfile.tmpl:124-134`
- Go writable cache prep: `cooper/internal/templates/base.Dockerfile.tmpl:126-130`
- user PATH/npm prefix: `cooper/internal/templates/base.Dockerfile.tmpl:149-154`
- runtime proxy envs: `cooper/internal/templates/base.Dockerfile.tmpl:156-161`

## Recommended Install Commands

Go:

```dockerfile
RUN GOTOOLCHAIN=auto GOBIN=/home/user/.local/bin \
    go install golang.org/x/tools/gopls@{{.GoLSPVersion}} \
    && gopls version
```

Notes:

- keep `GOTOOLCHAIN=auto` so Go `1.21+` can satisfy newer `gopls` toolchain requirements
- install as `user`
- use `GOBIN=/home/user/.local/bin` because that path is already on PATH

Node / TypeScript:

```dockerfile
RUN npm install -g \
    typescript@{{.NodeTypeScriptVersion}} \
    typescript-language-server@{{.NodeTSLSPVersion}} \
    && typescript-language-server --version \
    && tsc --version
```

Python / Pyright:

```dockerfile
RUN npm install -g pyright@{{.PythonPyrightVersion}} \
    && pyright --version \
    && command -v pyright-langserver
```

Python / python-lsp-server:

```dockerfile
RUN python3 -m pip install --user "python-lsp-server=={{.PythonPylspVersion}}" \
    && command -v pylsp \
    && python3 -m pip show python-lsp-server
```

Do not use system-wide `pip install` as root in bookworm for this change. Installing as `user` is simpler and avoids PEP-668 system-package conflicts.

## Build / Configure / Update Wiring

## `cooper configure`

Update `cooper/internal/app/configure.go:165-199`.

Before saving config and writing templates:

- call shared `RefreshDesiredToolVersions(cfg, DesiredVersionRefreshOptions{AllowStaleFallback: true})`
- call `ResolveImplicitTools(cfg)`
- pass resolved implicit slice into `templates.WriteAllTemplates(...)`

Do not update `cfg.ImplicitTools` during save-only. No image was built yet.

Important save-only behavior:

- `cooper configure` save should not become a blanket hard network dependency
- it should attempt a fresh desired-version refresh at configure time because the requirements say configure-time resolution is part of the product behavior
- if refresh fails for a latest/mirror tool but the config already contains a usable concrete desired version (`PinnedVersion` for latest or `HostVersion` for mirror), save may continue and generate templates from that last-known concrete version
- if refresh fails and no usable concrete fallback exists for any enabled tool needed to render templates truthfully, return an error from `Save()` so the configure UI can display it
- when save proceeds using stale fallback, surface a clear warning that templates were generated from last-known resolved versions and that `cooper build` or `cooper update` with network access is required to refresh to the newest desired versions

Important distinction:

- `cooper build` and `cooper update` remain the strict freshness points for latest/mirror resolution
- save-only is allowed to fall back to last-known concrete desired versions; build/update are not
- save-only generated templates are only a preview of the current saved config state; `cooper build` must always re-refresh desired versions and re-render templates before building, so an offline save cannot by itself cause a stale image build

Warning plumbing for save-only:

- make the warning channel explicit instead of relying on ad hoc stderr output
- recommended API change:

```go
func (a *ConfigureApp) Save() ([]string, error)
```

- returned warning strings are non-fatal save warnings, primarily stale-fallback notices from configure-time version refresh
- `ConfigureApp.SaveAndBuild()` should call `Save()`, print any returned warnings to stderr, then proceed with build
- `cooper/internal/configure/save.go` should append returned warnings into `doneMsgs` before the final guidance lines so the save screen shows them deterministically
- do not rely on stderr alone for the TUI save path

Also update configure copy text:

- `cooper/internal/configure/programming.go:378-433`
- `cooper/internal/configure/save.go:140-142`

Recommended detail copy additions:

- Go: “Also installs `gopls`, selected from the configured Go version.”
- Node.js: “Also installs `typescript-language-server` and `typescript`, selected from the configured Node version.”
- Python: “Also installs `pyright` and `python-lsp-server`, selected from the configured Python version and the base Node version.”

Recommended save-screen copy update:

Current line says base rebuild happens only if programming tools changed. That becomes incomplete once implicit latest-package drift can trigger rebuild.

Update to say the base image rebuilds when:

- programming tool versions change
- implicit language-server/support-tool versions change

## `cooper build`

Update `cooper/main.go:284-390`.

Recommended order:

1. resolve top-level concrete programming/AI tool versions
2. resolve implicit tools
3. write templates using resolved implicit tools
4. build proxy image
5. build base image
6. immediately persist successful base built state
7. build built-in AI CLI images one by one, persisting each successful tool image state immediately
8. build custom images
9. return success only if all requested images succeed

Persistence rule for partial failure:

- if proxy build fails, nothing new is built and no new built state is saved
- if base build fails, no new base built state is saved
- if base build succeeds and a later AI/custom image build fails, the command returns an error but `config.json` must keep the new base built state because it is already real
- if one built-in AI CLI image succeeds and a later one fails, the succeeded AI tool's `ContainerVersion` should remain persisted; only failed or not-yet-built tool images remain unchanged

Pseudo-flow:

```go
if err := config.RefreshDesiredToolVersions(cfg, config.DesiredVersionRefreshOptions{AllowStaleFallback: false}); err != nil { ... }

implicit, err := config.ResolveImplicitTools(cfg)
if err != nil { ... }

if err := templates.WriteAllTemplates(baseDir, cliDir, cfg, implicit); err != nil { ... }

if err := buildProxy(...); err != nil { ... }

if err := buildBase(...); err != nil { ... }

updateProgrammingToolContainerVersions(cfg)
cfg.ImplicitTools = append([]config.ImplicitToolConfig(nil), implicit...)
config.SaveConfig(configPath, cfg)

for each built-in AI tool {
	if err := buildToolImage(...); err != nil {
		return err
	}
	updateSingleAIToolContainerVersion(cfg, toolName)
	config.SaveConfig(configPath, cfg)
}

for each custom image {
	if err := buildCustomImage(...); err != nil {
		return err
	}
}

return nil
```

Important: the implementation agent does not have to use these exact helper names, but the persisted-state behavior above is required.

## `cooper update`

Update `cooper/main.go:1005-1201`.

Current problem:

- `collectUpdatePlan` only marks `baseChanged` when a programming tool version changed
- it misses implicit-tool drift such as new upstream `gopls` or `pyright` releases
- it currently refreshes `PinnedVersion` or `HostVersion` only inside mismatch branches, which means implicit resolution can use stale or empty desired versions when the container already matches
- when `cooper-base` changes, current `runUpdate` rebuilds enabled built-in AI tool images but not user custom CLI images under `~/.cooper/cli/{name}/`, even though those custom images are also documented to use `FROM cooper-base`

Why this matters:

- this does not make the new `cooper-base` image stale
- it makes already-built child CLI images stale relative to the new base image
- Docker child images do not automatically rebind themselves to a newly rebuilt parent tag; an existing `cooper-cli-{custom}` image still refers to the older parent image content it was built from until it is rebuilt
- therefore any implicit-LSP-driven base rebuild must also rebuild all custom CLI images, not just built-in AI tool images

Required change:

- split update into two explicit phases:
  - phase 1: fully refresh desired top-level tool state for all enabled programming and AI tools
  - phase 2: compare refreshed desired state against built state to compute the rebuild plan
- implicit tool resolution must run only after phase 1 completed successfully enough to determine the effective programming-tool versions it depends on
- do not rely on mismatch-branch mutation to populate `PinnedVersion` or `HostVersion`
- after refreshed desired programming-tool versions are available, resolve target implicit tools
- compare target implicit tools vs `cfg.ImplicitTools`
- if any mismatch exists, mark `plan.baseChanged = true`

This comparison must happen even when Go/Node/Python versions themselves did not change.

Example:

- Go pinned `1.24.10`
- built implicit `gopls=v0.20.0`
- upstream latest `gopls=v0.21.1`
- current code would incorrectly say “No rebuild needed”
- new code must rebuild base

Recommended new plan shape:

```go
type updatePlan struct {
	baseChanged      bool
	toolsChanged     map[string]bool
	customImages     []string
	targetImplicit   []config.ImplicitToolConfig
}
```

Recommended algorithm:

1. run a full desired-version refresh pass for all enabled top-level tools:
   - latest-mode tools always resolve and store fresh `PinnedVersion`
   - mirror-mode tools always detect and store fresh `HostVersion`
   - pin-mode tools retain their pinned version
2. compare refreshed desired top-level versions against built `ContainerVersion` values to determine base/tool rebuild needs
3. resolve `targetImplicit` from the refreshed programming-tool state
4. compare `cfg.ImplicitTools` vs `targetImplicit`
5. if mismatch:
   - log mismatch lines to stderr
   - set `plan.baseChanged = true`
6. discover custom CLI image directories in `cli/*` that are not built-in AI tool names and contain a `Dockerfile`
7. if base rebuild succeeds:
   - immediately persist refreshed programming-tool `ContainerVersion` values
   - assign `cfg.ImplicitTools = targetImplicit`
   - save config before any later child image rebuilds run
8. if `baseChanged == true`, rebuild all child CLI images that depend on `cooper-base`:
   - all enabled built-in AI tool images
   - all discovered custom CLI images
9. if `baseChanged == false`, rebuild only the specific built-in AI tool images in `toolsChanged`; custom CLI images do not rebuild in this case because their parent base image did not change and they are not version-managed by Cooper
10. if later built-in AI CLI rebuilds succeed, persist each successful tool's `ContainerVersion` incrementally
11. if a later child image rebuild fails, return an error but keep already-persisted successful base state and any already-persisted successful earlier AI tool state

If desired-version refresh fails for any enabled programming tool during `cooper update`, fail `cooper update` for that config. Do not fall back to stale stored `PinnedVersion` or `HostVersion`, and do not skip implicit comparison with a warning in the update path. `cooper update` is a strict freshness point.

Important:

- if `cfg.ImplicitTools` is empty and `targetImplicit` is non-empty, that is a mismatch
- if built set contains tools no longer expected, that is also a mismatch
- custom CLI images have no config entries, but they must still rebuild whenever `baseChanged == true`

## Startup Warnings and About Tab

## Startup warning logic

Startup checks already run before About is created:

- `cooper/main.go:592-699`

Extend the version-warning pass to include implicit tools.

Important behavior:

- `cooper up` remains non-blocking if version lookup fails
- implicit-tool version lookup failures should behave like current top-level latest lookups:
  - no crash
  - no block
  - skip or mark unknown
  - leave app usable

Recommended approach:

- deep-copy the loaded config first; never mutate the original config during startup warning computation
- run `RefreshDesiredToolVersionsBestEffort(cfgCopy, timeout)` on the copy before any top-level warning comparisons or implicit-tool resolution
- use the refreshed copy for both:
  - top-level programming/AI expected-version comparisons
  - implicit-tool target resolution
- latest-mode warning checks must use the freshly refreshed `PinnedVersion` written into the config copy, not a stale persisted value and not a local throwaway variable
- if a programming tool needed for implicit resolution could not be refreshed in the best-effort pass, skip implicit mismatch checking for that parent tool and append a concrete warning such as `could not verify implicit tools for go: <err>`
- append all mismatch and refresh-failure messages to the same startup warning slice

Update both existing duplicated version-check implementations:

- `cooper/main.go:1350-1415`
- `cooper/internal/app/cooper.go:785-837`

If convenient, extract shared helper logic instead of editing both independently.

## About tab

Extend `cooper/internal/tui/about/model.go`.

Recommended model fields:

```go
progTools      []config.ToolConfig
aiTools        []config.ToolConfig
implicitTools  []config.ImplicitToolConfig
```

Copy the new slice in `about.New(cfg)`.

Add a third section after Programming Tools and AI CLI Tools:

```text
Implicit Language Servers
```

Recommended columns:

- `TOOL`
- `FOR`
- `BINARY`
- `CONTAINER VERSION`

Do not show `Kind == "support"` rows here. Only show actual LSP rows.

Use `cfg.ImplicitTools` as the source of truth for built versions. Do not do network lookups in the About model.

Mismatch visibility remains driven by the existing banner and startup warning strings.

Also update `runTUITest` mock data in `cooper/main.go:1204-1301` so the About screen shows sample implicit LSP rows in visual QA mode without needing network.

## Proof, Doctor, and E2E

## `cooper proof`

Update `cooper/internal/proof/proof.go:413-470`.

After programming-tool checks, add implicit-tool checks in the first barrel.

Recommended checks:

- `gopls`:
  - command: `gopls version`
  - compare output against `cfg.ImplicitTools` entry version
- `typescript-language-server`:
  - command: `typescript-language-server --version`
  - compare against built version
- `pyright`:
  - command: `pyright --version`
  - separately verify `command -v pyright-langserver`
- `python-lsp-server`:
  - verify `command -v pylsp`
  - run `python3 -m pip show python-lsp-server`
  - compare reported version against built version

Keep proof logic tied to `cfg.ImplicitTools` rather than hardcoded expected versions.

## `doctor.sh`

Update `cooper/internal/templates/doctor.sh`.

Add a new section:

```text
Implicit Language Servers
```

Recommended checks:

```bash
check_tool "gopls" gopls version
check_tool "TypeScript Language Server" typescript-language-server --version
check_tool "Pyright" pyright --version
```

For `pyright-langserver`, use a binary presence check:

```bash
if command -v pyright-langserver >/dev/null 2>&1; then
    pass "pyright-langserver: present"
else
    warn "pyright-langserver: not found (may not be enabled)"
fi
```

For `python-lsp-server`, do not assume `pylsp --version` is reliable. Use:

```bash
if command -v pylsp >/dev/null 2>&1; then
    ver=$(python3 -m pip show python-lsp-server 2>/dev/null | grep '^Version:' | head -1)
    pass "python-lsp-server: ${ver}"
else
    warn "python-lsp-server: not found (may not be enabled)"
fi
```

The shell script already uses `head`/`grep`; this is acceptable inside the script.

## `test-e2e.sh`

Update the helper section around:

- `cooper/test-e2e.sh:398-402`

Add a new helper:

```bash
get_implicit_tool_version() {
    local tool_name=$1
    jq -r ".implicit_tools[] | select(.name==\"${tool_name}\") | .container_version // empty" "${CONFIG_DIR}/config.json"
}
```

Add new verification phase immediately after current programming-tool phase.

Recommended checks:

- `gopls`
- `typescript-language-server`
- `pyright`
- `python-lsp-server`

Use the built config as the expected source of truth.

Examples:

```bash
expected_gopls=$(get_implicit_tool_version gopls)
actual_gopls=$(barrel_exec 'gopls version 2>&1 || echo notfound')
```

```bash
expected_tsls=$(get_implicit_tool_version typescript-language-server)
actual_tsls=$(barrel_exec 'typescript-language-server --version 2>&1 || echo notfound')
```

```bash
expected_pyright=$(get_implicit_tool_version pyright)
actual_pyright=$(barrel_exec 'pyright --version 2>&1 || echo notfound')
```

```bash
expected_pylsp=$(get_implicit_tool_version python-lsp-server)
actual_pylsp=$(barrel_exec 'python3 -m pip show python-lsp-server 2>&1 || echo notfound')
```

Also verify presence of:

- `pyright-langserver`
- `pylsp`

## Testing Plan

This section must be treated as part of the implementation contract. The feature is not complete until both the automated matrix and the manual verification matrix are fully satisfied.

Definition of done for this feature:

- all automated scenarios below are implemented
- all automated scenarios pass locally
- `go test ./...` passes
- `./test-e2e.sh` passes
- `cooper proof` passes
- the manual verification matrix below is executed and each expected outcome is confirmed

## Automated Test Matrix

The automated test plan below is intentionally exhaustive for the behavior introduced by this feature. The implementation agent should treat each numbered item as a required scenario, not a suggestion.

### A. Resolver and compatibility unit tests

Primary file:

- new `cooper/internal/config/lsp_test.go`

Secondary files if needed:

- `cooper/internal/config/resolve_test.go`
- `cooper/internal/config/config_test.go`

Required scenarios:

1. Effective top-level version selection.
   Files: `cooper/internal/config/lsp_test.go`
   Suggested tests:
   - `TestEffectiveProgrammingToolVersion_Mirror`
   - `TestEffectiveProgrammingToolVersion_Pin`
   - `TestEffectiveProgrammingToolVersion_LatestUsesPinned`
   - `TestEffectiveProgrammingToolVersion_Disabled`
   - `TestEffectiveProgrammingToolVersion_EnabledButMissingVersion`
   Coverage:
   - mirror uses `HostVersion`
   - pin uses `PinnedVersion`
   - latest uses resolved `PinnedVersion`
   - latest-mode effective version still refreshes even when `ContainerVersion` already matches
   - mirror-mode effective version still refreshes even when `ContainerVersion` already matches
   - disabled tool returns not enabled without error
   - enabled tool with no effective version errors clearly

2. Effective base Node version selection.
   Files: `cooper/internal/config/lsp_test.go`
   Suggested tests:
   - `TestEffectiveBaseNodeVersion_UsesEnabledNodeTool`
   - `TestEffectiveBaseNodeVersion_FallsBackToDefaultWhenNodeDisabled`

3. Go `gopls` compatibility mapping.
   Files: `cooper/internal/config/lsp_test.go`
   Suggested tests:
   - `TestResolveGoplsVersion_UsesLatestForGo121Plus`
   - `TestResolveGoplsVersion_Go120`
   - `TestResolveGoplsVersion_Go118And119`
   - `TestResolveGoplsVersion_Go117`
   - `TestResolveGoplsVersion_Go115And116`
   - `TestResolveGoplsVersion_Go112To114`
   - `TestResolveGoplsVersion_UnsupportedGo`
   Coverage:
   - Go `1.24` resolves latest `gopls`
   - Go `1.20` resolves `v0.15.3`
   - Go `1.18` resolves `v0.14.2`
   - Go `1.17` resolves `v0.11.0`
   - Go `1.15` resolves `v0.9.5`
   - Go `1.12` resolves `v0.7.5`
   - Go `1.11` returns unsupported error

4. Node / TypeScript compatibility mapping.
   Files: `cooper/internal/config/lsp_test.go`
   Suggested tests:
   - `TestResolveTypeScriptLanguageServerVersion_Node20OrNewerUsesLatest`
   - `TestResolveTypeScriptLanguageServerVersion_Node18And19`
   - `TestResolveTypeScriptLanguageServerVersion_Node16And17`
   - `TestResolveTypeScriptLanguageServerVersion_NodeTooOld`
   Coverage:
   - Node `20` resolves latest `typescript-language-server`
   - Node `18` resolves `4.4.1`
   - Node `16` resolves `3.3.2`
   - Node `14.16` returns unsupported error

5. TypeScript package selection.
   Files: `cooper/internal/config/lsp_test.go`
   Suggested tests:
   - `TestResolveTypeScriptPackageVersion_UsesLatestWhenCompatible`
   - `TestResolveTypeScriptPackageVersion_FallsBackWhenLatestIncompatible`
   - `TestResolveTypeScriptPackageVersion_TooOldEvenForFallback`
   Coverage:
   - latest is used when `engines.node` allows it
   - fallback `5.8.3` is used when latest is incompatible
   - clear error when Node is too old even for fallback

6. Python LSP compatibility mapping.
   Files: `cooper/internal/config/lsp_test.go`
   Suggested tests:
   - `TestResolvePythonLSPServerVersion_Python39PlusUsesLatest`
   - `TestResolvePythonLSPServerVersion_Python38`
   - `TestResolvePythonLSPServerVersion_Python37`
   - `TestResolvePythonLSPServerVersion_Python36`
   - `TestResolvePythonLSPServerVersion_UnsupportedPython`
   Coverage:
   - Python `3.12` resolves latest `python-lsp-server`
   - Python `3.8` resolves `1.12.2`
   - Python `3.7` resolves `1.7.4`
   - Python `3.6` resolves `1.3.3`
   - Python `<3.6` returns unsupported error

7. Pyright compatibility.
   Files: `cooper/internal/config/lsp_test.go`
   Suggested tests:
   - `TestResolvePyrightVersion_UsesLatestWhenCompatible`
   - `TestResolvePyrightVersion_FallsBackWhenLatestIncompatible`
   - `TestResolvePyrightVersion_TooOldNode`
   Coverage:
   - latest is used when effective base Node supports it
   - fallback `1.1.408` is used if needed
   - clear error if effective base Node `<14.0.0`

8. Full implicit resolver output.
   Files: `cooper/internal/config/lsp_test.go`
   Suggested tests:
   - `TestResolveImplicitTools_GoOnly`
   - `TestResolveImplicitTools_NodeOnly`
   - `TestResolveImplicitTools_PythonOnly`
   - `TestResolveImplicitTools_AllProgrammingTools`
   - `TestResolveImplicitTools_PythonUsesBaseNodeWhenNodeToolDisabled`
   - `TestResolveImplicitTools_DisabledToolsProduceNoEntries`
   - `TestResolveImplicitTools_IsSortedDeterministically`
   Coverage:
   - Go only yields only `gopls`
   - Node only yields `typescript-language-server` and `typescript`
   - Python only yields `pyright` and `python-lsp-server`
   - all tools enabled yields all five implicit entries
   - Python-only still resolves `pyright` using default base Node version
   - disabled tools do not leak implicit tools
   - returned order is stable

9. Comparison helpers.
   Files: `cooper/internal/config/lsp_test.go`
   Suggested tests:
   - `TestCompareImplicitTools_NoDifferences`
   - `TestCompareImplicitTools_MissingBuiltTool`
   - `TestCompareImplicitTools_VersionMismatch`
   - `TestCompareImplicitTools_UnexpectedBuiltTool`
   - `TestVisibleImplicitLSPs_FiltersSupportTools`
   Coverage:
   - missing built tool
   - version mismatch
   - built-but-no-longer-expected tool
   - About visibility filter excludes `typescript`

10. Latest resolver plumbing.
    Files: `cooper/internal/config/resolve_test.go`
    Suggested tests:
    - `TestResolveGoplsLatest`
    - `TestResolveGoplsLatestInvalidJSON`
    - `TestResolvePyPIPackageLatest`
    - `TestResolvePyPIPackageVersionMetadata`
    - `TestResolveNPMPackageMetadata_EnginesNode`
    - `TestRefreshDesiredToolVersionsBestEffort_WritesLatestAndMirrorIntoCopy`
    - `TestRefreshDesiredToolVersionsBestEffort_ReturnsPerToolErrors`
    Coverage:
    - verify HTTP parsing and error handling for any new external endpoints added for implicit tool version resolution
    - verify startup-warning refresh can mutate a config copy without persisting state

### B. Config persistence and copy semantics

Primary files:

- `cooper/internal/config/config_test.go`
- `cooper/internal/app/configure_test.go`

Required scenarios:

1. JSON round-trip persists `implicit_tools`.
   Suggested test:
   - `TestConfigJSONRoundTrip_WithImplicitTools`

2. Empty or missing `implicit_tools` loads cleanly from older config files.
   Suggested test:
   - `TestLoadConfig_MissingImplicitToolsBackwardCompatible`

3. `ConfigureApp.Config()` deep-copies `ImplicitTools` so callers cannot mutate app state.
   Suggested test:
   - `TestConfigureApp_ConfigIsCopy_ImplicitTools`

4. Save-only configure flow does not persist built implicit state.
   Suggested test:
   - `TestConfigureApp_Save_DoesNotPersistImplicitTools`
   Expected behavior:
   - generated Dockerfile may contain resolved implicit package versions
   - `config.json` does not claim they are built yet

5. Save warning plumbing is explicit and stable.
   Suggested tests:
   - `TestConfigureApp_Save_ReturnsWarnings`
   - `TestSaveModel_DoSave_AppendsReturnedWarningsToDoneMessages`
   Coverage:
   - stale-fallback warnings are returned from `ConfigureApp.Save()`
   - the TUI save screen displays them via `doneMsgs`
   - save path does not rely on stderr-only output

### C. Template rendering tests

Primary file:

- `cooper/internal/templates/templates_test.go`

Required scenarios:

1. Base Dockerfile includes the correct install commands when each parent tool is enabled.
   Suggested tests:
   - `TestRenderBaseDockerfile_InstallsGopls`
   - `TestRenderBaseDockerfile_InstallsTypeScriptLanguageServer`
   - `TestRenderBaseDockerfile_InstallsPyright`
   - `TestRenderBaseDockerfile_InstallsPythonLSPServer`

2. Base Dockerfile includes exact pinned versions in commands.
   Suggested tests:
   - `TestRenderBaseDockerfile_GoplsVersionPinned`
   - `TestRenderBaseDockerfile_TypeScriptVersionsPinned`
   - `TestRenderBaseDockerfile_PythonLSPVersionsPinned`

3. Base Dockerfile placement is correct.
   Suggested tests:
   - `TestRenderBaseDockerfile_ImplicitToolInstallsAppearAfterUserSetup`
   - `TestRenderBaseDockerfile_ImplicitToolInstallsAppearBeforeRuntimeProxyEnv`

4. Disabled tools do not leak unrelated installs.
   Suggested tests:
   - `TestRenderBaseDockerfile_GoDisabledOmitsGopls`
   - `TestRenderBaseDockerfile_NodeDisabledOmitsTypeScriptLanguageServer`
   - `TestRenderBaseDockerfile_PythonDisabledOmitsPyrightAndPylsp`
   - `TestRenderBaseDockerfile_PythonOnlyDoesNotInstallTypeScriptLanguageServer`
   Note:
   `pyright` should still be installed for Python-only because it belongs to Python support, but `typescript-language-server` and `typescript` should not.

5. Write-all-templates API passes implicit tool data correctly.
   Suggested tests:
   - `TestWriteAllTemplates_PassesImplicitToolVersionsIntoBaseDockerfile`

### D. Configure save tests

Primary file:

- `cooper/internal/app/configure_test.go`

Required scenarios:

1. Save resolves latest-mode top-level tool versions before writing templates.
   Suggested test:
   - `TestConfigureApp_Save_ResolvesLatestVersionsBeforeTemplateWrite`

2. Save writes templates with concrete implicit package versions.
   Suggested test:
   - `TestConfigureApp_Save_WritesConcreteImplicitToolVersions`

3. Save surfaces compatibility failures.
   Suggested tests:
   - `TestConfigureApp_Save_FailsForUnsupportedGoGoplsCombination`
   - `TestConfigureApp_Save_FailsForUnsupportedNodeTypeScriptLanguageServerCombination`
   - `TestConfigureApp_Save_FailsForUnsupportedPythonPylspCombination`

4. Save falls back to last-known concrete desired versions when fresh latest/mirror refresh fails but a usable fallback exists.
   Suggested tests:
   - `TestConfigureApp_Save_LatestModeFallsBackToExistingPinnedVersionWhenRefreshFails`
   - `TestConfigureApp_Save_MirrorModeFallsBackToExistingHostVersionWhenDetectionFails`
   Coverage:
   - save succeeds
   - generated templates use the last-known concrete desired versions
   - stale fallback warning is returned to the caller/UI

5. Save fails when latest/mirror refresh fails and no usable concrete fallback exists.
   Suggested tests:
   - `TestConfigureApp_Save_LatestModeFailsWithoutFallback`
   - `TestConfigureApp_Save_MirrorModeFailsWithoutFallback`

6. Build remains strict even if save-only allows stale fallback.
   Suggested test:
   - `TestRunBuild_LatestRefreshFailureDoesNotUseStaleFallback`

7. Save updates configure copy text assumptions if the programming tools summary is rendered in tests.
   Suggested snapshot/content tests only if that screen already has UI tests; otherwise manual verification is enough.

### E. Build and update orchestration tests

Primary file:

- `cooper/main_test.go`

Secondary file if shared startup-check logic is extracted there:

- `cooper/internal/app/cooper_test.go`

Required scenarios:

1. Build persists built implicit tools after successful image build.
   Suggested test:
   - `TestRunBuild_PersistsImplicitToolsAfterSuccess`

2. Build does not persist implicit tools if base build fails.
   Suggested test:
   - `TestRunBuild_DoesNotPersistImplicitToolsOnFailure`

3. Build persists successful base state even when a later built-in AI CLI image fails.
   Suggested test:
   - `TestRunBuild_PersistsBaseStateWhenLaterCLIBuildFails`
   Coverage:
   - base `ProgrammingTools[].ContainerVersion` is updated and saved
   - `ImplicitTools` is updated and saved
   - failed child tool `ContainerVersion` is not falsely marked built

4. Build persists successful earlier AI CLI tool state when a later AI CLI image fails.
   Suggested test:
   - `TestRunBuild_PersistsEarlierSuccessfulAIToolStateOnLaterFailure`

5. Update plan refreshes desired versions before implicit resolution, even if container versions already match.
   Suggested tests:
   - `TestCollectUpdatePlan_RefreshesLatestPinnedVersionBeforeImplicitResolution`
   - `TestCollectUpdatePlan_RefreshesMirrorHostVersionBeforeImplicitResolution`
   Coverage:
   - implicit resolution uses freshly refreshed desired state, not stale stored values

6. Update fails if any enabled programming tool needed for implicit resolution cannot be freshly resolved.
   Suggested tests:
   - `TestCollectUpdatePlan_FailsWhenLatestRefreshFailsForEnabledProgrammingTool`
   - `TestCollectUpdatePlan_FailsWhenMirrorRefreshFailsForEnabledProgrammingTool`
   Coverage:
   - update does not fall back to stale desired-version data
   - update does not silently skip implicit comparison in this case

7. Update plan marks base rebuild when implicit tools changed even if top-level programming tool versions did not.
   Suggested tests:
   - `TestCollectUpdatePlan_ImplicitToolMismatchRebuildsBase`
   - `TestCollectUpdatePlan_MissingBuiltImplicitToolsRebuildsBase`
   - `TestCollectUpdatePlan_UnexpectedBuiltImplicitToolsRebuildsBase`

8. Update plan does not rebuild when implicit target exactly matches built state.
   Suggested test:
   - `TestCollectUpdatePlan_ImplicitToolsMatchNoBaseRebuild`

9. Update persists new implicit built state after successful rebuild.
   Suggested test:
   - `TestRunUpdate_PersistsImplicitToolsAfterBaseRebuild`

10. Update persists successful base state even when a later child rebuild fails.
   Suggested test:
   - `TestRunUpdate_PersistsBaseStateWhenLaterCLIBuildFails`

11. Update rebuilds custom CLI images whenever `baseChanged == true`.
   Suggested tests:
   - `TestRunUpdate_BaseChangedRebuildsCustomCLIImages`
   - `TestRunUpdate_BaseChangedRebuildsBuiltInAndCustomCLIImages`
   Coverage:
   - custom `cli/{name}/Dockerfile` images are discovered and rebuilt on base change
   - this happens for implicit-LSP-driven base rebuilds too, not just direct programming-tool version changes

12. Update does not rebuild custom CLI images when only specific built-in AI tool images changed and base did not change.
   Suggested test:
   - `TestRunUpdate_ToolOnlyChangeDoesNotRebuildCustomCLIImages`

13. Update no-op path remains correct when both top-level and implicit tool state match.
   Suggested test:
   - `TestRunUpdate_NoRebuildNeededWhenImplicitToolsMatch`

14. Startup warning computation refreshes a config copy before both top-level and implicit mismatch checks.
   Suggested tests:
   - `TestCheckToolVersions_UsesRefreshedLatestVersionInConfigCopy`
   - `TestCheckToolVersions_UsesRefreshedMirrorVersionInConfigCopy`
   - `TestCheckToolVersions_DoesNotMutateOriginalConfig`

15. Startup mismatch warnings include implicit tools.
   Suggested tests:
   - `TestCheckToolVersions_IncludesImplicitToolMismatches`
   - `TestCheckToolVersions_SkipsImplicitWarningsWhenResolverFails`
   - `TestCheckToolVersions_AppendsRefreshFailureWarningForImplicitParent`
   File placement:
   - if the logic stays in `main.go`, add to `cooper/main_test.go`
   - if the logic is extracted to app/shared code, add corresponding tests there

### F. About tab tests

Primary file:

- new `cooper/internal/tui/about/model_test.go`

Required scenarios:

1. About renders implicit LSP section when LSP entries exist.
   Suggested test:
   - `TestAboutView_RendersImplicitLanguageServersSection`

2. About hides support entries.
   Suggested test:
   - `TestAboutView_OmitsSupportEntries`

3. About shows parent tool and binary.
   Suggested test:
   - `TestAboutView_ShowsImplicitToolParentAndBinary`

4. About still renders startup warning banner with implicit mismatches.
   Suggested test:
   - `TestAboutView_ShowsStartupWarningsForImplicitToolMismatches`

5. About renders correctly with no implicit tools.
   Suggested test:
   - `TestAboutView_NoImplicitToolsSectionWhenEmpty`

### G. Shared Docker-test bootstrap correctness

Primary file:

- `cooper/internal/testdocker/bootstrap.go`

Required scenario:

1. If the implementation adds resolver logic outside `internal/templates`, update the shared image fingerprint inputs so test image caching invalidates when resolver code changes.
   This should be verified by a focused unit test if the bootstrap package already has tests. If it does not, add a small targeted test or a code comment explaining why the fingerprint list was changed and how to maintain it.

Current fingerprint only hashes:

- `.testfiles/config-pinned.json`
- `internal/templates`
- `internal/aclsrc`
- `internal/x11src`

If new resolver code lives in `internal/config` or another package, that path must be added.

### H. End-to-end shell coverage in `test-e2e.sh`

Primary file:

- `cooper/test-e2e.sh`

Required runtime scenarios:

1. All-language config verifies installed language runtimes and all expected implicit tools.
2. `config.json` generated by build includes `implicit_tools` with the built versions.
3. `gopls` executable runs and its output matches `config.json`.
4. `typescript-language-server` executable runs and its output matches `config.json`.
5. `pyright` executable runs and its output matches `config.json`.
6. `pyright-langserver` binary is present when Python is enabled.
7. `pylsp` binary is present when Python is enabled.
8. `python3 -m pip show python-lsp-server` matches `config.json`.
9. Disabled tools do not leak unrelated LSPs.
   Minimum required assertions:
   - Go disabled means no `gopls`
   - Node disabled means no `typescript-language-server`
   - Python disabled means no `pyright`, no `pyright-langserver`, no `pylsp`

If the existing `test-e2e.sh` only runs one all-tools fixture today, add additional fixture scenarios or targeted negative assertions so disabled-tool leakage is covered explicitly.

### I. Full repo test execution phase

After all automated tests are implemented, the implementation agent must run, in this order:

1. `go test ./...`
2. `./test-e2e.sh`
3. `cooper proof`

When running bash or test commands in this repo, follow `AGENTS.md` and write outputs to `/tmp` files.

## Manual Verification Matrix

The prior version of this plan only had a short happy-path manual flow. That was not comprehensive enough. The matrix below is the required manual verification plan.

Run all manual steps against a disposable Cooper directory so config edits used to simulate drift do not affect the operator's real environment.

Recommended manual test workspace setup:

1. Use the real persistent CLI flag, not a hypothetical environment variable. Example: `cooper --config /tmp/cooper-lsp-manual configure` and the same `--config /tmp/cooper-lsp-manual` prefix for `build`, `update`, `up`, `cli`, and `proof`.
2. Capture command output to `/tmp` files.
3. Keep copies of generated `config.json` and `base/Dockerfile` after each scenario.

Unless a step says otherwise, every manual command below should be run with the `--config /tmp/cooper-lsp-manual` prefix.

### MV-01: Save-only generates truthful templates but does not claim tools were built

Purpose:

- verify configure-time resolution is used for template generation
- verify save-only does not persist built implicit state

Steps:

1. Run `cooper --config /tmp/cooper-lsp-manual configure`.
2. Enable Go, Node, and Python.
3. Choose `Latest` for all three programming tools.
4. Select `Save Only`, not build.
5. Inspect generated `/tmp/cooper-lsp-manual/base/Dockerfile`.
6. Inspect `/tmp/cooper-lsp-manual/config.json`.

Expected results:

- `base/Dockerfile` contains concrete versions for `gopls`, `typescript-language-server`, `typescript`, `pyright`, and `python-lsp-server`
- `config.json` does not claim those implicit tools are built yet, or leaves `implicit_tools` empty/unset depending on final implementation

### MV-01b: Save-only offline/stale-fallback behavior

Purpose:

- verify the configure-save behavior is not a blanket hard network dependency

Steps:

1. Using `--config /tmp/cooper-lsp-manual`, perform one successful online save/build so the config contains concrete desired versions for at least one latest-mode or mirror-mode programming tool.
2. Disconnect network access or otherwise force latest/host refresh failure for a subsequent save-only run.
3. Run `cooper --config /tmp/cooper-lsp-manual configure` and save-only again without changing away from Latest/Mirror mode.
4. Inspect the save result, generated `base/Dockerfile`, and any warning output.
5. Repeat the scenario with a fresh config directory that has no prior concrete fallback values.

Expected results:

- with an existing concrete fallback, save-only succeeds
- generated templates use the last-known concrete desired versions
- a clear warning is visible in the configure save result screen via `doneMsgs` and may also be printed to stderr by non-TUI callers
- the warning explains that stale fallback was used and a networked `cooper build` or `cooper update` is required to refresh
- with no existing concrete fallback, save-only fails clearly rather than pretending to have resolved fresh desired versions

### MV-02: Full build with all programming tools

Purpose:

- verify the happy path end-to-end

Steps:

1. Run `cooper --config /tmp/cooper-lsp-manual configure`.
2. Enable Go, Node, and Python.
3. Build.
4. Inspect `config.json`.
5. Start a barrel with `cooper --config /tmp/cooper-lsp-manual up` or a one-shot CLI invocation with the same `--config` prefix.
6. In the barrel, run:
   - `gopls version`
   - `typescript-language-server --version`
   - `tsc --version`
   - `pyright --version`
   - `command -v pyright-langserver`
   - `command -v pylsp`
   - `python3 -m pip show python-lsp-server`

Expected results:

- `config.json` contains `implicit_tools`
- each command succeeds
- reported versions match `config.json`

### MV-03: About tab visibility and copy

Purpose:

- verify user-facing visibility of implicit LSPs

Steps:

1. Start Cooper after a successful full build.
2. Open the About tab.
3. Inspect the rendered sections.

Expected results:

- a dedicated implicit LSP section is visible
- `gopls`, `typescript-language-server`, `pyright`, and `python-lsp-server` are shown
- parent tool and binary columns are shown if the design kept them
- `typescript` support entry is not shown in the About tab

### MV-04: Doctor output includes implicit tooling

Purpose:

- verify runtime diagnostics

Steps:

1. Enter a built barrel.
2. Run `/usr/local/bin/doctor.sh`.

Expected results:

- doctor output includes explicit checks for `gopls`, `typescript-language-server`, `pyright`, `pyright-langserver`, and `python-lsp-server`/`pylsp`
- no false negatives when the corresponding parent tool is enabled

### MV-05: Proof covers implicit tooling

Purpose:

- verify the higher-level integration path

Steps:

1. Run `cooper --config /tmp/cooper-lsp-manual proof` after building with Go, Node, and Python enabled.

Expected results:

- proof succeeds
- proof output explicitly confirms the implicit language servers and related binaries

### MV-06: Go-only configuration does not leak non-Go LSPs

Purpose:

- verify parent-tool isolation

Steps:

1. Configure only Go as enabled.
2. Build.
3. Start a barrel.
4. Run:
   - `gopls version`
   - `typescript-language-server --version || true`
   - `pyright --version || true`
   - `command -v pylsp || true`

Expected results:

- `gopls` exists
- TypeScript and Python LSP tooling does not exist

### MV-07: Node-only configuration does not leak Go or Python LSPs

Purpose:

- verify parent-tool isolation

Steps:

1. Configure only Node as enabled.
2. Build.
3. Start a barrel.
4. Run:
   - `typescript-language-server --version`
   - `tsc --version`
   - `gopls version || true`
   - `pyright --version || true`
   - `command -v pylsp || true`

Expected results:

- TypeScript tooling exists
- Go and Python LSP tooling does not exist

### MV-08: Python-only configuration installs Python LSPs and still supports Pyright via base Node

Purpose:

- verify the special case where Python uses base Node even if the `node` programming tool is disabled

Steps:

1. Configure only Python as enabled.
2. Build.
3. Start a barrel.
4. Run:
   - `pyright --version`
   - `command -v pyright-langserver`
   - `command -v pylsp`
   - `typescript-language-server --version || true`
   - `gopls version || true`

Expected results:

- `pyright`, `pyright-langserver`, and `pylsp` exist
- `typescript-language-server` does not exist
- `gopls` does not exist

### MV-09: Update rebuilds base image when implicit tool drift exists

Purpose:

- verify the main behavioral gap this feature closes

Steps:

1. Build with Go enabled so `gopls` is persisted in `config.json`.
2. Edit `config.json` and change one built implicit tool version to an obviously older value, or remove one implicit tool entry entirely.
3. Run `cooper update`.
4. Inspect stderr and the saved config.

Expected results:

- update reports implicit-tool mismatch
- base rebuild is triggered even if top-level Go/Node/Python versions did not change
- `config.json` is updated to the resolved implicit target set after the rebuild

### MV-09a: Update also rebuilds custom CLI images when base changed

Purpose:

- verify the custom-image behavior called out in review item 1

Steps:

1. Create a simple custom image in `/tmp/cooper-lsp-manual/cli/custom-check/Dockerfile` using `FROM cooper-base`.
2. Make that custom image produce a visible marker derived from the base build, for example by copying a base-installed binary path or running a command that only succeeds if the new base contents are present.
3. Build once successfully.
4. Force an implicit-tool-driven base rebuild condition, for example by editing persisted `implicit_tools` in `config.json` to an older or missing value.
5. Run `cooper update`.
6. Launch the custom CLI image with `cooper --config /tmp/cooper-lsp-manual cli custom-check` and inspect its runtime.

Expected results:

- `cooper update` rebuilds the custom CLI image as part of the base-changed path
- the custom image reflects the new base contents after update
- this happens even though the custom image has no explicit entry in `config.json`

Note:

The important underlying rule is that rebuilding `cooper-base` alone is not enough. Previously built child images still carry the older parent image content until they themselves are rebuilt.

### MV-09b: Update uses refreshed desired versions, not stale stored values

Purpose:

- verify the exact bug class described in review item 2

Steps:

1. Build a working config in mirror or latest mode for at least one programming tool.
2. Edit `config.json` so the stored `host_version` or `pinned_version` for that tool is stale or empty, while leaving `container_version` matching the real desired version.
3. Run `cooper update`.
4. Inspect the resulting generated `base/Dockerfile` and saved `config.json`.

Expected results:

- update refreshes the desired version from the host/latest resolver even though `container_version` already matched before the run
- implicit tool resolution uses the refreshed desired version, not the stale or empty saved value
- no false success based on stale desired-version state

### MV-10: Startup warnings surface implicit mismatch without crashing the app

Purpose:

- verify About/startup warning path

Steps:

1. After building successfully, manually edit `config.json` to make one implicit built version stale.
2. Start Cooper with `cooper --config /tmp/cooper-lsp-manual up`.
3. Open About.

Expected results:

- Cooper starts successfully
- startup warning banner mentions the implicit mismatch
- About shows the warning banner and still renders normally

### MV-10b: Startup warning refresh uses fresh latest-mode desired versions, not stale persisted `PinnedVersion`

Purpose:

- verify the exact latest-mode startup warning bug class from review item 2

Steps:

1. Build successfully with at least one programming tool in latest mode.
2. Manually edit `config.json` to make that tool's persisted `PinnedVersion` stale or empty while leaving the rest of the config intact.
3. Start Cooper with `cooper --config /tmp/cooper-lsp-manual up` while network access is available.
4. Open About and inspect startup warnings.

Expected results:

- startup warning computation refreshes the latest-mode desired version in memory rather than trusting the stale persisted `PinnedVersion`
- any implicit mismatch warning derived from that parent tool matches the freshly refreshed desired version, not the stale saved value
- the original on-disk config remains unchanged by startup warning computation

### MV-11: Compatibility bucket spot-checks in generated Dockerfile

Purpose:

- verify old-version mapping branches that are harder to validate from runtime binaries alone

Steps:

1. Configure an older supported Go version, save-only, inspect `base/Dockerfile` and confirm the expected `gopls` version branch.
2. Configure an older supported Node version, save-only, inspect `base/Dockerfile` and confirm the expected `typescript-language-server` branch.
3. Configure Python `3.8`, `3.7`, and `3.6` in save-only mode one at a time, inspect `base/Dockerfile` and confirm the expected `python-lsp-server` pin.

Expected results:

- the Dockerfile reflects the documented compatibility matrix exactly

Note:

Because Cooper does not truly pin the container Python interpreter today, this verification should inspect generated template content rather than trying to infer bucket behavior only from the runtime interpreter version inside the final image.

### MV-12: Unsupported combinations fail clearly

Purpose:

- verify negative-path UX

Steps:

1. Configure Go below the supported `gopls` floor and attempt save/build.
2. Configure Node below the supported `typescript-language-server` floor and attempt save/build.
3. Configure Python below the supported `python-lsp-server` floor and attempt save/build.

Expected results:

- each attempt fails fast
- the error message names the parent tool, the unsupported version, and the implicit tool that cannot be resolved
- no partial persisted `implicit_tools` state is written claiming success

### MV-13: Partial build failure preserves truthful built state

Purpose:

- verify the exact rule added in response to review item 3

Steps:

1. Configure Go, Node, and Python, plus at least one built-in AI CLI image.
2. Force a later child image build failure after the base image has already succeeded.
   Examples:
   - temporarily break one per-tool Dockerfile after template generation but before build, or
   - inject a failing custom image build after the base image step
3. Run `cooper build` or `cooper update`.
4. Inspect the returned error.
5. Inspect `config.json` after the failed command.

Expected results:

- the command returns an error
- `config.json` still reflects the newly built base state:
  - updated programming-tool `container_version` values
  - updated `implicit_tools`
- any child image that failed is not falsely marked as successfully rebuilt
- any earlier child image that succeeded before the failure remains accurately persisted if the implementation chose per-tool staged persistence

## Required Test Execution Artifacts

The implementation agent should capture and retain these artifacts while validating the feature:

- `go test ./...` output
- `./test-e2e.sh` output
- `cooper proof` output
- at least one generated `config.json`
- at least one generated `base/Dockerfile`
- screenshots or textual notes for the About tab manual verification

These artifacts are not part of the final code change, but they are required during implementation to ensure the feature is actually verified rather than only reasoned about.

## Documentation Updates

Update:

- `cooper/README.md`
- `cooper/REQUIREMENTS.md`

Required README updates:

- Programming Tools section: state that built-in programming tools also install standard language servers
- clarify exact mappings:
  - Go -> `gopls`
  - Node.js / TypeScript -> `typescript-language-server` + `typescript`
  - Python -> `pyright` + `python-lsp-server`
- state that these are implicit and versioned from the selected language runtime
- state that About/startup warnings can surface language-server version mismatches
- optionally note that TypeScript is bundled under Node rather than being a separate programming tool

Required REQUIREMENTS updates:

- Programming Tool Setup flow should explicitly mention automatic standard LSP installation
- About tab should mention showing built implicit language servers
- update behavior should mention that base rebuilds also occur when implicit language-server versions drift

## Suggested Implementation Order

1. Add `ImplicitToolConfig` and `Config.ImplicitTools`.
2. Add deep-copy handling for the new slice in `ConfigureApp.Config()`.
3. Extract shared top-level latest-resolution helper from `main.go` into `internal/config`.
4. Implement `internal/config/lsp.go` with:
   - effective version helpers
   - compatibility mapping
   - external API resolvers
   - compare/filter helpers
5. Change templates API to accept resolved implicit tool state explicitly.
6. Update base Dockerfile template to install implicit tools in the correct location.
7. Wire `ConfigureApp.Save()` to resolve concrete versions before writing templates.
8. Wire `runBuild` to resolve and persist `cfg.ImplicitTools`.
9. Wire `runUpdate` to compare and persist implicit tools.
10. Extend startup mismatch warnings.
11. Extend About tab.
12. Extend proof/doctor/e2e.
13. Update testdocker fingerprint.
14. Update docs.
15. Run unit tests, then e2e.

## Verified Assumptions

### 1. Cooper currently only models `go`, `node`, and `python` as built-in programming tools.

Verified by:

- `cooper/internal/configure/programming.go:52-56`
- `cooper/internal/app/configure.go:27-35`

### 2. TypeScript is not a separate programming tool today.

Verified by:

- the built-in tool lists above
- repo-wide searches found no `typescript` programming-tool model, and no existing TS LSP wiring

### 3. The base image install logic is generated from `cooper/internal/templates/base.Dockerfile.tmpl`.

Verified by:

- `cooper/internal/templates/templates.go:145-159`
- `cooper/internal/templates/templates.go:341-357`

### 4. There are currently no LSP installs or LSP checks in the Cooper codebase.

Verified by:

- repo-wide searches for `gopls`, `pyright`, `typescript-language-server`, `pylsp`, `pyright-langserver`, and `tsserver` in Go sources
- current proof/doctor/e2e code only checks language runtimes

### 5. Node is always installed in the base image even when the `node` programming tool is disabled.

Verified by:

- `cooper/internal/templates/base.Dockerfile.tmpl:79-88`

This is important because Python's `pyright` can rely on a base Node runtime.

### 6. Python version pinning is not actually enforced in the current base image.

Verified by:

- `cooper/internal/templates/base.Dockerfile.tmpl:93-100`

The template comment explicitly says exact version pinning is not supported and installs Debian's default `python3`.

### 7. `cooper build` resolves latest-mode top-level tool versions before generating templates, but `ConfigureApp.Save()` currently does not.

Verified by:

- `cooper/main.go:294-299`
- `cooper/internal/app/configure.go:165-199`

This is a pre-existing inconsistency with the requirements.

### 8. `cooper update` currently only rebuilds the base image when programming tool versions changed.

Verified by:

- `cooper/main.go:1025-1058`
- `cooper/main.go:1110-1113`
- `cooper/main.go:1141-1152`

It does not account for template-only or implicit-tool drift today.

### 9. The About tab only tracks top-level programming and AI tools today.

Verified by:

- `cooper/internal/tui/about/model.go:31-35`
- `cooper/internal/tui/about/model.go:57-60`
- `cooper/internal/tui/about/model.go:169-180`
- `cooper/internal/tui/about/model.go:205-249`

### 10. Startup version warnings are computed before the About tab is created.

Verified by:

- `cooper/main.go:592-699`

This means About can continue to be network-free and render warning strings that were already computed during startup.

### 11. `gopls` has an official Go-version support matrix that must be respected.

Verified from:

- `https://go.dev/gopls/`

Verified compatibility table from the docs:

- Go `1.20` -> final supported `gopls v0.15.3`
- Go `1.18` -> final supported `gopls v0.14.2`
- Go `1.17` -> final supported `gopls v0.11.0`
- Go `1.15` -> final supported `gopls v0.9.5`
- Go `1.12` -> final supported `gopls v0.7.5`

Also verified that the Go module proxy exposes `gopls` latest and version info:

- `https://proxy.golang.org/golang.org/x/tools/gopls/@latest`
- `https://proxy.golang.org/golang.org/x/tools/gopls/@v/v0.15.3.info`

### 12. `typescript-language-server` compatibility varies by Node version.

Verified from npm registry metadata:

- latest `typescript-language-server` is `5.1.3`
- latest requires `node >=20`
- `4.4.1` requires `node >=18`
- `3.3.2` requires `node >=14.17`

Also verified that the package does not declare `dependencies` or `peerDependencies` on `typescript`, and its install docs explicitly tell users to install `typescript-language-server` and `typescript` together.

### 13. `typescript` itself currently supports `node >=14.17`.

Verified from npm registry metadata for:

- `typescript/latest`
- `typescript/5.8.3`
- `typescript/5.4.5`

All verified versions declare `engines.node >=14.17`.

### 14. `pyright` latest currently supports `node >=14.0.0` and exposes both CLI and language-server binaries.

Verified from npm registry metadata for `pyright/1.1.408`:

- `engines.node >=14.0.0`
- `bin.pyright`
- `bin.pyright-langserver`

### 15. `python-lsp-server` has verified release buckets by Python version.

Verified from PyPI metadata:

- latest `1.14.0` requires `Python >=3.9`
- `1.12.2` requires `Python >=3.8`
- `1.7.4` requires `Python >=3.7`
- `1.3.3` requires `Python >=3.6`

### 16. The shared Docker-backed test bootstrap fingerprint will miss resolver changes if new logic lives outside `internal/templates` unless updated.

Verified by:

- `cooper/internal/testdocker/bootstrap.go:457-484`

Current fingerprint inputs do not include `internal/config` or any new resolver package.

### 17. Child CLI Dockerfiles already install npm-based tooling as `USER user`, and the base image already sets up `~/.npm-global` and `~/.local/bin`.

Verified by:

- `cooper/internal/templates/cli-tool.Dockerfile.tmpl:13-22`
- `cooper/internal/templates/base.Dockerfile.tmpl:149-154`

This is the correct model to follow for implicit npm/go/pip installs in the base image.

### 18. The Go base image already prepares a user-writable Go workspace/cache path, so `go install` as `user` is viable.

Verified by:

- `cooper/internal/templates/base.Dockerfile.tmpl:124-130`

### 19. `cooper proof`, `doctor.sh`, and `test-e2e.sh` are the correct end-to-end verification points for this feature.

Verified by current code:

- `cooper/internal/proof/proof.go:413-470`
- `cooper/internal/templates/doctor.sh:268-307`
- `cooper/test-e2e.sh:1540-1628`

They already verify runtime tool availability and are the natural places to extend for implicit language servers.
