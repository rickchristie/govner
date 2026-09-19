// Package profileui implements the Profiles tab. It sends typed requests to
// the application boundary and owns only selection, forms, and feedback.
package profileui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

type formKind int

const (
	formNone formKind = iota
	formLoad
	formName
	formDelete
	formConflict
	formDetails
)

type form struct {
	Kind    formKind
	Name    string
	Error   string
	Pending ProfileActionCompletedMsg
	Content components.ScrollableContent
}

type Model struct {
	manager       app.ProfileManager
	list          components.ScrollableList
	harness       string
	form          form
	busy          bool
	message       string
	failed        bool
	width, height int
}

func New(manager app.ProfileManager) *Model {
	return &Model{manager: manager, harness: "codex", list: components.NewScrollableList(80, 10), busy: true, width: 80, height: 18}
}

func (m *Model) Init() tea.Cmd { return listCmd(m.manager) }

func (m *Model) ModalActive() bool { return m.form.Kind != formNone }

func (m *Model) Update(message tea.Msg) (theme.SubModel, tea.Cmd) {
	defer m.layout()
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
	case ProfilesListedMsg:
		m.busy = false
		if message.Err != nil {
			m.message, m.failed = message.Err.Error(), true
			return m, nil
		}
		m.applyItems(message.Items)
	case ProfileActionCompletedMsg:
		return m.actionCompleted(message)
	case tea.MouseMsg:
		if m.form.Kind == formDetails {
			m.form.Content.HandleMouse(message, m.detailsFrame(m.width, m.height).BodyHeight())
			return m, nil
		}
		if !m.ModalActive() && !m.busy {
			m.list.HandleMouse(message)
			m.selectHarness()
		}
	case tea.KeyMsg:
		if m.ModalActive() {
			return m.formKey(message)
		}
		if m.busy {
			return m, nil
		}
		return m.key(message)
	}
	return m, nil
}

func (m *Model) applyItems(items []profiles.Summary) {
	selectedID := ""
	if selected := m.list.Selected(); selected != nil {
		selectedID = selected.ID
	}
	rows := make([]components.ListItem, 0, len(items))
	for _, item := range items {
		rows = append(rows, components.ListItem{ID: item.ID, Data: item})
	}
	m.list.SetItems(rows)
	for index, item := range rows {
		if item.ID == selectedID {
			m.list.SelectedIdx = index
			break
		}
	}
	m.list.ClampScroll()
	m.selectHarness()
}

func (m *Model) selectHarness() {
	if item := m.list.Selected(); item != nil {
		m.harness = item.Data.(profiles.Summary).Harness
	}
}

func (m *Model) key(key tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		m.list.MoveUp()
		m.selectHarness()
	case "down", "j":
		m.list.MoveDown()
		m.selectHarness()
	case "r":
		m.busy = true
		return m, listCmd(m.manager)
	case "h":
		harnesses := aitool.Names()
		sort.Strings(harnesses)
		for index, name := range harnesses {
			if name == m.harness {
				m.harness = harnesses[(index+1)%len(harnesses)]
				break
			}
		}
	case "s":
		return m.start(ProfileActionCompletedMsg{Action: "save", Save: profiles.SaveRequest{Harness: m.harness}})
	case "i":
		m.form = form{Kind: formDetails}
	case "n":
		m.form = form{Kind: formLoad}
	case "enter", "l":
		if item := m.list.Selected(); item != nil {
			profile := item.Data.(profiles.Summary)
			return m.start(ProfileActionCompletedMsg{Action: "load", Load: profiles.LoadRequest{Harness: profile.Harness, Name: profile.Name}})
		}
	case "d":
		if item := m.list.Selected(); item != nil {
			profile := item.Data.(profiles.Summary)
			if profile.Loaded || profile.Mixed || profile.InUse {
				m.message, m.failed = "Load another profile and stop its sessions before deletion.", true
				return m, nil
			}
			m.form = form{Kind: formDelete, Pending: ProfileActionCompletedMsg{Action: "delete", Load: profiles.LoadRequest{Harness: profile.Harness, Name: profile.Name}}}
		}
	}
	return m, nil
}

