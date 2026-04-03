# Plan: Clipboard-Bridge for Cooper

## Status

- Date: 2026-04-03
- Scope: clipboard-bridge v1 image paste support for all officially supported
  AI CLIs in Cooper
  - `claude`
  - `opencode`
  - `codex`
  - `copilot`
- Additional scope: custom `cooper-cli-<tool>` AI barrels must be able to opt
  into the same clipboard plumbing even if Cooper does not officially support
  that CLI
- Host OS scope: Linux only for now
- This is a working document. We will keep updating it as decisions, tests,
  and tradeoffs become clearer.

## Problem

Today, clipboard-bridge does not exist in Cooper, so image paste does not work
reliably inside Cooper barrels because the AI CLI inside Docker cannot see the
host clipboard image data.

The naive fix would be to expose the host clipboard continuously to the
container. We do not want that.

The user requirement is stronger:

1. Cooper must support clipboard-bridge v1 image paste across all supported AI
   CLIs.
2. Cooper must expose the same clipboard plumbing to custom Cooper AI CLI
   barrels.
3. Security must be very good.
4. Cooper must not passively expose the live host clipboard to barrels.
5. The user must explicitly grant clipboard data to Cooper before the AI CLI
   can paste it.

## High-Level Conclusion

The correct model is:

- `cooper up` is the trust boundary and consent surface.
- The host clipboard is only read when the user explicitly asks Cooper to stage
  the current image.
- Barrels never get live clipboard access.
- A staged image grant is shared to all eligible AI barrels, authenticated,
  time-limited, visible in the TUI, and explicitly deletable.
- "All barrels" includes eligible AI barrels started after capture, not only
  barrels already running at capture time.
- Eligible AI barrels include the four officially supported tools plus custom
  Cooper AI CLI barrels by default, unless they explicitly opt out.

This is not really "mirror the live clipboard into Docker". It is "make Cooper
bridge the clipboard protocol each AI CLI expects, but only after an explicit
user grant".

## Naming

`clipboard-bridge` is a good name for this feature.

Reason:

- it describes the real job more accurately than "image paste"
- it leaves room for future clipboard payload kinds beyond images
- it still fits the current implementation model, where Cooper mediates between
  the host clipboard and tool-specific clipboard consumers inside barrels

This document uses `clipboard-bridge` as the umbrella feature name from this
point forward.

## Required Reference Reading

For any implementation session touching shim behavior or native/X11 clipboard
behavior, the plan document is not sufficient by itself.

Required reading before coding Phase 2 or Phase 3:

- `.scratch/cc-clip/README.md`
- `.scratch/cc-clip/internal/shim/template.go`
- `.scratch/cc-clip/internal/x11bridge/bridge.go`
- `.scratch/cc-clip/internal/x11bridge/*`
- `.scratch/cc-clip/internal/daemon/server.go`

Reason:

- the plan captures architecture and constraints
- `cc-clip` contains the low-level behavior that is easy to get subtly wrong,
  especially around helper interception, X11 selection ownership, `TARGETS`,
  and `INCR`

## What We Learned

### 1. `cc-clip` uses two different strategies

The most important takeaway from `.scratch/cc-clip` is that image paste support
is not one problem. It is two different problems:

1. Apps that shell out to clipboard helper binaries
   - solve with shims
   - example: `xclip`, `xsel`, `wl-paste`

2. Apps that read clipboard in-process via native APIs / X11
   - solve with an X11 selection-owner bridge on top of `Xvfb`
   - example: Codex via `arboard`

`cc-clip` makes this split explicit:

```text
Claude Code:  Mac clipboard -> daemon -> tunnel -> xclip shim
Codex CLI:    Mac clipboard -> daemon -> tunnel -> x11-bridge/Xvfb
```

Relevant references:

- `.scratch/cc-clip/README.md`
- `.scratch/cc-clip/internal/shim/template.go`
- `.scratch/cc-clip/internal/x11bridge/bridge.go`
- `.scratch/cc-clip/internal/daemon/server.go`

### 2. Current Cooper already has the transport primitive we need

Cooper already has a host process reachable from barrels through the existing
bridge + socat chain:

- host `cooper up` process listens on localhost + Docker gateway IPs
- proxy relays to `host.docker.internal`
- barrel entrypoint relays to `cooper-proxy`
- inside the barrel, the host-side bridge is already reachable as container
  local `127.0.0.1:<bridge-port>`

Relevant current files:

- `internal/bridge/server.go`
- `internal/bridge/handler.go`
- `internal/templates/entrypoint.sh.tmpl`
- `internal/templates/proxy-entrypoint.sh.tmpl`

This means Cooper does not need SSH tunneling like `cc-clip`. We already own
both ends of the path.

## Assumptions

These are the concrete assumptions the current plan depends on.

- `A1`: The existing Cooper bridge can safely host built-in `/clipboard/*`
  endpoints on the current bridge port.
- `A2`: Barrels can already reach the host bridge locally through Cooper's
  current relay chain.
- `A3`: Claude's current Linux CLI uses helper-binary clipboard access that is
  compatible with a shim approach.
- `A4`: OpenCode's current Linux CLI uses helper-binary clipboard access that
  is compatible with a shim approach.
- `A5`: Codex's current Linux CLI uses native clipboard access on Linux and is
  compatible with an X11 bridge approach.
- `A6`: Copilot's current Linux CLI uses native clipboard access on Linux and
  can read image clipboard data under `Xvfb`.
- `A7`: X11 auth via `-auth` / `XAUTHORITY` and `MIT-MAGIC-COOKIE-1` is the
  right primary control mechanism for the native clipboard path; host ACLs and
  the X11 `SECURITY` extension are not sufficient on their own.
- `A8`: The current Cooper TUI can absorb a global clipboard action and header
  TTL state without structural rewrites.
- `A9`: Current Cooper images already contain useful clipboard runtime pieces,
  but still need a few additions for the final design.
- `A10`: `cc-clip` is still a sound behavioral reference for shim and X11
  bridge mechanics.
- `A11`: Cooper's existing custom `cooper-cli-<tool>` image path is real and
  should be treated as a first-class clipboard integration target.

### 3. Tool-by-tool classification

Current evidence points to this split:

| Tool | Strategy class | Evidence | Planned approach |
|------|----------------|----------|------------------|
| `claude` | helper binary | `cc-clip` already treats Claude this way | shim |
| `opencode` | helper binary | runtime strings show `wl-paste`, `xclip`, `xsel` image reads | shim |
| `codex` | native/X11 | `cc-clip` Codex design and implementation | X11 bridge |
| `copilot` | likely native/X11 | bundled native clipboard module with image APIs; real `Xvfb` experiment succeeded | X11 bridge |

#### `opencode`

Inspection of the installed OpenCode binary inside `cooper-cli-opencode`
showed explicit image clipboard helper calls:

- `wl-paste -t image/png`
- `xclip -selection clipboard -t image/png -o`
- `xsel`
- temporary file `opencode-clipboard.png`

This places OpenCode firmly in the helper-binary category.

#### `codex`

`cc-clip` documents the key fact clearly: Codex uses `arboard`, which reads the
clipboard in-process via X11. A shim around `xclip` does not solve that. It
needs an X11 selection owner on `Xvfb`.

