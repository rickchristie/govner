//go:build linux

package clipboard

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// fakeEnv returns a lookup function that reads from a map.
func fakeEnv(vars map[string]string) func(string) string {
	return func(key string) string {
		return vars[key]
	}
}

// --- Backend detection ---

func TestDetectBackendWayland(t *testing.T) {
	env := fakeEnv(map[string]string{"WAYLAND_DISPLAY": "wayland-0"})
	r := NewLinuxReader(env)
	if r.Backend() != backendWayland {
		t.Fatalf("expected wayland backend, got %s", r.Backend())
	}
}

func TestDetectBackendX11Fallback(t *testing.T) {
	env := fakeEnv(map[string]string{})
	r := NewLinuxReader(env)
	if r.Backend() != backendX11 {
		t.Fatalf("expected x11 backend, got %s", r.Backend())
	}
}

func TestDetectBackendX11WhenWaylandEmpty(t *testing.T) {
	env := fakeEnv(map[string]string{"WAYLAND_DISPLAY": ""})
	r := NewLinuxReader(env)
	if r.Backend() != backendX11 {
		t.Fatalf("expected x11 backend when WAYLAND_DISPLAY is empty, got %s", r.Backend())
	}
}

// --- Best image type selection ---

func TestPickBestImageType_PrefersPNG(t *testing.T) {
	targets := []string{"text/plain", "image/jpeg", "image/png", "image/bmp"}
	got := pickBestImageType(targets)
	if got != "image/png" {
		t.Fatalf("expected image/png, got %s", got)
	}
}

func TestPickBestImageType_FallsBackToFirstImage(t *testing.T) {
	targets := []string{"text/plain", "image/jpeg", "image/bmp"}
	got := pickBestImageType(targets)
	if got != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %s", got)
	}
}

func TestPickBestImageType_NoImageTargets(t *testing.T) {
	targets := []string{"text/plain", "text/html", "UTF8_STRING"}
	got := pickBestImageType(targets)
	if got != "" {
		t.Fatalf("expected empty string, got %s", got)
	}
}

func TestPickBestImageType_EmptyList(t *testing.T) {
	got := pickBestImageType(nil)
	if got != "" {
		t.Fatalf("expected empty string for nil targets, got %s", got)
	}
}

// --- Extension mapping ---

func TestExtensionForMIME(t *testing.T) {
	cases := []struct {
		mime string
		ext  string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"image/svg+xml", ".svg"},
		{"image/x-custom", ".x-custom"},
		{"application/octet-stream", ".bin"},
	}
	for _, tc := range cases {
		got := extensionForMIME(tc.mime)
		if got != tc.ext {
			t.Errorf("extensionForMIME(%q) = %q, want %q", tc.mime, got, tc.ext)
		}
	}
}

// --- Mocked exec tests ---

// withMockExec replaces execCommand with a mock that records calls and returns
// predetermined output. It restores the original after the test.
func withMockExec(t *testing.T, handler func(ctx context.Context, name string, args ...string) *exec.Cmd) {
	t.Helper()
	original := execCommand
	execCommand = handler
	t.Cleanup(func() { execCommand = original })
}

// fakeExecSuccess creates a Cmd that prints the given stdout and exits 0.
// Uses cat with a heredoc so newlines and special characters pass through.
func fakeExecSuccess(_ context.Context, stdout string) *exec.Cmd {
	// Write stdout to a temp pipe via /dev/stdin workaround.
	// Simplest reliable method: use "echo" with -e or "printf" with $'...'
	// But safest: use the Go command with an explicit stdin pipe.
	cmd := exec.Command("cat")
	cmd.Stdin = strings.NewReader(stdout)
	return cmd
}

// fakeExecFailure creates a Cmd that prints to stderr and exits 1.
func fakeExecFailure(_ context.Context, stderr string) *exec.Cmd {
	return exec.Command("sh", "-c", fmt.Sprintf("cat >&2 <<'FAKEERR'\n%s\nFAKEERR\nexit 1", stderr))
}

func TestReadWaylandSuccess(t *testing.T) {
	withMockExec(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		switch {
		case name == "wl-paste" && len(args) > 0 && args[0] == "--list-types":
			return fakeExecSuccess(ctx, "text/plain\nimage/png\nimage/jpeg\n")
		case name == "wl-paste" && len(args) >= 2 && args[0] == "--type" && args[1] == "image/png":
			// Return fake PNG data as a recognizable string.
			return fakeExecSuccess(ctx, "fake-png-image-data")
		default:
			t.Fatalf("unexpected exec call: %s %v", name, args)
			return nil
		}
	})

	env := fakeEnv(map[string]string{"WAYLAND_DISPLAY": "wayland-0"})
	r := NewLinuxReader(env)
	result, err := r.Read(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MIME != "image/png" {
		t.Errorf("expected MIME image/png, got %s", result.MIME)
	}
	if result.Extension != ".png" {
		t.Errorf("expected extension .png, got %s", result.Extension)
	}
	if result.Filename != "clipboard.png" {
		t.Errorf("expected filename clipboard.png, got %s", result.Filename)
	}
	if len(result.Bytes) == 0 {
		t.Error("expected non-empty bytes")
	}
	if len(result.OriginalTargets) != 3 {
		t.Errorf("expected 3 targets, got %d", len(result.OriginalTargets))
	}
}

