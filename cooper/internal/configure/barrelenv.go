package configure

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

type barrelEnvResult int

const (
	barrelEnvNone barrelEnvResult = iota
	barrelEnvBack
)

type barrelEnvModalResult int

const (
	barrelEnvModalNone barrelEnvModalResult = iota
	barrelEnvModalSaved
	barrelEnvModalCancelled
)

type barrelEnvModal struct {
	active     bool
	editing    bool
	editIndex  int
	keyInput   textInput
	valueInput textInput
	focusField int // 0=key, 1=value, 2=save, 3=cancel
	err        string
	savedEntry config.BarrelEnvVar
}

type barrelEnvModel struct {
	entries       []config.BarrelEnvVar
	cursor        int
	modal         barrelEnvModal
	scrollOffset  int
	lastHeight    int
	lastMaxScroll int
}

func newBarrelEnvModel(entries []config.BarrelEnvVar) barrelEnvModel {
	return barrelEnvModel{
		entries: append([]config.BarrelEnvVar(nil), entries...),
		modal: barrelEnvModal{
			keyInput:   newTextInput("e.g., API_BASE_URL", 26),
			valueInput: newTextInput("e.g., https://internal.example.com", 42),
		},
	}
}

func (m *barrelEnvModel) update(msg tea.Msg) barrelEnvResult {
	if m.modal.active {
		return m.updateModal(msg)
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		handleMouseScroll(msg, &m.scrollOffset, m.lastMaxScroll)
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if len(m.entries) > 0 && m.cursor > 0 {
				m.cursor--
				ensureLineVisible(&m.scrollOffset, 4+m.cursor, m.lastHeight, 5, 1)
			} else if m.scrollOffset > 0 {
				m.scrollOffset--
			}
		case "down", "j":
			if len(m.entries) > 0 && m.cursor < len(m.entries)-1 {
				m.cursor++
				ensureLineVisible(&m.scrollOffset, 4+m.cursor, m.lastHeight, 5, 1)
			} else if m.scrollOffset < m.lastMaxScroll {
				m.scrollOffset++
			}
		case "n":
			m.modal.open(false, -1, config.BarrelEnvVar{})
		case "e", "enter":
			if len(m.entries) > 0 {
				m.modal.open(true, m.cursor, m.entries[m.cursor])
			}
		case "x":
			if len(m.entries) > 0 && m.cursor < len(m.entries) {
				m.entries = append(m.entries[:m.cursor], m.entries[m.cursor+1:]...)
				if m.cursor >= len(m.entries) && m.cursor > 0 {
					m.cursor--
				}
			}
		case "pgup", "ctrl+u":
			m.scrollOffset -= 10
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
		case "pgdown", "ctrl+d":
			m.scrollOffset += 10
			if m.scrollOffset > m.lastMaxScroll {
				m.scrollOffset = m.lastMaxScroll
			}
		case "esc":
			return barrelEnvBack
		}
	}

	return barrelEnvNone
}

func (m *barrelEnvModel) updateModal(msg tea.Msg) barrelEnvResult {
	result := m.modal.update(msg, m.entries)
	switch result {
	case barrelEnvModalSaved:
		if m.modal.editing {
			m.entries[m.modal.editIndex] = m.modal.savedEntry
		} else {
			m.entries = append(m.entries, m.modal.savedEntry)
			m.cursor = len(m.entries) - 1
		}
	case barrelEnvModalCancelled:
		// nothing
	}
	return barrelEnvNone
}

