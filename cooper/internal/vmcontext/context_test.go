package vmcontext

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContextRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.json")
	want := validContext()
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COOPER_VM_CONTEXT", path)
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if *got != want {
		t.Fatalf("context = %#v; want %#v", *got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o444 {
		t.Fatalf("context mode = %o", info.Mode().Perm())
	}
}

func TestContextRejectsUnsafeValues(t *testing.T) {
	t.Parallel()
	wrongSchema := validContext()
	wrongSchema.Schema++
	tooDeep := validContext()
	tooDeep.Depth = 3
	badNetwork := validContext()
	badNetwork.ParentNetwork = "bad network"
	publicProxy := validContext()
	publicProxy.ParentProxy = "8.8.8.8"
	wrongTemp := validContext()
	wrongTemp.TempDir = "/var/tmp"
	tests := []Context{wrongSchema, tooDeep, badNetwork, publicProxy, wrongTemp}
	for _, test := range tests {
		if err := test.Validate(); err == nil {
			t.Fatalf("expected rejection for %#v", test)
		}
	}
}

func TestContextRequiresFixedAgentContainer(t *testing.T) {
	t.Parallel()
	context := validContext()
	if err := context.Validate(); err != nil {
		t.Fatal(err)
	}
	context.AgentContainer = "../../host"
	if err := context.Validate(); err == nil {
		t.Fatal("Context.Validate accepted an unsafe agent container")
	}
}

func validContext() Context {
	return Context{
		Schema: Schema, Depth: 1,
		ParentNetwork: "cooper-control", ParentProxy: "172.30.0.1",
		AgentContainer: "cooper-vm-project-codex-agent",
		ProxyPort:      3128, BridgePort: 4343,
		WorkspaceDir: "/work/project", TempDir: "/tmp", CooperDir: "/home/user/.cooper",
	}
}
