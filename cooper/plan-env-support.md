# Cooper Barrel Environment Variable Support

## Intent

This document is the implementation brief for adding user-defined barrel environment variables to Cooper.

The intended behavior is:

1. User defines key/value environment variables in `cooper configure`.
2. Cooper persists them in `~/.cooper/config.json`.
3. Every later `cooper cli <tool>` session loads those user-defined env vars.
4. Cooper-managed and framework-specific env vars are then restored last.
5. Users cannot break Cooper by overriding env vars Cooper relies on.

This plan is intentionally prescriptive. It is written for another implementation agent. The goal is to minimize implementation drift by anchoring the work to the actual current codebase, naming exact files/functions to modify, and spelling out concrete test scenarios and assertions.

## Final Design Summary

The recommended implementation is:

1. Add a new persisted config field `Config.BarrelEnvVars []BarrelEnvVar` in `cooper/internal/config`.
2. Add strict validation for configure/save paths in `cooper/internal/config`.
3. Add tolerant runtime sanitization for `cooper cli` so hand-edited bad config does not break CLI startup.
4. Add a new configure screen `Barrel Environment` implemented using the same screen-model pattern already used by whitelist and port forwarding.
5. Do not inject user env into image templates or `docker run`.
6. Apply user env only at `cooper cli` session start using a generated per-session env file plus a small bash wrapper.
7. Preserve existing token forwarding via `auth.ResolveTokens(...)` and `docker exec -e`.
8. Restore Cooper/framework/token env vars after sourcing the user env file.
9. Reuse the same env-wrapper helper from `main.go`, `proof.go`, and Docker-backed smoke tests so there is one source of runtime behavior.
10. Make v1 scope explicit: barrel env vars are global across all Cooper barrels, tools, and workspaces because they are configured in global `~/.cooper/config.json`.

## Hard Constraints

These are design constraints, not suggestions.

1. Do not bake user-defined barrel env vars into generated Dockerfiles.
2. Do not inject user-defined barrel env vars in `docker.StartBarrel(...)`.
3. Do not persist auth tokens through this feature.
4. Do not weaken or remove existing token resolution logic in `cooper/internal/auth/resolve.go`.
5. Do not make `cooper cli` require a rebuild or barrel restart to pick up new env values.
6. Do not turn malformed manually edited env config into a hard CLI failure unless the error is truly unrecoverable. Prefer runtime warning + skip for malformed user env entries.
7. Do not silently drop invalid env entries inside `cooper configure` models when loading existing config. Users must be able to see/edit/delete bad entries they hand-edited into `config.json`.
8. Do not duplicate runtime env precedence logic in multiple places. Put it behind one reusable helper package.
9. Do not add barrel-env strict validation to generic `(*Config).Validate()`. That method is reused by runtime mutations like `CooperApp.UpdatePortForwards(...)` and `CooperApp.UpdateSettings(...)`, and those flows must not start failing because of unrelated hand-edited barrel env entries.

## Existing Code Anchors

This section captures the concrete current code shape the implementation must fit.

## 1. Config lives in `cooper/internal/config/config.go`

Current `Config` shape:

```go
type Config struct {
	ProgrammingTools    []ToolConfig         `json:"programming_tools"`
	AITools             []ToolConfig         `json:"ai_tools"`
	ImplicitTools       []ImplicitToolConfig `json:"implicit_tools"`
	WhitelistedDomains  []DomainEntry        `json:"whitelisted_domains"`
	PortForwardRules    []PortForwardRule    `json:"port_forward_rules"`
	ProxyPort           int                  `json:"proxy_port"`
	BridgePort          int                  `json:"bridge_port"`
	MonitorTimeoutSecs  int                  `json:"monitor_timeout_secs"`
	BlockedHistoryLimit int                  `json:"blocked_history_limit"`
	AllowedHistoryLimit int                  `json:"allowed_history_limit"`
	BridgeLogLimit      int                  `json:"bridge_log_limit"`
	BridgeRoutes        []BridgeRoute        `json:"bridge_routes"`
	ClipboardTTLSecs    int                  `json:"clipboard_ttl_secs"`
	ClipboardMaxBytes   int                  `json:"clipboard_max_bytes"`
	BaseNodeVersion     string               `json:"base_node_version,omitempty"`
	BarrelSHMSize       string               `json:"barrel_shm_size"`
}
```

Existing default and load paths to extend:

- `LoadConfig(path string)`
- `(*Config).applyMissingDefaults()`
- `DefaultConfig()`
- `CloneConfig(cfg *Config)`
- `(*Config).Validate()`

Implementation implication:

- `BarrelEnvVars` belongs in this struct and must participate in defaults, load compatibility, cloning, and validation.

## 2. Configure app boundary is `cooper/internal/app/configure.go`

Current setter pattern:

```go
func (a *ConfigureApp) SetProgrammingTools(tools []config.ToolConfig) {
	a.cfg.ProgrammingTools = append([]config.ToolConfig(nil), tools...)
}

func (a *ConfigureApp) SetWhitelistedDomains(domains []config.DomainEntry) {
	a.cfg.WhitelistedDomains = append([]config.DomainEntry(nil), domains...)
}

func (a *ConfigureApp) SetPortForwardRules(rules []config.PortForwardRule) {
	a.cfg.PortForwardRules = append([]config.PortForwardRule(nil), rules...)
}
```

Implementation implication:

- Add `SetBarrelEnvVars([]config.BarrelEnvVar)` here, using the same defensive-copy style.

## 3. Configure wizard is a hub model in `cooper/internal/configure/configure.go`

Current screen enum:

```go
const (
	ScreenWelcome Screen = iota
	ScreenProgramming
	ScreenAICLI
	ScreenWhitelist
	ScreenPortForward
	ScreenProxy
	ScreenSave
)
```

Current sub-model wiring:

```go
type model struct {
	...
	welcome     welcomeModel
	programming programmingModel
	aicli       aicliModel
	whitelist   whitelistModel
	portForward portFwdModel
	proxySetup  proxyModel
	save        saveModel
}
```

Current config sync point:

```go
func (m *model) syncConfigFromSubModels() {
	m.cfg.ProgrammingTools = m.programming.toToolConfigs()
	m.cfg.AITools = m.aicli.toToolConfigs()
	m.cfg.WhitelistedDomains = m.whitelist.toDomainEntries()
	m.cfg.PortForwardRules = m.portForward.toPortForwardRules()
	m.cfg.ProxyPort = m.proxySetup.proxyPort
	m.cfg.BridgePort = m.proxySetup.bridgePort
	m.cfg.BarrelSHMSize = m.proxySetup.shmSize
}
```

Current welcome menu items:

```go
items: []welcomeItem{
	{label: "Programming Tools", desc: "Go, Node.js, Python"},
	{label: "AI CLI Tools", desc: "Claude Code, Copilot, Codex, OpenCode"},
	{label: "Proxy Whitelist", desc: "Domain whitelist for network access"},
	{label: "Port Forwarding to Host", desc: "Route container ports to host services"},
	{label: "Proxy Settings", desc: "Proxy port, bridge port"},
	{label: "Save & Build", desc: "Write config, build images"},
}
```

Implementation implication:

- A new screen must be added at the root-model level, not bolted onto save/proxy as a side panel.
- Numeric shortcuts and welcome index dispatch must be updated.
- `isTextInputActive()` must be updated for the new modal state.

## 4. Barrel runtime env is layered today

Current container-start env injection in `cooper/internal/docker/barrel.go`:

```go
args = append(args,
	"-e", fmt.Sprintf("HTTP_PROXY=http://%s:%d", ProxyHost(), cfg.ProxyPort),
	"-e", fmt.Sprintf("HTTPS_PROXY=http://%s:%d", ProxyHost(), cfg.ProxyPort),
	"-e", "NO_PROXY=localhost,127.0.0.1",
	"-e", fmt.Sprintf("COOPER_PROXY_HOST=%s", ProxyHost()),
	"-e", fmt.Sprintf("COOPER_INTERNAL_NETWORK=%s", InternalNetworkName()),
)

args = append(args,
	"-e", "DISPLAY=127.0.0.1:99",
	"-e", "XAUTHORITY=/home/user/.cooper-clipboard.xauth",
	"-e", "COOPER_CLIPBOARD_DISPLAY=127.0.0.1:99",
	"-e", "COOPER_CLIPBOARD_XAUTHORITY=/home/user/.cooper-clipboard.xauth",
)

args = append(args,
	"-e", "PLAYWRIGHT_BROWSERS_PATH=/home/user/.cache/ms-playwright",
)
```

