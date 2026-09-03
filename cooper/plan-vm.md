# Plan: Secure Cooper VM Runtime and Self-Hosting

## Status

This document is the implementation contract for Cooper VM support.

It is a handoff plan. It is not an implementation. The implementor must keep
the decisions and acceptance criteria in this document unless new test evidence
shows that one decision is not valid. Record that evidence before a design
change.

The first supported platform is Linux on x86-64 with KVM. macOS, Windows, and
ARM64 are not part of this implementation.

## Required Outcome

Add this command:

```text
cooper vm <agent>
```

It must give the same agent experience as:

```text
cooper cli <agent>
```

The same experience includes:

- The same current workspace at the same absolute path.
- The same selected agent image and tool version.
- The same complete selected-agent host state mounts.
- The same environment policy and protected variables.
- The same Cooper proxy and domain approval policy.
- The same execution bridge, clipboard bridge, and port-forward rules.
- The same interactive shell and `-c` one-shot command behavior.
- The same TUI lifecycle controls and health information.

The execution boundary is the only functional difference:

- `cooper cli` runs the agent image as a host Docker barrel.
- `cooper vm` starts a real KVM virtual machine. The VM has its own Docker
  daemon. The same agent image runs as a container in that VM.

The host Docker socket must never enter the VM. The VM must not have a virtual
network interface. Guest root must not be able to add a direct route to the
internet or the host LAN.

## Primary Acceptance Case: Cooper Develops Cooper VM

Self-hosting is a required release case, not a later optimization.

A coding agent in an outer Cooper VM must be able to:

1. Edit the Cooper source in the mounted workspace.
2. Build the Cooper binary.
3. Run the Cooper Go tests.
4. Build Cooper Docker images with the outer VM Docker daemon.
5. Run the Docker-backed Cooper tests.
6. Start one inner `cooper vm` session.
7. Run a command in the inner agent container.
8. Use the nested proxy chain with no direct network route at either VM level.

The supported hierarchy is:

```text
physical host, depth 0
├── host Cooper proxy
└── outer Cooper VM, depth 1
    ├── outer agent container
    ├── outer guest Docker daemon
    ├── nested Cooper proxy, when the agent tests Cooper
    └── inner Cooper VM, depth 2
        └── inner agent container
```

The required network path is:

```text
inner process
  -> inner guest relay
  -> inner Cooper proxy
  -> outer guest relay
  -> physical-host Cooper proxy
  -> approved internet destination
```

Cooper supports two managed VM levels. At depth 2, hide `vmx` or `svm` from
the inner guest and do not expose `/dev/kvm`. The CLI must reject a request for
depth 3. These CPU and device settings are the hard limit for the managed
depth-2 VM. The depth environment value is only an early error aid because
guest root can change environment values.

This depth limit is not a security boundary against root in the outer guest.
The outer guest receives `/dev/kvm` so it can develop Cooper. Root there can
start a VMM without Cooper and can choose its own virtual CPU settings. The
outer supervisor resource limits and no-NIC boundary remain the enforceable
host controls. State this residual risk in the security document.

## Non-Negotiable Security Rules

1. QEMU must use KVM and run as the invoking, unprivileged host user.
2. The VM must have `-nic none`. Do not add TAP, SLIRP, bridge, macvtap, or a
   second optional NIC.
3. Do not pass `/dev/net/tun`, a host network namespace, a host Docker socket,
   a containerd socket, or a Podman socket into the guest.
4. The QEMU supervisor must have no Docker network.
5. The QEMU supervisor can receive only `/dev/kvm`. Do not give it privileged
   mode or other host devices.
6. The component that has selected host file mounts must not have a network.
7. The component that has a proxy-only network must not have selected host file
   mounts.
8. A small relay outside the guest must map protocol service names to exact
   Cooper proxy ports. Guest data must never supply a host name, IP address,
   Unix path, or arbitrary destination port.
9. The guest can reach the internet only through the running host Cooper Squid
   proxy and its ACL helper.
10. The guest can reach a host bridge route or host port only through an
    enabled Cooper rule.
11. Only the selected agent's complete host state roots can enter a VM.
12. Host agent state is read-write and host-owned. Cooper must not copy it,
    split it, migrate it, or delete it.
13. Keep the `.git/hooks` overlay read-only in both runtimes.
14. Keep transient Grok leader transport in the per-runtime `/tmp`. Do not let
    a VM attach to a host Grok process.
15. Reject direct and symlink-resolved overlap between any host-owned agent
    state root and every Cooper-owned deletion root.
16. The agent image binary must stay outside its mounted state root.
17. Secrets must not enter Docker arguments, image layers, labels, manifests,
    or logs.
18. Every generated file, socket, image archive, and disk must have an explicit
    owner, mode, lifecycle, and cleanup rule.
19. Runtime failure must close access. A failed relay, failed proxy, failed
    guest agent, or failed policy reload must not enable a direct path.
20. `cooper down` and `cooper cleanup` must stop all VM processes before they
    remove VM runtime files.
21. Enforce CPU, memory, process, disk, stream, and connection bounds outside
    the guest where possible. Guest root must not be able to exceed the outer
    supervisor resource envelope.

## Explicit Non-Goals

Do not add these items in this change:

- macOS support.
- Windows support.
- ARM64 support.
- A full guest desktop.
- SPICE, VNC, GPU pass-through, USB pass-through, audio pass-through, or host
  X11 socket pass-through.
- Live migration, suspend, resume, or VM snapshots.
- More than two VM levels.
- A host Docker socket in the VM.
- A direct guest network for faster downloads.
- A shared state option that mounts all agent roots.
- Persistent guest operating-system changes between `cooper up` sessions.
- libvirt or a privileged system service.

The existing headless Xvfb and clipboard behavior in the selected agent image
remain in scope. A later GUI design can use a private Unix display socket, but
it must have a separate threat review.

## Verified Evidence Before Planning

The following checks ran on `dreambox-mini` on 2026-09-02 and 2026-09-03.
These are design inputs, not assumptions.

### Host Capability

- Ubuntu 22.04.5 and Linux 6.8 expose AMD-V.
- `kvm_amd` is loaded.
- `/sys/module/kvm_amd/parameters/nested` is `1`.
- The active user can open `/dev/kvm` through group `kvm`.
- The host has 24 logical CPUs, approximately 26 GiB of available memory, and
  more than 360 GiB of free workspace storage during the check.
- `./cooper/dev/setup.sh --check` passed with no warnings after login refresh.

### No-NIC VM and virtiofs

A pinned Ubuntu 24.04 cloud image booted under KVM with `-nic none`.

The guest reported:

```text
INTERFACES=lo
VIRTIOFS=pass
NETWORK=pass
VIRTIO_SERIAL=pass
PROBE_PASS
```

The guest read a host file, wrote an approved read-write export, and could not
write a read-only export. It had no IPv4 default route.

### Docker Inside the VM

Docker 29.7.2 ran inside the no-NIC guest. It loaded a host-provided Alpine
archive and ran a nested container. The guest and nested container had no
direct external route.

```text
NESTED_DOCKER=pass
NESTED_DIRECT_EGRESS=blocked
DOCKER_PROBE_PASS
```

### Guest Docker Bridge to the Relay

An internal Docker network at `172.30.0.0/24` reached a relay on its explicit
gateway `172.30.0.1`. The same container could not reach `1.1.1.1`.

Docker's `host-gateway` alias did not work for this network. It selected
`172.17.0.1`, which was outside the internal network route.

```text
INTERNAL_BRIDGE_GATEWAY=pass
HOST_GATEWAY_MAGIC=fail
NESTED_DIRECT_EGRESS=blocked
NETWORK_PROBE_PASS
```

