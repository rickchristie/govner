package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/vmcontext"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

type recordingRunner struct {
	commands []string
	output   func(command string) ([]byte, error)
}

func (r *recordingRunner) Run(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
	return errors.New("unexpected Run call")
}

func (r *recordingRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	command := strings.Join(append([]string{name}, args...), " ")
	r.commands = append(r.commands, command)
	return r.output(command)
}

func TestManagedDepthFromContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.json")
	outerContext := vmcontext.Context{
		Schema: vmcontext.Schema, Depth: 1,
		ParentNetwork: "cooper-control", ParentProxy: "172.30.0.1",
		AgentContainer: "cooper-outer-agent",
		ProxyPort:      3128, BridgePort: 4343,
		WorkspaceDir: "/work/project", TempDir: "/tmp", CooperDir: "/home/user/.cooper",
	}
	if err := vmcontext.Write(path, outerContext); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COOPER_VM_CONTEXT", path)
	depth, err := ManagedDepth()
	if err != nil {
		t.Fatal(err)
	}
	if depth != 2 {
		t.Fatalf("managed depth = %d", depth)
	}
}

func TestNestedResourcesAreBounded(t *testing.T) {
	t.Parallel()
	manager := Manager{Config: config.DefaultConfig()}
	request := manager.applyResources(StartRequest{}, 2)
	if request.CPUs != 4 || request.MemoryMiB != 6144 || request.DiskGiB != 24 {
		t.Fatalf("nested resources = %#v", request)
	}
}

func TestStartRequestUsesStableReferenceForNormalStart(t *testing.T) {
	t.Parallel()
	request := StartRequest{ImageRef: "cooper-cli-codex:latest"}
	got, err := request.imageSource()
	if err != nil {
		t.Fatal(err)
	}
	if got != request.ImageRef {
		t.Fatalf("image source = %q, want %q", got, request.ImageRef)
	}
}

func TestStartRequestUsesImmutableIdentityForRestart(t *testing.T) {
	t.Parallel()
	request := StartRequest{
		ImageRef: "cooper-cli-codex:latest",
		ImageID:  "sha256:" + strings.Repeat("a", 64),
	}
	got, err := request.imageSource()
	if err != nil {
		t.Fatal(err)
	}
	if got != request.ImageID {
		t.Fatalf("image source = %q, want immutable identity %q", got, request.ImageID)
	}
	if request.ImageRef != "cooper-cli-codex:latest" {
		t.Fatalf("image reference changed to %q", request.ImageRef)
	}
}

func TestStartRequestRejectsInvalidImmutableIdentity(t *testing.T) {
	t.Parallel()
	request := StartRequest{ImageRef: "cooper-cli-codex:latest", ImageID: "latest"}
	if _, err := request.imageSource(); err == nil {
		t.Fatal("imageSource() accepted a mutable image ID")
	}
}

func TestInspectImageIDRequiresVMImageContract(t *testing.T) {
	t.Parallel()
	imageID := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name    string
		output  string
		wantID  string
		wantErr string
	}{
		{
			name: "current",
			output: fmt.Sprintf(`[{"Id":%q,"Config":{"Labels":{%q:%q}}}]`,
				imageID, workload.VMImageContractLabel, workload.VMImageContractVersion),
			wantID: imageID,
		},
		{name: "missing", output: fmt.Sprintf(`[{"Id":%q,"Config":{}}]`, imageID), wantErr: "run 'cooper build'"},
		{
			name: "old",
			output: fmt.Sprintf(`[{"Id":%q,"Config":{"Labels":{%q:"0"}}}]`,
				imageID, workload.VMImageContractLabel),
			wantErr: "run 'cooper build'",
		},
		{name: "malformed inspection", output: `[`, wantErr: "unexpected end of JSON input"},
		{name: "no record", output: `[]`, wantErr: "returned 0 records, want 1"},
		{name: "multiple records", output: `[{},{}]`, wantErr: "returned 2 records, want 1"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runner := &recordingRunner{output: func(command string) ([]byte, error) {
				if command != "docker image inspect cooper-cli-codex:latest" {
					t.Fatalf("image inspection command = %s", command)
				}
				return []byte(test.output + "\n"), nil
			}}
			got, err := inspectImageID(context.Background(), "cooper-cli-codex:latest", runner)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("inspectImageID() error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.wantID {
				t.Fatalf("inspectImageID() = %q, want %q", got, test.wantID)
			}
		})
	}
}