Current per-barrel tmp mount in `cooper/internal/docker/barrel.go`:

```go
barrelTmpDir := filepath.Join(cooperDir, "tmp", containerName)
args = append(args, "-v", barrelTmpDir+":/tmp:rw")
```

Current tmp-root helper in `cooper/internal/docker/tmpdir.go`:

```go
func BarrelTmpRoot(cooperDir string) string {
	return filepath.Join(cooperDir, "tmp")
}
```

Implementation implication:

- Reuse the host-backed barrel tmp directory for per-session env files.
- Add `BarrelTmpDir(cooperDir, containerName string)` helper so `main.go` and tests do not duplicate path joining.

## 5. `cooper cli` today only forwards token/IDE envs per session

Current `runCLI(...)` skeleton in `main.go`:

```go
cfg, cooperDir, err := loadConfig()
tokens, err := auth.ResolveTokens(workspaceDir, cooperDir, []string{toolName})
containerName := docker.BarrelContainerName(workspaceDir, toolName)
...
sessionName := names.Generate(workspaceDir)
...
var envArgs []string
for _, t := range tokens {
	envArgs = append(envArgs, fmt.Sprintf("%s=%s", t.Name, t.Value))
}

var execCmd []string
if cliOneShot != "" {
	execCmd = []string{"bash", "-c", cliOneShot}
} else {
	execCmd = []string{"bash", "-l"}
}

interactive := cliOneShot == ""
if err := docker.ExecBarrel(containerName, execCmd, envArgs, interactive); err != nil {
	return fmt.Errorf("exec barrel: %w", err)
}
```

Implementation implication:

- This is the correct integration point.
- Do not change the barrel-start path for this feature.
- Replace the direct `execCmd` with a wrapper command built by a reusable helper.

## 6. Token/IDE forwarding source of truth is `cooper/internal/auth/resolve.go`

Current token outputs and VS Code env vars:

```go
var toolTokenDefs = map[string][]tokenDef{
	"copilot": {{envVars: []string{"GH_TOKEN", "GITHUB_TOKEN"}, outputName: "GH_TOKEN", ...}},
	"codex":   {{envVars: []string{"OPENAI_API_KEY"}, outputName: "OPENAI_API_KEY"}},
}

var vsCodeEnvVars = []string{
	"TERM",
	"TERM_PROGRAM",
	"TERM_PROGRAM_VERSION",
	"CLAUDE_CODE_SSE_PORT",
	"CLAUDE_CODE_ENTRYPOINT",
	"ENABLE_IDE_INTEGRATION",
}
```

Implementation implication:

- Protected runtime env restoration must include the actual token names returned by `ResolveTokens(...)`.
- Do not hardcode only the current token set if the helper can instead accept dynamic names from `tokens`.

## 7. There is already Docker-backed CLI test coverage in `main_test.go`

Current helpers already available:

- `setupCommandDriver(...)`
- `withCommandGlobals(...)`
- `captureCommandIO(...)`

Current runtime test example:

```go
cliOneShot = "printf cli-ok"
stdout, stderr, err := captureCommandIO(t, "", func() error {
	return runCLI(nil, []string{"claude"})
})
```

Implementation implication:

- Add CLI feature integration tests in `main_test.go` using this same style.
- Do not create a separate ad-hoc Docker bootstrap path.

## V1 Scope Decision

This plan explicitly chooses global scope for v1.

- `Config.BarrelEnvVars` is one top-level list in global `~/.cooper/config.json`.
- The configure UI should describe these as global values applied to every `cooper cli` session.
- Per-tool or per-workspace env scoping is a future feature, not part of this implementation.

## Explicit Non-Goals

These are out of scope for this feature and should not be added unless the user later asks for them.

1. Per-tool env vars.
2. Per-workspace env vars.
3. Secret-store integration.
4. Multiline env values.
5. Env-file import/export from disk.
6. Template-time or build-time environment injection.
7. Arbitrary shell interpolation or template expansion inside values.

## Recommended File Layout

Use the following file layout unless there is a compelling reason not to. It keeps the logic discoverable and testable.

### Existing files to modify

- `cooper/internal/config/config.go`
- `cooper/internal/app/configure.go`
- `cooper/internal/configure/configure.go`
- `cooper/internal/configure/save.go`
- `cooper/internal/docker/tmpdir.go`
- `cooper/main.go`
- `cooper/internal/proof/proof.go`
- `cooper/README.md`
- `cooper/REQUIREMENTS.md`

### New files to add

- `cooper/internal/config/barrel_env.go`
- `cooper/internal/config/barrel_env_test.go`
- `cooper/internal/configure/barrelenv.go`
- `cooper/internal/barrelenv/files.go`
- `cooper/internal/barrelenv/script.go`
- `cooper/internal/barrelenv/files_test.go`
- `cooper/internal/barrelenv/script_test.go`
- `cooper/internal/testdriver/barrelenv_smoke.go`

If the implementor chooses different filenames, keep the same logical boundaries:

1. Config types/validation.
2. Session env-file prep/rendering.
3. Session wrapper-command building.
4. Configure screen model.

## Data Model Design

## Type Definition

Add in `cooper/internal/config/barrel_env.go`:

```go
package config

type BarrelEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
```

Add to `Config`:

```go
BarrelEnvVars []BarrelEnvVar `json:"barrel_env_vars"`
```

## Defaulting

Update `DefaultConfig()` so it returns:

```go
BarrelEnvVars: []BarrelEnvVar{},
```

Update `applyMissingDefaults()` so missing JSON fields are normalized to an empty slice:

```go
if c.BarrelEnvVars == nil {
	c.BarrelEnvVars = []BarrelEnvVar{}
}
```

## Clone Behavior

Update `CloneConfig(...)` to deep-copy the env slice:

```go
cp.BarrelEnvVars = append([]BarrelEnvVar(nil), cfg.BarrelEnvVars...)
```

## Key Rules

Key rules must be owned by `cooper/internal/config`, not by the TUI.

Recommended helper constants/functions:

```go
var barrelEnvNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func ValidateBarrelEnvVars(vars []BarrelEnvVar) error
func CanonicalizeBarrelEnvVars(vars []BarrelEnvVar) []BarrelEnvVar
func NormalizeBarrelEnvVarsForRuntime(vars []BarrelEnvVar) ([]BarrelEnvVar, []string)
func IsProtectedBarrelEnvName(name string) bool
func ProtectedBarrelEnvNames() []string
```

## Key Validation Semantics

Strict configure/save validation must do the following.

1. `ValidateBarrelEnvVars(...)` must be pure and non-mutating.
2. `CanonicalizeBarrelEnvVars(...)` must return a copied slice with `Name` trimmed and `Value` unchanged.
3. Strict validation must validate the trimmed form of `Name`.
4. Reject empty key.
5. Reject keys that do not match `^[A-Za-z_][A-Za-z0-9_]*$`.
6. Reject duplicate keys case-insensitively.
7. Reject protected keys.
8. Reject values containing `NUL`, newline, or carriage return.
9. Allow empty values.

### Important nuance

- Canonicalization happens in the configure/UI/app save path and in runtime normalization on a copy.
- Validation itself must not mutate the input slice.
- `Name` canonicalization is `strings.TrimSpace(name)`.
- `Value` must remain exact because spaces can be meaningful.

## Strict Validation Boundary

Strict barrel-env validation must not be wired into generic `(*Config).Validate()`.

Why:

- `cooper/internal/app/cooper.go` currently reuses `candidate.Validate()` in runtime mutation flows such as:
  - `CooperApp.UpdatePortForwards(...)`
  - `CooperApp.UpdateSettings(...)`
- If `(*Config).Validate()` starts rejecting invalid barrel env entries, those unrelated runtime actions would fail for users who hand-edited bad barrel env config.

Required behavior:

1. Keep `(*Config).Validate()` focused on runtime-safe, generic config invariants.
2. Call `ValidateBarrelEnvVars(...)` only in configure/persist paths.
3. Runtime CLI session startup uses tolerant normalization, warning, and skip behavior instead of strict failure.

Recommended strict-validation call sites:

- `(*ConfigureApp).Validate()`
- `(*ConfigureApp).Save()`
- barrel-env modal save path in the configure TUI

