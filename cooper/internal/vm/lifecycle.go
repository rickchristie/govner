package vm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/profilemanager"
	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/statelock"
	"github.com/rickchristie/govner/cooper/internal/usercontext"
	"github.com/rickchristie/govner/cooper/internal/vmcontext"
	"github.com/rickchristie/govner/cooper/internal/vmhost"
	"github.com/rickchristie/govner/cooper/internal/vmproto"
	"github.com/rickchristie/govner/cooper/internal/vmrelay"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

const (
	guestControlSubnet   = "172.30.0.0/24"
	guestControlGateway  = "172.30.0.1"
	guestBridgeCIDR      = "172.29.0.1/24"
	relayRemoveAttempts  = 20
	relayRemoveDelay     = 100 * time.Millisecond
	shutdownReplyTimeout = time.Second
)

// Manager owns host Docker resources for Cooper VM workloads.
type Manager struct {
	CooperDir    string
	HomeDir      string
	Namespace    string
	ImagePrefix  string
	ProxyName    string
	Config       *config.Config
	Runner       CommandRunner
	Out          io.Writer
	Executable   string
	SkipPrepare  bool
	PreparedBase string
	// PreparedArchive is optional. Development tests use an immutable archive
	// held under their cache lease. A missing or stale archive must fail before
	// a runtime changes, without an export or a preparation fallback.
	PreparedArchive *ImageArchive
}

// StartRequest contains the selected-agent values for one reusable VM.
type StartRequest struct {
	WorkspaceDir string
	ToolName     string
	ImageRef     string
	// ImageID is optional. A restart sets it to the recorded immutable image
	// identity so a moved tag cannot change the VM. ImageRef stays unchanged
	// because the guest needs the normal local tag for development commands.
	ImageID       string
	RuntimeID     string
	CPUs          int
	MemoryMiB     int
	DiskGiB       int
	ClipboardMode string
	ProfileID     string
	ProfileName   string
	// profilePaths is optional until a named profile is resolved under the
	// shared startup lock. It is never serialized into restart metadata.
	profilePaths *workload.AgentPaths
}

// Runtime is a reusable VM workload.
type Runtime struct {
	ID            string
	ToolName      string
	WorkspaceDir  string
	ContainerName string
	RelayName     string
	RelayNetwork  string
	RuntimeDir    string
	ControlDir    string
	ControlSocket string
	Depth         int
}

// Start creates a VM or returns the existing healthy VM for the same stable
// identity.
func (m Manager) Start(ctx context.Context, request StartRequest) (Runtime, error) {
	if err := m.validate(); err != nil {
		return Runtime{}, err
	}
	stateLock, err := statelock.Acquire(ctx, false)
	if err != nil {
		return Runtime{}, err
	}
	defer stateLock.Close()
	if err := m.resolveProfile(ctx, &request); err != nil {
		return Runtime{}, err
	}
	depth, err := ManagedDepth()
	if err != nil {
		return Runtime{}, err
	}
	if depth > m.Config.VM.MaxDepth {
		return Runtime{}, fmt.Errorf("cooper VM depth %d exceeds the configured maximum %d", depth, m.Config.VM.MaxDepth)
	}
	if err := HostRequirements(depth == 1); err != nil {
		return Runtime{}, err
	}
	request = m.applyResources(request, depth)
	if err := validateResources(request); err != nil {
		return Runtime{}, err
	}
	imageSource, err := request.imageSource()
	if err != nil {
		return Runtime{}, err
	}
	if request.RuntimeID == "" {
		request.RuntimeID, err = ProfileRuntimeID(m.Namespace, request.WorkspaceDir, request.ToolName, request.ProfileID)
		if err != nil {
			return Runtime{}, err
		}
	}
	runtime := runtimeFor(m.CooperDir, request, depth)
	if err := m.checkImageAccount(ctx, imageSource); err != nil {
		return Runtime{}, err
	}
	requestedImageID, err := inspectImageID(ctx, imageSource, m.Runner)
	if err != nil {
		return Runtime{}, err
	}
	if err := m.checkPreparedArchive(requestedImageID); err != nil {
		return Runtime{}, err
	}
	lock, err := acquireRuntimeLock(m.CooperDir, runtime.ID)
	if err != nil {
		return Runtime{}, err
	}
	defer lock.release()
	return m.startLocked(ctx, request, runtime, imageSource, requestedImageID)
}

