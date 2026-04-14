package loading

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewWithOptionsUsesCustomConfigureFlowSettings(t *testing.T) {
	m := NewWithOptions(Options{
		Steps:           []LoadingStep{{Name: "Saving configuration..."}, {Name: "Building images..."}},
		RunningSubtitle: "applying configuration...",
		DoneSubtitle:    "configuration applied",
		ErrorSubtitle:   "configuration failed",
		AllowCancel:     false,
	})

	if len(m.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(m.Steps))
	}
	if m.Steps[0].Status != StepRunning {
		t.Fatalf("Steps[0].Status = %v, want %v", m.Steps[0].Status, StepRunning)
	}
	if got := m.subtitle(); got != "applying configuration..." {
		t.Fatalf("subtitle() = %q, want %q", got, "applying configuration...")
	}
	if got := m.helpLine(); strings.Contains(got, "Cancel") {
		t.Fatalf("helpLine() should hide cancel affordance, got %q", got)
	}
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}); cmd != nil {
		t.Fatal("expected q to be ignored while cancel is disabled")
	}
}

func TestCustomProgressTargetsDefaultToEvenDistribution(t *testing.T) {
	m := NewWithOptions(Options{
		Steps: []LoadingStep{{Name: "one"}, {Name: "two"}, {Name: "three"}},
	})

	updated, _ := m.completeStep(0)
	if updated.targetProg != 1.0/3.0 {
		t.Fatalf("targetProg after first step = %v, want %v", updated.targetProg, 1.0/3.0)
	}
	updated, _ = updated.completeStep(1)
	updated, _ = updated.completeStep(2)
	if updated.targetProg != 1.0 {
		t.Fatalf("targetProg after final step = %v, want 1.0", updated.targetProg)
	}
}
