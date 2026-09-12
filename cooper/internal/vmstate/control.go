// Package vmstate defines host runtime paths shared by VM lifecycle clients.
package vmstate

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// ControlDir returns a short host path for one VM control socket. Unix socket
// paths have a small limit, so hashes keep long Cooper paths and runtime IDs
// separate without putting either full value in the socket path.
func ControlDir(cooperDir, runtimeID string) string {
	runtimeDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(runtimeID)))[:16]
	return filepath.Join(ControlRoot(cooperDir), runtimeDigest)
}

// ControlRoot contains the short control directories for one Cooper config.
func ControlRoot(cooperDir string) string {
	rootDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(filepath.Clean(cooperDir))))[:12]
	return filepath.Join(os.TempDir(), fmt.Sprintf("cooper-vm-control-%d", os.Getuid()), rootDigest)
}

// ControlSocketPath returns the private host control endpoint for one VM.
func ControlSocketPath(cooperDir, runtimeID string) string {
	return filepath.Join(ControlDir(cooperDir, runtimeID), "control.sock")
}
