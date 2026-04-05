package testdriver

import (
	"context"
	"testing"
	"time"
)

func TestClipboardSmoke(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := RunClipboardSmoke(ctx, driver); err != nil {
		t.Fatalf("RunClipboardSmoke: %v", err)
	}
}