func TestStopLockedBoundsSilentGuestHandshake(t *testing.T) {
	t.Parallel()
	cooperDir := t.TempDir()
	controlDir := filepath.Join(cooperDir, "control")
	if err := os.MkdirAll(controlDir, 0o700); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(controlDir, "control.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(io.Discard, connection)
	}()

	runner := &recordingRunner{output: func(string) ([]byte, error) {
		return nil, errors.New("No such Docker object")
	}}
	cfg := config.DefaultConfig()
	cfg.VM.StopTimeoutS = 1
	manager := Manager{CooperDir: cooperDir, Namespace: "unit", Runner: runner, Config: cfg}
	runtime := Runtime{
		ID: "unit-vm-silent-codex-aabbccddeeff", ContainerName: "unit-vm-silent-codex-aabbccddeeff",
		RelayName: "unit-vm-silent-codex-aabbccddeeff-relay", RelayNetwork: "unit-vm-silent-codex-aabbccddeeff-relay",
		ControlDir: controlDir, ControlSocket: socketPath,
	}
	started := time.Now()
	if err := manager.stopLocked(context.Background(), runtime); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("silent guest handshake took %s", elapsed)
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("silent guest connection remained open after shutdown")
	}
}

func TestEnsureClipboardTokenKeepsMatchingTokenWithoutRuntimeChange(t *testing.T) {
	t.Parallel()
	cooperDir := t.TempDir()
	runtimeID := "unit-vm-project-codex-aabbccddeeff"
	if _, err := clipboard.WriteRuntimeToken(cooperDir, runtimeID, "existing-token", clipboard.RuntimeVM, "codex", "shim"); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{output: func(command string) ([]byte, error) {
		return nil, fmt.Errorf("unexpected command: %s", command)
	}}
	manager := Manager{CooperDir: cooperDir, Namespace: "unit", Runner: runner, Config: config.DefaultConfig()}
	runtime := runtimeFor(cooperDir, StartRequest{RuntimeID: runtimeID, ToolName: "codex"}, 1)
	fresh, err := manager.ensureClipboardTokenLocked(context.Background(), runtime, StartRequest{ToolName: "codex", ClipboardMode: "shim"})
	if err != nil {
		t.Fatal(err)
	}
	if fresh {
		t.Fatal("matching token was reported as fresh")
	}
	if len(runner.commands) != 0 {
		t.Fatalf("matching token changed runtime resources: %v", runner.commands)
	}
}