#### `copilot`

Copilot is not a good `xclip` shim target.

Findings from the installed `cooper-cli-copilot` image:

- the package includes `@teddyzhu/clipboard`
- that dependency exposes image clipboard APIs
- Copilot's packaged JS outside vendored clipboard code does not give a clean,
  obvious image-paste call site

But the important technical experiment succeeded:

1. ran `Xvfb` inside the Copilot image
2. wrote a real PNG to the X11 clipboard with `xclip`
3. read it back using Copilot's bundled native clipboard library
4. result:
   - `hasImage = true`
   - `getImageData()` succeeded on a real PNG

That is strong evidence that Copilot belongs in the same implementation family
as Codex: native clipboard via X11, not shell shims.

### 4. Linux terminal "paste image into `cooper up` first" is not the right literal primitive

We should keep the security intent, but probably not the literal mechanism.

Reason:

- normal Linux terminal paste is text-oriented
- terminals generally do not deliver an image clipboard payload to stdin as a
  raw image MIME object
- relying on "first paste the image into the terminal" is not a portable or
  robust image-ingress mechanism

So the right implementation is:

- `cooper up` provides an explicit "capture current host clipboard image now"
  action
- that action is the consent boundary
- after that, the staged image becomes available to eligible AI barrels until
  it expires, is replaced, or is deleted

This preserves the security property the user wants without depending on a
terminal image-paste feature that usually does not exist.

## Security Model

Clipboard-bridge should be designed around staged clipboard grants, not live
clipboard access.

### Security goals

1. No passive host clipboard exposure to barrels.
2. No background polling of the host clipboard.
3. User must explicitly grant each staged image to Cooper.
4. The staged image must only be available to valid Cooper AI barrel sessions.
5. Clipboard delivery must be authenticated.
6. Clipboard grants must expire automatically.
7. Clipboard data must never be logged.
8. Clipboard state should be memory-first and short-lived.

### Security non-goals

1. This does not protect against a compromised AI CLI after the user explicitly
   grants it an image.
2. This does not solve text clipboard sync in general.
3. This does not guarantee that upstream CLIs behave well with pasted images.

### Proposed security properties

#### Explicit grant only

The host clipboard reader runs only when the user triggers it from `cooper up`.

There is no endpoint that proxies the live clipboard on demand.

#### Staged image, not live clipboard

`cooper up` captures the current host clipboard image once and stores it as a
staged object in memory:

- bytes
- format
- size
- dimensions if cheaply available
- SHA-256 for display/debugging
- scope marker: all eligible AI barrels
- creation time
- expiry time
- last access time
- revocation state

#### Generic staged object, image-focused v1

The internal clipboard-bridge core should not be hard-coded to "image only"
objects forever.

Recommended internal model:

- stage a generic clipboard object envelope
- keep original bytes and original clipboard metadata
- allow derived/canonical representations to be attached to the staged object

Suggested staged-object fields:

- raw bytes
- MIME type
- optional filename
- optional file extension
- original clipboard targets/types
- semantic kind:
  - `image`
  - `text`
  - `file`
  - `unknown`
- derived variants keyed by MIME type, for example:
  - `image/png`

Recommended rule:

- core clipboard-bridge state and auth should be generic
- v1 capture policy should accept only image objects
- current tool adapters should consume only the image forms they actually
  support

This keeps the core reusable if we later add clipboard-bridge support for
payloads like CSV, text fragments, or file attachments.

#### Per-barrel authentication

Each running barrel gets a session token.

Any valid token from an eligible Cooper AI barrel may read the staged image.

Invalid tokens, non-barrel callers, and non-eligible containers must be
rejected.

Current recommendation:

- keep per-barrel tokens even though clipboard scope is shared
- mount tokens as read-only files, not env vars
- rotate tokens on barrel restart/recreate
- never print tokens in logs, diagnostics, or shell output
- custom `cooper-cli-*` AI barrels should be clipboard-eligible by default,
  unless they explicitly set clipboard mode to `off`

#### Configurable TTL + explicit deletion

Suggested initial policy:

- default TTL: 5 minutes
- configurable TTL
- staged image remains readable by eligible AI barrels until expiry
- user can delete it early from `cooper up`
- capturing a new image replaces the previous staged image

This is less strict than target-scoped delivery. The safety model becomes
"explicitly staged by the user, kept for a bounded time, visible in the TUI,
and only readable by eligible Cooper AI barrels".

#### Image-only, size-capped

The clipboard-bridge core may be generic, but the exposed supported payload for
v1 should still be image-only.

Suggested hard cap:

- 20 MB maximum image payload

Rejecting oversized clipboard payloads is good both for safety and for
practical CLI prompt limits.

#### Format normalization

User requirement: Cooper should support all image formats from the host
clipboard, not only PNG.

Implementation direction:

- accept any clipboard payload that is recognizable as an image
- normalize staged storage to a canonical internal representation
- expose PNG to helper shims and X11 clipboard consumers unless a tool
  explicitly requires something else
- preserve original format metadata for diagnostics, but do not rely on the
  original format surviving end-to-end
- use a two-tier conversion stack:
  - in-process decode plus PNG re-encode for common raster formats
  - external fallback conversion for uncommon `image/*` payloads

This keeps downstream adapters simple while still accepting a broad set of host
clipboard image formats.

#### Exact conversion requirement

Image conversion must be a first-class implementation requirement, not a
best-effort extra.

Required behavior:

- if the clipboard advertises an `image/*` payload, Cooper should attempt to
  ingest it
- staged storage should be canonical PNG bytes plus metadata about the original
  clipboard type
- PNG input should pass through without lossy conversion
- raster formats should decode to an image buffer and re-encode to PNG
- animated formats should use the first frame in v1
- vector formats should rasterize to PNG before staging
- if an `image/*` payload is unsupported by the in-process codecs, Cooper
  should fall back to a configured external converter rather than immediately
  failing

Recommended conversion stack:

1. detect MIME/target from clipboard metadata and sniff bytes
2. direct-validate PNG input
3. in-process decode for common raster formats:
   - PNG
   - JPEG
   - GIF
   - BMP
   - TIFF
   - WEBP
4. external fallback conversion to PNG for uncommon image formats, for example:
   - SVG
   - AVIF
   - HEIC/HEIF
   - ICO
   - any other clipboard payload advertised as `image/*`

Recommended fallback converter:

- `magick` / ImageMagick on the host

Because the user requirement is "all image formats", startup checks and
`cooper doctor` should validate both:

- clipboard-read prerequisites
- image-conversion prerequisites

Why raw pass-through is not enough for v1:

- current supported AI CLIs do not all consume arbitrary clipboard blobs
- helper-binary tools often explicitly request `image/png`
- native/X11 consumers ask for specific clipboard targets, not just "some
  bytes"
- if the host clipboard holds JPEG, WEBP, SVG, HEIC, or another non-PNG image,
  Cooper must often transform it into the image representation the CLI
  actually expects

So the right split is:

- generic staged clipboard object internally
- typed protocol adaptation at the barrel edge
- image normalization for current AI CLI compatibility

#### No raw bytes in logs

Only log minimal access events:

- container/tool id
- access time
- whether access succeeded

