package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestPrepareSessionCreatesAndCleansCommonFiles(t *testing.T) {
	t.Parallel()
	cooperDir := t.TempDir()
	workspace := t.TempDir()
	session, warnings, err := PrepareSession(SessionRequest{
		Config: config.DefaultConfig(), CooperDir: cooperDir, RuntimeID: "runtime-one",
		ToolName: "test-tool", WorkspaceDir: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || !session.Interactive || len(session.Command) == 0 || !strings.Contains(session.Title, filepath.Base(workspace)) {
		t.Fatalf("prepared session = %#v; warnings = %#v", session, warnings)
	}
	for _, path := range []string{session.timezonePath, session.markerPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("session file %s is missing: %v", path, err)
		}
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close() failed: %v", err)
	}
	for _, path := range []string{session.timezonePath, session.markerPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("session file %s remains: %v", path, err)
		}
	}
}

func TestPrepareSessionUsesOneShotCommandWithoutShellMarker(t *testing.T) {
	t.Parallel()
	session, _, err := PrepareSession(SessionRequest{
		Config: config.DefaultConfig(), CooperDir: t.TempDir(), RuntimeID: "runtime-two",
		ToolName: "test-tool", WorkspaceDir: t.TempDir(), OneShot: "printf ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.Interactive || session.markerPath != "" {
		t.Fatalf("one-shot session has an interactive marker: %#v", session)
	}
	joined := strings.Join(session.Command, " ")
	if !strings.Contains(joined, "printf ok") {
		t.Fatalf("one-shot command = %q", joined)
	}
}
