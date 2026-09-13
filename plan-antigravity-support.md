# Antigravity support in Cooper

**Status: Real file-OAuth login, natural token refresh, conversation restore,
and private profile switching passed. All automated regression gates passed
after the required-domain fix. Final physical-host acceptance remains open.**

## Current implementation and authorization

The account-profile feature is committed as `3b34dec`. After the automated
Antigravity checks passed, the user authorized committing this implementation,
its tests, and documentation. Keep this plan for final physical-host acceptance.
The user completed native login in the private fixture after the automated
checks. Do not request credentials in conversation. Do not remove this plan
while account acceptance or required runtime checks remain open.

Ordinary launches mount live host state. Explicit profiles copy the complete
shared root catalog and change mount sources only. This record supersedes
older statements below that prohibit profile copies or assume five harnesses.
The original research remains below to retain its sources and open questions.

### Implemented boundaries

- `internal/aitool`: Cooper identity `antigravity`, native command `agy`, X11
  clipboard, native approval flag, and whole `.gemini` initial directory.
  Profiles discovers the catalog without a second fixed harness list.
- `internal/antigravity`: bounded official manifest client, strict archive
  metadata checks, retained verified 1.2.2 records, explicit missing-version
  errors, and a native complete-result proof validator. The public CLI repo
  supplies docs and examples; it is not native implementation source.
- `internal/config`: strict desired-version refresh freezes both platforms
  together; config snapshots clone the records. Configure preserves resolved
  inputs outside the TUI. Exact managed hosts retain user rules and other
  enabled features' hosts.
- `internal/templates`: pure rendering, checked SHA-512, one regular tar member,
  `/opt/cooper/bin/agy`, exact native version, and an image-owned Playwright
  1.57.0 driver. No moving native install script. Custom `cli/antigravity`
  directories are refused and preserved. The interactive alias uses `agy`.
- `internal/workload`: complete `.gemini`, and a conditional complete ADC
  credential parent, at identical host/guest paths. Cleanup protects default
  and explicit ADC roots even after ADC is disabled. No unobserved `.agents`
  or native-installer cache root was added.
- `internal/profileauth`: bounded Linux file OAuth identity from Google
  subject/audience plus auth method/project/region. Email is a label. Refresh
  preserves identity. API-key identity requires the Gemini provider setting
  and includes the endpoint. ADC/WIF/external helpers/keyring-only auth remain
  unsupported named identities and cannot overwrite saved accounts.
- `internal/profilemanager`: native `agy`, Gemini CLI, and Antigravity desktop
  count as known shared-root writers. Default Linux session-bus presence is
  observed even without an explicit env value; no bus is forwarded or saved.
- Existing profiles, launch, Docker, and VM boundaries provide copying,
  recovery, locking, credential isolation, restart metadata, and cleanup.
  No second profile implementation was added for Antigravity.
- Test pins, full image/E2E matrices, finite VM commands and hook cases include
  the new agent. `profiles antigravity` uses the existing two-start contract.
  Native parity runs a local fake model and restores a real native conversation.
- User guides, requirements, and `cooper/dev/adding-an-agent.md` describe the
  supported behavior, shared Google state, dependency choices, and auth limits.

### Selected-release evidence (1.2.2)

| Check | Observation and evidence |
| --- | --- |
| Current manifests | Both official Linux manifests returned 1.2.2 with the retained URLs/digests. `/tmp/cooper-agy-manifest.json`, `/tmp/cooper-agy-arm.json`. |
| amd64 archive | SHA-512 matched; exactly one regular `antigravity` executable; native version 1.2.2. `/tmp/cooper-antigravity-install.cXj4uh/`. |
| arm64 archive | Downloaded 54,016,481 bytes; exact official SHA-512; one regular executable with ELF machine AArch64. `/tmp/cooper-agy-arm64.tar.gz`. Native ARM execution remains untested. |
| Native startup | Empty private home, no inherited credentials, no network. Native login choices rendered in the generated Antigravity image; double Ctrl+C exited zero. `/tmp/cooper-antigravity-generated-pty.txt`. VM terminal acceptance remains open. |
| OAuth fallback | File-only trace and native synthetic consumer/GCP records established `.gemini/antigravity-cli/antigravity-oauth-token`, nested OAuth token fields, `auth_method`, `id_token`, project/region, and WIF selectors. No real token was read or sent. `/tmp/cooper-agy-research/`. |
| Consumer/GCP hosts | Native consumer fixture selected `daily-cloudcode-pa.googleapis.com`; GCP selected `cloudcode-pa.googleapis.com` and read the configured project/region. Network was disabled. |
| ADC | Native file trace read `~/.config/gcloud/application_default_credentials.json` with both `AGY_ADC_AUTH=true` and `1`. The Google Go ADC resolver uses the explicit credentials path or that default, not `CLOUDSDK_CONFIG`. `adc-trace.txt`, `adc-one-trace.txt`. |
| Playwright driver | Native 1.2.2 embeds Playwright Go 1.57.0. Old driver CDN paths returned origin 404. Exact official npm `playwright@1.57.0`, selected through driver/Node env paths, worked without startup downloads. |
| Local native model | Actual `agy` selected `modelProvider: gemini`, used a fake key and loopback endpoint, and returned native JSON with `SUCCESS`, conversation ID, response, turn count, and usage. A new native process restored the conversation and sent its prior model-role answer. The generated image also passed. `/tmp/cooper-antigravity-built-probe.txt`. |
| Partial output | A native two-second print timeout exited zero and emitted `status: SUCCESS` with partial output plus a timeout diagnostic. Cooper rejects this result. `/tmp/cooper-agy-research/partial-output.txt`. |
| Proxy batch | All 31 credential-free exact-host HEAD curls reached their origins without Squid denial, including the previously denied daily Cloud Code host. `/tmp/cooper-agy-batch-*.headers`. Origin root responses of 400/403/404 are not proxy denials. No redirects were followed. |

Batch research access does not add every host to product defaults. Pause and
ask for an allowlist change if a newly required host is blocked. Never bypass
the enclosing Cooper proxy. An anonymous bucket-listing 403 was an origin IAM
response; it does not provide historical release lookup.

### Verification record and remaining steps

Passed local packages: release resolver, catalog, config, templates, workload,
profile identity/service, process guards, auth, environment wrapper, configure,
Profiles UI, proof, and VM fixture compilation. Logs:
`/tmp/cooper-antigravity-integration-tests.txt`,
`/tmp/cooper-antigravity-profile-flow.txt`,
`/tmp/cooper-antigravity-final-local.txt`.

The VM unit command passed with Docker and QEMU blocked. Report:
`/tmp/cooper-vm-dev-unit-58c97ba0c091.json`. Finite-command and permission-hook
tests passed. The native fixture now uses the base image's existing Node
runtime, so it does not require Python in a minimal selected-agent image.

Additional completed checks:

- The full module compiled for Darwin arm64 with `CGO_ENABLED=0` and
  `-exec /bin/true`. `/tmp/cooper-antigravity-darwin-compile.txt`. This is a
  compile check, not macOS runtime or Keychain acceptance.
- Deterministic Xvfb captures were inspected: Profiles at 100x30 and its new
  Work profile form at 80x24, both with Antigravity selected.
  `/tmp/cooper-antigravity-profiles.png`,
  `/tmp/cooper-antigravity-profile-form.png`.
- The first full Go attempt exceeded the default ten-minute package timeout
  after cleanup tests rebuilt their images. Editing imports during that run
  also caused an import-graph failure. The second run uses unchanged source,
  serial package execution, and a twenty-minute package timeout. The first
  log is retained at `/tmp/cooper-antigravity-go-first-failure.txt`.

The full Go suite passed with serial package execution and a twenty-minute
package timeout. A cached full run after the native continuation fixture
change also passed. `/tmp/cooper-go-test.txt`. The full shell E2E suite passed
390 checks with zero failures. `/tmp/cooper-e2e.txt`. The current VM unit
report is `/tmp/cooper-vm-dev-unit-c1fc2809d258.json`.

The native fixture now also checks a real terminal and clean double-Ctrl+C
exit. Shared fixture mode requires a private state sentinel and preserves a
conversation record, so the VM must restore the Docker conversation. Both
phases passed in separate Docker containers before the real VM check.

The unauthenticated Google selection displayed its browser URL and manual
code prompt in an isolated network-disabled container, then exited zero.
No login or token exchange occurred. Private capture:
`/tmp/cooper-antigravity-login-flow.txt`.
[Final account procedure](cooper/dev/antigravity-acceptance.md).

All Docker build modes passed: 86 checks in each of mirror, latest, and
pinned (258 total), with zero failures. `/tmp/cooper-docker-build.txt`.
Resolved configs are retained in `/tmp/cooper-antigravity-build-inputs/`.
The cleanup-only command completed after the gate.

Antigravity preparation passed in 31.39 seconds, reused the prepared guest,
and exported one 1,053,963,264-byte image archive. Report:
`/tmp/cooper-vm-dev-prepare-agent-c5bb8a38d966.json`.
Parity passed in 80.58 seconds with one VM start/import, zero exports, and a
hard-linked archive. Native Docker-to-VM conversation restore, same-path
state writes, terminal exit, and cleanup passed. Report:
`/tmp/cooper-vm-dev-parity-78fccd73f283.json`.

A separate fake-model tool probe made the real native `run_command` write a
marker in its private workspace under `--cap-drop ALL` and no-new-privileges.
`/tmp/cooper-antigravity-native-tool.txt`. With native `--sandbox`, the tool
failed and the required marker was absent. The native response and syscall
trace identify a denied namespace clone, not a missing executable. Removing
seccomp only in a separate credential-free namespace diagnostic advanced to
a filesystem-propagation denial. No product policy was weakened. Evidence:
`/tmp/cooper-antigravity-native-sandbox.txt`,
`/tmp/cooper-agy-tool-probe/sandbox-0.strace`,
`/tmp/cooper-antigravity-namespace-probe.txt`.
The optional native sandbox is an explicit supported-configuration limit.

The profile VM case passed in 140.17 seconds with two starts/imports, zero
exports, and hard-linked archive staging. It checked selected profile roots,
host-state isolation, credentials, Docker-to-VM writes, restart, labels, and
cleanup. Report: `/tmp/cooper-vm-dev-profiles-af142f7a6f15.json`.

Before the real-account domain fix, all required automated gates completed.
The development binary build and `git diff --check` passed. Test runtimes were
removed. No Antigravity changes were staged or committed.

The user has logged in on the physical host. The active Codex VM still uses
an older released Cooper with `/home/user`; its selected Codex mounts do not
expose the host `.gemini`. The host account-path fix is already in source, but
this running VM does not have it. Do not ask the user to move this session to
the host to continue it.