Never log the image body, MIME details, dimensions, hashes, auth headers, or
tokens.

## Proposed UX

### User flow

1. User runs `cooper up`
2. From any panel, user presses `c`
3. Cooper reads the current Linux clipboard image on the host
4. TUI header shows staged state and remaining TTL
5. User switches to any running AI CLI shell
6. User presses `Ctrl+V`
7. The tool-specific shim or X11 bridge reads the staged image
8. The staged image remains available until it expires, is replaced, or the
   user presses `x` to delete it early

### Important UX note

The intent is "explicitly grant clipboard content to Cooper from the TUI", not
literally "terminal stdin receives the image bytes".

### TUI surface

Clipboard should be a global surface in `cooper up`, not its own tab.

Current preferred UX:

- `c` from any panel captures the current host clipboard image
- right side of the header shows clipboard state
- when staged, the header shows:
  - scope: all running and future eligible AI barrels until expiry
  - remaining TTL
  - shrinking duration bar reusing the monitor timer style
- `x` deletes the staged clipboard image early
- states:
  - empty
  - staged
  - expired
  - capture failed
- shortcut caveat:
  - treat `c` / `x` as app-level shortcuts only when Cooper is not currently
    editing a text input or form field
  - text-entry surfaces must keep receiving literal `c` / `x` keystrokes

The current TUI code supports this direction well:

- the app-level key handler already has a small global-key layer
- the header has room for another status segment on the right
- the existing timer bar component can be reused for the TTL visualization

### Why this is safer than live clipboard bridging

With this design:

- the barrel cannot fetch whatever happens to be in the user's clipboard later
- the user decides when to expose one specific clipboard image
- exposure is time-limited, limited to eligible Cooper AI barrels, and visible
  in the TUI

## Proposed Architecture

We should implement this as four cooperating pieces.

### A. Host clipboard capture in `cooper up`

New package, likely `internal/clipboard/`, responsible for:

- reading the Linux clipboard on explicit user action
- building a staged clipboard object envelope
- converting clipboard image formats into Cooper's canonical PNG staged form
  when the staged object is an image
- staging clipboard grants in memory
- clearing expired grants
- serving authenticated clipboard metadata and typed payload bytes

Likely internal components:

- `Reader` interface
- Linux backend(s)
- `Manager` for staged grant lifecycle
- `Object` / `Variant` model for staged clipboard content
- HTTP handler(s)

Startup behavior:

- if host prerequisites for the active Linux clipboard backend are missing,
  `cooper up` should refuse to start
- the failure should print concrete install instructions for the missing tools
- `cooper doctor` should report the same prerequisite failures
- the same startup/doctor checks should validate the required image conversion
  backend

### B. Authenticated clipboard service

This should be separate from user-defined script routes conceptually, even if it
shares the same `cooper up` process.

Chosen direction:

- same existing bridge port
- reserve `/clipboard/*` paths for clipboard operations
- keep clipboard auth and route auth completely separate internally

This means the bridge must treat `/clipboard/*` as a protected built-in
namespace, not as user-configurable routes.

Clipboard endpoints would be something like:

- `GET /health`
- `GET /clipboard/type`
- `GET /clipboard/image`

All clipboard endpoints must require bearer auth.

Design note:

- the internal clipboard-bridge should be generic enough to hold arbitrary
  typed clipboard objects
- v1 external endpoints can remain image-focused because that is what the
  current supported AI CLIs need
- when we later add CSV/file/text clipboard support, we should prefer adding
  generic metadata/blob endpoints over cloning image-specific logic again

Additional hard requirement when reusing the bridge port:

- user-defined bridge routes must not be allowed under `/clipboard/*`
- built-in clipboard handlers must run before generic route matching
- existing user routes under `/clipboard/*` must be ignored/replaced by the
  built-in clipboard handlers at runtime

Validation direction:

- reject reserved-prefix routes in `cooper configure`
- reject reserved-prefix routes in `cooper up` route add/edit UI
- validate on startup/load and sanitize existing persisted routes that collide
  with `/clipboard/*`

### C. Tool-specific adapters inside barrels

#### Shim path: `claude`, `opencode`

Install Cooper-owned wrapper scripts earlier in `PATH` for:

- `xclip`
- `xsel`
- optionally `wl-paste`

Behavior:

- intercept only image clipboard reads / target negotiation
- call local barrel clipboard relay endpoint
- fall back to real binary for everything else

This follows the `cc-clip` shim design closely.

Important boundary:

- the bridge core may store a generic staged clipboard object
- the shim still needs to answer in the exact helper-binary shape the target
  CLI requests
- for current tools, that means image clipboard semantics, not arbitrary file
  passthrough

#### Native path: `codex`, `copilot`

Inside the barrel:

1. start `Xvfb`
2. start `cooper-x11-bridge`
3. export `DISPLAY=127.0.0.1:<display>`
4. set `XAUTHORITY` to a Cooper-managed cookie file
5. native CLI reads clipboard through X11
6. `cooper-x11-bridge` fetches the staged image from the host clipboard service

This follows the `cc-clip` Codex pattern.

Important boundary:

- X11 clipboard consumers do not ask for a generic opaque blob
- they negotiate clipboard targets/formats
- clipboard-bridge must therefore advertise and serve the target types the
  native CLI can actually consume

### D. Custom AI CLI barrels

This feature must not be limited to the four built-in AI CLIs.

Cooper already supports custom `cooper-cli-<tool>` images. Clipboard support
should be exposed to those barrels through a documented runtime contract.

Recommended contract for custom AI CLI barrels:

- custom images inherit from `cooper-base`
- custom images keep using Cooper's standard entrypoint/runtime plumbing
- each custom AI tool declares a clipboard mode:
  - `auto`
  - `off`
  - `shim`
  - `x11`
- default for Cooper barrels should be `auto`, not `off`
- `shim` mode may request any combination of:
  - `xclip`
  - `xsel`
  - `wl-paste`
- `x11` mode gets:
  - `Xvfb`
  - `cooper-x11-bridge`
  - `DISPLAY`
  - `XAUTHORITY`
- all enabled clipboard modes get:
  - a per-barrel clipboard token file
  - bridge port/runtime env
  - access to the staged clipboard endpoints

Meaning of `auto`:

- install helper shims
- start native X11 clipboard plumbing
- let the CLI consume whichever path it actually uses

This is intentionally broader than the built-in tool-specific minimum. It keeps
custom Cooper barrels working by default without forcing a manual classification
step up front.

This makes clipboard support reusable for unsupported AI CLIs without giving
arbitrary user containers clipboard access by default.

Security assumptions and requirements:

- configure loopback TCP access explicitly in Cooper; do not rely on X server
  defaults across builds
- use X11 auth via MIT-MAGIC-COOKIE-1 and an `XAUTHORITY` file
- do not rely on host-based access control via `xhost`
- do not rely on the X11 SECURITY extension as the primary isolation mechanism
- do not expose Xvfb outside container loopback
- token and X authority files should be mode `0600`

This keeps the X server reachable only to processes in the barrel that possess
the local X cookie.

### Why `DISPLAY=127.0.0.1:N`

