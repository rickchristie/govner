package vm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/vmhost"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

const preparedMetadataSchema = 9

type preparedMetadata struct {
	Schema         int    `json:"schema"`
	AssetSchema    string `json:"asset_schema"`
	UbuntuSHA256   string `json:"ubuntu_sha256"`
	DockerSHA256   string `json:"docker_sha256"`
	ImageSchema    string `json:"infrastructure_image_schema"`
	PreparedSHA256 string `json:"prepared_sha256"`
	PreparedSize   int64  `json:"prepared_size"`
}

// Preparer creates the immutable no-NIC guest base.
type Preparer struct {
	CooperDir  string
	Prefix     string
	Runner     CommandRunner
	Out        io.Writer
	Client     AssetClient
	Executable string
}

// AssetClient lets tests replace network asset preparation.
type AssetClient interface {
	Ensure(ctx context.Context, asset Asset) (string, error)
}

// PreparedGuestValid verifies an existing base without preparing or downloading
// anything. Tests can reuse a host base after this full content check.
func PreparedGuestValid(cooperDir string) bool {
	return preparedGuestValid(AssetDir(cooperDir))
}

// Prepare is safe to call at the same time from multiple Cooper processes.
func (p Preparer) Prepare(ctx context.Context) (string, error) {
	if err := HostRequirements(true); err != nil {
		return "", err
	}
	assetDir := AssetDir(p.CooperDir)
	if err := os.MkdirAll(assetDir, 0o700); err != nil {
		return "", fmt.Errorf("create VM asset directory: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(assetDir, ".prepare.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", fmt.Errorf("open VM preparation lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock VM preparation: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	// The same lock protects the shared infrastructure build context and the
	// prepared image. Concurrent prepare commands must not replace each
	// other's helper binary or Dockerfile during a build.
	if err := (InfrastructureImages{CooperDir: p.CooperDir, Prefix: p.Prefix, Runner: p.Runner, Out: p.Out, Executable: p.Executable}).Ensure(ctx); err != nil {
		return "", err
	}
	if preparedGuestValid(assetDir) {
		return PreparedGuestPath(p.CooperDir), nil
	}
	// A copied, schema-matched base is sufficient for nested Cooper. Download
	// the large source assets only when Cooper must create a new base.
	store := p.Client
	if store == nil {
		store = AssetStore{Dir: assetDir}
	}
	ubuntuPath, err := store.Ensure(ctx, UbuntuGuestAsset)
	if err != nil {
		return "", err
	}
	dockerPath, err := store.Ensure(ctx, DockerEngineAsset)
	if err != nil {
		return "", err
	}
	return p.prepareLocked(ctx, ubuntuPath, dockerPath)
}

func (p Preparer) prepareLocked(ctx context.Context, ubuntuPath, dockerPath string) (string, error) {
	runner := runnerOrSystem(p.Runner)
	assetDir := AssetDir(p.CooperDir)
	runtimeDir, err := os.MkdirTemp(filepath.Join(p.CooperDir, "vm"), ".prepare-*")
	if err != nil {
		return "", fmt.Errorf("create VM preparation directory: %w", err)
	}
	removeRuntimeDir := false
	defer func() {
		if removeRuntimeDir {
			_ = os.RemoveAll(runtimeDir)
		}
	}()
	exportDir := filepath.Join(runtimeDir, "exports")
	if err := os.MkdirAll(exportDir, 0o700); err != nil {
		return "", err
	}
	nonce, err := randomHex(16)
	if err != nil {
		return "", err
	}
	marker := "prepared-" + nonce + ".ok"
	userData := guestProvisionCloudConfig(marker)
	for name, data := range map[string]string{
		"user-data":      userData,
		"meta-data":      "instance-id: cooper-vm-prepare-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "\nlocal-hostname: cooper-vm-prepare\n",
		"network-config": "version: 2\nethernets: {}\n",
	} {
		if err := os.WriteFile(filepath.Join(runtimeDir, name), []byte(data), 0o600); err != nil {
			return "", fmt.Errorf("write VM preparation %s: %w", name, err)
		}
	}
	uid, gid, kvmGID, err := currentIDs()
	if err != nil {
		return "", err
	}
	image := SupervisorImageName(p.Prefix)
	seccompPath, err := docker.EnsureSeccompProfile(p.CooperDir)
	if err != nil {
		return "", fmt.Errorf("prepare VM supervisor seccomp profile: %w", err)
	}
	baseDockerArgs := supervisorSecurityArgs(uid, gid, kvmGID, 2, 3072, seccompPath)
	seedArgs := append([]string{"run", "--rm"}, baseDockerArgs...)
	seedArgs = append(seedArgs,
		"--mount", workload.DockerBindMount(runtimeDir, "/cooper/runtime", false),
		"--entrypoint", "cloud-localds", image,
		"--network-config=/cooper/runtime/network-config",
		"/cooper/runtime/seed.img", "/cooper/runtime/user-data", "/cooper/runtime/meta-data",
	)
	if err := runner.Run(ctx, nil, p.output(), p.output(), "docker", seedArgs...); err != nil {
		return "", fmt.Errorf("create VM preparation seed: %w", err)
	}
	overlay := filepath.Join(assetDir, ".prepared-overlay.qcow2")
	partial := filepath.Join(assetDir, ".prepared-guest.qcow2.part")
	for _, path := range []string{overlay, partial, filepath.Join(exportDir, marker)} {
		_ = os.Remove(path)
	}
	config := vmhost.SupervisorConfig{
		Mode: vmhost.ModePrepare, Name: "cooper-vm-prepare",
		BaseImage: "/cooper/assets/" + filepath.Base(ubuntuPath),
		Overlay:   "/cooper/assets/" + filepath.Base(overlay),
		SeedImage: "/cooper/runtime/seed.img",
		ExportDir: "/cooper/exports", RuntimeDir: "/cooper/runtime",
		CPUs: 2, MemoryMiB: 3072, DiskGiB: 8, Depth: 1, PrepareMarker: marker,
	}
	if err := writeJSON(filepath.Join(runtimeDir, "supervisor.json"), config, 0o600); err != nil {
		return "", err
	}
	prepareName := preparationContainerName(p.CooperDir, p.Prefix)
	runArgs := append([]string{"run", "--rm", "--name", prepareName,
		"--label", "cooper.kind=vm-preparer",
		"--label", "cooper.prepare-owner=" + prepareName,
	}, baseDockerArgs...)
	runArgs = append(runArgs,
		"--mount", workload.DockerBindMount(assetDir, "/cooper/assets", false),
		"--mount", workload.DockerBindMount(runtimeDir, "/cooper/runtime", false),
		"--mount", workload.DockerBindMount(exportDir, "/cooper/exports", false),
		"--mount", workload.DockerBindMount(dockerPath, "/cooper/exports/docker.tgz", true),
		image, "--config-file", "/cooper/runtime/supervisor.json",
	)
	if err := runner.Run(ctx, nil, p.output(), p.output(), "docker", runArgs...); err != nil {
		return "", fmt.Errorf("prepare no-NIC VM guest: %w; logs remain in %s", err, runtimeDir)
	}
	convertArgs := append([]string{"run", "--rm"}, baseDockerArgs...)
	convertArgs = append(convertArgs,
		"--mount", workload.DockerBindMount(assetDir, "/cooper/assets", false),
		"--entrypoint", "qemu-img", image,
		"convert", "-p", "-O", "qcow2",
		"/cooper/assets/"+filepath.Base(overlay), "/cooper/assets/"+filepath.Base(partial),
	)
	if err := runner.Run(ctx, nil, p.output(), p.output(), "docker", convertArgs...); err != nil {
		return "", fmt.Errorf("flatten prepared VM guest: %w", err)
	}
	checkArgs := append([]string{"run", "--rm"}, baseDockerArgs...)
	checkArgs = append(checkArgs,
		"--mount", workload.DockerBindMount(assetDir, "/cooper/assets", false),
		"--entrypoint", "qemu-img", image,
		"check", "/cooper/assets/"+filepath.Base(partial),
	)
	if err := runner.Run(ctx, nil, p.output(), p.output(), "docker", checkArgs...); err != nil {
		return "", fmt.Errorf("check prepared VM guest: %w", err)
	}
	final := PreparedGuestPath(p.CooperDir)
	if err := os.Chmod(partial, 0o444); err != nil {
		return "", err
	}
	if err := os.Rename(partial, final); err != nil {
		return "", fmt.Errorf("install prepared VM guest: %w", err)
	}
	info, err := os.Stat(final)
	if err != nil {
		return "", fmt.Errorf("inspect prepared VM guest: %w", err)
	}
	preparedSHA, err := digestFile(final)
	if err != nil {
		return "", fmt.Errorf("hash prepared VM guest: %w", err)
	}
	metadata := expectedPreparedMetadata(preparedSHA, info.Size())
	if err := writeJSON(final+".json", metadata, 0o444); err != nil {
		return "", err
	}
	_ = os.Remove(overlay)
	removeRuntimeDir = true
	return final, nil
}

func preparedGuestValid(assetDir string) bool {
	path := filepath.Join(assetDir, "cooper-guest-base.qcow2")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Mode().Perm()&0o222 != 0 {
		return false
	}
	data, err := os.ReadFile(path + ".json")
	if err != nil {
		return false
	}
	var got preparedMetadata
	if json.Unmarshal(data, &got) != nil {
		return false
	}
	if got != expectedPreparedMetadata(got.PreparedSHA256, got.PreparedSize) || got.PreparedSize != info.Size() || !validSHA256(got.PreparedSHA256) {
		return false
	}
	digest, err := digestFile(path)
	return err == nil && digest == got.PreparedSHA256
}

func expectedPreparedMetadata(preparedSHA256 string, preparedSize int64) preparedMetadata {
	return preparedMetadata{
		Schema: preparedMetadataSchema, AssetSchema: AssetSchema,
		UbuntuSHA256: UbuntuGuestAsset.SHA256, DockerSHA256: DockerEngineAsset.SHA256,
		ImageSchema: infrastructureImageSchema, PreparedSHA256: preparedSHA256, PreparedSize: preparedSize,
	}
}

func preparationContainerName(cooperDir, prefix string) string {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(filepath.Clean(cooperDir)+"\x00"+prefix)))[:12]
	return "cooper-vm-prepare-" + digest
}

