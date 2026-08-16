//go:build plan9 || js || wasip1 || ios

package main

import (
	"errors"
	"os"
	"os/exec"
)

func configureProcessGroup(*exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) error {
	err := cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

// Platforms in this file have no portable process-group primitive. The
// generic runner closes inherited descriptors after its bounded drain period.
func cleanupProcessGroupAfterExit(*exec.Cmd) error { return nil }