// startLocked owns token changes under the runtime lock. Preflight failures
// occur before this method and must not remove a token from an existing VM.
func (m Manager) startLocked(ctx context.Context, request StartRequest, runtime Runtime, imageSource, requestedImageID string) (result Runtime, returnErr error) {
	depth := runtime.Depth
	freshToken, err := m.ensureClipboardTokenLocked(ctx, runtime, request)
	if err != nil {
		return Runtime{}, err
	}
	// Only this operation's token can be removed on failure. A rejected start
	// must keep an existing VM's token, or the next start would replace its disk.
	tokenChanged := freshToken
	defer func() {
		if returnErr != nil && tokenChanged {
			_ = clipboard.RemoveTokenFile(m.CooperDir, runtime.ID)
		}
	}()
	mounts, environment, mountDigest, err := m.resolveMountPlan(request, runtime.ID)
	if err != nil {
		return Runtime{}, err
	}
	healthy, healthErr := m.Healthy(runtime)
	metadata, metadataErr := loadRuntimeMetadata(m.CooperDir, runtime.ID)
	metadataExists := runtimeMetadataExists(m.CooperDir, runtime.ID)
	if healthy && metadataErr == nil && metadata.matches(request, depth, requestedImageID, mountDigest) {
		return runtime, nil
	}
	if metadataExists && metadataErr != nil {
		return Runtime{}, fmt.Errorf("VM %s has invalid runtime metadata: %w; run 'cooper vm stop %s' before retrying", runtime.ID, metadataErr, runtime.ID)
	}
	if !healthy && metadataErr == nil && metadata.matches(request, depth, requestedImageID, mountDigest) {
		if healthErr != nil {
			return Runtime{}, fmt.Errorf("VM %s is unhealthy: %w; run 'cooper vm stop %s' before retrying", runtime.ID, healthErr, runtime.ID)
		}
		return Runtime{}, fmt.Errorf("VM %s is unhealthy; inspect %s and run 'cooper vm stop %s' before retrying", runtime.ID, runtime.RuntimeDir, runtime.ID)
	}
	// A reusable VM already owns its memory. Check free memory only when Cooper
	// must start a new QEMU process. Otherwise, a low-memory host can reject the
	// session that would let the user stop or inspect the existing VM.
	available, err := AvailableMemoryMiB()
	if err == nil && request.MemoryMiB+768 > available {
		return Runtime{}, fmt.Errorf("cooper VM needs %d MiB plus 768 MiB overhead; only %d MiB is available", request.MemoryMiB, available)
	}
	if err := m.stopResources(ctx, runtime, true); err != nil {
		return Runtime{}, fmt.Errorf("remove stale VM resources: %w", err)
	}
	if !freshToken {
		if err := m.writeClipboardToken(runtime.ID, request.ToolName, request.ClipboardMode); err != nil {
			return Runtime{}, fmt.Errorf("rotate VM clipboard token: %w", err)
		}
		tokenChanged = true
	}

	base := m.PreparedBase
	if !m.SkipPrepare {
		base, err = (Preparer{CooperDir: m.CooperDir, Prefix: m.ImagePrefix, Runner: m.Runner, Out: m.Out, Executable: m.Executable}).Prepare(ctx)
		if err != nil {
			return Runtime{}, err
		}
	}
	if base == "" {
		return Runtime{}, errors.New("prepared VM base is required")
	}
	archive, err := m.imageArchive(ctx, imageSource)
	if err != nil {
		return Runtime{}, err
	}
	if archive.ImageID != requestedImageID {
		return Runtime{}, errors.New("agent image changed while Cooper prepared the VM")
	}
	if err := os.MkdirAll(runtime.RuntimeDir, 0o700); err != nil {
		return Runtime{}, fmt.Errorf("create VM runtime directory: %w", err)
	}
	if err := m.writeRuntimeFiles(runtime, request, archive, mounts, environment, mountDigest, depth); err != nil {
		return Runtime{}, err
	}
	if err := m.startResources(ctx, runtime, request, archive, mounts, mountDigest, base); err != nil {
		_ = m.stopResources(context.Background(), runtime, false)
		return Runtime{}, fmt.Errorf("%w; VM failure logs remain in %s", err, runtime.RuntimeDir)
	}
	if err := m.waitHealthy(ctx, runtime, time.Duration(m.Config.VM.StartTimeoutS)*time.Second); err != nil {
		_ = m.stopResources(context.Background(), runtime, false)
		return Runtime{}, err
	}
	return runtime, nil
}

func (m Manager) ensureClipboardTokenLocked(ctx context.Context, runtime Runtime, request StartRequest) (bool, error) {
	path := clipboard.TokenFilePath(m.CooperDir, runtime.ID)
	metadata, err := clipboard.ReadTokenMetadata(path)
	if err == nil && metadata.RuntimeID == runtime.ID && metadata.RuntimeKind == clipboard.RuntimeVM &&
		metadata.ToolName == request.ToolName && metadata.ClipboardMode == request.ClipboardMode {
		return false, nil
	}

	// A file bind keeps its original inode. If a running VM lost its host token
	// file, replacing only the file would leave the guest on the revoked token.
	// Remove the old runtime before a new token can enter a new mount.
	_ = clipboard.RemoveTokenFile(m.CooperDir, runtime.ID)
	if err := m.stopResources(ctx, runtime, true); err != nil {
		return false, fmt.Errorf("stop VM before clipboard token replacement: %w", err)
	}
	if err := m.writeClipboardToken(runtime.ID, request.ToolName, request.ClipboardMode); err != nil {
		return false, err
	}
	return true, nil
}

func (m Manager) writeClipboardToken(runtimeID, toolName, clipboardMode string) error {
	token, err := clipboard.GenerateToken()
	if err != nil {
		return err
	}
	if _, err := clipboard.WriteRuntimeToken(m.CooperDir, runtimeID, token, clipboard.RuntimeVM, toolName, clipboardMode); err != nil {
		return err
	}
	return nil
}

func (r StartRequest) imageSource() (string, error) {
	if strings.TrimSpace(r.ImageRef) == "" {
		return "", errors.New("agent image reference is required")
	}
	if r.ImageID == "" {
		return r.ImageRef, nil
	}
	if !validImageID(r.ImageID) {
		return "", fmt.Errorf("agent image ID %q is invalid", r.ImageID)
	}
	return r.ImageID, nil
}

