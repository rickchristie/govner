package profileauth

import (
	"errors"
	"os"
	"time"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func (v stateView) grok() (profiles.Identity, error) {
	if anyValue(v.environment, "GROK_AUTH_PATH", "GROK_AUTH_PROVIDER_COMMAND") {
		return profiles.Identity{}, errUnknown
	}
	var config struct {
		Provider string `toml:"auth_provider_command"`
	}
	if err := v.read("grok-state", "config.toml", &config, true); err != nil && !errors.Is(err, os.ErrNotExist) {
		return profiles.Identity{}, err
	}
	if config.Provider != "" {
		return profiles.Identity{}, errUnknown
	}
	var auth map[string]struct {
		Key     string    `json:"key"`
		Mode    string    `json:"auth_mode"`
		Created time.Time `json:"create_time"`
		// Grok requires this field even when an API record uses an empty user.
		User         *string `json:"user_id"`
		Organization string  `json:"organization_id"`
		Issuer       string  `json:"oidc_issuer"`
		Email        string  `json:"email"`
	}
	var err error
	if value := v.environment["GROK_AUTH"]; value != "" {
		err = decodeJSON([]byte(value), &auth)
	} else {
		err = v.read("grok-state", "auth.json", &auth, false)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return profiles.Identity{}, err
	}
	identities := map[string]profiles.Identity{}
	for scope, value := range auth {
		if value.Key == "" || value.Created.IsZero() || value.User == nil {
			return profiles.Identity{}, errUnknown
		}
		var identity profiles.Identity
		switch value.Mode {
		case "api_key":
			identity, err = apiIdentity("xAI", scope, value.Key)
		case "oidc", "grok":
			if *value.User == "" {
				return profiles.Identity{}, errUnknown
			}
			identity = accountIdentity([]string{"xai", scope, value.Mode, *value.User, value.Organization, value.Issuer}, "xAI / "+*value.User+" / "+value.Organization)
		default:
			return profiles.Identity{}, errUnknown
		}
		if err != nil {
			return profiles.Identity{}, err
		}
		identities[scope] = identity
	}
	// Grok gives a stored session priority over XAI_API_KEY. Keep the complete
	// stored scope set as the account mapping; the copy also keeps every scope.
	if len(identities) > 0 {
		return combinedIdentity(identities)
	}
	if key := v.environment["GROK_DEPLOYMENT_KEY"]; key != "" {
		return apiIdentity("xAI deployment", "", key)
	}
	return apiIdentity("xAI", "", v.environment["XAI_API_KEY"])
}
