package profilemanager

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func TestLiveHostHarnessBlocksItsRoots(t *testing.T) {
	home := t.TempDir()
	command := exec.Command("bash", "-c", "exec -a codex sleep 30")
	command.Env = append(os.Environ(), "HOME="+home, "CODEX_HOME="+filepath.Join(home, ".codex"))
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := processStateUse(t.Context(), filepath.Join("/proc", strconv.Itoa(command.Process.Pid)), command.Process.Pid, []string{filepath.Join(home, ".codex")})
		var issue *profiles.Issue
		if errors.As(err, &issue) && issue.Kind == profiles.StateInUse {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live fixture harness was not found: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := processStateUse(t.Context(), filepath.Join("/proc", strconv.Itoa(command.Process.Pid)), command.Process.Pid, []string{t.TempDir()}); err != nil {
		t.Fatalf("unrelated host roots were blocked: %v", err)
	}
}

func TestProcessStateChild(t *testing.T) {
	kind, path := os.Getenv("COOPER_PROCESS_FIXTURE_KIND"), os.Getenv("COOPER_PROCESS_FIXTURE_PATH")
	if kind == "" {
		t.Skip("process fixture")
	}
	if kind == "nondumpable" {
		if _, _, err := syscall.RawSyscall(syscall.SYS_PRCTL, 4, 0, 0); err != 0 { // PR_SET_DUMPABLE
			t.Fatal(err)
		}
	} else if kind == "cwd" {
		if err := os.Chdir(path); err != nil {
			t.Fatal(err)
		}
	} else {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		mapping, err := syscall.Mmap(int(file.Fd()), 0, 4096, syscall.PROT_READ, syscall.MAP_SHARED)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.Munmap(mapping)
	}
	os.Stdout.Write([]byte("ready"))
	os.Stdin.Read(make([]byte, 1))
}

func TestNonDumpableProcessStillChecksKnownHarness(t *testing.T) {
	for _, name := range []string{"service", "chatgpt"} {
		t.Run(name, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestProcessStateChild$")
			command.Args[0] = name
			command.Env = append(os.Environ(), "COOPER_PROCESS_FIXTURE_KIND=nondumpable")
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
			defer func() { input.Close(); command.Wait() }()
			if _, err := io.ReadFull(output, make([]byte, 5)); err != nil {
				t.Fatal(err)
			}
			base := filepath.Join("/proc", strconv.Itoa(command.Process.Pid))
			if _, err := os.ReadFile(filepath.Join(base, "environ")); !errors.Is(err, os.ErrPermission) {
				t.Skip("this account can inspect non-dumpable processes")
			}
			if err := openStateUse(t.Context(), base, command.Process.Pid, []string{t.TempDir()}); err != nil {
				t.Fatalf("unreadable extra file check must defer to the harness check: %v", err)
			}
			err = processStateUse(t.Context(), filepath.Join("/proc", strconv.Itoa(command.Process.Pid)), command.Process.Pid, []string{t.TempDir()})
			if name == "service" && err != nil {
				t.Fatalf("unrelated service: %v", err)
			}
			if name == "chatgpt" && err == nil {
				t.Fatal("unreadable ChatGPT environment was accepted")
			}
		})
	}
}

func TestWorkingDirectoryAndClosedFileMappingBlockSwitch(t *testing.T) {
	for _, kind := range []string{"cwd", "mapping"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := root
			if kind == "mapping" {
				path = filepath.Join(root, "state with spaces")
				if err := os.WriteFile(path, make([]byte, 4096), 0600); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command(os.Args[0], "-test.run=^TestProcessStateChild$")
			command.Env = append(os.Environ(), "COOPER_PROCESS_FIXTURE_KIND="+kind, "COOPER_PROCESS_FIXTURE_PATH="+path)
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
			defer func() { input.Close(); command.Wait() }()
			if _, err := io.ReadFull(output, make([]byte, 5)); err != nil {
				t.Fatal(err)
			}
			var issue *profiles.Issue
			if err := processStateUse(t.Context(), filepath.Join("/proc", strconv.Itoa(command.Process.Pid)), command.Process.Pid, []string{root}); !errors.As(err, &issue) || issue.Kind != profiles.StateInUse {
				t.Fatalf("%s use not found: %v", kind, err)
			}
		})
	}
}

func TestUnknownWriterWithOpenStateBlocksSwitch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session")
	if err := os.WriteFile(path, []byte("state"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", "-c", `exec 9<> "$1"; printf ready; read answer`, "fixture", path)
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
	defer func() { input.Close(); command.Wait() }()
	ready := make([]byte, 5)
	if _, err := io.ReadFull(output, ready); err != nil {
		t.Fatal(err)
	}
	if err := processStateUse(t.Context(), filepath.Join("/proc", strconv.Itoa(command.Process.Pid)), command.Process.Pid, []string{root}); err == nil {
		t.Fatal("unknown writer was ignored")
	}
}