For Codex, `cc-clip` found that the sandbox blocks access to
`/tmp/.X11-unix`, so TCP loopback display addressing is required:

- use `DISPLAY=127.0.0.1:<n>`
- make `Xvfb` listen on TCP loopback

Copilot may not require this, but using the same model for all native/X11 tools
is simpler and safer.

### Fixed vs dynamic display number

Because each barrel is its own container namespace, a fixed display like `:99`
is acceptable in Cooper.

That is simpler than `cc-clip`'s dynamic remote-display lifecycle.

Initial recommendation:

- use a fixed display per barrel container
- expose it as TCP loopback

If a native CLI later proves sensitive to this, we can switch to dynamic
allocation.

## Linux Clipboard Capture Backend

For Linux host clipboard capture, the simplest initial backend is external tool
based:

### Wayland

- list types with `wl-paste --list-types`
- read image with `wl-paste --type image/png`

### X11

- list targets with `xclip -selection clipboard -t TARGETS -o`
- read image with `xclip -selection clipboard -t image/png -o`

This keeps the host-side implementation simple and explicit.

We should expect possible prerequisites on the host:

- `wl-clipboard` for Wayland
- `xclip` for X11
- `magick` / ImageMagick for uncommon image-format conversion fallback

Expected startup checks:

- detect whether the host clipboard path is Wayland or X11
- verify the required host tool exists before `cooper up` starts
- verify the image-conversion backend exists before `cooper up` starts
- print distro-oriented install hints when missing

We can later decide whether to keep these as external dependencies or replace
them with an embedded Go/Linux backend.

## Endpoint Contract Sketch

This is a draft, not final.

Recommended shape:

- keep simple image-focused endpoints for v1 compatibility
- design the implementation so these are convenience views over a more generic
  staged clipboard object
- when clipboard-bridge grows beyond images, add generic endpoints such as:
  - `GET /clipboard/object`
  - `GET /clipboard/blob`
  - optional content negotiation or explicit MIME selection

### `GET /clipboard/type`

Auth required.

Response examples:

```json
{"type":"empty"}
```

```json
{"type":"image","format":"png","size":504703}
```

### `GET /clipboard/image`

Auth required.

Responses:

- `200` with image bytes
- `204` if no staged image
- `401` if token invalid
- `403` if token is valid but caller is not an eligible Cooper AI barrel
- `413` if staged image exceeds configured size cap

Future-facing rule:

- the bridge should not assume every staged object is an image forever
- current image endpoints exist because today’s supported CLI adapters need
  them

## Concrete Implementation Reference

This section is intentionally prescriptive. The goal is to reduce ambiguity for
the implementation session.

### Suggested Go Types

Example shape for the generic staged clipboard object:

```go
type ClipboardKind string

const (
    ClipboardKindImage   ClipboardKind = "image"
    ClipboardKindText    ClipboardKind = "text"
    ClipboardKindFile    ClipboardKind = "file"
    ClipboardKindUnknown ClipboardKind = "unknown"
)

type ClipboardVariant struct {
    MIME   string
    Bytes  []byte
    Size   int64
    Width  int
    Height int
}

type ClipboardObject struct {
    Kind            ClipboardKind
    MIME            string
    Filename        string
    Extension       string
    Raw             []byte
    RawSize         int64
    OriginalTargets []string
    Variants        map[string]ClipboardVariant
}

type StagedSnapshot struct {
    ID           string
    Object       ClipboardObject
    CreatedAt    time.Time
    ExpiresAt    time.Time
    LastAccessAt time.Time
    AccessCount  int
}
```

Suggested capture result shape:

```go
type CaptureResult struct {
    MIME            string
    Filename        string
    Extension       string
    Bytes           []byte
    OriginalTargets []string
}

type Reader interface {
    Read(ctx context.Context) (*CaptureResult, error)
}
```

Suggested token/session metadata:

```go
type BarrelSession struct {
    Token         string
    ContainerName string
    ToolName      string
    ClipboardMode string // off, shim, x11
    Eligible      bool
}
```

### Race-Free Manager Pattern

Recommended concurrency model:

- one immutable `StagedSnapshot`
- readers operate on a snapshot pointer captured once at request start
- stage/replace installs a brand-new snapshot atomically
- clear swaps the pointer to `nil` atomically
- no handler should stream from mutable shared state

Acceptable implementation patterns:

- `sync.RWMutex` around the current snapshot pointer
- or `atomic.Pointer[StagedSnapshot]` plus a small mutex for mutation and
  access-time updates

Recommended manager API shape:

```go
type Manager interface {
    Stage(obj ClipboardObject, ttl time.Duration) (*StagedSnapshot, error)
    Clear()
    Current() *StagedSnapshot
    Touch(id string, when time.Time)
    ValidateToken(token string) (*BarrelSession, error)
}
```

Implementation rule:

- `Stage()` must fully build the new immutable object before publishing it
- `Clear()` must revoke future reads immediately
- in-flight reads may finish from the old snapshot, but must never observe
  mixed old/new bytes

### HTTP/Auth Examples

All clipboard-bridge requests should use bearer auth:

```http
GET /clipboard/type HTTP/1.1
Host: 127.0.0.1:4343
Authorization: Bearer <token>
```

Recommended v1 `GET /clipboard/type` response:

```json
{
  "state": "staged",
  "kind": "image",
  "mime": "image/png",
  "raw_mime": "image/jpeg",
  "size": 504703,
  "available_variants": ["image/png"],
  "created_at": "2026-04-03T14:20:00Z",
  "expires_at": "2026-04-03T14:25:00Z"
}
```

Recommended v1 `GET /clipboard/image` behavior:

- default response MIME: `image/png`
- optional future query: `?mime=image/png`
- `Cache-Control: no-store`
- `X-Cooper-Clipboard-Id: <snapshot-id>`

Recommended errors:

```json
{"error":"no staged clipboard image"}
```

```json
{"error":"invalid clipboard token"}
```

```json
{"error":"clipboard payload exceeds configured size cap"}
```

Implementation rule:

- do not trust any container-provided tool name or mode on the request
- resolve eligibility from the host-side token/session map only

### Runtime Contract Inside Barrels

Recommended mounted files:

- token file: `/etc/cooper/clipboard-token`
- X authority file for native mode: `/etc/cooper/clipboard.xauth`

Recommended env vars:

```bash
COOPER_CLIPBOARD_ENABLED=1
COOPER_CLIPBOARD_MODE=auto   # auto / shim / x11 / off
COOPER_CLIPBOARD_BRIDGE_URL=http://127.0.0.1:${COOPER_BRIDGE_PORT}
COOPER_CLIPBOARD_TOKEN_FILE=/etc/cooper/clipboard-token
COOPER_CLIPBOARD_XAUTHORITY=/etc/cooper/clipboard.xauth
COOPER_CLIPBOARD_DISPLAY=127.0.0.1:99
COOPER_CLIPBOARD_SHIMS=xclip,xsel
```

Rules:

- built-in tool images should receive these automatically
- custom images should default to `COOPER_CLIPBOARD_MODE=auto`
- custom images may set `COOPER_CLIPBOARD_MODE=off` to opt out
- custom images may set explicit `shim` or `x11` to reduce runtime overhead
- host-side token/session registration still decides actual eligibility