func TestFailedStartRemovesOnlyItsNewToken(t *testing.T) {
	for _, existing := range []bool{true, false} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			home := t.TempDir()
			cooperDir := filepath.Join(home, ".cooper")
			request := StartRequest{RuntimeID: "unit-vm-project-codex-aabbccddeeff", ToolName: "codex", ClipboardMode: "shim", WorkspaceDir: filepath.Join(home, "project")}
			runtime := runtimeFor(cooperDir, request, 1)
			marker := filepath.Join(runtime.RuntimeDir, "guest-disk-marker")
			if existing {
				if _, err := clipboard.WriteRuntimeToken(cooperDir, runtime.ID, "existing-token", clipboard.RuntimeVM, "codex", "shim"); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(runtime.RuntimeDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(marker, []byte("private data"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// Invalid state placement fails after token setup, without Docker,
			// KVM, image preparation, or a guest disk write.
			t.Setenv("CODEX_HOME", filepath.Join(cooperDir, "state"))
			if err := os.MkdirAll(filepath.Join(cooperDir, "state"), 0o700); err != nil {
				t.Fatal(err)
			}
			runner := &recordingRunner{output: func(string) ([]byte, error) {
				return nil, errors.New("no such object")
			}}
			manager := Manager{CooperDir: cooperDir, HomeDir: home, Namespace: "unit", Runner: runner, Config: config.DefaultConfig()}
			if _, err := manager.startLocked(context.Background(), request, runtime, "unused-reference", "unused-image"); err == nil || !strings.Contains(err.Error(), "overlap") {
				t.Fatalf("expected mount preflight failure: %v", err)
			}
			metadata, err := clipboard.ReadTokenMetadata(clipboard.TokenFilePath(cooperDir, runtime.ID))
			if !existing {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed start left its new token: %v", err)
				}
				return
			}
			if err != nil || metadata.Token != "existing-token" {
				t.Fatalf("failed start changed the existing token: %v", err)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "private data" || len(runner.commands) != 0 {
				t.Fatalf("failed start changed the existing VM: %v; commands: %v", err, runner.commands)
			}
		})
	}
}

func TestEnsureClipboardTokenRecreatesRuntimeBeforeReplacingMissingFile(t *testing.T) {
	t.Parallel()
	cooperDir := t.TempDir()
	runtimeID := "unit-vm-project-codex-aabbccddeeff"
	runtime := runtimeFor(cooperDir, StartRequest{RuntimeID: runtimeID, ToolName: "codex"}, 1)
	if err := os.MkdirAll(runtime.RuntimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldRuntimeMarker := filepath.Join(runtime.RuntimeDir, "old-runtime")
	if err := os.WriteFile(oldRuntimeMarker, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 64)
	runner := &recordingRunner{output: func(command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "container inspect") && strings.HasSuffix(command, " "+runtime.ContainerName):
			return []byte(id + "\tvm-supervisor\t" + runtimeID + "\n"), nil
		case strings.Contains(command, "container inspect") && strings.HasSuffix(command, " "+runtime.RelayName):
			return []byte(id + "\tvm-relay\t" + runtimeID + "\n"), nil
		case strings.Contains(command, "network inspect"):
			return []byte(id + "\tvm-relay-network\t" + runtimeID + "\n"), nil
		default:
			return nil, nil
		}
	}}
	manager := Manager{CooperDir: cooperDir, Namespace: "unit", Runner: runner, Config: config.DefaultConfig()}
	fresh, err := manager.ensureClipboardTokenLocked(context.Background(), runtime, StartRequest{ToolName: "codex", ClipboardMode: "shim"})
	if err != nil {
		t.Fatal(err)
	}
	if !fresh {
		t.Fatal("replacement token was not reported as fresh")
	}
	if _, err := os.Stat(oldRuntimeMarker); !os.IsNotExist(err) {
		t.Fatalf("old runtime remains after token replacement: %v", err)
	}
	metadata, err := clipboard.ReadTokenMetadata(clipboard.TokenFilePath(cooperDir, runtimeID))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ToolName != "codex" || metadata.ClipboardMode != "shim" {
		t.Fatalf("replacement token metadata = %#v", metadata)
	}
	joined := strings.Join(runner.commands, "\n")
	if strings.Count(joined, "docker rm -f ") != 2 || !strings.Contains(joined, "docker network rm ") {
		t.Fatalf("old runtime was not fully removed before token replacement:\n%s", joined)
	}
}

func TestExpandedContainerPorts(t *testing.T) {
	t.Parallel()
	ports := expandedContainerPorts([]config.PortForwardRule{
		{ContainerPort: 8080},
		{ContainerPort: 9000, IsRange: true, RangeEnd: 9002},
		{ContainerPort: 8080},
	})
	want := []int{8080, 9000, 9001, 9002}
	if len(ports) != len(want) {
		t.Fatalf("ports = %#v", ports)
	}
	for index := range want {
		if ports[index] != want[index] {
			t.Fatalf("ports = %#v", ports)
		}
	}
}

func TestRelayDockerArgsDoNotMountSelectedData(t *testing.T) {
	t.Parallel()
	runtime := Runtime{ID: "cooper-vm-test", RelayName: "cooper-vm-test-relay", RelayNetwork: "cooper-vm-test-relay", RuntimeDir: "/state/run"}
	args := strings.Join(relayDockerRunArgs(runtime, "", 1000, 1000), " ")
	for _, forbidden := range []string{"/home/user", "/workspace", "/dev/kvm", "docker.sock", "--privileged"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("relay arguments contain %q: %s", forbidden, args)
		}
	}
}