func (m Manager) resolveMountPlan(request StartRequest, runtimeID string) ([]workload.MountSpec, []string, string, error) {
	if request.ProfileID != "" && request.profilePaths == nil {
		if err := m.resolveProfile(context.Background(), &request); err != nil {
			return nil, nil, "", err
		}
	}
	input, err := workload.ResolveMountInput(workload.MountInput{
		WorkspaceDir: request.WorkspaceDir, HomeDir: m.HomeDir, CooperDir: m.CooperDir,
		RuntimeID: runtimeID, ToolName: request.ToolName,
		Environment: workload.HostPathEnvironment(), Config: m.Config,
		Agent: request.profilePaths,
	})
	if err != nil {
		return nil, nil, "", err
	}
	if err := workload.EnsureDirectories(input); err != nil {
		return nil, nil, "", err
	}
	mounts, err := workload.BuildMountPlan(input)
	if err != nil {
		return nil, nil, "", err
	}
	environment := append(workload.RuntimeEnvironment(m.Config, guestControlGateway, "cooper-control"), input.Agent.Environment...)
	digest, err := workload.RuntimeDigest(mounts, environment)
	return mounts, workload.RenderEnvironment(environment), digest, err
}

func (m Manager) resolveProfile(ctx context.Context, request *StartRequest) error {
	if request.ProfileID == "" {
		return profiles.CheckReady(m.CooperDir)
	}
	selection, err := profilemanager.SelectID(ctx, m.CooperDir, request.WorkspaceDir, m.HomeDir, request.ToolName, request.ProfileID)
	if err != nil {
		return err
	}
	request.ProfileName = selection.Name
	request.profilePaths = &selection.Paths
	return nil
}

func (m Manager) checkImageAccount(ctx context.Context, imageRef string) error {
	account, err := usercontext.Current()
	if err != nil {
		return err
	}
	account.Home = m.HomeDir
	if err := account.Validate(); err != nil {
		return err
	}
	output, err := runnerOrSystem(m.Runner).Output(ctx, "docker", "image", "inspect", "--format", `{{index .Config.Labels "cooper.account"}}`, imageRef)
	if err != nil {
		return fmt.Errorf("inspect VM image account: %w", err)
	}
	return account.CheckLabel(strings.TrimSpace(string(output)))
}

func inspectImageID(ctx context.Context, imageRef string, runner CommandRunner) (string, error) {
	data, err := runnerOrSystem(runner).Output(ctx, "docker", "image", "inspect", imageRef)
	if err != nil {
		return "", fmt.Errorf("inspect agent image %s: %w", imageRef, err)
	}
	var images []struct {
		ID     string `json:"Id"`
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.Unmarshal(data, &images); err != nil {
		return "", fmt.Errorf("decode agent image %s inspection: %w", imageRef, err)
	}
	if len(images) != 1 {
		return "", fmt.Errorf("agent image %s inspection returned %d records, want 1", imageRef, len(images))
	}
	if images[0].Config.Labels[workload.VMImageContractLabel] != workload.VMImageContractVersion {
		return "", fmt.Errorf("agent image %s does not support Cooper VM sessions; run 'cooper build' to rebuild it", imageRef)
	}
	imageID := strings.TrimSpace(images[0].ID)
	if !validImageID(imageID) {
		return "", fmt.Errorf("agent image %s has invalid ID %q", imageRef, imageID)
	}
	return imageID, nil
}

func runtimeMetadataExists(cooperDir, runtimeID string) bool {
	_, err := os.Stat(filepath.Join(RuntimeDir(cooperDir, runtimeID), "runtime.json"))
	return err == nil
}

func (m Manager) validate() error {
	if m.Config == nil {
		return errors.New("VM manager config is required")
	}
	if err := m.Config.VM.Validate(); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"Cooper directory": m.CooperDir, "home directory": m.HomeDir,
		"namespace": m.Namespace, "proxy name": m.ProxyName,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("VM manager %s is required", name)
		}
	}
	return nil
}

// ManagedDepth returns the depth of the next Cooper VM. It reads only the
// host-written outer context, so command handlers can reject depth three
// before they inspect images or require a running proxy.
func ManagedDepth() (int, error) {
	outer, err := vmcontext.Load()
	if err != nil {
		return 0, err
	}
	if outer == nil {
		return 1, nil
	}
	return outer.Depth + 1, nil
}

func (m Manager) applyResources(request StartRequest, depth int) StartRequest {
	if request.CPUs == 0 {
		request.CPUs = m.Config.VM.CPUs
	}
	if request.MemoryMiB == 0 {
		request.MemoryMiB = m.Config.VM.MemoryMiB
	}
	if request.DiskGiB == 0 {
		request.DiskGiB = m.Config.VM.DiskGiB
	}
	if depth == 2 {
		if request.CPUs > 4 {
			request.CPUs = 4
		}
		if request.MemoryMiB > 6144 {
			request.MemoryMiB = 6144
		}
		if request.DiskGiB > 24 {
			request.DiskGiB = 24
		}
	}
	return request
}

