package chatgpt

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeProcess(t *testing.T, root, pid, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(root, pid)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string]string{"comm": name + "\n", "cmdline": strings.Join(args, "\x00")} {
		if err := os.WriteFile(filepath.Join(path, file), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestHostProcessNames(t *testing.T) {
	for _, test := range []struct {
		name, comm, want string
		args             []string
	}{
		{"desktop", "chatgpt", "ChatGPT", []string{"/usr/lib/chatgpt/chatgpt"}},
		{"old desktop", "Codex", "Codex", []string{"/opt/Codex"}},
		{"native CLI", "codex", "Codex", []string{"/opt/codex", "resume"}},
		{"desktop engine", "codex", "Codex", []string{"/usr/lib/chatgpt/resources/codex", "app-server"}},
		{"changed argv", "codex", "Codex", []string{"session"}},
		{"npm launcher", "node", "Codex", []string{"node", "/opt/lib/node_modules/@openai/codex/bin/codex.js"}},
		{"npm bin link", "node", "Codex", []string{"node", "/usr/local/bin/codex"}},
		{"desktop launcher", "bash", "ChatGPT", []string{"bash", "/usr/lib/chatgpt/chatgpt.sh"}},
		{"shell tool", "bash", "Codex", []string{"sh", "/usr/bin/codex"}},
		{"argument only", "grep", "", []string{"grep", "codex"}},
		{"shell command text", "bash", "", []string{"bash", "-c", "codex --version"}},
		{"unrelated node", "node", "", []string{"node", "/work/server.js", "codex"}},
		{"unrelated script", "node", "", []string{"node", "/work/codex.js"}},
		{"helper", "codex-code-mode", "", []string{"codex-code-mode"}},
		{"Cooper command", "cooper", "", []string{"cooper", "vm", "chatgpt"}},
		{"exited process", "codex", "", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeProcess(t, root, "123", test.comm, test.args...)
			err := checkHostProcesses(t.Context(), root, 999)
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "host "+test.want+" process 123 is running") || !strings.Contains(err.Error(), "close all ChatGPT apps and Codex CLI sessions") {
				t.Fatalf("process was not identified: %v", err)
			}
		})
	}
}

func TestHostProcessScanHandlesExitAndSelf(t *testing.T) {
	root := t.TempDir()
	writeProcess(t, root, "123", "codex", "codex")
	writeProcess(t, root, "456", "codex")
	if err := os.Mkdir(filepath.Join(root, "789"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "meminfo"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkHostProcesses(t.Context(), root, 123); err != nil {
		t.Fatal(err)
	}
}

func TestHostProcessScanFailsWhenItCannotRead(t *testing.T) {
	root := t.TempDir()
	if err := checkHostProcesses(t.Context(), filepath.Join(root, "absent"), 0); err == nil {
		t.Fatal("missing process view was accepted")
	}
	path := writeProcess(t, root, "123", "codex", "codex")
	if err := os.Remove(filepath.Join(path, "cmdline")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, "cmdline"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := checkHostProcesses(t.Context(), root, 0); err == nil || !strings.Contains(err.Error(), "cannot check host process 123") {
		t.Fatalf("unreadable process was accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := checkHostProcesses(ctx, root, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled scan: %v", err)
	}
}

func TestHostProcessChild(t *testing.T) {
	if os.Getenv("COOPER_HOST_PROCESS_CHILD") != "1" {
		t.Skip("process fixture")
	}
	if _, err := os.Stdout.Write([]byte("ready")); err != nil {
		t.Fatal(err)
	}
	_, _ = os.Stdin.Read(make([]byte, 1))
}

func TestLiveHostProcessIsDetectedWithoutDesktopLock(t *testing.T) {
	for _, name := range []string{"chatgpt", "codex"} {
		t.Run(name, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHostProcessChild$")
			command.Args[0] = name
			command.Env = append(os.Environ(), "COOPER_HOST_PROCESS_CHILD=1")
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
			tool, err := hostProcessTool(filepath.Join("/proc", strconv.Itoa(command.Process.Pid)))
			if err != nil || tool != hostToolName(name) {
				t.Fatalf("live process: %q, %v", tool, err)
			}
			if err := CheckHostProcesses(t.Context()); err == nil {
				t.Fatal("live host process did not block the VM")
			}
		})
	}
}
