# Cooper VM development

Start with local tests. They find mount-plan, account, protocol, lifecycle,
relay, template, clipboard, and account-profile errors without Docker or a VM:

```bash
./cooper/test-vm-dev.sh unit > /tmp/cooper-vm-unit.txt 2>&1
```

No arguments also select `unit`. The explicit package list excludes packages
with Docker setup. Docker and QEMU commands are blocked. Registry downloads
are disabled and HTTP proxies reject public requests; the clipboard shim uses a local HTTP server and
`curl`, `jq`, and `bash`. Install the Go toolchain and module dependencies once
before running this offline command. It never clears the Go build cache.

For changes that need a real kernel and Docker boundary, prepare once:

```bash
./cooper/test-vm-dev.sh prepare > /tmp/cooper-vm-dev-prepare.txt 2>&1
./cooper/test-vm-dev.sh smoke > /tmp/cooper-vm-smoke.txt 2>&1
```

`prepare` uses Cooper's production build code with optional languages and
provider agents off. The small image retains mandatory Node, fonts, system
libraries, runtime entrypoints, and clipboard tools. VM startup supplies its
Docker command. It prepares the production guest base and VM helper images,
then exports one selected image. To reuse an existing base, set
`COOPER_VM_PREPARED_BASE` to its normal `vm/assets/schema-1/` file. Cooper checks
its production metadata and full digest before staging it. This is a
read-only input; the development fixture never changes the host base.

Choose the smallest runtime check for the change:

| Command after `./cooper/test-vm-dev.sh` | New VMs | Scope |
| --- | ---: | --- |
| `smoke` | 1 | Boundary, mounts, Docker, local HTTPS policy, bridge, live ports, clipboard, exec, warm reuse, stop |
| `mounts` | 1 | Create/remove/replace a nested bind file, timezone content, ownership, virtiofs logs |
| `lifecycle restart` | 2 | Explicit restart, token rotation, new private state |
| `lifecycle resources` | 2 | Rejected CLI request preserves the token and disk; valid resource change recreates the VM |
| `lifecycle relay` | 2 | Lost relay, unhealthy-start refusal, recovery |
| `lifecycle agent` | 2 | Lost agent, health change, recovery |
| `prepare-agent codex` | 0, or 1 cold base | Prepare only the selected real agent and its required base/helpers |
| `parity codex` | 1 | Real CLI/VM version, account, same paths, selected state, isolation, writes |
| `profiles codex` | 2 | Saved profile in Docker and VM, complete roots, credential isolation, restart, status, cleanup |

The agent choices are `claude`, `copilot`, `codex`, `opencode`, `grok`, and
`antigravity`. `profiles codex` and `profiles antigravity` use the assets from
the matching `prepare-agent` command. They test the shared profile integration
with fabricated accounts; local tests cover every root catalog and identity adapter. The restart check requires the second VM and
image import.

Preparation uses explicit version pins in `internal/vmdev/config.go`.
Change a pin to test a new version, then prepare that agent again. The fixture
uses empty test-owned state; it never needs host provider credentials.

Runtime commands cannot build, pull, import, or export host images. A stale
or missing input tells you which preparation command to run. They still load
one archive into each new guest's private Docker store; reusing a host archive
does not remove this production requirement. Small guest Docker builds are
part of smoke, using the already loaded image and a local HTTPS fixture.

When a profile runs inside Cooper, its two HTTPS test origins stay on the
fixture's private Docker network. The test proxy routes only those two local
DNS aliases directly. It keeps the parent route for other destinations. This
checks the child VM's proxy boundary; the complete parent proxy chain remains
a release check. The origin and proxy paths are checked before a VM starts.
For ports, a nested Cooper proxy uses the parent's container port. Nested
development tests therefore use the same port number on both sides. The
physical-host release check also tests different host and container ports.

The full self-host build and two-level VM test stay in the release gate:

```bash
timeout 90m ./cooper/test-vm.sh > /tmp/cooper-vm-gate.txt 2>&1
```

Run it only before a Cooper release, from the physical Linux host. It is not
a routine development task or a way to obtain a benchmark. Development
profiles can run from a physical host or from a depth-one Cooper VM. In the
latter case, they check a depth-two workload with no KVM device. There is no
`all` or `nested` development mode that silently starts the full gate.

## Cache and cleanup

Prepared manifests and archives live below
`cooper/.test-tmp/vm-dev-cache/`. Cache identity includes the Docker daemon and
UID. A manifest records dirty and new source files, embedded payloads, module
files, fixture code, binary digest, platform, account, helper and agent image
IDs, and base metadata. Preparation checks source before and after its work.
A run checks source before compilation and after its assertions.

One exclusive lease covers preparation, a runtime test, and cleanup. A second
command fails after five seconds instead of changing a live cache. The test
home is stable for the build-account contract, but contains only fixture
state and is cleared after a run. Other runtime files use unique run IDs.
VM files and archives share the workspace filesystem so staging can use hard
links; short checked `/tmp` aliases keep Unix socket paths within their limit.
No cleanup recursively changes ownership or modes of a linked archive.

Each command prints a unique JSON report and complete log path under `/tmp`.
Runtime logs remain under `cooper/.test-tmp/vm-dev-runs/`. Reports include actual
supervisor identities and image-load counts, archive size, staging method,
overlay allocation, resource settings, cache state, and measured times.
`vm_starts` counts workload VMs. Preparation reports its separate guest in
`preparation_vm_starts`: zero for a validated base, one after successful cold
preparation, and unknown if cold preparation fails. A cold base was not built
again solely to measure this change.
After interruption, use:

```bash
./cooper/test-vm-dev.sh clean > /tmp/cooper-vm-dev-clean.txt 2>&1
```