## Protected Name Policy

There are two related but distinct concepts.

### 1. Protected names for configure/save

These are names users may not define.

Protected prefix:

- `COOPER_`

Protected exact names:

- `HTTP_PROXY`
- `HTTPS_PROXY`
- `NO_PROXY`
- `http_proxy`
- `https_proxy`
- `no_proxy`
- `ALL_PROXY`
- `all_proxy`
- `DISPLAY`
- `XAUTHORITY`
- `PLAYWRIGHT_BROWSERS_PATH`
- `PATH`
- `HOME`
- `USER`
- `LOGNAME`
- `SHELL`
- `NODE_EXTRA_CA_CERTS`
- `NPM_CONFIG_PREFIX`
- `GOPATH`
- `GOMODCACHE`
- `GOCACHE`
- `OPENAI_API_KEY`
- `GH_TOKEN`
- `GITHUB_TOKEN`
- `TERM`
- `TERM_PROGRAM`
- `TERM_PROGRAM_VERSION`
- `CLAUDE_CODE_SSE_PORT`
- `CLAUDE_CODE_ENTRYPOINT`
- `ENABLE_IDE_INTEGRATION`
- `CLAUDECODE`

This list should live in one file and one source of truth.

### 2. Protected runtime names to restore after sourcing user env

This set must include:

1. All infra/runtime names that already exist in the shell process.
2. All dynamic token/IDE env names returned by `auth.ResolveTokens(...)`.

Recommended static runtime list:

- `HTTP_PROXY`
- `HTTPS_PROXY`
- `NO_PROXY`
- `http_proxy`
- `https_proxy`
- `no_proxy`
- `ALL_PROXY`
- `all_proxy`
- `PATH`
- `HOME`
- `USER`
- `LOGNAME`
- `SHELL`
- `DISPLAY`
- `XAUTHORITY`
- `PLAYWRIGHT_BROWSERS_PATH`
- `NODE_EXTRA_CA_CERTS`
- `NPM_CONFIG_PREFIX`
- `GOPATH`
- `GOMODCACHE`
- `GOCACHE`
- `COOPER_PROXY_HOST`
- `COOPER_INTERNAL_NETWORK`
- `COOPER_CLIPBOARD_DISPLAY`
- `COOPER_CLIPBOARD_XAUTHORITY`
- `COOPER_CLIPBOARD_ENABLED`
- `COOPER_CLIPBOARD_BRIDGE_URL`
- `COOPER_CLIPBOARD_TOKEN_FILE`
- `COOPER_CLIPBOARD_SHIMS`
- `COOPER_CLI_TOOL`
- `COOPER_CLI_AUTO_APPROVE`
- `COOPER_CLIPBOARD_MODE`

Then union this with every `TokenResult.Name` returned from `auth.ResolveTokens(...)`.

## Runtime Tolerance Policy

This is an important anti-drift requirement.

`cooper configure` and `ConfigureApp.Save()` must reject invalid barrel env entries.

`cooper cli` must not hard-fail just because `config.json` was manually edited to include:

- malformed names
- protected names
- values containing newline/NUL

Instead, runtime must:

1. sanitize the list
2. skip unsafe entries
3. print warnings to stderr
4. continue launching the session

Reason:

- Cooper should remain usable even if a user hand-edited invalid barrel env config.
- This protects the runtime from implementation drift and from bad hand-edited config.

Recommended runtime sanitizer behavior:

```go
func NormalizeBarrelEnvVarsForRuntime(vars []BarrelEnvVar) ([]BarrelEnvVar, []string)
```

Behavior:

1. Preserve order of valid entries.
2. Trim key whitespace.
3. Skip invalid/protected entries with warnings.
4. Do not fail for duplicates; preserve order and let normal shell export semantics make the last valid duplicate win.
5. Do not modify values except rejecting newline/NUL/CR.

Reason for runtime duplicate handling:

- Configure already rejects duplicates.
- Hand-edited duplicates are not dangerous; shell export order is deterministic.

## Session Env Helper Package

Create `cooper/internal/barrelenv` so `main.go`, `proof.go`, and smoke tests can use the same behavior.

Recommended functions:

```go
package barrelenv

type SessionEnvFile struct {
	HostPath      string
	ContainerPath string
}

func PrepareSessionEnvFile(cooperDir, containerName, sessionName string, vars []config.BarrelEnvVar) (SessionEnvFile, []string, error)
func RemoveSessionEnvFile(path string) error
func RenderUserEnvFile(vars []config.BarrelEnvVar) ([]byte, error)
func ProtectedRuntimeEnvNames(extra []string) []string
func BuildExecWrapperCommand(userEnvFile string, protectedNames []string, targetCmd []string) ([]string, error)
```

These helpers should be small and test-heavy.

## Env File Rendering

`RenderUserEnvFile(...)` must render literal shell-safe exports.

Recommended output format:

```bash
# Generated by cooper cli. User barrel env vars only.
export API_BASE_URL='https://internal.example.com'
export EMPTY=''
export QUOTE_TEST='it'"'"'s fine'
```

Use shell single-quote escaping, not Go string quoting.

Recommended escaping strategy:

```go
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
```

Do not use `strconv.Quote(...)` for shell escaping.

## Env File Preparation

`PrepareSessionEnvFile(...)` should:

1. call `config.NormalizeBarrelEnvVarsForRuntime(...)`
2. if no usable vars remain, return `SessionEnvFile{}` plus warnings and no error
3. create `docker.BarrelTmpDir(cooperDir, containerName)` if needed
4. write `cooper-cli-env-{sessionName}.sh`
5. write with mode `0600`
6. return both host path and container path

Recommended path format:

- Host: `filepath.Join(docker.BarrelTmpDir(cooperDir, containerName), "cooper-cli-env-"+sessionName+".sh")`
- Container: `"/tmp/cooper-cli-env-" + sessionName + ".sh"`

Add a new helper in `cooper/internal/docker/tmpdir.go`:

```go
func BarrelTmpDir(cooperDir, containerName string) string {
	return filepath.Join(BarrelTmpRoot(cooperDir), containerName)
}
```

Then update `cooper/internal/docker/barrel.go` to use `BarrelTmpDir(...)` instead of joining the path inline.

## Wrapper Command Builder

`BuildExecWrapperCommand(...)` must return the exact argv passed to `docker.ExecBarrel(...)`.

The helper should build a command like:

```go
[]string{
	"bash", "-c", wrapperScript,
	"cooper-env-wrapper",
	userEnvFile,
	"bash", "-l",
}
```

for interactive sessions, and:

```go
[]string{
	"bash", "-c", wrapperScript,
	"cooper-env-wrapper",
	userEnvFile,
	"bash", "-c", cliOneShot,
}
```

for one-shot sessions.

### Critical anti-drift rule

Do not interpolate `cliOneShot` into the wrapper script string.

Pass it as its own argv element so host-side shell quoting is not reintroduced.

The outer wrapper shell must be non-login (`bash -c`), not login (`bash -lc`).

Why:

- Current `cooper cli` behavior is one-shot `bash -c` and interactive `bash -l`.
- An outer login shell would source startup files one extra time, changing PATH, producing side effects, and potentially polluting stdout/stderr.
- A non-login outer wrapper preserves current shell semantics while still letting the final target command be `bash -l` or `bash -c` as it is today.

## Wrapper Script Semantics

The wrapper script must:

1. receive `$1` as the user env file path
2. `shift`
3. snapshot protected env names from the current wrapper process environment
4. source the user env file if present
5. restore protected env names, preserving set-vs-unset state
6. `exec "$@"`

The set-vs-unset behavior is important. If a protected env is not set in the wrapper process, the restore phase must `unset` it rather than exporting an empty string.

Recommended script structure:

```bash
set -euo pipefail

user_env_file="$1"
shift

# generated snapshot section per protected env name
# generated restore section per protected env name

if [[ -n "$user_env_file" && -f "$user_env_file" ]]; then
  # shellcheck source=/dev/null
  . "$user_env_file"
fi

# generated restore section here

exec "$@"
```

Recommended generated snapshot/restore per name:

```bash
__cooper_has_HTTP_PROXY=0
if [[ ${HTTP_PROXY+x} ]]; then
  __cooper_has_HTTP_PROXY=1
  __cooper_val_HTTP_PROXY="$HTTP_PROXY"
fi

...

if [[ $__cooper_has_HTTP_PROXY -eq 1 ]]; then
  export HTTP_PROXY="$__cooper_val_HTTP_PROXY"
else
  unset HTTP_PROXY
fi
```