The implementation must use the explicit Cooper control-network gateway. It
must not use `host-gateway` for the agent control network.

### Nested KVM

The outer guest received the `svm` flag, loaded `kvm_amd`, opened its virtual
`/dev/kvm`, created an inner VM and virtual CPU, and ran that CPU until an I/O
exit and a halt exit.

```text
KVM_API=12 INNER_VCPU=pass
OUTER_CPU_SVM=pass
OUTER_KVM_DEVICE=pass
OUTER_NETWORK_ISOLATION=pass
KVM_PROBE_PASS
```

This proves nested KVM on this host. Checking only the host module setting
would not have proved it.

### Containerized QEMU Supervisor

A temporary Ubuntu 24.04 supervisor image ran QEMU 8.2 and Rust `virtiofsd`
1.10 as UID 1000 with:

- `--network none`.
- `--cap-drop ALL`.
- `no-new-privileges`.
- `/dev/kvm` as its only device.
- CPU, memory, and process limits.

The supervisor booted the no-NIC guest, mounted virtiofs, and passed the nested
KVM virtual-CPU test. The temporary image size was approximately 188 MiB.

This result selects a containerized QEMU supervisor instead of host QEMU
processes. It gives Cooper a fixed QEMU user space and normal Docker lifecycle
control without weakening the VM boundary.

### Full Self-Hosting Boundary

Docker inside an outer no-NIC VM loaded the supervisor image and started an
inner QEMU container. The inner VM also had no NIC. A file moved from the
physical host through outer virtiofs and inner virtiofs, and then back to the
physical host.

```text
OUTER_NESTED_DOCKER=pass
OUTER_NESTED_KVM=pass
VIRTIOFS_OVER_VIRTIOFS=pass
SELF_HOST_DIRECT_EGRESS=blocked
SELF_HOST_PROBE_PASS
```

The inner VM reported:

```text
INNER_INTERFACES=lo
INNER_DIRECT_EGRESS=blocked
INNER_NESTED_KVM=blocked
INNER_VIRTIOFS_READ=pass
INNER_VIRTIOFS_WRITE=pass
INNER_FS_PROBE_PASS
```

The inner check also proved that `-cpu host,-svm` removed `svm` from
`/proc/cpuinfo` and that `/dev/kvm` was absent. A repeat run used fresh logs,
fresh cloud-init instance IDs, and fresh overlays. This rule is important:
append-only probe logs can let an old success marker hide a later failure.

This proves the file-system, nested-KVM, and managed depth-limit paths that the
self-hosting acceptance test will use.

### Docker Build Proxy Precedence

A local Dockerfile test used an inherited `HTTP_PROXY` value and an explicit
predefined `--build-arg HTTP_PROXY=...`. The build step received the predefined
build argument, with and without an `ARG HTTP_PROXY` line.

Do not declare the predefined proxy argument in generated Dockerfiles. Docker
then keeps it out of image history and cache keys. The nested Cooper build path
must add explicit predefined proxy arguments to every Docker build command.

### Base-Image Boot Delay

The unmodified Ubuntu cloud image waited approximately two minutes for
`systemd-networkd-wait-online` because the VM correctly had no NIC. This delay
is not acceptable at normal runtime. The prepared Cooper guest base must mask
the wait-online service and remove other network-dependent startup work.

## Architecture Decision

Use four boundaries:

```text
Host Docker daemon
│
├── Cooper proxy container
│   ├── external network
│   └── one private internal network for each VM relay
│
├── VM relay container
│   ├── private internal network only
│   ├── no selected host data mounts
│   └── one private Unix socket to the supervisor
│
└── VM supervisor container
    ├── network=none
    ├── selected host data mounts
    ├── /dev/kvm
    ├── QEMU
    ├── virtiofsd
    └── guest VM with -nic none
        ├── Cooper guest agent
        ├── guest Docker daemon
        ├── local proxy/bridge relay
        └── selected agent container
            └── guest Docker socket
```

The QEMU supervisor is a Docker container, but the agent execution boundary is
a KVM VM. Host Docker is only the trusted process supervisor for QEMU and the
small relay. The host Docker socket never enters the VM.

### Why This Design Is Selected

- It passed the no-NIC, rootless virtiofs, nested Docker, nested KVM, and
  virtiofs-over-virtiofs probes.
- It pins QEMU and `virtiofsd` independently from host Ubuntu packages.
- Docker already gives Cooper names, labels, logs, health checks, limits,
  cleanup, and TUI statistics.
- The supervisor can have host file mounts with no network.
- The relay can have a proxy-only network with no host file mounts.
- `docker exec` gives the host a local control entry point without a new host
  daemon or public socket.
- The same design works when an outer VM's Docker daemon becomes the host for
  an inner supervisor.

### Rejected Designs

#### Mount the host Docker socket

Reject this. Docker socket access gives the agent effective host root access.
It defeats the reason for `cooper vm`.

#### Docker-in-Docker without a VM

Reject this. It gives Docker development, but it keeps the untrusted agent on
the host kernel.

#### Direct host QEMU processes

The probe worked, but do not select it. Ubuntu 22.04 has an old legacy
`virtiofsd` that requires root. Host package versions also differ by Ubuntu
release. A supervisor image is smaller in design scope and more reproducible.

#### libvirt

Reject this for the first implementation. It adds a host daemon, policy files,
and another privileged API. Cooper does not need its network or storage model.

#### TAP, SLIRP, or a bridged guest NIC

Reject all of them. A guest network plus firewall rules is more complex than
no guest network. Guest root can also inspect and attack that larger network
surface.

#### AF_VSOCK as a general host path

Reject it for this implementation. A guest with a general host VSOCK path can
probe unrelated host VSOCK listeners. One named virtio-serial port has a
smaller and more explicit boundary.

#### 9p file sharing

Keep it only as an emergency diagnostic option. virtiofs has better semantics
for a source workspace and has passed the required nested file-system test.

## Runtime User Experience

### Commands

Add these forms:

```text
cooper vm list
cooper vm claude
cooper vm codex
cooper vm copilot
cooper vm opencode
cooper vm grok
cooper vm codex -c "go test ./..."
cooper vm prepare
```

`cooper vm <agent>` requires a running `cooper up`, as `cooper cli` does. If
the proxy is not running, return one direct action:

```text
Cooper is not running. Start `cooper up` in another terminal.
```

`cooper vm prepare` downloads and prepares VM assets without starting an agent
session. `cooper vm <agent>` calls the same preparation code when an asset is
missing. Preparation must be safe to retry.

VM resource flags can override configuration for one VM:

```text
--cpus N
--memory SIZE
--disk SIZE
```

Do not add an agent-state sharing flag. The selected-agent-only rule is fixed.

### Session Lifetime

Match the barrel lifetime:

- The first `cooper vm <agent>` creates a VM workload for the current absolute
  workspace and selected agent.
- Leaving the interactive shell does not stop the VM.
- A later command reuses the healthy VM and agent container.
- Multiple shells can use the same VM.
- The TUI can stop or restart the VM workload.
- `cooper down` stops and removes all VM workloads.
- A new `cooper up` starts with clean VM operating-system overlays.

The guest Docker image and build cache only need to live for one `cooper up`
session in the first implementation. Do not make a writable guest system disk
persistent by default. A later cache design can use a separate data disk after
a persistence threat review.

### Runtime Identity

Always include a stable hash. Do not query Docker to decide whether a hash is
needed.

```text
<namespace>-vm-<workspace-base>-<agent>-<path-hash>
```

The hash input must include:

- The canonical absolute workspace path.
- The selected agent name.
- The runtime namespace.

