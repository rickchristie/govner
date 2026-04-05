package testdriver

import (
	"fmt"
	"os"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/testdocker"
)

func TestMain(m *testing.M) {
	lock, err := testdocker.SetupPackage(true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testdriver docker bootstrap failed: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := docker.CleanupRuntime(); err != nil {
		fmt.Fprintf(os.Stderr, "testdriver docker runtime cleanup failed: %v\n", err)
		if code == 0 {
			code = 1
		}
	}

	if err := lock.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "testdriver docker lock release failed: %v\n", err)
		if code == 0 {
			code = 1
		}
	}

	os.Exit(code)
}
