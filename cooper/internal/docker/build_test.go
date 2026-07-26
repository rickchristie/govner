package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