A private native login helper is ready at
`cooper/.test-tmp/antigravity-account/login.sh` (gitignored). It uses the cached
verified 1.2.2 image, private persistent home/workspace, the VM's internal
control network, and the enclosing proxy/CA. Its login screen and exit were
checked. A credential-free request reached Google's OAuth origin. The user
can open another `cooper vm codex` shell and enter the auth code in the native
prompt. This will permit real-account tests here; it does not establish reuse
of the physical-host login. Do not print token files or native auth logs.

### Real-account results in the existing released VM

The user completed the native Google manual-code flow in the private fixture.
Native 1.2.2 saved a consumer OAuth file with a refresh token and an ID token.
No credential values or account labels are recorded here.

- A bounded real model request returned the exact proof marker, one turn,
  nonzero output usage, and exit zero. The production proof validator passed.
- A new native process in a new container restored the conversation and
  repeated the previous answer. Its conversation ID stayed the same.
- The production identity reader recognized the real file login.
- Natural token refresh passed after the saved access token expired. A new
  native process advanced the token expiry and completed the exact one-turn
  model proof. The refreshed login still matched the saved account identity.
  The first attempt hit an OAuth TLS handshake timeout and showed a login
  prompt; it retained the token file. A credential-free endpoint check reached
  Google, and one bounded retry succeeded without another user login or any
  edit to token contents. Private results: `refresh-result.json` and
  `first-refresh-result.json`; identity/proof checks:
  `/tmp/cooper-antigravity-account-check.txt`.
- On a separate private copy, the production profile service saved Default,
  created empty pending Work state, and loaded Default again. Named selection
  validated the saved account. A native container mounted that saved source
  at the original target path and continued the same conversation. Hashes of
  both the original login root and the restored host-copy root stayed equal.
  This fixture supplies a guard for its private paths and serial native
  writers; it does not bypass the production host command's VM refusal.
- A real native shell-tool request wrote the exact requested marker file and
  completed successfully under the existing container restrictions.
- A temporary CONNECT filter restricted native traffic to the managed exact
  hosts and forwarded allowed traffic through the enclosing Cooper proxy.
  The first attempt found a missing required host: native eligibility failed
  when the Google account picture at `lh3.googleusercontent.com` was denied.
  This exact host is now a managed default with a regression test.
  The repeated tool request passed with that host included. Requests to
  `antigravity-unleash.goog` and `play.googleapis.com` remained denied. Neither
  was added to defaults. This proves the tested model/tool flow under the
  restricted set; the initial browser login was completed before this filter.

Private evidence and fixture scripts are under the ignored
`cooper/.test-tmp/antigravity-account/` directory. Keep raw native output and
logs private. The profile check summary is at
`/tmp/cooper-antigravity-account-profile.txt`. The current VM unit check passed
at `/tmp/cooper-vm-dev-unit-625088546598.json`. Full regression reruns after
the required-domain change have the download blocker below.

The repeated full Go run found three archive-download setup failures in its
first package: curl exited 52 with an empty response. A later attempt retrieved
the same archive, verified its SHA-512, and built the image. Subsequent HEAD
and 1 KiB range requests returned 200/206 through the enclosing proxy, including
from Docker's bridge and internal control networks. There was no observed
allowlist-denial response. The available logs do not identify which component
closed the failed connections. Retain this failed-run evidence and repeat the
failed checks with the verified image cache before reporting a passed gate.

The second full Go run also failed after a cleanup test removed the native
image and forced another download. Three later test setups received curl 52;
the other packages passed or reused their successful results. A separate full
GET established TLS with the Cooper CA, then timed out after 30 seconds with
no HTTP response or downloaded bytes. This differs from the successful HEAD
and small range requests. Further image gates are paused while the user checks
the host Cooper monitor. Do not bypass the enclosing proxy or claim a confirmed
allowlist cause without its logs.

Current evidence: `/tmp/cooper-antigravity-go-download-failures.txt`,
`/tmp/cooper-go-test.txt`, `/tmp/cooper-antigravity-archive-head.txt`, and
`/tmp/cooper-antigravity-archive-get.txt`. The local development binary build
and `git diff --check` passed. Six failed build containers from these logs were
removed. Existing Docker-build test images and their tags were preserved;
temporary backup tags were removed. The shell E2E and Docker-build gates have
their earlier successful results, but have not been repeated after this domain
change. No Antigravity changes were staged or committed.

After the user requested a domain batch, credential-free GET requests covered
36 core and build-dependency hosts plus the full native archive URL. The first
archive GET returned curl 52; three other hosts timed out during TLS. On the
next continuation, all four requests succeeded through the enclosing proxy.
The full archive returned HTTP 200, 57,596,853 bytes in 7.67 seconds, and its
SHA-512 matched the pinned release. No alternate route or download method was
used. These results establish current access, but do not identify the earlier
failure's cause. Evidence: `/tmp/cooper-antigravity-allowlist/recheck/`.
The full Go suite has resumed with unchanged production source. Preserve the
second failed run at `/tmp/cooper-antigravity-go-second-download-failures.txt`.

The resumed full Go suite passed. A separate network-disabled native terminal
probe also verified Ctrl+V image paste from X11. After the first-run screens,
native 1.2.2 submitted the exact 68-byte PNG to a loopback fake Gemini model,
displayed its answer, and exited zero with double Ctrl+C. The PNG SHA-256 was
`f65b0ab7e131cbfd8d934c694230560e4b11e40278f398be2b79b77b069dff9d`.
This used local Xvfb and `/usr/bin/xclip`, with no account credentials or
network access. It verifies the selected native X11 mode, but not the physical
host bridge or text-copy behavior. Earlier fixture attempts only reached
onboarding and were not counted as passes. Evidence:
`/tmp/cooper-antigravity-clipboard-probe.txt` and private fixture
`clipboard-result.json`. The full shell E2E suite is now running.

Two network-disabled native containers also passed a concurrent-state probe.
They used the same private complete `.gemini` root and workspace target, with
separate loopback fake models and no credentials. Their native process time
windows overlapped. Each created a distinct conversation, then a new process
in each container restored the other container's prior model answer and
continued at turn two. This verifies the tested local backend/state behavior;
real-account host/VM concurrency and control-port access remain separate
acceptance items. Ignored evidence: `concurrent-probe.mjs`,
`concurrent-path.txt`, and the referenced directory's results.

The repeated concurrent probe also inspected TCP listeners while the native
clients made requests. All observed listeners used loopback addresses. Both
cross-container restores passed again. Evidence:
`concurrent-listeners-path.txt` and the referenced results/listener files.
This bounds exposure to the container network namespace for the tested mode;
it does not establish authentication on each native control endpoint.

The first resumed shell gate exited after 292 passed checks, before its
loopback-server assertion. The development container had no `ss` executable;
the test's pipeline exited under `set -e` with its diagnostic redirected.
`/tmp/cooper-antigravity-e2e-missing-ss.txt` retains the result. The Debian
`ss` executable and its missing `libmnl` library were obtained in temporary
containers and placed in the ignored fixture's `test-tools` directory.
The real executable now reports this container's sockets. The unchanged
shell gate has resumed with that directory on PATH. No assertion was skipped.

The full shell rerun passed all 390 checks with zero failures and completed
cleanup. `/tmp/cooper-e2e.txt` is the successful log. The Docker build matrix
has started in all three reviewed modes, with pre-existing images and config
directories preserved for restoration. Production source remains unchanged
since the required-domain fix.

Final rerun results, completed on 2026-09-14 UTC:

- Full Go: 55 packages passed, seven packages had no tests, and no package
  failed. The root package completed in 460.560 seconds.
  `/tmp/cooper-go-test.txt`.
- Full shell E2E: 390 passed, zero failed. `/tmp/cooper-e2e.txt`.
- Docker build matrix: mirror, latest, and pinned each passed 86 checks
  (258 total), including the real native request, conversation restore,
  terminal exit, version, helper, and image-isolation checks.
  `/tmp/cooper-docker-build.txt`.
- The exact development binary build and `git diff --check` passed.
  `/tmp/cooper-build.txt`.
- All 14 pre-existing Docker image references were restored. No pre-existing
  test config directories were present; this gate's configs were retained in
  its ignored results directory. New disposable images were removed, including
  12 new untagged outputs left after restoring existing tags. Cleanup selected
  only final image IDs from this gate's log that were created during this run,
  had no tags, and had no container references. No global prune was used.
  The original Codex container is the only running container. The VM has
  approximately 12 GB free. Preservation and cleanup records are in
  `cooper/.test-tmp/antigravity-account/gate-build-dji6ehnu/`.

The previously passed finite VM parity and profile checks remain applicable;
their runtime source did not change after those checks. No full VM release
gate was run during this development verification. The private login and
profile data remain available for final acceptance. No Antigravity changes
were staged or committed, and this plan remains until physical-host
acceptance and the final support-scope decision are complete.

These real checks used the cached image inside the existing released VM.
They do not replace a test through the new production launch commands on the
physical host. The image account and private fixture HOME differ, so this is
also not evidence for the new build-account parity rule. The earlier prepared
Docker/VM tests cover that rule with synthetic account state.

Remaining acceptance:

1. The user completes the [private account procedure](cooper/dev/antigravity-acceptance.md).
   Check initial login under the default policy and real conversation
   continuation through the production Docker and VM launch commands in both
   directions. Real login, natural refresh, and native process restore already
   passed in the current private fixture.
2. Record native clipboard interaction, concurrent backend behavior, and
   actual host continuity for a supported file account or Gemini API mode.
   Desktop keyring profiles and optional native namespace sandbox mode have
   documented compatibility limits. Do not weaken policy or invent a
   portable storage switch to hide those limits.
3. Keep ARM execution and macOS runtime gaps explicit. Record acceptance of
   the final support scope before removing this plan. Real credentials and
   authorization codes must not enter this document or test logs.

### Current assumption decisions

This table updates the original register without deleting its research context.
A conditional result is not full acceptance for another auth mode or platform.