Use at least 12 hexadecimal characters from SHA-256. Normalize and truncate the
human-readable part so the full Docker name stays within Docker limits.

Use labels as the source of truth:

```text
cooper.kind=vm-supervisor
cooper.runtime-id=<id>
cooper.workspace=<canonical path>
cooper.tool=<agent>
cooper.depth=<1-or-2>
cooper.mount-plan=<sha256>
cooper.image-id=<agent image id>
```

Do not put credentials or full environment values in labels.

## Shared Launch Architecture

Do not copy `runCLI` and change Docker calls. First separate the shared session
policy from the execution back ends.

Add `internal/workload` with transport-neutral values:

```go
type Kind string

const (
    KindCLI Kind = "cli"
    KindVM  Kind = "vm"
)

type Access string

const (
    ReadOnly  Access = "ro"
    ReadWrite Access = "rw"
)

type MountSpec struct {
    ID         string
    Source     string
    Target     string
    Access     Access
    Kind       PathKind
    Ownership Ownership
}

type WorkloadSpec struct {
    ID           string
    Kind         Kind
    ToolName     string
    WorkspaceDir string
    ImageRef     string
    ImageID      string
    Mounts       []MountSpec
    Env          []EnvVar
    Depth        int
}

type ExecSpec struct {
    SessionName string
    Command     []string
    Environment []EnvVar
    Interactive bool
}
```

The final names can differ, but keep these boundaries:

- One pure resolver makes the complete mount plan.
- One pure resolver makes non-secret container environment.
- One shared launcher resolves auth values and session environment.
- The Docker barrel back end renders the plan as host Docker mounts.
- The VM back end renders the same plan as supervisor exports and guest Docker
  mounts.

Refactor `main.go:runCLI` into a small command adapter. A shared launch service
must own these current steps:

1. Load and validate configuration.
2. Validate the selected tool and image.
3. Resolve the workspace.
4. Resolve and validate the selected host state roots.
5. Resolve tokens for only the selected tool.
6. Create the runtime ID, session name, and terminal title.
7. Create the clipboard token and session files.
8. Build the protected environment wrapper.
9. Start or reuse the chosen workload.
10. Execute the interactive shell or one-shot command.
11. Restore the terminal and remove per-shell files.

This refactor is complete only when existing CLI tests prove that `cooper cli`
behavior did not change.

## One Mount Plan

Move the policy in `internal/docker/barrel.go:appendVolumeMounts` and
`barrelMountDirs` into a pure, runtime-neutral mount-plan builder.

The plan must include, when applicable:

- Workspace, read-write, at the same absolute path.
- `.git/hooks`, read-only, over the workspace mount.
- Only the selected agent state roots, read-write.
- `.gitconfig`, read-only.
- Cooper language caches, read-write.
- Cooper CA public certificate, read-only.
- Cooper live-configuration directory, read-only.
- Clipboard token, read-only.
- Clipboard shims, read-only.
- Fonts, read-only.
- Playwright browser cache, read-write.
- Per-workload `/tmp`, read-write.
- Per-workload session files, read-only.
- Host timezone snapshot, read-only.

Mount a dedicated Cooper live-configuration directory, not an individual
live rule file. An atomic rename of a bind-mounted file can leave a container
on the old inode. Mounting the containing directory lets an atomic file
replacement become visible to barrels, supervisors, and guests. Do not put
secrets in this directory.

The mount planner must preserve all current selected-agent mappings:

| Agent | Host sources | Container targets |
| --- | --- | --- |
| Claude | `~/.claude`, optional `~/.claude.json` | `/home/user/.claude`, `/home/user/.claude.json` |
| Copilot | `~/.copilot` | `/home/user/.copilot` |
| Codex | `~/.codex` | `/home/user/.codex` |
| OpenCode | Current cache, config, share, state, and compatibility roots | Current `/home/user` targets |
| Grok | Effective `GROK_HOME`, or `~/.grok` | `/home/user/.grok` |

Do not invent separate Grok auth, session, history, or memory mounts.

### Mount Validation

For each mount:

1. Convert the source to an absolute clean path.
2. Resolve all existing symlink components.
3. Record whether the source is a file or directory.
4. Reject an unexpected missing source. Create only known state or cache
   directories with their specified mode.
5. Reject duplicate targets.
6. Reject target overlap unless it is an explicit child read-only overlay such
   as `.git/hooks`.
7. Reject an agent state root that contains, or is contained by, a Cooper
   deletion root before any Cooper cleanup or temporary reset.
8. Render Docker mounts with `--mount` arguments. Do not build colon-separated
   `-v` strings because source paths can contain spaces and colons.

The VM supervisor receives each source at an opaque path such as:

```text
/cooper/exports/000-workspace
/cooper/exports/010-agent-state
/cooper/exports/020-git-hooks
```

The manifest contains the opaque export ID and the guest target. It does not
need the physical host source path.

The supervisor container mount mode is the first read-only enforcement layer.
The guest bind-remount mode is the second layer.

## Supervisor and Relay Images

Build two fixed infrastructure images.

### Supervisor Image

Add a generated or static Dockerfile for a versioned image such as:

```text
cooper-vm-supervisor:<schema>
```

It contains:

- QEMU x86 system support.
- `qemu-img`.
- A modern Rust `virtiofsd`.
- The `cooper-vm-host` binary.
- The `cooper-vm-guest` provisioning payload.
- The pinned guest Docker engine archive.

Pin the base image by digest. Pin QEMU packages through a fixed Ubuntu snapshot
or verify downloaded package hashes. Pin `virtiofsd` to an audited release. The
initial implementation should evaluate current `virtiofsd` 1.14.x. The 1.10
Ubuntu build passed the probes, but a newer pinned Rust build has better
rootless documentation and options.

Run the supervisor container with:

- `--network none`.
- `--cap-drop ALL`.
- `--security-opt no-new-privileges`.
- Docker's reviewed default seccomp policy, or a smaller tested policy.
- `--device /dev/kvm` only.
- The numeric KVM group as a supplementary group.
- `--read-only`.
- Explicit `tmpfs` mounts for its private `/run` and `/tmp`.
- CPU, memory, and PID limits.
- `--init` or a correct Go PID 1 that reaps children.
- No Docker socket.
- No host PID, IPC, UTS, user, or network namespace.
- No host home mount.

Run QEMU and `virtiofsd` as the invoking numeric UID and GID. The container can
start as root only for a short, reviewed ownership setup. It must drop to the
invoking user before either daemon starts.

### Relay Image

Use a separate minimal image, preferably `FROM scratch`, with one static Go
binary. It contains no shell and no QEMU tools.

The relay receives:

- One per-VM internal Docker network.
- One small Unix-socket directory shared with the supervisor.
- A read-only live-configuration directory that contains only allowed service
  IDs and ports.

It must not receive:

- Workspace or agent state mounts.
- VM disk mounts.
- The Cooper CA private key.
- The Docker socket.
- `/dev/kvm`.
- Host network mode.

The host Cooper proxy joins the same per-VM internal network. No other barrel
or VM must join it.

The relay maps only these services:

| Service ID | Destination |
| --- | --- |
| `proxy` | Current Cooper proxy container at `ProxyPort` |
| `bridge` | Current Cooper proxy container at `BridgePort` |
| `forward:<container-port>` | Current Cooper proxy container at an enabled container-side rule port |

Unknown services and disabled ports must fail before a network dial.
Reload a policy only after its syntax and version pass validation. Apply
global concurrent-stream, per-service connection, idle-time, and byte-buffer
bounds before the relay accepts the new version.

## QEMU Device Model

Start with an explicit QEMU device list. During implementation, prove that
`-nodefaults` works with the prepared guest.

Required settings include:

