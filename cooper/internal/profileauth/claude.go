package profileauth

import (
	"errors"
	"os"
	"runtime"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func (v stateView) claude() (profiles.Identity, error) {
	var settings struct {
		APIKeyHelper string            `json:"apiKeyHelper"`
		Environment  map[string]string `json:"env"`
	}
	if err := v.read("claude-state", "settings.json", &settings, false); err != nil && !errors.Is(err, os.ErrNotExist) {
		return profiles.Identity{}, err
	}
	env := make(map[string]string, len(v.environment)+len(settings.Environment))
	for name, value := range v.environment {
		env[name] = value
	}
	for name, value := range settings.Environment {
		env[name] = value
	}
	if settings.APIKeyHelper != "" || anyValue(env, "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR", "CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR", "ANTHROPIC_AUTH_TOKEN") {
		return profiles.Identity{}, errUnknown
	}
	if key := env["ANTHROPIC_API_KEY"]; key != "" {
		return apiIdentity("Anthropic", env["ANTHROPIC_BASE_URL"], key)
	}
	if runtime.GOOS == "darwin" {
		return profiles.Identity{}, errUnknown
	} // Claude prefers the system keychain on macOS.
	var credentials struct {
		OAuth struct {
			Access  string `json:"accessToken"`
			Refresh string `json:"refreshToken"`
		} `json:"claudeAiOauth"`
	}
	if err := v.read("claude-state", ".credentials.json", &credentials, false); err != nil {
		return profiles.Identity{}, err
	}
	if credentials.OAuth.Access == "" || credentials.OAuth.Refresh == "" {
		return profiles.Identity{}, errUnknown
	}
	var config struct {
		Account struct {
			ID           string `json:"accountUuid"`
			Organization string `json:"organizationUuid"`
			Email        string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	root, child := "claude-config", ""
	if _, err := v.root(root); err != nil {
		root, child = "claude-state", ".claude.json"
	}
	if err := v.read(root, child, &config, false); err != nil {
		return profiles.Identity{}, err
	}
	account := config.Account
	if account.ID == "" || account.Organization == "" {
		return profiles.Identity{}, errUnknown
	}
	return accountIdentity([]string{"anthropic", account.ID, account.Organization}, account.Email+" / "+account.Organization), nil
}