func validateResources(request StartRequest) error {
	if request.CPUs < 1 || request.CPUs > 128 {
		return fmt.Errorf("VM CPU count %d is outside 1-128", request.CPUs)
	}
	if request.MemoryMiB < 2048 || request.MemoryMiB > 262144 {
		return fmt.Errorf("VM memory %d MiB is outside 2048-262144", request.MemoryMiB)
	}
	if request.DiskGiB < 8 || request.DiskGiB > 2048 {
		return fmt.Errorf("VM disk %d GiB is outside 8-2048", request.DiskGiB)
	}
	switch request.ClipboardMode {
	case "off", "shim", "x11", "auto":
	default:
		return fmt.Errorf("VM clipboard mode %q is invalid", request.ClipboardMode)
	}
	return nil
}

func runtimeFor(cooperDir string, request StartRequest, depth int) Runtime {
	dir := RuntimeDir(cooperDir, request.RuntimeID)
	controlDir := ControlDir(cooperDir, request.RuntimeID)
	return Runtime{
		ID: request.RuntimeID, ToolName: request.ToolName,
		WorkspaceDir: request.WorkspaceDir, ContainerName: request.RuntimeID,
		RelayName: RelayContainerName(request.RuntimeID), RelayNetwork: RelayNetworkName(request.RuntimeID),
		RuntimeDir: dir, ControlDir: controlDir, ControlSocket: ControlSocketPath(cooperDir, request.RuntimeID), Depth: depth,
	}
}

func (m Manager) writeRuntimeFiles(runtime Runtime, request StartRequest, archive ImageArchive, mounts []workload.MountSpec, environment []string, mountDigest string, depth int) error {
	exportDir := filepath.Join(runtime.RuntimeDir, "exports")
	controlDir := filepath.Join(exportDir, "control")
	relayDir := filepath.Join(runtime.RuntimeDir, "relay")
	liveDir := filepath.Join(runtime.RuntimeDir, "live")
	supervisorDir := SupervisorRuntimeDir(runtime.RuntimeDir)
	for _, dir := range []string{controlDir, relayDir, liveDir, supervisorDir, runtime.ControlDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create VM runtime subdirectory: %w", err)
		}
	}
	executable := m.Executable
	if executable == "" {
		var err error
		executable, err = currentExecutable()
		if err != nil {
			return err
		}
	}
	if err := copyFileAtomic(executable, filepath.Join(controlDir, "cooper"), 0o555); err != nil {
		return fmt.Errorf("stage current Cooper guest helper: %w", err)
	}
	seccompSource, err := docker.EnsureSeccompProfile(m.CooperDir)
	if err != nil {
		return fmt.Errorf("prepare VM seccomp profile: %w", err)
	}
	if err := copyFileAtomic(seccompSource, filepath.Join(controlDir, "seccomp.json"), 0o444); err != nil {
		return fmt.Errorf("stage VM seccomp profile: %w", err)
	}
	imageExportDir := filepath.Join(exportDir, "image")
	if err := os.MkdirAll(imageExportDir, 0o700); err != nil {
		return fmt.Errorf("create VM image export directory: %w", err)
	}
	imageExport := filepath.Join(imageExportDir, "agent.tar")
	_ = os.Remove(imageExport)
	if err := os.Link(archive.Path, imageExport); err != nil {
		if err := copyFileAtomic(archive.Path, imageExport, 0o444); err != nil {
			return fmt.Errorf("stage exact VM image archive: %w", err)
		}
	}
	nonce, err := randomHex(32)
	if err != nil {
		return err
	}
	guestMounts := make([]vmproto.GuestMount, 0, len(mounts))
	supervisorMounts := make([]vmhost.MountExport, 0, len(mounts))
	for index, mount := range mounts {
		exportName := fmt.Sprintf("%03d-%s", index, cleanNamePart(mount.ID))
		tag := fmt.Sprintf("cooper-m-%03d", index)
		entry := ""
		if mount.Kind == workload.File {
			entry = "value"
		}
		guestMounts = append(guestMounts, vmproto.GuestMount{
			ID: mount.ID, Tag: tag, Entry: entry,
			Target: mount.Target, ReadOnly: mount.Access == workload.ReadOnly, Kind: string(mount.Kind),
		})
		supervisorMounts = append(supervisorMounts, vmhost.MountExport{
			Tag: tag, Path: "/cooper/mounts/" + exportName,
			CachePolicy: scratchCachePolicy(mount.ID), ReadOnly: mount.Access == workload.ReadOnly,
		})
	}
	caDigest, err := digestFile(filepath.Join(m.CooperDir, "ca", "cooper-ca.pem"))
	if err != nil {
		return fmt.Errorf("hash Cooper CA: %w", err)
	}
	forwardPorts := expandedContainerPorts(m.Config.PortForwardRules)
	manifest := vmproto.Manifest{
		Schema: vmproto.ManifestSchema, Nonce: nonce, RuntimeID: runtime.ID,
		ToolName: request.ToolName, AgentContainer: AgentContainerName(runtime.ID),
		ImageRef: request.ImageRef, ImageID: archive.ImageID,
		ImageArchive:   "/run/cooper/host/image/agent.tar",
		SeccompProfile: "/run/cooper/host/control/seccomp.json",
		WorkspaceDir:   request.WorkspaceDir, HomeDir: m.HomeDir, CooperDir: filepath.Join(m.HomeDir, ".cooper"),
		ProxyPort: m.Config.ProxyPort, BridgePort: m.Config.BridgePort,
		ControlSubnet: guestControlSubnet, ControlGateway: guestControlGateway,
		DefaultBridgeCIDR: guestBridgeCIDR, UID: os.Getuid(), GID: os.Getgid(),
		Depth: depth, SHMSize: m.Config.BarrelSHMSize, Mounts: guestMounts,
		Environment: environment,
		CADigest:    caDigest,
	}
	for _, port := range forwardPorts {
		manifest.ForwardPorts = append(manifest.ForwardPorts, vmproto.PortForward{Port: port})
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(controlDir, "manifest.json"), manifest, 0o444); err != nil {
		return err
	}
	policy := vmrelay.Policy{ProxyHost: m.ProxyName, ProxyPort: m.Config.ProxyPort, BridgePort: m.Config.BridgePort, Forwards: forwardPorts}
	if err := writeJSON(relayPolicyPath(runtime.RuntimeDir), policy, 0o444); err != nil {
		return err
	}
	supervisor := vmhost.SupervisorConfig{
		Mode: vmhost.ModeRuntime, Name: runtime.ID,
		BaseImage: "/cooper/assets/cooper-guest-base.qcow2",
		Overlay:   "/cooper/runtime/disk.qcow2", ExportDir: "/cooper/exports",
		RuntimeDir: "/cooper/runtime", ControlDir: "/cooper/control", RelaySocket: "/cooper/relay/relay.sock",
		Nonce: nonce, CPUs: request.CPUs, MemoryMiB: request.MemoryMiB,
		DiskGiB: request.DiskGiB, Depth: depth, Mounts: supervisorMounts,
	}
	if err := writeJSON(filepath.Join(supervisorDir, "supervisor.json"), supervisor, 0o444); err != nil {
		return err
	}
	metadata := RuntimeMetadata{
		ProfileID: request.ProfileID, ProfileName: request.ProfileName,
		Schema: runtimeMetadataSchema, RuntimeID: runtime.ID, ToolName: request.ToolName,
		WorkspaceDir: request.WorkspaceDir, ImageRef: request.ImageRef, ImageID: archive.ImageID,
		Depth: depth, CPUs: request.CPUs, MemoryMiB: request.MemoryMiB, DiskGiB: request.DiskGiB,
		ClipboardMode: request.ClipboardMode, MountPlanSHA256: mountDigest,
	}
	return writeJSON(filepath.Join(runtime.RuntimeDir, "runtime.json"), metadata, 0o444)
}

