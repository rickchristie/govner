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

// imageMagickBinary returns the ImageMagick CLI binary name.
// ImageMagick 7 provides "magick", ImageMagick 6 provides "convert".
func imageMagickBinary() string {
	if _, err := exec.LookPath("magick"); err == nil {
		return "magick"
	}
	return "convert"
}

// convertExternalToPNG pipes the input data through ImageMagick's
// CLI to produce PNG output. It uses stdin/stdout to avoid temporary files.
// Supports both ImageMagick 7 ("magick") and ImageMagick 6 ("convert").
//
// This is the fallback path for formats we cannot decode in-process
// (SVG, AVIF, HEIC, ICO, etc.).
func convertExternalToPNG(data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), externalConvertTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, imageMagickBinary(), "-", "png:-")
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

// CheckConversionPrerequisites verifies that ImageMagick (either "magick"
// from v7+ or "convert" from v6) is available on PATH. Returns nil when
// ready, or a descriptive error with an install hint.
func CheckConversionPrerequisites() error {
	if _, err := exec.LookPath("magick"); err == nil {
		return nil
	}
	if _, err := exec.LookPath("convert"); err == nil {
		return nil
	}
	return fmt.Errorf(
		"ImageMagick not found on PATH (needed for SVG, AVIF, HEIC, and other uncommon image formats). " +
			"Install it with:\n" +
			"  Ubuntu/Debian:  sudo apt install imagemagick\n" +
			"  macOS (brew):   brew install imagemagick\n" +
			"  Arch:           sudo pacman -S imagemagick\n" +
			"  Fedora:         sudo dnf install ImageMagick",
	)
}
