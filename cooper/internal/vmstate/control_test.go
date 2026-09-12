package vmstate

import (
	"path/filepath"
	"testing"
)

func TestControlSocketPathIsStableAndBounded(t *testing.T) {
	t.Parallel()
	first := ControlSocketPath("/a/very/long/Cooper path", "cooper-vm-project-codex-identity")
	second := ControlSocketPath("/a/very/long/Cooper path", "cooper-vm-project-codex-identity")
	other := ControlSocketPath("/a/very/long/Cooper path", "cooper-vm-other-codex-identity")
	if first != second || first == other {
		t.Fatalf("control socket paths = %q, %q, %q", first, second, other)
	}
	if filepath.Base(first) != "control.sock" || len(first) >= 108 {
		t.Fatalf("control socket path is invalid or too long: %q", first)
	}
}
