package vmguest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

func growRootDisk(ctx context.Context, log io.Writer) error {
	grow := exec.CommandContext(ctx, "/usr/bin/growpart", "/dev/vda", "1")
	output, err := grow.CombinedOutput()
	if len(output) > 0 {
		_, _ = log.Write(output)
	}
	if err := validateGrowpartResult(output, err); err != nil {
		return err
	}

	resize := exec.CommandContext(ctx, "/usr/sbin/resize2fs", "/dev/vda1")
	resize.Stdout = log
	resize.Stderr = log
	if err := resize.Run(); err != nil {
		return fmt.Errorf("grow VM root filesystem: %w", err)
	}
	return nil
}

func validateGrowpartResult(output []byte, commandErr error) error {
	if commandErr == nil || bytes.Contains(output, []byte("NOCHANGE:")) {
		return nil
	}
	return fmt.Errorf("grow VM root partition: %w", commandErr)
}
