# Cooper VM host setup

Run the setup script before you develop or test Cooper VM support:

```bash
./cooper/dev/setup.sh
```

Run it as your normal user. The script uses `sudo` only to configure and load
the CPU KVM module and to add your user to the `kvm` group. Its KVM module
option enables nested virtualization after the next module load or host
restart. If it changes the group, log out and log in once. Then verify the
host:

```bash
./cooper/dev/setup.sh --check
```

The supported host is Linux on x86-64. It must have Docker Engine, hardware
virtualization, `/dev/kvm` access, and nested KVM. Nested KVM is necessary for
the release test that starts `cooper vm` from inside `cooper vm`.

Cooper runs QEMU and cloud-image tools in pinned Cooper-owned containers. It
includes a checksum-locked static `virtiofsd` helper. You do not need these
tools on the host. The smaller host requirement set prevents host package
versions from changing the VM result. Cooper does not need OVMF, libvirt,
`/dev/net/tun`, `vhost_vsock`, a GUI viewer, or a host virtual network.

The setup script does not install Docker, create a VM, change firewall rules,
enable IP forwarding, or create a network. The VM supervisor has no Docker
network and receives only `/dev/kvm`. The guest has no network device. Its
approved traffic uses the Cooper relay and proxy.

Prepare the pinned guest base once:

```bash
go build -C ./cooper -o ./cooper .
./cooper/cooper vm prepare
```

Preparation downloads exact Ubuntu and Docker assets. Cooper checks their
sizes and SHA-256 values before use. The result is under
`~/.cooper/vm/assets/schema-1/`.

Run the complete VM gate from the repository root:

```bash
timeout 90m ./cooper/test-vm.sh
```

The gate needs nested KVM. It starts a depth-1 VM, builds and tests Cooper
inside it, and starts one depth-2 VM. It also runs the shared CLI/VM feature
matrix for all built-in agents. The test uses isolated runtime names and does
not need provider credentials. Its default logs are `/tmp/cooper-vm.txt` and
`/tmp/cooper-vm-prepare.txt`.

Use `./cooper/test-vm.sh clean` after an interrupted run. Use `cooper vm
doctor` to report host access, image and asset state, configured resources,
and running guest health.
