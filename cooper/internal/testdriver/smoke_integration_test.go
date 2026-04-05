//go:build integration

package testdriver

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestClipboardSmoke(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	driver, err := New(Options{
		ImagePrefix:          DefaultImagePrefix,
		DisableHostClipboard: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if closeErr := driver.Close(); closeErr != nil {
			t.Fatalf("Close: %v", closeErr)
		}
	}()

	if err := driver.RequireProxyImage(); err != nil {
		t.Skip(err)
	}
	if err := driver.RequireBaseImage(); err != nil {
		t.Skip(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := RunClipboardSmoke(ctx, driver); err != nil {
		t.Fatalf("RunClipboardSmoke: %v", err)
	}
}
