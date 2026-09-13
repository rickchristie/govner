package vmhost

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/vmproto"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func TestSupervisorAcceptsCompleteMountCatalog(t *testing.T) {
	for _, count := range []int{25, vmproto.MaxGuestMounts, vmproto.MaxGuestMounts + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			config := validSupervisorConfig()
			for index := range count {
				config.Mounts = append(config.Mounts, MountExport{
					Tag: fmt.Sprintf("cooper-m-%03d", index), Path: fmt.Sprintf("/cooper/mounts/%03d", index),
				})
			}
			err := config.Validate()
			if count > vmproto.MaxGuestMounts {
				if err == nil {
					t.Fatal("accepted more mounts than the guest supports")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertMountDeviceSlots(t, config)
		})
	}
}

func TestSupervisorAcceptsSharedOpenCodeMountPlan(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.ProgrammingTools = []config.ToolConfig{{Name: "go", Enabled: true}, {Name: "node", Enabled: true}, {Name: "python", Enabled: true}}
	input := workload.MountInput{
		HomeDir: filepath.Join(root, "home"), CooperDir: filepath.Join(root, "cooper"),
		WorkspaceDir: filepath.Join(root, "workspace"), RuntimeID: "test-opencode", ToolName: "opencode", Config: cfg,
		Environment: map[string]string{
			"OPENCODE_CONFIG_DIR": filepath.Join(root, "extra-config"),
			"OPENCODE_CONFIG":     filepath.Join(root, "settings", "opencode.json"),
			"OPENCODE_DB":         filepath.Join(root, "database", "opencode.db"),
		},
	}
	for _, path := range []string{filepath.Join(input.HomeDir, ".gitconfig"),
		input.Environment["OPENCODE_CONFIG"], input.Environment["OPENCODE_DB"],
		filepath.Join(input.CooperDir, "ca", "cooper-ca.pem"),
		filepath.Join(input.CooperDir, "tokens", input.RuntimeID),
		filepath.Join(input.CooperDir, "session", input.RuntimeID, "cooper-localtime")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{filepath.Join(input.WorkspaceDir, ".git", "hooks"),
		filepath.Join(input.CooperDir, "base", "shims"), filepath.Join(input.CooperDir, "live")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := workload.EnsureDirectories(input); err != nil {
		t.Fatal(err)
	}
	mounts, err := workload.BuildMountPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) < 25 {
		t.Fatalf("fixture did not cover the full OpenCode plan: %d mounts", len(mounts))
	}
	supervisor := validSupervisorConfig()
	for index := range mounts {
		supervisor.Mounts = append(supervisor.Mounts, MountExport{Tag: fmt.Sprintf("cooper-m-%03d", index), Path: fmt.Sprintf("/cooper/mounts/%03d", index)})
	}
	if err := supervisor.Validate(); err != nil {
		t.Fatal(err)
	}
	assertMountDeviceSlots(t, supervisor)
}

func assertMountDeviceSlots(t *testing.T, config SupervisorConfig) {
	t.Helper()
	args := BuildQEMUArgs(config, "/cooper/runtime/fs.sock", "")
	buses := map[string]bool{}
	addresses := map[string]bool{}
	devices := 0
	for index, arg := range args {
		if arg != "-device" {
			continue
		}
		fields := strings.Split(args[index+1], ",")
		properties := map[string]string{}
		for _, field := range fields[1:] {
			name, value, _ := strings.Cut(field, "=")
			properties[name] = value
		}
		if fields[0] == "pcie-pci-bridge" {
			buses[properties["id"]] = true
		}
		if fields[0] != "vhost-user-fs-pci" || properties["tag"] == "cooper-host" {
			continue
		}
		bus := properties["bus"]
		address := bus + ":" + properties["addr"]
		slot, err := strconv.ParseUint(properties["addr"], 0, 8)
		if !buses[bus] || err != nil || slot < 1 || slot > 31 || addresses[address] {
			t.Fatalf("mount has no unique PCI slot: %s", args[index+1])
		}
		addresses[address] = true
		devices++
	}
	if devices != len(config.Mounts) || len(buses) > 3 {
		t.Fatalf("device layout: %d mounts, %d devices, %d bridges", len(config.Mounts), devices, len(buses))
	}
}

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