```text
-machine q35,accel=kvm,usb=off,dump-guest-core=off
-cpu host
-nic none
-display none
-monitor none
-no-user-config
-nodefaults
-sandbox on,obsolete=deny,elevateprivileges=deny,spawn=deny,resourcecontrol=deny
```

Add only:

- One read-only prepared base disk and one ephemeral qcow2 overlay.
- One memory backend that permits vhost-user sharing.
- One `vhost-user-fs-pci` device.
- One `virtio-serial-pci` controller.
- One named control port: `org.cooper.gateway`.
- One private serial log or emergency console.
- A QMP Unix socket inside the supervisor.
- A virtual random device if the guest requires it.

Do not add default USB, audio, graphics, SCSI, IDE, or network devices.

At depth 1, `-cpu host` must expose nested virtualization when the host KVM
module supports it. At depth 2, use the architecture-correct CPU option to hide
`vmx` or `svm`. Add a boot assertion that depth 2 does not expose either flag.

## Guest Base Image

### Source Lock

The probe used this official image:

```text
URL: https://cloud-images.ubuntu.com/releases/noble/release-20260801/ubuntu-24.04-server-cloudimg-amd64.img
SHA-256: 0533b0655c32e68b31d792ecd6ccfca95abdbc536c4446874fe0513bd4140ffe
```

The hash matched the official `SHA256SUMS` file. Put the selected source URL,
digest, size, and architecture in a reviewed asset lock file. Do not resolve a
moving `current` URL at runtime.

Download to a unique `.part` file. Verify size and SHA-256 before an atomic
rename. Hold a file lock so two `cooper vm prepare` processes cannot prepare
the same asset.

### Offline Preparation

Prepare the base in a temporary supervisor container with no network. Give the
preparation VM the same `-nic none` rule as a runtime VM.

Inject these verified files through a read-only preparation export:

- `cooper-vm-guest`.
- Pinned Docker engine, containerd, runc, and docker-proxy binaries.
- Docker systemd units and daemon configuration.
- Guest mount and relay units.
- A preparation manifest with a random nonce.

Do not put the current user's Cooper CA in the reusable prepared base. The CA
can change without a guest schema change.

The preparation process must:

1. Create user `user` with the configured numeric UID and GID, or configure
   runtime UID mapping in a deterministic way.
2. Install the guest agent and Docker daemon.
3. Install the runtime CA update helper and its empty trust-store target.
4. Enable required kernel modules for overlayfs, bridge networking, netfilter,
   virtiofs, and nested KVM.
5. Mask `systemd-networkd-wait-online`.
6. Disable SSH, getty services, cloud-init after preparation, package update
   timers, firmware refresh, ModemManager, snap auto work, and other services
   that require a network or are not needed.
7. Lock root and all password logins.
8. Configure a serial emergency log without an interactive login.
9. Clear host keys, machine identity, cloud-init data, logs, and preparation
   secrets.
10. Write a guest schema marker.
11. Report the preparation nonce and component versions through virtio-serial.
12. Shut down cleanly.

After shutdown, run `qemu-img check`. Reject an image with errors or a missing
nonce. Write a metadata file with all component versions and digests. Make the
prepared base read-only. Runtime code must never open it for writing.

### Runtime Disk

Create one ephemeral qcow2 overlay per VM workload. Its backing file is the
read-only prepared base. Store it only under the workload runtime directory.

Delete the overlay after the VM stops and all QEMU processes close it. Never
delete an overlay from a guessed PID or name. Verify Docker labels and runtime
ownership first.

## Guest Startup and Docker

`cooper-vm-guest` runs as a system service. It must fail before agent startup if
the manifest, export, image, proxy path, or Docker daemon is not ready.

Startup order:

1. Open `/dev/virtio-ports/org.cooper.gateway`.
2. Complete a protocol-version and random-session-nonce handshake.
3. Mount the single virtiofs export at `/run/cooper/host`.
4. Validate the manifest digest and every opaque export.
5. Bind each export to its guest target in defined order.
6. Apply read-only bind remounts after all parent mounts.
7. Verify and install the current Cooper CA public certificate into the
   ephemeral guest trust store.
8. Prepare guest-local `/home/user/.cooper` for nested Cooper development.
9. Make the guest and agent share the same absolute workspace path and `/tmp`.
10. Start the local relay listeners.
11. Start guest Docker with explicit daemon settings and proxy settings.
12. Load the selected agent image archive if its exact image ID is absent.
13. Create the fixed Cooper control network.
14. Start the selected agent container from the shared `WorkloadSpec`.
15. Mount the guest Docker socket in the agent container.
16. Add the live guest Docker socket group to the agent container.
17. Report ready only after the agent entrypoint and relay listeners are healthy.

The runtime manifest must contain the expected CA digest. A CA change requires
a new VM workload or a tested live trust-store reload. It must not require a
new prepared base.

Use fixed guest Docker address pools. Reserve, for example:

```text
172.29.0.0/24  guest default Docker bridge
172.30.0.0/24  Cooper agent control network
172.30.0.1     Cooper guest relay on the control network
```

Do not rely on these example addresses without collision checks in the guest.
Write the final addresses into the manifest and tests. Docker must reject a
user network that conflicts with the active Cooper network.

The agent container joins only the internal Cooper control network. Add this
exact host mapping:

```text
cooper-proxy -> <explicit-control-network-gateway>
```

Do not use Docker's `host-gateway` keyword for this mapping. The probe proved
that it selects the wrong bridge for an internal user-defined network.

The agent container keeps its existing local socat behavior. Its proxy,
bridge, and configured port connections reach the guest relay. The guest relay
opens one approved logical stream to the supervisor for each TCP connection.

## Guest Docker Internet Policy

Guest Docker needs two proxy paths:

1. The Docker daemon needs the Cooper proxy for image manifest and layer
   downloads.
2. Docker build steps need predefined HTTP and HTTPS build proxy arguments.

Write guest daemon proxy settings as described by Docker's daemon proxy
configuration. Point them to a guest bridge address where the guest relay is
listening. Do not add a guest route for this.

When Cooper runs inside a Cooper VM, inject a reviewed guest-context file. The
nested Cooper build code must read this file and add these predefined build
arguments to every `docker build` call:

```text
HTTP_PROXY
HTTPS_PROXY
NO_PROXY
http_proxy
https_proxy
no_proxy
```

Do not declare the predefined proxy arguments in a Dockerfile. Do not bake the
proxy URL into an image layer. Do not accept a proxy destination from a normal
user environment variable on a physical host.

Add one command builder for all Cooper Docker builds. Proxy, base, built-in
agent, custom agent, and supervisor image builds must use it.

## Nested Cooper Proxy Chain

An inner Cooper proxy must never try a direct connection.

The outer guest agent mounts a read-only context file into the outer agent
container. It contains:

- Current VM depth.
- Outer guest relay proxy address.
- Outer Cooper control-network name.
- Docker-host-visible shared paths.
- A schema version.

When `cooper up` runs in that agent container:

1. Create its own normal internal network.
2. Create its nominal external network with `--internal` too.
3. Start its Squid proxy without a direct external path.
4. Attach that proxy to the outer VM control network.
5. Generate a Squid `cache_peer` for the outer guest relay.
6. Generate `never_direct allow all`.
7. Validate the full Squid configuration before startup or reload.

The nested proxy must use the explicit gateway of the connected outer control
network. Do not use `host-gateway`.

Add an integration test that proves this sequence:

```text
inner Squid access log
-> outer Squid access log
-> one approved target
```

Also prove that stopping the outer Squid makes the inner request fail. It must
not fall back to direct access.

## Agent Image Transfer

