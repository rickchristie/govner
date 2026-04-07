package app

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/testdocker"
)

func TestMain(m *testing.M) {
	logTestMain("starting package bootstrap")
	lock, err := testdocker.SetupPackageNamed("internal/app", true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "app docker bootstrap failed: %v\n", err)
		os.Exit(1)
	}
	logTestMain("starting test execution")

	code := m.Run()

	logTestMain("cleaning package runtime resources")
	if err := docker.CleanupRuntime(); err != nil {
		fmt.Fprintf(os.Stderr, "app docker runtime cleanup failed: %v\n", err)
		if code == 0 {
			code = 1
		}
	}

	logTestMain("releasing shared docker test lock")
	if err := lock.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "app docker lock release failed: %v\n", err)
		if code == 0 {
			code = 1
		}
	}

	os.Exit(code)
}

func logTestMain(msg string) {
	fmt.Fprintf(os.Stderr, "[cooper test bootstrap][internal/app][%s] %s\n", time.Now().Format("15:04:05"), msg)
}
