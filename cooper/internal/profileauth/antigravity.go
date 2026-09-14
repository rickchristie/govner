package profileauth

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

// Antigravity 1.2.2 reads this nested file schema when it bypasses the OS
// keyring. Native probes with synthetic credentials verified the file path,
// consumer method, token decoding, and service selection without networking.
// A profile copies the whole .gemini root; this reader only identifies it.
func (v stateView) antigravity() (profiles.Identity, error) {
	if value := v.environment["AGY_ADC_AUTH"]; value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil || enabled {
			return profiles.Identity{}, errUnknown
		}
	}
	if anyValue(v.environment, "GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_CONFIG", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "CLOUD_CODE_URL", "BAICODE_ENDPOINT_URL", "JETSKI_OAUTH_TOKEN") {
		return profiles.Identity{}, errUnknown
	}
	var settings struct {
		Provider string `json:"modelProvider"`
	}
	if err := v.read("antigravity-state", "antigravity-cli/settings.json", &settings, false); err != nil && !errors.Is(err, os.ErrNotExist) {
		return profiles.Identity{}, err
	}
	if settings.Provider == "gemini" {
		return apiIdentity("Antigravity Gemini", v.environment["GOOGLE_GEMINI_BASE_URL"], v.environment["GEMINI_API_KEY"])
	}
	if settings.Provider != "" || anyValue(v.environment, "GEMINI_API_KEY", "GOOGLE_GEMINI_BASE_URL") {
		return profiles.Identity{}, errUnknown
	}
	// A present file cannot prove that a desktop host selects that file over
	// another account in its keyring. The checked host wrapper establishes
	// file selection on Linux. Never read or forward the session bus.
	if runtime.GOOS == "darwin" || (v.environment["DBUS_SESSION_BUS_ADDRESS"] != "" && !v.antigravityFileAuth) {
		return profiles.Identity{}, errUnknown
	}
	var stored struct {
		Token struct {
			Access  string    `json:"access_token"`
			Refresh string    `json:"refresh_token"`
			Expiry  time.Time `json:"expiry"`
		} `json:"token"`
		Method   string `json:"auth_method"`
		ID       string `json:"id_token"`
		WIF      string `json:"wif_provider"`
		SavedWIF string `json:"saved_wif_provider"`
		Project  string `json:"project_id"`
		Region   string `json:"region"`
	}
	if err := v.read("antigravity-state", "antigravity-cli/antigravity-oauth-token", &stored, false); err != nil {
		return profiles.Identity{}, err
	}
	if (stored.Method != "consumer" && stored.Method != "gcp") || stored.WIF != "" || stored.SavedWIF != "" || strings.TrimSpace(stored.Token.Access) == "" || strings.TrimSpace(stored.Token.Refresh) == "" || stored.Token.Expiry.IsZero() {
		return profiles.Identity{}, errUnknown
	}
	if stored.Method == "gcp" && (stored.Project == "" || stored.Region == "") {
		return profiles.Identity{}, errUnknown
	}
	var claims struct {
		Issuer   string `json:"iss"`
		Subject  string `json:"sub"`
		Audience string `json:"aud"`
		Email    string `json:"email"`
	}
	if err := jwtClaims(stored.ID, &claims); err != nil {
		return profiles.Identity{}, err
	}
	if (claims.Issuer != "https://accounts.google.com" && claims.Issuer != "accounts.google.com") || claims.Subject == "" || claims.Audience == "" {
		return profiles.Identity{}, errUnknown
	}
	label := claims.Email
	if label == "" {
		label = "Google account"
	}
	if stored.Project != "" {
		label += " / " + stored.Project + " / " + stored.Region
	}
	return accountIdentity([]string{"antigravity", "google", claims.Subject, claims.Audience, stored.Method, stored.Project, stored.Region}, label), nil
}