Run the exact host-built agent image in the guest. Do not rebuild an agent image
inside the guest for a normal `cooper vm` launch.

First implementation:

1. Inspect the host image ID and platform.
2. Create a Docker archive with `docker image save --platform linux/amd64`.
3. Write to a unique temporary file under the Cooper VM image cache.
4. Validate the archive and record its source image ID.
5. Atomically rename it to a cache path keyed by image ID.
6. Mount it read-only into the supervisor export.
7. Load it with the guest Docker daemon.
8. Inspect the guest image and require the exact expected image ID and labels.

Use a lock per image ID. A failed export must not replace a valid cache entry.
Do not read host Docker storage directories directly.

The first launch can be slower because an agent image can be large. Keep the
archive cache across `cooper down`. `cooper cleanup` can remove it because it is
Cooper-owned data.

Record export and load times. A later content-addressed layer transport can
replace the archive only if it keeps the same trust and identity checks.

## Docker Development Paths

Docker bind sources are resolved by the guest Docker daemon, not by the agent
container. These paths must exist at the same absolute path in both places:

- The workspace.
- The VM `/tmp` used by Cooper tests.
- `/home/user/.cooper` inside the VM.
- Selected mounted cache or state paths that tests can pass to Docker.

Mount the per-VM transient directory as guest `/tmp`, then mount that same
guest `/tmp` into the agent container. This lets Go tests create a temporary
Docker build context that the guest daemon can read at the same path.

Create guest-local `/home/user/.cooper` and mount it into the agent container at
the same path. Do not mount the physical host's complete Cooper directory. The
nested Cooper instance must not share the physical host runtime sockets, CA
private key, or mutable configuration.

The workspace remains the main durable source of truth. The selected agent
state remains the only physical host home state that is shared.

## VM Control Protocol

Use one named virtio-serial channel and multiplex it. Pin
`github.com/hashicorp/yamux` to a reviewed version. The current reviewed
candidate is `v0.1.2`. Record its license and checksum in `go.sum`.

Do not invent a complete stream multiplexer. Add a small Cooper protocol above
yamux.

Every logical stream starts with:

- Protocol magic.
- Protocol version.
- Service type.
- Request ID.
- Payload length.
- A bounded JSON or binary header.

Set a small header limit, such as 64 KiB. Reject unknown fields where they can
change security behavior. Set deadlines for handshake and control messages.
Set explicit bounds for concurrent streams, pending accepts, frame size,
buffer size, idle time, and connection lifetime. Reject excess work before it
allocates an unbounded goroutine or buffer.

Supported streams:

| Direction | Type | Purpose |
| --- | --- | --- |
| Host to guest | `control` | Ready, health, reload, and shutdown |
| Host to guest | `exec` | Start a shell or one-shot command |
| Host to guest | `exec-control` | Resize and signal one exec session |
| Guest to host | `proxy` | One TCP connection to host Squid |
| Guest to host | `bridge` | One TCP connection to host bridge through proxy relay |
| Guest to host | `forward` | One enabled container-side port connection |

For raw TCP streams, accept the service header and then copy bytes in both
directions with correct half-close behavior.

For exec streams, use bounded frames for:

- Standard input.
- Combined terminal output for interactive sessions.
- Separate stdout and stderr for non-interactive sessions.
- Terminal resize.
- Signal.
- Exit status.

The host `cooper vm` process must put the terminal in raw mode and always
restore it. Do not use a second Docker PTY around the control client. Run the
supervisor control client through `docker exec -i`, then let Cooper own the
actual PTY protocol.

Inside the guest, either use the Docker Engine exec API or run the pinned Docker
CLI under a controlled PTY. Select one after a focused resize, signal, and exit
status test. Keep that choice behind one small guest exec interface.

Never send auth environment values in command arguments. Send the initial exec
request through the framed standard-input channel. Redact secret names and
values from error text.

## Clipboard and Session Authentication

The current clipboard token slow path assumes every session is a host Docker
barrel and calls `docker inspect` on that barrel. Generalize it before VM
clipboard tests.

Replace the token-only disk file with host-written metadata that includes:

- Random clipboard token.
- Runtime ID.
- Runtime kind: `cli` or `vm`.
- Tool name.
- Clipboard mode.
- Creation time.
- A format version.

Keep the file mode `0600` in a `0700` directory. Write it atomically. Never
trust metadata written by the guest.

Validation must:

1. Compare the token in constant time.
2. Find the matching host Docker workload by exact runtime label.
3. Require the barrel or VM supervisor to be running and healthy.
4. Require its label tool and runtime kind to match the metadata.
5. Reject stale or malformed files.

The clipboard token file then follows the common mount plan into the selected
agent container. Existing X11 and shim modes remain unchanged.

On stop or restart, rotate or remove the token before the old guest can use it.

## Resource Policy

Add a `VMConfig` value to `config.Config`. Use integers in MiB and GiB after
validation. Suggested initial fields:

```go
type VMConfig struct {
    CPUs          int `json:"cpus"`
    MemoryMiB     int `json:"memory_mib"`
    DiskGiB       int `json:"disk_gib"`
    MaxDepth      int `json:"max_depth"`
    StartTimeoutS int `json:"start_timeout_secs"`
    StopTimeoutS  int `json:"stop_timeout_secs"`
}
```

Keep `MaxDepth` fixed at 2 for this release even if it is stored in config.

Choose defaults from host capacity, with conservative caps. On this 24-CPU
host, which had approximately 26 GiB available during the probe, a useful
outer development VM is 8 CPUs and 12 GiB.
An inner test VM can use 4 CPUs and 4 to 6 GiB. Do not reserve all host memory.

Validation must leave enough memory and CPU for the host proxy, Docker, TUI,
and operating system. Reject a nested request that is larger than its outer VM
limit.

The supervisor Docker limit must include QEMU memory plus fixed overhead. The
guest agent must apply limits to the selected agent container only when current
CLI behavior already applies them. Do not silently make CLI and VM tools act
differently.

## Lifecycle and Recovery

Use a clear state machine:

```text
absent -> preparing -> starting -> ready -> stopping -> absent
                       |          |
                       v          v
                     failed <-----+
```

Persist only non-secret diagnostic state. Each transition must be idempotent.

### Start

- Hold a per-runtime file lock.
- Validate all host-owned roots before any Cooper-owned reset.
- Remove only stale sockets inside the verified runtime directory.
- Create relay network, relay container, and proxy attachment.
- Start the networkless supervisor.
- Wait for relay, virtiofs, guest, Docker, image, and agent health in order.
- On any failure, stop all children and remove the per-VM network.
- Keep logs and the failed runtime metadata until the error is reported.

### Stop

- Revoke clipboard access first.
- Stop new exec and relay streams.
- Ask the guest agent to stop the agent container and Docker cleanly.
- Ask systemd to power off.
- Use QMP `system_powerdown`, then `quit` after a bounded timeout.
- Let Docker stop the supervisor after the guest timeout.
- Stop and remove the relay.
- Disconnect the proxy and remove the per-VM network.
- Remove the ephemeral overlay and runtime files only after QEMU has exited.

### Crash Recovery

Test and handle:

- QEMU exit.
- `virtiofsd` exit.
- Relay exit.
- Guest agent hang.
- Guest Docker failure.
- Agent container exit.
- Host `cooper vm` client interruption.
- Host `cooper up` SIGTERM and SIGKILL recovery through `cooper down`.
- Stale supervisor, relay, network, socket, PID file, and overlay.
- Corrupt qcow2 overlay.
- Missing or changed host mount while the VM is running.
- Disk full during image export or overlay writes.

Never restart QEMU automatically with a possibly corrupt overlay. Report the
failure in the TUI and require a reviewed recreate path.

## TUI Refactor

