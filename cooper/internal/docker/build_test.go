package docker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/vmcontext"
)

// Use real, network-free images to check reuse through both build entry points.
// Image IDs alone are insufficient: Docker can run a build and return the same ID.
func TestBuildReusesUnchangedImages(t *testing.T) {
	previousPrefix := ImagePrefix()
	SetImagePrefix("cooper-cache-test-")
	t.Cleanup(func() { SetImagePrefix(previousPrefix) })
	root := t.TempDir()
	names := []string{GetImageBase(), GetImageCLI("codex"), GetImageCLI("claude")}
	for _, name := range names {
		t.Cleanup(func() { _ = RemoveImage(name) })
		if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, file, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name, file), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(names[0], "Dockerfile", "FROM scratch\nCOPY ca.pem /ca.pem\n")
	write(names[0], "ca.pem", "test CA one\n")
	for _, name := range names[1:] {
		write(name, "Dockerfile", "FROM "+names[0]+"\nCOPY version /version\n")
		write(name, "version", "one\n")
	}
	build := func(name string, clean bool) bool {
		t.Helper()
		dir := filepath.Join(root, name)
		var out bytes.Buffer
		if name == names[1] {
			lines, errs := BuildImageWithOutput(name, filepath.Join(dir, "Dockerfile"), dir, nil, clean)
			for line := range lines {
				fmt.Fprintln(&out, line)
			}
			if err := <-errs; err != nil {
				t.Fatalf("build %s: %v\n%s", name, err, out.String())
			}
		} else if err := BuildImageWithWriter(name, filepath.Join(dir, "Dockerfile"), dir, nil, clean, &out); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out.String())
		}
		return strings.Contains(out.String(), "Reusing "+name+" image")
	}
	checkBuilds := func(want []bool) {
		t.Helper()
		for i, name := range names {
			if reused := build(name, false); reused != want[i] {
				t.Fatalf("%s reused = %v, want %v", name, reused, want[i])
			}
		}
	}
	imageID := func(name string) string {
		t.Helper()
		data, err := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", name).Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(data))
	}
	checkBuilds([]bool{false, false, false})
	checkBuilds([]bool{true, true, true})
	previousID := imageID(names[1])
	// Template regeneration changes times, but it must not change build inputs.
	write(names[0], "ca.pem", "test CA one\n")
	write(names[1], "version", "two\n")
	checkBuilds([]bool{true, false, true})
	if imageID(names[1]) == previousID {
		t.Fatal("changed agent retained its old image")
	}
	// A changed shared input must rebuild both children, including the unchanged agent.
	write(names[0], "ca.pem", "test CA two\n")
	checkBuilds([]bool{false, false, false})
	if err := RemoveImage(names[1]); err != nil {
		t.Fatal(err)
	}
	checkBuilds([]bool{true, false, true})
	if build(names[1], true) {
		t.Fatal("clean build reused an image")
	}
	checkBuilds([]bool{true, true, true})
	previousID = imageID(names[1])
	write(names[1], "Dockerfile", "FROM "+names[0]+"\nCOPY missing /missing\n")
	dir := filepath.Join(root, names[1])
	var failure bytes.Buffer
	if err := BuildImageWithWriter(names[1], filepath.Join(dir, "Dockerfile"), dir, nil, false, &failure); err == nil {
		t.Fatal("build with missing input succeeded")
	}
	if imageID(names[1]) != previousID {
		t.Fatal("failed build replaced the working image")
	}
	write(names[1], "Dockerfile", "FROM "+names[0]+"\nCOPY version /version\n")
	checkBuilds([]bool{true, true, true})
	// Replacing the tag with a derived image must not inherit a reuse decision.
	replacement := t.TempDir()
	if err := os.WriteFile(filepath.Join(replacement, "Dockerfile"), []byte("FROM "+names[1]+"\nLABEL replacement=true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("docker", "build", "-t", names[1], replacement).CombinedOutput(); err != nil {
		t.Fatalf("replace image: %v\n%s", err, out)
	}
	checkBuilds([]bool{true, false, true})
	// A damaged record must not block a build or reuse an unverified image.
	recordPath, err := imageBuildRecordPath(names[1], dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordPath, []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	checkBuilds([]bool{true, false, true})
}

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
