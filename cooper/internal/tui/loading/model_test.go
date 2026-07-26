package loading

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
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

func TestLoadingScreenKeepsFixedFrameAndScrollsOverflowingSteps(t *testing.T) {
	var steps []LoadingStep
	for i := range 20 {
		steps = append(steps, LoadingStep{Name: fmt.Sprintf("step %02d", i)})
	}
	m := NewWithOptions(Options{
		Steps:           steps,
		RunningSubtitle: "applying configuration...",
		AllowCancel:     false,
	})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 70, Height: 10})

	rendered := m.View(m.Width, m.Height)
	lines := strings.Split(rendered, "\n")
	if len(lines) != m.Height {
		t.Fatalf("rendered height = %d, want %d:\n%s", len(lines), m.Height, ansi.Strip(rendered))
	}
	if !strings.Contains(ansi.Strip(strings.Join(lines[:4], "\n")), "applying configuration...") {
		t.Fatalf("fixed header missing subtitle:\n%s", ansi.Strip(rendered))
	}
	if !strings.Contains(ansi.Strip(lines[len(lines)-1]), theme.BarrelEmoji) {
		t.Fatalf("fixed footer missing barrel: %q", ansi.Strip(lines[len(lines)-1]))
	}

	before := m.viewport.ScrollOffset
	m, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.viewport.ScrollOffset <= before {
		t.Fatalf("mouse wheel offset = %d, want > %d", m.viewport.ScrollOffset, before)
	}
	afterMouse := m.viewport.ScrollOffset
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.viewport.ScrollOffset <= afterMouse {
		t.Fatalf("down key offset = %d, want > %d", m.viewport.ScrollOffset, afterMouse)
	}
}
