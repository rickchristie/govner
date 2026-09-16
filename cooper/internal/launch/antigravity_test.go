package launch

import (
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/profileauth"
	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func TestAntigravityLaunchStopsBeforeCreatingSessionFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/false")
	for _, name := range []string{"GEMINI_API_KEY", "GOOGLE_GEMINI_BASE_URL", "AGY_ADC_AUTH", "GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_CONFIG", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "CLOUD_CODE_URL", "BAICODE_ENDPOINT_URL", "JETSKI_OAUTH_TOKEN", "DBUS_SESSION_BUS_ADDRESS"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	request := SessionRequest{Config: config.DefaultConfig(), CooperDir: dir, RuntimeID: "agy-file-test", ToolName: "antigravity", WorkspaceDir: t.TempDir()}
	session, _, err := PrepareSession(request)
	if err == nil || session != nil {
		t.Fatalf("missing OAuth files started a session: %v", err)
	}
	if runtime.GOOS == "linux" && !errors.Is(err, profileauth.ErrAntigravitySetupRequired) {
		t.Fatalf("missing OAuth files did not request setup: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected authentication created runtime state")
	}
	// Named selections were already checked against their saved identity.
	// The active host's different login must not replace that selection.
	request.State = &profiles.Selection{ID: "selected-account", Name: "Work"}
	request.OneShot = "true"
	session, _, err = PrepareSession(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
}
