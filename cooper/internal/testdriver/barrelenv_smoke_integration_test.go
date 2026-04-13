package testdriver

import (
	"context"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestBarrelEnvSmoke(t *testing.T) {
	driver, err := New(Options{
		ImagePrefix:          DefaultImagePrefix,
		DisableHostClipboard: true,
		ConfigMutator: func(cfg *config.Config) {
			cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "SMOKE_ALPHA", Value: "one"}, {Name: "SMOKE_EMPTY", Value: ""}}
		},
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer func() {
		if closeErr := driver.Close(); closeErr != nil {
			t.Fatalf("Close() failed: %v", closeErr)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := RunBarrelEnvSmoke(ctx, driver); err != nil {
		t.Fatalf("RunBarrelEnvSmoke() failed: %v", err)
	}
}