### Shim Implementation Example

Recommended pattern:

- install real helper binaries in their normal distro path
- place Cooper wrapper scripts earlier in `PATH`, for example in
  `/home/user/.local/bin`
- wrapper handles only supported clipboard read cases and falls back to the
  real helper for everything else

Example `xclip` wrapper flow:

```bash
#!/bin/bash
set -euo pipefail

REAL_XCLIP=/usr/bin/xclip
TOKEN=$(cat "${COOPER_CLIPBOARD_TOKEN_FILE}")
BRIDGE="${COOPER_CLIPBOARD_BRIDGE_URL}"

if [[ "$*" == *"-selection clipboard"* && "$*" == *"-t TARGETS"* && "$*" == *"-o"* ]]; then
  printf 'TARGETS\nimage/png\n'
  exit 0
fi

if [[ "$*" == *"-selection clipboard"* && "$*" == *"-t image/png"* && "$*" == *"-o"* ]]; then
  exec curl -fsS \
    -H "Authorization: Bearer ${TOKEN}" \
    "${BRIDGE}/clipboard/image"
fi

exec "${REAL_XCLIP}" "$@"
```

Implementation notes:

- do not parse helper arguments lazily; match only the exact read cases Cooper
  wants to intercept
- preserve binary-safe stdout behavior
- stderr should stay quiet on success
- fallback must preserve normal helper behavior for unrelated calls

### Native/X11 Implementation Example

Recommended safer runtime pattern:

1. start `Xvfb` with auth enabled
2. keep the native X server on the local container only
3. expose the TCP loopback display the CLI expects
4. run `cooper-x11-bridge` as the clipboard selection owner

Recommended concrete pieces:

- `cmd/cooper-x11-bridge`
- a launcher script generated from a template
- `xauth` cookie generation during barrel startup

Implementation choice:

- `cooper-x11-bridge` should be a real Go binary
- shelling out to X11 helper tools is acceptable for startup concerns such as
  `Xvfb`, `xauth`, and `mcookie`
- shelling out is not acceptable for the X11 bridge itself

Reason:

- the bridge must stay resident as selection owner
- it must implement `TARGETS`, `image/png`, and `INCR` correctly
- it must integrate cleanly with bearer auth, retries, and tests
- hiding that behavior behind `xclip`/`xsel` subprocesses would make the
  native clipboard path brittle and harder to verify

Illustrative launcher sketch:

```bash
XAUTH_FILE=/etc/cooper/clipboard.xauth
DISPLAY_NUM=99
DISPLAY_ADDR=127.0.0.1:${DISPLAY_NUM}

xauth -f "${XAUTH_FILE}" add "${DISPLAY_ADDR}" . "$(mcookie)"
Xvfb ":${DISPLAY_NUM}" -screen 0 1024x768x24 -auth "${XAUTH_FILE}" >/tmp/xvfb.log 2>&1 &
export DISPLAY="${DISPLAY_ADDR}"
export XAUTHORITY="${XAUTH_FILE}"

cooper-x11-bridge \
  --display "${DISPLAY}" \
  --xauthority "${XAUTHORITY}" \
  --bridge-url "${COOPER_CLIPBOARD_BRIDGE_URL}" \
  --token-file "${COOPER_CLIPBOARD_TOKEN_FILE}" &
```

Implementation rules:

- if direct Xvfb TCP exposure cannot be constrained to container loopback,
  use a local loopback proxy to the X server rather than exposing container
  network TCP broadly
- `cooper-x11-bridge` must answer `TARGETS` and `image/png`
- large transfers must support `INCR`
- native mode should never bypass bearer auth to the host bridge

### Custom Barrel Examples

Recommended custom default-mode Dockerfile example:

```Dockerfile
FROM cooper-base

ENV COOPER_CLI_TOOL=my-custom

RUN npm install -g my-custom-cli
```

Recommended custom opt-out Dockerfile example:

```Dockerfile
FROM cooper-base

ENV COOPER_CLI_TOOL=my-isolated-cli
ENV COOPER_CLIPBOARD_MODE=off

RUN npm install -g my-isolated-cli
```

Recommended custom forced X11-mode Dockerfile example:

```Dockerfile
FROM cooper-base

ENV COOPER_CLI_TOOL=my-vision-cli
ENV COOPER_CLIPBOARD_MODE=x11

RUN npm install -g my-vision-cli
```

Implementation rule:

- a custom barrel must not get clipboard access merely because its image name
  starts with `cooper-cli-`
- it gets clipboard-bridge by default as a Cooper barrel unless it opts out
- host session registration must still mark it eligible

### TUI Example States

Recommended header examples:

```text
Clipboard Empty  [c Copy]
Clipboard Staged [████████░░] 287s  [c Replace] [x Delete]
Clipboard Failed: no image in host clipboard  [c Retry]
```

Implementation rule:

- capture/replace/delete should surface immediate feedback in the TUI
- expiry should transition the header state automatically without user action

### Suggested Config Additions

Recommended config fields:

```json
{
  "clipboard_ttl_secs": 300,
  "clipboard_max_bytes": 20971520
}
```

Recommended semantics:

- `clipboard_ttl_secs`
  - default `300`
  - used by `cooper up` when staging new clipboard objects
- `clipboard_max_bytes`
  - default `20971520` (20 MiB)
  - enforced after capture and after image conversion

Suggested validation rules:

- TTL must be positive
- max bytes must be positive
- max bytes must be enforced on both raw and derived variants

## Faithful Implementation Rules

These are non-negotiable unless this document is updated deliberately.

- do not expose the live host clipboard continuously to barrels
- do not make clipboard access implicit; user must press `c` in `cooper up`
- do not auto-clear the staged clipboard on first successful read
- do not scope the staged clipboard to only one barrel
- do not log clipboard bytes, MIME details, filenames, hashes, or auth tokens
- do not store clipboard auth tokens in environment variables
- do not let user-defined bridge routes occupy `/clipboard/*`
- do not implement the v1 core as an image-only data model; keep the staged
  object generic
- do not implement current barrel adapters as raw blob passthrough; they must
  satisfy the exact helper/X11 protocol the CLI expects
- do not implement `cooper-x11-bridge` as shell glue around `xclip`/`xsel`; it
  should be a real Go X11 client/selection owner
- do not make `c` / `x` unconditional globals while a text input is active
- do not require manual clipboard opt-in for Cooper barrels; default to
  clipboard enabled and let barrels opt out with `COOPER_CLIPBOARD_MODE=off`

## Tool Support Plan

### 1. `claude`

Planned implementation:

- install `xclip` shim
- install `wl-paste` shim if useful
- use clipboard service `/type` + `/image`

Expected difficulty: low to moderate

### 2. `opencode`

Planned implementation:

- install `xclip` shim
- install `xsel` shim
- optionally install `wl-paste` shim
- keep existing headless display support where useful

Important current-state note:

- current Cooper base installs `xclip` and `xvfb` when OpenCode is enabled
- it does not currently install `xsel`

Expected difficulty: low to moderate

### 3. `codex`

Planned implementation:

- add `Xvfb`
- add `cooper-x11-bridge`
- export TCP-loopback `DISPLAY`
- authenticated fetch from staged clipboard service

