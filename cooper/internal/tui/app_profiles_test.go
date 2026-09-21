package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/tui/profileui"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

func TestProfilesReceiveResultsAfterTabChange(t *testing.T) {
	fake := app.NewMockApp(&config.Config{}, t.TempDir())
	m := NewModel(fake)
	screen := profileui.New(fake)
	m.SetProfilesModel(screen)
	m.SetActiveTab(theme.TabRuntime)
	m.Update(profileui.ProfilesListedMsg{Items: app.ProfileStory()})
	m.Update(profileui.ProfileActionCompletedMsg{Action: "save", Result: profiles.Result{Saved: "Default"}})
	if !strings.Contains(screen.View(100, 25), "Checked live profile Default") {
		t.Fatal("late action result was lost on another tab")
	}
	m.SetActiveTab(theme.TabProfiles)
	m.Update(profileui.ProfilesListedMsg{Items: app.ProfileStory()})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("qcx")})
	if m.activeTab != theme.TabProfiles || !screen.ModalActive() || m.modal != nil {
		t.Fatal("global keys escaped the profile form")
	}
	if !strings.Contains(screen.View(100, 25), "qcx") {
		t.Fatal("profile name characters went to global shortcuts")
	}
}

func TestProfilesSmallTerminalKeepsBrandAndScreenHeight(t *testing.T) {
	fake := app.NewMockApp(&config.Config{}, t.TempDir())
	m := NewModel(fake)
	screen := profileui.New(fake)
	m.SetProfilesModel(screen)
	m.SetActiveTab(theme.TabProfiles)
	screen.Update(profileui.ProfilesListedMsg{Items: app.ProfileStory()})
	for _, width := range []int{60, 80, 120} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) != 24 || !strings.Contains(lines[0], "Cooper") {
			t.Fatalf("root frame at %d columns lost its header or height", width)
		}
		for _, line := range lines {
			if lipgloss.Width(line) > width {
				t.Fatalf("root frame wrapped at %d columns (%d): %q", width, lipgloss.Width(line), line)
			}
		}
	}
}
