//go:build darwin

package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

func newHostWriter(envLookup func(string) string) Writer {
	return NewDarwinReader(envLookup)
}

// WriteText updates the macOS clipboard text using pbcopy.
func (r *DarwinReader) WriteText(ctx context.Context, text []byte) error {
	cmd := execCommand(ctx, "pbcopy")
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
		return fmt.Errorf("failed to write host clipboard text: %s", detail)
	}
	return nil
}
