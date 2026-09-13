package vmhost

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

const (
	ModePrepare = "prepare"
	ModeRuntime = "runtime"
)

var supervisorName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// SupervisorConfig is the validated contract read inside the networkless
// supervisor container. All paths refer to fixed container mount points.
type SupervisorConfig struct {
	Mode          string        `json:"mode"`
	Name          string        `json:"name"`
	BaseImage     string        `json:"base_image"`
	Overlay       string        `json:"overlay"`
	SeedImage     string        `json:"seed_image,omitempty"`
	ExportDir     string        `json:"export_dir"`
	RuntimeDir    string        `json:"runtime_dir"`
	ControlDir    string        `json:"control_dir,omitempty"`
	RelaySocket   string        `json:"relay_socket,omitempty"`
	Nonce         string        `json:"nonce,omitempty"`
	CPUs          int           `json:"cpus"`
	MemoryMiB     int           `json:"memory_mib"`
	DiskGiB       int           `json:"disk_gib"`
	Depth         int           `json:"depth"`
	PrepareMarker string        `json:"prepare_marker,omitempty"`
	Mounts        []MountExport `json:"mounts,omitempty"`
}

// MountExport is one host-authorized source exposed as one virtiofs device.
type MountExport struct {
	Tag         string `json:"tag"`
	Path        string `json:"path"`
	CachePolicy string `json:"cache_policy,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
}

const (
	CacheAuto   = "auto"
	CacheAlways = "always"
)

// LoadSupervisorConfig decodes a strict supervisor file.
func LoadSupervisorConfig(path string) (SupervisorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SupervisorConfig{}, fmt.Errorf("read VM supervisor config: %w", err)
	}
	var config SupervisorConfig
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return SupervisorConfig{}, fmt.Errorf("decode VM supervisor config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected data after JSON object")
		}
		return SupervisorConfig{}, fmt.Errorf("decode VM supervisor config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return SupervisorConfig{}, err
	}
	return config, nil
}

// Validate rejects paths outside the three fixed supervisor mount trees.
func (c SupervisorConfig) Validate() error {
	if c.Mode != ModePrepare && c.Mode != ModeRuntime {
		return fmt.Errorf("invalid VM supervisor mode %q", c.Mode)
	}
	if !supervisorName.MatchString(c.Name) {
		return errors.New("VM supervisor name is invalid")
	}
	if c.CPUs < 1 || c.CPUs > 128 || c.MemoryMiB < 1024 || c.DiskGiB < 4 {
		return errors.New("VM supervisor resources are invalid")
	}
	if c.Depth < 1 || c.Depth > 2 {
		return fmt.Errorf("VM supervisor depth %d is outside 1-2", c.Depth)
	}
	if c.ExportDir != "/cooper/exports" || c.RuntimeDir != "/cooper/runtime" {
		return errors.New("VM supervisor export or runtime directory does not match its fixed mount")
	}
	if !cleanChildPath("/cooper/assets", c.BaseImage) {
		return errors.New("VM supervisor base image is outside its fixed asset mount")
	}
	if c.Mode == ModePrepare {
		if !cleanChildPath("/cooper/assets", c.Overlay) || c.SeedImage != "/cooper/runtime/seed.img" ||
			c.PrepareMarker == "" || c.PrepareMarker == "." || c.PrepareMarker == ".." || filepath.Base(c.PrepareMarker) != c.PrepareMarker {
			return errors.New("VM preparation seed and marker are required")
		}
		return nil
	}
	if c.Overlay != "/cooper/runtime/disk.qcow2" || c.ControlDir != "/cooper/control" || c.RelaySocket != "/cooper/relay/relay.sock" {
		return errors.New("VM runtime paths do not match the fixed supervisor mounts")
	}
	if len(c.Mounts) > vmproto.MaxGuestMounts {
		return fmt.Errorf("VM supervisor has %d mounts; the maximum is %d", len(c.Mounts), vmproto.MaxGuestMounts)
	}
	seenTags := make(map[string]bool, len(c.Mounts))
	seenPaths := make(map[string]bool, len(c.Mounts))
	for _, mount := range c.Mounts {
		cleanPath := filepath.Clean(mount.Path)
		relative, err := filepath.Rel("/cooper/mounts", cleanPath)
		if mount.Tag == "" || strings.ContainsAny(mount.Tag, "/ ,=") || !filepath.IsAbs(mount.Path) ||
			err != nil || relative == "." || strings.HasPrefix(relative, "..") || seenTags[mount.Tag] || seenPaths[cleanPath] {
			return fmt.Errorf("VM supervisor mount tag %q is invalid or duplicated", mount.Tag)
		}
		if mount.CachePolicy != "" && mount.CachePolicy != CacheAuto && mount.CachePolicy != CacheAlways {
			return fmt.Errorf("VM supervisor mount %q has invalid cache policy %q", mount.Tag, mount.CachePolicy)
		}
		seenTags[mount.Tag] = true
		seenPaths[cleanPath] = true
	}
	decodedNonce, nonceErr := hex.DecodeString(c.Nonce)
	if nonceErr != nil || len(decodedNonce) != 32 || c.Nonce != strings.ToLower(c.Nonce) {
		return errors.New("VM runtime nonce is not a lowercase 256-bit hexadecimal value")
	}
	return nil
}

func cleanChildPath(parent, child string) bool {
	if !filepath.IsAbs(child) || filepath.Clean(child) != child {
		return false
	}
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != "." && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// RunSupervisor starts virtiofsd, the host gateway, and QEMU. The caller must
// put this process in a Docker container with no network namespace peers.
func RunSupervisor(ctx context.Context, config SupervisorConfig, stderr io.Writer) (returnErr error) {
	if err := config.Validate(); err != nil {
		return err
	}
	if stderr == nil {
		stderr = io.Discard
	}
	lifecycleFile, err := os.OpenFile(filepath.Join(config.RuntimeDir, "lifecycle.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open VM supervisor lifecycle log: %w", err)
	}
	defer lifecycleFile.Close()
	lifecycle := log.New(lifecycleFile, "", log.LstdFlags|log.Lmicroseconds|log.LUTC)
	lifecycle.Printf("supervisor starting mode=%s depth=%d cpus=%d memory_mib=%d disk_gib=%d", config.Mode, config.Depth, config.CPUs, config.MemoryMiB, config.DiskGiB)
	defer func() {
		if returnErr != nil {
			lifecycle.Printf("supervisor stopped error=%q", returnErr.Error())
			return
		}
		lifecycle.Printf("supervisor stopped cleanly")
	}()
	if err := prepareOverlay(ctx, config, stderr); err != nil {
		return err
	}
	virtioFailure := make(chan processExit, len(config.Mounts)+1)
	var virtioProcesses []*supervisedProcess
	defer func() {
		for index := len(virtioProcesses) - 1; index >= 0; index-- {
			virtioProcesses[index].stop()
		}
	}()
	virtioSocket := filepath.Join(config.RuntimeDir, "virtiofs.sock")
	controlFS, err := startVirtioFSD(ctx, "control", config.ExportDir, virtioSocket,
		filepath.Join(config.RuntimeDir, "virtiofsd.log"), CacheAuto, false, virtioFailure)
	if err != nil {
		return err
	}
	virtioProcesses = append(virtioProcesses, controlFS)
	if err := waitForUnixSocket(ctx, virtioSocket, controlFS, 10*time.Second); err != nil {
		return fmt.Errorf("wait for control virtiofsd: %w", err)
	}
	for index, mount := range config.Mounts {
		socket := mountSocket(config.RuntimeDir, index)
		logPath := filepath.Join(config.RuntimeDir, fmt.Sprintf("virtiofsd-mount-%03d.log", index))
		process, err := startVirtioFSD(ctx, mount.Tag, mount.Path, socket, logPath,
			effectiveCachePolicy(mount.CachePolicy), mount.ReadOnly, virtioFailure)
		if err != nil {
			return err
		}
		virtioProcesses = append(virtioProcesses, process)
		if err := waitForUnixSocket(ctx, socket, process, 10*time.Second); err != nil {
			return fmt.Errorf("wait for mount %s virtiofsd: %w", mount.Tag, err)
		}
	}

	qemuContext, cancelQEMU := context.WithCancel(ctx)
	defer cancelQEMU()
	var gatewayDone chan error
	if config.Mode == ModeRuntime {
		gatewayDone = make(chan error, 1)
		gateway := NewGateway(
			filepath.Join(config.RuntimeDir, "gateway.sock"),
			filepath.Join(config.ControlDir, "control.sock"),
			config.RelaySocket,
			config.Nonce,
		)
		gateway.Logger = lifecycle
		go func() { gatewayDone <- gateway.Serve(qemuContext) }()
		if err := waitForPath(ctx, filepath.Join(config.RuntimeDir, "gateway.sock"), 10*time.Second); err != nil {
			return err
		}
	}

	qemuLog, err := os.OpenFile(filepath.Join(config.RuntimeDir, "qemu.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open QEMU log: %w", err)
	}
	defer qemuLog.Close()
	cpuInfo, _ := os.ReadFile("/proc/cpuinfo")
	args := BuildQEMUArgs(config, virtioSocket, string(cpuInfo))
	qemu := exec.CommandContext(qemuContext, "qemu-system-x86_64", args...)
	qemu.Stdout = qemuLog
	qemu.Stderr = qemuLog
	if err := qemu.Start(); err != nil {
		return fmt.Errorf("start QEMU: %w", err)
	}
	lifecycle.Printf("QEMU started pid=%d", qemu.Process.Pid)
	qemuDone := make(chan error, 1)
	go func() { qemuDone <- qemu.Wait() }()

	select {
	case err := <-qemuDone:
		cancelQEMU()
		return validateQEMUExit(ctx, config, err)
	case err := <-gatewayDone:
		cancelQEMU()
		<-qemuDone
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			return errors.New("VM gateway stopped before QEMU")
		}
		return fmt.Errorf("VM gateway stopped: %w", err)
	case failure := <-virtioFailure:
		// QEMU closes its vhost-user connection just before it exits during a
		// clean guest poweroff. Give that normal ordering a short grace period.
		// A real virtiofsd failure leaves QEMU running and still fails closed.
		select {
		case err := <-qemuDone:
			cancelQEMU()
			return validateQEMUExit(ctx, config, err)
		case <-time.After(time.Second):
			cancelQEMU()
			<-qemuDone
		}
		if ctx.Err() != nil {
			return nil
		}
		if failure.Err == nil {
			return fmt.Errorf("virtiofsd %s stopped before QEMU", failure.Name)
		}
		return fmt.Errorf("virtiofsd %s stopped: %w", failure.Name, failure.Err)
	case <-ctx.Done():
		cancelQEMU()
		<-qemuDone
		return nil
	}
}

func validateQEMUExit(ctx context.Context, config SupervisorConfig, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("QEMU stopped: %w; see %s", err, filepath.Join(config.RuntimeDir, "qemu.log"))
	}
	if config.Mode != ModePrepare {
		return nil
	}
	marker := filepath.Join(config.ExportDir, config.PrepareMarker)
	if _, err := os.Stat(marker); err != nil {
		return fmt.Errorf("VM preparation marker is missing: %w", err)
	}
	return nil
}

func prepareOverlay(ctx context.Context, config SupervisorConfig, stderr io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(config.Overlay), 0o700); err != nil {
		return fmt.Errorf("create VM overlay directory: %w", err)
	}
	if _, err := os.Stat(config.Overlay); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat VM overlay: %w", err)
	}
	size := strconv.Itoa(config.DiskGiB) + "G"
	command := exec.CommandContext(ctx, "qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", config.BaseImage, config.Overlay, size)
	command.Stdout = stderr
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("create VM overlay: %w", err)
	}
	return nil
}

// BuildQEMUArgs returns the complete explicit no-NIC device model.
func BuildQEMUArgs(config SupervisorConfig, virtioSocket, cpuInfo string) []string {
	args := []string{
		"-name", config.Name,
		"-machine", "q35,accel=kvm,usb=off,dump-guest-core=off",
		"-cpu", CPUModel(config.Depth, cpuInfo),
		"-smp", strconv.Itoa(config.CPUs),
		"-m", strconv.Itoa(config.MemoryMiB) + "M",
		"-object", fmt.Sprintf("memory-backend-memfd,id=mem,size=%dM,share=on", config.MemoryMiB),
		"-numa", "node,memdev=mem",
		"-drive", "if=virtio,format=qcow2,file=" + config.Overlay,
		"-chardev", "socket,id=hostfs,path=" + virtioSocket,
		"-device", "vhost-user-fs-pci,chardev=hostfs,tag=cooper-host,queue-size=1024",
		"-device", "virtio-rng-pci",
		"-nic", "none",
		"-display", "none",
		"-monitor", "none",
		"-serial", "file:" + filepath.Join(config.RuntimeDir, "console.log"),
		"-qmp", "unix:" + filepath.Join(config.RuntimeDir, "qmp.sock") + ",server=on,wait=off",
		"-no-user-config",
		"-nodefaults",
		"-no-reboot",
		"-sandbox", "on,obsolete=deny,elevateprivileges=deny,spawn=deny,resourcecontrol=deny",
	}
	if config.SeedImage != "" {
		args = append(args, "-drive", "if=virtio,format=raw,readonly=on,file="+config.SeedImage)
	}
	for index, mount := range config.Mounts {
		// State roots, language caches, and path overrides can exceed the root
		// bus's free slots. Each PCI bridge holds at most 16 mount devices; the
		// root bus then needs only three ports for the complete mount contract.
		const mountsPerBus = 16
		bus := fmt.Sprintf("mount-bus-%d", index/mountsPerBus)
		if index%mountsPerBus == 0 {
			port := fmt.Sprintf("mount-port-%d", index/mountsPerBus)
			args = append(args,
				"-device", fmt.Sprintf("pcie-root-port,id=%s,chassis=%d,slot=%d", port, index/mountsPerBus+1, index/mountsPerBus+1),
				"-device", "pcie-pci-bridge,id="+bus+",bus="+port,
			)
		}
		chardevID := fmt.Sprintf("mountfs%d", index)
		args = append(args,
			"-chardev", "socket,id="+chardevID+",path="+mountSocket(config.RuntimeDir, index),
			"-device", fmt.Sprintf("vhost-user-fs-pci,chardev=%s,tag=%s,queue-size=1024,bus=%s,addr=0x%x", chardevID, mount.Tag, bus, index%mountsPerBus+1),
		)
	}
	if config.Mode == ModeRuntime {
		args = append(args,
			"-device", "virtio-serial-pci",
			"-chardev", "socket,id=gateway,path="+filepath.Join(config.RuntimeDir, "gateway.sock"),
			"-device", "virtserialport,chardev=gateway,name=org.cooper.gateway",
			"-chardev", "file,id=guestlog,path="+filepath.Join(config.RuntimeDir, "guest.log"),
			"-device", "virtserialport,chardev=guestlog,name=org.cooper.log",
		)
	}
	return args
}

// CPUModel exposes nested KVM only at the first managed VM depth.
func CPUModel(depth int, cpuInfo string) string {
	if depth < 2 {
		return "host"
	}
	for scanner := bufio.NewScanner(strings.NewReader(cpuInfo)); scanner.Scan(); {
		line := scanner.Text()
		if strings.HasPrefix(line, "vendor_id") && strings.Contains(line, "AuthenticAMD") {
			return "host,-svm"
		}
		if strings.HasPrefix(line, "vendor_id") && strings.Contains(line, "GenuineIntel") {
			return "host,-vmx"
		}
	}
	return "host,-svm,-vmx"
}

func virtiofsdPath() string {
	for _, path := range []string{"/usr/libexec/virtiofsd", "/usr/lib/qemu/virtiofsd", "/usr/lib/virtiofsd"} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "virtiofsd"
}

type processExit struct {
	Name string
	Err  error
}

type supervisedProcess struct {
	name    string
	command *exec.Cmd
	done    chan error
}

func startVirtioFSD(ctx context.Context, name, sharedDir, socket, logPath, cachePolicy string, readOnly bool, failures chan<- processExit) (*supervisedProcess, error) {
	_ = os.Remove(socket)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open virtiofsd %s log: %w", name, err)
	}
	args := virtioFSDArgs(sharedDir, socket, cachePolicy, readOnly, os.Geteuid(), os.Getegid())
	command := exec.CommandContext(ctx, virtiofsdPath(), args...)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start virtiofsd %s: %w", name, err)
	}
	if err := logFile.Close(); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("close virtiofsd %s log: %w", name, err)
	}
	process := &supervisedProcess{name: name, command: command, done: make(chan error, 1)}
	go func() {
		err := command.Wait()
		process.done <- err
		close(process.done)
		failures <- processExit{Name: name, Err: err}
	}()
	return process, nil
}

func virtioFSDArgs(sharedDir, socket, cachePolicy string, readOnly bool, uid, gid int) []string {
	args := []string{
		"--socket-path=" + socket,
		"--shared-dir=" + sharedDir,
		// The supervisor runs as the invoking user with all capabilities
		// dropped. Map every guest identity to that one authorized host
		// identity. This keeps guest root from creating root-owned host files
		// and prevents virtiofsd from trying setuid or setgid operations.
		"--translate-uid=squash-guest:0:" + strconv.Itoa(uid) + ":4294967295",
		"--translate-gid=squash-guest:0:" + strconv.Itoa(gid) + ":4294967295",
		"--sandbox=none",
		"--seccomp=kill",
		"--cache=" + effectiveCachePolicy(cachePolicy),
		"--inode-file-handles=never",
		"--announce-submounts",
		"--thread-pool-size=2",
		"--log-level=info",
	}
	if readOnly {
		args = append(args, "--readonly")
	}
	return args
}

func effectiveCachePolicy(policy string) string {
	if policy == "" {
		return CacheAuto
	}
	return policy
}

func (p *supervisedProcess) stop() {
	if p == nil || p.command == nil || p.command.Process == nil {
		return
	}
	select {
	case <-p.done:
		return
	default:
	}
	_ = p.command.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
		return
	case <-time.After(500 * time.Millisecond):
	}
	_ = p.command.Process.Kill()
	<-p.done
}

func mountSocket(runtimeDir string, index int) string {
	return filepath.Join(runtimeDir, fmt.Sprintf("virtiofs-mount-%03d.sock", index))
}

func waitForUnixSocket(ctx context.Context, path string, process *supervisedProcess, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return nil
		}
		select {
		case err := <-process.done:
			if err == nil {
				return errors.New("process stopped before its socket became ready")
			}
			return fmt.Errorf("process stopped before its socket became ready: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return fmt.Errorf("socket %s did not become ready", path)
}

func waitForPath(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return fmt.Errorf("path %s did not become ready", path)
}