This is verbose, but it is explicit, testable, and avoids surprising bash behavior.

## Runtime Integration in `main.go`

Modify `runCLI(...)` in place. Do not add a second CLI code path.

Recommended flow after change:

1. Keep steps 1-9 exactly as they are today.
2. After `sessionName := names.Generate(workspaceDir)`, prepare the session env file.
3. Build `envArgs` from `tokens` exactly as today.
4. Build the protected runtime env-name list from:
   - static infra list
   - dynamic `tokens[i].Name`
5. Build `targetCmd`:
   - interactive: `[]string{"bash", "-l"}`
   - one-shot: `[]string{"bash", "-c", cliOneShot}`
6. Build `execCmd` using `barrelenv.BuildExecWrapperCommand(...)`.
7. Execute using the existing `docker.ExecBarrel(...)`.
8. Remove the env file after exec returns.
9. Print runtime sanitizer warnings to stderr before launching the shell.

### Recommended insertion sketch

```go
sessionName := names.Generate(workspaceDir)
defer names.Release(sessionName)

sessionEnvFile, warnings, err := barrelenv.PrepareSessionEnvFile(cooperDir, containerName, sessionName, cfg.BarrelEnvVars)
if err != nil {
	return fmt.Errorf("prepare barrel env session file: %w", err)
}
if sessionEnvFile.HostPath != "" {
	defer barrelenv.RemoveSessionEnvFile(sessionEnvFile.HostPath)
}
for _, warning := range warnings {
	fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
}

var envArgs []string
var tokenNames []string
for _, t := range tokens {
	envArgs = append(envArgs, fmt.Sprintf("%s=%s", t.Name, t.Value))
	tokenNames = append(tokenNames, t.Name)
}

targetCmd := []string{"bash", "-l"}
if cliOneShot != "" {
	targetCmd = []string{"bash", "-c", cliOneShot}
}

execCmd, err := barrelenv.BuildExecWrapperCommand(
	sessionEnvFile.ContainerPath,
	barrelenv.ProtectedRuntimeEnvNames(tokenNames),
	targetCmd,
)
if err != nil {
	return fmt.Errorf("build barrel env exec command: %w", err)
}

interactive := cliOneShot == ""
if err := docker.ExecBarrel(containerName, execCmd, envArgs, interactive); err != nil {
	return fmt.Errorf("exec barrel: %w", err)
}
```

### Important behavior preservation

This preserves:

- existing token resolution
- existing one-shot final shell semantics (`bash -c` remains the only login/startup-sensitive shell involved in one-shot execution)
- existing interactive shell semantics (`bash -l`)
- existing barrel reuse behavior

## Configure Screen Design

Add a new screen implemented in `cooper/internal/configure/barrelenv.go`.

Recommended screen name:

- `Barrel Environment`

Recommended welcome description:

- `Global env vars for every cooper cli session`

## Root-Model Wiring Changes

Update in `cooper/internal/configure/configure.go`:

1. Add `ScreenBarrelEnv` to the enum, between `ScreenProxy` and `ScreenSave`.
2. Add `barrelEnv barrelEnvModel` to the top-level model struct.
3. Initialize it in `newModel(...)` with `cfg.BarrelEnvVars`.
4. Update `syncConfigFromSubModels()` with `m.cfg.BarrelEnvVars = m.barrelEnv.toEntries()`.
5. Update `Update(...)` dispatcher with `case ScreenBarrelEnv:`.
6. Update `View()` dispatcher with `case ScreenBarrelEnv:`.
7. Update `isTextInputActive()` to report true when the barrel env modal is open.
8. Update welcome-screen dispatch indices and numeric shortcuts.
9. Update welcome menu item count from 6 to 7.

Recommended new screen order:

1. Programming Tools
2. AI CLI Tools
3. Proxy Whitelist
4. Port Forwarding to Host
5. Proxy Settings
6. Barrel Environment
7. Save & Build

## Barrel Environment Screen Model

Follow the same architectural style as `whitelistModel` and `portModal`.

Recommended types:

```go
type barrelEnvResult int

const (
	barrelEnvNone barrelEnvResult = iota
	barrelEnvBack
)

type barrelEnvModal struct {
	active     bool
	editing    bool
	editIndex  int
	keyInput   textInput
	valueInput textInput
	focusField int // 0=key, 1=value, 2=save, 3=cancel
	err        string
}

type barrelEnvModel struct {
	entries       []config.BarrelEnvVar
	cursor        int
	modal         barrelEnvModal
	scrollOffset  int
	lastHeight    int
	lastMaxScroll int
}
```

Recommended methods:

```go
func newBarrelEnvModel(entries []config.BarrelEnvVar) barrelEnvModel
func (m *barrelEnvModel) update(msg tea.Msg) barrelEnvResult
func (m *barrelEnvModel) view(width, height int) string
func (m *barrelEnvModel) toEntries() []config.BarrelEnvVar
```

## UI Behavior Requirements

The screen must support:

- `n` add
- `e` edit
- `Enter` edit current row
- `x` delete
- `Esc` back
- scroll behavior consistent with other screens

Modal behavior:

- key is required
- key is trimmed before save
- value may be empty
- value is not trimmed
- validation error shown inline in modal
- `Esc` closes modal without saving

## Handling Existing Invalid Entries in UI

This is important.

If an existing `config.json` contains invalid or protected barrel env entries, the screen should still load and render them. Do not sanitize them away during model initialization.

Recommended behavior:

1. Render them as regular rows.
2. Optionally add a `Status` column showing `invalid` or `protected`.
3. Allow user to edit or delete them.
4. Saving the modal should use strict validation for the row being edited, but must not fail just because some other rows elsewhere in the table are still invalid.
5. Modal-save validation must therefore be row-scoped plus conflict-aware:
   - validate the edited row itself after canonicalizing its key
   - reject duplicate-key conflicts against the other rows
   - do not reject because some unrelated row elsewhere is malformed or protected
6. Saving the overall config should still fail with a clear error until all invalid rows are fixed or removed.

This gives users a recovery path from hand-edited bad config.

## Save Screen Changes

Update `cooper/internal/configure/save.go`.

### `doSave()`

Add:

```go
m.configureApp.SetBarrelEnvVars(m.cfg.BarrelEnvVars)
```

### Summary view

Add a line:

```text
Barrel Environment:  N entries
```

### Post-save messaging

Current save messaging is image-centric:

```go
m.doneMsgs = append(m.doneMsgs, "Configuration saved. Run 'cooper build' to rebuild images.")
```

Update it so runtime-only settings are clearly called out.

Recommended replacement:

- `Configuration saved.`
- `Runtime-only settings, including barrel environment variables, apply on the next 'cooper cli' session.`
- `Run 'cooper build' only if you changed image-affecting settings.`
- `Base image rebuilds if programming tool or implicit language-server versions changed.`
- `AI tool images rebuild independently.`

## Proof / Smoke Reuse Requirement

Do not re-implement session env precedence separately in `proof.go`.

Instead:

1. add a reusable `cooper/internal/barrelenv` helper package
2. use it from `main.go`
3. use the same helper from `cooper/internal/proof/proof.go`
4. use the same helper from Docker-backed smoke tests

Reason:

- If `proof` verifies a different code path than `cooper cli`, it becomes paper coverage.

## `cooper proof` Plan

Current proof helpers are:

```go
func dockerExec(container, shellCmd string) (string, error)
func dockerExecWithEnv(container, shellCmd string, envArgs []string) (string, error)
```

For this feature, extend proof using the shared wrapper logic rather than a plain `docker exec bash -c ...` call.

Recommended implementation:

1. create session env file using `barrelenv.PrepareSessionEnvFile(...)`
2. use a unique session name generated with the same `names.Generate(...)` / `names.Release(...)` pattern used by `runCLI(...)`
3. build exec wrapper cmd using `barrelenv.BuildExecWrapperCommand(...)`
4. run it via `docker exec` with the same env args style as CLI
5. assert configured env vars are visible
6. assert protected env still has Cooper values

## File-by-File Implementation Checklist

## `cooper/internal/config/barrel_env.go`

Implement:

1. `BarrelEnvVar` type.
2. name regex.
3. protected-name helpers.
4. strict validator.
5. tolerant runtime normalizer.

## `cooper/internal/config/config.go`

Implement:

1. add `BarrelEnvVars` to `Config`
2. add default empty slice in `DefaultConfig()`
3. add missing-default fill-in in `applyMissingDefaults()`
4. add deep-copy in `CloneConfig()`
5. add defaults/load-compat/clone support for `BarrelEnvVars`
6. do not call `ValidateBarrelEnvVars(...)` from `(*Config).Validate()`

## `cooper/internal/app/configure.go`

Implement:

1. `SetBarrelEnvVars([]config.BarrelEnvVar)`
2. update `(*ConfigureApp).Validate()` to call both:
   - `a.cfg.Validate()`
   - `config.ValidateBarrelEnvVars(...)` on a canonicalized copy
3. update `(*ConfigureApp).Save()` to:
   - canonicalize `BarrelEnvVars` on a copy
   - run strict barrel-env validation there
   - assign the canonicalized slice back into in-memory `a.cfg.BarrelEnvVars` on successful save
   - persist the canonicalized values to `config.json`

## `cooper/internal/configure/barrelenv.go`

Implement the new screen model.

## `cooper/internal/configure/configure.go`

Implement:

1. screen enum update
2. model field update
3. new-model initialization
4. welcome menu update
5. welcome numeric shortcuts update
6. update/view dispatch
7. `syncConfigFromSubModels()` update
8. `isTextInputActive()` update

## `cooper/internal/configure/save.go`

Implement:

1. `SetBarrelEnvVars(...)` call in `doSave()`
2. summary line for env count
3. runtime-only save messaging

## `cooper/internal/docker/tmpdir.go`

Implement:

1. `BarrelTmpDir(cooperDir, containerName string) string`

## `cooper/internal/docker/barrel.go`

Implement:

1. use `BarrelTmpDir(...)` where `/tmp` mount path is built today

No other barrel runtime changes should be needed for this feature.

## `cooper/internal/barrelenv/files.go`

Implement:

1. `SessionEnvFile`
2. `PrepareSessionEnvFile(...)`
3. `RemoveSessionEnvFile(...)`
4. file permission handling

## `cooper/internal/barrelenv/script.go`

Implement:

1. shell-quote helper for values
2. `RenderUserEnvFile(...)`
3. `ProtectedRuntimeEnvNames(...)`
4. wrapper script builder
5. `BuildExecWrapperCommand(...)`

## `cooper/main.go`

Implement:

1. session env-file preparation after `sessionName` generation
2. warning emission for skipped env entries
3. wrapper-command usage in place of direct `bash -l` / `bash -c`
4. cleanup defer for temp file

## `cooper/internal/proof/proof.go`

Implement:

1. a proof step that verifies configured barrel envs when `cfg.BarrelEnvVars` is non-empty
2. use shared wrapper logic, not an ad-hoc duplicate

## `cooper/internal/testdriver/barrelenv_smoke.go`

Implement a reusable smoke helper for real runtime verification.

## `cooper/README.md` and `cooper/REQUIREMENTS.md`

Document:

1. new configure screen
2. runtime-only behavior
3. precedence rules
4. protected-name restrictions
5. plain-text storage caveat

## Detailed Test Plan

This section is intentionally verbose. Each test entry describes:

1. where it should live
2. how to build the fixture or scenario
3. what exact behavior to assert

## General Test Rules

1. Do not add `t.Parallel()` to Docker-backed tests in `main_test.go` because the helper `captureCommandIO(...)` swaps global stdio.
2. Configure UI tests should target `newModel(...)` or submodels directly, not `configure.Run(...)`, because `Run(...)` checks for a live Docker daemon.
3. Docker-backed tests should reuse the existing bootstraps:
   - `main_test.go` uses `setupCommandDriver(...)`
   - `cooper/internal/testdriver` uses `TestMain(...)` and `testdriver.New(...)`
4. Runtime integration tests must verify actual in-container behavior using Docker, not just string generation helpers.

## Package/Directory: `cooper/internal/config`

Recommended file:

- `cooper/internal/config/barrel_env_test.go`

### Test: default config contains empty barrel env slice

Setup:

- call `cfg := DefaultConfig()`

Action:

- inspect `cfg.BarrelEnvVars`

Assert:

- `cfg.BarrelEnvVars` is not nil
- `len(cfg.BarrelEnvVars) == 0`

### Test: old config missing `barrel_env_vars` loads successfully

Setup:

- create temp file with raw JSON that omits `barrel_env_vars`
- include enough required fields to make loading realistic; easiest is to marshal a `DefaultConfig()` clone after setting `BarrelEnvVars = nil`, then manually remove the JSON field or write a minimal older JSON fixture

Action:

- call `LoadConfig(path)`

Assert:

- no error
- `cfg.BarrelEnvVars` is non-nil empty slice

### Test: config clone deep-copies barrel env vars

Setup:

- create config with `BarrelEnvVars = []BarrelEnvVar{{Name: "FOO", Value: "a"}}`

Action:

- call `CloneConfig(cfg)`
- mutate clone `clone.BarrelEnvVars[0].Value = "b"`

Assert:

- original value remains `"a"`

### Test: save/load round-trip preserves env values and order

Setup:

- temp dir
- config with ordered entries:
  - `A=1`
  - `B=two words`
  - `C=`

Action:

- `SaveConfig(path, cfg)`
- `LoadConfig(path)`

Assert:

- same count
- same order
- same exact values, including empty string

### Test: validation rejects empty key

Setup:

- env slice or config with `BarrelEnvVars = []{{Name: "", Value: "x"}}`

Action:

- call `ValidateBarrelEnvVars(...)`

Assert:

- error is non-nil
- error message contains `barrel env` and `key` or `name`

### Test: canonicalization trims key whitespace without mutating input

Setup:

- config with `Name: "  FOO  "`, `Value: "x"`

Action:

- call `CanonicalizeBarrelEnvVars(...)`

Assert:

- returned copied slice contains `FOO`
- original input slice still contains `"  FOO  "`

### Test: strict validation validates trimmed names without mutating input

Setup:

- input slice with `Name: "  FOO  "`

Action:

- call `ValidateBarrelEnvVars(...)`

Assert:

- no error
- original input slice remains unchanged

### Test: validation rejects malformed names

Create table-driven cases:

- `1BAD`
- `BAD-NAME`
- `BAD NAME`
- `BAD=NAME`
- `BAD.NAME`

For each case:

Setup:

- single-entry env slice

Action:

- `ValidateBarrelEnvVars(...)`

Assert:

- error non-nil
- error mentions the offending key

### Test: validation rejects duplicates case-insensitively

Setup:

- `FOO=1`
- `foo=2`

Action:

- `ValidateBarrelEnvVars(...)`

Assert:

- error non-nil
- error mentions duplicate/conflict and `FOO`/`foo`

### Test: validation rejects protected names

Create table-driven cases:

- `HTTP_PROXY`
- `http_proxy`
- `COOPER_PROXY_HOST`
- `COOPER_ANYTHING`
- `OPENAI_API_KEY`
- `TERM_PROGRAM`
- `PATH`

For each case:

Setup:

- one entry with innocuous value

Action:

- `ValidateBarrelEnvVars(...)`

Assert:

- error non-nil
- error mentions protected/reserved and the name

### Test: validation allows empty value

Setup:

- `EMPTY=""`

Action:

- `ValidateBarrelEnvVars(...)`

Assert:

- no error

### Test: validation rejects newline, carriage return, and NUL values

Table-driven cases:

- `"line1\nline2"`
- `"line1\rline2"`
- `"abc\x00def"`

Action:

- call `ValidateBarrelEnvVars(...)`

Assert:

- error non-nil

### Test: runtime normalizer skips protected and malformed entries but keeps valid ones

Setup:

- env slice:
  - `GOOD=1`
  - `HTTP_PROXY=http://bad`
  - `BAD-NAME=x`
  - `ALSO_GOOD=2`

Action:

- call `NormalizeBarrelEnvVarsForRuntime(...)`

Assert:

- returned usable slice contains exactly `GOOD`, `ALSO_GOOD`
- returned warnings length is 2
- warnings mention `HTTP_PROXY` and `BAD-NAME`

### Test: runtime normalizer preserves duplicate order and values

Setup:

- env slice:
  - `FOO=1`
  - `FOO=2`