Expected difficulty: moderate

### 4. `copilot`

Planned implementation:

- treat like native/X11 clipboard consumer
- add `Xvfb`
- add `cooper-x11-bridge`
- verify real user-facing paste flow end-to-end in the actual CLI

Expected difficulty: moderate, with more upstream uncertainty than Codex

Important evidence already collected:

- Copilot's bundled clipboard module successfully read a real PNG from an X11
  clipboard under `Xvfb`

### 5. Custom AI CLI barrels

Planned implementation:

- expose a stable clipboard runtime contract to any custom
  `cooper-cli-<tool>` image
- default custom tools to `auto` clipboard mode
- allow custom tools to force `shim` or `x11`, or opt out with `off`
- keep token/auth behavior identical to built-in tools
- document the expected env vars, mounted files, and helper binaries

Expected difficulty: moderate

## Cooper Code Areas Likely to Change

This list is intentionally concrete so the implementation work is easy to map.

### Config / validation

- `internal/config/config.go`
  - add clipboard config fields, likely:
    - `ClipboardTTLSeconds`
    - `ClipboardMaxBytes`
  - validate positive values
- config-related tests
  - validate defaults
  - validate negative/zero rejection

### Host-side startup and services

- `internal/app/cooper.go`
  - start/stop clipboard service with `cooper up`
  - wire events/logging/state into the TUI
  - fail startup with prerequisite instructions when clipboard backend is
    unavailable

### HTTP service / auth / state

- `internal/bridge/` if we extend the existing service
- or new `internal/clipboard/` package if we keep clipboard separate
- `internal/bridge/handler.go`
  - reserve `/clipboard/*`
  - dispatch built-in clipboard handlers before generic user routes
- `internal/bridge/config.go`
  - sanitize persisted routes that collide with `/clipboard/*`

Recommended new package:

- `internal/clipboard/types.go`
- `internal/clipboard/reader_linux.go`
- `internal/clipboard/manager.go`
- `internal/clipboard/http.go`
- `internal/clipboard/token.go`
- `internal/clipboard/normalize.go`
- `internal/clipboard/convert.go`
- `internal/clipboard/convert_external.go`
- `internal/clipboard/errors.go`

### Docker/build/runtime templates

- `internal/templates/templates.go`
  - pass clipboard-related template data and env defaults into rendered files
- `internal/templates/templates_test.go`
  - assert clipboard deps, env vars, and launcher/shim rendering
- `internal/templates/base.Dockerfile.tmpl`
  - install any missing runtime deps:
    - `xclip`
    - `xsel`
    - `xvfb`
    - `xauth`
  - keep clipboard runtime dependencies in the shared base so custom
    `cooper-cli-*` images can inherit them
- `internal/templates/entrypoint.sh.tmpl`
  - generate shim files
  - start `Xvfb` for native-clipboard tools
  - start `cooper-x11-bridge` where required
  - mount/use clipboard token file
  - mount/use X authority file
- likely new templates for:
  - `xclip` shim
  - `xsel` shim
  - optional `wl-paste` shim
  - `cooper-x11-bridge` launcher script
  - custom clipboard runtime contract docs/examples

### New commands / binaries

- `cmd/cooper-x11-bridge/main.go`
  - own X11 clipboard selections for native mode
  - fetch staged clipboard variants from the host bridge
  - serve `TARGETS`, `image/png`, and `INCR`

### Barrel/container setup

- `internal/docker/barrel.go`
  - mount session token file
  - mount X authority file for native-clipboard tools
  - mount any clipboard runtime config
  - pass env vars for clipboard relay port / token path
  - pass clipboard mode/config for custom AI CLI barrels

### TUI

- `internal/tui/model.go`
- `internal/tui/messages.go`
- `internal/tui/...`
- `internal/tui/bridgeui/routes.go`
  - reject reserved `/clipboard/*` prefix on add/edit

Likely additions:

- global clipboard capture/delete actions
- header clipboard status segment
- countdown bar reuse in header
- events for capture, clear, expired, fetch metadata

### Tests

- unit tests for Linux clipboard reader parsing and failure cases
- unit tests for manager TTL/clear/replace semantics
- unit tests for image conversion and PNG canonicalization
- shim tests copied from `cc-clip` style
- X11 bridge tests copied from `cc-clip` style
- Cooper integration tests for service startup and auth
- integration tests for custom AI CLI barrel clipboard modes

## Recommended Implementation Phases

### Phase 0: locked design decisions for v1

These should be treated as implementation inputs, not open questions:

1. all four supported AI CLIs are in scope
2. custom Cooper AI CLI barrels must get clipboard-bridge by default and be
   able to opt out
3. Linux host only for now
4. staged clipboard grants, not live clipboard bridge
5. explicit user action in `cooper up`
6. host clipboard backend uses external Linux tools in v1:
   - `wl-paste` on Wayland
   - `xclip` on X11
7. uncommon image-format conversion fallback uses host `magick` in v1
8. `cooper-x11-bridge` is a real Go binary in v1; shelling out is acceptable
   only for X11 startup helpers

### Phase 1: host clipboard capture + secure staging

Deliverables:

- Linux clipboard reader
- generic staged clipboard-object model
- image normalization pipeline
- image conversion pipeline with explicit uncommon-format fallback
- in-memory staged grant manager
- token auth
- TTL / clear / replace state machine
- tests

This phase alone should still expose nothing to barrels until the user stages
an image.

### Phase 2: helper-binary path

Deliverables:

- `xclip` shim
- `xsel` shim
- optional `wl-paste` shim
- integrate `claude`
- integrate `opencode`
- expose helper-shim mode to custom AI CLI barrels

This should bring up two of the four CLIs quickly.

### Phase 3: native X11 path

Deliverables:

- `cooper-x11-bridge`
- barrel-side `Xvfb` startup
- TCP-loopback `DISPLAY`
- X11 cookie generation and `XAUTHORITY` wiring
- integrate `codex`
- integrate `copilot`
- expose native/X11 mode to custom AI CLI barrels

### Phase 4: TUI polish and operational safety

Deliverables:

- explicit clipboard/paste UI
- clear status and expiry display
- minimal access logging
- docs and doctor checks

## Test Plan

### Unit tests

- clipboard reader:
  - image present
  - no image
  - unsupported backend
  - oversized image
- manager:
  - stage
  - authorize valid barrel token
  - expire
  - clear
  - replace
  - repeated reads within TTL from valid barrel tokens
  - reject wrong token
  - no torn reads during replace/delete races
- normalization:
  - PNG passthrough preserves bytes when already canonical
  - JPEG converts to PNG
  - WEBP converts to PNG
  - GIF converts first frame to PNG
  - TIFF converts to PNG
  - BMP converts to PNG
  - SVG rasterizes to PNG through fallback converter
  - AVIF converts to PNG through fallback converter
  - HEIC/HEIF converts to PNG through fallback converter
  - ICO converts to PNG through fallback converter
  - alpha channel survives conversion where applicable
  - oversized converted output is rejected cleanly
  - unsupported or malformed image input reports a clear error

### Shim tests

Modeled after `cc-clip`:

- TARGETS/type interception
- image fetch interception
- binary-safe fetch path
- fallback to real binary on failure
- custom barrel in `shim` mode can read staged image through the documented
  shim contract

