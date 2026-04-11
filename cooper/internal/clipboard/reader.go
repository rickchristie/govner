package clipboard

import (
	"context"
	"fmt"
	"os/exec"
)

// Reader captures clipboard content from the host OS.
type Reader interface {
	Read(ctx context.Context) (*CaptureResult, error)
	CheckPrerequisites(ctx context.Context) error
}

// execCommand is a package-level variable to allow test mocking.
var execCommand = exec.CommandContext

// NewHostReader constructs the platform-specific host clipboard reader.
func NewHostReader(envLookup func(string) string) Reader {
	return newHostReader(envLookup)
}

// checkTool verifies that a command-line tool is available on the PATH.
func checkTool(ctx context.Context, name string) error {
	cmd := execCommand(ctx, "which", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s not found", name)
	}
	return nil
}
