// Package runtimefs owns Cooper-managed files that are shared by CLI and VM
// workloads. It has no execution-back-end dependency.
package runtimefs

import "path/filepath"

const (
	SessionContainerDir   = "/run/cooper/session"
	TimezoneContainerPath = "/run/cooper/host-localtime"
	TimezoneFilename      = "cooper-localtime"
)

// TempRoot is the Cooper-managed host directory for workload /tmp mounts.
func TempRoot(cooperDir string) string {
	return filepath.Join(cooperDir, "tmp")
}

// SessionRoot stores host-controlled files for all workload sessions.
func SessionRoot(cooperDir string) string {
	return filepath.Join(cooperDir, "session")
}

// TempDir is the host directory mounted as /tmp for one workload.
func TempDir(cooperDir, runtimeID string) string {
	return filepath.Join(TempRoot(cooperDir), runtimeID)
}

// SessionDir is the host directory mounted read-only for one workload.
func SessionDir(cooperDir, runtimeID string) string {
	return filepath.Join(SessionRoot(cooperDir), runtimeID)
}
