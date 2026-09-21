package profileui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func key(value string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)} }

func readyModel(t *testing.T) (*Model, *app.MockApp) {
	t.Helper()
	fake := &app.MockApp{ProfilesVal: app.ProfileStory()}
	m := New(fake)
	m.Update(m.Init()())
	return m, fake
}

func TestSaveUsesHarnessAndReportsAccountMismatch(t *testing.T) {
	m, fake := readyModel(t)
	fake.ProfileActionErr = &profiles.Issue{Kind: profiles.AccountConflict, Message: "account mismatch"}
	_, command := m.Update(key("s"))
	if command == nil || !m.busy || len(fake.SaveProfileCalls) != 0 {
		t.Fatal("save ran in Update or did not enter busy state")
	}
	m.Update(command())
	if m.ModalActive() || !m.failed {
		t.Fatal("account mismatch was not reported")
	}
	if len(fake.SaveProfileCalls) != 1 || fake.SaveProfileCalls[0].Harness != "claude" {
		t.Fatalf("save requests: %+v", fake.SaveProfileCalls)
	}
}

func TestLoadAndCreateUseSameAction(t *testing.T) {
	m, fake := readyModel(t)
	_, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("load command missing")
	}
	m.Update(command())
	if fake.LoadProfileCalls[0].Name != "Default" || fake.LoadProfileCalls[0].Harness != "claude" {
		t.Fatal("selected row was not loaded")
	}
	m.Update(ProfilesListedMsg{Items: app.ProfileStory()})
	m.Update(key("n"))
	m.Update(key("WorkNew"))
	_, command = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(command())
	if fake.LoadProfileCalls[1].Name != "WorkNew" {
		t.Fatal("new name was not sent through Load")
	}
}

func TestDeleteCancelBusyAndFailure(t *testing.T) {
	m, fake := readyModel(t)
	m.Update(key("d"))
	if m.ModalActive() || !m.failed {
		t.Fatal("loaded profile opened a delete confirmation")
	}
	m.Update(key("j"))
	m.Update(key("d"))
	if !m.ModalActive() {
		t.Fatal("unused profile has no delete confirmation")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(fake.DeleteProfileCalls) != 0 || m.ModalActive() {
		t.Fatal("cancel deleted data")
	}
	m.Update(key("d"))
	fake.ProfileActionErr = errors.New("fixture is in use")
	_, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, repeat := m.Update(key("d"))
	if repeat != nil {
		t.Fatal("busy screen allowed another action")
	}
	m.Update(command())
	if !m.failed || !strings.Contains(m.message, "fixture is in use") || m.list.SelectedIdx != 1 {
		t.Fatal("failure lost selection or its reason")
	}
}

func TestConfirmationRetriesOriginalRequestAndDefaultsToNo(t *testing.T) {
	m, fake := readyModel(t)
	fake.ProfileActionErr = &profiles.Issue{Kind: profiles.ConfirmationRequired, ProfileID: "outgoing", Message: "Close all claude CLI instances and apps."}
	_, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(command())
	if m.form.Kind != formConfirm {
		t.Fatal("confirmation form missing")
	}
	_, canceled := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if canceled != nil || m.ModalActive() || len(fake.LoadProfileCalls) != 1 {
		t.Fatal("default answer was not No")
	}
	_, command = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(command())
	fake.ProfileActionErr = nil
	_, command = m.Update(key("Y"))
	m.Update(command())
	if !fake.LoadProfileCalls[2].Confirmed || fake.LoadProfileCalls[2].ExpectedProfileID != "outgoing" || fake.LoadProfileCalls[2].Name != "Default" {
		t.Fatal("confirmation changed its target or lost the outgoing selection")
	}
}

func TestViewsArePureAndFitSmallTerminal(t *testing.T) {
	m, _ := readyModel(t)
	before := *m
	for _, size := range [][2]int{{120, 28}, {60, 12}, {35, 7}} {
		view := m.View(size[0], size[1])
		if len(strings.Split(view, "\n")) > size[1] {
			t.Fatal("view exceeded height")
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > size[0] {
				t.Fatalf("view exceeded width: %q", line)
			}
		}
	}
	if !reflect.DeepEqual(before, *m) {
		t.Fatal("View changed model state")
	}
	m.Update(key("n"))
	before = *m
	_ = m.View(60, 12)
	if !reflect.DeepEqual(before, *m) {
		t.Fatal("form View changed model state")
	}
}

func TestEmptyScreenCanSaveAndChooseHarness(t *testing.T) {
	fake := &app.MockApp{}
	m := New(fake)
	m.Update(m.Init()())
	if !strings.Contains(m.View(80, 18), "No saved profiles") {
		t.Fatal("empty instructions missing")
	}
	m.Update(key("h"))
	_, command := m.Update(key("s"))
	m.Update(command())
	if fake.SaveProfileCalls[0].Harness != "copilot" {
		t.Fatal("empty state harness was not selected")
	}
}

func TestDetailsUseSharedViewportForKeysAndMouse(t *testing.T) {
	m, _ := readyModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	m.message = strings.Repeat("A complete account recovery message.\n", 35) + "Recovery details end."
	m.Update(key("i"))
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.form.Content.ScrollOffset == 0 {
		t.Fatal("mouse could not scroll long details")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !strings.Contains(m.View(60, 12), "Recovery details end.") {
		t.Fatal("end key could not reach the recovery result")
	}
	before := m.form.Content.ScrollOffset
	_ = m.View(35, 7)
	if m.form.Content.ScrollOffset != before {
		t.Fatal("detail View changed scroll state")
	}
}

func TestLiveDetailsAndSelectedDelete(t *testing.T) {
	m, _ := readyModel(t)
	m.Update(ProfilesListedMsg{Items: []profiles.Summary{{ID: "selected", Harness: "codex", Name: "Default", Loaded: true}}})
	_, cmd := m.Update(key("d"))
	if cmd != nil || m.ModalActive() {
		t.Fatal("selected profile can be deleted")
	}
	m.Update(key("i"))
	text := m.detailsText()
	for _, want := range []string{"Live profile", "cooper profiles backup", ".agents directory is shared", "Linux only"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing detail", want)
		}
	}
}

func TestConfirmationScrollsWithoutConfirming(t *testing.T) {
	m, fake := readyModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	fake.ProfileActionErr = &profiles.Issue{Kind: profiles.ConfirmationRequired, ProfileID: "outgoing", Message: strings.Repeat("Close the affected apps.\n", 20)}
	_, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(command())
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.form.Content.ScrollOffset == 0 {
		t.Fatal("confirmation did not scroll with the mouse")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !strings.Contains(m.View(60, 12), "The default answer is No.") {
		t.Fatal("confirmation end was not reachable")
	}
	if len(fake.LoadProfileCalls) != 1 || !m.ModalActive() {
		t.Fatal("scrolling confirmed the switch")
	}
	before := m.form.Content.ScrollOffset
	_ = m.View(35, 7)
	if m.form.Content.ScrollOffset != before {
		t.Fatal("confirmation View changed scroll state")
	}
}
