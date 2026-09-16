// Package squidlog shows the recent Squid log and explicit copy controls.
package squidlog

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

const maxLines = 200

type Model struct{ log components.TextLog }

// New accepts an optional copier for fixtures that do not have a desktop.
func New(copier ...components.TextCopier) *Model {
	var copyService components.TextCopier
	if len(copier) > 0 {
		copyService = copier[0]
	}
	return &Model{log: components.NewTextLog("squid", maxLines, copyService)}
}
func (m *Model) Init() tea.Cmd       { return nil }
func (m *Model) AddLine(line string) { m.log.AppendLine(line) }
func (m *Model) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch event := msg.(type) {
	case events.SquidLogLineMsg:
		m.AddLine(event.Line)
	case events.TabActivatedMsg:
		m.log.Update(tea.KeyMsg{Type: tea.KeyEnd})
	default:
		return m, m.log.Update(msg)
	}
	return m, nil
}
func (m *Model) View(width, height int) string { return m.log.View(width, height) }
