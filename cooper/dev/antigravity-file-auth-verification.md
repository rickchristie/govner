# Antigravity file-auth verification

Status: the Linux amd64 file-auth goal passed with native agy 1.2.2. Host
acceptance finished on 2026-09-15 in Asia/Jakarta. The test limits below remain
explicit; this report does not qualify another platform or login method.

Work started on 2026-09-14 inside a depth-one Cooper VM. This report is for
the host-wrapper goal, not a new release. No host keyring bridge, credential
export, or token synchronization service is included.

## Implemented behavior

- A Linux host build with Antigravity enabled installs a separate wrapper
  under `~/.local/share/cooper/antigravity` and marked Bash/Zsh shell setup.
  The native executable and existing shell content remain in place. Setup
  does not read, copy, or delete credentials and is skipped inside Cooper.
- Host and image wrappers select the native file fallback for agy and its
  children. The parent shell retains its D-Bus environment. The image's
  original binary is outside state mounts at `/opt/cooper/libexec/agy`.
- Desktop profiles check the selected host wrapper on each identity read.
  Changed files or PATH cannot retain an earlier file-auth observation.
  Existing external-provider and account-identity restrictions still apply.
- Ordinary OAuth launch checks stop before session files are created when
  portable authentication is unavailable. The error gives host setup or
  login instructions. Named sessions retain their saved account selection.
- Complete writable state roots and the profile format are unchanged.
  Ordinary refresh writes the live host root. Named refresh writes the saved
  profile, and host load restores it with existing conflict/recovery rules.

## Completed checks

| Check | Evidence and limit |
| --- | --- |
| Initial local VM suite | Passed. `/tmp/cooper-agy-file-unit-baseline.txt`. |
| Targeted Go packages | Passed for Antigravity, profile auth/manager, build flow, templates, launch, and workload paths. `/tmp/cooper-agy-file-targeted.txt`. |
| Final local VM suite | Passed with Docker, QEMU, and public network requests blocked. `/tmp/cooper-agy-file-unit-final.txt`, report `/tmp/cooper-vm-dev-unit-cbf5a8165346.json`. |
| Full Go suite | Passed with `GOFLAGS='-p=1 -timeout=90m'`. `/tmp/cooper-go-test.txt`. |
| Shell E2E gate | Passed all 390 checks after supplying the missing host test dependency. `/tmp/cooper-e2e.txt`. |
| Docker-build `all` gate | Passed all 259 checks across Mirror, Latest, and Pin modes. Real agent executables supplied mirror inputs. `/tmp/cooper-docker-build.txt`. |
| Race checks | Passed for Antigravity, profile auth/manager, launch, and workload paths. `/tmp/cooper-agy-file-race.txt`. |
| Prepared Antigravity image | Passed with a checked existing guest base and no preparation VM. One image export, 1,053,975,040 bytes. `/tmp/cooper-vm-dev-prepare-agent-e061ce8324b0.json`. |
| Docker/VM parity | Passed for version, account, paths, complete selected state, isolation, and shared writes. One VM and one guest import; no host build or export. `/tmp/cooper-vm-dev-parity-fe7c3f9ca320.json`. |
| Docker/VM profiles | Passed complete-root and credential isolation, atomic token replacement, Docker-to-VM state, restart, cleanup, and loading the updated profile onto the host. Two VM starts and imports; no host build or export. `/tmp/cooper-vm-dev-profiles-8f7771beaad3.json`. |
| Native host wrapper | Production installer with agy 1.2.2, private GNOME Keyring, and different fabricated file/keyring accounts. Two fresh Bash and two fresh Zsh launches selected the file account. A direct native control selected the keyring account. Initial PATH excluded the wrapper. Repeated setup passed; both records and the parent bus stayed usable. No network or real host credentials. |
| Development binary | Passed. `go build -C ./cooper -o ./cooper .`, log `/tmp/cooper-build.txt`. |
| Shell documentation | Bash syntax checks passed for the guide and host acceptance command blocks. |

The native probe log is `/tmp/cooper-agy-native-host-wrapper.txt`. Its source,
helper, logs, and JSON report are in
`cooper/.test-tmp/antigravity-host-wrapper/`. Native online eligibility failed
because the test network was disabled. The check proves credential selection,
not a real provider login or OAuth refresh.

The first full Go run hit the default ten-minute package deadline during
Docker fixture creation. Source edits during that run also invalidated its
workload import graph. That run is not a pass; its log is retained at
`/tmp/cooper-agy-go-first.txt`. The repeat uses the finished source and a longer
test deadline, with package serialization for the shared Docker test lock.

The first shell E2E run stopped at its host-relay check because this minimal
Codex workload did not contain `ss`. The command returned status 127 in a
direct check. A real Debian `iproute2` executable and its libraries were then
staged in a private test-tools directory. That run also recorded two Grok
requests with HTTP status `000`; the cause is not established from that log.
The repeat passed both Grok requests: proxy diagnostics show upstream
`TCP_MISS/401` for `/v1/models` and local `TCP_DENIED/403` for `/v1/storage/`.
These records are retained under
`cooper/.test-tmp/antigravity-e2e-diagnostics/`. They do not establish the cause
of the first run's transport failures. The first log is retained at
`/tmp/cooper-agy-e2e-first.txt`.

