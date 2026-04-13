package configure

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestWelcomeMenuIncludesBarrelEnvironment(t *testing.T) {
	m := newModel(config.DefaultConfig(), t.TempDir(), nil, false)
	if len(m.welcome.items) != 7 {
		t.Fatalf("len(welcome.items) = %d, want 7", len(m.welcome.items))
	}
	if got := m.welcome.items[5].label; got != "Barrel Environment" {
		t.Fatalf("welcome.items[5].label = %q, want %q", got, "Barrel Environment")
	}
	if got := m.welcome.items[6].label; got != "Save & Build" {
		t.Fatalf("welcome.items[6].label = %q, want %q", got, "Save & Build")
	}
}

func TestWelcomeShortcutSixNavigatesToBarrelEnv(t *testing.T) {
	m := newModel(config.DefaultConfig(), t.TempDir(), nil, false)
	m.updateWelcome(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'6'}})
	if m.screen != ScreenBarrelEnv {
		t.Fatalf("screen = %v, want %v", m.screen, ScreenBarrelEnv)
	}
}

func TestWelcomeShortcutSevenNavigatesToSave(t *testing.T) {
	m := newModel(config.DefaultConfig(), t.TempDir(), nil, false)
	m.updateWelcome(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'7'}})
	if m.screen != ScreenSave {
		t.Fatalf("screen = %v, want %v", m.screen, ScreenSave)
	}
}

func TestNewBarrelEnvModelInitializesEntries(t *testing.T) {
	entries := []config.BarrelEnvVar{{Name: "FOO", Value: "1"}, {Name: "BAR", Value: "2"}}
	m := newBarrelEnvModel(entries)
	if len(m.entries) != 2 || m.entries[0] != entries[0] || m.entries[1] != entries[1] {
		t.Fatalf("entries = %+v, want %+v", m.entries, entries)
	}
}

func TestBarrelEnvModalSavesTrimmedKeyAndExactValue(t *testing.T) {
	m := newBarrelEnvModel(nil)
	m.modal.open(false, -1, config.BarrelEnvVar{})
	m.modal.keyInput.SetValue("  API_URL  ")
	m.modal.valueInput.SetValue("  keep leading and trailing spaces  ")

	m.updateModal(tea.KeyMsg{Type: tea.KeyEnter})

	if len(m.entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(m.entries))
	}
	if got := m.entries[0].Name; got != "API_URL" {
		t.Fatalf("entry name = %q, want %q", got, "API_URL")
	}
	if got := m.entries[0].Value; got != "  keep leading and trailing spaces  " {
		t.Fatalf("entry value = %q", got)
	}
}

func TestBarrelEnvModalAllowsEmptyValue(t *testing.T) {
	m := newBarrelEnvModel(nil)
	m.modal.open(false, -1, config.BarrelEnvVar{})
	m.modal.keyInput.SetValue("EMPTY")
	m.modal.valueInput.SetValue("")

	m.updateModal(tea.KeyMsg{Type: tea.KeyEnter})

	if len(m.entries) != 1 || m.entries[0].Value != "" {
		t.Fatalf("entries = %+v, want EMPTY with empty value", m.entries)
	}
}

func TestBarrelEnvModalRejectsMalformedKey(t *testing.T) {
	m := newBarrelEnvModel(nil)
	m.modal.open(false, -1, config.BarrelEnvVar{})
	m.modal.keyInput.SetValue("BAD-NAME")

	m.updateModal(tea.KeyMsg{Type: tea.KeyEnter})

	if !m.modal.active {
		t.Fatal("expected modal to remain open on error")
	}
	if m.modal.err == "" {
		t.Fatal("expected modal error")
	}
	if len(m.entries) != 0 {
		t.Fatalf("entries = %+v, want empty", m.entries)
	}
}

func TestBarrelEnvModalRejectsProtectedKey(t *testing.T) {
	m := newBarrelEnvModel(nil)
	m.modal.open(false, -1, config.BarrelEnvVar{})
	m.modal.keyInput.SetValue("HTTP_PROXY")

	m.updateModal(tea.KeyMsg{Type: tea.KeyEnter})

	if !m.modal.active {
		t.Fatal("expected modal to remain open on error")
	}
	if !strings.Contains(strings.ToLower(m.modal.err), "protected") {
		t.Fatalf("modal err = %q, want protected-name error", m.modal.err)
	}
}

