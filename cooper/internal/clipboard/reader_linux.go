//go:build linux

package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// execCommand is a package-level variable to allow test mocking.
var execCommand = exec.CommandContext

// backendKind identifies the display server protocol.
type backendKind string

const (
	backendWayland backendKind = "wayland"
	backendX11     backendKind = "x11"
)

// LinuxReader reads image content from the host clipboard using
// wl-paste (Wayland) or xclip (X11).
type LinuxReader struct {
	// backend is resolved once at construction time.
	backend backendKind
	// envLookup is used to read environment variables. Defaults to os.Getenv
	// but can be replaced in tests.
	envLookup func(string) string
}

// NewLinuxReader creates a LinuxReader after detecting the display server.
func NewLinuxReader(envLookup func(string) string) *LinuxReader {
	return &LinuxReader{
		backend:   detectBackend(envLookup),
		envLookup: envLookup,
	}
}

// detectBackend checks WAYLAND_DISPLAY to choose between Wayland and X11.
func detectBackend(envLookup func(string) string) backendKind {
	if envLookup("WAYLAND_DISPLAY") != "" {
		return backendWayland
	}
	return backendX11
}

// Read captures the current clipboard image. It lists the available types,
// picks the best image MIME type, then reads the image bytes.
func (r *LinuxReader) Read(ctx context.Context) (*CaptureResult, error) {
	targets, err := r.listTargets(ctx)
	if err != nil {
		return nil, err
	}

	best := pickBestImageType(targets)
	if best == "" {
		return nil, fmt.Errorf("no image in clipboard")
	}

	raw, err := r.readImage(ctx, best)
	if err != nil {
		return nil, err
	}

	ext := extensionForMIME(best)
	return &CaptureResult{
		MIME:            best,
		Filename:        "clipboard" + ext,
		Extension:       ext,
		Bytes:           raw,
		OriginalTargets: targets,
	}, nil
}

// CheckPrerequisites verifies that the required host tools are installed.
func (r *LinuxReader) CheckPrerequisites(ctx context.Context) error {
	switch r.backend {
	case backendWayland:
		if err := checkTool(ctx, "wl-paste"); err != nil {
			return fmt.Errorf("clipboard tool not found: wl-paste is required for Wayland.\nInstall with: sudo apt install wl-clipboard")
		}
	case backendX11:
		if err := checkTool(ctx, "xclip"); err != nil {
			return fmt.Errorf("clipboard tool not found: xclip is required for X11.\nInstall with: sudo apt install xclip")
		}
	}

	if err := checkTool(ctx, "magick"); err != nil {
		return fmt.Errorf("magick (ImageMagick) is required for image format conversion.\nInstall with: sudo apt install imagemagick")
	}

	return nil
}

// Backend returns the detected display server backend.
func (r *LinuxReader) Backend() backendKind {
	return r.backend
}

// listTargets returns the list of clipboard types/targets available.
func (r *LinuxReader) listTargets(ctx context.Context) ([]string, error) {
	var cmd *exec.Cmd
	switch r.backend {
	case backendWayland:
		cmd = execCommand(ctx, "wl-paste", "--list-types")
	case backendX11:
		cmd = execCommand(ctx, "xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
	default:
		return nil, fmt.Errorf("unsupported backend: %s", r.backend)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("failed to list clipboard targets: %s", strings.TrimSpace(stderr.String()))
	}

	var targets []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			targets = append(targets, t)
		}
	}
	return targets, nil
}

// readImage reads the clipboard content for the given MIME type.
func (r *LinuxReader) readImage(ctx context.Context, mime string) ([]byte, error) {
	var cmd *exec.Cmd
	switch r.backend {
	case backendWayland:
		cmd = execCommand(ctx, "wl-paste", "--type", mime)
	case backendX11:
		cmd = execCommand(ctx, "xclip", "-selection", "clipboard", "-t", mime, "-o")
	default:
		return nil, fmt.Errorf("unsupported backend: %s", r.backend)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("failed to read clipboard image: %s", strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), nil
}

// pickBestImageType selects the best image MIME type from a list of targets.
// It prefers image/png, then any other image/* type.
func pickBestImageType(targets []string) string {
	var fallback string
	for _, t := range targets {
		if t == "image/png" {
			return "image/png"
		}
		if strings.HasPrefix(t, "image/") && fallback == "" {
			fallback = t
		}
	}
	return fallback
}

// extensionForMIME returns a file extension (with leading dot) for common
// image MIME types.
func extensionForMIME(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/webp":
		return ".webp"
	case "image/tiff":
		return ".tiff"
	case "image/svg+xml":
		return ".svg"
	default:
		// For unknown image types, strip the "image/" prefix.
		if strings.HasPrefix(mime, "image/") {
			return "." + strings.TrimPrefix(mime, "image/")
		}
		return ".bin"
	}
}

// checkTool verifies that a command-line tool is available on the PATH.
func checkTool(ctx context.Context, name string) error {
	cmd := execCommand(ctx, "which", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s not found", name)
	}
	return nil
}