func TestSupervisorDockerArgsMountOnlyKVMDevice(t *testing.T) {
	t.Parallel()
	runtime := Runtime{ID: "cooper-vm-test", ContainerName: "cooper-vm-test", RuntimeDir: "/state/run", Depth: 1}
	request := StartRequest{WorkspaceDir: "/work/project", ToolName: "codex", CPUs: 4, MemoryMiB: 4096, DiskGiB: 16}
	mounts := []workload.MountSpec{
		{ID: "workspace", Source: "/work/project", Target: "/work/project", Access: workload.ReadWrite},
		{ID: "git-hooks", Source: "/work/project/.git/hooks", Target: "/work/project/.git/hooks", Access: workload.ReadOnly},
	}
	args, err := supervisorDockerArgs(runtime, request, ImageArchive{ImageID: "sha256:" + strings.Repeat("a", 64), Path: "/state/image.tar"}, "/state/base.qcow2", mounts, strings.Repeat("b", 64), "", "/state/seccomp.json", 1001, 1002, 993)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if strings.Count(joined, "--device") != 1 || !strings.Contains(joined, "--device /dev/kvm") {
		t.Fatalf("supervisor devices are unsafe: %s", joined)
	}
	for _, forbidden := range []string{"docker.sock", "--privileged", "--network host"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("supervisor arguments contain %q: %s", forbidden, joined)
		}
	}
	if !strings.Contains(joined, "dst=/cooper/mounts/000-workspace/.git/hooks,readonly") {
		t.Fatalf("workspace export does not enforce the read-only hooks overlay: %s", joined)
	}
	if !strings.Contains(joined, "cooper.mount-plan="+strings.Repeat("b", 64)) {
		t.Fatalf("supervisor arguments do not label the mount plan: %s", joined)
	}
	if !strings.Contains(joined, "src=/state/run/supervisor,dst=/cooper/runtime") {
		t.Fatalf("supervisor does not use its private writable directory: %s", joined)
	}
	if strings.Contains(joined, "src=/state/run,dst=/cooper/runtime") {
		t.Fatalf("supervisor can change host-owned runtime metadata: %s", joined)
	}
	if !strings.Contains(joined, "src=/state/run/relay,dst=/cooper/relay,readonly") {
		t.Fatalf("supervisor relay socket directory is not read-only: %s", joined)
	}
}

func TestSupervisorDockerArgsRejectHooksOutsideWorkspace(t *testing.T) {
	t.Parallel()
	runtime := Runtime{ID: "cooper-vm-test", ContainerName: "cooper-vm-test", RuntimeDir: "/state/run", Depth: 1}
	request := StartRequest{WorkspaceDir: "/work/project", ToolName: "codex", CPUs: 4, MemoryMiB: 4096, DiskGiB: 16}
	mounts := []workload.MountSpec{
		{ID: "workspace", Source: "/work/project", Target: "/work/project", Access: workload.ReadWrite},
		{ID: "git-hooks", Source: "/other/hooks", Target: "/other/hooks", Access: workload.ReadOnly},
	}
	_, err := supervisorDockerArgs(runtime, request, ImageArchive{ImageID: "sha256:" + strings.Repeat("a", 64)}, "/state/base.qcow2", mounts, strings.Repeat("b", 64), "", "/state/seccomp.json", 1001, 1002, 993)
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("supervisorDockerRunArgs() error = %v", err)
	}
}

func TestStopResourcesDerivesProxyNameAndRetriesActiveNetwork(t *testing.T) {
	t.Parallel()
	cooperDir := t.TempDir()
	runtimeDir := filepath.Join(cooperDir, "vm", "run", "unit-vm-project-codex-aabbccddeeff")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	networkAttempts := 0
	objectID := strings.Repeat("a", 64)
	runner := &recordingRunner{output: func(command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "docker container inspect") && strings.Contains(command, "-relay"):
			return []byte(objectID + "\tvm-relay\tunit-vm-project-codex-aabbccddeeff\n"), nil
		case strings.Contains(command, "docker container inspect"):
			return []byte(objectID + "\tvm-supervisor\tunit-vm-project-codex-aabbccddeeff\n"), nil
		case strings.Contains(command, "docker network inspect"):
			return []byte(objectID + "\tvm-relay-network\tunit-vm-project-codex-aabbccddeeff\n"), nil
		case strings.Contains(command, "docker rm -f"):
			return nil, nil
		case strings.Contains(command, "docker network disconnect"):
			if !strings.HasSuffix(command, " "+objectID+" unit-proxy") {
				t.Fatalf("proxy disconnect command = %q", command)
			}
			return nil, nil
		case strings.Contains(command, "docker network rm"):
			networkAttempts++
			if networkAttempts == 1 {
				return nil, errors.New("network has active endpoints")
			}
			return nil, nil
		default:
			t.Fatalf("unexpected command %q", command)
			return nil, nil
		}
	}}
	manager := Manager{CooperDir: cooperDir, Namespace: "unit", Runner: runner}
	runtime := Runtime{
		ID:            "unit-vm-project-codex-aabbccddeeff",
		ContainerName: "unit-vm-project-codex-aabbccddeeff",
		RelayName:     "unit-vm-project-codex-aabbccddeeff-relay",
		RelayNetwork:  "unit-vm-project-codex-aabbccddeeff-relay",
		RuntimeDir:    runtimeDir,
	}
	if err := manager.stopResources(context.Background(), runtime, true); err != nil {
		t.Fatalf("stopResources() failed: %v", err)
	}
	if networkAttempts != 2 {
		t.Fatalf("network remove attempts = %d, want 2", networkAttempts)
	}
	if _, err := os.Stat(runtimeDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime directory still exists or stat failed: %v", err)
	}
}

