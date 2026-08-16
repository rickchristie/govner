//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
)

// Windows has no Setpgid equivalent in syscall.SysProcAttr. Process.Kill is
// still preferable to making the entire Gowt binary Unix-only.
func configureProcessGroup(*exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) error {
	err := cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

// Windows descendants may retain inherited handles after their parent exits.
// The generic runner closes those handles after its bounded drain interval.
func cleanupProcessGroupAfterExit(*exec.Cmd) error { return nil }
