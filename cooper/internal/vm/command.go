package vm

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// CommandRunner is the process boundary used by VM orchestration tests.
type CommandRunner interface {
	Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type systemRunner struct{}

func (systemRunner) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run %s: %w", name, err)
	}
	return nil
}

func (systemRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("run %s: %w: %s", name, err, output)
	}
	return output, nil
}

func runnerOrSystem(runner CommandRunner) CommandRunner {
	if runner != nil {
		return runner
	}
	return systemRunner{}
}

func currentExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find Cooper executable: %w", err)
	}
	return path, nil
}