func scratchCachePolicy(mountID string) string {
	// Go and other compilers can mmap files in /tmp. The directory belongs to
	// one VM runtime, so the virtiofsd always-cache policy is coherent there.
	// Shared host paths use auto so host and guest changes stay visible.
	if mountID == "tmp" {
		return vmhost.CacheAlways
	}
	return vmhost.CacheAuto
}

func (m Manager) startResources(ctx context.Context, runtime Runtime, request StartRequest, archive ImageArchive, mounts []workload.MountSpec, mountDigest, base string) error {
	runner := runnerOrSystem(m.Runner)
	if err := runner.Run(ctx, nil, m.output(), m.output(), "docker", "network", "create", "--internal",
		"--label", "cooper.kind=vm-relay-network", "--label", "cooper.runtime-id="+runtime.ID, runtime.RelayNetwork); err != nil {
		return fmt.Errorf("create VM relay network: %w", err)
	}
	if _, err := runner.Output(ctx, "docker", "network", "connect", runtime.RelayNetwork, m.ProxyName); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return fmt.Errorf("connect Cooper proxy to VM relay network: %w", err)
	}
	relayArgs := relayDockerRunArgs(runtime, m.ImagePrefix, os.Getuid(), os.Getgid())
	if err := runner.Run(ctx, nil, m.output(), m.output(), "docker", relayArgs...); err != nil {
		return fmt.Errorf("start VM relay: %w", err)
	}
	if err := waitForFile(filepath.Join(runtime.RuntimeDir, "relay", "relay.sock"), 10*time.Second); err != nil {
		return fmt.Errorf("wait for VM relay: %w", err)
	}
	supervisorArgs, err := supervisorDockerRunArgs(runtime, request, archive, base, mounts, mountDigest, m.ImagePrefix, filepath.Join(m.CooperDir, "cli", "seccomp.json"))
	if err != nil {
		return err
	}
	if err := runner.Run(ctx, nil, m.output(), m.output(), "docker", supervisorArgs...); err != nil {
		return fmt.Errorf("start VM supervisor: %w", err)
	}
	return nil
}

func relayDockerRunArgs(runtime Runtime, prefix string, uid, gid int) []string {
	return []string{
		"run", "-d", "--name", runtime.RelayName,
		"--network", runtime.RelayNetwork,
		"--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--read-only", "--pids-limit", "300", "--memory", "256m", "--cpus", "1",
		"--user", fmt.Sprintf("%d:%d", uid, gid),
		"--label", "cooper.kind=vm-relay", "--label", "cooper.runtime-id=" + runtime.ID,
		"--mount", workload.DockerBindMount(filepath.Join(runtime.RuntimeDir, "relay"), "/cooper/relay", false),
		"--mount", workload.DockerBindMount(filepath.Join(runtime.RuntimeDir, "live"), "/cooper/live", true),
		RelayImageName(prefix), "--socket", "/cooper/relay/relay.sock", "--policy", "/cooper/live/relay-policy.json", "--log", "/cooper/relay/relay.log",
	}
}

func relayPolicyPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "live", "relay-policy.json")
}

func supervisorDockerRunArgs(runtime Runtime, request StartRequest, archive ImageArchive, base string, mounts []workload.MountSpec, mountDigest, prefix, seccompPath string) ([]string, error) {
	uid, gid, kvmGID, err := currentIDs()
	if err != nil {
		return nil, err
	}
	return supervisorDockerArgs(runtime, request, archive, base, mounts, mountDigest, prefix, seccompPath, uid, gid, kvmGID)
}

// Keep host discovery outside argument construction. Security assertions must
// run on developer machines that do not have a KVM device.
func supervisorDockerArgs(runtime Runtime, request StartRequest, archive ImageArchive, base string, mounts []workload.MountSpec, mountDigest, prefix, seccompPath string, uid, gid, kvmGID int) ([]string, error) {
	args := []string{"run", "-d", "--name", runtime.ContainerName}
	if request.ProfileID != "" {
		args = append(args, "--label", "cooper.profile-id="+request.ProfileID, "--label", "cooper.profile="+request.ProfileName)
	}
	args = append(args, supervisorSecurityArgs(uid, gid, kvmGID, request.CPUs, request.MemoryMiB, seccompPath)...)
	args = append(args,
		"--label", "cooper.kind=vm-supervisor",
		"--label", "cooper.runtime-id="+runtime.ID,
		"--label", "cooper.workspace="+request.WorkspaceDir,
		"--label", "cooper.tool="+request.ToolName,
		"--label", "cooper.clipboard-mode="+request.ClipboardMode,
		"--label", "cooper.depth="+strconv.Itoa(runtime.Depth),
		"--label", "cooper.mount-plan="+mountDigest,
		"--label", "cooper.image-id="+archive.ImageID,
		"-e", "COOPER_CLI_TOOL="+request.ToolName,
		"-e", "COOPER_CLIPBOARD_MODE="+request.ClipboardMode,
		"--mount", workload.DockerBindMount(filepath.Dir(base), "/cooper/assets", true),
		"--mount", workload.DockerBindMount(SupervisorRuntimeDir(runtime.RuntimeDir), "/cooper/runtime", false),
		"--mount", workload.DockerBindMount(runtime.ControlDir, "/cooper/control", false),
		"--mount", workload.DockerBindMount(filepath.Join(runtime.RuntimeDir, "exports"), "/cooper/exports", true),
		"--mount", workload.DockerBindMount(filepath.Join(runtime.RuntimeDir, "relay"), "/cooper/relay", true),
	)
	workspaceExport := ""
	for index, mount := range mounts {
		exportName := fmt.Sprintf("%03d-%s", index, cleanNamePart(mount.ID))
		destination := "/cooper/mounts/" + exportName
		if mount.Kind == workload.File {
			destination += "/value"
		}
		value := workload.DockerBindMount(mount.Source, destination, mount.Access == workload.ReadOnly)
		args = append(args, "--mount", value)
		if mount.ID == "workspace" {
			workspaceExport = destination
		}
		if mount.ID == "git-hooks" && workspaceExport != "" {
			// The guest can mount a virtiofs tag again at another path. Apply the
			// hooks overlay inside the workspace export too, so guest root cannot
			// recover a writable hooks directory by remounting the workspace tag.
			relative, relativeErr := filepath.Rel(request.WorkspaceDir, mount.Target)
			if relativeErr != nil || relative == "." || strings.HasPrefix(relative, "..") {
				return nil, errors.New("git hooks mount is outside the workspace export")
			}
			args = append(args, "--mount", workload.DockerBindMount(mount.Source, filepath.Join(workspaceExport, relative), true))
		}
	}
	args = append(args, SupervisorImageName(prefix), "--config-file", "/cooper/runtime/supervisor.json")
	return args, nil
}

// Healthy asks the private supervisor control socket for guest readiness.
func (m Manager) Healthy(runtime Runtime) (bool, error) {
	return m.healthy(context.Background(), runtime)
}

func (m Manager) healthy(ctx context.Context, runtime Runtime) (bool, error) {
	healthContext, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	proxyName := m.ProxyName
	if strings.TrimSpace(proxyName) == "" {
		proxyName = cleanNamePart(m.Namespace) + "-proxy"
	}
	for _, containerName := range []string{runtime.ContainerName, runtime.RelayName, proxyName} {
		running, err := containerRunning(healthContext, m.Runner, containerName)
		if err != nil {
			return false, fmt.Errorf("check VM dependency %s: %w", containerName, err)
		}
		if !running {
			return false, nil
		}
	}
	dialer := net.Dialer{Timeout: 250 * time.Millisecond}
	connection, err := dialer.DialContext(healthContext, "unix", runtime.ControlSocket)
	if err != nil {
		return false, nil
	}
	defer connection.Close()
	// The context bounds Docker checks and dialing. Bound stream reads and
	// writes too, and close promptly if startup is canceled before its deadline.
	deadline, _ := healthContext.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return false, err
	}
	stopClose := context.AfterFunc(healthContext, func() { connection.Close() })
	defer stopClose()
	requestID, err := randomHex(12)
	if err != nil {
		return false, err
	}
	header := vmproto.NewHeader(vmproto.ServiceHealth, requestID)
	if err := vmproto.WriteHeader(connection, header); err != nil {
		return false, err
	}
	frame, err := vmproto.ReadFrame(connection)
	if err != nil {
		return false, nil
	}
	return frame.Type == vmproto.FrameStdout && string(frame.Data) == "ready", nil
}