The prepared VM checks used source digest
`987d9ae877abb59fa39f94a38023fd7c56065f768b114011f199e14df8640017`
and binary digest
`0d66eda8f21ae891bb6fa9f046a1fc4e1f29ae403904702b6b0c1f7d6f7b80ed`.
The profile test replaces fabricated token files; it does not perform a real
OAuth refresh request. Runtime cleanup removed the test VMs and kept the
current Codex conversation running.

## Physical-host checks

On 2026-09-14, the user completed setup on the physical Linux host with the
normal home and Cooper configuration. The user confirmed that profile save
succeeded after file login, and that a fresh interactive Bash launch selected
the wrapper and restored the same account. The file token is present. The
desktop session bus remains available. These checks used the active host
account, not the separate home described in the acceptance procedure.
The user does not recall whether the browser opened automatically or the
login link was opened manually. Automatic browser opening is not verified.

The production Docker and VM launch paths then passed a real-account
conversation test. One conversation moved from the wrapped host process to
Docker, to a four-CPU/4096-MiB VM, and back to the host. Each process reported
the same account and conversation. The VM recovered the original marker from
conversation history and wrote it through a native shell tool to the shared
workspace. The host read the exact file bytes and restored the VM's answer.
The JSON checks required native `SUCCESS`, a nonempty conversation ID, output
token usage, and the exact expected answer. Process exit status alone was not
used as proof. No extra proxy approval was given.

The host token expiry recorded at 11:16 UTC was 12:10:39 UTC. The host request
started at 15:55:20 UTC and passed. The file expiry then became 16:55:20 UTC.
The native log records file fallback on both token read and token save. The
test did not change the token or clock. Docker, VM, and the final fresh host
process then used the same file account successfully.

Private helper scripts, raw native logs, and result JSON files are retained
under `cooper/.test-tmp/antigravity-physical-host-20260914/`. They use the
normal host home, a separate test workspace, and the current development
binary. Only test runtimes are stopped. Profile save also refused while a
test runtime was still stopping, then passed after both runtimes had exited;
the copied complete state matched the host byte for byte.

A named `Default` Docker session created a new real conversation. The named
VM restored it. Both mounted the saved complete state read-write, excluded
the live host root and host keyring paths, and left the live host state digest
unchanged. This is a real single-account profile check. Separate account
selection is covered by the automated two-account fixtures; this run did not
log in to a second real Google account.

Native Docker and VM terminals passed text and image paste with that saved
account. The helper read test text from the physical X11 clipboard and sent
it through terminal bracketed paste. It then copied a test PNG to the desktop
clipboard. Pressing `c` in the production Cooper control panel staged the
image; native `Ctrl+V` attached it. The real model returned the text marker
and four digits present only in the image. Both native processes exited zero,
and the helper restored the previous desktop clipboard content. This uses a
driven terminal; it is not a manual check of each terminal emulator's paste
shortcut. No synthetic clipboard HTTP server was used.

The optional `antigravity-unleash.goog` request received HTTP 403 in native
runtime logs. Model requests, state restore, and image paste still passed.
No domain was added to pass these checks.

The named VM restarted and restored the same profile conversation. Direct
checks inside both production runtimes could read the selected Antigravity
auth file. The physical desktop bus socket and keyring directory were absent,
as were the Codex, Claude, and Grok private state roots. These checks did not
query the host keyring.

Natural OAuth refresh passed in the named VM. Its saved token expired at
16:55:20 UTC. A native request started at 16:55:21 UTC, restored the same
conversation, returned the exact expected answer with `SUCCESS`, and wrote
a new file expiry of 17:55:40 UTC. The live host state digest stayed unchanged.
The helper waited for real expiry; it did not edit token contents or change
the clock. See `natural-profile-refresh-result.json` in the private results.

After both runtimes stopped, production `cooper load antigravity Default`
restored the saved state exactly. The host token retained the VM's refreshed
expiry. A fresh wrapped host process opened the profile conversation and
returned its expected marker with `SUCCESS`. Login and account selection did
not change inside either runtime.

The wrapped native host process also passed the same X11 text/image check
with the loaded account. It attached the image, returned the exact text and
image answer, and exited zero. The helper restored the previous clipboard.
Thus, the process-local D-Bus setting did not prevent native X11 paste in
this host environment.

The final host state was saved to `Default` and matched its complete saved
copy. Normal control-panel shutdown exited zero and stopped the proxy. No
Docker runtime remained running. Host state, saved profile state, and the
wrapper retained their pre-shutdown digests. Temporary clipboard backups
were removed after successful restoration; private test logs and results
remain. No source was staged, committed, tagged, or pushed for this goal.

## Scope and limits

- This host run used one real consumer OAuth account. Automated fixtures
  cover distinct account identities, isolation, and profile conflicts.
- Automatic browser opening was not confirmed. The user completed login
  but does not recall whether the browser opened automatically.
- Clipboard checks used physical X11 and driven native terminals. Wayland
  and individual terminal-emulator paste shortcuts were not checked here.
- Native ARM and macOS runtime checks remain separate. The host wrapper
  applies to Linux and does not disable macOS Keychain. ADC/WIF profiles
  remain outside this file-OAuth goal.

Use [the host acceptance procedure](antigravity-acceptance.md) to repeat or
extend these checks. Antigravity remains experimental. No full VM release
gate is required for this development goal; that gate is still required
before the next Cooper release.
