// Package workload defines the session policy shared by Docker barrels and
// virtual machines. Back ends can render this policy, but they must not add
// mounts or environment values on their own.
package workload

const (
	// EntrypointReadyPath is created after the shared agent entrypoint installs
	// all Cooper runtime support. Both back ends wait for it before user commands.
	EntrypointReadyPath = "/tmp/.cooper-entrypoint-ready"

	// VMImageContractLabel lets the host reject an old CLI image before it
	// starts a VM. An old entrypoint cannot create EntrypointReadyPath and would
	// otherwise fail after a misleading guest startup timeout.
	VMImageContractLabel   = "cooper.vm.agent-schema"
	VMImageContractVersion = "2"
)

// Access is the permitted access to a mounted path.
type Access string

const (
	ReadOnly  Access = "ro"
	ReadWrite Access = "rw"
)

// PathKind identifies the expected host path type.
type PathKind string

const (
	Directory PathKind = "directory"
	File      PathKind = "file"
)

// Ownership identifies who controls a mount source and its lifecycle.
type Ownership string

const (
	HostWorkspace Ownership = "host-workspace"
	HostState     Ownership = "host-state"
	ProfileState  Ownership = "profile-state"
	HostConfig    Ownership = "host-config"
	CooperCache   Ownership = "cooper-cache"
	CooperRuntime Ownership = "cooper-runtime"
)

// MountSpec is one validated source-to-target mount.
type MountSpec struct {
	ID        string
	Source    string
	Target    string
	Access    Access
	Kind      PathKind
	Ownership Ownership
}

// EnvVar keeps an environment name and value together without rendering it
// into a command argument too early.
type EnvVar struct {
	Name   string
	Value  string
	Secret bool
	Unset  bool
}
