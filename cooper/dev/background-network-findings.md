# Antigravity and Grok background requests

Checked on 2026-09-15 on the physical Linux host with Cooper 0.5.1,
Antigravity CLI 1.2.2, Grok 1.0.30 alpha (`04b7ffed98c6`), and Squid 6.12.

## Decision

Keep `antigravity-unleash.goog`, `play.googleapis.com`, and `x.ai` outside the
default whitelist. The observed requests serve feature settings, telemetry,
and release information. Successful model responses do not require approval
of these requests in the sessions checked here.

An approval prompt means that a destination needs permission. It does not
mean that the destination is required for the agent to answer. The current
approval timeout is 20 seconds. A background task that waits for the request
can therefore delay startup until the user denies it or the timer expires.
Pressing `d` denies the selected request now; later requests can prompt again.

This investigation changed no network rules or running sessions. An optional
future deny-domain rule could return a denial at once and reduce repeated
prompts. That would need a separate design for rule scope and precedence.

## Antigravity

The current proxy access log contains these exact requests:

| Host | Observed method and path | Purpose |
| --- | --- | --- |
| `antigravity-unleash.goog` | `GET /api/client/features` | Download feature settings for local evaluation. |
| `antigravity-unleash.goog` | `POST /api/client/register` | Register the feature client. |
| `antigravity-unleash.goog` | `POST /api/client/metrics` | Report feature-use counts. |
| `play.googleapis.com` | `POST /log` | Send Clearcut logging events. |

The Unleash paths match its documented APIs for
[feature settings](https://docs.getunleash.io/api/get-all-client-features),
[client registration](https://docs.getunleash.io/api/register-client-application),
and [usage metrics](https://docs.getunleash.io/api/register-client-metrics).
The native Antigravity log also names Unleash and reports a 403 for metrics.
This host serves both feature settings and metrics; it is not only a
telemetry destination.

The native Antigravity log reports `Clearcut responded with HTTP code: 403`
at the same times as the denied `/log` requests. Google's
[Clearcut logger source](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/telemetry/clearcut-logger/clearcut-logger.ts)
also identifies this endpoint as a logging service. That source belongs to
Gemini CLI, so it does not establish the content of Antigravity's events.
This investigation did not inspect event bodies.

In the current session, initial approvals let feature registration, feature
settings, and Clearcut requests succeed. Later requests received 403 after
20,000 to 20,002 ms. Antigravity continued to use
`daily-cloudcode-pa.googleapis.com:443` through the existing whitelist, and
the user confirmed successful model responses. The earlier
[file-auth acceptance check](antigravity-file-auth-verification.md) also
passed model requests, state restore, and image paste with Unleash denied.

These checks support the default denial. They do not prove that every
Antigravity feature works without remote feature settings. Reassess a
specific feature if a repeatable failure requires that host.

## Grok

The current proxy log records two denied `CONNECT x.ai:443` requests from
the Grok barrel. Each ended after about 20 seconds. It then records four
successful `POST /v1/responses` requests to `cli-chat-proxy.grok.com`.
The model transport already has permission under Cooper's path policy.

The host's `[cli] auto_update` setting is already `false`. To separate
startup tasks, an isolated container used the exact running Grok image,
fresh temporary state, a fake API key, and a local TLS test proxy. Docker
network mode was `none`; no test request could reach an external server.
The test proxy denied release requests and returned temporary server errors
for fake model endpoints so the UI could reach its background startup tasks.

| Test | Requests to `x.ai` |
| --- | --- |
| Default startup | `/cli/stable`, `/cli/changelogs/1.0.30.external.json`, `/cli/changelogs/1.0.30.external.md` |
| `--no-auto-update` | Both changelog paths; no `/cli/stable` check. |
| `--no-auto-update` and `GROK_CHANGELOG_OFFLINE=1` | None during the 12-second observation. |

The default update check also tried the fallback
`storage.googleapis.com/grok-build-public-artifacts/cli/stable` after the
`x.ai/cli/stable` denial. Blocking `x.ai` alone does not disable every
possible update transport.

The two changelog downloads are the strongest explanation for the live
startup prompts, given the host setting and the matching two-request
pattern. The live CONNECT records do not contain a path, so they cannot
establish that mapping on their own.

xAI documents both `--no-auto-update` and the persistent
`[cli] auto_update = false` setting in its
[scripting guide](https://docs.x.ai/build/cli/headless-scripting).
`GROK_CHANGELOG_OFFLINE` was a probe control, not a Cooper setting change.
Cooper must continue to apply the user's host settings without a hidden
behavior override. None of these checks justifies adding `x.ai` to the
default whitelist.

## Separate defect: source address displayed as a port

The screenshots show a barrel IP in the Port field and `-` as Source.
This is an ACL input-format defect before the TUI renders the request.

The 0.5.1 template requests `%DST %SRC`. Squid automatically appends
`%DATA`, which is `-` when the ACL has no arguments. This behavior is in
the [Squid external ACL documentation](https://www.squid-cache.org/Doc/config/external_acl_type/).
An isolated run of the current Squid image produced:

```text
example.invalid 127.0.0.1 -
```

The 0.5.1 listener treats any three-field input as `domain port source_ip`.
A direct probe of the current Go listener confirmed `Port=127.0.0.1` and
`SourceIP=-`, with an `ERR` reply when denied. This reproduces the screenshot.
The inspected destination stayed `example.invalid`; this test did not find
an allow-rule bypass.

A template using `%DST %>rP %SRC` produced the correct destination ports:

```text
example.invalid 443 127.0.0.1 -
example.invalid 80 127.0.0.1 -
```

The corrected template now sends `%DST %>rP %SRC %DATA` explicitly. The
listener accepts this format and the older `domain source -` format. It
removes the empty ACL-data marker before reading fields. It rejects invalid
ports or source addresses before it publishes a request for review.

Regression tests send the observed old and new lines through a real Unix
socket listener. They check ports 80 and 443, IPv4 and IPv6 sources, invalid
fields, and denied responses. A template test checks the exact format tokens.
The physical-host Squid probe above established the wire format; the current
VM's new Docker integration run is tracked in
[the TUI verification report](proxy-tui-verification.md).

## Local evidence

The temporary probe programs and selected request metadata are retained in
`cooper/.test-tmp/network-background-20260915/`. This directory is ignored by
Git. The Grok and Squid test containers removed themselves on exit.
The Go probe used a separate temporary Unix socket. No test used real
credentials or mounted host agent state.
