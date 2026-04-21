package clipboard

import "context"

// Writer updates the host clipboard from the Cooper bridge.
type Writer interface {
	WriteText(ctx context.Context, text []byte) error
}

// NewHostWriter constructs the platform-specific host clipboard writer.
func NewHostWriter(envLookup func(string) string) Writer {
	return newHostWriter(envLookup)
}
