# Plan: macOS Apple Silicon Support for Cooper

## Scope

**Target**: macOS with Apple Silicon (M1/M2/M3/M4), running Docker Desktop 4.x+.

**Not in scope**: macOS Intel (easy follow-up since Docker Desktop handles it), Windows, bare Docker Engine on macOS (Colima, etc.). These can be explored later but Docker Desktop is the primary macOS Docker runtime.

**Goal**: Cooper runs fully on macOS Apple Silicon -- configure, build, up, cli, proof, cleanup -- with correct instructions, error messages, and documentation throughout.

---

## Analysis: Docker Desktop vs Docker Engine

Cooper was built for Linux with Docker Engine. Docker Desktop for Mac runs Docker Engine inside a Linux VM (Apple Virtualization.framework). This means:

**Things that work identically** (no changes needed):
- `--internal` network isolation -- enforced inside the VM's Docker Engine, same guarantees
- `--cap-drop=ALL`, `--security-opt=no-new-privileges`, seccomp profiles -- all enforced inside the VM's Linux kernel
- Squid SSL bump, CA certificate injection, socat relays inside containers -- all run in Linux containers
- Docker DNS (`cooper-proxy` resolution on internal network) -- same behavior
- `--add-host=host.docker.internal:host-gateway` -- supported since Docker Desktop 4.x (and Docker Engine 20.10+)
- Container entrypoint scripts (`sed -i`, `/dev/tcp`, `Xvfb`, `bash` features) -- all run inside Linux containers
- Bubblewrap build from source -- architecture-independent source tarball, compiles natively

**Things that differ** (changes needed):

