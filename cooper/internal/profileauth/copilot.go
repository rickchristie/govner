package profileauth

import (
	"strings"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func (v stateView) copilot() (profiles.Identity, error) {
	if anyValue(v.environment, "COPILOT_PROVIDER_API_KEY", "COPILOT_PROVIDER_BEARER_TOKEN") {
		return profiles.Identity{}, errUnknown
	}
	for _, name := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if key := v.environment[name]; key != "" {
			return apiIdentity("GitHub", v.environment["GH_HOST"]+"/"+v.environment["COPILOT_API_URL"], key)
		}
	}
	type user struct{ Host, Login string }
	var config struct {
		Last      user              `json:"last_logged_in_user"`
		Users     []user            `json:"logged_in_users"`
		Tokens    map[string]string `json:"copilot_tokens"`
		Plaintext bool              `json:"store_token_plaintext"`
	}
	if err := v.read("copilot-state", "config.json", &config, false); err != nil {
		return profiles.Identity{}, err
	}
	if !config.Plaintext && v.environment["COPILOT_DISABLE_KEYTAR"] != "1" {
		return profiles.Identity{}, errUnknown
	}
	selected := config.Last
	if selected.Login == "" && len(config.Users) > 0 {
		selected = config.Users[0]
	}
	if selected.Login == "" || selected.Host == "" || strings.TrimSpace(config.Tokens[selected.Host+":"+selected.Login]) == "" {
		return profiles.Identity{}, errUnknown
	}
	return accountIdentity([]string{"github", strings.ToLower(selected.Host), strings.ToLower(selected.Login)}, selected.Login+" / "+selected.Host), nil
}