func (m *barrelEnvModel) view(width, height int) string {
	m.lastHeight = height
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("Barrel Environment")

	header := breadcrumb

	var content string
	content += lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(
		" Global env vars applied to every cooper cli session.") + "\n\n"

	tbl := tableutil.NewTable("KEY", "VALUE", "STATUS")
	tbl.SetHeaderStyle(theme.ColorDusty, true)
	sepColor := theme.ColorOakLight
	tbl.SetSeparator(theme.BorderH, &sepColor)

	for i, entry := range m.entries {
		value := displayBarrelEnvValue(entry.Value)
		if value == "" {
			value = lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true).Render("(empty)")
		} else {
			value = lipgloss.NewStyle().Foreground(theme.ColorParchment).Render(value)
		}
		statusText, statusColor := barrelEnvEntryStatus(entry, m.entries, i)
		tbl.AddRow(
			lipgloss.NewStyle().Foreground(theme.ColorParchment).Render(strings.TrimSpace(entry.Name)),
			value,
			lipgloss.NewStyle().Foreground(statusColor).Render(statusText),
		)
	}

	rowIndent := "   "
	content += rowIndent + tbl.RenderHeader() + "\n"
	content += rowIndent + tbl.RenderSeparator(0) + "\n"

	if len(m.entries) == 0 {
		content += rowIndent + lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true).Render("(no env vars configured)") + "\n"
	}

	_, rows := tbl.RenderRows(0)
	for i, row := range rows {
		prefix := rowIndent
		if i == m.cursor {
			prefix = " " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " "
		}

		line := prefix + row
		if i == m.cursor {
			line = lipgloss.NewStyle().Background(theme.ColorOakMid).Render(line)
		}
		content += line + "\n"
	}

	content += "\n"
	content += infoBox(
		" Applies globally to every cooper cli session. User env loads first, then\n"+
			" Cooper restores protected runtime values like proxy, display, PATH,\n"+
			" token, terminal color/hyperlink policy and metadata, and IDE env.\n\n"+
			" Values are stored in plain text in ~/.cooper/config.json. Invalid or\n"+
			" protected hand-edited rows stay visible here so you can fix or delete\n"+
			" them. Changes apply on the next cooper cli session; no rebuild needed.",
		width,
	)

	footer := " " + helpBar("[n New]", "[e Edit]", "[x Delete]", "[Esc Back]")

	ly := newLayout(header, content, footer, width, height)
	ly.scrollOffset = m.scrollOffset
	if len(m.entries) > 0 {
		ly.EnsureVisible(4 + m.cursor)
	}
	s := ly.Render()
	m.scrollOffset = ly.scrollOffset
	m.lastMaxScroll = ly.MaxScrollOffset()

	if m.modal.active {
		s = overlayModal(s, m.modal.view(width), width, height)
	}

	return s
}

func (m *barrelEnvModel) toEntries() []config.BarrelEnvVar {
	return append([]config.BarrelEnvVar(nil), m.entries...)
}

func (m *barrelEnvModal) open(editing bool, index int, entry config.BarrelEnvVar) {
	m.active = true
	m.editing = editing
	m.editIndex = index
	m.err = ""
	m.focusField = 0
	m.keyInput.Focus()
	m.valueInput.Blur()

	if editing {
		m.keyInput.SetValue(entry.Name)
		// Existing invalid control characters are escaped here so a hand-edited
		// bad config row remains visible and repairable in this single-line input.
		// Valid values are loaded exactly so editing does not silently rewrite
		// tabs or other accepted bytes.
		m.valueInput.SetValue(editableBarrelEnvValue(entry.Value))
	} else {
		m.keyInput.SetValue("")
		m.valueInput.SetValue("")
	}
}

func (m *barrelEnvModal) close() {
	m.active = false
	m.keyInput.Blur()
	m.valueInput.Blur()
}

func (m *barrelEnvModal) update(msg tea.Msg, entries []config.BarrelEnvVar) barrelEnvModalResult {
	const modalFields = 4

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.close()
			return barrelEnvModalCancelled
		case "down":
			m.focusField = (m.focusField + 1) % modalFields
			m.keyInput.Blur()
			m.valueInput.Blur()
			switch m.focusField {
			case 0:
				m.keyInput.Focus()
			case 1:
				m.valueInput.Focus()
			}
			return barrelEnvModalNone
		case "up":
			m.focusField = (m.focusField - 1 + modalFields) % modalFields
			m.keyInput.Blur()
			m.valueInput.Blur()
			switch m.focusField {
			case 0:
				m.keyInput.Focus()
			case 1:
				m.valueInput.Focus()
			}
			return barrelEnvModalNone
		case "enter":
			if m.focusField == 3 {
				m.close()
				return barrelEnvModalCancelled
			}
			entry, err := m.parseEntry(entries)
			if err != "" {
				m.err = err
				return barrelEnvModalNone
			}
			m.savedEntry = entry
			m.close()
			return barrelEnvModalSaved
		}

		switch m.focusField {
		case 0:
			m.keyInput.handleKeyMsg(msg)
		case 1:
			m.valueInput.handleKeyMsg(msg)
		}
	}

	return barrelEnvModalNone
}