| Aspect | Docker Engine (Linux) | Docker Desktop (macOS) |
|--------|----------------------|----------------------|
| Container arch | `linux/amd64` (unless ARM host) | `linux/arm64` natively on Apple Silicon |
| `host.docker.internal` | Resolves to bridge gateway IP (e.g., `172.17.0.1`) | Resolves to host machine (magic tunneling) |
| Gateway IP bindability | Host can `bind()` to gateway IP (same namespace) | Host CANNOT bind to gateway IP (it's inside the VM) |
| Loopback-only services | NOT reachable from containers via gateway IP | Reachable via `host.docker.internal` (Docker Desktop tunnels to host loopback) |
| Host clipboard | `xclip`/`wl-paste` (X11/Wayland) | `pbpaste`/`osascript` (AppKit) |
| Host font paths | `/usr/share/fonts`, `~/.local/share/fonts` | `/Library/Fonts`, `~/Library/Fonts`, `/System/Library/Fonts` |
| UID/GID | Typically 1000:1000 | Typically 501:20; Docker Desktop handles file ownership transparently |

---

## Changes Required

### 1. ARM64 Node.js Download in Dockerfile Template

**Problem**: `base.Dockerfile.tmpl:86` hardcodes `linux-x64`:
```dockerfile
RUN curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
```
On Apple Silicon Docker Desktop, containers build as `linux/arm64`. This downloads the wrong binary and the container will fail to run Node.js (or the build itself will fail at `node --version`).

**Fix**: Use Docker's automatic `TARGETARCH` build arg to select the correct binary.

**File**: `cooper/internal/templates/base.Dockerfile.tmpl`

**Change**: Replace the Node.js download block (lines 80-88) with architecture-aware download:
```dockerfile
# Node.js installation via official tarball (pinned version).
# Required for npm-based AI tools even when Node is not a programming tool.
# TARGETARCH is provided automatically by Docker (amd64 or arm64).
{{- if and .HasNode .NodeVersion}}
ARG NODE_VERSION={{.NodeVersion}}
{{- else}}
ARG NODE_VERSION=22.12.0
{{- end}}
ARG TARGETARCH
RUN NODE_ARCH=$([ "$TARGETARCH" = "arm64" ] && echo "arm64" || echo "x64") && \
    curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz" \
    | tar -xJ -C /usr/local --strip-components=1 \
    && node --version && npm --version
```

**Why `TARGETARCH` works without buildx**: Docker Engine 20.10+ (which Cooper already requires) auto-provides `TARGETPLATFORM`, `TARGETOS`, `TARGETARCH` when building. On Apple Silicon Docker Desktop, `TARGETARCH=arm64` by default. On Linux x86_64, `TARGETARCH=amd64`. Declaring `ARG TARGETARCH` makes it available in the build stage.

**Testing**: Build on Apple Silicon, verify `node --version` succeeds, verify `file $(which node)` shows `aarch64`.

---

### 2. Go Version Validation URL

**Problem**: `config/resolve.go:106` validates Go versions by checking if `linux-amd64` tarball exists:
```go
url := fmt.Sprintf("https://go.dev/dl/go%s.linux-amd64.tar.gz", version)
```
This works for validation purposes (Go releases always include both architectures for any recent version), but is conceptually wrong when running on ARM64.

**Fix**: This is a validation-only check (HTTP HEAD to see if the version exists). The Go installation itself uses `golang:X-bookworm` multi-arch Docker image, which Docker handles automatically. Two options:

**Option A (minimal, recommended)**: Add a comment explaining why `linux-amd64` is used and leave the URL as-is. The version number is what's being validated, not the architecture. Any Go version >= 1.16 exists for both `linux-amd64` and `linux-arm64`.

```go
// Validate version existence using linux-amd64 as canonical check.
// The actual Go installation uses the golang: Docker image (multi-arch),
// not this tarball. Version availability is platform-independent for Go >= 1.16.
url := fmt.Sprintf("https://go.dev/dl/go%s.linux-amd64.tar.gz", version)
```

**Option B (thorough)**: Use `runtime.GOARCH` to validate against the host architecture:
```go
arch := runtime.GOARCH
if arch == "arm64" {
    arch = "arm64"
} else {
    arch = "amd64"
}
url := fmt.Sprintf("https://go.dev/dl/go%s.linux-%s.tar.gz", version, arch)
```

**Recommendation**: Option A. The validation check is not architecture-sensitive for any Go version Cooper would realistically use. Option B adds complexity for no practical benefit.

**File**: `cooper/internal/config/resolve.go:106`

---

### 3. macOS Clipboard Reader

**Problem**: Only `reader_linux.go` exists (build-tagged `//go:build linux`). On macOS, the clipboard bridge's host-side image capture has no implementation. `cooper up` will either fail to compile for darwin or panic at runtime.

**Fix**: Create `reader_darwin.go` implementing the same `ClipboardReader` interface using macOS native tools.

**Files to create/modify**:
- **New**: `cooper/internal/clipboard/reader_darwin.go`
- **Modify**: `cooper/internal/clipboard/manager.go` (if it references `LinuxReader` directly)

**Implementation for `reader_darwin.go`**:

macOS clipboard reading approach:
- Use `osascript` to check clipboard type: `osascript -e 'clipboard info'` returns available types (e.g., `{class PNGf}`, `{TIFF picture}`, `{class JPEG}`)
- Use `osascript` + AppleScript to write clipboard image to a temp file, then read it:
  ```bash
  osascript -e 'set png to (the clipboard as "class PNGf")' \
            -e 'set f to open for access POSIX file "/tmp/cooper-clip.png" with write permission' \
            -e 'write png to f' \
            -e 'close access f'
  ```
- Alternatively, use `pbpaste` but it only handles text. For images, AppleScript is the reliable approach.
- For format detection: parse `clipboard info` output to find the best image type.
- `CheckPrerequisites`: Verify `osascript` exists (always present on macOS). Check for ImageMagick if needed for format conversion.

**Structure**:
```go
//go:build darwin

package clipboard

type DarwinReader struct {
    envLookup func(string) string
}

func NewDarwinReader(envLookup func(string) string) *DarwinReader { ... }
func (r *DarwinReader) Read(ctx context.Context) (*CaptureResult, error) { ... }
func (r *DarwinReader) CheckPrerequisites(ctx context.Context) error { ... }
```

**CheckPrerequisites messages** -- must be macOS-specific:
```
ImageMagick is required for image format conversion.
Install with: brew install imagemagick
```

(Not `sudo apt install imagemagick` as the Linux version says.)

**Integration**: The `manager.go` code that constructs the reader needs to be platform-aware. If it currently does `NewLinuxReader(...)` directly, that needs to become a platform-dispatching function or use build tags to select the right constructor.

**Testing**: Unit tests with mocked `osascript` command (same pattern as `reader_linux_test.go` which mocks `execCommand`). Manual test: copy an image on macOS, run clipboard capture, verify PNG output.

---

### 4. macOS Font Sync

**Problem**: `fontsync/sync.go` only has `LinuxSources()` returning Linux font directories:
```go
func LinuxSources(homeDir string) []Source {
    return []Source{
        {filepath.Join(homeDir, ".local", "share", "fonts"), "user-local-share-fonts"},
        {filepath.Join(homeDir, ".fonts"), "user-dot-fonts"},
        {"/usr/local/share/fonts", "usr-local-share-fonts"},
        {"/usr/share/fonts", "usr-share-fonts"},
    }
}
```
On macOS, fonts are in different locations. `cooper up` calls `SyncLinuxFonts()` which will find nothing on macOS.

**Fix**: Add `DarwinSources()` and `SyncDarwinFonts()`, and make the caller use `runtime.GOOS` to dispatch.

**File**: `cooper/internal/fontsync/sync.go`

**New function**:
```go
func DarwinSources(homeDir string) []Source {
    return []Source{
        {filepath.Join(homeDir, "Library", "Fonts"), "user-library-fonts"},
        {"/Library/Fonts", "library-fonts"},
        {"/System/Library/Fonts", "system-library-fonts"},
        {"/System/Library/Fonts/Supplemental", "system-supplemental-fonts"},
    }
}

func SyncDarwinFonts(homeDir, cooperDir string) (Result, error) {
    return SyncFonts(DarwinSources(homeDir), cooperDir)
}
```

**Caller change**: Wherever `SyncLinuxFonts` is called (likely in the `cooper up` startup flow), replace with:
```go
switch runtime.GOOS {
case "darwin":
    result, err = fontsync.SyncDarwinFonts(homeDir, cooperDir)
default:
    result, err = fontsync.SyncLinuxFonts(homeDir, cooperDir)
}
```

**Testing**: Add `TestDarwinSourcesReturnsExpectedPrefixes` mirroring the existing Linux test. On macOS, verify fonts are actually copied from `/Library/Fonts` to `~/.cooper/fonts`.

---

### 5. HostRelay Platform Awareness

**Problem**: `docker/hostrelay.go` binds TCP listeners to the Docker gateway IP on the host to relay connections from containers to `127.0.0.1` services. On macOS:
- The gateway IP returned by `docker network inspect` is inside the Linux VM, NOT bindable from the macOS host
- `net.Listen("tcp", "172.17.0.1:5432")` will fail with "bind: can't assign requested address"
- Docker Desktop already handles `host.docker.internal` → host loopback tunneling natively, so HostRelay is unnecessary

**Fix**: Skip HostRelay entirely on macOS. Docker Desktop's built-in tunneling already bridges `host.docker.internal` to the host's loopback interface.

**File**: `cooper/internal/docker/hostrelay.go` and wherever `NewHostRelay` is called.

**Option A (recommended)**: Make HostRelay construction aware of the platform:
```go
func NewHostRelay(gatewayIPs []string, logger *log.Logger) *HostRelay {
    if runtime.GOOS == "darwin" {
        // Docker Desktop for macOS handles host.docker.internal → host loopback
        // tunneling natively. HostRelay cannot bind to the gateway IP (it's
        // inside the Linux VM) and isn't needed.
        logger.Printf("[host-relay] skipped on macOS (Docker Desktop handles host access)")
        return &HostRelay{
            active: make(map[int]relayEntry),
            stopCh: make(chan struct{}),
            logger: logger,
        }
    }
    // ... existing Linux logic
}
```

And make `Start()` a no-op when `gatewayIPs` is empty:
```go
func (hr *HostRelay) Start(rules []config.PortForwardRule) {
    if len(hr.gatewayIPs) == 0 {
        return // no-op (e.g., macOS)
    }
    // ... existing logic
}
```

**Option B**: Skip HostRelay at the call site based on platform, passing `nil` for the relay.

**User-facing impact**: Port forwarding "just works" on macOS, even for services bound to `127.0.0.1`. The port forwarding setup warning in `cooper configure` about services needing to bind to `0.0.0.0` should be macOS-conditional -- on macOS, `127.0.0.1` services ARE reachable.

---

### 6. Execution Bridge Bind Address

**Problem**: The execution bridge binds to `127.0.0.1:{port}` AND `{gateway-ip}:{port}` (discovered via `docker network inspect cooper-external`). On macOS, the gateway IP is inside the VM and cannot be bound from the host.

**Where**: The `cooper up` startup code that calls `GetGatewayIP()` and binds the bridge listener.

**Fix**: On macOS, only bind to `127.0.0.1`. Docker Desktop can reach host `127.0.0.1` services from containers via `host.docker.internal`, so the bridge is reachable without binding to the gateway IP.

**Pseudo-change** (in the `cooper up` startup flow):
```go
bindAddrs := []string{fmt.Sprintf("127.0.0.1:%d", cfg.BridgePort)}
if runtime.GOOS != "darwin" {
    // On Linux, also bind to the Docker gateway IP so containers can reach
    // the bridge without relying on host.docker.internal tunneling.
    gatewayIP, err := docker.GetGatewayIP(docker.ExternalNetworkName())
    if err == nil {
        bindAddrs = append(bindAddrs, fmt.Sprintf("%s:%d", gatewayIP, cfg.BridgePort))
    }
}
```

**Files to modify**: The startup code in `cooper up` that initializes the bridge HTTP server and the HostRelay. Likely in `cooper/internal/app/` or the main `up` command handler.

---

### 7. Documentation and Error Messages

#### 7a. README.md -- Supported Platforms

**File**: `cooper/README.md:36-39`

**Current**:
```markdown
## Supported Platforms

- **Primary**: Linux (Ubuntu/Debian) with Docker Engine 20.10+.
- **Other Linux**: Any distro with Docker Engine and bash or zsh.
- **macOS / Windows**: Not supported in v1 (Docker Desktop networking differences).
```

**New**:
```markdown
## Supported Platforms

- **Linux**: Any distro with Docker Engine 20.10+ and bash or zsh.
- **macOS (Apple Silicon)**: Docker Desktop 4.x+. Requires macOS 12+.
- **macOS (Intel)**: Docker Desktop 4.x+. Untested but expected to work.
- **Windows**: Not supported.
```

#### 7b. README.md -- Prerequisites

**File**: `cooper/README.md:122-127`

**Current**:
```markdown
### Prerequisites

- **Docker Engine 20.10+** (not Docker Desktop)
- **Go 1.21+** (for installation via `go install`)
- **Linux** with bash or zsh
```

**New**:
```markdown
### Prerequisites

- **Linux**: Docker Engine 20.10+
- **macOS**: Docker Desktop 4.x+ (Docker Engine runs inside a Linux VM)
- **Go 1.21+** (for installation via `go install`)
- bash or zsh
```

#### 7c. README.md -- Port Forwarding Note

**File**: `cooper/README.md:242-243`

**Current**:
```markdown
**Note:** Host services must bind to `0.0.0.0` or the Docker gateway IP to be reachable.
Services bound to `127.0.0.1` are handled by Cooper's HostRelay...
```

**New** -- add macOS note:
```markdown
**Note (Linux):** Host services must bind to `0.0.0.0` or the Docker gateway IP to be
reachable from containers. Services bound to `127.0.0.1` are handled by Cooper's HostRelay,
which transparently proxies connections from the gateway IP to localhost.

**Note (macOS):** Docker Desktop handles host access natively. Services on any bind address
(including `127.0.0.1`) are reachable from containers via `host.docker.internal`. No
HostRelay is needed.
```

#### 7d. REQUIREMENTS.md -- Supported Platforms

**File**: `cooper/REQUIREMENTS.md:19-27`

Update the supported platforms section to include macOS Apple Silicon.

#### 7e. `cooper configure` Docker check messages

**File**: `cooper/internal/configure/configure.go:101-105`

**Current** -- shows all platforms' startup instructions together:
```
"On Linux:  sudo systemctl start docker\n"+
"On macOS:  open -a Docker\n"+
"On Windows: start Docker Desktop"
```

**Fix**: Use `runtime.GOOS` to show only the relevant instruction:
```go
switch runtime.GOOS {
case "darwin":
    hint = "Start Docker Desktop: open -a Docker"
case "linux":
    hint = "Start Docker: sudo systemctl start docker"
}
```

#### 7f. `cooper configure` Port Forwarding Setup Hint

**Where**: The port forwarding setup flow in `cooper configure`.

**Current**: Warns that host services must bind to `0.0.0.0`.

**Fix**: On macOS, adjust the hint:
```
On macOS with Docker Desktop, services on any bind address (including 127.0.0.1) are
reachable from barrels. No special configuration needed.
```

#### 7g. Clipboard prerequisite messages

**Where**: `cooper up` startup clipboard check and `reader_linux.go:81-98` error messages.

**Current**: All messages say `sudo apt install ...`.

**Fix**: macOS messages should say `brew install imagemagick`. The `pbpaste`/`osascript` tools are built into macOS and need no installation.

#### 7h. `cooper up` clipboard prerequisite check

**Where**: The `cooper up` startup flow that calls `CheckPrerequisites`.

**Current**: Checks for `xclip` or `wl-paste` on the host.

**Fix**: On macOS, skip this check (or check for `osascript` which is always present). The clipboard reader construction is already platform-dispatched via build tags, so the prerequisite check is part of the reader implementation.

---

### 8. Test Infrastructure

#### 8a. `doctor.sh` -- macOS-Aware Diagnostics

**File**: `cooper/internal/templates/doctor.sh`

This script runs INSIDE the container, so most of it works regardless of host OS. However, some checks reference Linux-specific paths or tools. Since the container is always Linux, most checks are fine.

**No changes needed** for doctor.sh itself -- it runs inside the Linux container.

#### 8b. `test-e2e.sh` -- Platform-Aware Test Script

**File**: `cooper/test-e2e.sh`

This script runs on the HOST. Several commands are Linux-specific:

1. **`getent hosts` (line ~568)**: Not available on macOS. Replace with:
   ```bash
   if command -v getent >/dev/null 2>&1; then
       dns_result=$(barrel_exec 'getent hosts cooper-proxy 2>&1 || true')
   else
       dns_result=$(barrel_exec 'nslookup cooper-proxy 2>&1 || true')
   fi
   ```
   Note: This runs inside the container via `barrel_exec`, so `getent` IS available (it's a Debian container). No change actually needed for this specific usage since it's `barrel_exec` not host execution. Review each `getent` usage to determine if it's host-side or container-side.

2. **Font test paths (line ~427-440)**: Hardcoded Linux font paths for test font lookup. Add macOS paths:
   ```bash
   if [ "$(uname)" = "Darwin" ]; then
       TEST_FONT="/Library/Fonts/Arial.ttf"
   else
       for candidate in /usr/share/fonts/truetype/dejavu/DejaVuSans.ttf ...; do
           ...
       done
   fi
   ```

3. **Host-side commands**: Review every host-side command for macOS compatibility. Most container-side commands are fine since they run in the Debian container.

#### 8c. `testdocker/bootstrap.go` -- macOS Compatibility

**File**: `cooper/internal/testdocker/bootstrap.go`

- `syscall.Flock` -- works on macOS (Darwin supports flock). No change needed.
- `/tmp` paths -- exist on macOS. No change needed.
- `os.Getuid()/os.Getgid()` -- returns valid values on macOS (e.g., 501:20). No change needed.
- `chmod -R u+rwX` -- macOS BSD `chmod` supports `+X`. No change needed.

**No changes needed** for testdocker/bootstrap.go.

---

### 9. Build and Distribution

#### 9a. Cross-Compilation

Cooper is a pure Go binary. Cross-compile for macOS:
```bash
GOOS=darwin GOARCH=arm64 go build -o cooper-darwin-arm64 ./cooper
```

No CGO dependencies, so cross-compilation should work cleanly.

#### 9b. Release Script

**File**: `cooper/release.sh` (if it exists) or the CI pipeline.

Add macOS arm64 as a build target alongside `linux-amd64`:
```bash
GOOS=linux  GOARCH=amd64 go build -o dist/cooper-linux-amd64  ./cooper
GOOS=darwin GOARCH=arm64 go build -o dist/cooper-darwin-arm64  ./cooper
```

#### 9c. Installation instructions

Update install instructions to mention macOS. `go install` works on macOS, so:
```bash
go install github.com/rickchristie/govner/cooper@latest
```
works as-is. No changes needed for the install command itself.

---

## What Does NOT Need to Change

These are things that might seem like they need changes but actually don't, because they run inside Linux containers (not on the macOS host):

| Component | Reason it's fine |
|-----------|-----------------|
| `entrypoint.sh.tmpl` (`sed -i`, `/dev/tcp`, `Xvfb`, `xauth`, `mcookie`) | Runs inside the Debian container, not on macOS |
| `proxy-entrypoint.sh.tmpl` (socat relays, `host.docker.internal`) | Runs inside the Alpine container |
| `doctor.sh` (X11 checks, `/proc`, `/dev/shm`) | Runs inside the Debian container |
| `base.Dockerfile.tmpl` apt packages (xclip, xsel, xvfb, fontconfig) | Installed inside the Linux container |
| `proxy.Dockerfile.tmpl` Squid build from source | Built inside the Alpine container |
| Seccomp profile (`seccomp-bwrap.json`) | Already includes `SCMP_ARCH_AARCH64`; enforced in the VM's Linux kernel |
| CA certificate paths (`/usr/local/share/ca-certificates/`) | Debian paths inside the container |
| Bubblewrap build from source | Architecture-independent source tarball |
| `golang:X-bookworm` base image | Multi-arch, Docker pulls correct platform automatically |
| `alpine:3.21` proxy base image | Multi-arch |
| `--cap-drop=ALL`, `--security-opt=no-new-privileges` | Enforced by Docker Engine inside the VM |
| `--internal` network isolation | Same isolation inside the VM as on bare Linux |
| Container UID/GID mapping | Docker Desktop handles file ownership transparently |
| socat inside containers | Runs in Linux, same behavior |
| X11/Xvfb clipboard bridge | Runs inside the Linux container |
| `--add-host=host.docker.internal:host-gateway` | Supported by Docker Desktop 4.x+ |

---

## Implementation Order

**Phase 1 -- Critical blockers** (Cooper won't build/run on macOS without these):
1. ARM64 Node.js download in Dockerfile template (Section 1)
2. macOS clipboard reader (Section 3)
3. HostRelay skip on macOS (Section 5)
4. Execution bridge bind address (Section 6)

**Phase 2 -- Functional completeness** (features work but with rough edges without these):
5. macOS font sync (Section 4)
6. Go version validation comment (Section 2)

**Phase 3 -- Polish** (docs, messages, tests):
7. Documentation updates (Section 7a-7h)
8. Test infrastructure (Section 8)
9. Build and distribution (Section 9)

---

## Verification Plan

### Automated: `cooper proof`

`cooper proof` already validates the entire stack end-to-end. On macOS, it should verify:
- Docker networks created (including `--internal` isolation)
- Proxy starts and SSL bump works
- Barrel containers start and can reach proxy
- Direct internet access is blocked (no route)
- Port forwarding works
- AI CLI smoke tests pass
- Clipboard bridge works (if we add clipboard to proof)

### Manual Checklist

- [ ] `cooper configure` runs, detects Docker Desktop, shows correct hints
- [ ] `cooper build` succeeds -- all images build on ARM64
- [ ] `docker images` shows `cooper-proxy`, `cooper-base`, `cooper-cli-*` all as `linux/arm64`
- [ ] `cooper up` starts without HostRelay errors, bridge binds to `127.0.0.1` only
- [ ] `cooper cli claude` opens a barrel, `node --version` works, `go version` shows arm64
- [ ] Port forwarding works for a `127.0.0.1`-bound host service (no HostRelay needed)
- [ ] Clipboard: copy image on macOS, press `c` in TUI, paste works in barrel
- [ ] `cooper proof` passes all phases
- [ ] `cooper cleanup` removes everything

### Regression: Linux

All changes must be gated on `runtime.GOOS == "darwin"` or build tags. Verify existing Linux behavior is unchanged:
- [ ] `cooper proof` still passes on Linux
- [ ] HostRelay still works on Linux
- [ ] Bridge still binds to gateway IP on Linux
- [ ] `xclip`/`wl-paste` clipboard still works on Linux

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| `--internal` network leaks on Docker Desktop | Low | High (security) | `cooper proof` tests direct egress is blocked |
| Docker Desktop VM networking changes in future versions | Low | Medium | Pin minimum Docker Desktop version, test in CI |
| `TARGETARCH` not available on older Docker versions | Very Low | High (build fails) | Cooper already requires Docker 20.10+ which supports it |
| macOS clipboard `osascript` approach is fragile | Medium | Medium | Comprehensive error handling, fallback to "no image" |
| Docker Desktop file sharing performance | Medium | Low (slower I/O) | Document that VirtioFS (default on modern Docker Desktop) is recommended |

---

## Summary of Files to Modify

| File | Change | Phase |
|------|--------|-------|
| `cooper/internal/templates/base.Dockerfile.tmpl` | ARM64 Node.js download | 1 |
| `cooper/internal/clipboard/reader_darwin.go` | **New file** -- macOS clipboard reader | 1 |
| `cooper/internal/clipboard/manager.go` | Platform dispatch for reader construction | 1 |
| `cooper/internal/docker/hostrelay.go` | Skip on macOS | 1 |
| `cooper up` startup code | Bridge bind address, HostRelay skip | 1 |
| `cooper/internal/fontsync/sync.go` | Add `DarwinSources()`, `SyncDarwinFonts()` | 2 |
| `cooper up` font sync caller | Platform dispatch | 2 |
| `cooper/internal/config/resolve.go` | Add explanatory comment | 2 |
| `cooper/README.md` | Platform support, prerequisites, port forwarding notes | 3 |
| `cooper/REQUIREMENTS.md` | Platform support section | 3 |
| `cooper/internal/configure/configure.go` | Platform-specific Docker hints | 3 |
| `cooper/test-e2e.sh` | macOS-compatible test paths | 3 |
| `cooper/internal/fontsync/sync_test.go` | Darwin source test | 3 |
| Release/CI pipeline | Add `darwin/arm64` build target | 3 |
