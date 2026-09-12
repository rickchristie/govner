package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupervisorSecurityArgsHaveNoNetworkOrBroadPrivilege(t *testing.T) {
	t.Parallel()
	args := strings.Join(supervisorSecurityArgs(1000, 1000, 108, 4, 4096, "/state/seccomp.json"), " ")
	for _, want := range []string{"--network none", "--cap-drop=ALL", "--security-opt=no-new-privileges", "seccomp=/state/seccomp.json", "--device /dev/kvm", "--read-only", "--user 1000:1000"} {
		if !strings.Contains(args, want) {
			t.Fatalf("supervisor arguments do not contain %q: %s", want, args)
		}
	}
	for _, forbidden := range []string{"--privileged", "/var/run/docker.sock", "--network host", "/dev/vhost"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("supervisor arguments contain %q: %s", forbidden, args)
		}
	}
}

func TestWriteJSONAtomicallyReplacesReadOnlyFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("old\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(path, map[string]int{"schema": 2}, 0o444); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\n  \"schema\": 2\n}\n" {
		t.Fatalf("JSON = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o444 {
		t.Fatalf("mode = %04o", info.Mode().Perm())
	}
}

func TestGuestProvisionHasNoNetworkSetup(t *testing.T) {
	t.Parallel()
	config := guestProvisionCloudConfig("prepared.ok")
	for _, want := range []string{
		"Type=virtiofs", "cooper-vm-guest.service", "docker.tgz", "poweroff, -f", "prepared.ok",
		"StandardOutput=append:/dev/virtio-ports/org.cooper.log", "systemctl mask", "systemd-networkd-wait-online.service",
		"cloud-init.disabled", ": > /etc/machine-id", "ExecStartPre=/usr/bin/install -D -m 0555",
		"ExecStart=/usr/local/libexec/cooper-vm-guest-runtime", "^LABEL=BOOT", "^LABEL=UEFI",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("cloud config does not contain %q", want)
		}
	}
	for _, forbidden := range []string{"apt-get", "curl ", "wget ", "ssh_authorized_keys"} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("cloud config contains network or credential action %q", forbidden)
		}
	}
}

func TestPreparedGuestValidationChecksContentDigest(t *testing.T) {
	t.Parallel()
	assetDir := t.TempDir()
	path := filepath.Join(assetDir, "cooper-guest-base.qcow2")
	if err := os.WriteFile(path, []byte("valid prepared guest"), 0o444); err != nil {
		t.Fatal(err)
	}
	digest, err := digestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(path+".json", expectedPreparedMetadata(digest, info.Size()), 0o444); err != nil {
		t.Fatal(err)
	}
	if !preparedGuestValid(assetDir) {
		t.Fatal("preparedGuestValid() rejected matching content and metadata")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("other prepared guest"), 0o444); err != nil {
		t.Fatal(err)
	}
	if preparedGuestValid(assetDir) {
		t.Fatal("preparedGuestValid() accepted changed content with the same size")
	}
}

func TestPreparationContainerNameIsScopedToConfiguration(t *testing.T) {
	t.Parallel()
	first := preparationContainerName("/home/user/.cooper-one", "test-")
	again := preparationContainerName("/home/user/.cooper-one", "test-")
	other := preparationContainerName("/home/user/.cooper-two", "test-")
	if first != again || first == other || !strings.HasPrefix(first, "cooper-vm-prepare-") {
		t.Fatalf("preparation container names = %q, %q, %q", first, again, other)
	}
}