Before TUI changes, read `AGENTS.TUI.md`.

The App boundary currently exposes container-specific names. Generalize it to
workloads without making the TUI import QEMU or Docker details.

Replace concepts such as:

```text
ContainerStats
StopContainer
RestartContainer
ListContainers
```

with runtime-neutral operations such as:

```text
WorkloadStats
StopWorkload
RestartWorkload
ListWorkloads
```

Each workload includes:

- Stable ID.
- Display name.
- Kind: proxy, CLI, or VM.
- Tool.
- Workspace.
- Status.
- Shell count.
- CPU and memory.
- Temporary-disk usage.
- VM depth where applicable.
- Health reason where applicable.

Rename the Containers tab to Runtimes, or use another short term that clearly
includes VMs. Keep the proxy first. Add one compact kind indicator. Do not add a
second VM-only management screen.

VM restart and stop actions must call the same lifecycle service as the CLI.
Do not let the TUI build Docker commands.

## Code Layout

Use this as the target package split. Adjust names only to make ownership more
clear.

```text
cooper/
├── cmd/
│   ├── cooper-vm-host/
│   │   └── main.go
│   └── cooper-vm-guest/
│       └── main.go
├── internal/
│   ├── workload/
│   │   ├── mountplan.go
│   │   ├── session.go
│   │   ├── types.go
│   │   └── validation.go
│   ├── vm/
│   │   ├── assets.go
│   │   ├── backend.go
│   │   ├── cleanup.go
│   │   ├── context.go
│   │   ├── imagearchive.go
│   │   ├── lifecycle.go
│   │   ├── names.go
│   │   ├── qemu.go
│   │   ├── relay.go
│   │   ├── resources.go
│   │   └── supervisor.go
│   ├── vmguest/
│   │   ├── docker.go
│   │   ├── exec.go
│   │   ├── mounts.go
│   │   ├── network.go
│   │   └── service.go
│   └── vmproto/
│       ├── frame.go
│       ├── handshake.go
│       ├── mux.go
│       └── service.go
├── internal/templates/
│   ├── vm-supervisor.Dockerfile.tmpl
│   ├── vm-guest-provision.sh.tmpl
│   └── squid.conf.tmpl
└── test-vm.sh
```

Keep host orchestration in `internal/vm`. Keep guest-only behavior in
`internal/vmguest`. Keep wire values in `internal/vmproto`. Keep mount and
session policy in `internal/workload`.

Do not put VM orchestration back into `main.go`. Cobra handlers should parse
arguments and call services.

## Required Refactors by Existing File

### `main.go`

- Add the `vm` command and flags.
- Extract shared CLI and VM session preparation from `runCLI`.
- Keep TTY cleanup and terminal-title behavior shared.
- Add VM workloads to startup checks and cleanup calls.

### `internal/docker/barrel.go`

- Remove mount-policy ownership.
- Render a `workload.MountSpec` list to Docker `--mount` arguments.
- Keep Docker-only start, inspect, and exec behavior in this package.
- Replace collision-dependent barrel names with stable runtime IDs.

### `internal/docker/network.go` and `runtime_names.go`

- Stop relying on process-global mutable names where one app can own more than
  one VM network.
- Pass a runtime naming value explicitly.
- Add per-VM relay network names and labels.
- Add nested parent-network context without weakening physical-host networks.

### `internal/docker/proxy.go`

- Support an optional, verified parent-proxy context.
- In parent mode, use internal networks only and connect to the outer control
  network.
- Do not publish a direct egress path.

### `internal/docker/portforward.go`

- Signal both barrels and VM supervisors after an atomic rule update.
- Update the supervisor allow set and guest relay before reporting success.
- Collect errors from all runtimes.

### `internal/templates/squid.conf.tmpl`

- Add a parent-proxy block only for a verified nested context.
- Generate `never_direct allow all` in that block.
- Keep the existing ACL helper and domain policy.

### `internal/templates/base.Dockerfile.tmpl`

- Install a pinned Docker CLI in the common agent image.
- Do not mount or configure a Docker socket in CLI mode.
- Keep the selected agent image identical between CLI and VM.

### Build code and Dockerfile templates

- Route every Docker build through one command builder.
- Add predefined proxy arguments only inside a verified VM context.
- Build the supervisor and relay images with reviewed immutable inputs.
- Invalidate the prepared guest base only when its schema or locked components
  change.

### `internal/clipboard`

- Replace Docker-barrel-only token discovery with generic runtime metadata.
- Keep host-side liveness validation.
- Add VM token rotation and stale-token tests.

### `internal/app` and `internal/tui/containers`

- Generalize container management to workload management.
- Aggregate host Docker barrels and VM supervisors.
- Preserve the existing presentation/infrastructure boundary.

### `down.go` and cleanup code

- Detect and stop supervisor and relay containers by exact labels and runtime
  namespace.
- Remove per-VM networks.
- Remove transient VM runtime directories.
- Preserve prepared bases and image archives on `down`.
- Remove Cooper-owned VM caches only through `cleanup`.
- Validate host agent roots before any recursive Cooper deletion.

### `cooper/dev/setup.sh`

The containerized supervisor changes the minimum host requirements.

Required host items are:

- Linux x86-64.
- Docker Engine.
- CPU virtualization support.
- Loaded `kvm_amd` or `kvm_intel`.
- User access to `/dev/kvm`.
- Nested KVM for the self-hosting gate.
- Sufficient CPU, memory, and disk.

Host QEMU, host OVMF, host `virtiofsd`, `vhost_vsock`, `/dev/vhost-vsock`,
`/dev/net/tun`, GUI packages, `remote-viewer`, and bubblewrap are not runtime
requirements for the selected design. Remove them from the required install
set unless a focused diagnostic still uses them. Do not keep setup work only
because the earlier direct-QEMU design needed it.

Update `cooper/dev/setup_test.sh` and `cooper/dev/README.md` with the final
requirements and the reason for the smaller host surface.

## Implementation Phases

Do the work in this order. Keep each phase green before the next phase.

### Phase 1: Freeze Shared CLI Behavior

1. Add golden or table tests for current mount lists, environment, command
   wrappers, token handling, names, and lifecycle.
2. Add `internal/workload` values and pure builders.
3. Make Docker barrels use the builders.
4. Keep all existing CLI integration tests green.

Exit condition: `cooper cli` has one tested policy source and no behavior
change.

### Phase 2: Add VM Assets and Infrastructure Images

1. Add the reviewed asset lock.
2. Add atomic download and digest verification.
3. Build static host and guest binaries.
4. Build pinned supervisor and minimal relay images.
5. Add image labels and version checks.
6. Add unit tests that do not need KVM.

Exit condition: asset and image builds are deterministic and fail on any
digest mismatch.

### Phase 3: Prepare the Guest Base

1. Implement the no-NIC preparation boot.
2. Install guest Docker and services offline.
3. Remove unneeded services and the network wait.
4. Verify nonce, schema, versions, shutdown, and qcow integrity.
5. Make the base immutable.

Exit condition: a prepared guest boots to guest-agent ready without a physical
NIC and without the two-minute wait.

### Phase 4: Add Supervisor, Relay, and Protocol

1. Implement the yamux session and bounded service headers.
2. Implement the networkless supervisor lifecycle.
3. Implement the minimal relay allow map.
4. Implement guest health and shutdown.
5. Add crash and malformed-protocol tests.

Exit condition: a guest can use only proxy, bridge, and enabled forward
services. Arbitrary destinations fail.

### Phase 5: Start the Agent in Guest Docker

