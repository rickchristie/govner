package vme2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/testdocker"
	"github.com/rickchristie/govner/cooper/internal/testdriver"
	"github.com/rickchristie/govner/cooper/internal/vm"
)

// TestVMHomePaths can also run inside a depth-one Cooper VM. It checks the
// account and complete selected state in both boundaries without requiring
// a third virtualization level or provider credentials.
func TestVMHomePaths(t *testing.T) {
	if os.Getenv("COOPER_RUN_VM_E2E") != "1" {
		t.Skip("set COOPER_RUN_VM_E2E=1 to run the Linux KVM gate")
	}
	preparedBase := requiredFile(t, "COOPER_VM_PREPARED_BASE")
	cooperBinary := requiredFile(t, "COOPER_VM_BINARY")
	lock, err := testdocker.SetupPackageNamed("vm-home-paths", true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	driver, err := testdriver.New(testdriver.Options{
		DisableHostClipboard: true,
		ConfigMutator: func(cfg *config.Config) {
			cfg.VM = config.VMConfig{CPUs: 2, MemoryMiB: 3072, DiskGiB: 16, MaxDepth: 2, StartTimeoutS: 600, StopTimeoutS: 15}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := driver.Close(); err != nil {
			t.Error(err)
		}
	}()
	docker.SetRuntimeNamespace(vmE2ENamespace)
	home := driver.HomeDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "COPILOT_HOME", "COPILOT_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "OPENCODE_DB"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GROK_HOME", filepath.Join(home, "grok-state"))
	if err := (vm.InfrastructureImages{CooperDir: driver.CooperDir(), Prefix: driver.ImagePrefix(), Executable: cooperBinary, Out: os.Stderr}).Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := driver.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	runBuiltInParityMatrix(t, driver, home, preparedBase, cooperBinary)
}
