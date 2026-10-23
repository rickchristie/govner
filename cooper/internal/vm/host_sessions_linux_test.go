package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func TestVMHostSessionChild(t *testing.T) {
	if os.Getenv("COOPER_VM_HOST_SESSION_CHILD") != "1" {
		t.Skip("process fixture")
	}
	_, _ = os.Stdout.Write([]byte("ready"))
	_, _ = os.Stdin.Read(make([]byte, 1))
}

func startHostSessionProcess(t *testing.T) {
	t.Helper()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestVMHostSessionChild$")
	command.Args[0] = "codex"
	command.Env = append(os.Environ(), "COOPER_VM_HOST_SESSION_CHILD=1")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close(); command.Wait() })
	if _, err := io.ReadFull(output, make([]byte, 5)); err != nil {
		t.Fatal(err)
	}
}

func TestHostSessionRefusesStartAndRestartBeforeChanges(t *testing.T) {
	startHostSessionProcess(t)
	for _, tool := range []string{"codex", "claude", "vm-test-agent"} {
		if err := CheckHostSessions(t.Context(), tool); err != nil {
			t.Fatalf("unrelated VM tool %s was blocked: %v", tool, err)
		}
	}
	manager := Manager{
		CooperDir: filepath.Join(t.TempDir(), "cooper"), HomeDir: t.TempDir(),
		Namespace: "test", ProxyName: "test-proxy", Config: config.DefaultConfig(),
		Runner: &recordingRunner{output: func(command string) ([]byte, error) {
			t.Fatalf("host refusal reached an external command: %s", command)
			return nil, nil
		}},
	}
	checkRefused := func(err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "cannot start ChatGPT VM: host ") {
			t.Fatalf("host process was not refused: %v", err)
		}
	}
	_, err := manager.Start(t.Context(), StartRequest{ToolName: "chatgpt"})
	checkRefused(err)
	if _, err := os.Stat(manager.CooperDir); !os.IsNotExist(err) {
		t.Fatalf("refused start created runtime state: %v", err)
	}
	metadata := RuntimeMetadata{
		Schema: runtimeMetadataSchema, RuntimeID: "test-vm-project-chatgpt-aabbccddeeff", ToolName: "chatgpt",
		WorkspaceDir: t.TempDir(), ImageRef: "test-image", ImageID: "sha256:" + strings.Repeat("a", 64), Depth: 1,
		CPUs: 2, MemoryMiB: 2048, DiskGiB: 8, ClipboardMode: "shim", MountPlanSHA256: strings.Repeat("b", 64),
	}
	directory := RuntimeDir(manager.CooperDir, metadata.RuntimeID)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "runtime.json")
	if err := writeJSON(path, metadata, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Restart(t.Context(), metadata.RuntimeID)
	checkRefused(err)
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("refused restart changed its metadata: %v", err)
	}
	checkRefused(manager.StartDesktop(t.Context(), Runtime{ToolName: "chatgpt"}))
}

