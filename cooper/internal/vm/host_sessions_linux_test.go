package vm

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestVMHostSessionChild(t *testing.T) {
	if os.Getenv("COOPER_VM_HOST_SESSION_CHILD") != "1" {
		t.Skip("process fixture")
	}
	_, _ = os.Stdout.Write([]byte("ready"))
	_, _ = os.Stdin.Read(make([]byte, 1))
}

func TestHostSessionRefusesStartAndRestartBeforeChanges(t *testing.T) {
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
	_, err = manager.Start(t.Context(), StartRequest{ToolName: "chatgpt"})
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
