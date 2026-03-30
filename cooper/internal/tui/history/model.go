// Package history provides a reusable two-pane list+detail model for the
// Blocked and Allowed history tabs.
package history

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/proxy"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// defaultMaxCapacity is the default maximum number of entries kept in the history list.
const defaultMaxCapacity = 500

// Mode distinguishes between the Blocked and Allowed history tabs.
type Mode int

const (
	// ModeBlocked renders the tab as Proxy Blocked.
	ModeBlocked Mode = iota
	// ModeAllowed renders the tab as Proxy Allowed.
	ModeAllowed
)

// HistoryEntry is a single resolved request shown in the history list.
type HistoryEntry struct {
	Request         proxy.ACLRequest
	Decision        string // "timeout", "denied", "whitelist", "approved"
	ResponseStatus  int    // HTTP status code (only meaningful for Allowed)
	ResponseHeaders string // raw response headers (only meaningful for Allowed)
	Timestamp       time.Time
}

// Model is the BubbleTea sub-model for a history tab (Blocked or Allowed).
// It implements the tui.SubModel interface.
type Model struct {
	Items       []HistoryEntry
	list        components.ScrollableList
	detailOpen  bool
	mode        Mode
	maxCapacity int
	width       int
	height      int
}

// New creates a new history Model for the given mode with the default capacity.
func New(mode Mode) *Model {
	return NewWithCapacity(mode, defaultMaxCapacity)
}

// NewWithCapacity creates a new history Model for the given mode with the specified capacity.
func NewWithCapacity(mode Mode, capacity int) *Model {
	if capacity < 1 {
		capacity = defaultMaxCapacity
	}
	return &Model{
		mode:        mode,
		maxCapacity: capacity,
		list:        components.NewScrollableList(0, 0),
	}
}

// SetMaxCapacity updates the maximum number of history entries kept at runtime.
func (m *Model) SetMaxCapacity(n int) {
	if n < 1 {
		n = defaultMaxCapacity
	}
	m.maxCapacity = n
	// Trim existing items if needed.
	if len(m.Items) > m.maxCapacity {
		m.Items = m.Items[:m.maxCapacity]
		m.rebuildListItems()
	}
}

// ----- tui.SubModel interface -----

// Init returns nil; the history tab has no startup commands.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update handles messages forwarded from the root model.
func (m *Model) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		m.list.HandleMouse(msg)
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// View renders the history tab content within the given dimensions.
func (m *Model) View(width, height int) string {
	m.width = width
	m.height = height
	m.syncListGeometry()

	if len(m.Items) == 0 {
		return m.renderEmpty(width, height)
	}

	return m.render(width, height)
}

// ----- Public API -----

// AddEntry prepends an entry to the history list and trims to max capacity.
func (m *Model) AddEntry(entry HistoryEntry) {
	m.Items = append([]HistoryEntry{entry}, m.Items...)
	if len(m.Items) > m.maxCapacity {
		m.Items = m.Items[:m.maxCapacity]
	}
	m.rebuildListItems()
}

// ----- Key handling -----

func (m *Model) handleKey(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.list.MoveUp()
	case "down", "j":
		m.list.MoveDown()
	case "G":
		// Jump to bottom (newest is at index 0, so bottom = last index).
		if len(m.list.Items) > 0 {
			m.list.SelectedIdx = len(m.list.Items) - 1
			m.list.ClampScroll()
		}
	case "g":
		// Jump to top.
		m.list.SelectedIdx = 0
		m.list.ClampScroll()
	case "enter":
		m.detailOpen = !m.detailOpen
	case "esc":
		m.detailOpen = false
	}
	return m, nil
}

// ----- Internal helpers -----

// syncListGeometry updates the scrollable list dimensions based on the
// current tab size and whether the detail pane is open.
func (m *Model) syncListGeometry() {
	listHeight := m.height
	if m.detailOpen {
		// List takes top half, detail takes bottom half.
		listHeight = m.height / 2
	}
	// Subtract 1 for the column header row.
	listHeight -= 1
	if listHeight < 1 {
		listHeight = 1
	}

	m.list.Height = listHeight
	m.list.Width = m.width
}

// rebuildListItems converts the HistoryEntry slice into ScrollableList items.
func (m *Model) rebuildListItems() {
	items := make([]components.ListItem, len(m.Items))
	for i, entry := range m.Items {
		items[i] = components.ListItem{
			ID:   entry.Request.ID,
			Data: entry,
		}
	}
	m.list.SetItems(items)
}

// selectedEntry returns the currently selected HistoryEntry, or nil.
func (m *Model) selectedEntry() *HistoryEntry {
	sel := m.list.Selected()
	if sel == nil {
		return nil
	}
	entry, ok := sel.Data.(HistoryEntry)
	if !ok {
		return nil
	}
	return &entry
}