func isolatedHostSessionFixture(t *testing.T) (Manager, StartRequest, func()) {
	t.Helper()
	for _, name := range []string{"CODEX_HOME", "CODEX_ELECTRON_USER_DATA_PATH", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		t.Setenv(name, "")
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	release, err := RegisterIsolatedTestHome(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	workspace := filepath.Join(home, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	return Manager{HomeDir: home}, StartRequest{ToolName: "chatgpt", WorkspaceDir: workspace}, release
}

func TestIsolatedTestHomeKeepsTheNormalGuard(t *testing.T) {
	startHostSessionProcess(t)
	manager, request, release := isolatedHostSessionFixture(t)
	if err := manager.checkHostSessions(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := CheckHostSessions(t.Context(), "chatgpt"); err == nil {
		t.Fatal("test registration disabled the normal launch guard")
	}
	release()
	if err := manager.checkHostSessions(t.Context(), request); err == nil {
		t.Fatal("released test registration still allowed the VM")
	}
}

func TestIsolatedTestHomeRejectsLiveState(t *testing.T) {
	for _, name := range []string{"CODEX_HOME", "CODEX_ELECTRON_USER_DATA_PATH", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Run(name, func(t *testing.T) {
			manager, request, _ := isolatedHostSessionFixture(t)
			t.Setenv(name, t.TempDir())
			if err := manager.checkHostSessions(t.Context(), request); err == nil {
				t.Fatal("test VM accepted a state root outside its home")
			}
		})
	}
	for _, name := range []string{"workspace", "whole home", "named profile", "symlink", "hard link", "replaced home", "permissions"} {
		t.Run(name, func(t *testing.T) {
			manager, request, _ := isolatedHostSessionFixture(t)
			outside := t.TempDir()
			switch name {
			case "workspace":
				request.WorkspaceDir = outside
			case "whole home":
				request.WorkspaceDir = manager.HomeDir
			case "named profile":
				request.ProfileID = "another-account"
			case "symlink":
				if err := os.Symlink(outside, filepath.Join(request.WorkspaceDir, "state")); err != nil {
					t.Fatal(err)
				}
			case "hard link":
				path := filepath.Join(outside, "state.sqlite")
				if err := os.WriteFile(path, []byte("host-state"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(path, filepath.Join(request.WorkspaceDir, "state.sqlite")); err != nil {
					t.Fatal(err)
				}
			case "replaced home":
				if err := os.Rename(manager.HomeDir, manager.HomeDir+"-old"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(manager.HomeDir + "-old") })
				if err := os.Mkdir(manager.HomeDir, 0700); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.Chmod(manager.HomeDir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := manager.checkHostSessions(t.Context(), request); err == nil {
				t.Fatal("test VM accepted unsafe state")
			}
		})
	}
}

func TestIsolatedTestMountsRejectChangedSources(t *testing.T) {
	manager, _, _ := isolatedHostSessionFixture(t)
	root := filepath.Join(manager.HomeDir, ".codex")
	for _, mount := range []workload.MountSpec{
		{Source: t.TempDir(), Target: root},
		{Source: root, Target: t.TempDir()},
	} {
		if err := manager.checkTestStateMounts("chatgpt", []workload.MountSpec{mount}); err == nil {
			t.Fatal("test VM accepted a state mount outside its home")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := manager.checkHostSessions(ctx, StartRequest{ToolName: "chatgpt"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled test launch: %v", err)
	}
}

func TestIsolatedTestHomeRegistrationRequiresEmptyPrivateState(t *testing.T) {
	checkRefused := func(home, want string) {
		t.Helper()
		release, err := RegisterIsolatedTestHome(home)
		if err == nil {
			release()
			t.Fatal("unsafe test home was registered")
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("registration error = %v, want %q", err, want)
		}
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "existing-state"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	checkRefused(home, "must be empty")
	private := t.TempDir()
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	checkRefused(link, "cannot use symlinks")
	if err := os.Chmod(private, 0755); err != nil {
		t.Fatal(err)
	}
	checkRefused(private, "private owned directory")
	checkRefused("relative", "clean absolute path")
	if err := os.Chmod(private, 0700); err != nil {
		t.Fatal(err)
	}
	release, err := RegisterIsolatedTestHome(private)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	checkRefused(private, "already registered")
}

func TestIsolatedTestHomeIsUnavailableInNormalBuild(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot find Cooper module")
	}
	module := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	directory := t.TempDir()
	goMod := fmt.Sprintf("module github.com/rickchristie/govner/cooper/testguard\n\ngo 1.25.0\n\nrequire github.com/rickchristie/govner/cooper v0.0.0\nreplace github.com/rickchristie/govner/cooper => %q\n", module)
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(goMod), 0600); err != nil {
		t.Fatal(err)
	}
	program := `package main
import (
    "fmt"
    "os"
    "github.com/rickchristie/govner/cooper/internal/vm"
)
func main() {
    home, err := os.MkdirTemp("", "cooper-normal-guard-")
    if err != nil { panic(err) }
    defer os.RemoveAll(home)
    if release, err := vm.RegisterIsolatedTestHome(home); err != nil {
        fmt.Println(err)
    } else {
        release()
        panic("normal build accepted the test exception")
    }
}
`
	if err := os.WriteFile(filepath.Join(directory, "main.go"), []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "run", "-mod=mod", ".")
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "available only in Go tests") {
		t.Fatalf("normal build did not refuse the test exception: %v\n%s", err, output)
	}
}
