package profilemanager

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
		err := processUsage(t.Context(), []string{filepath.Join(home, ".codex")})
		var issue *profiles.Issue
		if errors.As(err, &issue) && issue.Kind == profiles.StateInUse {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live fixture harness was not found: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := processUsage(t.Context(), []string{t.TempDir()}); err != nil {
		t.Fatalf("unrelated host roots were blocked: %v", err)
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
	if err := processUsage(t.Context(), []string{root}); err == nil {
		t.Fatal("unknown writer was ignored")
	}
}
