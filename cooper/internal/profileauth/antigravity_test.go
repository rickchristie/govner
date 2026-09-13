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
	"github.com/rickchristie/govner/cooper/internal/usercontext"
)

func googleToken(subject, method, project, region, access string) map[string]any {
	claims, _ := json.Marshal(map[string]string{"iss": "https://accounts.google.com", "sub": subject, "aud": "fixture-client", "email": "same@example.test"})
	return map[string]any{
		"token":       map[string]string{"access_token": access, "refresh_token": "fake-refresh", "expiry": "2099-01-01T00:00:00Z"},
		"id_token":    "fixture." + base64.RawURLEncoding.EncodeToString(claims) + ".fake-signature",
		"auth_method": method, "project_id": project, "region": region,
	}
}

func antigravityFixture(t *testing.T) authFixture {
	t.Helper()
	f := newAuthFixture(t, "antigravity")
	if err := os.MkdirAll(f.path("antigravity-state", "antigravity-cli"), 0700); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAntigravityFileIdentityAndScope(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("native macOS keyring has no file identity contract")
	}
	f := antigravityFixture(t)
	write := func(subject, method, project, region, access string) {
		f.write("antigravity-state", "antigravity-cli/antigravity-oauth-token", googleToken(subject, method, project, region, access))
	}
	write("personal", "consumer", "", "", "old-access")
	personal := f.identity("antigravity")
	write("personal", "consumer", "", "", "new-access")
	if f.identity("antigravity").Key != personal.Key {
		t.Fatal("refresh changed identity")
	}
	write("other", "consumer", "", "", "new-access")
	if f.identity("antigravity").Key == personal.Key {
		t.Fatal("same email merged different subjects")
	}
	write("personal", "gcp", "work-project", "global", "new-access")
	work := f.identity("antigravity")
	if work.Key == personal.Key {
		t.Fatal("billing mode merged with consumer account")
	}
	write("personal", "gcp", "other-project", "global", "new-access")
	if f.identity("antigravity").Key == work.Key {
		t.Fatal("different projects merged")
	}
	write("personal", "gcp", "work-project", "eu", "new-access")
	if f.identity("antigravity").Key == work.Key {
		t.Fatal("different regions merged")
	}
}