Action:

- runtime normalizer

Assert:

- both entries remain in order
- warning count is 0

Reason for this test:

- It locks in the chosen tolerant-runtime semantics so a later refactor does not unexpectedly drop duplicates.

## Package/Directory: `cooper/internal/app`

Recommended file:

- update `cooper/internal/app/configure_test.go`

Also update:

- `cooper/internal/app/cooper_test.go`

### Test: `SetBarrelEnvVars` updates config copy safely

Setup:

- `ca, _ := NewConfigureApp(t.TempDir())`
- input slice `vars := []config.BarrelEnvVar{{Name: "FOO", Value: "1"}}`

Action:

- `ca.SetBarrelEnvVars(vars)`
- mutate original `vars[0].Value = "mutated"`
- inspect `ca.Config()`

Assert:

- stored config still has `FOO=1`

### Test: `Save()` persists barrel env vars to config.json

Setup:

- configure app temp dir
- set at least one valid env var

Action:

- `warnings, err := ca.Save()`
- load `config.json`

Assert:

- no error
- saved config contains expected entry
- env var order preserved if multiple entries were set

### Test: existing config with barrel env vars reloads correctly

Setup:

- write config.json via `config.SaveConfig(...)` including env entries

Action:

- `NewConfigureApp(cooperDir)`

Assert:

- `ca.Config().BarrelEnvVars` matches saved data exactly

### Test: save fails when barrel env vars are invalid

Setup:

- set invalid env name using `ca.SetBarrelEnvVars(...)`

Action:

- `ca.Save()`

Assert:

- error non-nil
- error message mentions barrel env validation

### Test: successful `Save()` canonicalizes in-memory and persisted barrel env vars

Setup:

- configure app temp dir
- set `BarrelEnvVars` to entries with trim-only canonicalization needed, for example `Name: "  FOO  ", Value: "x"`

Action:

- call `ca.Save()`
- inspect both `ca.Config()` and persisted `config.json`

Assert:

- no error
- in-memory `ca.Config().BarrelEnvVars[0].Name == "FOO"`
- persisted `config.json` also contains `"FOO"`
- values remain otherwise unchanged

### Test: invalid hand-edited barrel env does not break `UpdateSettings`

Setup:

- create `cooperDir, cfg := setupCooperDir(t)` using the existing `cooper/internal/app/cooper_test.go` helpers
- set `cfg.BarrelEnvVars` directly to an invalid entry such as `HTTP_PROXY=http://bad`
- write that config to disk if the test path relies on persisted reloads
- create `app := NewCooperApp(cfg, cooperDir)`

Action:

- call `app.UpdateSettings(...)` with otherwise valid values

Assert:

- method returns nil
- updated runtime settings are persisted and reflected in memory
- failure is not triggered by the invalid barrel env entry

### Test: invalid hand-edited barrel env does not break `UpdatePortForwards`

Setup:

- use the existing Docker-backed `TestCooperApp_UpdatePortForwards` pattern
- seed `cfg.BarrelEnvVars` with an invalid entry such as `BAD-NAME=x` or protected `HTTP_PROXY=http://bad`

Action:

- call `app.UpdatePortForwards(...)` with valid rules

Assert:

- method returns nil
- `socat-rules.json` is updated as expected
- no validation error is raised because of unrelated barrel env config

## Package/Directory: `cooper/internal/configure`

Recommended files:

- `cooper/internal/configure/barrelenv.go`
- update `cooper/internal/configure/configure_test.go`

### Test: welcome menu contains Barrel Environment item and Save & Build shifts to slot 7

Setup:

- `m := newModel(config.DefaultConfig(), t.TempDir(), nil, false)`

Action:

- inspect `m.welcome.items`

Assert:

- length is 7
- item 5 label is `Barrel Environment`
- item 6 label is `Save & Build`

### Test: numeric shortcut 6 navigates to Barrel Environment

Setup:

- root model from `newModel(...)`

Action:

- send key message for `6` to `m.updateWelcome(...)`

Assert:

- `m.screen == ScreenBarrelEnv`

### Test: numeric shortcut 7 navigates to Save screen

Setup:

- root model from `newModel(...)`

Action:

- send key message for `7`

Assert:

- `m.screen == ScreenSave`

### Test: new model initializes from existing config values

Setup:

- config with two env entries

Action:

- `m := newBarrelEnvModel(cfg.BarrelEnvVars)`

Assert:

- model has two rows with exact names/values

### Test: add modal saves trimmed key and exact value

Setup:

- `m := newBarrelEnvModel(nil)`
- open modal via `n` or helper
- type key `"  API_URL  "`
- type value `"  keep leading and trailing spaces  "`

Action:

- confirm save

Assert:

- saved key is `API_URL`
- saved value is exactly `"  keep leading and trailing spaces  "`

### Test: empty value is allowed

Setup:

- add entry with key `EMPTY` and blank value

Action:

- save modal

Assert:

- entry exists
- value is empty string

### Test: malformed key shows modal error and does not save

Setup:

- open modal
- set key `BAD-NAME`

Action:

- press Enter to save

Assert:

- modal remains open
- `m.modal.err` is non-empty
- entries slice still empty

### Test: protected key shows modal error and does not save

Setup:

- key `HTTP_PROXY`

Action:

- save

Assert:

- modal remains open
- error mentions protected/reserved name
- no row added

### Test: edit updates the selected row only

Setup:

- model with `FOO=1`, `BAR=2`
- cursor on `BAR`

Action:

- edit and save as `BAR=updated`

Assert:

- first row unchanged
- second row updated

### Test: delete removes selected row and cursor adjusts safely

Setup:

- model with two rows, cursor on last row

Action:

- press `x`

Assert:

- row count decremented
- cursor moved to previous row or zero safely

### Test: `Esc` from modal cancels without mutation

Setup:

- model with one existing row
- open edit modal and change fields

Action:

- press `Esc`

Assert:

- original row unchanged
- modal closes

### Test: root-model sync writes barrel env vars back to config

Setup:

- root model with barrel env screen containing entries

Action:

- call `m.syncConfigFromSubModels()`

Assert:

- `m.cfg.BarrelEnvVars` matches `m.barrelEnv.toEntries()`

### Test: `isTextInputActive()` returns true when barrel env modal is open

Setup:

- root model on `ScreenBarrelEnv`
- open modal

Action:

- call `m.isTextInputActive()`

Assert:

- returns true

### Test: save screen summary includes barrel env count

Setup:

- save model with two env vars

Action:

- render save view

Assert:

- output contains `Barrel Environment:`
- output contains `2 entries`

### Test: existing invalid env entry can still be rendered and deleted

Setup:

- initialize barrel env model with `HTTP_PROXY=bad`

Action:

- render view
- delete the row

Assert:

- render succeeds without panic
- row count becomes zero

This test is important because it enforces the recovery-path requirement for hand-edited bad config.

### Test: modal save can repair one invalid row while other invalid rows still exist

Setup:

- initialize `barrelEnvModel` with two invalid rows, for example:
  - `HTTP_PROXY=bad`
  - `BAD-NAME=x`
- open edit modal on the first row
- change the first row to a valid entry such as `REPAIRED_ONE=ok`

Action:

- confirm modal save

Assert:

- modal save succeeds and closes
- first row is now `REPAIRED_ONE=ok`
- second row remains present and invalid
- the model remains editable

Then, as a second step in the same test or a sibling test:

Setup:

- put the repaired model into the root configure model / config snapshot used by save

Action:

- invoke overall save validation path

Assert:

- overall save still fails because the second invalid row remains

This test is important because it prevents an implementation from validating the full slice on every modal save and thereby making sequential repair awkward or impossible.

## Package/Directory: `cooper/internal/docker`

Recommended file:

- update `cooper/internal/docker/tmpdir_test.go`

### Test: `BarrelTmpDir` returns expected per-barrel path

Setup:

- cooper dir `/tmp/cooper`
- container name `barrel-demo-claude`

Action:

- call `BarrelTmpDir(...)`

Assert:

- equals `filepath.Join("/tmp/cooper", "tmp", "barrel-demo-claude")`

### Test: barrel run path uses `BarrelTmpDir(...)`

This does not need to assert through real Docker args if that becomes awkward, but if a small helper extraction is added it should be covered.

Minimum requirement:

- there is at least one test ensuring the path-building logic for the per-barrel tmp directory remains centralized and correct.