func (m *barrelEnvModal) parseEntry(entries []config.BarrelEnvVar) (config.BarrelEnvVar, string) {
	entry := config.BarrelEnvVar{
		Name:  strings.TrimSpace(m.keyInput.Value()),
		Value: m.valueInput.Value(),
	}
	if err := config.ValidateBarrelEnvVars([]config.BarrelEnvVar{entry}); err != nil {
		return entry, err.Error()
	}
	for i, existing := range entries {
		if m.editing && i == m.editIndex {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(existing.Name), entry.Name) {
			return entry, fmt.Sprintf("barrel env %q duplicates an existing entry", entry.Name)
		}
	}
	return entry, ""
}

func (m *barrelEnvModal) view(width int) string {
	title := "Add Barrel Environment"
	if m.editing {
		title = "Edit Barrel Environment"
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(theme.ColorAmber).
		Padding(1, 3).
		Width(min(58, width-10))

	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen)

	var inner string
	inner += "  " + titleStyle.Render(title) + "\n\n"
	inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorOakLight).Render(repeatStr(theme.BorderH, 42)) + "\n\n"

	inner += "  " + labelStyle.Render("Key:") + "\n"
	inner += m.keyInput.viewWithMargin(2) + "\n\n"

	inner += "  " + labelStyle.Render("Value:") + "\n"
	inner += m.valueInput.viewWithMargin(2) + "\n\n"

	if m.err != "" {
		inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorFlame).Render(m.err) + "\n\n"
	}

	inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorOakLight).Render(repeatStr(theme.BorderH, 42)) + "\n\n"

	if m.focusField == 2 {
		inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " " +
			lipgloss.NewStyle().Background(theme.ColorOakMid).Foreground(theme.ColorProof).Bold(true).Render("["+theme.IconCheck+" Save]")
	} else {
		inner += "    " + lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true).Render("["+theme.IconCheck+" Save]")
	}

	inner += "    "

	if m.focusField == 3 {
		inner += lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " " +
			lipgloss.NewStyle().Background(theme.ColorOakMid).Foreground(theme.ColorDusty).Render("[Cancel]")
	} else {
		inner += lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[Cancel]")
	}

	return boxStyle.Render(inner)
}

func barrelEnvEntryStatus(entry config.BarrelEnvVar, entries []config.BarrelEnvVar, index int) (string, lipgloss.Color) {
	trimmedName := strings.TrimSpace(entry.Name)
	switch {
	case trimmedName == "":
		return "invalid", theme.ColorFlame
	case config.IsProtectedBarrelEnvName(trimmedName):
		return "protected", theme.ColorCopper
	case !isValidBarrelEnvName(trimmedName):
		return "invalid", theme.ColorFlame
	case barrelEnvValueInvalid(entry.Value):
		return "invalid", theme.ColorFlame
	case barrelEnvHasDuplicate(entries, trimmedName, index):
		return "duplicate", theme.ColorCopper
	default:
		return "ok", theme.ColorProof
	}
}

func isValidBarrelEnvName(name string) bool {
	if name == "" {
		return false
	}
	if name[0] != '_' && (name[0] < 'A' || name[0] > 'Z') && (name[0] < 'a' || name[0] > 'z') {
		return false
	}
	for i := 1; i < len(name); i++ {
		ch := name[i]
		if ch == '_' {
			continue
		}
		if ch >= 'A' && ch <= 'Z' {
			continue
		}
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			continue
		}
		return false
	}
	return true
}

func barrelEnvValueInvalid(value string) bool {
	return strings.ContainsRune(value, '\x00') || strings.ContainsRune(value, '\n') || strings.ContainsRune(value, '\r')
}

func barrelEnvHasDuplicate(entries []config.BarrelEnvVar, candidate string, skipIndex int) bool {
	for i, entry := range entries {
		if i == skipIndex {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(entry.Name), candidate) {
			return true
		}
	}
	return false
}

func displayBarrelEnvValue(value string) string {
	value = strings.ReplaceAll(value, "\x00", `\0`)
	value = strings.ReplaceAll(value, "\r", `\r`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, "\t", `\t`)
	return value
}

func editableBarrelEnvValue(value string) string {
	if barrelEnvValueInvalid(value) {
		value = strings.ReplaceAll(value, "\x00", `\0`)
		value = strings.ReplaceAll(value, "\r", `\r`)
		value = strings.ReplaceAll(value, "\n", `\n`)
	}
	return value
}
