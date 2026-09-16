package profileauth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/rickchristie/govner/cooper/internal/antigravity"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// ErrAntigravitySetupRequired stops launch before a session starts. The CLI
// and VM commands show this normal setup instruction and exit successfully.
var ErrAntigravitySetupRequired = errors.New("Please run cooper build and then run agy to relogin")

// CheckAntigravityLaunch rejects an OAuth session that would need the host
// keyring. Explicit API and external-provider modes retain their native rules.
// Credential values never appear in a launch error.
func CheckAntigravityLaunch(home string, mounts []workload.MountSpec, environment map[string]string) error {
	view := stateView{mounts: mounts, environment: environment}
	var settings struct {
		Provider string `json:"modelProvider"`
	}
	if err := view.read("antigravity-state", "antigravity-cli/settings.json", &settings, false); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("Antigravity settings cannot be read; fix them on the host")
	}
	if settings.Provider != "" {
		return nil
	}
	if enabled, _ := strconv.ParseBool(environment["AGY_ADC_AUTH"]); enabled || anyValue(environment,
		"GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_CONFIG", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION",
		"CLOUD_CODE_URL", "BAICODE_ENDPOINT_URL", "JETSKI_OAUTH_TOKEN") {
		return nil
	}
	if runtime.GOOS != "linux" {
		return errors.New("Antigravity OAuth sharing requires Linux file authentication; the host keyring is not shared")
	}
	busPresent := environment["DBUS_SESSION_BUS_ADDRESS"] != ""
	if _, err := os.Stat(filepath.Join("/run/user", strconv.Itoa(os.Getuid()), "bus")); !os.IsNotExist(err) {
		busPresent = true
	}
	if busPresent && !antigravity.HostFileAuthActive(home) {
		return ErrAntigravitySetupRequired
	}
	// Native OAuth ignores Gemini API variables until modelProvider is gemini.
	// Validate the portable OAuth record without treating an unused key as an
	// account switch. Named profiles keep their stricter ambiguity checks.
	view.environment = map[string]string{}
	if _, err := view.antigravity(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrAntigravitySetupRequired
		}
		return errors.New("Antigravity needs file authentication. Sign in on the host:\n  DBUS_SESSION_BUS_ADDRESS=unix:path=/dev/null agy")
	}
	return nil
}