| Original IDs | Current decision |
| --- | --- |
| A01-A04 | Native `agy` 1.2.2 is integrated and runs in generated Linux amd64 images. Full shell runtime checks passed. |
| A05 | ARM64 manifest, digest, one-file archive, and ELF architecture verified. Native ARM execution is pending. |
| A06 | Retained verified records and saved frozen records resolve exact versions. Unknown historical versions fail explicitly; no guessed build IDs. |
| A07-A09 | Whole `.gemini` and native file OAuth schema verified with native requests, file traces, and real consumer login. Memory, plugin, and macOS coverage remain open. |
| A10-A11 | Host keyring portability is unsupported. No keyring broker or forced storage mode was introduced. Real file-account natural refresh and stable profile identity passed in the private fixture. Physical-host continuity remains pending. |
| A12-A13 | Durable conversation restore passed between native Docker and VM processes using the shared state root. Same-path writes and cleanup passed. Real-account process and saved-profile restore also passed in the private fixture. Production host/VM real-account, concurrent-writer, and macOS cases remain open. |
| A14-A15 | No global `.agents` or installer cache mount was justified by observed core startup. Image-owned driver avoids cross-platform executable caches. Optional feature discovery remains open. |
| A16 | Native ADC default lookup and `AGY_ADC_AUTH=1` verified. Complete explicit parent/default gcloud root mapping and cleanup guards are tested. Real credential rotation and WIF remain open; named ADC/WIF profiles are unsupported. |
| A17 | Native Gemini provider/key/endpoint behavior passed against a loopback fake model. Credential selection and unknown-account refusal are tested. |
| A18-A19 | Exact image versions and protected updater/helper settings are tested. Fresh network-disabled native startup/model/terminal checks pass with the installed driver. Real Google consumer login and inference passed in the private fixture. |
| A20-A23 | Exact-host curls and real consumer model/tool traffic passed. Restricted account testing found required `lh3.googleusercontent.com`; it is now a managed default. Initial login under the restricted defaults and operation-specific optional hosts still need account acceptance. |
| A24-A26 | Existing runtime and clipboard boundaries remain in use; full shell isolation/X11 checks passed. Native clipboard interaction and concurrent backend behavior need final acceptance. |
| A27 | Actual interactive `agy` alias and one-shot behavior passed in the E2E suite. |
| A28 | Native shell-tool execution passed under the normal outer restrictions. Optional native `--sandbox` failed at namespace clone with EPERM; a namespace diagnostic confirmed policy restrictions. Host settings are preserved. No capability or seccomp exception was added. |
| A29 | Generated Linux amd64 native terminal reaches login and exits zero with double Ctrl+C. Native VM terminal passed in the prepared fixture. Other platforms and authenticated Ctrl+D remain open. |
| A30 | Official public CLI repo contains docs/examples. It was not treated as native implementation source. |
| A31-A33 | Custom directory preservation, managed-domain ownership, finite VM commands, hooks, profile catalog, and local profile flow are implemented and tested. |
| A34 | Actual native partial timeout can exit zero. The proof requires complete bounded native JSON and rejects observed partial output. |
| A35 | Current docs state selected-release evidence and limits. Original conflicts remain recorded below; no host data migration was added. |

R01 and R04 are resolved within the stated platform/version limits. R02 passed for the reviewed Linux amd64 generated Docker and VM images.
R07 passed its prepared synthetic parity and profile cases; real-account
continuity remains in R03. R03, R05, and R06 retain the real-account,
host-keyring, native clipboard, and sandbox conditions above. R08 includes the
new agent checklist, account procedure, and completed automated gate record.
Its final account and support-scope acceptance remain open.

## Original research and acceptance register

Research date: 2026-09-13, from the session date. Download response headers and
local application logs reported 2026-09-12. Keep these different time sources
in the evidence record. The observed CLI version was **1.2.2**.

Repository baseline at the end of research:
`5e66aa72528e6405a28e636fbe08464deacef0a2`.
This commit adds the prepared VM development profiles. The repository changed
during research because another session was working on it.

**The implementor must start with another research pass.** Verify the open
items in the assumption register and the continuation checklist before using
them as implementation facts. Update this document with results and sources.
Keep an item open when the available evidence does not prove it.

## 1. Intended result

Add Google Antigravity CLI as a built-in Cooper agent. Use:

| Item | Proposed value |
| --- | --- |
| Cooper tool name and configuration key | `antigravity` |
| Display name | `Antigravity CLI` |
| Native executable | `agy` |
| Host version command | `agy --version` |
| Image | `cooper-cli-antigravity`, with the normal Cooper image prefix |
| Image executable location | `/opt/cooper/bin/agy` |
| Main host state root | The complete effective `~/.gemini` directory, read-write |
| Execution modes | Both `cooper cli antigravity` and `cooper vm antigravity` |

Keep Cooper's current shell behavior. These commands open the selected
environment; the user then runs `agy`. A command supplied with `-c` runs through
the current one-shot command path. Do not change all agents to start their
native CLI automatically as part of this feature.