1. Load and verify the exact agent image.
2. Apply the shared mount and environment plan.
3. Mount only the guest Docker socket.
4. Add interactive and one-shot exec.
5. Add resize, signal, exit-code, and concurrent-shell behavior.
6. Add clipboard token metadata for VMs.

Exit condition: all built-in tools pass the common no-credential smoke tests in
CLI and VM modes.

### Phase 6: Complete Network and Live Updates

1. Configure daemon pulls through the relay.
2. Configure build proxy arguments.
3. Route bridge and port-forward traffic.
4. Reload rules in proxy, barrels, supervisors, and guests.
5. Add nested Squid parent mode.
6. Prove fail-closed behavior when each upstream component stops.

Exit condition: all guest, Docker, build, and nested-proxy traffic appears in
the correct host Squid logs and direct traffic fails.

### Phase 7: TUI and Cleanup

1. Generalize the Containers tab to Runtimes.
2. Add VM stats, health, stop, and restart.
3. Add `down` and stale-runtime recovery.
4. Add safe cleanup of VM-owned assets.
5. Run deterministic TUI visual QA as required by `AGENTS.TUI.md`.

Exit condition: normal exit, SIGTERM, crash recovery, `down`, and cleanup leave
no QEMU, virtiofs, relay, VM network, or transient disk.

### Phase 8: Self-Hosting

1. Make workspace, `/tmp`, and guest-local Cooper paths visible to the guest
   Docker daemon at identical absolute paths.
2. Pass the verified VM context to nested Cooper.
3. Support nested supervisor image builds and guest-base preparation through
   the outer proxy.
4. Expose nested KVM at depth 1.
5. Hide nested virtualization at depth 2.
6. Add the full self-hosting release test.

Exit condition: Cooper VM builds, tests, and starts Cooper VM from inside an
outer Cooper VM.

### Phase 9: Documentation and Release Gates

1. Update README, requirements, help, proof output, and development setup.
2. Add a VM security document.
3. Add test logs and troubleshooting commands.
4. Run all old and new release gates.

Exit condition: no known red gate, stale generated file, or undocumented host
requirement remains.

## Test Plan

### Pure Unit Tests

Add table tests for:

- Stable VM names and path hashes.
- Runtime namespace isolation.
- Mount list equality between CLI and VM.
- Read-only child overlays.
- Missing sources and allowed directory creation.
- Direct and symlink-resolved state-root overlap.
- Paths with spaces, colons, Unicode, and long names.
- VM resource defaults and bounds.
- Depth calculation and CPU flag selection.
- QEMU argument generation.
- Supervisor and relay Docker argument generation.
- Service allow-map generation.
- Nested Squid parent configuration.
- Build proxy argument generation.
- Guest context parsing and rejection.
- Image archive cache identity.
- Asset SHA and size checks.
- Lifecycle state transitions.
- Clipboard metadata and liveness checks.
- Cleanup ownership validation.

Use injected command runners, clocks, random readers, and file systems where
the package already uses that pattern. Do not make unit tests call host Docker
or KVM.

### Protocol Tests

Use `net.Pipe`, Unix sockets, and the race detector. Test:

- Correct version handshake.
- Wrong magic and version.
- Short and oversized headers.
- Invalid lengths and JSON.
- Unknown service IDs.
- Arbitrary destination attempts.
- Disabled forward ports.
- Slow readers and writers.
- Backpressure between concurrent streams.
- Half-close in both directions.
- Session close while streams are blocked.
- Keepalive timeout.
- Duplicate request IDs.
- Exec output larger than one frame.
- Resize storms.
- Ctrl-C, SIGTERM, and client disconnect.
- No secret text in errors or logs.

Run affected packages with `go test -race`.

### Supervisor Integration Tests

Run a temporary supervisor with no network and only `/dev/kvm`. Assert:

- UID is not root when QEMU starts.
- Capabilities are empty.
- No network interface exists in the supervisor except loopback.
- Only `/dev/kvm` is present from the host device set.
- The root file system is read-only.
- QEMU has the reviewed sandbox arguments.
- The guest has only loopback before Docker starts.
- A raw IPv4, IPv6, DNS, ICMP, and TCP egress test fails.
- Read-write and read-only virtiofs exports behave correctly.
- A workspace symlink cannot expose a physical-host path that is not mounted in
  the supervisor.

### Guest Docker Tests

Assert:

- Docker starts without a guest NIC.
- Image load preserves the exact expected image ID.
- The agent container has the same tool version as CLI mode.
- The agent can run and build containers.
- A nested container cannot use direct IPv4 or IPv6.
- A Docker registry pull succeeds only through Squid.
- A Dockerfile `RUN` download succeeds only with Cooper build proxy arguments.
- Stopping the relay or proxy makes pulls and builds fail.
- The agent container has a guest Docker socket.
- The guest and agent have no physical host Docker socket.
- Workspace and `/tmp` sibling bind mounts work.

### Mount and Agent-State Tests

For each built-in agent:

- Put a unique sentinel in each supported host state root.
- Start CLI and VM separately.
- Confirm that the selected agent sees all its sentinels read-write.
- Confirm that unselected agent sentinels do not exist.
- Change a session file in the VM and read it on the host.
- Change it on the host and read it in the VM.
- Confirm the installed CLI binary remains visible.
- Confirm `.git/hooks` is read-only.
- Confirm `cooper down` and `cooper cleanup` do not change host state.

For Grok, test a custom `GROK_HOME`, direct overlap, parent overlap, child
overlap, and symlink-resolved overlap.

### Feature-Parity Matrix

Run the same test function against `KindCLI` and `KindVM` for:

- Workspace path.
- Tool version.
- Login shell.
- One-shot command.
- Protected environment.
- Timezone.
- Language caches.
- Playwright cache and fonts.
- Execution bridge route.
- Clipboard shim mode.
- Clipboard X11 mode.
- Clipboard token rejection after stop.
- Single port forward.
- Port range.
- Live port reload.
- Concurrent shells.
- TUI stop and restart.

Credentials are not required for common clipboard and mount behavior. Keep real
provider login lifecycle checks manual, as they are for CLI mode.

### Network Isolation Tests

Run all tests from:

- Guest host namespace.
- Agent container.
- A normal nested container.
- A Docker build step.
- A nested Cooper proxy.
- The depth-2 guest.
- The depth-2 agent container.

Try:

- Public IPv4 by address.
- Public IPv6 by address.
- Host LAN gateway.
- Another LAN address.
- Physical-host Docker bridge gateway.
- Physical-host loopback assumptions.
- UDP DNS.
- TCP DNS.
- An unapproved HTTPS host.
- An approved HTTPS host through Squid.
- A process with all proxy environment values removed.
- A process that sets a fake proxy value.

Only the approved request through the Cooper relay and Squid can succeed.

### Failure Injection

Kill or corrupt one item at a time:

- Relay process.
- Supervisor process.
- QEMU.
- `virtiofsd`.
- Guest agent.
- Guest Docker daemon.
- Agent container.
- Host Squid.
- ACL helper socket.
- Clipboard token file.
- Agent image archive.
- Prepared base metadata.
- qcow2 overlay.
- One host mount.

Also test full disk, read-only Cooper directory, invalid KVM permissions, lost
KVM group, nested KVM disabled, too little memory, and a port collision.

Each test must assert the final process, container, network, socket, mount, and
disk state. An error message alone is not enough.

### Required Self-Hosting E2E Test

Add a separate Linux KVM gate, for example `cooper/test-vm.sh`. It must use a
test runtime namespace and temporary Cooper configuration.

The release case must do the equivalent of:

