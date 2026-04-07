package docker

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// stopTimeoutSeconds controls how long Docker waits after sending SIGTERM
// before force-killing a container. A negative value preserves Docker's
// default grace period. Tests override this to fail fast if shutdown regresses.
var stopTimeoutSeconds = -1

// SetStopTimeoutSeconds overrides the container stop grace period used by
// StopProxy and StopBarrel. Pass a negative value to restore Docker's default.
func SetStopTimeoutSeconds(seconds int) {
	stopTimeoutSeconds = seconds
}

func stopAndRemoveContainer(name string) error {
	cmd := exec.Command("docker", dockerStopArgs(name, stopTimeoutSeconds)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "No such container") &&
			!strings.Contains(string(output), "is not running") {
			return fmt.Errorf("docker stop %s failed: %w\n%s", name, err, string(output))
		}
	}

	cmd = exec.Command("docker", "rm", "-f", name)
	output, err = cmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "No such container") {
			return fmt.Errorf("docker rm %s failed: %w\n%s", name, err, string(output))
		}
	}

	return nil
}

func dockerStopArgs(name string, timeoutSeconds int) []string {
	args := []string{"stop"}
	if timeoutSeconds >= 0 {
		args = append(args, "-t", strconv.Itoa(timeoutSeconds))
	}
	args = append(args, name)
	return args
}
