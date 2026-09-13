package profileauth

import (
	"errors"
	"os"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func (v stateView) opencode() (profiles.Identity, error) {
	if anyValue(v.environment, "AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "GOOGLE_APPLICATION_CREDENTIALS", "ANTHROPIC_AUTH_TOKEN") {
		return profiles.Identity{}, errUnknown
	}
	var auth map[string]struct {
		Type       string `json:"type"`
		Key        string `json:"key"`
		Access     string `json:"access"`
		Refresh    string `json:"refresh"`
		Account    string `json:"accountId"`
		Enterprise string `json:"enterpriseUrl"`
	}
	if err := v.read("opencode-share", "auth.json", &auth, false); err != nil && !errors.Is(err, os.ErrNotExist) {
		return profiles.Identity{}, err
	}
	identities := map[string]profiles.Identity{}
	for provider, value := range auth {
		var identity profiles.Identity
		var err error
		switch value.Type {
		case "api":
			identity, err = apiIdentity(provider, "", value.Key)
		case "oauth":
			if value.Access == "" || value.Refresh == "" {
				return profiles.Identity{}, errUnknown
			}
			account := value.Account
			// OpenCode's OpenAI adapter stores accountId. Other OAuth plugins
			// need their own reviewed stable ID; rotating tokens are not IDs.
			if account == "" {
				return profiles.Identity{}, errUnknown
			}
			user := account
			if provider == "openai" {
				var claims openAIClaims
				if err := jwtClaims(value.Access, &claims); err != nil {
					return profiles.Identity{}, err
				}
				user = claims.Auth.User
				if user == "" {
					user = claims.Auth.LegacyUser
				}
				if user == "" || (claims.Auth.Account != "" && claims.Auth.Account != account) {
					return profiles.Identity{}, errUnknown
				}
			}
			identity = accountIdentity([]string{provider, "oauth", value.Enterprise, user, account}, provider+" / "+account)
		default:
			return profiles.Identity{}, errUnknown
		}
		if err != nil {
			return profiles.Identity{}, err
		}
		identities[provider] = identity
	}
	for _, name := range CredentialNames("opencode") {
		value := v.environment[name]
		if value == "" || name == "OPENAI_BASE_URL" {
			continue
		}
		identity, err := apiIdentity(strings.TrimSuffix(name, "_API_KEY"), v.environment["OPENAI_BASE_URL"], value)
		if err != nil {
			return profiles.Identity{}, err
		}
		identities["environment:"+name] = identity
	}
	return combinedIdentity(identities)
}
