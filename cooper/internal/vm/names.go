package vm

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/vmstate"
)

const dockerNameLimit = 128

var unsafeNameCharacter = regexp.MustCompile(`[^a-z0-9_.-]+`)

// RuntimeID returns a stable VM identity. It always includes a path hash, so
// two workspaces with the same base name cannot share a VM by accident.
func RuntimeID(namespace, workspace, tool string) (string, error) {
	canonical, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve VM workspace: %w", err)
	}
	canonical = filepath.Clean(canonical)
	namespace = cleanNamePart(namespace)
	tool = cleanNamePart(tool)
	base := cleanNamePart(filepath.Base(canonical))
	if namespace == "" || tool == "" || base == "" {
		return "", fmt.Errorf("VM namespace, workspace, and tool must have a usable name")
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(namespace+"\x00"+canonical+"\x00"+tool)))[:12]
	suffix := "-vm-" + tool + "-" + hash
	maximumBase := dockerNameLimit - len(namespace) - len(suffix) - 1
	if maximumBase < 1 {
		return "", fmt.Errorf("VM namespace %q is too long", namespace)
	}
	if len(base) > maximumBase {
		base = strings.Trim(base[:maximumBase], "_.-")
	}
	return namespace + "-vm-" + base + "-" + tool + "-" + hash, nil
}

func cleanNamePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = unsafeNameCharacter.ReplaceAllString(value, "-")
	return strings.Trim(value, "_.-")
}

// RelayContainerName returns the network-side helper name for a VM.
func RelayContainerName(runtimeID string) string { return runtimeID + "-relay" }

// RelayNetworkName returns the private relay network for a VM.
func RelayNetworkName(runtimeID string) string { return runtimeID + "-relay" }

// AgentContainerName returns the selected agent container inside the guest.
func AgentContainerName(runtimeID string) string { return runtimeID + "-agent" }

// RuntimeDir returns the transient host directory for one VM.
func RuntimeDir(cooperDir, runtimeID string) string {
	return filepath.Join(cooperDir, "vm", "run", runtimeID)
}

// SupervisorRuntimeDir contains only the disposable disk, sockets, and logs
// that the networkless supervisor can change. Host restart metadata and relay
// policy stay in the parent directory, which does not enter the supervisor.
func SupervisorRuntimeDir(runtimeDir string) string {
	return filepath.Join(runtimeDir, "supervisor")
}

// RuntimeDiskPath returns the disposable VM overlay path.
func RuntimeDiskPath(cooperDir, runtimeID string) string {
	return filepath.Join(SupervisorRuntimeDir(RuntimeDir(cooperDir, runtimeID)), "disk.qcow2")
}

// ControlDir returns a short host path for a supervisor control socket. Unix
// socket paths are limited to about 108 bytes on Linux, while a Cooper
// directory and Docker runtime ID can both be long. The digest keeps separate
// Cooper directories and runtimes isolated without using a guessed name.
func ControlDir(cooperDir, runtimeID string) string {
	return vmstate.ControlDir(cooperDir, runtimeID)
}

func controlRoot(cooperDir string) string {
	return vmstate.ControlRoot(cooperDir)
}

// ControlSocketPath returns the host endpoint for VM control operations.
func ControlSocketPath(cooperDir, runtimeID string) string {
	return vmstate.ControlSocketPath(cooperDir, runtimeID)
}

// ImageCacheDir returns the persistent exact-image archive cache.
func ImageCacheDir(cooperDir string) string {
	return filepath.Join(cooperDir, "vm", "images")
}

// SupervisorImageName returns the infrastructure image for this schema.
func SupervisorImageName(prefix string) string {
	return prefix + "cooper-vm-supervisor:" + infrastructureImageSchema
}

// RelayImageName returns the infrastructure image for this schema.
func RelayImageName(prefix string) string {
	return prefix + "cooper-vm-relay:" + infrastructureImageSchema
}
