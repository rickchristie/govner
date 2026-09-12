package templates

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/vmcontext"
)

func TestNestedProxyTemplatesAreParentOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.json")
	context := vmcontext.Context{
		Schema: vmcontext.Schema, Depth: 1,
		ParentNetwork: "cooper-control", ParentProxy: "172.30.0.1",
		AgentContainer: "cooper-outer-agent",
		ProxyPort:      3128, BridgePort: 4343,
		WorkspaceDir: "/work/project", TempDir: "/tmp", CooperDir: "/home/user/.cooper",
	}
	if err := vmcontext.Write(path, context); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COOPER_VM_CONTEXT", path)

	squid, err := RenderSquidConf(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	dockerfile, err := RenderProxyDockerfile(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dockerfile, "COPY parent-cooper-ca.pem") || !strings.Contains(dockerfile, "update-ca-certificates") {
		t.Fatalf("nested proxy image does not trust the outer Cooper CA:\n%s", dockerfile)
	}
	for _, want := range []string{
		"cache_peer 172.30.0.1 parent 3128 0 no-query default",
		"never_direct allow all",
	} {
		if !strings.Contains(squid, want) {
			t.Fatalf("nested Squid config does not contain %q", want)
		}
	}

	entrypoint, err := RenderProxyEntrypoint(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entrypoint, "cooper-outer-agent") || strings.Contains(entrypoint, "host.docker.internal") {
		t.Fatalf("nested proxy entrypoint has an unsafe relay target:\n%s", entrypoint)
	}
}