func (m *Model) formKey(key tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	if key.String() == "esc" || key.String() == "ctrl+c" {
		m.form = form{}
		return m, nil
	}
	if m.form.Kind == formConflict {
		return m.conflictKey(key)
	}
	if m.form.Kind == formDetails {
		if key.String() == "enter" {
			m.form = form{}
		} else {
			m.form.Content.HandleKey(key, m.detailsFrame(m.width, m.height).BodyHeight())
		}
		return m, nil
	}
	if m.form.Kind == formDelete {
		if key.String() == "enter" {
			return m.start(m.form.Pending)
		}
		return m, nil
	}
	switch key.String() {
	case "backspace":
		m.form.Name = components.TrimLastRune(m.form.Name)
	case "enter":
		name := strings.TrimSpace(m.form.Name)
		if err := profiles.ValidateName(name); err != nil {
			m.form.Error = err.Error()
			return m, nil
		}
		if m.form.Kind == formLoad {
			return m.start(ProfileActionCompletedMsg{Action: "load", Load: profiles.LoadRequest{Harness: m.harness, Name: name}})
		}
		pending := m.form.Pending
		pending.Save.NewName, pending.Load.NewName = name, name
		return m.start(pending)
	default:
		if entry := components.TextEntryFromKeyMsg(key, nil); entry != "" && len(m.form.Name)+len(entry) <= 40 {
			m.form.Name += entry
		}
	}
	return m, nil
}

func (m *Model) conflictKey(key tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	var choice profiles.ConflictChoice
	switch key.String() {
	case "h":
		choice = profiles.KeepHost
	case "s":
		choice = profiles.KeepSaved
	default:
		return m, nil
	}
	pending := m.form.Pending
	pending.Save.ConflictChoice, pending.Load.ConflictChoice = choice, choice
	return m.start(pending)
}

func (m *Model) start(request ProfileActionCompletedMsg) (theme.SubModel, tea.Cmd) {
	m.busy, m.failed = true, false
	m.message = "Working. Keep the host harness closed."
	m.form = form{}
	manager := m.manager
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		switch request.Action {
		case "save":
			request.Result, request.Err = manager.SaveProfile(ctx, request.Save)
		case "load":
			request.Result, request.Err = manager.LoadProfile(ctx, request.Load)
		case "delete":
			request.Err = manager.DeleteProfile(ctx, request.Load.Harness, request.Load.Name)
		}
		return request
	}
}

func (m *Model) actionCompleted(message ProfileActionCompletedMsg) (theme.SubModel, tea.Cmd) {
	m.busy = false
	if message.Err != nil {
		m.message, m.failed = message.Err.Error(), true
		var issue *profiles.Issue
		if errors.As(message.Err, &issue) {
			switch issue.Kind {
			case profiles.NameRequired:
				m.form = form{Kind: formName, Pending: message}
			case profiles.StateConflict:
				m.form = form{Kind: formConflict, Pending: message}
			}
		}
		return m, nil
	}
	m.failed = false
	m.message = resultText(message)
	m.busy = true
	return m, listCmd(m.manager)
}

func resultText(message ProfileActionCompletedMsg) string {
	if message.Action == "delete" {
		return fmt.Sprintf("Deleted %s/%s.", message.Load.Harness, message.Load.Name)
	}
	result := message.Result
	var parts []string
	if result.Saved != "" {
		if result.Managed {
			parts = append(parts, "Validated live profile "+result.Saved+".")
		} else {
			parts = append(parts, "Saved "+result.Saved+".")
		}
	}
	if result.Loaded != "" {
		parts = append(parts, "Loaded "+result.Loaded+" on the host.")
	}
	if result.Pending {
		parts = append(parts, "Log in on the host, then save this harness.")
	}
	if result.Recovery != "" {
		parts = append(parts, "Recovery: "+result.Recovery)
	}
	if result.Warning != "" {
		parts = append(parts, result.Warning)
	}
	return strings.Join(parts, " ")
}

func listCmd(manager app.ProfileManager) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := manager.ListProfiles(ctx)
		return ProfilesListedMsg{Items: items, Err: err}
	}
}