### X11 bridge tests

Modeled after `cc-clip`:

- claim ownership
- TARGETS response
- small direct transfer
- large `INCR` transfer
- tunnel/service down does not hang
- invalid token rejected
- custom barrel in `x11` mode can read staged image through X11

### End-to-end tests

Per tool:

1. stage image in `cooper up`
2. open running barrel
3. paste in actual CLI
4. confirm tool receives image
5. confirm staged image remains available until expiry or manual delete
6. confirm another running AI barrel can also read it while staged
7. confirm a barrel started after capture can also read it while staged
8. confirm invalid or missing token is rejected
9. confirm expiry or manual delete removes access

We should not call this feature done until all four supported CLIs pass their
real end-to-end tests.

Script direction:

- extend [test-e2e.sh](/home/ricky/Personal/govner/cooper/test-e2e.sh) with
  interactive image-paste tests for `claude`, `opencode`, `codex`, and
  `copilot`
- add generic custom-barrel clipboard smoke tests:
  - custom shim-mode barrel
  - custom x11-mode barrel

## Assumption Verification

### `A1` Same-Port Clipboard Endpoints

- Method:
  source inspection of `internal/bridge/handler.go` plus `go test
  ./internal/bridge`
- Evidence:
  the current handler already reserves built-in paths (`/health`, `/routes`)
  and dispatches them before generic route matching
- Result:
  verified. Reusing the existing bridge port is structurally compatible with
  reserved `/clipboard/*` handlers.

### `A2` Existing Barrel-to-Host Reachability

- Method:
  source inspection of `internal/bridge/server.go` and
  `internal/templates/entrypoint.sh.tmpl`, plus `go test
  ./internal/templates ./internal/app`
- Evidence:
  the bridge binds `127.0.0.1` plus explicit gateway IPs, and the barrel
  entrypoint already runs a loopback `socat` relay for the bridge port
- Result:
  verified. Cooper already has the transport path needed for clipboard
  requests.

### `A3` Claude Helper-Binary Strategy

- Method:
  runtime inspection of the installed `cooper-cli-claude` image with
  `docker run`
- Evidence:
  the shipped Claude binary contains `xclip`, `xsel`, `--clipboard`, and
  `image/png` strings
- Result:
  verified by runtime inspection. The shim strategy is consistent with the
  current shipped Claude image. Full interactive CLI paste e2e is still
  pending.

### `A4` OpenCode Helper-Binary Strategy

- Method:
  runtime inspection of the installed `cooper-cli-opencode` image with
  `docker run`
- Evidence:
  the shipped OpenCode binary contains `wl-paste`, `xclip`, `xsel`,
  `opencode-clipboard.png`, and `image/png`
- Result:
  verified by runtime inspection. OpenCode belongs in the helper-binary shim
  path.

### `A5` Codex Native Clipboard Strategy

- Method:
  runtime inspection of the installed `cooper-cli-codex` image with
  `docker run`
- Evidence:
  the shipped Codex package contains a Rust binary with
  `arboard-3.6.1/src/platform/linux/x11.rs`,
  `x11rb-0.13.2/src/rust_connection/mod.rs`, `DISPLAY`,
  `WAYLAND_DISPLAY`, `XAUTHORITY`, `clipboard unavailable:`,
  `no image on clipboard:`, and `pasted image size=`
- Result:
  verified by runtime inspection. The current shipped Codex image still fits
  the native/X11 bridge design.

### `A6` Copilot Native Clipboard Strategy

- Method:
  package inspection plus a live `Xvfb` clipboard read experiment inside
  `cooper-cli-copilot`
- Evidence:
  the package includes `@teddyzhu/clipboard`, and after writing a real PNG to
  the X11 clipboard under `Xvfb`, Copilot's bundled clipboard module reported
  `hasImage true`, `width 48`, `height 48`, `size 1873`
- Result:
  verified by live experiment. Copilot can read image clipboard data from X11
  under `Xvfb`.

### `A7` X11 Auth Model

- Method:
  official X.Org documentation review
- Evidence:
  `Xserver(1)` documents `-auth` authorization files and states that when
  authorization records are present, only clients presenting matching
  authorization data are allowed.
  `Xsecurity(7)` and the X.Org security docs describe host-based access
  control as weak and the `SECURITY` extension as limited.
- Sources:
  `https://www.x.org/archive/X11R6.9.0/doc/html/Xserver.1.html`
  `https://x.org/releases/X11R7.5/doc/man/man7/Xsecurity.7.html`
  `https://www.x.org/wiki/Development/Documentation/Security/`
  `https://www.x.org/releases/X11R7.6/doc/xextproto/security.html`
- Result:
  verified by primary-source docs. The plan should use loopback-only X11 plus
  `MIT-MAGIC-COOKIE-1` / `XAUTHORITY`, and should not depend on `xhost` or the
  `SECURITY` extension for primary protection.

### `A8` TUI Feasibility

- Method:
  source inspection of `internal/tui/app.go` and
  `internal/tui/components/timer.go`
- Evidence:
  the app-level global key handler currently reserves only `q`, `ctrl+c`,
  `tab`, and `shift+tab`; `headerBar()` is a single-row composition that can
  accept another status segment; `TimerBar` is a reusable shrinking countdown
  component. Source inspection also shows that new global `c` / `x` bindings
  must be suppressed while a text input or form field is active
- Result:
  verified with an implementation caveat. The planned header TTL UI and
  clipboard shortcuts fit the current TUI structure, but `c` / `x` cannot be
  unconditional globals because they would otherwise interfere with text
  entry.

### `A9` Runtime Dependency Baseline

- Method:
  `docker run` inspection across all four current Cooper CLI images
- Evidence:
  all four current images contain `xclip` and `Xvfb`; none of the four contain
  `xauth` or `xsel`
- Result:
  verified. The current images already have useful clipboard plumbing, but the
  final design still requires explicit addition of `xauth` and `xsel`.

### `A10` `cc-clip` as Reference

- Method:
  `go test ./internal/daemon ./internal/shim ./internal/x11bridge
  ./internal/xvfb ./internal/tunnel ./cmd/cc-clip`
- Evidence:
  all referenced `cc-clip` packages passed
- Result:
  verified. `cc-clip` remains a sound working reference for the shim and X11
  bridge mechanics we want to adapt.

### `A11` Custom Cooper AI CLI Barrel Path

- Method:
  source inspection of `internal/docker/build.go` and the custom-tool path in
  `internal/app/cooper_test.go`, plus `go test -tags=integration
  ./internal/app -run TestCooperApp_CustomToolImage -count=1`
- Evidence:
  `GetImageCLI(toolName)` already maps any tool name to `cooper-cli-<tool>`,
  custom tool Dockerfiles are intentionally user-managed outside the built-in
  template set, and the custom image integration test passes
- Result:
  verified. Cooper already supports custom `cooper-cli-*` images as real barrel
  targets, so clipboard support should be exposed through a documented runtime
  contract rather than being limited to built-in tools.

## Risks

### 1. Copilot user-facing paste path may still be gated upstream

We have strong technical evidence for native image clipboard support, but we
still need a real UI/path verification inside the Copilot CLI itself.

