package vm

import (
	"strings"
	"testing"
)

func TestRuntimeIDIsStableAndPathSpecific(t *testing.T) {
	t.Parallel()
	first, err := RuntimeID("cooper", "/work/one/project", "codex")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := RuntimeID("cooper", "/work/one/project", "codex")
	other, _ := RuntimeID("cooper", "/work/two/project", "codex")
	if first != again {
		t.Fatalf("stable runtime ID changed: %q != %q", first, again)
	}
	if first == other {
		t.Fatalf("different workspace paths share runtime ID %q", first)
	}
	if !strings.HasPrefix(first, "cooper-vm-project-codex-") {
		t.Fatalf("runtime ID = %q", first)
	}
}

func TestRuntimeIDNormalizesAndBoundsDockerName(t *testing.T) {
	t.Parallel()
	workspace := "/work/" + strings.Repeat("Long Name:世界", 30)
	got, err := RuntimeID("Test Prefix", workspace, "CoDex")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > dockerNameLimit {
		t.Fatalf("runtime ID has %d bytes: %q", len(got), got)
	}
	if strings.ContainsAny(got, " :世界") {
		t.Fatalf("runtime ID has unsafe characters: %q", got)
	}
}

func TestRuntimeIDRejectsUnusableParts(t *testing.T) {
	t.Parallel()
	if _, err := RuntimeID("***", "/work/project", "codex"); err == nil {
		t.Fatal("expected unusable namespace error")
	}
}

func TestControlSocketPathStaysBelowLinuxLimit(t *testing.T) {
	t.Parallel()
	cooperDir := "/tmp/" + strings.Repeat("very-long-cooper-directory-", 20)
	runtimeID := strings.Repeat("a", dockerNameLimit)
	path := ControlSocketPath(cooperDir, runtimeID)
	if len(path) >= 108 {
		t.Fatalf("control socket path has %d bytes: %s", len(path), path)
	}
	if path == ControlSocketPath(cooperDir+"-other", runtimeID) || path == ControlSocketPath(cooperDir, runtimeID+"b") {
		t.Fatal("control socket path does not isolate Cooper directory and runtime identity")
	}
}