This takes the same lease, checks the account/daemon/run records, then removes
only recorded objects with matching IDs and types. A replaced resource or
invalid record causes refusal. Logs remain. `clean-cache` also removes owned
prepared image references and cache files when no run is active. It never
runs a Docker prune or touches host agent state. Ordinary runs do not need
cache cleanup.

Archive bytes and qcow2 allocation are logical work, not SSD or NAND writes.
The report marks physical SSD writes unavailable. A real physical-host device
window and complete elapsed time remain evidence for the next release.
Do not add guest counters to host counters. Do not assume `/tmp` is RAM.
RAM mode is not provided: this VM's `/dev/shm` is private to its container and
cannot be mounted by its Docker daemon, and physical-host swap is not visible.

## Host setup

Run the setup script as your normal user on a Linux x86-64 host:

```bash
./cooper/dev/setup.sh
./cooper/dev/setup.sh --check
```

It uses `sudo` only to configure/load the CPU KVM module and add your account
to the `kvm` group. Log out and in if group membership changes. Nested KVM is
required for the full release gate. The small development profiles check
actual device access at their current VM depth; they do not require a named
`kvm` group inside a container that already has the correct numeric group.

Cooper runs QEMU and cloud tools in pinned containers and includes a verified
static `virtiofsd`. The host does not need libvirt, TAP/TUN, OVMF, a GUI, or a
virtual host network. The guest has no NIC, and its approved traffic passes
through the Cooper relay and proxy. Use `cooper vm doctor` for runtime and
asset diagnostics.

The required full Go, shell E2E, and Docker-build gates remain unchanged.

Run local profile tests and real runtime checks in sequence. They use the
production per-UID state lock. A VM startup can therefore make a concurrent
profile test report that state is busy. For the full Go suite, use
`GOFLAGS=-p=1 go test -C ./cooper ./...` from the repository root when packages
would otherwise spend their test time limit waiting for the shared Docker
test lock. This changes package scheduling, not test coverage.

The Docker-build gate's mirror mode requires an actual host executable for
each enabled harness. A `cooper vm codex` session normally provides only
Codex. Put real test versions of the other harnesses in a private tools
directory and add that directory to `PATH` for the gate. A version-output
stub does not verify mirror behavior.

## Validation evidence

Initial validation ran inside a depth-one Cooper VM. All runtime profiles
passed, including each lifecycle case and all five pinned agent versions.
The local suite, full Go suite, Docker-build `all` gate, 345 shell E2E checks,
16 permission-hook tests, and development build also passed.

| Development check | Elapsed seconds | VM starts / guest imports | Host exports |
| --- | ---: | ---: | ---: |
| Local unit suite | 5.418 | 0 / 0 | 0 |
| Prepared smoke | 86.115 | 1 / 1 | 0 |
| Mount refresh | 85.262 | 1 / 1 | 0 |
| Each lifecycle case | 145.632–149.989 | 2 / 2 | 0 |
| Each selected-agent parity check | 91.501–104.373 | 1 / 1 | 0 |

These are observations, not time limits. Some profiles overlapped other
required gates. The small archive was 789,486,592 bytes, about 753 MiB.
All runtime profiles used hard links. Smoke reported 1,692,106,752 allocated
overlay bytes before stop; this does not measure physical SSD writes.

Real failure probes confirmed that missing or stale cache inputs fail before
a VM starts. A cleanup command could not acquire a live run's lease. Invalid
run records and changed Docker object IDs prevented removal. Exact-ID cleanup
removed only the recorded test resources and preserved shared archive bytes
and modes. Each agent preparation preserved the other agents' manifests and
image references. The original session container remained running.

The small tests also reproduced three product errors: custom-only builds
omitted clipboard setup, port reload required an absent external `kill`
program, and VM exec cancellation did not interrupt an established stream.
The fixes use the runtime clipboard setting, Docker's HUP signal, and
connection closure on cancellation. The exec check proves that the client
stops waiting; it does not prove that every remote child process is killed.

Development and release tests share `runVMSmokeChecks`, `mountRefreshScript`,
and the selected-agent state helpers in `internal/vme2e`. The release matrix
checks every built-in agent. Removing its duplicate home-path invocation
reduced the source inventory from 18 starts, 18 imports, and 14 exports to
13 starts, 13 imports, and nine exports. A cold base can add one preparation
VM. Complete runtime confirmation of these counts remains a next-release
check; no full gate was run solely to obtain a baseline.

The 30 GB outer Docker filesystem filled during initial preparation and
Docker-build validation, although the separate workspace had free space.
Exact-ID removal of failed build containers and completed test images freed
space. The complete gate then passed. This is why workspace free space alone
does not establish that preparation will fit in the Docker store.

## Physical-host release measurements

At the next required release, record the source and binary, machine, Docker
storage path and filesystem, cache state, all phases, complete elapsed time,
and a before/after device counter window. Use one physical leaf-device layer;
do not sum device-mapper, partition, guest, and whole-device counters. Check
for device replacement or counter reset, and state whether unrelated work
and delayed writeback are included.

No complete physical-host duration or SSD write total was measured during
development. Do not claim a percentage reduction in SSD wear from archive
sizes or source counts. Do not use a global cache drop, swap change, SSD
benchmark, SMART self-test, or trim operation to obtain these measurements.

Linux block `stat` counts write sectors in 512-byte units. These counters
measure device I/O, not SSD-internal NAND writes. Tmpfs can use swap. See
[Linux block statistics](https://docs.kernel.org/block/stat.html) and
[Linux tmpfs documentation](https://docs.kernel.org/filesystems/tmpfs.html).

For new harnesses, follow [Add a built-in agent](adding-an-agent.md). Antigravity
parity also runs its installed native client against a local model fixture and
checks conversation restoration, without using an external account.