func TestBarrelEnvEditUpdatesSelectedRowOnly(t *testing.T) {
	m := newBarrelEnvModel([]config.BarrelEnvVar{{Name: "FOO", Value: "1"}, {Name: "BAR", Value: "2"}})
	m.cursor = 1
	m.modal.open(true, 1, m.entries[1])
	m.modal.keyInput.SetValue("BAR")
	m.modal.valueInput.SetValue("updated")

	m.updateModal(tea.KeyMsg{Type: tea.KeyEnter})

	if m.entries[0].Value != "1" || m.entries[1].Value != "updated" {
		t.Fatalf("entries = %+v", m.entries)
	}
}

func TestBarrelEnvDeleteAdjustsCursor(t *testing.T) {
	m := newBarrelEnvModel([]config.BarrelEnvVar{{Name: "FOO", Value: "1"}, {Name: "BAR", Value: "2"}})
	m.cursor = 1
	m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	if len(m.entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(m.entries))
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
}

func TestBarrelEnvModalEscapeCancelsWithoutMutation(t *testing.T) {
	m := newBarrelEnvModel([]config.BarrelEnvVar{{Name: "FOO", Value: "1"}})
	m.modal.open(true, 0, m.entries[0])
	m.modal.keyInput.SetValue("BAR")
	m.modal.valueInput.SetValue("2")

	m.updateModal(tea.KeyMsg{Type: tea.KeyEsc})

	if m.modal.active {
		t.Fatal("expected modal to close on escape")
	}
	if m.entries[0].Name != "FOO" || m.entries[0].Value != "1" {
		t.Fatalf("entries mutated on cancel: %+v", m.entries)
	}
}

func TestModelSyncConfigFromSubModelsIncludesBarrelEnv(t *testing.T) {
	cfg := config.DefaultConfig()
	m := newModel(cfg, t.TempDir(), nil, false)
	m.barrelEnv = newBarrelEnvModel([]config.BarrelEnvVar{{Name: "FOO", Value: "1"}})
	m.syncConfigFromSubModels()

	if len(m.cfg.BarrelEnvVars) != 1 || m.cfg.BarrelEnvVars[0].Name != "FOO" {
		t.Fatalf("cfg.BarrelEnvVars = %+v", m.cfg.BarrelEnvVars)
	}
}

func TestModelIsTextInputActiveWhenBarrelEnvModalOpen(t *testing.T) {
	m := newModel(config.DefaultConfig(), t.TempDir(), nil, false)
	m.screen = ScreenBarrelEnv
	m.barrelEnv.modal.open(false, -1, config.BarrelEnvVar{})

	if !m.isTextInputActive() {
		t.Fatal("expected isTextInputActive() to be true when barrel env modal is open")
	}
}

func TestSaveViewIncludesBarrelEnvCount(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "FOO", Value: "1"}, {Name: "BAR", Value: "2"}}
	m := newSaveModel(cfg, t.TempDir(), "/tmp/config.json", nil)
	view := m.view(120, 40)
	if !strings.Contains(view, "Barrel Environment:") || !strings.Contains(view, "2 entries") {
		t.Fatalf("save view missing barrel env count:\n%s", view)
	}
}

func TestBarrelEnvModelRendersAndDeletesInvalidEntry(t *testing.T) {
	m := newBarrelEnvModel([]config.BarrelEnvVar{{Name: "HTTP_PROXY", Value: "bad"}})
	view := m.view(120, 40)
	if !strings.Contains(view, "HTTP_PROXY") {
		t.Fatalf("view missing invalid entry:\n%s", view)
	}
	m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if len(m.entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0", len(m.entries))
	}
}

func TestBarrelEnvModalCanRepairRowWhileOtherInvalidRowsRemain(t *testing.T) {
	m := newBarrelEnvModel([]config.BarrelEnvVar{{Name: "HTTP_PROXY", Value: "bad"}, {Name: "BAD-NAME", Value: "x"}})
	m.modal.open(true, 0, m.entries[0])
	m.modal.keyInput.SetValue("REPAIRED_ONE")
	m.modal.valueInput.SetValue("ok")

	m.updateModal(tea.KeyMsg{Type: tea.KeyEnter})

	if m.modal.active {
		t.Fatal("expected modal to close after repairing the selected row")
	}
	if m.entries[0].Name != "REPAIRED_ONE" || m.entries[0].Value != "ok" {
		t.Fatalf("repaired row = %+v", m.entries[0])
	}
	if m.entries[1].Name != "BAD-NAME" {
		t.Fatalf("other invalid row should remain editable, got %+v", m.entries[1])
	}
}
