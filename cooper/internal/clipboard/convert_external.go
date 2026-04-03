package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// externalConvertTimeout is the maximum time allowed for an external
// conversion process. Exported via a variable so tests can override.
var externalConvertTimeout = 30 * time.Second

// convertExternalToPNG pipes the input data through ImageMagick's
// `magick` CLI to produce PNG output. It uses stdin/stdout to avoid
// temporary files:
//
//	magick - png:-
//
// This is the fallback path for formats we cannot decode in-process
// (SVG, AVIF, HEIC, ICO, etc.).
func convertExternalToPNG(data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), externalConvertTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "magick", "-", "png:-")
	cmd.Stdin = bytes.NewReader(data)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("external conversion timed out after %s", externalConvertTimeout)
		}
		detail := stderr.String()
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("external conversion failed: %s", detail)
	}

	return stdout.Bytes(), nil
}

// CheckConversionPrerequisites verifies that the `magick` binary
// (ImageMagick 7+) is available on PATH. Returns nil when ready, or
// a descriptive error with an install hint.
func CheckConversionPrerequisites() error {
	path, err := exec.LookPath("magick")
	if err != nil {
		return fmt.Errorf(
			"ImageMagick 7 not found on PATH (needed for SVG, AVIF, HEIC, and other uncommon image formats). " +
				"Install it with:\n" +
				"  Ubuntu/Debian:  sudo apt install imagemagick\n" +
				"  macOS (brew):   brew install imagemagick\n" +
				"  Arch:           sudo pacman -S imagemagick\n" +
				"  Fedora:         sudo dnf install ImageMagick",
		)
	}
	_ = path
	return nil
}
