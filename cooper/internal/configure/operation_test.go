package configure

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/buildflow"
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
	warnings, prepared, err := executeRequestedPreparation(ca, config.DefaultConfig(), saveModel{saveRequested: true}, func(step int, stepErr error) {
		if stepErr != nil {
			t.Fatalf("step %d returned unexpected error: %v", step, stepErr)
		}
		reported = append(reported, step)
	})
	if err != nil {
		t.Fatalf("executeRequestedPreparation() failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if prepared != nil {
		t.Fatal("save-only operation unexpectedly prepared a Docker build")
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

func TestRequestedPreparationStepsEndBeforeDockerBuild(t *testing.T) {
	steps := requestedPreparationStepNames(saveModel{saveRequested: true, buildRequested: true})
	saveSteps := app.SaveStepNames()
	if len(steps) <= len(saveSteps) {
		t.Fatalf("expected preparation steps after save steps, got %v", steps)
	}
	if want := len(saveSteps) + len(buildflow.StagingStepNames()); len(steps) != want {
		t.Fatalf("loading steps = %d, want %d without duplicated preparation work: %v", len(steps), want, steps)
	}
	if !reflect.DeepEqual(steps[:len(saveSteps)], saveSteps) {
		t.Fatalf("save step prefix = %v, want %v", steps[:len(saveSteps)], saveSteps)
	}
	if got := steps[len(steps)-1]; got != "Staging CA files..." {
		t.Fatalf("last loading step = %q, want %q", got, "Staging CA files...")
	}
	for _, step := range steps {
		if strings.HasPrefix(step, "Building ") {
			t.Fatalf("Docker step %q must not appear on the preparation loading screen", step)
		}
	}
}