func TestStopResourcesPreservesDiagnosticsAfterDisconnectFailure(t *testing.T) {
	t.Parallel()
	cooperDir := t.TempDir()
	runtimeDir := filepath.Join(cooperDir, "vm", "run", "unit-vm-project-codex-aabbccddeeff")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	objectID := strings.Repeat("b", 64)
	runner := &recordingRunner{output: func(command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "docker container inspect") && strings.Contains(command, "-relay"):
			return []byte(objectID + "\tvm-relay\tunit-vm-project-codex-aabbccddeeff\n"), nil
		case strings.Contains(command, "docker container inspect"):
			return []byte(objectID + "\tvm-supervisor\tunit-vm-project-codex-aabbccddeeff\n"), nil
		case strings.Contains(command, "docker network inspect"):
			return []byte(objectID + "\tvm-relay-network\tunit-vm-project-codex-aabbccddeeff\n"), nil
		case strings.Contains(command, "docker network disconnect"):
			return nil, errors.New("permission denied")
		default:
			return nil, nil
		}
	}}
	manager := Manager{CooperDir: cooperDir, Namespace: "unit", ProxyName: "unit-proxy", Runner: runner}
	runtime := Runtime{
		ID:            "unit-vm-project-codex-aabbccddeeff",
		ContainerName: "unit-vm-project-codex-aabbccddeeff",
		RelayName:     "unit-vm-project-codex-aabbccddeeff-relay",
		RelayNetwork:  "unit-vm-project-codex-aabbccddeeff-relay",
		RuntimeDir:    runtimeDir,
	}
	err := manager.stopResources(context.Background(), runtime, true)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("stopResources() error = %v", err)
	}
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Fatalf("runtime diagnostics were removed: %v", err)
	}
}

func TestStopResourcesRefusesForeignDockerObject(t *testing.T) {
	t.Parallel()
	cooperDir := t.TempDir()
	runtimeID := "unit-vm-project-codex-aabbccddeeff"
	runtimeDir := filepath.Join(cooperDir, "vm", "run", runtimeID)
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	removeCalled := false
	runner := &recordingRunner{output: func(command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "docker container inspect"):
			return []byte(strings.Repeat("c", 64) + "\tcli\t" + runtimeID + "\n"), nil
		case strings.Contains(command, "docker network inspect"):
			return nil, errors.New("No such network")
		case strings.Contains(command, "docker rm -f"):
			removeCalled = true
			return nil, nil
		default:
			return nil, nil
		}
	}}
	manager := Manager{CooperDir: cooperDir, Namespace: "unit", ProxyName: "unit-proxy", Runner: runner}
	runtime := Runtime{
		ID: runtimeID, ContainerName: runtimeID, RelayName: RelayContainerName(runtimeID),
		RelayNetwork: RelayNetworkName(runtimeID), RuntimeDir: runtimeDir,
	}
	err := manager.stopResources(context.Background(), runtime, true)
	if err == nil || !strings.Contains(err.Error(), "ownership labels") {
		t.Fatalf("stopResources() error = %v", err)
	}
	if removeCalled {
		t.Fatal("stopResources removed a Docker object with foreign labels")
	}
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Fatalf("stopResources removed diagnostics after an ownership failure: %v", err)
	}
}
