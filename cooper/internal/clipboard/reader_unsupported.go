//go:build !linux && !darwin

package clipboard

import (
	"context"
	"fmt"
	"runtime"
)

type unsupportedReader struct{}

func newHostReader(func(string) string) Reader {
	return unsupportedReader{}
}

func (unsupportedReader) Read(context.Context) (*CaptureResult, error) {
	return nil, fmt.Errorf("clipboard capture is not supported on %s", runtime.GOOS)
}

func (unsupportedReader) CheckPrerequisites(context.Context) error {
	return fmt.Errorf("clipboard capture is not supported on %s", runtime.GOOS)
}
