//go:build aix || android || (darwin && !ios) || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(cmd *exec.Cmd) error {
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		err = syscall.Kill(-pgid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	if killErr := cmd.Process.Kill(); killErr != nil {
		if errors.Is(killErr, os.ErrProcessDone) {
			return nil
		}
		return killErr
	}
	return nil
}

// cleanupProcessGroupAfterExit terminates descendants that outlived the group
// leader. Setpgid creates a group whose ID is the leader PID, so the group can
// still be addressed after Cmd.Wait has reaped that leader.
func cleanupProcessGroupAfterExit(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
