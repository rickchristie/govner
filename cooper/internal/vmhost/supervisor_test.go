package vmhost

import (
	"strings"
	"testing"
)

func validSupervisorConfig() SupervisorConfig {
	return SupervisorConfig{
		Mode: ModeRuntime, Name: "cooper-vm-test", BaseImage: "/cooper/assets/base.qcow2",
		Overlay: "/cooper/runtime/disk.qcow2", ExportDir: "/cooper/exports",
		RuntimeDir: "/cooper/runtime", ControlDir: "/cooper/control", RelaySocket: "/cooper/relay/relay.sock",
		Nonce: strings.Repeat("a", 64), CPUs: 4, MemoryMiB: 4096, DiskGiB: 16, Depth: 1,
	}
}

func TestBuildQEMUArgsHasNoNetworkAndExplicitDevices(t *testing.T) {
	t.Parallel()
	args := BuildQEMUArgs(validSupervisorConfig(), "/cooper/runtime/fs.sock", "vendor_id : AuthenticAMD")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-nic none", "-nodefaults", "-no-user-config", "vhost-user-fs-pci", "virtserialport", "-sandbox on,",
		"name=org.cooper.gateway", "name=org.cooper.log", "file,id=guestlog",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("QEMU arguments do not contain %q: %s", want, joined)
		}
	}
	for _, forbidden := range []string{"-net ", "user,id=", "tap,id=", "-device e1000", "-device virtio-net"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("QEMU arguments contain network device %q: %s", forbidden, joined)
		}
	}
}

func TestCPUModelMasksNestedVirtualizationAtDepthTwo(t *testing.T) {
	t.Parallel()
	if got := CPUModel(1, "vendor_id : AuthenticAMD"); got != "host" {
		t.Fatalf("depth-1 CPU = %q", got)
	}
	if got := CPUModel(2, "vendor_id : AuthenticAMD"); got != "host,-svm" {
		t.Fatalf("AMD depth-2 CPU = %q", got)
	}
	if got := CPUModel(2, "vendor_id : GenuineIntel"); got != "host,-vmx" {
		t.Fatalf("Intel depth-2 CPU = %q", got)
	}
}

func TestSupervisorConfigValidation(t *testing.T) {
	t.Parallel()
	config := validSupervisorConfig()
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.Depth = 3
	if err := config.Validate(); err == nil {
		t.Fatal("expected depth rejection")
	}
	config = validSupervisorConfig()
	config.Mounts = []MountExport{{Tag: "workspace", Path: "/cooper/mounts/workspace", CachePolicy: "unsafe"}}
	if err := config.Validate(); err == nil {
		t.Fatal("expected cache policy rejection")
	}
}

func TestSupervisorConfigRejectsPathsOutsideFixedMounts(t *testing.T) {
	t.Parallel()
	tests := []func(*SupervisorConfig){
		func(config *SupervisorConfig) { config.BaseImage = "/etc/passwd" },
		func(config *SupervisorConfig) { config.Overlay = "/cooper/control/disk.qcow2" },
		func(config *SupervisorConfig) { config.ControlDir = "/tmp/control" },
		func(config *SupervisorConfig) { config.RelaySocket = "/tmp/relay.sock" },
		func(config *SupervisorConfig) { config.Nonce = "not-a-nonce" },
	}
	for _, change := range tests {
		config := validSupervisorConfig()
		change(&config)
		if err := config.Validate(); err == nil {
			t.Fatalf("Validate() accepted unsafe config: %#v", config)
		}
	}
}

func TestVirtioFSDUsesAlwaysCacheOnlyWhenAuthorized(t *testing.T) {
	t.Parallel()
	without := strings.Join(virtioFSDArgs("/share", "/run/fs.sock", "", false, 1234, 5678), " ")
	with := strings.Join(virtioFSDArgs("/share", "/run/fs.sock", CacheAlways, true, 1234, 5678), " ")
	if !strings.Contains(without, "--cache=auto") {
		t.Fatalf("default virtiofsd arguments do not support normal mmap: %s", without)
	}
	if !strings.Contains(with, "--cache=always") {
		t.Fatalf("scratch virtiofsd arguments do not permit mmap: %s", with)
	}
	for _, want := range []string{
		"--translate-uid=squash-guest:0:1234:4294967295",
		"--translate-gid=squash-guest:0:5678:4294967295",
		"--readonly",
	} {
		if !strings.Contains(with, want) {
			t.Fatalf("virtiofsd arguments do not contain %q: %s", want, with)
		}
	}
	if strings.Contains(without, "--readonly") {
		t.Fatalf("writable virtiofsd export is read-only: %s", without)
	}
}