### 2. Host clipboard backend differences across Linux desktops

Wayland vs X11 may require different tools and failure handling.

### 3. Repeated reads within TTL

Because the default policy keeps the staged image alive for 5 minutes, the
running barrels can read it multiple times during that window. This is an
intentional tradeoff for simplicity and usability, but it is weaker isolation
than target-scoped delivery.

### 4. Mixing clipboard service with current bridge routes

If we reuse the existing bridge service, we must be careful not to blur
security boundaries with the route-execution subsystem.

### 5. Global keybinding conflict

Global delete should use `x`, not `d`, because `d` already means "deny" in the
monitor tab.

### 6. Shared scope across barrels

The user-selected model is intentionally simple: once staged, the clipboard is
available to all eligible AI barrels, including custom Cooper AI barrels by
default unless they opt out. That is still safer than live clipboard bridging,
but it is weaker than per-barrel scoping.

### 7. X11 server exposure for `codex` and `copilot`

`Xvfb` on TCP must not become an unauthenticated clipboard server visible to
other containers on the Docker network.

We should require X11 auth, likely via an `XAUTHORITY` cookie, even if the
primary client is local to the container.

Research-backed implementation assumptions:

- Cooper should request loopback TCP access explicitly rather than relying on
  X server defaults
- host-based access control is not sufficient
- MIT-MAGIC-COOKIE-1 is the practical auth mechanism
- the cookie is plaintext over the X11 transport, so the display must stay on
  container loopback only
- the SECURITY extension exists but has limitations and should not be the main
  defense

### 8. Image format normalization

Different Linux clipboard sources may expose different image MIME types.

Implementation direction:

- support image payloads regardless of original clipboard image format
- normalize to a canonical format for downstream delivery
- probe clipboard type first and convert only after explicit capture
- if the clipboard only contains file references and not image bytes, attempt to
  resolve the file as an image; otherwise fail clearly

### 9. Token provisioning and lifecycle

Even with shared clipboard scope across barrels, auth still matters.

Chosen direction:

- per-barrel token
- mounted as a read-only file
- rotated on container restart
- never exposed via env vars or logs
- validated by the host clipboard service on every request

### 10. Replace/delete races during active reads

If the user presses `c` again or presses `x` while a shim or X11 bridge is
reading, the manager must behave atomically:

- never serve mixed bytes
- never partially replace an image mid-response
- either finish the in-flight read from the old snapshot or fail cleanly

Implementation direction:

- every read operates on an immutable staged snapshot
- replace installs a new snapshot atomically
- delete revokes future reads atomically
- no handler may stream directly from mutable shared state

### 11. Host clipboard backend prerequisites

Linux support is split across Wayland and X11, and the first implementation is
likely shelling out to host tools.

We need clear behavior for:

- Wayland host without `wl-paste`
- X11 host without `xclip`
- uncommon image format without a working conversion backend
- clipboard contains no image
- clipboard image exceeds size cap

This is mostly operational, but it affects how reliable the feature feels.

### 12. Conversion backend breadth and packaging

The "all image formats" requirement is practical only if the fallback
conversion backend is installed and has the right delegates/codecs.

This affects:

- startup prerequisite checks
- distro install instructions
- test coverage for uncommon formats
- how strongly we can claim universal format support on a given host

### 13. Validation gap: real end-to-end tool behavior

We have strong evidence for the implementation strategies, but we still need
real interactive end-to-end confirmation for all four supported CLIs in Cooper,
especially `copilot`.

## Current Recommendation

Build this feature around a secure staged clipboard service owned by
`cooper up`, exposed on the existing bridge port through reserved built-in
clipboard paths.

Implementation strategy:

1. explicit capture in `cooper up`
2. staged in-memory image grant
3. bearer-auth protected clipboard endpoints
4. external-tool host capture in v1:
   - `wl-paste` on Wayland
   - `xclip` on X11
5. host `magick` fallback conversion for uncommon image formats in v1
6. helper shims for `claude` and `opencode`
7. `Xvfb` + X11 selection-owner bridge for `codex` and `copilot`
8. documented `shim` / `x11` runtime contract for custom Cooper AI barrels
9. header-visible TTL and manual delete from the TUI

This gives the right security model and matches the actual clipboard behavior
of the four supported AI CLIs, while exposing the same clipboard contract to
custom Cooper AI barrels by default unless they opt out.

## Decision Log

### 2026-04-03

- Scope expanded to all supported AI CLIs: `claude`, `opencode`, `codex`,
  `copilot`.
- Custom `cooper-cli-*` AI barrels should get the same clipboard plumbing by
  default through a documented Cooper runtime contract, with explicit opt-out.
- Feature naming should be `clipboard-bridge`, with image paste as the v1 use
  case rather than the whole long-term abstraction.
- Host scope limited to Linux for v1.
- Security direction changed from "bridge the live clipboard" to "stage one
  explicitly granted image in `cooper up`".
- We should not depend on literal image bytes being pasted into terminal stdin.
  The correct primitive is an explicit clipboard capture action in the TUI.
- Current classification:
  - `claude`: shim
  - `opencode`: shim
  - `codex`: X11 bridge
  - `copilot`: X11 bridge
- Clipboard service should reuse the existing bridge port.
- Host clipboard capture in v1 should use external tools:
  - `wl-paste` on Wayland
  - `xclip` on X11
- Uncommon image-format conversion fallback in v1 should use host `magick`.
- `cooper-x11-bridge` should be implemented as a real Go binary, not shell
  orchestration around X11 helpers.
- Preferred TUI UX:
  - `c` captures from any panel
  - header shows clipboard state on the right
  - staged clipboard shows a shrinking TTL bar
- Staged clipboard should be available to all eligible AI barrels, not scoped
  to a specific barrel.
- Eligible access includes barrels started after capture, as long as they are
  supported or custom Cooper AI barrels with valid tokens, unless explicitly
  opted out.
- Default staged-image TTL should be 5 minutes and configurable.
- Successful image fetch should not clear the staged clipboard automatically.
- Capturing a new image should replace the previous staged clipboard
  immediately.
- User should be able to clear staged clipboard early from `cooper up` via
  `x`.
- Image conversion is a first-class requirement:
  - canonical staged format is PNG
  - uncommon `image/*` formats need fallback conversion
- Internal clipboard-bridge state should be generic enough to hold typed
  clipboard objects, even though v1 externally supports only image payloads.
- Remaining technical/security issues to handle:
  - X11 auth for native clipboard tools
  - exact fallback conversion backend packaging/delegate coverage
  - atomic behavior during replace/delete races
  - host clipboard backend prerequisites and failure UX

## Evidence Log

### Tests run

- `go test ./internal/daemon ./internal/shim ./internal/x11bridge ./internal/xvfb ./internal/tunnel ./cmd/cc-clip`
- `go test ./internal/bridge ./internal/templates ./internal/app`

Both passed.

### Runtime inspection notes

- OpenCode binary contains explicit references to `wl-paste`, `xclip`, `xsel`,
  and image clipboard handling.
- Copilot package includes a native clipboard dependency with image APIs.
- Copilot native clipboard experiment under `Xvfb` succeeded on a real PNG.
