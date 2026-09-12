# Cooper VM Security Model

`cooper vm` is for work that needs Docker or a stronger kernel boundary than
`cooper cli`. It gives the selected agent root-equivalent control of a guest
machine. It does not give the agent root on the physical host.

This document defines the boundary. A change that weakens this boundary must
include a security review and tests.

## Trust Model

Treat the agent, all project code, all guest processes, and all containers in
the guest as untrusted. Guest root can:

- Change the guest operating system and its Docker data.
- Start privileged guest containers.
- Read and change the mounted workspace and selected agent state.
- Read Cooper-managed language caches and write to the configured cache
  targets.
- Use the execution bridge and host ports that the user configured.
- At depth 1, use `/dev/kvm` to start a nested virtual machine.

Guest root cannot use a normal guest setting to:

- Add a QEMU network device. QEMU starts with `-nic none`.
- Mount a physical-host path that the supervisor did not receive.
- Use the physical-host Docker socket. Cooper never mounts that socket in the
  supervisor or guest.
- Ask the VM relay to connect to an arbitrary host or port. The host writes the
  relay service map.
- Start a managed depth-3 Cooper VM. A depth-2 guest has no `/dev/kvm` and its
  virtual CPU has no `vmx` or `svm` feature.

## Process Boundaries

The physical host uses Docker only to supervise two small components:

1. The VM supervisor has no Docker network. It runs as the invoking user, has
   a read-only root file system, drops all capabilities, and receives only
   `/dev/kvm` as a host device. It can read the immutable guest base, its
   private VM files, and the exact host paths that Cooper approved for
   virtiofs export.
2. The VM relay has one private internal Docker network. It has no workspace,
   agent state, KVM device, or physical-host Docker socket. It accepts only a
   Unix socket from the supervisor and connects only to the Cooper proxy,
   execution bridge, or configured forwarded ports.

QEMU uses KVM, an explicit device list, `-nodefaults`, `-nic none`, and the
QEMU process sandbox. Cooper gives each approved host mount a separate
virtiofs device. The guest mounts only the targets in the host-owned,
read-only run manifest.
The workspace Git hooks target has an extra read-only overlay in both the
supervisor export and the agent container.
For a normal Git checkout, Cooper creates a missing `.git/hooks` directory
before it adds this overlay. Cooper rejects a symbolic-link `.git` or
`.git/hooks` path because Docker can follow the link before it adds the
read-only overlay.

Cooper embeds and verifies a static `virtiofsd` 1.14.0 executable. Each
`virtiofsd` process runs as the invoking user with no capabilities. Soft UID
and GID translation maps all guest identities, including guest root, to that
one host identity. Thus, guest root cannot create root-owned files on a host
mount or use a host identity that Cooper did not authorize. Read-only exports
also use `virtiofsd`'s server-side read-only mode.

The guest Docker daemon uses a guest-local Unix socket. The selected agent
container receives this socket. Control of this socket is equivalent to guest
root, which is intentional. The socket cannot control physical-host Docker.

## Network Boundary

The VM has no emulated or passed-through network interface. This is stronger
than a guest firewall rule because guest root can change a firewall, but it
cannot create a QEMU device that does not exist.

Guest traffic uses a virtio serial channel and a bounded protocol:

```text
guest process or container
  -> guest service listener
  -> virtio serial channel
  -> networkless supervisor
  -> Unix socket
  -> minimal VM relay
  -> Cooper Squid proxy, bridge, or configured host port
```

The service header selects a symbolic service. It does not contain a network
destination. The relay loads the host-written allow-map for each new stream
and rejects an unknown service or disabled port. If the relay, proxy, or
control channel stops, new traffic fails closed.

Squid still controls internet access. Ports 80 and 443 are only eligible
transport ports. A destination must also be in the static domain allowlist or
receive explicit approval. Grok inference traffic stays TLS-inspected because
it has a path allowlist. Other static domains use end-to-end TLS after Squid
checks the CONNECT destination. This permits fresh Docker build stages to use
their normal public certificate store.

The guest Docker daemon receives the same proxy route. Cooper writes proxy
configuration for pulls and adds predefined proxy arguments to Docker build
commands. A nested container has no direct route because the guest has no
external network interface.

## Host Data

CLI and VM mode use one mount-plan builder. A VM mounts only:

- The current workspace at the same absolute path, read-write.
- The workspace `.git/hooks` directory, read-only.
- The complete state roots of the selected agent, read-write.
- The configured read-only host identity files.
- Cooper-owned caches, clipboard files, generated configuration, and one
  per-runtime temporary directory.

These mounts are intentional authority. Malicious code can delete or change
any read-write mounted data. Use version control and backups for important
data. Cooper does not copy agent state into Cooper-owned storage, and
`cooper cleanup` must not remove a host-owned agent state root.

The agent container also receives `/home/user/.cooper` from the guest file
system. This directory is guest-local. It supports Cooper self-development
and nested VM assets. It is not the physical host `~/.cooper` directory.

## Nested Virtualization

A depth-1 Cooper VM receives a virtual KVM device and the host CPU
virtualization feature. This is necessary because Cooper development must be
able to start and test a second Cooper VM.

This authority has an important limit: depth-1 guest root can run a VMM
directly instead of using Cooper. Cooper can limit managed nesting, but it
cannot force all KVM users in that guest to use Cooper. The physical boundary
is still the outer QEMU process and its approved mounts.

A managed depth-2 VM receives neither `/dev/kvm` nor the `vmx` or `svm` CPU
feature. Cooper rejects a depth-3 request before it starts resources. Two
levels are the supported limit.

## Credentials and Clipboard

Only the selected agent state roots enter the VM. The same roots enter
`cooper cli`, so a user can continue an agent session on the host or in either
Cooper mode. Do not run one mutable conversation from two processes at the
same time.

Clipboard data is user-staged, has a time limit, and uses a random token for
one runtime. Cooper revokes the token before VM shutdown and rotates it on VM
restart. The VM relay does not receive clipboard content as a file mount.

## Integrity and Cleanup

Cooper pins the Ubuntu cloud image, Docker archive, infrastructure base image,
snapshot date, and package versions. Downloads have exact size and SHA-256
checks. The prepared base has an independent schema and metadata file. Each
VM uses an ephemeral qcow2 overlay. Agent images move through a verified
Docker archive and must keep the expected image ID after guest import.

Runtime names and paths use a namespace and stable identity. Stop and cleanup
operations require Cooper-owned metadata and validate the removal path.
Shutdown revokes the clipboard token, asks the guest to stop, removes the
supervisor and relay, disconnects the proxy, removes the private network, and
then removes transient VM files. Prepared assets and image archives remain
until an explicit Cooper cache cleanup.

## Remaining Risks

The VM reduces risk. It is not a complete security guarantee.

- A QEMU, KVM, Linux kernel, virtio, virtiofsd, Docker, or CPU vulnerability
  can cross an intended boundary.
- Guest code can consume CPU, memory, disk space, process slots, proxy
  capacity, and allowed network quotas. Resource limits reduce this denial of
  service risk but do not remove it.
- Shared CPU hardware has side-channel risks. Cooper does not claim
  protection from microarchitectural attacks.
- The agent can destroy or leak data that the user intentionally mounted or
  send it to an approved destination.
- An execution-bridge script or forwarded host service can grant more host
  authority than its name suggests. Review these settings as carefully as a
  shell command.
- At depth 1, direct KVM access lets guest root start an unmanaged inner VMM.

Keep the host kernel, Docker Engine, and CPU firmware current. Treat the
domain allowlist, bridge routes, port forwards, and mounted directories as
security policy.
