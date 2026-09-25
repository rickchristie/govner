package workload

import (
	"crypto/sha256"
	"fmt"
)

// DesktopHostname keeps Chromium's lock namespace stable across a restart.
// It differs from the host and other runtimes, so a live desktop elsewhere
// cannot be mistaken for a stale process in this workload's PID namespace.
func DesktopHostname(runtimeID string) string {
	digest := sha256.Sum256([]byte(runtimeID))
	return fmt.Sprintf("cooper-desktop-%x", digest[:12])
}
