//go:build darwin

package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DarwinReader reads image content from the macOS clipboard using osascript.
type DarwinReader struct {
	envLookup func(string) string
}

// NewDarwinReader creates a DarwinReader.
func NewDarwinReader(envLookup func(string) string) *DarwinReader {
	return &DarwinReader{envLookup: envLookup}
}

func newHostReader(envLookup func(string) string) Reader {
	return NewDarwinReader(envLookup)
}

// Read captures the current clipboard image by asking AppleScript for the
// available clipboard types, then exporting the best supported image type.
func (r *DarwinReader) Read(ctx context.Context) (*CaptureResult, error) {
	info, err := r.clipboardInfo(ctx)
	if err != nil {
		return nil, err
	}

	targets := darwinReadTargets(info)
	if len(targets) == 0 {
		return nil, fmt.Errorf("no image in clipboard")
	}

	originalTargets := darwinReportedMIMEs(info)
	if len(originalTargets) == 0 {
		originalTargets = darwinTargetMIMEs(targets)
	}
	var lastErr error
	for _, target := range targets {
		raw, err := r.readTarget(ctx, target)
		if err != nil {
			lastErr = err
			continue
		}
		return &CaptureResult{
			MIME:            target.MIME,
			Filename:        "clipboard" + target.Extension,
			Extension:       target.Extension,
			Bytes:           raw,
			OriginalTargets: originalTargets,
		}, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no image in clipboard")
}

// CheckPrerequisites verifies that the required host tool is available.
func (r *DarwinReader) CheckPrerequisites(ctx context.Context) error {
	if err := checkTool(ctx, "osascript"); err != nil {
		return fmt.Errorf("clipboard tool not found: osascript is required on macOS")
	}
	return nil
}

func (r *DarwinReader) clipboardInfo(ctx context.Context) (string, error) {
	cmd := execCommand(ctx, "osascript", "-e", "clipboard info")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed to inspect clipboard: %s", strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}

func (r *DarwinReader) readTarget(ctx context.Context, target darwinClipboardTarget) ([]byte, error) {
	tmpFile, err := os.CreateTemp("", "cooper-clipboard-*"+target.Extension)
	if err != nil {
		return nil, fmt.Errorf("create temp clipboard file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	quotedPath := strconv.Quote(tmpPath)
	args := []string{
		"-e", "try",
		"-e", fmt.Sprintf("set clipData to the clipboard as %s", target.AppleScriptType),
		"-e", fmt.Sprintf("set outFile to POSIX file %s", quotedPath),
		"-e", "set outRef to open for access outFile with write permission",
		"-e", "set eof outRef to 0",
		"-e", "write clipData to outRef",
		"-e", "close access outRef",
		"-e", "on error errMsg number errNum",
		"-e", "try",
		"-e", fmt.Sprintf("close access POSIX file %s", quotedPath),
		"-e", "end try",
		"-e", "error errMsg number errNum",
		"-e", "end try",
	}

	cmd := execCommand(ctx, "osascript", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("failed to read clipboard image: %s", detail)
	}

	raw, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("read exported clipboard image: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("failed to read clipboard image: exported clipboard file is empty")
	}
	return raw, nil
}