```text
physical host:
  start test Cooper control plane
  start outer cooper vm test-agent

outer agent command:
  go build -C ./cooper -o ./cooper .
  go test -C ./cooper ./...
  build Cooper images with guest Docker
  run Docker-backed Cooper tests
  start inner cooper vm test-agent -c <assertions>

inner agent command:
  confirm workspace sentinel
  confirm selected state sentinel
  confirm Docker works
  confirm approved proxy request works
  confirm direct and unapproved requests fail
```

Do not require a real Claude, Codex, Copilot, OpenCode, or Grok request in this
gate. Use a deterministic test agent image. Run separate no-credential smoke
checks for every built-in agent image.

The self-host test must also assert:

- Outer guest sees `vmx` or `svm` and has `/dev/kvm`.
- Inner guest sees neither `vmx` nor `svm`.
- Inner guest does not have `/dev/kvm`.
- `cooper vm` at depth 2 rejects depth 3.
- Both VMs have no NIC.
- Both Docker daemons have no direct route.
- virtiofs over virtiofs is read-write only at approved targets.
- Both proxy logs contain the expected chained request.
- No supervisor or relay remains after shutdown.

### Existing Cooper Gates

Finish implementation validation with all required project gates from the
repository root:

```bash
go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1
timeout 90m ./cooper/test-e2e.sh > /tmp/cooper-e2e.txt 2>&1
timeout 90m ./cooper/test-docker-build.sh all > /tmp/cooper-docker-build.txt 2>&1
go build -C ./cooper -o ./cooper . > /tmp/cooper-build.txt 2>&1
```

Add the VM gate with a timeout that covers two clean VM boots. Keep its full log
under `/tmp`.

Run focused race tests for the protocol, lifecycle, clipboard, and workload
packages. Run the TUI visual checks required by `AGENTS.TUI.md` after the
Runtimes screen change.

## Observability

Write structured, timestamped logs under the verified per-runtime directory:

- Supervisor lifecycle.
- QEMU stderr.
- QMP events.
- `virtiofsd`.
- Relay policy decisions without payload content.
- Guest-agent lifecycle.
- Guest Docker startup.
- Agent container startup.
- Image export and load timing.

Use a unique run ID and new result file for every probe and E2E case. Do not
accept a success marker from an append-only log. A test must match the marker
for its current nonce and must also prove that the current process exited with
the expected status.

Do not log:

- Auth environment values.
- Clipboard tokens.
- Clipboard data.
- Agent state file content.
- Proxy request bodies.

Add `cooper vm doctor` or extend `cooper proof` to report:

- Host KVM access.
- Nested KVM state.
- Supervisor and relay image IDs.
- Guest asset digest and schema.
- VM resource defaults.
- Running VM workload health.
- Guest Docker version.
- Whether direct guest network devices are absent.

Diagnostics must distinguish `unsupported`, `not prepared`, `not running`, and
`unhealthy`.

## Documentation

Update:

- `cooper/README.md` with `cooper vm`, when to use it, first-start cost, and
  the self-hosting example.
- `cooper/REQUIREMENTS.md` with normative VM, nesting, mount, proxy, and cleanup
  behavior.
- Root `AGENTS.md` only if implementation evidence changes a design invariant.
- `cooper/dev/README.md` and setup help with final host requirements.
- CLI help and examples.
- `cooper proof` output.

Add `cooper/docs/vm-security.md`. It must state:

- What guest root can change.
- Which host files are intentionally writable.
- Why no NIC is stronger than guest firewall rules.
- Why the supervisor has no network.
- Why the relay has no host data.
- Why the guest Docker socket is safe relative to the physical host and still
  gives root in the guest.
- Why managed depth 2 hides nested KVM, and why root at depth 1 can bypass
  Cooper and start another VMM because `/dev/kvm` is an intentional feature.
- Which denial-of-service and CPU side-channel risks remain.
- Why a VM boundary reduces risk but is not a guarantee against a QEMU or KVM
  vulnerability.

## Migration and Compatibility

- Existing config files get tested VM defaults through `applyMissingDefaults`.
- Existing `cooper cli` names, images, and behavior remain compatible where a
  stable-hash migration is not required. If names change, stop old barrels
  through labels and document the one-time recreate.
- VM assets use a schema version independent from the Cooper release version.
- A schema change creates a new prepared base. Do not modify an old base.
- Tool image changes create a new archive cache key. Do not overwrite an
  archive for another image ID.
- `cooper down` must clean old pre-release VM labels and names that this branch
  can create during development.
- Do not migrate or copy any host agent state.

## Performance Targets

Measure before optimizing. Record cold and warm values in VM E2E logs.

Targets for this host:

- Prepared guest ready signal: less than 30 seconds on a warm host.
- Reused VM shell start: less than 3 seconds.
- Clean shutdown: less than 15 seconds before forced cleanup.
- No repeated agent image export when the image ID is unchanged.
- No repeated guest image load when the same VM is still running.

Do not weaken mount consistency, image verification, or network isolation to
meet a time target.

## Review Checkpoints

Request a focused review after each checkpoint:

1. Shared mount and session policy refactor.
2. Asset locks and guest-base preparation.
3. Supervisor and relay security arguments.
4. VM protocol and malformed-input tests.
5. Guest Docker and proxy path.
6. Clipboard and TUI runtime generalization.
7. Nested parent-proxy mode.
8. Full self-hosting gate.

At each checkpoint, remove old helpers and comments that only served the prior
design. Do not keep both direct-QEMU and supervisor-container paths.

## Primary References

- QEMU system invocation and sandbox options:
  <https://www.qemu.org/docs/master/system/invocation.html>
- QEMU virtiofs guidance:
  <https://qemu.readthedocs.io/en/latest/tools/virtiofsd.html>
- Rust `virtiofsd` 1.14 documentation:
  <https://gitlab.com/virtio-fs/virtiofsd/-/blob/v1.14.0/README.md>
- Ubuntu Noble cloud images and official checksums:
  <https://cloud-images.ubuntu.com/releases/noble/>
- cloud-init NoCloud data source:
  <https://cloudinit.readthedocs.io/en/latest/reference/datasources/nocloud.html>
- Docker network model:
  <https://docs.docker.com/engine/network/>
- Docker daemon proxy configuration:
  <https://docs.docker.com/engine/daemon/proxy/>
- Docker CLI proxy configuration:
  <https://docs.docker.com/engine/cli/proxy/>
- Docker image save:
  <https://docs.docker.com/reference/cli/docker/image/save/>
- Docker image load:
  <https://docs.docker.com/reference/cli/docker/image/load/>
- HashiCorp yamux source and protocol:
  <https://github.com/hashicorp/yamux>

## Definition of Done

The feature is complete only when all statements below are true:

- `cooper vm <agent>` works for all built-in agents.
- CLI and VM parity tests pass from one shared policy source.
- The selected complete host agent state is read-write in the VM.
- Unselected agent state is absent.
- The host Docker socket is absent from the guest and agent container.
- The guest has no virtual NIC and no direct IPv4 or IPv6 route.
- Guest Docker pulls, builds, and containers use the host Cooper proxy.
- Bridge, clipboard, port rules, and live reload work in VM mode.
- The supervisor has host data and no network.
- The relay has a proxy-only network and no host data.
- QEMU runs unprivileged with only `/dev/kvm`.
- `cooper down` leaves no VM process, container, network, socket, or transient
  disk.
- Cleanup never changes a host agent state root.
- TUI runtime controls work for barrels and VMs.
- Cooper can build and test Cooper inside an outer VM.
- Cooper can start one inner Cooper VM from the outer VM.
- A Cooper-managed depth-2 VM has no nested virtualization flag or KVM device
  and cannot start depth 3 through Cooper.
- Existing Go, E2E, Docker-build, and build gates pass.
- The new VM and self-hosting gates pass.
- Security and user documentation match the implemented behavior.
