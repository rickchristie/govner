# Antigravity keyring access findings

Recorded on 2026-09-14 during development inside a depth-one Cooper VM.
This is an investigation result. Host keyring sharing is not implemented.

The user later authorized implementation of the host file-auth wrapper as an
active goal. That implementation and its tests are now in the working tree;
the proposal sections below retain the investigation history. See the
[native guide](../docs/antigravity.md) and
[file-auth verification](antigravity-file-auth-verification.md) for current
behavior and remaining acceptance work.

## Problem

Cooper shares the selected agent's complete state directories. An OAuth
credential stored in an OS keyring is held by a separate service. Mounting
`~/.gemini` does not give Antigravity access to that service.

Forwarding the complete host session bus would grant access beyond the selected
agent state. Forwarding only `org.freedesktop.secrets` would still expose a
service that can read and change unrelated keyring entries. The Secret Service
specification does not require application access controls. Lookup attributes
select an item; they do not prove which application made the request.

Sources: [access controls](https://specifications.freedesktop.org/secret-service/latest/ch10.html),
[lookup attributes](https://specifications.freedesktop.org/secret-service/latest/lookup-attributes.html),
and [item operations](https://specifications.freedesktop.org/secret-service/latest/org.freedesktop.Secret.Item.html).

## Confirmed results

All probes used fabricated credentials, a private D-Bus session, and a private
GNOME Keyring 42.1 service. Docker test containers had no network connection,
all capabilities removed, and `no-new-privileges`. Native processes ran as UID
1000. The physical host bus, keyring, and account credentials were not exposed.

| Check | Result |
| --- | --- |
| Private keyring control | A separate `secret-tool` process could store and read the fabricated entry. |
| Access to an unrelated entry | Another process with a separate `HOME` and the same bus could read, replace, and delete the unrelated entry. The selected Antigravity entry remained intact. |
| Native Antigravity 1.2.2 in Docker | With `/.dockerenv` present, the client selected file storage despite the available private keyring. No native Secret Service calls appeared in the bus trace. |
| Container detection control | Removing only `/.dockerenv` in a disposable test container caused a fresh native process to query the private keyring. Cooper runtime code was not changed. |
| Native credential lookup | The client searched for `service=gemini` and `username=antigravity`, opened a `plain` Secret Service session, and called `GetSecret`. |
| Native credential parsing | With a fabricated OAuth record at that key, the client accepted the local saved identity. Its subsequent online eligibility request failed because the test container had no network. This does not prove a valid Google login. |

The native archive matched the retained SHA-512 digest for Linux amd64 release
1.2.2 in `internal/antigravity/release.go`. The binary contains the
`zalando/go-keyring` client symbols. Its observed lookup follows that client's
[Secret Service implementation](https://github.com/zalando/go-keyring/blob/master/keyring_unix.go).

Local native probe files remain under
`cooper/.test-tmp/keyring-probe-20260914/`. The access test report is
`/tmp/cooper-keyring-access-limits.txt`. The reports contain only fabricated
account data. These files are development evidence, not release artifacts.

## Selecting file storage on a Linux host

A second native probe placed different fabricated accounts in the private
keyring and token file. It removed the Docker marker only inside the disposable
test container so that normal container detection could not decide the result.
A private bus was available both through its environment address and through
`/run/user/1000/bus`. Each native invocation had a new private home and process.

| Native process environment | Selected account |
| --- | --- |
| Normal private bus address | Keyring account |
| `GEMINI_FORCE_FILE_STORAGE=true`, normal bus address | Keyring account |
| `DBUS_SESSION_BUS_ADDRESS` unset, default bus socket present | Keyring account |
| Bus address points to an absent Unix socket | File account |
| `DBUS_SESSION_BUS_ADDRESS=unix:path=/dev/null` | File account |
| Normal bus address restored in a later invocation | Keyring account |

All six controls passed with native 1.2.2. The token files were unchanged. The
client logged a keyring connection error followed by a file fallback for the
unavailable addresses. Its later auth-provider label still said `keyring`, so
the test compared the distinct fabricated account identities instead of
trusting that label. Online eligibility failed as expected with no network;
these results establish credential selection, not a working provider session.

The commonly reported `GEMINI_FORCE_FILE_STORAGE` variable did not change this
native release's selection. Unsetting the bus variable was also insufficient.
The `/dev/null` override is a tested Linux workaround for this release, not an
Antigravity storage setting or a verified migration procedure. It applies to
the process and its children; it does not stop or change the desktop keyring.

Cooper could offer a host launcher that applies this override after the user
selects shared file authentication. The user would sign in through that
launcher to create the native token file, and use that launch mode for later
host sessions. This avoids granting the workload access to the host keyring.
It would not automatically export or delete an existing keyring credential.

Before adding this option, verify native login writes, refresh, logout,
conversation continuity, profile selection, and desktop integration. Child
processes inherit the D-Bus override, which can affect browser opening or
other desktop service calls. File storage also places the OAuth record in an
ordinary file rather than the host's protected keyring. Configuration must
explain that change, and a build must not silently change the host login mode.
Check the effective mode again at launch: a build-time observation can become
stale after another login or CLI update. An available bus alone cannot prove
which real account is active.

The script, six-case JSON report, and native logs are in the same local probe
directory under `storage_options.py`, `storage-options.json`, and
`storage-*.log`. The complete outer log is
`/tmp/cooper-antigravity-storage-options.txt`.

The user also tested the physical host: ordinary `agy` restored the login,
while `DBUS_SESSION_BUS_ADDRESS=unix:path=/dev/null agy` showed the sign-in
screen. This confirms that the file fallback did not restore that host login.
It does not establish that logout from the existing keyring session is needed
before a separate file login, or that a later ordinary launch will use files.
The user has not yet reported a completed file login and restart check.

## Host wrapper proposal

The user proposed that `cooper build` create an `agy` wrapper that always
selects file authentication. Login, logout, and account changes would occur
on the host, with Cooper save/load for account profiles. Automatic token
refresh must still write state during a workload session.

The wrapper must apply to the host's normal `agy` command. A wrapper only
inside the image would leave the host credential in its keyring. On Linux,
the tested wrapper exports `DBUS_SESSION_BUS_ADDRESS=unix:path=/dev/null`
and uses `exec` with the original absolute executable path and all arguments.
Keep the wrapper separate from the upstream executable and make it resolve
first on `PATH`. The upstream installer uses `~/.local/bin/agy` and can modify
shell aliases and `PATH`; installation and upgrade checks must account for
those operations. See the
[native installation instructions](https://antigravity.google/docs/cli/install).

A further private probe ran a real shell wrapper twice with native 1.2.2,
different fabricated file and keyring accounts, and an available private bus.
Both wrapped processes selected the file account. A direct call to the
original executable then selected the keyring account. Both saved records
were unchanged, and the parent process could still use the private bus.
The probe used no host mounts or network. It removed the Docker marker only
inside the disposable test container to prevent container detection from
deciding the result. Native online eligibility failed with no network, as in
the earlier tests. The report is `/tmp/cooper-antigravity-wrapper-probe.txt`.

With file authentication, ordinary sessions use the same host state files
through complete writable directory mounts. A token refresh therefore updates
that shared state without a second credential store or synchronization service.
A named session writes the selected saved profile's state. Host `cooper load`
later restores that state to the active host roots, using the existing profile
conflict and recovery rules. These are different host directories; a saved
profile update does not immediately replace the active host account.

The current Antigravity profile reader rejects OAuth files whenever it sees
a host session bus, including an explicit unavailable bus address. Thus the
wrapper alone cannot make desktop profile save/load work. The reader needs
an explicit, checked contract for the managed file mode while retaining the
existing identity and external-credential checks. The directory catalog and
profile copy format do not need a new keyring store.

The initial host file login is still needed; the wrapper does not migrate an
existing keyring credential. Verify login writes, a fresh shell resolving the
wrapper, browser and clipboard behavior, token refresh, and profile save/load
before implementation is accepted. This Linux D-Bus workaround is not a
verified macOS Keychain control. No host wrapper or profile rule was changed
during this investigation.

## Private runtime keyring

A separate runtime keyring can contain only the selected agent's credentials.
The host must select the exact supported entries before transfer. An
Antigravity runtime must not receive Codex entries, or the reverse. Do not
copy the complete host keyring database, mount its bus, or let the guest choose
arbitrary host item names. Credentials must not enter image layers, command
arguments, or logs.

A Codex 0.154.0 probe used a fresh private GNOME Keyring with no host mounts
and no network. With `cli_auth_credentials_store="keyring"`, native Codex
saved a fabricated API key and a second native process restored its login
status. The keyring contained exactly one item, with service `Codex Auth`;
`auth.json` was absent. A second private home with the same available keyring
used Codex's default storage setting and created `auth.json` instead. All
assertions passed. The report is `/tmp/cooper-codex-private-keyring.txt`.
This verifies local storage, not a real provider login, host export, OAuth
refresh, or a complete Cooper implementation.

The current Codex sample configuration also declares `file` as the default
for `cli_auth_credentials_store`. Its separate `mcp_oauth_credentials_store`
setting defaults to `auto`. Main login storage does not select storage for
every MCP credential. See the
[Codex sample configuration](https://learn.chatgpt.com/docs/config-file/config-sample)
and [credential storage controls](https://learn.chatgpt.com/docs/auth#credential-storage).

A copy at startup needs no live host keyring bridge. It does create separate
credential state. A refresh, logout, or account change inside the runtime can
leave the host copy out of date. OAuth permits a server to replace a refresh
token and revoke the old one; provider-specific rotation was not tested here.
See [OAuth token refresh](https://datatracker.ietf.org/doc/html/rfc6749#section-6).
Cooper's host-to-runtime continuity requirement therefore still needs a defined
way to synchronize changes, including concurrent sessions and interrupted
runtimes. This can use a restricted transfer mechanism rather than a general
Secret Service bridge, but it is additional implementation and test work.

Antigravity's observed Docker detection remains a separate limit: populating
a private keyring does not make native 1.2.2 use it in the tested container.
Cooper VM also runs the selected agent in Docker. This design needs either a
verified way to select keyring storage there or a separate, supported way to
transfer the selected credential to Antigravity's file store. Neither path is
implemented. Do not use removal of the Docker marker as a product workaround.

## Required access limits

A host broker would be a new security boundary. A D-Bus socket forwarder or a
filter that permits the whole Secret Service interface cannot provide the
required item restriction.

Before implementation, the broker design must specify:

- A grant for one selected agent, account mapping, and runtime. The host must
  choose the credential. Guest requests must not select arbitrary services,
  usernames, item paths, or other saved accounts.
- The exact read, refresh, and logout operations needed for that credential.
  These limits must apply to changes and deletion as well as reads. No general
  search, collection management, or access to unrelated items is permitted.
- Authentication for each runtime, grant expiry or revocation, and protection
  against credential or account changes while a grant is active. A matching
  OS UID alone does not restrict access to the selected item.
- Transport in both execution modes through an explicit restricted endpoint.
  The guest must not receive the host session bus, a route to the host LAN,
  or the host container-runtime socket.
- Negative tests that use a malicious client to request other items, change
  request fields, reuse a revoked grant, and cross account boundaries.

Any process in an authorized workload, including guest root, could use a raw
credential supplied to that workload. A broker can restrict which credential
crosses the boundary. It cannot make that credential available only to `agy`
inside a workload where the agent can execute arbitrary commands.

## Open conditions

The native Docker detection rule is separate from broker access control. A
broker alone cannot make native 1.2.2 select keyring storage in the tested Docker
environment. The marker removal was a diagnostic control, not a product fix.
The public settings page and native `--help` did not provide a storage override.
This does not prove that no undocumented control exists.

Host keyring continuity, native keyring refresh and logout, locked keyrings,
saved account behavior, macOS Keychain, and a complete broker implementation
remain unverified. Do not change the current profile rejection or mark host
acceptance complete on the basis of these probes.

The local VM unit suite passed with the cached Go 1.25.0 toolchain. The full Go
suite was interrupted when work paused for the access review; it has no new
pass result. No full VM release gate was run for this investigation.
