package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/vmcontext"
)

func TestBuildDockerArgsUseOnlyVerifiedNestedProxy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.json")
	context := vmcontext.Context{
		Schema: vmcontext.Schema, Depth: 1,
		ParentNetwork: "cooper-control", ParentProxy: "172.30.0.1",
		AgentContainer: "cooper-outer-agent",
		ProxyPort:      3128, BridgePort: 4343,
		WorkspaceDir: "/work/project", TempDir: "/tmp", CooperDir: "/home/user/.cooper",
	}
	if err := vmcontext.Write(path, context); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COOPER_VM_CONTEXT", path)
	t.Setenv("HTTP_PROXY", "http://attacker.invalid:9999")

	args, err := buildDockerArgs("image", "Dockerfile", ".", map[string]string{"USER_UID": "1000"}, false)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"HTTP_PROXY=http://172.30.0.1:3128", "HTTPS_PROXY=http://172.30.0.1:3128",
		"NO_PROXY=localhost,127.0.0.1,172.30.0.1", "http_proxy=http://172.30.0.1:3128",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("nested build arguments do not contain %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "attacker.invalid") {
		t.Fatalf("nested build accepted an environment proxy: %s", joined)
	}
}

func TestBuildImageWithOutputStreamsCombinedOutputWithoutLineLimit(t *testing.T) {
	binDir := t.TempDir()
	longLine := strings.Repeat("x", 70*1024)
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '" + longLine + "'\n" +
		"printf '%s\\n' 'stderr line' >&2\n"
	dockerPath := filepath.Join(binDir, "docker")
	if err := os.WriteFile(dockerPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	lines, errc := BuildImageWithOutput("test", "Dockerfile", ".", nil, false)
	var got []string
	for line := range lines {
		got = append(got, line)
	}
	if err := <-errc; err != nil {
		t.Fatalf("BuildImageWithOutput() failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("output lines = %d, want 2", len(got))
	}
	if got[0] != longLine {
		t.Fatalf("long stdout line length = %d, want %d", len(got[0]), len(longLine))
	}
	if got[1] != "stderr line" {
		t.Fatalf("stderr line = %q, want %q", got[1], "stderr line")
	}
}

func TestBuildImageWithOutputReportsStartFailureAndClosesChannels(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	lines, errc := BuildImageWithOutput("test", "Dockerfile", ".", nil, false)
	if _, ok := <-lines; ok {
		t.Fatal("line channel remained open after docker start failure")
	}
	err, ok := <-errc
	if !ok || err == nil {
		t.Fatal("expected a concrete docker start error")
	}
	if !strings.Contains(err.Error(), "failed to start") {
		t.Fatalf("start error = %q", err)
	}
}
