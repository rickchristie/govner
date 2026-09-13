package profileauth

import (
	"errors"
	"os"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

type openAIClaims struct {
	Email string `json:"email"`
	Auth  struct {
		User       string `json:"chatgpt_user_id"`
		LegacyUser string `json:"user_id"`
		Account    string `json:"chatgpt_account_id"`
	} `json:"https://api.openai.com/auth"`
}

func (v stateView) codex() (profiles.Identity, error) {
	if anyValue(v.environment, "CODEX_API_KEY", "CODEX_AUTH_JSON") {
		return profiles.Identity{}, errUnknown
	}
	var config struct {
		Store    string `toml:"cli_auth_credentials_store"`
		Provider string `toml:"model_provider"`
		Profile  string `toml:"profile"`
	}
	if err := v.read("codex-state", "config.toml", &config, true); err != nil && !errors.Is(err, os.ErrNotExist) {
		return profiles.Identity{}, err
	}
	if (config.Store != "" && config.Store != "file") || (config.Provider != "" && config.Provider != "openai") || config.Profile != "" {
		return profiles.Identity{}, errUnknown
	}
	var auth struct {
		Mode   string `json:"auth_mode"`
		Key    string `json:"OPENAI_API_KEY"`
		Tokens struct {
			ID      string `json:"id_token"`
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
			Account string `json:"account_id"`
		} `json:"tokens"`
	}
	err := v.read("codex-state", "auth.json", &auth, false)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return profiles.Identity{}, err
	}
	// Codex can use a saved ChatGPT login even when an API key exists in the
	// shell. Do not guess across two different configured authentication modes.
	if auth.Tokens.ID != "" {
		if anyValue(v.environment, "OPENAI_API_KEY", "OPENAI_BASE_URL") || auth.Key != "" || (auth.Mode != "" && auth.Mode != "chatgpt") {
			return profiles.Identity{}, errUnknown
		}
		if auth.Tokens.Access == "" || auth.Tokens.Refresh == "" {
			return profiles.Identity{}, errUnknown
		}
		var claims openAIClaims
		if err := jwtClaims(auth.Tokens.ID, &claims); err != nil {
			return profiles.Identity{}, err
		}
		user := claims.Auth.User
		if user == "" {
			user = claims.Auth.LegacyUser
		}
		account := claims.Auth.Account
		if account == "" {
			account = auth.Tokens.Account
		}
		if user == "" || account == "" || (auth.Tokens.Account != "" && auth.Tokens.Account != account) {
			return profiles.Identity{}, errUnknown
		}
		label := claims.Email
		if label == "" {
			label = "OpenAI account"
		}
		return accountIdentity([]string{"openai", "chatgpt", user, account}, label+" / "+account), nil
	}
	if auth.Mode != "" && auth.Mode != "apikey" {
		return profiles.Identity{}, errUnknown
	}
	key := v.environment["OPENAI_API_KEY"]
	if key == "" {
		key = auth.Key
	}
	return apiIdentity("OpenAI", v.environment["OPENAI_BASE_URL"]+"/"+v.environment["OPENAI_ORG_ID"]+"/"+v.environment["OPENAI_PROJECT_ID"], key)
}