func (p Preparer) output() io.Writer {
	if p.Out != nil {
		return p.Out
	}
	return os.Stderr
}

func supervisorSecurityArgs(uid, gid, kvmGID, cpus, memoryMiB int, seccompPath string) []string {
	return []string{
		"--network", "none",
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
		"--security-opt", "seccomp=" + seccompPath,
		"--device", "/dev/kvm",
		"--group-add", strconv.Itoa(kvmGID),
		"--user", fmt.Sprintf("%d:%d", uid, gid),
		"--read-only",
		"--tmpfs", "/tmp:rw,nosuid,nodev,noexec,mode=1777",
		"--tmpfs", "/run:rw,nosuid,nodev,mode=0755",
		"--pids-limit", "1024",
		"--cpus", strconv.Itoa(cpus),
		"--memory", strconv.Itoa(memoryMiB+768) + "m",
		"--init",
	}
}

func guestProvisionCloudConfig(marker string) string {
	return `#cloud-config
ssh_pwauth: false
disable_root: true
write_files:
  - path: /etc/systemd/system/run-cooper-host.mount
    owner: root:root
    permissions: '0644'
    content: |
      [Unit]
      Description=Cooper host exports
      Before=cooper-vm-guest.service

      [Mount]
      What=cooper-host
      Where=/run/cooper/host
      Type=virtiofs
      Options=rw

      [Install]
      WantedBy=multi-user.target
  - path: /etc/systemd/system/cooper-vm-guest.service
    owner: root:root
    permissions: '0644'
    content: |
      [Unit]
      Description=Cooper VM guest service
      Requires=run-cooper-host.mount
      After=run-cooper-host.mount

      [Service]
      Type=simple
      ExecStartPre=/usr/bin/install -D -m 0555 /run/cooper/host/control/cooper /usr/local/libexec/cooper-vm-guest-runtime
      ExecStart=/usr/local/libexec/cooper-vm-guest-runtime __vm-guest --manifest /run/cooper/host/control/manifest.json
      Restart=no
      StandardOutput=append:/dev/virtio-ports/org.cooper.log
      StandardError=inherit

      [Install]
      WantedBy=multi-user.target
runcmd:
  - [ mkdir, -p, /run/cooper/host ]
  - [ mount, -t, virtiofs, cooper-host, /run/cooper/host ]
  - [ sh, -c, 'tar -xzf /run/cooper/host/docker.tgz --strip-components=1 -C /usr/local/bin' ]
  - [ chmod, '0555', /usr/local/bin/docker, /usr/local/bin/dockerd, /usr/local/bin/containerd, /usr/local/bin/containerd-shim-runc-v2, /usr/local/bin/runc ]
  - [ systemctl, enable, run-cooper-host.mount, cooper-vm-guest.service ]
  - [ sh, -c, 'for unit in systemd-networkd-wait-online.service NetworkManager-wait-online.service ssh.service ssh.socket ModemManager.service pollinate.service apport.service apt-daily.service apt-daily-upgrade.service apt-daily.timer apt-daily-upgrade.timer fwupd-refresh.service fwupd-refresh.timer update-notifier-download.timer update-notifier-motd.timer motd-news.timer snapd.service snapd.socket; do systemctl mask "$unit" || true; done' ]
  - [ sh, -c, 'rm -f /etc/ssh/ssh_host_* /var/lib/dbus/machine-id' ]
  - [ sh, -c, ': > /etc/machine-id' ]
  - [ sh, -c, 'sed -i "/^LABEL=BOOT[[:space:]]/d;/^LABEL=UEFI[[:space:]]/d" /etc/fstab' ]
  - [ touch, /etc/cloud/cloud-init.disabled ]
  - [ sh, -c, 'touch /run/cooper/host/` + marker + `' ]
  - [ sync ]
  - [ poweroff, -f ]
`
}

func writeJSON(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".cooper-json-*.part")
	if err != nil {
		return fmt.Errorf("create temporary JSON for %s: %w", path, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary JSON for %s: %w", path, err)
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("set JSON mode for %s: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync JSON for %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close JSON for %s: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
