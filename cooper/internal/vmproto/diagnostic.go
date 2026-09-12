package vmproto

import (
	"errors"
	"fmt"
)

const GuestDiagnosticSchema = 2

// GuestDiagnostic contains non-secret evidence from the guest host namespace.
type GuestDiagnostic struct {
	Schema             int      `json:"schema"`
	Depth              int      `json:"depth"`
	Interfaces         []string `json:"interfaces"`
	ExternalInterfaces []string `json:"external_interfaces"`
	DefaultRoutes      []string `json:"default_routes"`
	DockerVersion      string   `json:"docker_version"`
	KVMAvailable       bool     `json:"kvm_available"`
}

// Validate checks the no-NIC and managed-depth invariants.
func (d GuestDiagnostic) Validate() error {
	if d.Schema != GuestDiagnosticSchema {
		return fmt.Errorf("unsupported guest diagnostic schema %d", d.Schema)
	}
	if d.Depth < 1 || d.Depth > 2 {
		return fmt.Errorf("guest diagnostic depth %d is outside 1-2", d.Depth)
	}
	if len(d.Interfaces) == 0 {
		return errors.New("guest diagnostic has no network interfaces")
	}
	if len(d.ExternalInterfaces) != 0 {
		return fmt.Errorf("guest has external network interfaces: %v", d.ExternalInterfaces)
	}
	if len(d.DefaultRoutes) != 0 {
		return fmt.Errorf("guest has default network routes: %v", d.DefaultRoutes)
	}
	if d.DockerVersion == "" {
		return errors.New("guest Docker version is empty")
	}
	if d.Depth == 1 && !d.KVMAvailable {
		return errors.New("depth-1 guest does not have nested KVM")
	}
	if d.Depth == 2 && d.KVMAvailable {
		return errors.New("depth-2 guest exposes nested KVM")
	}
	return nil
}
