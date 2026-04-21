//go:build linux

package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
)

func newHostWriter(envLookup func(string) string) Writer {
	return NewLinuxReader(envLookup)
}

// WriteText updates the host clipboard text using the backend-appropriate tool.
// On Wayland, wl-copy is preferred and xclip is used as a best-effort fallback
// when XWayland clipboard access is available.
func (r *LinuxReader) WriteText(ctx context.Context, text []byte) error {
	type candidate struct {
		name string
		args []string
	}

	var candidates []candidate
	switch r.backend {
	case backendWayland:
		if err := checkTool(ctx, "wl-copy"); err == nil {
			candidates = append(candidates, candidate{name: "wl-copy"})
		}
		if err := checkTool(ctx, "xclip"); err == nil {
			candidates = append(candidates, candidate{name: "xclip", args: []string{"-selection", "clipboard"}})
		}
		if len(candidates) == 0 {
			return fmt.Errorf("clipboard tool not found: wl-copy or xclip is required to write clipboard text on Linux")
		}
	case backendX11:
		candidates = append(candidates, candidate{name: "xclip", args: []string{"-selection", "clipboard"}})
	default:
		return fmt.Errorf("unsupported backend: %s", r.backend)
	}

	var failures []string
	for _, candidate := range candidates {
		var err error
		switch candidate.name {
		case "xclip":
			err = writeTextWithXclipAsync(ctx, text)
		default:
			err = writeTextWithCommand(ctx, candidate.name, candidate.args, text)
		}
		if err == nil {
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		} else {
			failures = append(failures, fmt.Sprintf("%s: %s", candidate.name, err.Error()))
		}
	}

	return fmt.Errorf("failed to write host clipboard text: %s", strings.Join(failures, "; "))
}

func writeTextWithCommand(ctx context.Context, name string, args []string, text []byte) error {
	cmd := execCommand(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("%s", detail)
	}
	return nil
}

func writeTextWithXclipAsync(ctx context.Context, text []byte) error {
	tmpFile, err := os.CreateTemp("", "cooper-host-clipboard-*")
	if err != nil {
		return fmt.Errorf("create temp clipboard file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(text); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp clipboard file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp clipboard file: %w", err)
	}
	defer os.Remove(tmpPath)

	cmd := execCommand(ctx, "sh", "-lc", `xclip -selection clipboard < "$1" >/dev/null 2>&1 &`, "sh", tmpPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("%s", detail)
	}
	return nil
}