## Package/Directory: `cooper/internal/barrelenv`

Recommended files:

- `cooper/internal/barrelenv/files_test.go`
- `cooper/internal/barrelenv/script_test.go`

### Test: `RenderUserEnvFile` renders simple exports

Setup:

- `[]config.BarrelEnvVar{{Name: "FOO", Value: "bar"}}`

Action:

- render bytes to string

Assert:

- output contains `export FOO='bar'`

### Test: `RenderUserEnvFile` renders empty value correctly

Setup:

- `EMPTY=""`

Action:

- render

Assert:

- output contains `export EMPTY=''`

### Test: `RenderUserEnvFile` escapes single quotes correctly

Setup:

- value `it's fine`

Action:

- render

Assert:

- output contains `export KEY='it'"'"'s fine'`

### Test: `RenderUserEnvFile` preserves spaces, dollar signs, backslashes, and equals signs literally

Setup:

- value ` a $HOME path\\with\\slashes and=x `

Action:

- render

Assert:

- output is single-quoted shell literal
- output does not contain unquoted expansion points

### Test: `PrepareSessionEnvFile` writes file to barrel tmp dir with mode 0600

Setup:

- temp cooper dir
- container name `barrel-demo-claude`
- session name `session-a`
- usable env entry `FOO=1`

Action:

- call `PrepareSessionEnvFile(...)`
- `os.Stat(hostPath)`

Assert:

- file exists
- permissions are `0600`
- host path is under `docker.BarrelTmpDir(...)`
- container path equals `/tmp/cooper-cli-env-session-a.sh`

### Test: `PrepareSessionEnvFile` returns no file when all entries are filtered out

Setup:

- only invalid/protected entries

Action:

- `PrepareSessionEnvFile(...)`

Assert:

- no error
- warnings non-empty
- `HostPath == ""`
- no file created

### Test: `RemoveSessionEnvFile` removes existing file and tolerates missing file

Setup:

- create temp file

Action:

- remove once
- remove again

Assert:

- both calls return nil
- file absent after first call

### Test: `ProtectedRuntimeEnvNames` de-duplicates and preserves stable order

Setup:

- extra names `[]string{"OPENAI_API_KEY", "TERM", "OPENAI_API_KEY"}`

Action:

- call helper

Assert:

- output contains static infra names
- output contains `OPENAI_API_KEY` and `TERM` once each
- order is deterministic across repeated calls

### Test: wrapper command for interactive session has expected argv shape

Setup:

- user env file `/tmp/cooper-cli-env-demo.sh`
- protected names `[]string{"HTTP_PROXY", "OPENAI_API_KEY"}`
- target cmd `[]string{"bash", "-l"}`

Action:

- build exec wrapper command

Assert:

- argv prefix is `bash -c`
- argv contains placeholder `$0` arg like `cooper-env-wrapper`
- argv contains env file path as the next arg
- tail argv is exactly `bash -l`

### Test: wrapper command for one-shot session keeps command string as separate argv element

Setup:

- one-shot `printf "%s" "$FOO"`

Action:

- build wrapper command with target `[]string{"bash", "-c", oneShot}`

Assert:

- tail args are exactly `bash`, `-c`, `<oneShot>`
- wrapper script string itself does not contain the literal one-shot command text

### Test: wrapper script preserves unset-vs-set semantics

This should be a real execution test of the wrapper script string, not only a string-inspection test.

Setup:

- use a temp env file with `export HTTP_PROXY='http://bad:1'` and `export FOO='ok'`
- build wrapper command that protects `HTTP_PROXY`
- execute locally using the returned argv directly, for example `exec.Command(wrapperArgs[0], wrapperArgs[1:]...)`
- seed host process env with `HTTP_PROXY=http://good:2`

Action:

- target command prints `"$HTTP_PROXY|$FOO"`

Assert:

- output is `http://good:2|ok`

Then repeat with `HTTP_PROXY` unset in the wrapper environment and assert the final `HTTP_PROXY` is unset/empty after restore.

This test is important because it verifies the exact precedence contract without Docker.

### Test: two prepared session env files in the same barrel do not clobber each other

Setup:

- same `cooperDir`, same `containerName`
- session names `a` and `b`
- different env values

Action:

- call `PrepareSessionEnvFile(...)` twice
- read both files

Assert:

- host paths differ
- file contents differ as expected
- both files exist simultaneously

This is the minimum required concurrency-safe coverage if full concurrent `runCLI` subprocess coverage is not added.

## Package: `main`

Recommended file:

- update `cooper/main_test.go`

All tests below should use the existing helpers:

- `setupCommandDriver(...)`
- `withCommandGlobals(...)`
- `captureCommandIO(...)`

### Test: one-shot CLI sees configured barrel env value

Setup:

- command driver with config mutator adding `BarrelEnvVars = []{{Name: "BARREL_TEST_VAR", Value: "cli-value"}}`
- start cooper runtime with `driver.Start(ctx)`
- set cwd to temp workspace
- set `cliOneShot = "printf %s \"$BARREL_TEST_VAR\""`

Action:

- `runCLI(nil, []string{"claude"})`

Assert:

- stdout equals or contains `cli-value`
- stderr may include barrel start message on first launch

### Test: barrel env change applies to next CLI session without barrel restart

Setup:

1. driver with initial config `MY_VAR=old`
2. start runtime
3. temp workspace + set cwd
4. first run: `cliOneShot = "printf %s \"$MY_VAR\""`

Action:

1. run `cooper cli claude`
2. assert `old`
3. mutate persisted config on disk to `MY_VAR=new` using `config.LoadConfig` + assignment + `config.SaveConfig`
4. run `cooper cli claude` again

Assert:

- second stdout equals `new`
- second stderr does not contain `Starting barrel container`, proving the existing barrel was reused

### Test: empty configured value is visible as empty, not unset to a non-empty default

Setup:

- set `EMPTY_TEST=""`
- one-shot command `bash -c 'if [[ -v EMPTY_TEST ]]; then printf "set:%s" "$EMPTY_TEST"; else printf "unset"; fi'`

Action:

- run CLI one-shot

Assert:

- stdout is `set:`

This distinguishes empty-string from unset.

### Test: special characters round-trip exactly inside the shell

Setup:

- set value like `a b '$PATH' \\ tail = done`
- one-shot command `printf %s "$SPECIAL_TEST"`

Action:

- run CLI one-shot

Assert:

- stdout matches the exact expected string byte-for-byte

### Test: protected `HTTP_PROXY` from bad config is ignored and Cooper value wins

Setup:

1. create driver normally
2. start runtime
3. temp workspace + set cwd
4. load persisted config and write invalid barrel env entry `HTTP_PROXY=http://bad:9999` directly to `config.json`
5. one-shot command `printf %s "$HTTP_PROXY"`

Action:

- run `cooper cli claude`

Assert:

- stdout equals `http://cooper-proxy:<cfg.ProxyPort>`
- stderr contains warning about ignored `HTTP_PROXY`

### Test: lowercase proxy aliases are unset/restored, not left user-controlled

Setup:

- bad config entry `http_proxy=http://bad:9999`
- one-shot command `printf '%s|%s' "${HTTP_PROXY-}" "${http_proxy-}"`

Action:

- run CLI

Assert:

- first field is Cooper proxy URL
- second field is empty

This locks in defense against tools that consult lowercase proxy env.

### Test: protected `DISPLAY` from bad config is ignored

Setup:

- bad config entry `DISPLAY=:55`
- one-shot command `printf %s "$DISPLAY"`

Action:

- run CLI

Assert:

- stdout equals `127.0.0.1:99`
- stderr contains warning

### Test: protected token env wins over bad config entry

Recommended codex scenario.

Setup:

- driver with Codex image available in test bootstrap
- `t.Setenv("OPENAI_API_KEY", "sk-real")`
- bad config entry `OPENAI_API_KEY=sk-bad`
- one-shot command `printf %s "$OPENAI_API_KEY"`

Action:

- run `cooper cli codex`

Assert:

- stdout equals `sk-real`
- stderr contains warning about ignored protected config entry

### Test: protected VS Code env wins over bad config entry

Setup:

- `t.Setenv("TERM_PROGRAM", "vscode")`
- bad config entry `TERM_PROGRAM=bad-term`
- one-shot command `printf %s "$TERM_PROGRAM"`

Action:

- run CLI

Assert:

