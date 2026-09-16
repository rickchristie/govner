package profileauth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/antigravity"
)

func TestAntigravityLaunchRequestsSetupForMissingFileAndChangedHostWrapper(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("managed file authentication requires Linux")
	}
	f := antigravityFixture(t)
	home := filepath.Dir(f.path("antigravity-state", ""))
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	f.env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=/cooper-fake-desktop-bus"
	if err := CheckAntigravityLaunch(home, f.mounts, f.env); !errors.Is(err, ErrAntigravitySetupRequired) {
		t.Fatalf("unwrapped host did not get setup instructions: %v", err)
	}
	setup, err := antigravity.InstallHostFileAuth(t.Context(), home, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(setup.Wrapper)+":"+os.Getenv("PATH"))
	if err := CheckAntigravityLaunch(home, f.mounts, f.env); !errors.Is(err, ErrAntigravitySetupRequired) {
		t.Fatalf("missing file did not get the host login command: %v", err)
	}
	f.write("antigravity-state", "antigravity-cli/antigravity-oauth-token", googleToken("person", "consumer", "", "", "never-print-this-access-token"))
	if err := CheckAntigravityLaunch(home, f.mounts, f.env); err != nil {
		t.Fatal(err)
	}
	f.env["GEMINI_API_KEY"] = "native-ignores-this-key-in-oauth-mode"
	if err := CheckAntigravityLaunch(home, f.mounts, f.env); err != nil {
		t.Fatal("an unused key changed native OAuth selection", err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	err = CheckAntigravityLaunch(home, f.mounts, f.env)
	if !errors.Is(err, ErrAntigravitySetupRequired) || strings.Contains(err.Error(), "never-print-this-access-token") {
		t.Fatalf("changed wrapper selection was not safely rejected: %v", err)
	}
}

func TestAntigravityLaunchRetainsExplicitProviderModes(t *testing.T) {
	f := antigravityFixture(t)
	home := filepath.Dir(f.path("antigravity-state", ""))
	f.env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=/cooper-fake-desktop-bus"
	f.write("antigravity-state", "antigravity-cli/settings.json", map[string]string{"modelProvider": "gemini"})
	f.env["GEMINI_API_KEY"] = "fake-key"
	if err := CheckAntigravityLaunch(home, f.mounts, f.env); err != nil {
		t.Fatal("API mode was forced to use OAuth", err)
	}
	f.write("antigravity-state", "antigravity-cli/settings.json", map[string]string{})
	delete(f.env, "GEMINI_API_KEY")
	f.env["AGY_ADC_AUTH"] = "true"
	if err := CheckAntigravityLaunch(home, f.mounts, f.env); err != nil {
		t.Fatal("explicit ADC mode was forced to use OAuth", err)
	}
}
