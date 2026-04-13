package testdriver

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/testdocker"
)

// RunClipboardSmoke starts the real Cooper runtime and verifies the clipboard
// bridge, persisted clipboard settings, token rotation/revocation, and custom
// `COOPER_CLIPBOARD_MODE=off` behavior end-to-end.
func RunClipboardSmoke(ctx context.Context, d *Driver) error {
	if err := d.RequireProxyImage(); err != nil {
		return err
	}
	if err := d.RequireBaseImage(); err != nil {
		return err
	}
	if err := d.Start(ctx); err != nil {
		return fmt.Errorf("start cooper runtime: %w", err)
	}

	if err := verifyStageFetchAndSettings(d); err != nil {
		return err
	}
	if err := verifyTokenRotationAndRevocation(d); err != nil {
		return err
	}
	if err := verifyCustomClipboardModeOff(d); err != nil {
		return err
	}
	return nil
}

func verifyStageFetchAndSettings(d *Driver) error {
	token, err := d.RegisterBarrelSession(clipboard.BarrelSession{
		ContainerName: "driver-stage",
		ToolName:      "claude",
		ClipboardMode: "shim",
		Eligible:      true,
	})
	if err != nil {
		return err
	}

	cfg := d.Config()
	if err := d.App().UpdateSettings(
		cfg.MonitorTimeoutSecs,
		cfg.BlockedHistoryLimit,
		cfg.AllowedHistoryLimit,
		cfg.BridgeLogLimit,
		2,
		1024,
		cfg.ProxyAlertSound,
	); err != nil {
		return fmt.Errorf("update runtime settings: %w", err)
	}

	persisted, err := d.PersistedConfig()
	if err != nil {
		return fmt.Errorf("load persisted config: %w", err)
	}
	if persisted.ClipboardTTLSecs != 2 || persisted.ClipboardMaxBytes != 1024 {
		return fmt.Errorf("persisted clipboard policy=(%d,%d), want (2,1024)", persisted.ClipboardTTLSecs, persisted.ClipboardMaxBytes)
	}

	pngBytes := minimalPNG()
	obj := clipboard.ClipboardObject{
		Kind:    clipboard.ClipboardKindImage,
		MIME:    "image/png",
		Raw:     pngBytes,
		RawSize: int64(len(pngBytes)),
		Variants: map[string]clipboard.ClipboardVariant{
			"image/png": {MIME: "image/png", Bytes: pngBytes, Size: int64(len(pngBytes))},
		},
	}
	if _, err := d.StageClipboard(obj, 2*time.Second); err != nil {
		return fmt.Errorf("stage clipboard object: %w", err)
	}

	typeResp, body, err := d.ClipboardGet("/clipboard/type", token)
	if err != nil {
		return err
	}
	if typeResp.StatusCode != http.StatusOK {
		return fmt.Errorf("/clipboard/type status=%d body=%s", typeResp.StatusCode, strings.TrimSpace(string(body)))
	}
	payload, err := DecodeJSON(body)
	if err != nil {
		return fmt.Errorf("decode /clipboard/type response: %w", err)
	}
	if payload["state"] != "staged" || payload["kind"] != "image" {
		return fmt.Errorf("/clipboard/type payload=%v", payload)
	}

	imgResp, imgBody, err := d.ClipboardGet("/clipboard/image", token)
	if err != nil {
		return err
	}
	if imgResp.StatusCode != http.StatusOK {
		return fmt.Errorf("/clipboard/image status=%d body=%s", imgResp.StatusCode, strings.TrimSpace(string(imgBody)))
	}
	if imgResp.Header.Get("Content-Type") != "image/png" {
		return fmt.Errorf("/clipboard/image content-type=%q, want image/png", imgResp.Header.Get("Content-Type"))
	}
	if !bytes.Equal(imgBody, pngBytes) {
		return fmt.Errorf("/clipboard/image bytes mismatch")
	}

	return nil
}

func verifyTokenRotationAndRevocation(d *Driver) error {
	barrel, err := d.StartBarrel("claude")
	if err != nil {
		return err
	}

	resp, body, err := d.ClipboardGet("/clipboard/type", barrel.ClipboardToken)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pre-restart token status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := d.RestartBarrel(barrel.Name); err != nil {
		return fmt.Errorf("restart barrel %s: %w", barrel.Name, err)
	}
	if err := d.WaitForContainer(barrel.Name, 15*time.Second); err != nil {
		return err
	}

	rotatedToken, err := d.ReadClipboardToken(barrel.Name)
	if err != nil {
		return err
	}
	if rotatedToken == "" || rotatedToken == barrel.ClipboardToken {
		return fmt.Errorf("token did not rotate for %s", barrel.Name)
	}

	resp, body, err = d.ClipboardGet("/clipboard/type", barrel.ClipboardToken)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("old token after restart status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	resp, body, err = d.ClipboardGet("/clipboard/type", rotatedToken)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rotated token status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := d.StopBarrel(barrel.Name); err != nil {
		return fmt.Errorf("stop barrel %s: %w", barrel.Name, err)
	}

	resp, body, err = d.ClipboardGet("/clipboard/type", rotatedToken)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("token after stop status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func verifyCustomClipboardModeOff(d *Driver) error {
	barrel, err := d.StartBarrel(testdocker.SharedClipboardOffToolName)
	if err != nil {
		return err
	}

	out, err := d.ExecBarrel(barrel.Name, `printf "%s" "$COOPER_CLIPBOARD_MODE"`)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "off" {
		return fmt.Errorf("custom barrel clipboard mode=%q, want off", strings.TrimSpace(out))
	}

	resp, body, err := d.ClipboardGet("/clipboard/type", barrel.ClipboardToken)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusForbidden {
		return fmt.Errorf("custom off clipboard status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func minimalPNG() []byte {
	return []byte{
		0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0d, 'I', 'D', 'A', 'T',
		0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00,
		0x03, 0x01, 0x01, 0x00, 0x18, 0xdd, 0x8d, 0xb1,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D',
		0xae, 0x42, 0x60, 0x82,
	}
}
