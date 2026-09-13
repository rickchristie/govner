package profileauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func fakeJWT(user, account, email string) string {
	data, _ := json.Marshal(map[string]any{"email": email, "https://api.openai.com/auth": map[string]string{"chatgpt_user_id": user, "chatgpt_account_id": account}})
	return "fixture." + base64.RawURLEncoding.EncodeToString(data) + ".fake-signature"
}

type authFixture struct {
	t      *testing.T
	mounts []workload.MountSpec
	env    map[string]string
}

func newAuthFixture(t *testing.T, harness string) authFixture {
	t.Helper()
	home := t.TempDir()
	paths, err := workload.ResolveAgentScope(harness, home, t.TempDir(), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, mount := range paths.Mounts {
		path := mount.Source
		if mount.Kind == workload.File {
			path = filepath.Dir(path)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return authFixture{t: t, mounts: paths.Mounts, env: map[string]string{}}
}

func (f authFixture) path(id, child string) string {
	f.t.Helper()
	for _, mount := range f.mounts {
		if mount.ID == id {
			return filepath.Join(mount.Source, child)
		}
	}
	f.t.Fatal("root is not in fixture", id)
	return ""
}

func (f authFixture) write(id, child string, data any) {
	f.t.Helper()
	encoded, err := json.Marshal(data)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(f.path(id, child), encoded, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f authFixture) identity(harness string) profiles.Identity {
	f.t.Helper()
	identity, err := (Reader{}).Read(context.Background(), harness, f.mounts, f.env)
	if err != nil {
		f.t.Fatal(err)
	}
	if len(identity.Key) != 64 || identity.Label == "" {
		f.t.Fatalf("invalid identity: %+v", identity)
	}
	return identity
}

func TestCodexIdentitySurvivesRefreshAndSeparatesWorkspaces(t *testing.T) {
	f := newAuthFixture(t, "codex")
	write := func(account, access string) {
		f.write("codex-state", "auth.json", map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{"id_token": fakeJWT("person", account, "fake@example.test"), "access_token": access, "refresh_token": "fake-refresh", "account_id": account}})
	}
	write("personal", "old-fake-access")
	first := f.identity("codex")
	write("personal", "new-fake-access")
	if f.identity("codex").Key != first.Key {
		t.Fatal("token refresh changed the account")
	}
	write("enterprise", "new-fake-access")
	if f.identity("codex").Key == first.Key {
		t.Fatal("organization was not part of account identity")
	}
	f.env["OPENAI_API_KEY"] = "never-print-this-key"
	_, err := (Reader{}).Read(context.Background(), "codex", f.mounts, f.env)
	if err == nil || strings.Contains(err.Error(), "never-print") {
		t.Fatalf("ambiguous login: %v", err)
	}
}

func TestClaudeReadsLoginPairAndRejectsLoggedOutMetadata(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS prefers keychain authentication")
	}
	f := newAuthFixture(t, "claude")
	f.write("claude-config", "", map[string]any{"oauthAccount": map[string]string{"accountUuid": "fixture-user", "organizationUuid": "fixture-org", "emailAddress": "fake@example.test"}})
	f.write("claude-state", ".credentials.json", map[string]any{"claudeAiOauth": map[string]string{"accessToken": "fake-access", "refreshToken": "fake-refresh"}})
	first := f.identity("claude")
	f.write("claude-state", ".credentials.json", map[string]any{"claudeAiOauth": map[string]string{"accessToken": "rotated-access", "refreshToken": "rotated-refresh"}})
	if f.identity("claude").Key != first.Key {
		t.Fatal("Claude refresh changed identity")
	}
	if err := os.Remove(f.path("claude-state", ".credentials.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := (Reader{}).Read(context.Background(), "claude", f.mounts, f.env); err == nil {
		t.Fatal("logged-out account metadata authorized save")
	}
}

func TestCopilotRequiresSelectedUserFileToken(t *testing.T) {
	f := newAuthFixture(t, "copilot")
	config := map[string]any{"store_token_plaintext": true, "last_logged_in_user": map[string]string{"host": "https://github.com", "login": "fixture-user"}, "copilot_tokens": map[string]string{"https://github.com:fixture-user": "fake-token"}}
	f.write("copilot-state", "config.json", config)
	first := f.identity("copilot")
	config["copilot_tokens"] = map[string]string{"https://github.com:fixture-user": "refreshed-token"}
	f.write("copilot-state", "config.json", config)
	if f.identity("copilot").Key != first.Key {
		t.Fatal("Copilot token rotation changed identity")
	}
	config["store_token_plaintext"] = false
	f.write("copilot-state", "config.json", config)
	if _, err := (Reader{}).Read(context.Background(), "copilot", f.mounts, f.env); err == nil {
		t.Fatal("possible keychain override accepted")
	}
}

func TestOpenCodeCombinesProviderIdentities(t *testing.T) {
	f := newAuthFixture(t, "opencode")
	config := map[string]any{
		"openai":    map[string]any{"type": "oauth", "access": fakeJWT("fixture-user", "work", "fake@example.test"), "refresh": "fake-refresh", "expires": 123, "accountId": "work"},
		"anthropic": map[string]string{"type": "api", "key": "fake-anthropic-key"},
	}
	f.write("opencode-share", "auth.json", config)
	first := f.identity("opencode")
	config["openai"].(map[string]any)["refresh"] = "rotated-refresh"
	f.write("opencode-share", "auth.json", config)
	if f.identity("opencode").Key != first.Key {
		t.Fatal("provider refresh changed combined identity")
	}
	config["anthropic"] = map[string]string{"type": "api", "key": "different-key"}
	f.write("opencode-share", "auth.json", config)
	if f.identity("opencode").Key == first.Key {
		t.Fatal("provider account change was missed")
	}
	config["anthropic"] = map[string]string{"type": "oauth", "access": "opaque-access", "refresh": "opaque-refresh"}
	f.write("opencode-share", "auth.json", config)
	if _, err := (Reader{}).Read(context.Background(), "opencode", f.mounts, f.env); err == nil {
		t.Fatal("opaque token was treated as an account ID")
	}
}

func TestGrokReadsReviewedScopeMap(t *testing.T) {
	f := newAuthFixture(t, "grok")
	entry := map[string]string{"key": "fake-access", "auth_mode": "oidc", "create_time": "2026-01-01T00:00:00Z", "user_id": "fixture-user", "organization_id": "personal", "oidc_issuer": "https://issuer.example.test"}
	f.write("grok-state", "auth.json", map[string]any{"fixture-scope": entry})
	first := f.identity("grok")
	entry["key"] = "rotated-access"
	f.write("grok-state", "auth.json", map[string]any{"fixture-scope": entry})
	if f.identity("grok").Key != first.Key {
		t.Fatal("Grok token rotation changed identity")
	}
	entry["organization_id"] = "enterprise"
	f.write("grok-state", "auth.json", map[string]any{"fixture-scope": entry})
	if f.identity("grok").Key == first.Key {
		t.Fatal("Grok organization change was missed")
	}
	entry["auth_mode"] = "web_login"
	f.write("grok-state", "auth.json", map[string]any{"fixture-scope": entry})
	if _, err := (Reader{}).Read(context.Background(), "grok", f.mounts, f.env); err == nil {
		t.Fatal("obsolete web login was accepted")
	}
}

func TestAPIKeyIdentityAndCredentialCatalog(t *testing.T) {
	for harness, name := range map[string]string{"codex": "OPENAI_API_KEY", "claude": "ANTHROPIC_API_KEY", "copilot": "GH_TOKEN", "opencode": "OPENAI_API_KEY", "grok": "XAI_API_KEY"} {
		t.Run(harness, func(t *testing.T) {
			f := newAuthFixture(t, harness)
			f.env[name] = "fake-api-key-A"
			first := f.identity(harness)
			if strings.Contains(first.Key+first.Label, "fake-api-key") {
				t.Fatal("secret leaked to identity")
			}
			f.env[name] = "fake-api-key-B"
			if f.identity(harness).Key == first.Key {
				t.Fatal("API credential change was missed")
			}
			names := CredentialNames(harness)
			found := false
			for _, candidate := range names {
				if candidate == name {
					found = true
				}
			}
			if !found {
				t.Fatal("API variable was not captured")
			}
		})
	}
}

func TestGrokRequiresCompleteAPIAuthRecords(t *testing.T) {
	entry := map[string]string{"key": "fake-file-key", "auth_mode": "api_key", "create_time": "2026-01-01T00:00:00Z", "user_id": ""}
	f := newAuthFixture(t, "grok")
	f.env["XAI_API_KEY"] = "fake-environment-key"
	f.write("grok-state", "auth.json", map[string]any{"fixture-scope": entry})
	f.identity("grok")
	for _, missing := range []string{"key", "auth_mode", "create_time", "user_id"} {
		t.Run(missing, func(t *testing.T) {
			incomplete := make(map[string]string, len(entry))
			for name, value := range entry {
				if name != missing {
					incomplete[name] = value
				}
			}
			f.write("grok-state", "auth.json", map[string]any{"fixture-scope": incomplete})
			if _, err := (Reader{}).Read(t.Context(), "grok", f.mounts, f.env); err == nil {
				t.Fatal("incomplete Grok record was used to select an account")
			}
		})
	}
}

func TestIdentityRejectsUnsafeDocumentsWithoutSecretOutput(t *testing.T) {
	for label, data := range map[string]string{"malformed": "{\"secret\":\"never-print", "duplicate": "{\"OPENAI_API_KEY\":\"never-print\",\"OPENAI_API_KEY\":\"other\"}", "oversized": strings.Repeat("s", maximumDocument+1), "null": "null", "trailing": "{} {}"} {
		t.Run(label, func(t *testing.T) {
			f := newAuthFixture(t, "codex")
			if err := os.WriteFile(f.path("codex-state", "auth.json"), []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := (Reader{}).Read(context.Background(), "codex", f.mounts, f.env)
			if err == nil || strings.Contains(err.Error(), "never-print") {
				t.Fatalf("unsafe document: %v", err)
			}
		})
	}
	f := newAuthFixture(t, "codex")
	outside := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(outside, []byte(`{"OPENAI_API_KEY":"fake-external-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, f.path("codex-state", "auth.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := (Reader{}).Read(context.Background(), "codex", f.mounts, f.env); err == nil {
		t.Fatal("external auth link was followed")
	}
}
