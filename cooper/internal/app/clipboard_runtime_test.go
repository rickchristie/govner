package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
)

func newRuntimeTestApp(t *testing.T) (*CooperApp, string) {
	t.Helper()
	cooperDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfgPath := filepath.Join(cooperDir, "config.json")
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return NewCooperApp(cfg, cooperDir), cooperDir
}

func TestCooperAppUpdateSettingsPersistsClipboardPolicy(t *testing.T) {
	app, cooperDir := newRuntimeTestApp(t)

	const (
		timeoutSecs       = 9
		blockedLimit      = 150
		allowedLimit      = 175
		bridgeLogLimit    = 80
		clipboardTTLSecs  = 47
		clipboardMaxBytes = 256
	)

	if err := app.UpdateSettings(
		timeoutSecs,
		blockedLimit,
		allowedLimit,
		bridgeLogLimit,
		clipboardTTLSecs,
		clipboardMaxBytes,
	); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	if got := app.Config(); got.ClipboardTTLSecs != clipboardTTLSecs || got.ClipboardMaxBytes != clipboardMaxBytes {
		t.Fatalf("config clipboard policy = (%d, %d), want (%d, %d)", got.ClipboardTTLSecs, got.ClipboardMaxBytes, clipboardTTLSecs, clipboardMaxBytes)
	}

	persisted, err := config.LoadConfig(filepath.Join(cooperDir, "config.json"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if persisted.ClipboardTTLSecs != clipboardTTLSecs || persisted.ClipboardMaxBytes != clipboardMaxBytes {
		t.Fatalf("persisted clipboard policy = (%d, %d), want (%d, %d)", persisted.ClipboardTTLSecs, persisted.ClipboardMaxBytes, clipboardTTLSecs, clipboardMaxBytes)
	}

	snap, err := app.ClipboardManager().Stage(clipboard.ClipboardObject{
		Kind: clipboard.ClipboardKindText,
		Raw:  bytes.Repeat([]byte("x"), 32),
	}, 0)
	if err != nil {
		t.Fatalf("Stage small object: %v", err)
	}
	expectedExpiry := snap.CreatedAt.Add(clipboardTTLSecs * time.Second)
	if snap.ExpiresAt.Sub(expectedExpiry).Abs() > time.Millisecond {
		t.Fatalf("snapshot expiry = %v, want ~%v", snap.ExpiresAt, expectedExpiry)
	}

	if _, err := app.ClipboardManager().Stage(clipboard.ClipboardObject{
		Kind: clipboard.ClipboardKindText,
		Raw:  bytes.Repeat([]byte("x"), clipboardMaxBytes+1),
	}, 0); err == nil {
		t.Fatal("expected updated clipboard max bytes to reject oversized stage")
	}
}

func TestCooperAppClipboardTokenLifecycleHelpers(t *testing.T) {
	app, cooperDir := newRuntimeTestApp(t)
	containerName := "barrel-token-test"
	tokenPath := clipboard.TokenFilePath(cooperDir, containerName)

	if _, err := clipboard.WriteTokenFile(cooperDir, containerName, "old-token"); err != nil {
		t.Fatalf("WriteTokenFile old-token: %v", err)
	}
	if err := app.ClipboardManager().RegisterBarrel(clipboard.BarrelSession{
		ContainerName: containerName,
		ToolName:      "claude",
		ClipboardMode: "shim",
		Eligible:      true,
	}); err != nil {
		t.Fatalf("RegisterBarrel: %v", err)
	}

	if err := app.rotateClipboardToken(containerName); err != nil {
		t.Fatalf("rotateClipboardToken: %v", err)
	}

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("ReadFile rotated token: %v", err)
	}
	newToken := strings.TrimSpace(string(data))
	if newToken == "" || newToken == "old-token" {
		t.Fatalf("rotated token = %q, want a new non-empty token", newToken)
	}
	if got := len(app.ClipboardManager().ActiveSessions()); got != 0 {
		t.Fatalf("expected rotate to unregister cached sessions, got %d", got)
	}

	if err := app.ClipboardManager().RegisterBarrel(clipboard.BarrelSession{
		ContainerName: containerName,
		ToolName:      "claude",
		ClipboardMode: "shim",
		Eligible:      true,
	}); err != nil {
		t.Fatalf("RegisterBarrel second time: %v", err)
	}

	app.revokeClipboardToken(containerName)

	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Fatalf("token file should be removed, stat err = %v", err)
	}
	if got := len(app.ClipboardManager().ActiveSessions()); got != 0 {
		t.Fatalf("expected revoke to unregister cached sessions, got %d", got)
	}
}
