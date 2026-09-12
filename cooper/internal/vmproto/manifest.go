package vmproto

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strings"
)

const ManifestSchema = 1

const maxGuestMounts = 24

var runtimeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var environmentName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
var sharedMemorySize = regexp.MustCompile(`(?i)^[1-9][0-9]*[kmg]?$`)

// GuestMount maps one opaque virtiofs entry to its path in the guest and the
// selected agent container.
type GuestMount struct {
	ID       string `json:"id"`
	Tag      string `json:"tag"`
	Entry    string `json:"entry,omitempty"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
	Kind     string `json:"kind"`
}

// PortForward is one allowed guest relay port.
type PortForward struct {
	Port int `json:"port"`
}

// Manifest is the host-authorized startup contract for one VM.
type Manifest struct {
	Schema            int           `json:"schema"`
	Nonce             string        `json:"nonce"`
	RuntimeID         string        `json:"runtime_id"`
	ToolName          string        `json:"tool_name"`
	AgentContainer    string        `json:"agent_container"`
	ImageRef          string        `json:"image_ref"`
	ImageID           string        `json:"image_id"`
	ImageArchive      string        `json:"image_archive"`
	SeccompProfile    string        `json:"seccomp_profile"`
	WorkspaceDir      string        `json:"workspace_dir"`
	CooperDir         string        `json:"cooper_dir"`
	ProxyPort         int           `json:"proxy_port"`
	BridgePort        int           `json:"bridge_port"`
	ForwardPorts      []PortForward `json:"forward_ports"`
	ControlSubnet     string        `json:"control_subnet"`
	ControlGateway    string        `json:"control_gateway"`
	DefaultBridgeCIDR string        `json:"default_bridge_cidr"`
	UID               int           `json:"uid"`
	GID               int           `json:"gid"`
	Depth             int           `json:"depth"`
	SHMSize           string        `json:"shm_size"`
	Mounts            []GuestMount  `json:"mounts"`
	Environment       []string      `json:"environment"`
	CADigest          string        `json:"ca_digest"`
}

// Validate rejects malformed or unsafe host input before guest mounts start.
func (m Manifest) Validate() error {
	if m.Schema != ManifestSchema {
		return fmt.Errorf("unsupported manifest schema %d", m.Schema)
	}
	for name, value := range map[string]string{
		"nonce": m.Nonce, "runtime ID": m.RuntimeID, "tool": m.ToolName,
		"agent container": m.AgentContainer, "image reference": m.ImageRef,
		"image ID": m.ImageID, "image archive": m.ImageArchive,
		"seccomp profile": m.SeccompProfile,
		"workspace":       m.WorkspaceDir, "Cooper directory": m.CooperDir,
		"shared memory size": m.SHMSize, "CA digest": m.CADigest,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("manifest %s is required", name)
		}
	}
	if !runtimeName.MatchString(m.RuntimeID) || !runtimeName.MatchString(m.AgentContainer) || !runtimeName.MatchString(m.ToolName) {
		return errors.New("manifest runtime, agent container, or tool name is invalid")
	}
	if !validHexDigest(m.Nonce) {
		return errors.New("manifest nonce is not a 256-bit hexadecimal value")
	}
	if !strings.HasPrefix(m.ImageID, "sha256:") || !validHexDigest(strings.TrimPrefix(m.ImageID, "sha256:")) {
		return errors.New("manifest image ID is invalid")
	}
	if m.ImageArchive != "/run/cooper/host/image/agent.tar" || m.SeccompProfile != "/run/cooper/host/control/seccomp.json" {
		return errors.New("manifest image or seccomp path does not match the fixed guest path")
	}
	if m.CooperDir != "/home/user/.cooper" || !filepath.IsAbs(m.WorkspaceDir) || filepath.Clean(m.WorkspaceDir) != m.WorkspaceDir {
		return errors.New("manifest workspace or Cooper path is invalid")
	}
	if !validHexDigest(m.CADigest) {
		return errors.New("manifest CA digest is not a SHA-256 value")
	}
	if m.Depth < 1 || m.Depth > 2 {
		return fmt.Errorf("manifest depth %d is outside 1-2", m.Depth)
	}
	if m.UID <= 0 || m.GID <= 0 {
		return errors.New("manifest UID and GID must be unprivileged")
	}
	if !sharedMemorySize.MatchString(m.SHMSize) {
		return fmt.Errorf("manifest shared memory size %q is invalid", m.SHMSize)
	}
	for name, port := range map[string]int{"proxy": m.ProxyPort, "bridge": m.BridgePort} {
		if port < 1 || port > 65535 {
			return fmt.Errorf("manifest %s port %d is invalid", name, port)
		}
	}
	controlIP, controlNetwork, err := net.ParseCIDR(m.ControlSubnet)
	if err != nil {
		return fmt.Errorf("parse control subnet: %w", err)
	}
	if controlIP.To4() == nil || !controlIP.IsPrivate() || !controlIP.Equal(controlNetwork.IP) {
		return fmt.Errorf("control subnet %q must be a canonical private IPv4 network", m.ControlSubnet)
	}
	gateway := net.ParseIP(m.ControlGateway)
	if gateway == nil || gateway.To4() == nil || !gateway.IsPrivate() || !controlNetwork.Contains(gateway) {
		return fmt.Errorf("invalid control gateway %q", m.ControlGateway)
	}
	bridgeIP, bridgeNetwork, err := net.ParseCIDR(m.DefaultBridgeCIDR)
	if err != nil {
		return fmt.Errorf("parse default bridge CIDR: %w", err)
	}
	if bridgeIP.To4() == nil || !bridgeIP.IsPrivate() || bridgeIP.Equal(bridgeNetwork.IP) {
		return fmt.Errorf("default bridge CIDR %q must contain a private IPv4 host address", m.DefaultBridgeCIDR)
	}
	if controlNetwork.Contains(bridgeIP) || bridgeNetwork.Contains(controlIP) {
		return errors.New("manifest Docker bridge overlaps the control network")
	}
	if len(m.Mounts) == 0 || len(m.Mounts) > maxGuestMounts {
		return fmt.Errorf("manifest has %d mounts; allowed range is 1-%d", len(m.Mounts), maxGuestMounts)
	}
	seenIDs := make(map[string]bool, len(m.Mounts))
	seenTags := make(map[string]bool, len(m.Mounts))
	seenTargets := make(map[string]bool, len(m.Mounts))
	workspaceFound := false
	for _, mount := range m.Mounts {
		if !runtimeName.MatchString(mount.ID) || mount.Tag == "" || strings.ContainsAny(mount.Tag, "/ ,=") ||
			!filepath.IsAbs(mount.Target) || filepath.Clean(mount.Target) != mount.Target || mount.Target == "/" {
			return fmt.Errorf("manifest mount %q has invalid paths", mount.ID)
		}
		if mount.Kind == "directory" && mount.Entry != "" {
			return fmt.Errorf("manifest directory mount %q has an unexpected entry", mount.ID)
		}
		if mount.Kind == "file" && (mount.Entry == "" || filepath.Base(mount.Entry) != mount.Entry || mount.Entry == "." || mount.Entry == "..") {
			return fmt.Errorf("manifest file mount %q has an invalid entry", mount.ID)
		}
		if seenIDs[mount.ID] || seenTags[mount.Tag] || seenTargets[filepath.Clean(mount.Target)] {
			return fmt.Errorf("manifest mount %q is duplicated", mount.ID)
		}
		if mount.Kind != "file" && mount.Kind != "directory" {
			return fmt.Errorf("manifest mount %q has invalid kind %q", mount.ID, mount.Kind)
		}
		seenIDs[mount.ID] = true
		seenTags[mount.Tag] = true
		seenTargets[filepath.Clean(mount.Target)] = true
		if mount.ID == "workspace" {
			if mount.Target != m.WorkspaceDir || mount.Kind != "directory" || mount.ReadOnly {
				return errors.New("manifest workspace mount does not match the workspace")
			}
			workspaceFound = true
		}
	}
	if !workspaceFound {
		return errors.New("manifest workspace mount is required")
	}
	if len(m.ForwardPorts) > MaxForwardPorts {
		return fmt.Errorf("manifest has %d forward ports; maximum is %d", len(m.ForwardPorts), MaxForwardPorts)
	}
	seenForwards := make(map[int]bool, len(m.ForwardPorts))
	for _, forward := range m.ForwardPorts {
		if forward.Port < 1 || forward.Port > 65535 {
			return fmt.Errorf("manifest forward port %d is invalid", forward.Port)
		}
		if forward.Port == m.ProxyPort || forward.Port == m.BridgePort {
			return fmt.Errorf("manifest forward port %d is reserved", forward.Port)
		}
		if seenForwards[forward.Port] {
			return fmt.Errorf("manifest forward port %d is duplicated", forward.Port)
		}
		seenForwards[forward.Port] = true
	}
	if len(m.Environment) > 256 {
		return errors.New("manifest environment has too many values")
	}
	seenEnvironment := make(map[string]bool, len(m.Environment))
	for _, value := range m.Environment {
		name, _, ok := strings.Cut(value, "=")
		if !ok || !environmentName.MatchString(name) || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("manifest environment contains an invalid value")
		}
		if seenEnvironment[name] {
			return fmt.Errorf("manifest environment contains duplicate %s", name)
		}
		seenEnvironment[name] = true
	}
	return nil
}

func validHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