The target is the native terminal product. Google also has Antigravity 2.0,
an IDE, IDE extensions, and an SDK. Desktop installation and desktop control
are separate work. The official terminal product is confirmed by the
[CLI product page](https://antigravity.google/product/antigravity-cli) and the
[CLI repository](https://github.com/google-antigravity/antigravity-cli).

Required behavior:

1. The image installs a known native version for its Linux architecture.
2. Host settings, sessions, history, memory, plugins, and auth state remain
   available at their original absolute paths.
3. A user can exit a host CLI session, continue it in Cooper, then continue it
   on the host again. Account login must not silently change provider or
   billing mode.
4. The default whitelist covers the verified service and install hosts needed
   for the supported modes. Normal operation must not need repeated manual
   approvals for required Antigravity service hosts.
5. CLI and VM use the same mount, environment, version, clipboard, and proxy
   policy. The VM keeps its existing network boundary.
6. Development checks use the small prepared profiles. The complete VM gate
   runs only before a Cooper release.
7. The implementation produces one short, reusable checklist for adding any
   coding agent harness to Cooper. Future integrations must be able to use it
   to find all required changes and checks without repeating this research.

The account-auth requirement and historical version resolution are the main
open implementation conditions. A successful `--version` check does not close
either condition.

## 2. Work performed and work limits

This session read source, official documentation, the published installer as
text, release manifests, and the public CLI repository. It did not execute
the installer script.

The user later authorized an installation and a startup check. This session:

- Downloaded one official Linux amd64 CLI archive into a unique `/tmp` directory.
- Verified its SHA-512 against Google's release manifest.
- Extracted its single executable as `bin/agy` in that directory.
- Ran `--help`, `--version`, and three bounded startup probes without login or
  a model prompt.
- Checked the application's startup and updater records.
- Confirmed that no process using the temporary executable remained afterward.

**No Cooper tests, builds, Docker operations, or VM operations ran in this
session.** The user reserved that work for another session. The native CLI
probe is not a Cooper integration test.

The native probe ran in the tool execution environment: Linux x86-64, UID
1000, with application home `/home/user`. Do not interpret that as a test of
the physical workstation, macOS, ARM64, a Cooper image, or the Cooper VM.
The native application created its normal state below that environment's
`/home/user/.gemini`. The install was temporary; it did not add `agy` to the
user's shell profile or global `PATH`.

An initial attempt to use an additional `bwrap` filesystem boundary failed
with `Failed to make / slave: Permission denied`. That is evidence about the
probe environment. It is not evidence that Antigravity's own sandbox fails
in Cooper. The subsequent native probes used an empty temporary workspace,
the normal application home, and an environment without provider credentials.

## 3. Evidence rules

Use these labels in follow-up records:

| Label | Meaning |
| --- | --- |
| Observed | A command or local record in this session directly showed it. |
| Documented | An official page or published installer says it. |
| Source | Cooper source establishes the current behavior. |
| Proposed | A design choice for the implementation. |
| Open | Needs another observation or a design decision. |

Documentation, binary strings, and successful requests to a domain have
different meanings. A string embedded in a binary does not prove that a
normal session contacts that host. A successful HTTP request does not prove
that model streaming or OAuth works through Cooper.

Temporary evidence locations, which can disappear:

- `/tmp/cooper-antigravity-install.cXj4uh/bin/agy`
- `/tmp/cooper-antigravity-install.cXj4uh/agy.tar.gz`
- `/tmp/cooper-antigravity-install.cXj4uh/help.txt`
- `/tmp/cooper-antigravity-install.cXj4uh/startup.log`
- `/tmp/cooper-antigravity-install.cXj4uh/startup-interactive.log`
- `/tmp/cooper-antigravity-install.cXj4uh/startup-foreground.log`
- `/tmp/cooper-antigravity-*.html`, `*.headers`, and extracted text files

Do not depend on these paths for the implementation. This plan preserves
the material findings. Do not commit raw account logs, OAuth URLs, tokens,
or real host state as fixtures.

## 4. Native installation and startup findings

### 4.1 Distribution

The official [Unix install script](https://antigravity.google/cli/install.sh)
selects a platform manifest, checks a SHA-512 digest, and installs a native
binary. Its default executable path is `~/.local/bin/agy`. It also invokes
the binary's `install` subcommand, which can change shell setup. Cooper should
use the artifact and digest directly during image construction.

Observed manifests:

| Target | Manifest | Version |
| --- | --- | --- |
| Linux amd64 | [linux_amd64.json](https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/linux_amd64.json) | `1.2.2` |
| Linux arm64 | [linux_arm64.json](https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/linux_arm64.json) | `1.2.2` |

These are moving manifests. Re-fetch and verify them before implementation.
The script also selects musl variants. Cooper's current base uses glibc;
do not select a musl artifact from the host OS by mistake.

Observed release records:

```text
version: 1.2.2

linux amd64 URL:
https://storage.googleapis.com/antigravity-public/antigravity-cli/1.2.2-6061403484848128/linux-x64/cli_linux_x64.tar.gz
sha512:
74342cf2a78b344392e573b638a648a6ad1f8e877f494b96e20f9c2b79158d5c423c40b2dcf788703362bb0a9150f09c707fde599d7557ce01c12208802a63cb

linux arm64 URL:
https://storage.googleapis.com/antigravity-public/antigravity-cli/1.2.2-6061403484848128/linux-arm/cli_linux_arm64.tar.gz
sha512:
a1645a30f36b767c7534c2f6a53e99a9bfade993267efcca715f7a45d797d47d6561df787e9d4a51a3bdfc9be855d49d23fa3f4b91b2c661fd17314050836048
```

Only amd64 was downloaded and run. Its archive was **57,596,853 bytes**.
It contained one regular file named `antigravity`, **213,582,080 bytes**.
The extraction renamed that file to `agy`. These sizes are file sizes, not
measured SSD writes. No SSD wear estimate follows from them.

### 4.2 Native commands

Observed `agy --version` output: `1.2.2`, exit status zero.
Observed `agy --help`: exit status zero. Relevant accepted options include:

- `--continue` / `-c` and `--conversation`
- `--print` / `-p`, `--prompt`, and `--print-timeout`
- `--output-format text|json|stream-json` and `--input-format`
- `--dangerously-skip-permissions`, `--sandbox`, and `--mode`
- `--model`, `--effort`, `--project`, `--new-project`, and `--add-dir`
- `--log-file`

The help lists `install`, `update`, `plugin`, `mcp`, and `remote-control`
subcommands. Do not invoke `install` or `remote-control start` during an
ordinary Cooper launch. They can change shell or service-manager state.

The [headless guide](https://antigravity.google/docs/cli/headless) describes
machine-readable results and continuation. Use the native help and a bounded
runtime check to confirm the exact selected release before writing a proof
command.

### 4.3 Startup result and limits

Observed startup records show:

- An in-process/native language-server backend starts and listens on random
  local HTTP and HTTPS/gRPC ports.
- The CLI initializes its store manager and reaches `CLI ready for user input`.
- With no D-Bus session, it selects file-based token storage.
- No authenticated account is present. The log reports that login is needed
  for account-dependent operations.
- The first probe explicitly disabled auto-update. The second allowed the
  normal updater, which spawned a background process. Its status record said
  that the installed version was already current.

No additional payload download was reported in these startup records. This
was not a complete packet or proxy trace. It does not prove that first login,
first inference, browser use, a plugin, or another platform needs no download.

The first two probes did not capture a rendered screen and returned status
137 after forced termination. A third probe used `timeout --foreground` so
the application could use the controlling terminal. It displayed the native
welcome screen with Google OAuth and Google Cloud login choices.

No login option was selected. Ctrl+C displayed an exit confirmation; a second
Ctrl+C restored the terminal and the process exited with status zero. The
log recorded CLI, language-server, and store-manager shutdown. The updater
skipped its recent check on this third start. This confirms **native login-screen
startup and a clean Ctrl+C exit in the tool environment**.

Use foreground timeout mode for later PTY probes. The changed harness resolved
the missing-screen observation. An empty crash-log file existed after the
second probe; it contained no error or stack trace and is not evidence of a
specific application crash.

The next session must repeat the check inside the actual Cooper image and VM.
Ctrl+D and authenticated-session exit remain untested. Do not report this
research as a successful authenticated session.

### 4.4 Public source availability

The official [google-antigravity/antigravity-cli repository](https://github.com/google-antigravity/antigravity-cli)
showed a README, changelog, issue templates, and examples. It did not show the
CLI implementation source. The [Python SDK](https://github.com/google-antigravity/antigravity-sdk-python)
is a separate product. Do not use its source as proof of CLI token storage or
proxy behavior.

No CLI implementation source was cloned. If Google publishes it before the
next pass, the user has allowed a source clone under `/tmp`. Confirm the
official repository, license, and commit first. Do not substitute Gemini CLI
source for Antigravity CLI source.

## 5. Complete host state and default mounts

### 5.1 Main decision

**Mount the complete `~/.gemini` root read-write for the selected Antigravity
agent. Do not mount only `~/.gemini/antigravity-cli`.**

The CLI uses sibling directories for shared configuration and conversation
data. Mounting a subset would lose settings and session continuity. The root
also contains data shared with other Google coding products; document that
shared ownership. Do not split its children into Cooper-managed copies.

Resolve `~` from Cooper's effective host account. Source and target must be
the same absolute path. For example, `/home/alex/.gemini` stays
`/home/alex/.gemini` in both execution modes. On a macOS host, preserve its
effective host path in the Linux barrel too. Do not hardcode `/home/user`.

Add the root through
[agentpaths.go](cooper/internal/workload/agentpaths.go), so both backends use
the same policy. With current evidence, the initial static entry is:

```go
"antigravity": {
    {ID: "antigravity-state", Base: "home", Path: ".gemini", Kind: Directory},
},
```

This is proposed code, not an instruction to bypass the research on effective
path overrides or additional roots.

### 5.2 Data covered by the root

These are children of one mounted root, not separate mount instructions:

| Path below the host home | Purpose and evidence |
| --- | --- |
| `.gemini/antigravity-cli/settings.json` | CLI settings; documented and requested at startup. |
| `.gemini/antigravity-cli/keybindings.json` | Keybindings; documented. |
| `.gemini/config/` | Shared config, MCP definitions, project definitions, migrations, and current plugin/customization data; observed plus current changelog. |
| `.gemini/config/config.json` | Shared settings; requested at startup. |
| `.gemini/config/projects/` | Project identity and grants; a default project file was created at startup. |
| `.gemini/antigravity/conversations/` | Conversation storage; startup requested this path. It was absent with no account/session. |
| `.gemini/antigravity-cli/conversation_summaries.db` and adjacent `-wal` / `-shm` | Summary database and SQLite side files; observed. |
| `.gemini/antigravity-cli/cache/` | Project/session lookup state; observed and documented. |
| `.gemini/antigravity-cli/cache/last_conversations.json` | Documented workspace-to-conversation cache; not produced by an authenticated probe. |
| `.gemini/antigravity-cli/knowledge/` | Knowledge state directory; its lock file was observed. Verify actual memory persistence later. |
| `.gemini/antigravity-cli/builtin/` | Bundled skills/customizations materialized at startup; observed. |
| `.gemini/antigravity-cli/plugins/` and `skills/` | Documented legacy/private customization paths; current plugin location also needs the shared config root. |
| `.gemini/antigravity/mcp_oauth_tokens.json` | Documented MCP token path. Confirm CLI use and actual selected-release behavior. |
| `.gemini/antigravity-cli/updater/`, installation IDs, logs, and state files | Application state; do not delete or hide these children through separate mounts. |
| Auth token/profile files and future children | Included by mounting the complete root. The exact account-token fallback filename is still unverified. |

Sources: [settings](https://antigravity.google/docs/cli/settings),
[resume](https://antigravity.google/docs/cli/commands/resume),
[MCP](https://antigravity.google/docs/cli/mcp),
[plugins](https://antigravity.google/docs/cli/plugins), and
[current changelog](https://raw.githubusercontent.com/google-antigravity/antigravity-cli/main/CHANGELOG.md).

Do not use a single SQLite file bind. Keep its parent directory mounted so
atomic file replacement, locking, WAL, and shared-memory files can work.
Whole-root mounts provide this property for the known paths.

### 5.3 Additional paths to resolve

| Path or input | Proposed treatment | What remains to verify |
| --- | --- | --- |
| Workspace `.agents/`, `.system_generated/`, and other workspace files | Already within the same-path workspace mount. | Follow customizations, generated worktrees, and absolute links during continuation. |
| Host `~/.agents` | Add as a shared selected-agent root only if the native CLI reads it. | Docs examined establish workspace `.agents`, not complete global discovery behavior. |
| `~/.cache/antigravity` | The published installer uses it for staging. Direct image installation avoids that installer path. | Determine whether the native runtime also stores reusable payloads or required state there. If required, preserve the complete effective host root. |
| Effective Google Cloud config root, commonly `~/.config/gcloud` | Conditional selected-agent dependency for ADC, not an unconditional mount for every Antigravity user. | Confirm `CLOUDSDK_CONFIG`, ADC lookup, required files, and Linux/macOS behavior. |
| `GOOGLE_APPLICATION_CREDENTIALS` | Support the effective explicit credential path if that auth mode is in scope. | Determine lookup rules, rotation, external-account dependencies, and the smallest complete required root. |
| OS keyring directories or D-Bus sockets | No blanket mount of a whole keyring, home, or host session bus. | A directory mount does not make macOS Keychain or a host Secret Service usable in Linux. See the auth condition below. |
| Native browser profile, browser helper, or remote-control state outside `.gemini` | Research only, until the actual paths and feature need are known. | Do not copy a complete host browser profile or expose a host control socket as a convenience. |

The implementor must finish this table after a real authenticated state trace.
It is not sufficient to assert that all auth must be under `.gemini` because
the visible settings are there.

### 5.4 Mount invariants and checks

- Only the selected agent gets its roots. Other agents must not acquire
  `.gemini` because Antigravity is enabled in the configuration.
- Use `HostState`, read-write access, the effective host account, and the
  shared mount-plan ownership checks.
- Preserve unknown files, settings, permissions, and future root children.
- Reject a state root that exposes the whole home or overlaps Cooper-owned
  storage. Check existing symlinks and both directions of overlap.
- Keep executable installation outside all state roots.
- Do not recursively change ownership or modes on existing host state.
- Mount identity changes must invalidate runtime reuse through the existing
  digest. Normal database writes must not cause a new runtime.
- Keep workspace paths identical. Session caches can contain absolute paths.
- Cooper stop, rebuild, cleanup, and VM cache cleanup must preserve host state.

## 6. Authentication and environment

### 6.1 Account login is the main compatibility condition

The [auth guide](https://antigravity.google/docs/cli/install) describes native
keyring login and a manual URL/code flow for remote terminals. The startup
probe and current changelog also show file storage when D-Bus is absent.

These facts establish that a container can use file storage. They do **not**
establish that a host account stored only in Keychain or Secret Service is
automatically available in that file storage.

Research these cases separately:

1. Host Linux account already uses file storage; Cooper opens that same root.
2. Host Linux account uses an unlocked keyring; Cooper has no host D-Bus.
3. Host macOS account uses Keychain; Cooper runs Linux.
4. First login takes place inside Cooper, followed by continuation on the host.
5. Token refresh and account selection remain correct across restarts.

Prefer an upstream-supported portable storage mode that the host and Cooper
can both use with the same selected state root. Verify whether it is automatic
or requires a user setting. Do not invent an export format, scrape credentials
from a keyring, copy tokens into a new Cooper profile, or mount the whole
session bus.

If transparent host-keyring reuse needs a narrow host broker, first document
the API, auth scope, local endpoint protection, VM transport, and both-mode
behavior. Treat that as a separate design condition, not a small mount fix.
If no supported approach meets the repository's continuity rule, report the
remaining compatibility limit before calling the feature complete.

No login was attempted in this session, as the user requested.

### 6.2 API key and ADC modes

The official auth guide supports `GEMINI_API_KEY` when the host settings select
`modelProvider: "gemini"`. The environment variable alone does not switch
provider. The guide also identifies `GOOGLE_GEMINI_BASE_URL` for a custom
compatible endpoint. Preserve the user's setting and endpoint.

The [enterprise guide](https://antigravity.google/docs/enterprise) documents
ADC with `AGY_ADC_AUTH=true` and a Google Cloud credential file. Enterprise
region and identity-provider behavior need separate verification.

| Value | Integration plan |
| --- | --- |
| `GEMINI_API_KEY` | Add to selected-agent secret resolution. Never place it in image layers, generated Dockerfiles, proof output, or argv. |
| `GOOGLE_GEMINI_BASE_URL` | Preserve an explicitly configured non-secret endpoint through the common launch policy. It does not authorize the destination in the whitelist. |
| `AGY_ADC_AUTH` | Preserve when supplied for the selected agent. Do not set it to change the user's login mode. |
| ADC path and Cloud SDK path settings | Resolve and mount only after the exact upstream lookup rules are verified. Keep source and target paths identical. |
| Model, effort, theme, permission, telemetry, credit, and rendering settings | Use the mounted host settings. Do not generate replacement preferences. |
| `AGY_CLI_DISABLE_AUTO_UPDATE=true` | Proposed image package control so the running tool remains at the version Cooper built. Apply in both modes; do not edit the host shell profile. |

Latest mode should resolve when Cooper builds or updates an image. It should
not mean that each session silently changes the binary behind Cooper's
version record. The startup probe confirmed that the documented update-disable
variable is recognized. Verify that it prevents background downloads and
binary replacement for every supported release.

Keep the secret and non-secret paths distinct in code. Reuse
[auth/resolve.go](cooper/internal/auth/resolve.go),
[launch/session.go](cooper/internal/launch/session.go), and the
[barrel environment policy](cooper/internal/barrelenv/script.go).
Protect Cooper's proxy and account variables from shell configuration changes.
Do not forward all `AGY_*`, `ANTIGRAVITY_*`, or Google environment values.

## 7. Internet access and the default whitelist

### 7.1 Domains requested in this research session

All requests used `curl` through the approval proxy. Initial requests did not
follow redirects. These exact eight hosts passed through the proxy:

| Exact host | Research purpose |
| --- | --- |
| `antigravity.google` | Product pages, docs, and installer source. |
| `developers.googleblog.com` | Approved official-post research; only the initial domain request was needed. |
| `blog.google` | Approved official-post research; only the initial domain request was needed. |
| `github.com` | Official organization and CLI repository. |
| `raw.githubusercontent.com` | Official CLI changelog. |
| `www.google.com` | Official-source search. The returned search page did not provide useful network guidance. |
| `antigravity-cli-auto-updater-974169037036.us-central1.run.app` | Platform release manifests. |
| `storage.googleapis.com` | Official archive, after the user authorized install and startup. |

Research approval is not itself a reason to add a domain to Cooper's product
defaults. For example, the two blog hosts are not established CLI dependencies.

### 7.2 Required install hosts

These have direct distribution evidence and should be handled when the
built-in Antigravity tool is enabled:

| Exact host | Purpose | Default-policy decision |
| --- | --- | --- |
| `antigravity-cli-auto-updater-974169037036.us-central1.run.app` | Resolve the current release and platform artifact metadata. | Include in the managed Antigravity install/default host set. Runtime auto-update remains disabled in Cooper. |
| `storage.googleapis.com` | Fetch the versioned native artifact. | Include for supported installation and nested Cooper builds. Verify the final artifact URL and digest. |
| `antigravity.google` | Official bootstrap and documented web/callback entry points. | Include if the final supported setup/auth flow uses it; the direct image recipe does not need to execute its installer. |

Cooper's domain ACL grants a host, not a specific storage bucket. An exact
`storage.googleapis.com` entry therefore covers more than this one artifact
path. Record this scope rather than claiming bucket-level restrictions.

### 7.3 Runtime hosts: research candidates, not a verified final list

**A complete runtime whitelist was not established without login and model
calls.** The following exact candidates come from official feature docs or
static strings in the verified binary. Observe the selected mode before
promoting a candidate to a required default.

| Exact host or family to resolve | Expected purpose | Evidence and required next step |
| --- | --- | --- |
| `accounts.google.com` | Google account authorization. | URL in the binary; docs describe Google sign-in. Record the actual supported browser/manual-code flow. |
| `oauth2.googleapis.com` | Account token exchange or refresh. | URL in the binary. Verify account login and a refresh through the proxy. |
| `www.googleapis.com` | Google account/API operations. | URL in the binary. Identify the actual operation before adding it. |
| `cloudcode-pa.googleapis.com` | Candidate account agent service. | Static hostname only. Observe production inference and capability lookup. |
| `daily-cloudcode-pa.googleapis.com` | Another compiled agent service candidate. | Static hostname only. Do not assume it is used by the public release. |
| `generativelanguage.googleapis.com` | Direct Gemini API mode. | Binary URL plus documented API-key mode. Verify the default endpoint and streaming transport. |
| `aiplatform.googleapis.com` | Enterprise API. | Named by enterprise docs and the binary. Verify the selected region and actual endpoints. |
| `businessaicode.googleapis.com`, `agentaicode.googleapis.com`, `aicode.googleapis.com` | Compiled enterprise/agent API candidates. | Static hostnames only. Classify with an authenticated trace; do not enable the entire group from strings. |
| `sts.googleapis.com`, `iamcredentials.googleapis.com` | Possible ADC, federation, or impersonation operations. | Binary URLs. Needed only if the selected credential flow uses them. |
| `antigravity.google.com` | Remote Control web surface and possible service use. | Official site link and binary URL. Remote Control is separate from a normal CLI startup; verify any core use. |
| `play.googleapis.com`, `safebrowsing.googleapis.com` | Possible metrics or URL-check operations. | Binary URLs only. Purpose and startup dependence are unverified. |
| `www.gstatic.com` | Possible web or feature assets. | Binary URL and site assets. Not proof of a native CLI requirement. |
| Exact regional service hosts, organization identity hosts, MCP hosts, and browser-download hosts | Selected optional features. | Resolve from official configuration and actual traffic. Never derive permission from a wildcard guess. |

Strings also contained staging/corporate endpoints, protobuf type domains,
and concatenated text that resembled invalid hostnames. Exclude these from
defaults. Do not add `*.googleapis.com`, `*.google.com`, `*.run.app`, or
all Google storage hosts to make an incomplete trace pass.

Optional user-selected endpoints must still work through normal Cooper
approval. A custom MCP URL, Gemini-compatible URL, or organization IdP is not
automatically trusted because Antigravity reads it from a configuration file.

### 7.4 How to finish the whitelist

Use one bounded session for each supported auth/feature mode, after the other
session releases the test resources:

1. Start with clean, test-owned state and the proposed default hosts.
2. Keep the Cooper proxy as the only outbound route. Record hostname, port,
   phase, and allow/deny result. Do not record token bodies or authorization
   URL query strings.
3. Cover first startup, manual login, account restore, refresh, one short
   response, session resume, and normal exit.
4. Separately cover API-key mode and the supported ADC/enterprise modes.
5. Exercise configured plugins/MCP and browser downloads only in their named
   feature cases. Record any helper artifact version and cache path.
6. For each new host, find an official source or a reproducible operation
   that needs it. Remove it once and repeat that bounded operation to confirm
   the dependency when safe and useful.
7. Re-run the core flow with the final exact list and no manual approvals.
8. Confirm that an unrelated host still reaches the approval path or is denied.

The final domain record must state its evidence, supported versions, owning
feature, whether it is required by default, and whether redirects introduce
another host. A successful unauthenticated `curl -I` is not that record.

### 7.5 Configuration implementation

Follow the enabled-tool reconciliation pattern in
[config/config.go](cooper/internal/config/config.go). Add managed Antigravity
defaults when the built-in tool is enabled. Preserve user-added entries and
avoid case-insensitive duplicates. When disabling Antigravity, remove only
its exclusively owned defaults.

Check shared ownership before extending the existing Grok pattern:
`github.com`, Google hosts, or storage hosts can serve other enabled features.
Disabling one tool must not remove a required default for another tool.
Use a small explicit ownership mapping if sharing requires it.

Keep `IncludeSubdomains: false` for exact entries. Update fresh configuration,
existing configuration migration, enable/disable, and TUI save paths. Use the
existing whitelist source field consistently; do not reclassify a user entry
as a default.

Verify that runtime policy reload works for already running sessions through
the current shared proxy path. Do not create a second Antigravity proxy or
separate CLI/VM domain lists.

## 8. Version resolution and image construction

### 8.1 Resolve an artifact, not only a version string

The current manifest has `version`, `url`, and `sha512`. Its artifact path
contains an opaque build identifier in addition to the semantic version.
Do not form an old artifact URL by replacing `1.2.2` in the observed URL.

Define a small Antigravity release record with:

- Product and semantic version.
- Target OS, architecture, and libc choice.
- Complete official artifact URL.
- SHA-512 digest of the archive.
- Source manifest/index URL and retrieval time.

Use the existing configuration/build flow to obtain the record before
rendering the Dockerfile. Store the resolved record with the generated
Antigravity build context, or in another existing owned metadata mechanism.
The build must consume that record rather than fetch a moving manifest again.
Keep deterministic template rendering free of hidden network access.

The proposed mode behavior is:

| Mode | Required behavior |
| --- | --- |
| Off | No selected image, no selected state mount, and no exclusively owned Antigravity defaults. |
| Latest | Resolve the official current artifact once for the target platform; freeze that record for the build. |
| Mirror | Detect native `agy --version` on the host, then resolve the matching Linux artifact. Do not copy a host macOS executable into a Linux image. |
| Pin | Resolve exactly the requested version for the target platform. A missing release is an explicit error. |

**Open:** An official historical CLI artifact index or version-addressed
manifest was not found in this pass. The website's releases page showed
desktop products, not a usable CLI archive index. Finish this research first.
If only retained verified release records are available, define the supported
version set explicitly. Never silently turn Mirror or Pin into Latest.

Use real JSON decoding, bounded response sizes, a timeout, status checks,
version validation, a strict digest format, and URL validation. Reject
credentials in URLs and malformed or unexpected schemes. Record any allowed
redirect hosts. Do not interpolate unvalidated metadata into shell code.
Use fixture HTTP servers for resolver tests; do not make unit tests depend on
Google's moving production manifest.

### 8.2 Image recipe

Implement the native recipe in
[templates.go](cooper/internal/templates/templates.go) and the current CLI
image template. The recipe should:

1. Select the artifact for the Docker build target architecture.
2. Download the frozen URL into a temporary image-build directory.
3. Verify SHA-512 before extraction or execution.
4. Extract only the expected regular executable. Reject unexpected archive
   member types or paths; do not extract the archive over the root filesystem.
5. Install as `/opt/cooper/bin/agy` with executable permissions.
6. Check its version against the release record.
7. Remove archive/staging files within the same image layer.
8. Set the package auto-update control for runtime.

Use the host-account image contract already implemented by Cooper. Do not
install into `.gemini`, `.local/bin`, or another mounted host-state location.
Do not run the native `install` subcommand inside a launched barrel.

Do not add a large desktop package, another Docker daemon, a browser profile,
or an SDK as a presumed CLI dependency. The amd64 probe started its backend
from the one native artifact. Verify shared-library and helper requirements
inside the actual base image, then add only required dependencies.

Any first-use helper download found later must have a known source, platform,
version policy, cache owner, and required domain. Prepare stable runtime
dependencies at image-build/preparation time where practical. A warm launch
must not rebuild images or download the main agent again.

### 8.3 Configuration collision

Before writing generated `cli/antigravity` files, detect an existing custom
tool directory with that name. Fail with a clear migration message when it
is user-managed. Do not overwrite its Dockerfile or call it a generated
built-in because the new catalog now reserves the name.

Inspect the current Grok collision handling in `templates.go` and
[buildflow/build.go](cooper/internal/buildflow/build.go). Extend the small
ownership check to the new built-in, without replacing unrelated custom-tool
behavior. Preserve catalog order and append the new item.

## 9. Command identity, permissions, and session behavior

### 9.1 Separate tool identity from executable name

The current built-ins happen to use a tool key that is also a command name.
Antigravity breaks that assumption. Keep `antigravity` as the config, image,
runtime-label, mount-policy, and UI identity. Use `agy` when executing the CLI.

Add a small executable field or equivalent lookup to
[aitool/catalog.go](cooper/internal/aitool/catalog.go). Give existing built-ins
their current executable names. Do not infer the executable from the display
name. Avoid a general provider framework; this is static identity metadata.

Audit these concrete consumers:

- Host version detection uses `agy --version`.
- Image/version checks run `agy`, including checks inside login shells.
- The generated auto-approve alias targets `agy`.
- Proof commands and agent-specific test commands target `agy`.
- Catalog names, completion, TUI labels, image names, and state lookups remain
  `antigravity`.
- Custom tools retain their existing command and image contract.

The current entrypoint forms its alias from `COOPER_CLI_TOOL`. It needs an
explicit executable value for this agent. Keep the tool identity variable
unchanged for code that uses it as an identity. Validate executable metadata
as a command name, not a shell fragment.

Do not create a misleading host `antigravity` alias or detect the desktop
launcher as the terminal agent. An extra public `agy` Cooper tool alias is
not required for the first implementation.

### 9.2 Permissions

The verified binary accepts `--dangerously-skip-permissions`. It is the
candidate for Cooper's existing interactive auto-approve alias policy. Check
that policy against the current repository before applying it. Do not add
the flag to arbitrary user `-c` commands or rewrite host permission files.

Preserve the agent's own configured sandbox and permission behavior unless
the existing Cooper invocation explicitly supplies an override. Test both
default/normal approval and the supported Cooper auto-approve invocation.
Keep the outer Cooper network boundary in both cases.

Native sandbox commands use OS facilities. If a configured native sandbox
fails in a barrel, identify the exact kernel or runtime requirement. Do not
solve it by adding privileged mode, a host Docker socket, a guest NIC, or an
unrestricted route. Do not silently change the user's saved sandbox setting.

### 9.3 Same session contract in CLI and VM

Use [launch/session.go](cooper/internal/launch/session.go) for selected-agent
credentials, environment protection, shell behavior, and one-shot execution.
Use [workload/environment.go](cooper/internal/workload/environment.go) for the
shared proxy environment. Avoid Antigravity branches in both execution
backends when the shared policy can express the behavior once.

Preserve resume arguments exactly. Keep both the workspace and state paths
identical, including paths with spaces and symlink cases covered by current
Cooper rules. Verify actual continuation by conversation ID and by `-c`;
seeing a database file in the guest does not prove session continuation.

When the user supplies `--project` or `--add-dir`, preserve upstream behavior.
An additional path must still be available through Cooper's permitted
workspace/state mounts. Do not mount the whole host home to make every
possible configured path appear.

## 10. Proxy transport, local services, and clipboard

### 10.1 Outbound transport

Verify account requests, direct API requests, MCP transports, tool subprocesses,
and any helper downloads with the same uppercase/lowercase proxy environment
that Cooper supplies today. Confirm TLS trust using Cooper's existing CA
setup. Do not disable certificate verification.

Keep local backend traffic local. The probe used random local HTTP and
HTTPS/gRPC ports. Check `localhost`, `127.0.0.1`, and any actual IPv6 loopback
use against `NO_PROXY`. Only change the common loopback policy if evidence
requires it. Do not add remote service hosts to `NO_PROXY`.

If HTTP/2, gRPC, WebSocket, or streaming requests fail, preserve the failing
operation and proxy evidence. Determine whether the failure is trust,
protocol support, name resolution, or policy. Do not infer a cause from a
timeout alone. The existing selective TLS handling must be reviewed before
adding any narrow protocol exception.

The VM still has no direct internet/LAN route. Guest root, guest Docker,
builds, containers, and nested Cooper must use the host proxy. Antigravity
must not introduce a new outbound network mechanism.

### 10.2 Local agent services and forwarding

Research whether startup always creates a private backend or can attach to a
process named in shared state or inherited environment. The binary contains
IDE/remote-control transport names, but their presence alone does not prove
use in normal CLI mode.

Do not forward a host backend address, CSRF token, browser debugging socket,
or host IPC socket as part of generic environment passthrough. Keep runtime
transport inside the existing runtime boundary. If an upstream path setting
is needed for isolation, document and verify it before use.

Check the existing port discovery/forwarding code against the agent's local
backend ports. Verify binding scope and backend access control. Developer
web servers must retain current forwarding behavior. Private control ports
must not become an unauthenticated host or LAN control interface.

Remote Control service-manager registration is not a startup dependency.
If the user later enables Remote Control, it needs an explicit feature test
and domain/state review. Do not start a persistent host service in order to
make a terminal session work.

### 10.3 Clipboard

The [prompting guide](https://antigravity.google/docs/cli/prompting) documents
media paste. The current changelog names native Wayland support with an X11
fallback. This is useful evidence, but it does not prove Cooper clipboard
compatibility.

Propose `x11` as the initial catalog mode because Cooper already supplies its
local X11 clipboard bridge. Confirm it with the actual Linux CLI. If it uses
the existing command shims correctly, use that proven mode instead. Do not
select `auto` merely to hide an unknown result.

Check text copy, image paste, cancellation, and multiple sessions. Compare a
known image digest and prove which Cooper bridge path was used. Terminal OSC
52 text copy is not evidence of image paste support. Preserve the current
clipboard payload limit; large video support would be separate capacity work.

## 11. Source map for implementation

Re-read each file at the implementation baseline. The names below were
confirmed during this pass; line numbers can change.

| Area | Files and required work |
| --- | --- |
| Static agent identity | [catalog.go](cooper/internal/aitool/catalog.go), its tests: append Antigravity, native executable identity, version command, directories, and verified clipboard/alias data. |
| Version modes and resolver | [versions.go](cooper/internal/config/versions.go), [resolve.go](cooper/internal/config/resolve.go), resolver tests: native release metadata, target architecture, strict Mirror/Pin/Latest behavior. |
| Defaults and migration | [config.go](cooper/internal/config/config.go), config tests: catalog enablement, managed exact domains, shared ownership, old config handling. |
| Generated images | [templates.go](cooper/internal/templates/templates.go), [cli-tool.Dockerfile.tmpl](cooper/internal/templates/cli-tool.Dockerfile.tmpl), template tests: frozen artifact and checksum, executable metadata, runtime update control. |
| Shell setup | [entrypoint.sh.tmpl](cooper/internal/templates/entrypoint.sh.tmpl), relevant runtime tests: alias uses `agy`; no write of replacement Antigravity settings. |
| Build ownership | [buildflow/build.go](cooper/internal/buildflow/build.go), generated-output checks: protect a pre-existing custom `antigravity` directory. |
| Host state | [agentpaths.go](cooper/internal/workload/agentpaths.go), [mountplan.go](cooper/internal/workload/mountplan.go), path/mount tests: whole selected roots, conditional auth roots, reuse identity, cleanup protection. |
| Auth and session | [auth/resolve.go](cooper/internal/auth/resolve.go), [launch/session.go](cooper/internal/launch/session.go), [barrelenv/script.go](cooper/internal/barrelenv/script.go), their tests: selected secret resolution and non-secret path/environment policy. |
| Version/proof consumers | [proof.go](cooper/internal/proof/proof.go), [proof_test.go](cooper/internal/proof/proof_test.go), [app tests](cooper/internal/app/cooper_test.go): remove new-agent assumptions that the tool key is the executable. |
| CLI and TUI discovery | [main.go](cooper/main.go), `cooper/internal/configure/`, TUI mocks and completion: help, new row, disabled/default state, and command examples. Read `AGENTS.TUI.md` before TUI changes. |
| Full build/shell gates | [test-docker-build.sh](cooper/test-docker-build.sh), [test-e2e.sh](cooper/test-e2e.sh), [testdriver](cooper/internal/testdriver/driver.go): known-version matrix, executable checks, state fixtures, image ownership/cleanup. |
| Small VM checks | [vmdev/config.go](cooper/internal/vmdev/config.go), [vm_test.py](cooper/dev/vm_test.py), its parser tests, `cooper/internal/vme2e/`: selected-agent pin, `prepare-agent`/`parity` choices, shared state assertions. |
| Permission hooks and editor tasks | [.codex hook](.codex/hooks/allow_govner_dev.py), hook cases/tests, [.vscode tasks](.vscode/tasks.json): permit the exact new finite agent choice; preserve existing command limits. |
| User/developer docs | [Cooper README](cooper/README.md), [REQUIREMENTS.md](cooper/REQUIREMENTS.md), [development README](cooper/dev/README.md), applicable root guidance: install, login modes, whole roots, domains, limits, and release coverage. |

Search for hardcoded five-agent lists, version maps, `tool.Name + " --version"`,
`which <tool name>`, shell aliases, image matrices, and tests that count catalog
entries. Review each match; do not perform an unreviewed global replacement.

The CLI and VM backend files should need little agent-specific code. Changes
there need a clear reason that the shared catalog, paths, launch, or template
policy cannot provide.

### Required reusable agent integration checklist

Create **`cooper/dev/adding-an-agent.md`** during the Antigravity implementation.
This is a required deliverable. Cooper will support more coding agent
harnesses, so the document must make the next integration faster and prevent
missed changes.

Keep it as one terse checklist, in implementation order. Each item should
state the action, link to the current source or test entry point, and name
the required check. Mark conditional items and their trigger. Link to detailed
guides for explanations; do not repeat this plan or create separate copies of
the common checklist for each provider.

The checklist must cover:

| Step | Required coverage |
| --- | --- |
| Research and scope | Confirm the native product, official distribution, supported platforms, source availability, internet approvals, and unresolved assumptions. |
| Identity and installation | Tool key versus executable; catalog; version detection and modes; architecture; artifact verification; dependencies; update control; custom-name collisions. |
| State and auth | Complete selected host roots; path overrides; account identity; read-write ownership; keyring/file/token modes; session continuity; symlinks; cleanup protection. |
| Launch and environment | Shared CLI/VM policy; selected secrets; protected environment; host settings; aliases and permission flags; interactive and one-shot behavior. |
| Network and runtime features | Verified exact default hosts and their ownership; proxy/TLS; local IPC; clipboard; ports; browser/MCP helpers; VM and nested-runtime isolation. |
| All integration points | Version/proof consumers; hardcoded agent lists; configuration migration; TUI/help/completion/mocks; image matrices; fixture pins; finite test choices and permission hooks. |
| Validation and release | Focused local checks; one selected image; prepared VM parity; actual auth/resume checks; required final gates; full VM testing only before release; owned cache reuse and cleanup. |
| Documentation and handoff | User/developer docs; support limits; evidence for assumptions; release coverage; update this same checklist with newly found common steps. |

Draft it as phases 1-4 reveal integration work, then finalize it in phase 5.
Keep Antigravity-specific versions, domains, paths, and unresolved research in
this plan or the agent's detailed documentation. The reusable checklist must
tell the implementor how to find those values for the next harness.

Before completion, walk the checklist against the final Antigravity change
set. Check it against one existing built-in with a different install or auth
method too, so it does not assume every harness behaves like Antigravity.
This is a source/document review; it does not require another VM run. Add any
missing common step, verify its links and commands, and link the checklist
from `cooper/README.md` and `cooper/dev/README.md`.

## 12. Documentation conflicts to resolve

Use the exact selected binary and dated official records to resolve conflicts.
Do not copy a web example into a fixture without checking its current behavior.

| Topic | Evidence conflict | Required response |
| --- | --- | --- |
| Current version | Product/download pages showed `1.2.0`; both manifests and the native binary showed `1.2.2`. | Resolve from platform release metadata and verify the executable. |
| Installer flags | Install docs list skip-profile flags; the script read here accepts a custom directory and hands off to `agy install`. | Avoid the convenience installer in the image; inspect actual subcommand help if future setup needs it. |
| Auth storage | General docs emphasize keyrings; the binary and changelog support a file fallback without D-Bus. | Trace the selected release and verify host-to-container account reuse. |
| Plugin roots | A docs page names private CLI plugin storage; the changelog moves installation into shared `.gemini/config`. | Mount the whole root and test both existing state and current plugin discovery. |
| Sandbox rules | Sandbox docs describe `unsandboxed(...)`; release `1.2.2` warns that these rules are deprecated. | Preserve host rules; use current supported syntax in new fixtures. Do not migrate the user's files for them. |
| Resume selection | Docs describe a workspace-keyed cache; release `1.2.1` expands fallback selection across related directories. | Test ID-based and recent-session continuation at the actual workspace path. |
| Headless completion | Current changelog allows a print timeout to return partial output with a successful exit status. | A proof must check the expected result and timeout/denial indicators, not only exit zero. |

The official [changelog](https://raw.githubusercontent.com/google-antigravity/antigravity-cli/main/CHANGELOG.md)
is the source for the release-specific changes in this table. These entries
also explain why a second research pass is required.

## 13. Implementation phases

### Phase 0: Recheck the baseline and close research conditions

1. Read current root/project instructions and README files. Preserve concurrent
   changes. Confirm which test resources are available before any runtime work.
2. Re-fetch official release metadata and help for the version to support.
3. Complete the default-root, account-storage, and host/Cooper continuity study.
4. Find or define a verified historical release lookup for Mirror and Pin.
5. Finish the required runtime-domain trace with an authorized account later.
6. Complete a rendered native startup and clean-exit check without a model call.
7. Update the assumption register with source, observation, and remaining scope.

Exit condition: the install record, auth/storage design, supported platform
matrix, and required-domain design are concrete. Open optional feature work
is named. Core continuity or version-mode failures are not hidden as TODOs
behind an enabled built-in.

### Phase 1: Add identity and a deterministic image

Implement catalog identity, command-name separation, version resolution,
generated release records, the native image recipe, and custom-directory
collision handling. Add focused resolver/catalog/template tests.

Exit condition: an Antigravity-only build installs the exact expected binary;
its version command works at the expected path. No unrelated agent image is
built as a dependency. Host state cannot cover the image executable.

### Phase 2: Add complete state and auth policy

Implement `.gemini` and any verified additional selected roots in the shared
path resolver. Add the verified token/env modes through the common launch
path. Preserve file and keyring auth distinctions. Add meaningful mount,
selection, symlink, cleanup, and environment tests.

Exit condition: both backends receive the same policy. Disposable session
fixtures confirm state writes and preservation; authorized account checks
confirm actual continuity in each supported host-auth case.

### Phase 3: Add and verify managed defaults

Implement the final exact-host defaults and migration/reconciliation rules.
Check shared ownership, live reload, streaming, CA trust, subprocess traffic,
and no direct VM route. Keep optional custom endpoints under normal approval.

Exit condition: the supported core flow completes using only default entries,
while an unrelated destination still needs approval or is denied.

### Phase 4: Complete shell and runtime behavior

Verify `agy` in the login shell and one-shot path, the intended alias policy,
normal agent permissions, native sandbox compatibility, session resume,
clipboard, local backend scope, and developer port forwarding. Check exit
and child-process cleanup. Update proof output to state what each check proves.

Exit condition: the common CLI/VM behavior contract is demonstrated. Provider
login, version availability, and a real model reply have separate results.

### Phase 5: Integrate small checks, documentation, and final gates

Add Antigravity to the finite development choices, selected-agent fixture pin,
permission-hook cases, and release agent matrix. Update all five-agent docs
and help lists to reflect the actual supported set. Finalize and review the
single reusable `cooper/dev/adding-an-agent.md` checklist described in section
11. Complete the validation sequence below once the code is stable and
resources are available.

Exit condition: required non-release gates pass, selected-agent VM parity
passes, docs match the verified behavior, and remaining external limitations
are explicit. The reusable checklist covers the completed integration and is
linked from the project and developer guides. At release time, the full VM
gate includes Antigravity.

## 14. Validation plan with limited SSD work

All commands in this section are for the implementor. They were not run as
part of this planning session. Read current `AGENTS.md` before executing them.
The prepared VM profiles landed during this research; extend their current
implementation instead of creating a second VM harness.

### 14.1 Local tests first

Use focused package tests while changing the relevant code. Cover behavior,
not only the presence of strings in a generated file:

| Area | Necessary cases |
| --- | --- |
| Catalog and executable | Stable order; `antigravity` identity resolves to `agy`; other built-ins/custom tools retain behavior; defensive metadata copies. |
| Release resolver | Valid amd64/arm64 records; malformed JSON; size limit; timeout; HTTP failure; bad version/digest/URL; wrong architecture; unavailable historical version; a moving latest manifest cannot change a frozen build record. |
| Version modes | Native host command; exact Mirror/Pin version; explicit unavailable-artifact error; Latest resolved once; desktop launcher cannot satisfy native version detection. |
| Image/template | Correct artifact and digest; checksum verification before execution; binary outside state; target architecture; no profile-changing install command; update control in both modes. |
| Custom-tool collision | Existing custom directory is preserved; recognized generated directory can be regenerated; failure occurs before writes. |
| State mounts | Whole `.gemini` root; same source/target; selected agent only; unknown child preserved; WAL/SHM visible; no whole-home mount; optional ADC roots only when applicable. |
| Path safety | Spaces; missing root; existing file instead of directory; symlink overlap; Cooper overlap; root replacement changes reuse identity; ordinary state content changes do not. |
| Auth/env | Selected API key only; unset and empty values where meaningful; no invented provider choice; custom endpoint preserved; proxy/account protection; no credentials in generated files or diagnostics. |
| Whitelist | Fresh and migrated config; enable/disable; user entries preserved; case-insensitive duplicates; shared default ownership; no wildcard broadening; config reload. |
| Launch and alias | `agy` alias targets the native command; one-shot command is preserved; CLI/VM use the same session preparation; explicit permissions remain explicit. |
| Test entry points | The finite parser and permission hook accept only the new agent choice in existing modes; invalid extra arguments still fail. |

The current VM `unit` profile excludes Docker setup, blocks Docker/QEMU, and
disables registry downloads. Its package list does not directly include
`internal/aitool`; run that package's changed tests separately when needed.
Do not assume one profile covers every file in the integration.

Example after implementation:

```bash
go test -C ./cooper ./internal/aitool ./internal/config ./internal/templates ./internal/workload ./internal/auth ./internal/launch ./internal/barrelenv ./internal/proof > /tmp/cooper-antigravity-unit.txt 2>&1
./cooper/test-vm-dev.sh unit > /tmp/cooper-vm-unit.txt 2>&1
```

Use local HTTP fixtures and fake credential values. Unit tests must not fetch
Google manifests, refresh a real token, start an agent session, or call a model.

### 14.2 One selected image and one prepared VM parity check

After adding Antigravity to the supported choices and pin map:

```bash
./cooper/test-vm-dev.sh prepare-agent antigravity > /tmp/cooper-vm-antigravity-prepare.txt 2>&1
./cooper/test-vm-dev.sh parity antigravity > /tmp/cooper-vm-antigravity-parity.txt 2>&1
```

Preparation can build/download/export and can require a cold base preparation
VM. Reuse prepared inputs when their source, platform, account, and image
identity still match. The runtime parity command must preserve its rule that
it cannot build, pull, or export host images on a cache miss.

Use test-owned state, not copied real credentials. Extend the shared agent
assertions to check:

- `/opt/cooper/bin/agy --version` in both execution modes.
- Identical account, home, workspace, root mounts, and effective environment.
- `.gemini` write visibility in both directions, including unknown children
  and database side files.
- Other agents' state is absent; cleanup preserves the selected host fixture.
- A credential-free bounded native startup, if it can remain deterministic
  with no provider call or helper download.

If shared proxy, port, mount-refresh, or lifecycle code changes, run the
small matching existing profile. Do not run every lifecycle profile after a
catalog-only change. A generic small VM smoke check and an Antigravity native
startup check prove different things; report them separately.

### 14.3 Authorized account and real behavior checks

These are explicit integration checks, separate from offline development
profiles. Use an authorized account and small prompts only after login is
available in the implementation session.

| Check | Required evidence |
| --- | --- |
| Native startup | Rendered terminal/login screen; no missing library; no unrecorded required download; clean exit. |
| Host to CLI barrel to host | Continue the same conversation by ID and by normal recent-session selection; new turns remain visible. |
| Host to VM to host | The same continuity check through VM mounts and relay. |
| Existing keyring account | A verified supported transition to the Linux runtime, or a clearly unresolved compatibility condition. |
| File-token account | Same mounted root restores the account and refreshes it; no copied Cooper token profile. |
| API-key mode | Existing host provider setting and key are used; no browser requirement or silent switch of billing/provider. |
| ADC/enterprise | Exact selected credential and region flow works if included in the support claim. |
| Default whitelist | Core setup/restore/inference/resume completes without new manual service-host approvals. |
| Negative network case | An unrelated destination is still controlled by Cooper, including from a tool subprocess. |
| Clipboard | A known text/image payload reaches the actual CLI via the selected bridge; both modes agree. |
| Settings/customizations | Existing permission, model, plugin, MCP, hook, and memory settings are read from host state; unknown settings remain intact. |
| Concurrent sessions | Two small sessions do not attach to a host backend or corrupt shared state; SQLite/lock behavior is observed. |
| Exit and reuse | Ctrl+C/Ctrl+D and Cooper stop end the relevant process tree; restart reuses the intended account and session state. |

A model response is not necessary to prove image installation. A version
response is not sufficient to prove auth. A file mount is not sufficient to
prove conversation compatibility. Keep these results distinct in `cooper proof`.

For headless proof, use a tiny deterministic expected response, a bounded
wall-clock deadline, and the release's supported result format. Check the
expected response plus partial-output, error, and denied-action signals.
Current upstream behavior can return exit zero with partial output after an
internal print timeout. Do not count that as a successful complete proof.

### 14.4 Required final checks and release check

Once the code is stable and the test resources are available, current project
instructions require:

```bash
go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1
timeout 90m ./cooper/test-e2e.sh > /tmp/cooper-e2e.txt 2>&1
timeout 90m ./cooper/test-docker-build.sh all > /tmp/cooper-docker-build.txt 2>&1
```

Use the exact development build command if a new Cooper binary is needed:

```bash
go build -C ./cooper -o ./cooper . > /tmp/cooper-build.txt 2>&1
```

The full VM gate is only for the next actual Cooper release:

```bash
timeout 90m ./cooper/test-vm.sh > /tmp/cooper-vm-gate.txt 2>&1
```

Do not run it to estimate this feature's SSD cost. Adding an agent to the
release matrix can change its boot/import/export counts. Update the source
inventory after implementation, then confirm actual counts and device I/O
at the required release run. Follow the measurement rules in
[the VM development guide](cooper/dev/README.md).

Retain owned prepared artifacts between development checks. Do not prune
Docker, clear shared build caches, change swap, or delete another session's
fixtures to make these checks fit. Use the existing leased, exact-identity
cleanup commands only for owned records when cleanup is needed.

## 15. Assumption and verification register

This register includes proposed design choices, incomplete observations, and
external facts that must be rechecked. The implementor must add the result,
version, environment, and evidence when closing an item. Mark unsupported
cases explicitly; do not convert an open item into a silent fallback.

| ID | Assumption or open condition | Current basis | Required verification |
| --- | --- | --- | --- |
| A01 | The desired product is the native Antigravity CLI, with Cooper key `antigravity`. | User request plus official product distinction; proposed key. | Keep this scope in the implementation description; do not substitute the IDE or Gemini CLI. |
| A02 | `agy` is the correct native executable and version command. | Observed in `1.2.2`. | Recheck selected release and host detection; distinguish desktop aliases. |
| A03 | The current release is `1.2.2`. | Two manifests and the downloaded binary; web labels differed. | Re-resolve at implementation time; freeze and record the chosen version. |
| A04 | Linux amd64 works in the Cooper base. | Native startup only in the tool environment. | Check required libraries, helpers, user identity, and actual image startup. |
| A05 | Linux arm64 is supported by the same recipe. | Manifest only. | Verify archive layout, digest, native version/startup on ARM64, and target selection. |
| A06 | Historical Mirror/Pin releases can be resolved reliably. | Open; current manifest has an opaque build ID. | Find an official historical record or define a verified retained-record policy with explicit missing-version errors. |
| A07 | Main persistent state is covered by the complete `.gemini` root. | Several sibling roots documented and observed. | Trace files after auth, resume, memory, plugins, and shutdown; identify outside-root state. |
| A08 | Default `.gemini` lookup follows the effective home on supported hosts. | Docs and Linux startup. | Verify macOS/Linux lookup and any supported root override, including empty/relative values. Do not invent an environment variable. |
| A09 | The exact account-token fallback files are inside the selected root. | File fallback observed; filename not established. | Locate paths and storage behavior with test credentials; record names and permissions without reading tokens into reports. |
| A10 | An existing host keyring login can meet the shared-state requirement. | Unverified; portable file fallback alone does not establish this. | Test Linux keyring and macOS Keychain cases; settle any required supported storage mode or broker design. |
| A11 | Login begun in Cooper remains usable on the host. | Required behavior, not tested. | Verify host restore and refresh with the same profile, without credential copies. |
| A12 | Same-path mounts preserve actual conversation continuity. | Directory-scoped state is documented. | Verify actual conversations and history in both directions for CLI and VM. |
| A13 | SQLite and state locks work through VM filesystem sharing. | Current Cooper shared-root design; no Antigravity VM run. | Exercise small real state updates, clean exit, restart, and two sessions; check WAL and lock failures. |
| A14 | Global `~/.agents` needs a separate mount. | Open; workspace `.agents` is documented. | Observe global discovery; add only if it is a supported selected-agent dependency. |
| A15 | Runtime needs `~/.cache/antigravity`. | Installer uses it; native runtime use unverified. | Trace first-use downloads and cache writes; classify ownership and complete-root needs. |
| A16 | ADC and external credential paths can be mapped without broad exposure. | Enterprise docs; not executed. | Verify effective lookup/rotation and dependent files on each supported host. |
| A17 | `GEMINI_API_KEY` and custom endpoint passthrough preserve host behavior. | Official auth docs. | Verify native key mode, unchanged provider setting, exact endpoint, and selected-agent secret handling. |
| A18 | Disabling auto-update preserves all version modes. | Disable variable recognized; normal updater reported current version. | Prove no background replacement/download in the supported images and that explicit Cooper update still works. |
| A19 | No further download is needed for core startup. | None reported in three unauthenticated startup logs; the final start reused recent updater state. | Capture actual traffic and file creation from a fresh profile; include first login and first inference later. |
| A20 | The service-host candidates form the required default whitelist. | Incomplete docs/static evidence. | Record authenticated traffic; select the actual production hosts and verify no-approval core operation. |
| A21 | Optional enterprise, browser, MCP, and remote-control hosts are separable. | Product features are distinct; actual dependencies unverified. | Attribute requests by operation and identify any core startup dependency. |
| A22 | Proxy variables and Cooper CA work for all native transports. | Existing Cooper contract only. | Verify streaming/model requests, tools, MCP, update metadata, and any gRPC/WebSocket use. |
| A23 | Current loopback exclusions cover the native backend. | Localhost listeners observed. | Trace client addresses, including IPv6; confirm no remote service bypass. |
| A24 | A new session never attaches to a host Antigravity process. | Native backend observed; shared transport behavior not traced. | Inspect process/IPC behavior with a separate host session and both Cooper modes. |
| A25 | Port discovery does not expose an unprotected control endpoint. | Backend listens on random local ports. | Check host binding and access control; verify developer port forwarding remains correct. |
| A26 | X11 is the correct Cooper clipboard mode. | Proposed from current platform support. | Verify actual text/image copy/paste through the Cooper bridge in both modes. |
| A27 | The existing auto-approve alias policy can target `agy`. | Flag exists; current entrypoint assumes key equals command. | Test alias expansion and normal one-shot behavior without rewriting host settings. |
| A28 | Antigravity's own sandbox can run under supported Cooper boundaries. | Documented namespace use; not tested here. | Identify actual syscall/capability requirements; preserve outer isolation and host preferences. |
| A29 | The CLI starts and exits normally in a rendered terminal. | Native `1.2.2` login screen and clean double-Ctrl+C exit observed with foreground timeout. | Repeat in the actual image/VM and supported platforms; check Ctrl+D and authenticated exit too. |
| A30 | Official public CLI implementation source is available. | Not found; the public repository showed docs/examples. | Recheck official links before any source-based claim or clone. Keep SDK and Gemini CLI separate. |
| A31 | Existing custom `antigravity` directories can be identified safely. | Current built-in collision pattern exists for Grok. | Prove a custom directory survives configure/build migration unchanged. |
| A32 | Enabled-tool defaults can be removed without affecting other features. | Current exact-host reconciliation exists; shared Google hosts add complexity. | Test shared ownership, user overrides, migration, and live reload. |
| A33 | The prepared VM profile API remains as inspected. | Source at commit `5e66aa7`. | Re-read after concurrent work; update parser, hook, pins, fixture helpers, and docs together. |
| A34 | Native headless exit zero means the requested proof completed. | Known unsafe assumption from current changelog. | Reject partial/timed-out/denied results and require the expected bounded output. |
| A35 | Current docs accurately describe all state and permission behavior. | Several documented conflicts are listed above. | Prefer selected-release observations; preserve unknown host data during migrations. |

## 16. Research to continue first

The next session must complete a second pass. This ordered list makes the
remaining work explicit and prevents repeated broad research.

| ID | Research task | Required artifact or decision |
| --- | --- | --- |
| R01 | Recheck baseline, release, platform, and official source availability. | Commit/version/source record; updated API and file map; status for A01-A06 and A30. |
| R02 | Repeat the successful native terminal check in the supported Cooper runtimes, without login. | Rendered PTY evidence, clean-exit result, fresh-state startup/download hosts, and helper/cache paths. Resolve A19 and A29 for the support matrix. |
| R03 | Map authenticated account storage and host continuity. | Complete root table; token filename/location metadata; Linux file/keyring and macOS cases; supported cross-boundary design. Resolve A07-A16. |
| R04 | Find historical artifact lookup and freeze build inputs. | Verified old-version record for each supported architecture, failure behavior, and the deterministic build metadata format. Resolve A06 and A18. |
| R05 | Finish the required exact-host whitelist. | Per-host purpose and traffic/source evidence; core flow with no extra approvals; optional-mode host lists. Resolve A20-A23 and A32. |
| R06 | Check local backend, native sandbox, clipboard, and optional helpers. | Transport isolation result; binding/access-control result; clipboard mode; required libraries/downloads. Resolve A24-A28. |
| R07 | Verify the integration in both Cooper modes using the prepared profiles. | Same-account/path/env/version comparison; actual session continuation results; state/cleanup/concurrency evidence. |
| R08 | Reconcile docs, proof, release coverage, and the reusable checklist. | Updated assumptions, honest support matrix, exact commands, all applicable gate logs, Antigravity in the next release matrix, and one reviewed `cooper/dev/adding-an-agent.md` checklist for future harnesses. |

R03 and R05 need an authorized account and later runtime access. This session
did not perform them because the user requested startup without login and
reserved Cooper tests for another session. ARM64, macOS, real VM behavior,
clipboard, native sandbox compatibility, and physical SSD I/O also remain
unmeasured.

If another domain is needed, submit an exact-host `curl` request through the
user's approval proxy and state its purpose. Do not follow a redirect onto a
new host silently. No approval granted in this research implies approval for
an unknown future host.

## 17. Completion conditions for the implementation

- [ ] The second research pass is complete for all core support conditions.
- [ ] Antigravity has one stable Cooper identity and the correct native command.
- [ ] Supported version modes resolve exact verified Linux artifacts.
- [ ] Both execution modes mount complete selected host state at identical paths.
- [ ] Account/keyring/file-storage behavior is verified for each claimed host case.
- [ ] Sessions continue in both directions without a Cooper-owned credential copy.
- [ ] Required domains are included as managed exact defaults and have evidence.
- [ ] Custom endpoints and unrelated destinations retain normal proxy controls.
- [ ] Clipboard, tool execution, ports, cancellation, and runtime isolation work.
- [ ] Images remain at their configured versions between Cooper updates.
- [ ] Existing custom tools, other agent mounts, and user whitelist entries remain valid.
- [ ] Local and prepared selected-agent checks pass; applicable final gates pass.
- [ ] The next actual release runs the full VM gate with Antigravity included.
- [ ] One terse `cooper/dev/adding-an-agent.md` checklist covers all common
  integration steps, has been checked against the Antigravity change set and
  another built-in, and is linked from the project and developer guides.
- [ ] README, requirements, developer docs, proof labels, and this register agree
  with the behavior that was actually verified.

The planning deliverable is complete when this document is written and checked.
The implementation remains separate. Its release claim must be based on the
checks above, not on the limited native startup result from this research.