func TestReadX11Success(t *testing.T) {
	withMockExec(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		joined := strings.Join(args, " ")
		switch {
		case name == "xclip" && strings.Contains(joined, "TARGETS"):
			return fakeExecSuccess(ctx, "TARGETS\nimage/png\ntext/plain\n")
		case name == "xclip" && strings.Contains(joined, "image/png") && strings.Contains(joined, "-o"):
			return fakeExecSuccess(ctx, "fake-png-data")
		default:
			t.Fatalf("unexpected exec call: %s %v", name, args)
			return nil
		}
	})

	env := fakeEnv(map[string]string{})
	r := NewLinuxReader(env)
	result, err := r.Read(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MIME != "image/png" {
		t.Errorf("expected MIME image/png, got %s", result.MIME)
	}
	if len(result.Bytes) == 0 {
		t.Error("expected non-empty bytes")
	}
}

func TestReadNoImageInClipboard(t *testing.T) {
	withMockExec(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		// Return only text targets, no image types.
		return fakeExecSuccess(ctx, "text/plain\nUTF8_STRING\nTEXT\n")
	})

	env := fakeEnv(map[string]string{"WAYLAND_DISPLAY": "wayland-0"})
	r := NewLinuxReader(env)
	_, err := r.Read(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no image in clipboard") {
		t.Fatalf("expected 'no image in clipboard' error, got: %v", err)
	}
}

func TestReadListTargetsFails(t *testing.T) {
	withMockExec(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return fakeExecFailure(ctx, "wl-paste error: no clipboard")
	})

	env := fakeEnv(map[string]string{"WAYLAND_DISPLAY": "wayland-0"})
	r := NewLinuxReader(env)
	_, err := r.Read(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to list clipboard targets") {
		t.Fatalf("expected 'failed to list clipboard targets' error, got: %v", err)
	}
}

func TestReadContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	withMockExec(t, func(_ context.Context, name string, args ...string) *exec.Cmd {
		// Return a command that sleeps, but the context is already cancelled.
		// The test relies on ctx.Err() being checked after cmd.Run fails.
		return exec.CommandContext(ctx, "sleep", "10")
	})

	env := fakeEnv(map[string]string{"WAYLAND_DISPLAY": "wayland-0"})
	r := NewLinuxReader(env)
	_, err := r.Read(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if ctx.Err() == nil {
		t.Fatal("expected context to be cancelled")
	}
}

// --- CheckPrerequisites ---

func TestCheckPrerequisitesWaylandMissing(t *testing.T) {
	withMockExec(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name == "which" && len(args) > 0 && args[0] == "wl-paste" {
			return fakeExecFailure(ctx, "")
		}
		return fakeExecSuccess(ctx, "/usr/bin/"+args[0])
	})

	env := fakeEnv(map[string]string{"WAYLAND_DISPLAY": "wayland-0"})
	r := NewLinuxReader(env)
	err := r.CheckPrerequisites(context.Background())
	if err == nil {
		t.Fatal("expected error for missing wl-paste")
	}
	if !strings.Contains(err.Error(), "clipboard tool not found") {
		t.Fatalf("expected 'clipboard tool not found' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "wl-clipboard") {
		t.Fatalf("expected install hint for wl-clipboard, got: %v", err)
	}
}

func TestCheckPrerequisitesX11Missing(t *testing.T) {
	withMockExec(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name == "which" && len(args) > 0 && args[0] == "xclip" {
			return fakeExecFailure(ctx, "")
		}
		return fakeExecSuccess(ctx, "/usr/bin/"+args[0])
	})

	env := fakeEnv(map[string]string{})
	r := NewLinuxReader(env)
	err := r.CheckPrerequisites(context.Background())
	if err == nil {
		t.Fatal("expected error for missing xclip")
	}
	if !strings.Contains(err.Error(), "clipboard tool not found") {
		t.Fatalf("expected 'clipboard tool not found' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "xclip") {
		t.Fatalf("expected install hint for xclip, got: %v", err)
	}
}

func TestCheckPrerequisitesMagickMissing(t *testing.T) {
	withMockExec(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name == "which" && len(args) > 0 && args[0] == "magick" {
			return fakeExecFailure(ctx, "")
		}
		return fakeExecSuccess(ctx, "/usr/bin/"+args[0])
	})

	env := fakeEnv(map[string]string{"WAYLAND_DISPLAY": "wayland-0"})
	r := NewLinuxReader(env)
	err := r.CheckPrerequisites(context.Background())
	if err == nil {
		t.Fatal("expected error for missing magick")
	}
	if !strings.Contains(err.Error(), "imagemagick") {
		t.Fatalf("expected install hint for imagemagick, got: %v", err)
	}
}

func TestCheckPrerequisitesAllPresent(t *testing.T) {
	withMockExec(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return fakeExecSuccess(ctx, "/usr/bin/tool")
	})

	env := fakeEnv(map[string]string{"WAYLAND_DISPLAY": "wayland-0"})
	r := NewLinuxReader(env)
	err := r.CheckPrerequisites(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}
