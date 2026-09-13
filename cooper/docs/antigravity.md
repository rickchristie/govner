# Antigravity CLI

Cooper uses Google's native terminal executable, `agy`. The Cooper tool name
is `antigravity`. This integration does not install the Antigravity desktop
application, an extension, or a Python package with the same name.

Enable Antigravity in `cooper configure`, then build. These commands open the
usual Cooper shell:

```sh
cooper cli antigravity
cooper vm antigravity
```

Run `agy` in that shell. Its `--continue` and `--conversation=<id>` arguments
work at the same workspace path as the host. Use the VM when the work needs
Docker or a separate kernel. Use a CLI barrel for faster ordinary work.

## Version and state contract

The reviewed native release is **1.2.2**, on Linux amd64 and arm64. Configure
can detect the host with `agy --version`. Existing configs gain a disabled
Antigravity row. First-time configuration can mirror an installed host CLI.

Google's archive paths include an opaque build ID. Cooper resolves exact
platform URLs and SHA-512 digests before a build and saves both platform
records in the config. Rendering uses those records without network access.
Mirror and Pin never substitute Latest for an unavailable historical release.
Cooper retains reviewed 1.2.2 records for offline resolution. A later native
release needs review of its helper dependency before Cooper can build it.

The image installs `agy` in `/opt/cooper/bin`, outside state mounts. Native
1.2.2 uses a Playwright 1.57.0 driver whose old download endpoints return 404.
Cooper installs that exact official driver through npm and selects the image's
Linux Node runtime. It does not use a host executable cache that can contain
macOS binaries. Browser installation remains the existing Cooper browser
feature. Automatic native CLI updates are disabled to keep the built version.

The complete `~/.gemini` root is mounted read-write at its host path in both
runtimes. This includes CLI state and sibling Google state directories,
SQLite journals, conversations, auth, settings, plugins, memory, and future
files. Cooper does not copy or select children during an ordinary launch.
It does not add a `.agents` mount without evidence that this CLI reads it.

With `AGY_ADC_AUTH=true` (also `1`), live launches additionally mount the
complete parent of `GOOGLE_APPLICATION_CREDENTIALS`, or `~/.config/gcloud`
when that variable is absent. Relative credential paths become absolute at
launch. The native Go ADC resolver does not use `CLOUDSDK_CONFIG` to select
its default file. Cooper rejects a credential parent that exposes the whole
home or overlaps Cooper-owned storage. Cleanup protects these credential
roots even when ADC mode is later disabled.

## Native sandbox limit

Normal native shell tools passed with all Docker capabilities dropped and
`no-new-privileges`. Antigravity 1.2.2's optional `--sandbox` mode could not
start its tool process under the reviewed Docker policy. A syscall trace
showed `clone` with new user, mount, PID, UTS, and network namespaces returning
`EPERM`. A separate namespace probe confirmed a seccomp restriction; removing
that filter for the diagnostic still met a filesystem-propagation denial.

Cooper preserves the host's sandbox setting. It does not grant extra
capabilities or disable isolation to make the native sandbox work. A host
configuration that requires this optional native mode has a compatibility
limit. The ordinary Cooper Docker/VM boundary remains in force.

## Accounts and profiles

For an ordinary launch, sign in with the host CLI first. A login that is
stored only in an OS keyring cannot be restored by a directory mount. Cooper
does not forward the host session bus or copy keyring databases. Such a login
can require a separate native login in the workload.

Named profiles use the existing profile service:

```sh
cooper save antigravity
cooper load antigravity Work
# Sign in to the new account on the host, then exit agy.
cooper save antigravity
cooper cli antigravity Work
cooper vm antigravity Work
```

The first profile is `Default`. Loading another profile saves outgoing changes
first. Profile selection changes mount sources only. It keeps targets, the
workspace, and the built OS account unchanged.

The local identity adapter supports the reviewed Linux file OAuth schema for
consumer and GCP accounts. It uses Google's subject and audience plus auth
method, project, and region. Email is only a display label. Access-token
refresh does not select another profile. Gemini API-key profiles require
`modelProvider: gemini` in the native settings and include the key and endpoint
in their identity. A key alone does not select API billing.

Named profiles do not support ADC, WIF, external credential helpers, macOS
keychain auth, or a file account that can be superseded by a host keyring.
Unknown or conflicting identity cannot overwrite a known account. Cooper
keeps recovery state and reports the issue. See [Account profiles](profiles.md)
for environment-credential load limits and recovery procedures.

`.gemini` is shared by other Google products. Saving or loading a profile
copies or replaces that **whole root**, including their state. Exit `agy`,
Gemini CLI, and Antigravity desktop before profile changes. Cooper also checks
known host writers and running Docker/VM mounts.

## Network and verification

Enabling the tool adds exact install, Google OAuth, Gemini API, and observed
consumer/GCP Cloud Code hosts. Consumer 1.2.2 selected
`daily-cloudcode-pa.googleapis.com`; GCP selected `cloudcode-pa.googleapis.com`.
Google account eligibility also requires `lh3.googleusercontent.com`: native
1.2.2 stops if it cannot fetch the account picture from this host. A real
file-OAuth account check found this requirement.
Disabling it removes its managed defaults and preserves user rules. Custom
providers, enterprise identity services, MCP servers, telemetry, and browser
downloads can need separate allowlist entries.

`cooper proof` makes a small model request when Antigravity is enabled. It
requires a complete JSON response with the exact marker, a conversation ID,
one turn, and nonzero output usage. The native print timeout can return partial
output with exit status zero, so that status alone cannot pass the check.
An outer timeout also bounds the process. Failed reports omit raw auth and
model output.

The automated native fixture uses a fake key and a local model server. It
checks a real native request and restoration by a second native process.
This proves executable and state behavior. It does not prove Google login,
token refresh, account eligibility, or compatibility with a user's existing
conversation. Those checks require final account testing.

A real Linux consumer file-OAuth account passed login, model and shell-tool
requests, new-process conversation restore, saved-profile restore, and natural
token refresh with 1.2.2. The refreshed login retained its profile identity.
This private fixture ran inside a released Cooper VM. A separate local-model
probe verified native Ctrl+V image paste from X11 with exact image bytes.
Concurrent containers created separate conversations in one shared state root,
then restored each other's conversations. Their observed TCP listeners used
loopback addresses. Physical-host continuity and clipboard bridge acceptance
remain separate checks.

The [account acceptance procedure](../dev/antigravity-acceptance.md) uses an
isolated setup and records the real-login checks separately.
