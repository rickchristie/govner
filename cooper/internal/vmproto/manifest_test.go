package vmproto

import (
	"strings"
	"testing"
)

func TestManifestValidateAcceptsCompleteContract(t *testing.T) {
	t.Parallel()
	if err := validManifest().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestManifestValidateRejectsUnsafeContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*Manifest)
	}{
		{name: "root user", change: func(manifest *Manifest) { manifest.UID = 0 }},
		{name: "bad nonce", change: func(manifest *Manifest) { manifest.Nonce = strings.Repeat("z", 64) }},
		{name: "unsafe runtime name", change: func(manifest *Manifest) { manifest.RuntimeID = "../outer" }},
		{name: "invalid image ID", change: func(manifest *Manifest) { manifest.ImageID = "latest" }},
		{name: "invalid image digest", change: func(manifest *Manifest) { manifest.ImageID = "sha256:" + strings.Repeat("z", 64) }},
		{name: "invalid shared memory size", change: func(manifest *Manifest) { manifest.SHMSize = "1g,invalid" }},
		{name: "unexpected image path", change: func(manifest *Manifest) { manifest.ImageArchive = "/tmp/agent.tar" }},
		{name: "public control network", change: func(manifest *Manifest) { manifest.ControlSubnet = "8.8.8.0/24" }},
		{name: "noncanonical control network", change: func(manifest *Manifest) { manifest.ControlSubnet = "172.30.0.2/24" }},
		{name: "gateway outside subnet", change: func(manifest *Manifest) { manifest.ControlGateway = "172.31.0.1" }},
		{name: "bridge network address", change: func(manifest *Manifest) { manifest.DefaultBridgeCIDR = "172.29.0.0/24" }},
		{name: "overlapping Docker bridge", change: func(manifest *Manifest) { manifest.DefaultBridgeCIDR = "172.30.0.2/24" }},
		{name: "missing mounts", change: func(manifest *Manifest) { manifest.Mounts = nil }},
		{name: "unclean target", change: func(manifest *Manifest) { manifest.Mounts[0].Target = "/work/../project" }},
		{name: "writable mismatch", change: func(manifest *Manifest) { manifest.Mounts[0].ReadOnly = true }},
		{name: "missing workspace", change: func(manifest *Manifest) { manifest.Mounts[0].ID = "other" }},
		{name: "invalid tag", change: func(manifest *Manifest) { manifest.Mounts[0].Tag = "tag,cache=always" }},
		{name: "parent file entry", change: func(manifest *Manifest) {
			manifest.Mounts = append(manifest.Mounts, GuestMount{ID: "settings", Tag: "cooper-m-001", Entry: "..", Target: "/home/user/.settings", Kind: "file"})
		}},
		{name: "bad CA digest", change: func(manifest *Manifest) { manifest.CADigest = "1234" }},
		{name: "invalid environment", change: func(manifest *Manifest) { manifest.Environment = []string{"=missing-name"} }},
		{name: "invalid environment name", change: func(manifest *Manifest) { manifest.Environment = []string{"BAD NAME=value"} }},
		{name: "duplicate environment", change: func(manifest *Manifest) { manifest.Environment = []string{"NAME=one", "NAME=two"} }},
		{name: "reserved forward", change: func(manifest *Manifest) { manifest.ForwardPorts = []PortForward{{Port: 3128}} }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manifest := validManifest()
			test.change(&manifest)
			if err := manifest.Validate(); err == nil {
				t.Fatalf("Manifest.Validate accepted %s", test.name)
			}
		})
	}
}

func TestManifestValidateRejectsBoundOverflows(t *testing.T) {
	t.Parallel()
	manifest := validManifest()
	manifest.Mounts = make([]GuestMount, maxGuestMounts+1)
	if err := manifest.Validate(); err == nil {
		t.Fatal("Manifest.Validate accepted too many mounts")
	}
	manifest = validManifest()
	manifest.Environment = make([]string, 257)
	for index := range manifest.Environment {
		manifest.Environment[index] = "NAME=value"
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("Manifest.Validate accepted too many environment values")
	}
}

func validManifest() Manifest {
	return Manifest{
		Schema:            ManifestSchema,
		Nonce:             strings.Repeat("a", 64),
		RuntimeID:         "cooper-vm-project-codex",
		ToolName:          "codex",
		AgentContainer:    "cooper-vm-project-codex-agent",
		ImageRef:          "cooper-cli-codex",
		ImageID:           "sha256:" + strings.Repeat("b", 64),
		ImageArchive:      "/run/cooper/host/image/agent.tar",
		SeccompProfile:    "/run/cooper/host/control/seccomp.json",
		WorkspaceDir:      "/work/project",
		CooperDir:         "/home/user/.cooper",
		HomeDir:           "/home/user",
		ProxyPort:         3128,
		BridgePort:        4343,
		ControlSubnet:     "172.30.0.0/24",
		ControlGateway:    "172.30.0.1",
		DefaultBridgeCIDR: "172.29.0.1/24",
		UID:               1000,
		GID:               1000,
		Depth:             1,
		SHMSize:           "1g",
		Mounts: []GuestMount{{
			ID: "workspace", Tag: "cooper-m-000", Target: "/work/project", Kind: "directory",
		}},
		Environment: []string{"HTTP_PROXY=http://172.30.0.1:3128"},
		CADigest:    strings.Repeat("c", 64),
	}
}