func TestAntigravityUnknownCredentialsCannotAuthorizeSave(t *testing.T) {
	for _, change := range []func(map[string]any){
		func(v map[string]any) { delete(v, "token") },
		func(v map[string]any) { delete(v, "id_token") },
		func(v map[string]any) { v["auth_method"] = "future-mode" },
		func(v map[string]any) { v["wif_provider"] = "external" },
		func(v map[string]any) { v["saved_wif_provider"] = "external" },
		func(v map[string]any) { v["auth_method"] = "gcp" },
		func(v map[string]any) { v["token"].(map[string]string)["expiry"] = "invalid" },
		func(v map[string]any) { v["token"].(map[string]string)["refresh_token"] = "" },
	} {
		f := antigravityFixture(t)
		token := googleToken("person", "consumer", "", "", "fake-access")
		change(token)
		f.write("antigravity-state", "antigravity-cli/antigravity-oauth-token", token)
		if _, err := (Reader{}).Read(t.Context(), "antigravity", f.mounts, f.env); err == nil {
			t.Fatal("unknown credentials accepted")
		}
	}
	f := antigravityFixture(t)
	f.write("antigravity-state", "antigravity-cli/antigravity-oauth-token", googleToken("person", "consumer", "", "", "fake-access"))
	for _, name := range []string{"DBUS_SESSION_BUS_ADDRESS", "AGY_ADC_AUTH", "JETSKI_OAUTH_TOKEN", "GEMINI_API_KEY"} {
		f.env[name] = "other-source"
		if _, err := (Reader{}).Read(t.Context(), "antigravity", f.mounts, f.env); err == nil {
			t.Fatalf("ambiguous source %s accepted", name)
		}
		delete(f.env, name)
	}
	path := f.path("antigravity-state", "antigravity-cli/antigravity-oauth-token")
	outside := filepath.Join(t.TempDir(), "token")
	if err := os.Rename(path, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := (Reader{}).Read(t.Context(), "antigravity", f.mounts, f.env); err == nil {
		t.Fatal("external credential link accepted")
	}
}

func TestAntigravityAPIKeyNeedsProviderAndKeepsEndpoint(t *testing.T) {
	f := antigravityFixture(t)
	f.env["GEMINI_API_KEY"] = "fake-api-key"
	if _, err := (Reader{}).Read(t.Context(), "antigravity", f.mounts, f.env); err == nil {
		t.Fatal("API key changed provider without host setting")
	}
	f.write("antigravity-state", "antigravity-cli/settings.json", map[string]string{"modelProvider": "gemini"})
	first := f.identity("antigravity")
	f.env["GOOGLE_GEMINI_BASE_URL"] = "https://fixture.example.test"
	if f.identity("antigravity").Key == first.Key {
		t.Fatal("endpoint is missing from credential identity")
	}
	delete(f.env, "GEMINI_API_KEY")
	if _, err := (Reader{}).Read(t.Context(), "antigravity", f.mounts, f.env); err == nil {
		t.Fatal("missing API key accepted")
	}
}

func TestAntigravityProfilesPreserveOutgoingStateAndRejectUnknownIdentity(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("native macOS keyring has no file identity contract")
	}
	f := antigravityFixture(t)
	account, err := usercontext.Current()
	if err != nil {
		t.Fatal(err)
	}
	account.Home = filepath.Dir(f.mounts[0].Source)
	service := profiles.New(profiles.Options{CooperDir: t.TempDir(), Workspace: t.TempDir(), Account: account,
		Environment: f.env, CredentialNames: CredentialNames, Reader: Reader{},
		Guard: profiles.GuardFunc(func(context.Context, []string) error { return nil })})
	write := func(subject, session string) {
		if err := os.MkdirAll(f.path("antigravity-state", "antigravity-cli"), 0700); err != nil {
			t.Fatal(err)
		}
		f.write("antigravity-state", "antigravity-cli/antigravity-oauth-token", googleToken(subject, "consumer", "", "", "fake-access"))
		f.write("antigravity-state", "future-session.json", map[string]string{"value": session})
	}
	save := func() profiles.Result {
		result, err := service.Save(t.Context(), profiles.SaveRequest{Harness: "antigravity"})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	load := func(name string) profiles.Result {
		result, err := service.Load(t.Context(), profiles.LoadRequest{Harness: "antigravity", Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	write("personal", "personal-first")
	if save().Saved != "Default" {
		t.Fatal("first profile was not Default")
	}
	if result := load("Work"); !result.Created || !result.Pending {
		t.Fatal("new profile was not empty and pending")
	}
	write("work", "work-first")
	if save().Saved != "Work" {
		t.Fatal("pending profile did not bind to the new account")
	}
	write("work", "work-updated")
	load("Default")
	if result := save(); result.Saved != "Default" {
		t.Fatal("current account mapping changed")
	}
	work, err := service.Select(t.Context(), "antigravity", "Work")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(work.Paths.Mounts[0].Source, "future-session.json"))
	if err != nil || !strings.Contains(string(data), "work-updated") {
		t.Fatal("load lost outgoing sessions")
	}
	f.write("antigravity-state", "antigravity-cli/antigravity-oauth-token", map[string]string{"future_auth": "unknown"})
	if _, err := service.Save(t.Context(), profiles.SaveRequest{Harness: "antigravity"}); err == nil {
		t.Fatal("unknown identity overwrote Default")
	}
	if _, err := service.Select(t.Context(), "antigravity", "Default"); err != nil {
		t.Fatalf("unknown host state damaged the saved account: %v", err)
	}
}