func (m Manager) waitHealthy(ctx context.Context, runtime Runtime, timeout time.Duration) error {
	startup, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for startup.Err() == nil {
		if data, err := os.ReadFile(filepath.Join(runtime.ControlDir, "guest-error.log")); err == nil && len(data) > 0 {
			return fmt.Errorf("VM guest failed: %s", strings.TrimSpace(string(data)))
		}
		healthy, err := m.healthy(startup, runtime)
		if err == nil && healthy {
			return nil
		}
		running, runErr := containerRunning(startup, m.Runner, runtime.ContainerName)
		if runErr == nil && !running {
			return fmt.Errorf("VM supervisor %s stopped before the guest became ready; inspect %s", runtime.ID, filepath.Join(runtime.ControlDir, "guest-error.log"))
		}
		select {
		case <-startup.Done():
		case <-time.After(200 * time.Millisecond):
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("VM %s did not become ready in %s; inspect %s", runtime.ID, timeout, filepath.Join(SupervisorRuntimeDir(runtime.RuntimeDir), "console.log"))
}

// Stop shuts down and removes one VM. It preserves prepared and image caches.
func (m Manager) Stop(ctx context.Context, runtime Runtime) error {
	lock, err := acquireRuntimeLock(m.CooperDir, runtime.ID)
	if err != nil {
		return err
	}
	defer lock.release()
	return m.stopLocked(ctx, runtime)
}

// Restart recreates one VM from its host-owned launch metadata. It rotates the
// clipboard token between the old and new guest lifetimes.
func (m Manager) Restart(ctx context.Context, runtimeID string) (Runtime, error) {
	stateLock, err := statelock.Acquire(ctx, false)
	if err != nil {
		return Runtime{}, err
	}
	defer stateLock.Close()
	metadata, err := loadRuntimeMetadata(m.CooperDir, runtimeID)
	if err != nil {
		return Runtime{}, err
	}
	request := metadata.startRequest()
	if err := m.resolveProfile(ctx, &request); err != nil {
		return Runtime{}, err
	}
	// Resolve every immutable launch input before the old VM stops. A failed
	// image lookup or preparation must leave the healthy workload intact.
	// Restart also uses the recorded image ID, not a tag that can move between
	// the first launch and the restart.
	request.ImageID = metadata.ImageID
	if _, err := m.imageArchive(ctx, request.ImageID); err != nil {
		return Runtime{}, err
	}
	if !m.SkipPrepare {
		if _, err := (Preparer{CooperDir: m.CooperDir, Prefix: m.ImagePrefix, Runner: m.Runner, Out: m.Out, Executable: m.Executable}).Prepare(ctx); err != nil {
			return Runtime{}, err
		}
	}
	runtime := runtimeFor(m.CooperDir, request, metadata.Depth)
	if err := m.Stop(ctx, runtime); err != nil {
		return Runtime{}, err
	}
	restarted, err := m.Start(ctx, request)
	if err != nil {
		_ = clipboard.RemoveTokenFile(m.CooperDir, runtimeID)
		return Runtime{}, err
	}
	return restarted, nil
}

// StopID stops a VM from its verified host-owned metadata. It does not accept
// a guessed Docker name as ownership evidence.
func (m Manager) StopID(ctx context.Context, runtimeID string) error {
	if !strings.HasPrefix(runtimeID, cleanNamePart(m.Namespace)+"-vm-") {
		return fmt.Errorf("VM %s is outside runtime namespace %s", runtimeID, m.Namespace)
	}
	if cleanNamePart(runtimeID) != runtimeID {
		return fmt.Errorf("VM runtime ID %q is invalid", runtimeID)
	}
	controlDir := ControlDir(m.CooperDir, runtimeID)
	runtime := Runtime{
		ID: runtimeID, ContainerName: runtimeID,
		RelayName: RelayContainerName(runtimeID), RelayNetwork: RelayNetworkName(runtimeID),
		RuntimeDir: RuntimeDir(m.CooperDir, runtimeID), ControlDir: controlDir,
		ControlSocket: ControlSocketPath(m.CooperDir, runtimeID),
	}
	return m.Stop(ctx, runtime)
}

func (m Manager) stopLocked(ctx context.Context, runtime Runtime) error {
	// Revoke clipboard access before the guest or relay shutdown begins.
	if err := clipboard.RemoveTokenFile(m.CooperDir, runtime.ID); err != nil {
		return err
	}
	if connection, err := net.DialTimeout("unix", runtime.ControlSocket, time.Second); err == nil {
		// A guest that fails before its command loop can accept the socket but
		// never reply. Keep cleanup independent from guest health.
		_ = connection.SetDeadline(time.Now().Add(shutdownReplyTimeout))
		header := vmproto.NewHeader(vmproto.ServiceShutdown, "shutdown")
		_ = vmproto.WriteHeader(connection, header)
		_, _ = vmproto.ReadFrame(connection)
		connection.Close()
	}
	deadline := time.Now().Add(time.Duration(m.Config.VM.StopTimeoutS) * time.Second)
	for time.Now().Before(deadline) {
		running, _ := containerRunning(ctx, m.Runner, runtime.ContainerName)
		if !running {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return m.stopResources(ctx, runtime, true)
}

func (m Manager) stopResources(ctx context.Context, runtime Runtime, removeDir bool) error {
	runner := runnerOrSystem(m.Runner)
	var messages []string
	containers := []struct {
		name   string
		kind   string
		id     string
		exists bool
	}{
		{name: runtime.ContainerName, kind: "vm-supervisor"},
		{name: runtime.RelayName, kind: "vm-relay"},
	}
	for index := range containers {
		container := &containers[index]
		id, exists, err := inspectOwnedContainer(ctx, runner, container.name, container.kind, runtime.ID)
		if err != nil {
			messages = append(messages, err.Error())
			continue
		}
		container.id = id
		container.exists = exists
	}
	networkID, networkExists, networkErr := inspectOwnedNetwork(ctx, runner, runtime.RelayNetwork, "vm-relay-network", runtime.ID)
	if networkErr != nil {
		messages = append(messages, networkErr.Error())
	}
	// Complete every ownership check before the first removal. A foreign name
	// collision must not cause partial cleanup of a valid Cooper runtime.
	if len(messages) > 0 {
		return errors.New(strings.Join(messages, "; "))
	}
	for _, container := range containers {
		if !container.exists {
			continue
		}
		if _, err := runner.Output(ctx, "docker", "rm", "-f", container.id); err != nil && !isMissingDockerObject(err) {
			messages = append(messages, fmt.Sprintf("remove %s %s: %v", container.kind, container.name, err))
		}
	}
	if networkExists {
		proxyName := m.ProxyName
		if strings.TrimSpace(proxyName) == "" {
			proxyName = cleanNamePart(m.Namespace) + "-proxy"
		}
		disconnected := true
		if _, err := runner.Output(ctx, "docker", "network", "disconnect", "-f", networkID, proxyName); err != nil && !isIgnorableNetworkDisconnect(err) {
			messages = append(messages, fmt.Sprintf("disconnect %s from %s: %v", proxyName, runtime.RelayNetwork, err))
			disconnected = false
		}
		if disconnected {
			if err := removeRelayNetwork(ctx, runner, networkID, runtime.RelayNetwork); err != nil {
				messages = append(messages, err.Error())
			}
		}
	}
	if removeDir && runtime.RuntimeDir != "" && len(messages) == 0 {
		runRoot := filepath.Join(m.CooperDir, "vm", "run")
		relative, err := filepath.Rel(runRoot, runtime.RuntimeDir)
		if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
			messages = append(messages, "refuse unsafe VM runtime directory removal")
		} else if err := os.RemoveAll(runtime.RuntimeDir); err != nil {
			messages = append(messages, err.Error())
		}
		if runtime.ControlDir != "" {
			expected := ControlDir(m.CooperDir, runtime.ID)
			if runtime.ControlDir != expected {
				messages = append(messages, "refuse unsafe VM control directory removal")
			} else if err := os.RemoveAll(runtime.ControlDir); err != nil {
				messages = append(messages, err.Error())
			}
		}
	}
	if len(messages) > 0 {
		return errors.New(strings.Join(messages, "; "))
	}
	return nil
}

func removeRelayNetwork(ctx context.Context, runner CommandRunner, networkID, networkName string) error {
	var lastErr error
	for attempt := 0; attempt < relayRemoveAttempts; attempt++ {
		if _, err := runner.Output(ctx, "docker", "network", "rm", networkID); err == nil || isMissingDockerObject(err) {
			return nil
		} else if !strings.Contains(strings.ToLower(err.Error()), "active endpoints") {
			return err
		} else {
			lastErr = err
		}

		timer := time.NewTimer(relayRemoveDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("remove relay network %s after %d attempts: %w", networkName, relayRemoveAttempts, lastErr)
}

func isIgnorableNetworkDisconnect(err error) bool {
	if isMissingDockerObject(err) {
		return true
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "is not connected") || strings.Contains(value, "not connected to network")
}

func containerRunning(ctx context.Context, runner CommandRunner, name string) (bool, error) {
	data, err := runnerOrSystem(runner).Output(ctx, "docker", "inspect", "--format", "{{.State.Running}}", name)
	if err != nil {
		if isMissingDockerObject(err) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(data)) == "true", nil
}

func isMissingDockerObject(err error) bool {
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "no such") || strings.Contains(value, "not found")
}

func expandedContainerPorts(rules []config.PortForwardRule) []int {
	set := make(map[int]bool)
	for _, rule := range rules {
		end := rule.ContainerPort
		if rule.IsRange && rule.RangeEnd > end {
			end = rule.RangeEnd
		}
		for port := rule.ContainerPort; port <= end; port++ {
			set[port] = true
		}
	}
	ports := make([]int, 0, len(set))
	for port := range set {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

func randomHex(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("create VM nonce: %w", err)
	}
	return hex.EncodeToString(data), nil
}

func digestFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("%s did not appear", path)
}

func (m Manager) output() io.Writer {
	if m.Out != nil {
		return m.Out
	}
	return os.Stderr
}
