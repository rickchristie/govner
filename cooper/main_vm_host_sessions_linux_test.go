package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/launch"
	"github.com/rickchristie/govner/cooper/internal/vm"
	"github.com/spf13/cobra"
)

func TestVMCommandHostProcessChild(t *testing.T) {
	if os.Getenv("COOPER_VM_COMMAND_PROCESS_CHILD") != "1" {
		t.Skip("process fixture")
	}
	_, _ = os.Stdout.Write([]byte("ready"))
	_, _ = os.Stdin.Read(make([]byte, 1))
}

func TestVMChatGPTChecksHostBeforeConfigAndProfileChanges(t *testing.T) {
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestVMCommandHostProcessChild$")
	command.Args[0] = "codex"
	command.Env = append(os.Environ(), "COOPER_VM_COMMAND_PROCESS_CHILD=1")
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
	previous := configDir
	configDir = filepath.Join(t.TempDir(), "missing-config")
	t.Cleanup(func() { configDir = previous })
	cmd := &cobra.Command{Use: "vm"}
	cmd.SetContext(t.Context())
	for _, args := range [][]string{{"chatgpt"}, {" CHATGPT "}, {"chatgpt", "Work"}} {
		for _, check := range []func(*cobra.Command, []string) error{rootCmd.PersistentPreRunE, runVM} {
			err := check(cmd, args)
			if err == nil || !strings.Contains(err.Error(), "cannot start ChatGPT VM: host ") {
				t.Fatalf("host check did not run first for %q: %v", args, err)
			}
		}
	}
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("refused launch changed configuration: %v", err)
	}
	// A host session can start while the guest boots. Check once more before
	// sending the command that starts the desktop inside the guest.
	err = executeVMSession(t.Context(), vm.Manager{}, vm.Runtime{ToolName: "chatgpt"}, &launch.Session{})
	if err == nil || !strings.Contains(err.Error(), "cannot start ChatGPT VM: host ") {
		t.Fatalf("session execution did not check the host: %v", err)
	}
}