- stdout equals `vscode`

### Test: `PATH` cannot be overridden by bad config

Setup:

- bad config entry `PATH=/broken`
- one-shot command `command -v bash >/dev/null && printf ok`

Action:

- run CLI

Assert:

- stdout contains `ok`

This avoids brittle exact PATH assertions while still proving PATH was not broken by user config.

### Test: session env file is cleaned up after CLI run

Setup:

- valid barrel env entry
- run one-shot CLI once
- identify barrel tmp dir using `docker.BarrelTmpDir(driver.CooperDir(), barrelName)`

Action:

- list files in the tmp dir after CLI returns

Assert:

- no file matching `cooper-cli-env-*.sh` remains

### Test: runtime warning does not block session startup when bad config exists

Setup:

- mix of valid and invalid env entries in config:
  - `GOOD=1`
  - `HTTP_PROXY=http://bad`
  - `BAD-NAME=x`
- one-shot command prints `GOOD`

Action:

- run CLI

Assert:

- stdout contains `1`
- stderr contains warnings for the two bad entries
- CLI returns nil error

## Package/Directory: `cooper/internal/testdriver`

Recommended files:

- `cooper/internal/testdriver/barrelenv_smoke.go`
- `cooper/internal/testdriver/barrelenv_smoke_integration_test.go`

### Smoke helper design

Add a helper like:

```go
func RunBarrelEnvSmoke(ctx context.Context, d *Driver) error
```

This helper should use the real runtime and at least one real barrel.

When the smoke helper needs a session env file, it should generate a unique session name with the same `names.Generate(...)` / `names.Release(...)` pattern used by `runCLI(...)`, rather than using a fixed filename or ad-hoc naming.

### Smoke scenario 1: configured env visible in real barrel session

Setup:

- driver with config mutator adding:
  - `SMOKE_ALPHA=one`
  - `SMOKE_EMPTY=`

Action:

- start runtime
- start a real barrel
- prepare session env file using the shared helper
- run wrapper command inside the barrel to print values

Assert:

- `SMOKE_ALPHA` equals `one`
- `SMOKE_EMPTY` is set but empty

### Smoke scenario 2: protected env restoration works in real barrel session

Setup:

- manually write bad config entries like `HTTP_PROXY=http://bad` and `DISPLAY=:55`

Action:

- prepare session env file and wrapper command through shared helpers
- execute inside real running barrel

Assert:

- `HTTP_PROXY` equals real Cooper proxy URL
- `DISPLAY` equals `127.0.0.1:99`

### Smoke scenario 3: next session sees updated value without barrel restart

Setup:

- start runtime and barrel with `SMOKE_CHANGE=before`
- execute once and assert `before`
- update `config.json` to `SMOKE_CHANGE=after`

Action:

- execute a second session through shared helper

Assert:

- second result equals `after`
- same barrel container remains running throughout

## Package/Directory: `cooper/internal/proof`

### Test / implementation expectation

`cooper proof` is runtime code, not just tests. Update it so proof output explicitly covers barrel env support when `cfg.BarrelEnvVars` is non-empty.

Minimum proof checks:

1. For each configured valid env key, the proof session sees the expected value.
2. Protected envs such as `HTTP_PROXY` and `DISPLAY` still have Cooper values even if config contains bad entries.

If proof cannot create a session env file or execute the wrapper, it should report a failing proof step, not silently skip.

## Documentation Assertions

README and requirements updates should include concrete examples.

Required documentation statements:

1. `cooper configure` now has a `Barrel Environment` screen.
2. Values are loaded into each `cooper cli` session.
3. Cooper-managed envs are restored after user envs are loaded.
4. The feature is runtime-only and does not require `cooper build`.
5. Values are stored in plain text in `~/.cooper/config.json`.
6. Protected names such as `HTTP_PROXY`, `PATH`, `OPENAI_API_KEY`, and `COOPER_*` cannot be configured.

## Manual Verification Checklist

This is not a substitute for automated tests. It is a release sanity checklist.

1. Run `cooper configure`, add `MY_VAR=manual-test`, save.
2. Run `cooper cli claude -c 'printf %s "$MY_VAR"'` and confirm `manual-test`.
3. Edit config via `cooper configure`, change it to `manual-test-2`, rerun the same command without restarting `cooper up`, and confirm the new value appears.
4. Hand-edit `config.json` to inject `HTTP_PROXY=http://bad:9999`, rerun `cooper cli claude -c 'printf %s "$HTTP_PROXY"'`, and confirm the real Cooper proxy value still wins.
5. Run `cooper proof` with barrel envs configured and confirm the new proof steps pass.

## Assumptions To Verify Later

These could not all be verified in the current sandbox and still need explicit confirmation or Docker-enabled verification.

| Assumption | Status | Why it matters |
|---|---|---|
| Storing values in plain text in `~/.cooper/config.json` is acceptable | Needs product/security confirmation | Users may treat env vars as secrets even if the feature is not meant as a secret store |
| Multiline values are out of scope for v1 | Needs confirmation | This plan intentionally rejects them for safety and testability |
| Runtime warning + skip is preferred over hard-fail for malformed hand-edited env config | Needs final confirmation | This plan chooses resilience over strict runtime failure |
| Docker-enabled runtime behavior of the wrapper works on all supported hosts | Needs real Docker verification | Docker is unavailable in this sandbox |

## Verified Assumptions

These were verified from the current codebase and available external references.

| Assumption | Result | Evidence |
|---|---|---|
| There is no existing user-configurable barrel env feature | Verified | `cooper/internal/config/config.go` has no such field and repo search found no implementation path |
| Configure uses separate screen models under a root shell model | Verified | `cooper/internal/configure/configure.go`, `cooper/internal/configure/whitelist.go`, `cooper/internal/configure/portforward.go`, `cooper/internal/configure/save.go` |
| Session-specific env is already injected through `docker exec -e` | Verified | `main.go` builds `envArgs`; `cooper/internal/docker/barrel.go` applies them in `ExecBarrel(...)` |
| Container-start env already sets proxy/X11/Playwright envs | Verified | `cooper/internal/docker/barrel.go` |
| There is already a host-backed per-barrel `/tmp` mount | Verified | `cooper/internal/docker/barrel.go` mounts `~/.cooper/tmp/{containerName}` to `/tmp` |
| `docker exec -e` sets env only for the exec process and can override existing env for that process | Verified | Official Docker CLI docs for `docker container exec` |
| `cooper cli` currently runs one-shot commands via `bash -c` and interactive shells via `bash -l` | Verified | `main.go` `runCLI(...)` |
| `cooper cli` already assumes `bash` exists in tool barrels | Verified | `runCLI(...)` always executes `bash`; current feature can rely on that same contract |
| `configure.Run(...)` is not suitable for unit tests because it checks Docker daemon availability | Verified | `cooper/internal/configure/configure.go` `Run(...)` calls `checkDocker()` |
| Docker is unavailable in the current sandbox | Verified | `docker: command not found` during verification |
| The local Go toolchain in this sandbox is too old to run Cooper tests directly | Verified | `cooper/go.mod` requires Go 1.25.0, sandbox has Go 1.24.10 |

## Anti-Drift Checklist For The Implementor

Before calling the feature done, verify each of these is true.

1. User env is not injected in Dockerfile templates.
2. User env is not injected in `docker.StartBarrel(...)`.
3. `runCLI(...)` uses a shared helper package for env-file prep and wrapper command building.
4. Protected env restoration preserves set-vs-unset semantics.
5. Bad hand-edited config entries produce warnings, not shell breakage.
6. `ConfigureApp.Save()` rejects invalid env keys.
7. Configure screen can display and delete invalid pre-existing entries.
8. `main_test.go` has Docker-backed end-to-end coverage for the feature.
9. `cooper/internal/testdriver` has real-runtime smoke coverage.
10. `cooper proof` verifies the same runtime behavior path, not a fake approximation.

## Final Recommendation

Implement this feature as a session-scoped runtime capability owned by `cooper cli`, backed by persisted config and a new configure screen, with strict validation at save time and tolerant sanitization at runtime.

That is the smallest correct design that satisfies the product requirement while preserving Cooper's architecture:

1. config remains the single source of truth
2. configure stays a presentation layer over app/config boundaries
3. runtime env precedence is explicit and centralized
4. barrel reuse keeps working without restart
5. users cannot override env vars Cooper needs to function
