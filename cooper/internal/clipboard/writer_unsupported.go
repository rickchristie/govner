//go:build !linux && !darwin

package clipboard

import (
	"context"
	"fmt"
	"runtime"
)

type unsupportedWriter struct{}

func newHostWriter(func(string) string) Writer {
	return unsupportedWriter{}
}

func (unsupportedWriter) WriteText(context.Context, []byte) error {
	return fmt.Errorf("clipboard text writes are not supported on %s", runtime.GOOS)
}
