package configure

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestSaveModelSaveOnlyDefersDiskWrites(t *testing.T) {
	cooperDir := t.TempDir()
	ca, err := app.NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp() failed: %v", err)
	}

	m := newSaveModel(config.DefaultConfig(), cooperDir, filepath.Join(cooperDir, "config.json"), ca)
	result := m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if result != saveQuit {
		t.Fatalf("update(save only) = %v, want %v", result, saveQuit)
	}
	if !m.saveRequested {
		t.Fatal("expected saveRequested=true after save-only shortcut")
	}
	if m.buildRequested {
		t.Fatal("expected buildRequested=false for save-only shortcut")
	}
	if m.cleanBuildRequested {
		t.Fatal("expected cleanBuildRequested=false for save-only shortcut")
	}
	if _, err := os.Stat(filepath.Join(cooperDir, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("config.json should not be written from the save key handler, stat err=%v", err)
	}
}

func TestExecuteRequestedActionSaveOnlyReportsAllSaveSteps(t *testing.T) {
	cooperDir := t.TempDir()
	ca, err := app.NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp() failed: %v", err)
	}

	var reported []int
	warnings, err := executeRequestedAction(ca, config.DefaultConfig(), saveModel{saveRequested: true}, io.Discard, func(step int, stepErr error) {
		if stepErr != nil {
			t.Fatalf("step %d returned unexpected error: %v", step, stepErr)
		}
		reported = append(reported, step)
	})
	if err != nil {
		t.Fatalf("executeRequestedAction() failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}

	wantReported := make([]int, len(app.SaveStepNames()))
	for i := range wantReported {
		wantReported[i] = i
	}
	if !reflect.DeepEqual(reported, wantReported) {
		t.Fatalf("reported steps = %v, want %v", reported, wantReported)
	}
	if _, err := os.Stat(filepath.Join(cooperDir, "config.json")); err != nil {
		t.Fatalf("expected config.json to be written, stat failed: %v", err)
	}
}

func TestRequestedStepNamesSaveAndBuildIncludesBuildPlan(t *testing.T) {
	cooperDir := t.TempDir()
	customDir := filepath.Join(cooperDir, "cli", "custom-tool")
	if err := os.MkdirAll(customDir, 0755); err != nil {
		t.Fatalf("MkdirAll(customDir) failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(customDir, "Dockerfile"), []byte("FROM scratch\n"), 0644); err != nil {
		t.Fatalf("WriteFile(Dockerfile) failed: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.AITools = []config.ToolConfig{{Name: "claude", Enabled: true}}

	steps, err := requestedStepNames(saveModel{saveRequested: true, buildRequested: true}, cfg, cooperDir)
	if err != nil {
		t.Fatalf("requestedStepNames() failed: %v", err)
	}

	saveSteps := app.SaveStepNames()
	if len(steps) <= len(saveSteps) {
		t.Fatalf("expected build steps after save steps, got %v", steps)
	}
	if !reflect.DeepEqual(steps[:len(saveSteps)], saveSteps) {
		t.Fatalf("save step prefix = %v, want %v", steps[:len(saveSteps)], saveSteps)
	}
	if !containsStep(steps, "Building claude image...") {
		t.Fatalf("expected claude build step in %v", steps)
	}
	if !containsStep(steps, "Building custom image custom-tool...") {
		t.Fatalf("expected custom image build step in %v", steps)
	}
}

func containsStep(steps []string, want string) bool {
	for _, step := range steps {
		if step == want {
			return true
		}
	}
	return false
}
