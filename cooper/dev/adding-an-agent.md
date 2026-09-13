# Add a built-in agent

Use the existing shared boundaries. A new agent must not add separate Docker,
VM, or profile mount implementations. Record research evidence and open
account tests before claiming support.

1. Identify the actual CLI product, executable, official release channel,
   version command, supported architectures, and license. Download a reviewed
   artifact and check its digest. Run it in isolated state without credentials.
   An SDK or desktop product with a similar name is not evidence for the CLI.
2. Add catalog metadata in `internal/aitool`. The Cooper name can differ from
   the executable. Use `aitool.Executable` or the host-version command wherever
   code runs the program. Check configure, launch aliases, proof, UI fixtures,
   and test matrices for fixed lists.
3. Resolve exact build inputs before rendering. Keep network clients bounded
   and injectable. Pin artifacts and helper dependencies. Reject unavailable
   versions instead of silently selecting Latest. Keep image binaries outside
   state roots. Protect new built-in names from custom-directory collisions.
4. Observe state reads and writes with an empty, private home and native
   file tracing. Repeat for supported auth modes, custom root variables,
   sessions, settings, plugins, and shutdown. Add complete roots and their
   effective path rules to `internal/workload/agentpaths.go`. Include absent
   roots in profile replacement and protect all known roots during cleanup.
   Check direct paths, symlink aliases, the whole-home boundary, and overlaps.
5. Add selected-agent credential and provider selectors to `internal/auth`
   and `internal/profileauth/environment.go`. Preserve explicit modes and
   endpoints. Capture set and unset values for profiles. Do not put secrets
   in images, argv, logs, or generated templates. Check shell startup files
   cannot replace the selected runtime credentials or helper paths.
6. Implement a bounded local identity adapter. Verify native credential
   formats. Use stable account and billing scope; do not identify an account
   by email, an access-token hash, or the last-loaded marker. Unsupported
   keyrings and external helpers must fail closed with recovery. Extend known
   host-writer detection, including products that share the same root.
7. Reuse profile generations, locks, journals, conflict rules, and selection.
   Test first `Default`, pending login, Default/Work/Default, outgoing saves,
   token refresh, changed account scope, unknown identity, copied-state
   identity, and captured credentials. Saved profiles change sources only.
8. Observe required network hosts in native runs. Add exact managed defaults
   with ownership-aware migration tests. A hostname in a binary or a batch
   research approval is not sufficient reason for a default rule. Verify the
   normal flow through Cooper's proxy without allowing an unrelated domain.
9. Run local and native fixtures before costly VM checks. Add the agent to
   test pins, image matrices, finite VM commands, and hook cases. Run VM unit
   checks first; prepare one selected image; run its parity and profile cases
   with cached inputs. Runtime tests cannot rebuild or export images. Preserve
   the other agents and all user-owned data during test cleanup.
10. Run the required full Go, shell E2E, and Docker-build suites. Run the full
    VM gate only before release. Inspect deterministic TUI screenshots for
    material UI changes. Document supported auth forms and remaining limits.
    Real login, refresh, model access, and conversation continuation are final
    account tests; local model fixtures do not replace them.
