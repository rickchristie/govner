# Cooper VM host setup

Run the setup script before you develop or test Cooper VM support:

```bash
./cooper/dev/setup.sh
```

Run it as your normal user. The script requests `sudo` only for the host changes that require it. If it adds your user to the `kvm` group, log out and log in once. Then verify the result:

```bash
./cooper/dev/setup.sh --check
```

The script currently supports Ubuntu 22.04 or later on x86-64. It installs:

- QEMU/KVM x86 system emulation and image tools.
- QEMU GTK and SPICE support for a later GUI mode.
- OVMF UEFI firmware.
- `virtiofsd` for selected-agent and workspace mounts. Ubuntu 22.04 provides it through `qemu-system-common`; newer Ubuntu releases provide a separate package.
- Cloud image seed tools.
- `remote-viewer` for a separate GUI display process.
- Bubblewrap for host process isolation.
- KVM checks and `socat` for development diagnostics.

The script adds only the invoking user to the `kvm` group. It creates `/etc/modules-load.d/cooper-vm.conf` and loads the CPU-specific KVM module plus `vhost_vsock`.

The script does not install or start libvirt. It does not create a VM, create a network, change firewall rules, enable IP forwarding, or install Docker. Cooper continues to use its existing host Docker installation for the Cooper proxy. A future `cooper vm` implementation will create and provision the guest, including the guest-local Docker daemon.

The installed tools do not weaken the network boundary by themselves. The `cooper vm` launcher must still run QEMU and `virtiofsd` with least privilege, give the guest no external network interface, and route all guest traffic through the host Cooper proxy.
