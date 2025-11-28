package view

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	model "github.com/rickchristie/govner/gowt/model"
)

// TreeViewRequest represents a request from TreeView to the controller
type TreeViewRequest interface {
	isTreeViewRequest()
}

// SelectTestRequest is emitted when user selects a test to view logs
type SelectTestRequest struct {
	Node *model.TestNode
}

func (SelectTestRequest) isTreeViewRequest() {}

// RerunTestRequest is emitted when user wants to rerun a test
type RerunTestRequest struct {
	Node *model.TestNode
}

func (RerunTestRequest) isTreeViewRequest() {}

// RerunFailedRequest is emitted when user wants to rerun all failed tests
type RerunFailedRequest struct{}

func (RerunFailedRequest) isTreeViewRequest() {}

// RerunAllRequest is emitted when user wants to rerun all tests from scratch
type RerunAllRequest struct{}

func (RerunAllRequest) isTreeViewRequest() {}

// QuitRequest is emitted when user wants to quit
type QuitRequest struct{}

func (QuitRequest) isTreeViewRequest() {}

// ShowHelpRequest is emitted when user wants to see help
type ShowHelpRequest struct{}

func (ShowHelpRequest) isTreeViewRequest() {}

// FilterMode represents the current filter state
type FilterMode int

const (
	FilterAll   FilterMode = iota
	FilterFocus            // Shows failed + running tests
)

func (f FilterMode) String() string {
	switch f {
	case FilterFocus:
		return "Focus"
	default:
		return "All"
	}
}

// TreeView is a pure view for displaying the test tree (Screen 1)
type TreeView struct {
	tree         *model.TestTree
	cursor       int
	scrollTop    int // First visible row index (for proper scroll behavior)
	filter       FilterMode
	width        int
	height       int
	viewport     viewport.Model
	ready        bool
	styles       treeStyles
	running      bool // Whether tests are still running
	animFrame    int  // Animation frame for spinner
	selectorAnim int  // Animation frame for selector (0 = no animation)
	expanded     bool // Track if tree is in expanded state (for toggle)

	// Cache for visible nodes to avoid repeated sort+flatten
	cachedNodes      []*model.TestNode // Cached result of getVisibleNodes()
	cachedNodesValid bool              // Whether cache is valid
}

type treeStyles struct {
	header      lipgloss.Style
	helpBar     lipgloss.Style
	selected    lipgloss.Style
	passed      lipgloss.Style
	failed      lipgloss.Style
	skipped     lipgloss.Style
	running     lipgloss.Style
	pending     lipgloss.Style
	cached      lipgloss.Style // For cached test indicator
	packageName lipgloss.Style
	testName    lipgloss.Style
	elapsed     lipgloss.Style
	progressBar lipgloss.Style
	selector    []lipgloss.Style // Colors for selector animation (cursor movement)

	// Pre-computed styles to avoid allocations in hot paths
	selectedRow lipgloss.Style // Selection style: deep blue background with white bold text
	boldPassed  lipgloss.Style // Bold variant of passed for header
	boldFailed  lipgloss.Style // Bold variant of failed for header
	boldSkipped lipgloss.Style // Bold variant of skipped for header
	boldCached  lipgloss.Style // Bold variant of cached for header

	// Pre-rendered progress bar segments [width] = rendered string (width 0-20)
	barPassed    [21]string // "━" repeated 0-20 times, styled green
	barFailed    [21]string // "━" repeated 0-20 times, styled red
	barSkipped   [21]string // "━" repeated 0-20 times, styled gray
	barRemaining [21]string // "─" repeated 0-20 times, styled dim

	// Pre-computed help bar widths (avoids lipgloss.Width() per frame)
	helpBarWidthAll   int // Width of help bar when filter is "All"
	helpBarWidthFocus int // Width of help bar when filter is "Focus"
}

func defaultTreeStyles() treeStyles {
	// Selector animation: bright green -> normal green (for cursor movement feedback)
	selectorColors := []lipgloss.Style{
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("154")), // Bright green (animated)
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("118")), // Light green
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("82")),  // Green
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("46")),  // Normal green (resting)
	}

	// Pre-compute base styles
	passedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	failedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	skippedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	cachedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	progressBarStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	// Pre-render progress bar segments (width 0-20)
	var barPassed, barFailed, barSkipped, barRemaining [21]string
	for i := 0; i <= 20; i++ {
		barPassed[i] = passedStyle.Render(strings.Repeat("━", i))
		barFailed[i] = failedStyle.Render(strings.Repeat("━", i))
		barSkipped[i] = skippedStyle.Render(strings.Repeat("━", i))
		barRemaining[i] = progressBarStyle.Render(strings.Repeat("─", i))
	}

	// Pre-compute help bar widths (avoids lipgloss.Width() per frame)
	helpBarAll := "[Space All]  [Arrows Navigate]  [↵ Logs]  [r Rerun]  [? Help]  [q Quit]"
	helpBarFocus := "[Space Focus]  [Arrows Navigate]  [↵ Logs]  [r Rerun]  [? Help]  [q Quit]"

	return treeStyles{
		header: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")),
		helpBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")),
		selected: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")),
		passed:  passedStyle,
		failed:  failedStyle,
		skipped: skippedStyle,
		running: lipgloss.NewStyle().
			Foreground(lipgloss.Color("33")),
		pending: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")),
		cached: cachedStyle,
		packageName: lipgloss.NewStyle().
			Bold(true),
		testName: lipgloss.NewStyle(),
		elapsed: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")),
		progressBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")),
		selector: selectorColors,

		// Pre-computed styles for hot paths
		selectedRow: lipgloss.NewStyle().
			Background(lipgloss.Color("24")).
			Foreground(lipgloss.Color("231")).
			Bold(true),
		boldPassed:  passedStyle.Bold(true),
		boldFailed:  failedStyle.Bold(true),
		boldSkipped: skippedStyle.Bold(true),
		boldCached:  cachedStyle.Bold(true),

		// Pre-rendered progress bar segments
		barPassed:    barPassed,
		barFailed:    barFailed,
		barSkipped:   barSkipped,
		barRemaining: barRemaining,

		// Pre-computed help bar widths
		helpBarWidthAll:   lipgloss.Width(helpBarAll),
		helpBarWidthFocus: lipgloss.Width(helpBarFocus),
	}
}

// NewTreeView creates a new TreeView
func NewTreeView() TreeView {
	return TreeView{
		tree:     model.NewTestTree(),
		cursor:   0,
		filter:   FilterAll,
		styles:   defaultTreeStyles(),
		expanded: false, // Start collapsed for stable view during test runs
	}
}

// Init implements tea.Model
func (v TreeView) Init() tea.Cmd {
	return nil
}

// SetData replaces the entire test tree and refreshes the cache
func (v TreeView) SetData(tree *model.TestTree) TreeView {
	v.tree = tree
	v.cachedNodesValid = false // Invalidate cache
	v = v.refreshCache()       // Recompute
	if v.cursor >= len(v.cachedNodes) {
		v.cursor = max(0, len(v.cachedNodes)-1)
	}
	return v
}

// refreshCache recomputes and caches visible nodes if invalid
func (v TreeView) refreshCache() TreeView {
	if v.cachedNodesValid {
		return v
	}
	v.cachedNodes = v.computeVisibleNodes()
	v.cachedNodesValid = true
	return v
}

// getVisibleNodes returns visible nodes, computing fresh if cache is invalid
// This is a pure function that doesn't mutate v - used in View() context
func (v TreeView) getVisibleNodes() []*model.TestNode {
	if v.cachedNodesValid {
		return v.cachedNodes
	}
	return v.computeVisibleNodes()
}

// SetRunning sets whether tests are still running
func (v TreeView) SetRunning(running bool) TreeView {
	v.running = running
	return v
}

// SetElapsed updates the elapsed time without invalidating the visible nodes cache.
// Use this for tick updates where only the elapsed time changes.
func (v TreeView) SetElapsed(elapsed float64) TreeView {
	v.tree.Elapsed = elapsed
	return v
}

// Tick advances the animation frame
func (v TreeView) Tick() TreeView {
	v.animFrame++
	// Decay selector animation
	if v.selectorAnim > 0 {
		v.selectorAnim--
	}
	return v
}

// UpdateEvent processes a single test event
func (v TreeView) UpdateEvent(event model.TestEvent) TreeView {
	v.tree.ProcessEvent(event)
	v.cachedNodesValid = false // Invalidate cache - status may have changed
	return v
}

// KeyMap defines the keybindings for TreeView
type treeKeyMap struct {
	Up           key.Binding
	Down         key.Binding
	Left         key.Binding
	Right        key.Binding
	Enter        key.Binding
	Filter       key.Binding
	Rerun        key.Binding
	RerunFailed  key.Binding
	Quit         key.Binding
	Top          key.Binding
	Bottom       key.Binding
	ToggleExpand key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	Help         key.Binding
}

var treeKeys = treeKeyMap{
	Up:           key.NewBinding(key.WithKeys("up", "k", "K"), key.WithHelp("↑/k", "up")),
	Down:         key.NewBinding(key.WithKeys("down", "j", "J"), key.WithHelp("↓/j", "down")),
	Left:         key.NewBinding(key.WithKeys("left", "h", "H"), key.WithHelp("←/h", "collapse")),
	Right:        key.NewBinding(key.WithKeys("right", "l", "L"), key.WithHelp("→/l", "expand")),
	Enter:        key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view logs")),
	Filter:       key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "filter")),
	Rerun:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rerun")),
	RerunFailed:  key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "rerun failed")),
	Quit:         key.NewBinding(key.WithKeys("q", "Q", "ctrl+c"), key.WithHelp("q", "quit")),
	Top:          key.NewBinding(key.WithKeys("g", "ctrl+home"), key.WithHelp("g", "top")),
	Bottom:       key.NewBinding(key.WithKeys("G", "ctrl+end"), key.WithHelp("G", "bottom")),
	ToggleExpand: key.NewBinding(key.WithKeys("e", "E"), key.WithHelp("e", "toggle expand")),
	PageUp:       key.NewBinding(key.WithKeys("pgup", "ctrl+u", "ctrl+U")),
	PageDown:     key.NewBinding(key.WithKeys("pgdown", "ctrl+d", "ctrl+D")),
	Help:         key.NewBinding(key.WithKeys("?")),
}

// Update implements tea.Model and returns (model, cmd, request)
// Request is non-nil when the view needs the controller to handle something
func (v TreeView) Update(msg tea.Msg) (TreeView, tea.Cmd, TreeViewRequest) {
	var cmd tea.Cmd
	var request TreeViewRequest

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		if !v.ready {
			v.viewport = viewport.New(msg.Width, msg.Height-4)
			v.ready = true
		} else {
			v.viewport.Width = msg.Width
			v.viewport.Height = msg.Height - 4
		}

	case tea.KeyMsg:
		// Ensure cache is valid before reading
		v = v.refreshCache()
		nodes := v.cachedNodes
		oldCursor := v.cursor

		switch {
		case key.Matches(msg, treeKeys.Help):
			request = ShowHelpRequest{}

		case key.Matches(msg, treeKeys.Up):
			if v.cursor > 0 {
				v.cursor--
			}

		case key.Matches(msg, treeKeys.Down):
			if v.cursor < len(nodes)-1 {
				v.cursor++
			}

		case key.Matches(msg, treeKeys.Left):
			if v.cursor < len(nodes) {
				node := nodes[v.cursor]
				if node.Expanded && node.HasChildren() {
					node.Expanded = false
					v.cachedNodesValid = false // Expansion changed
				} else if node.Parent != nil {
					v = v.selectNode(node.Parent)
				}
			}

		case key.Matches(msg, treeKeys.Right):
			if v.cursor < len(nodes) {
				node := nodes[v.cursor]
				if node.HasChildren() {
					node.Expanded = true
					v.cachedNodesValid = false // Expansion changed
				}
			}

		case key.Matches(msg, treeKeys.Enter):
			if v.cursor < len(nodes) {
				request = SelectTestRequest{Node: nodes[v.cursor]}
			}

		case key.Matches(msg, treeKeys.Filter):
			v.filter = (v.filter + 1) % 2
			v.cachedNodesValid = false // Filter changed
			// Always reset cursor and scroll to top when switching filters
			v.cursor = 0
			v.scrollTop = 0
			// Preserve user's expand/collapse state - no auto-expansion

		case key.Matches(msg, treeKeys.Rerun):
			request = RerunAllRequest{}

		case key.Matches(msg, treeKeys.RerunFailed):
			request = RerunFailedRequest{}

		case key.Matches(msg, treeKeys.Quit):
			request = QuitRequest{}

		case key.Matches(msg, treeKeys.Top):
			v.cursor = 0
			v.scrollTop = 0

		case key.Matches(msg, treeKeys.Bottom):
			if len(nodes) > 0 {
				v.cursor = len(nodes) - 1
			}

		case key.Matches(msg, treeKeys.PageUp):
			pageSize := v.height - 4
			if pageSize < 1 {
				pageSize = 10
			}
			v.cursor -= pageSize
			if v.cursor < 0 {
				v.cursor = 0
			}

		case key.Matches(msg, treeKeys.PageDown):
			pageSize := v.height - 4
			if pageSize < 1 {
				pageSize = 10
			}
			v.cursor += pageSize
			if v.cursor >= len(nodes) {
				v.cursor = max(0, len(nodes)-1)
			}

		case key.Matches(msg, treeKeys.ToggleExpand):
			// Toggle based on tracked state, not current node states
			// This prevents issues when new tests arrive with Expanded: false
			if v.expanded {
				// Collapse all
				for _, pkg := range v.tree.Packages {
					collapseAll(pkg)
				}
				v.cursor = 0
				v.scrollTop = 0
				v.expanded = false
			} else {
				// Expand all
				for _, pkg := range v.tree.Packages {
					expandAll(pkg)
				}
				v.expanded = true
			}
			v.cachedNodesValid = false // Expansion changed
		}

		// Trigger selector animation if cursor moved
		if v.cursor != oldCursor {
			v.selectorAnim = len(v.styles.selector) // Start animation
		}

		// Refresh cache if invalidated (before View() is called)
		v = v.refreshCache()

		// Update scroll position after cursor changes
		v.scrollTop = v.computeScrollTop()
	}

	return v, cmd, request
}

func expandAll(node *model.TestNode) {
	node.Expanded = true
	for _, child := range node.Children {
		expandAll(child)
	}
}

func collapseAll(node *model.TestNode) {
	node.Expanded = false
	for _, child := range node.Children {
		collapseAll(child)
	}
}

func (v TreeView) selectNode(target *model.TestNode) TreeView {
	// Use cached nodes (caller should have refreshed cache)
	for i, node := range v.cachedNodes {
		if node == target {
			v.cursor = i
			return v
		}
	}
	return v
}

// computeVisibleNodes computes the visible nodes list (expensive - use cache when possible)
func (v TreeView) computeVisibleNodes() []*model.TestNode {
	if v.filter == FilterAll {
		// Sort packages by done count (descending) so completed tests bubble up
		return v.flattenSortedByDone()
	}

	// FilterFocus: show failed + running tests and their parents
	// Only show if there are failures or running tests
	_, failed, _, running, _ := v.tree.ComputeAllStats()
	if failed == 0 && running == 0 {
		return nil // Nothing to focus on
	}

	// Collect focus-relevant packages and sort them:
	// 1. Failed packages first (alphabetically)
	// 2. Running packages second (alphabetically)
	var focusPackages []*model.TestNode
	for _, pkg := range v.tree.Packages {
		if pkg.Name == "" {
			continue
		}
		if isFocusRelevant(pkg) {
			focusPackages = append(focusPackages, pkg)
		}
	}

	// Sort packages: failed first (alpha), then running (alpha)
	sortNodesByFocusPriority(focusPackages)

	// Flatten sorted packages with children also sorted by focus priority
	var result []*model.TestNode
	for _, pkg := range focusPackages {
		result = append(result, flattenFocusNodesSorted(pkg)...)
	}
	return result
}

// sortNodesByFocusPriority sorts nodes for Focus mode:
// 1. Failed nodes first (or nodes containing failures), sorted alphabetically
// 2. Running nodes second (or nodes containing running), sorted alphabetically
func sortNodesByFocusPriority(nodes []*model.TestNode) {
	sort.Slice(nodes, func(i, j int) bool {
		// Determine if node has failures (itself or descendants) - O(1)
		iHasFailure := nodes[i].Status == model.StatusFailed || nodes[i].FailedCount > 0
		jHasFailure := nodes[j].Status == model.StatusFailed || nodes[j].FailedCount > 0

		// Failed nodes come first
		if iHasFailure != jHasFailure {
			return iHasFailure
		}

		// Within same priority group, sort alphabetically by name
		return nodes[i].Name < nodes[j].Name
	})
}

// isFocusRelevant returns true if node should be shown in Focus mode
// Uses pre-computed counts (O(1)) - counts are propagated synchronously in ProcessEvent
func isFocusRelevant(node *model.TestNode) bool {
	return node.Status == model.StatusFailed ||
		node.Status == model.StatusRunning ||
		node.FailedCount > 0 ||
		node.RunningCount > 0
}

// flattenFocusNodesSorted flattens focus nodes with children sorted by focus priority
func flattenFocusNodesSorted(node *model.TestNode) []*model.TestNode {
	var result []*model.TestNode

	// Include this node if it's focus-relevant (check actual status, not counts)
	if isFocusRelevant(node) {
		result = append(result, node)
		// Only include children if expanded
		if node.Expanded {
			// Collect and sort focus-relevant children
			var focusChildren []*model.TestNode
			for _, child := range node.Children {
				if isFocusRelevant(child) {
					focusChildren = append(focusChildren, child)
				}
			}
			// Sort uses counts (O(1) per comparison) - this is the optimized part
			sortNodesByFocusPriority(focusChildren)

			for _, child := range focusChildren {
				result = append(result, flattenFocusNodesSorted(child)...)
			}
		}
	}

	return result
}

// flattenSortedByDone returns nodes with packages sorted by completion count (descending)
func (v TreeView) flattenSortedByDone() []*model.TestNode {
	// Get packages and sort by done count descending
	packages := make([]*model.TestNode, 0, len(v.tree.Packages))
	for _, pkg := range v.tree.Packages {
		if pkg.Name == "" {
			continue
		}
		packages = append(packages, pkg)
	}

	sortNodesByDone(packages)

	// Flatten sorted packages (with children also sorted)
	var result []*model.TestNode
	for _, pkg := range packages {
		result = append(result, flattenNodeSortedByDone(pkg)...)
	}
	return result
}

// sortNodesByDone sorts nodes by completion count descending
func sortNodesByDone(nodes []*model.TestNode) {
	sort.Slice(nodes, func(i, j int) bool {
		passedI, failedI, skippedI, _ := nodes[i].CountByStatus()
		doneI := passedI + failedI + skippedI

		passedJ, failedJ, skippedJ, _ := nodes[j].CountByStatus()
		doneJ := passedJ + failedJ + skippedJ

		// Sort by done count descending, then by name for stability
		if doneI != doneJ {
			return doneI > doneJ
		}
		return nodes[i].FullPath < nodes[j].FullPath
	})
}

// flattenNodeSortedByDone flattens a node with children sorted by done count
func flattenNodeSortedByDone(node *model.TestNode) []*model.TestNode {
	result := []*model.TestNode{node}
	if node.Expanded && len(node.Children) > 0 {
		// Sort children by done count
		children := make([]*model.TestNode, len(node.Children))
		copy(children, node.Children)
		sortNodesByDone(children)

		for _, child := range children {
			result = append(result, flattenNodeSortedByDone(child)...)
		}
	}
	return result
}

// View implements tea.Model
func (v TreeView) View() string {
	// Ensure cache is fresh before rendering
	v = v.refreshCache()

	var sb strings.Builder

	// Header
	sb.WriteString(v.renderHeader())
	sb.WriteString("\n")

	// Help bar
	sb.WriteString(v.renderHelpBar())
	sb.WriteString("\n\n")

	// Tree content.
	// renderTree is the most expensive operation in TreeView.
	sb.WriteString(v.renderTree())

	return sb.String()
}

// Note: Spinner frames moved to icons.go as SpinnerFrames

// Pre-computed indent strings to avoid repeated strings.Repeat() allocations
// Covers depths 0-10 (4 spaces per level)
var indentPool = func() []string {
	pool := make([]string, 11)
	for i := range pool {
		pool[i] = strings.Repeat("    ", i)
	}
	return pool
}()

// getIndent returns the indent string for a given depth, using pooled strings when possible
func getIndent(depth int) string {
	if depth < len(indentPool) {
		return indentPool[depth]
	}
	// Fallback for very deep nesting (rare)
	return strings.Repeat("    ", depth)
}

func (v TreeView) renderHeader() string {
	// Get pre-computed stats (O(1) - counts are maintained incrementally)
	passed, failed, skipped, running, cached := v.tree.ComputeAllStats()
	elapsed := v.tree.Elapsed

	// Status indicator: always show logo - animated color when running, static when done
	var statusIndicator string
	if v.running {
		statusIndicator = GetSpinnerGear(v.animFrame) // Pre-rendered gear with spinner color
	} else {
		// Show logo when done - red if failures, green otherwise
		if failed > 0 {
			statusIndicator = IconGearFailed // Pre-rendered red gear
		} else {
			statusIndicator = IconGearPassed // Pre-rendered green gear
		}
	}

	// Show passed count with cached indicator if any (use pre-computed bold styles)
	var passedStr string
	if cached > 0 {
		passedStr = v.styles.boldPassed.Render(fmt.Sprintf("%s %d", IconCharPassed, passed)) +
			v.styles.boldCached.Render(fmt.Sprintf(" (%s %d)", IconCharCached, cached))
	} else {
		passedStr = v.styles.boldPassed.Render(fmt.Sprintf("%s %d", IconCharPassed, passed))
	}
	failedStr := v.styles.boldFailed.Render(fmt.Sprintf("%s %d", IconCharFailed, failed))
	skippedStr := v.styles.boldSkipped.Render(fmt.Sprintf("%s %d", IconCharSkipped, skipped))

	// Build header based on running state
	var header string
	if v.running {
		// Running: show animated spinner for running count
		frame := v.animFrame % len(SpinnerFrames)
		colorIdx := v.animFrame % len(SpinnerColors)
		spinnerStyle := lipgloss.NewStyle().Foreground(SpinnerColors[colorIdx])
		runningStr := spinnerStyle.Render(fmt.Sprintf("%s %d", SpinnerFrames[frame], running))
		elapsedStr := v.styles.elapsed.Render(fmt.Sprintf("(%s)", time.Duration(elapsed*float64(time.Second)).Round(time.Millisecond*100)))

		header = statusIndicator + " " + v.styles.header.Render("GOWT") + "  " +
			passedStr + "  " + failedStr + "  " + skippedStr + "  " + runningStr + "  " + elapsedStr
	} else {
		// Done: hide running count, show "Done (time)"
		doneStr := v.styles.passed.Render(fmt.Sprintf("Done (%s)", time.Duration(elapsed*float64(time.Second)).Round(time.Millisecond*100)))

		header = statusIndicator + " " + v.styles.header.Render("GOWT") + "  " +
			passedStr + "  " + failedStr + "  " + skippedStr + "  " + doneStr
	}

	return header
}

func (v TreeView) renderHelpBar() string {
	filterText := fmt.Sprintf("[Space %s]", v.filter)
	help := filterText + "  [Arrows Navigate]  [↵ Logs]  [r Rerun]  [? Help]  [q Quit]"
	helpRendered := v.styles.helpBar.Render(help)

	// Use pre-computed help bar width based on filter mode
	var helpWidth int
	if v.filter == FilterFocus {
		helpWidth = v.styles.helpBarWidthFocus
	} else {
		helpWidth = v.styles.helpBarWidthAll
	}

	// Add scroll info (similar to LogView)
	scrollInfo := ""
	nodes := v.getVisibleNodes()
	if len(nodes) > 0 {
		visibleRows := v.height - 4
		if visibleRows < 1 {
			visibleRows = 10
		}

		totalNodes := len(nodes)
		currentLine := v.cursor + 1

		// Calculate scroll percentage
		var percent float64
		if totalNodes <= visibleRows {
			percent = 100.0
		} else {
			maxScroll := totalNodes - visibleRows
			if maxScroll > 0 {
				percent = float64(v.scrollTop) / float64(maxScroll) * 100.0
			}
			// At bottom means 100%
			if v.cursor == totalNodes-1 {
				percent = 100.0
			}
		}

		scrollInfo = fmt.Sprintf("─ %3.0f%% ─ %d/%d", percent, currentLine, totalNodes)
	}

	scrollLen := len(scrollInfo)
	padding := v.width - helpWidth - scrollLen
	if padding < 1 {
		padding = 1
	}

	return helpRendered + strings.Repeat(" ", padding) + v.styles.helpBar.Render(scrollInfo)
}

// computeScrollTop returns the updated scrollTop value without modifying receiver
func (v TreeView) computeScrollTop() int {
	visibleRows := v.height - 4
	if visibleRows < 1 {
		visibleRows = 10
	}

	scrollTop := v.scrollTop

	// Only scroll when cursor goes off-screen
	if v.cursor < scrollTop {
		scrollTop = v.cursor
	}
	if v.cursor >= scrollTop+visibleRows {
		scrollTop = v.cursor - visibleRows + 1
	}

	// Clamp to valid range (use cached nodes)
	maxScrollTop := max(0, len(v.cachedNodes)-visibleRows)
	if scrollTop > maxScrollTop {
		scrollTop = maxScrollTop
	}
	if scrollTop < 0 {
		scrollTop = 0
	}

	return scrollTop
}

func (v TreeView) renderTree() string {
	// Use cached nodes (refreshed at start of View())
	nodes := v.cachedNodes
	if len(nodes) == 0 {
		return v.styles.pending.Render("No tests to display")
	}

	visibleRows := v.height - 4
	if visibleRows < 1 {
		visibleRows = 10
	}

	var lines []string
	endIdx := min(v.scrollTop+visibleRows, len(nodes))

	for i := v.scrollTop; i < endIdx; i++ {
		node := nodes[i]
		isSelected := i == v.cursor
		lines = append(lines, v.renderNode(node, isSelected))
	}

	return strings.Join(lines, "\n")
}

// getRenderedName returns the cached styled name for packages.
// For tests or selected rows, returns plain name (no caching needed).
// For truncated names, returns freshly styled truncated name.
func (v TreeView) getRenderedName(node *model.TestNode, selected bool, displayName string) string {
	// Selected rows use plain name (selection style applied separately)
	if selected {
		return displayName
	}
	// Tests don't need package styling
	if node.Parent != nil {
		return displayName
	}
	// Package with truncation: render fresh (can't use cache)
	if displayName != node.Name {
		return v.styles.packageName.Render(displayName)
	}
	// Package without truncation: use cached rendered name
	if node.RenderedName == "" {
		node.RenderedName = v.styles.packageName.Render(node.Name)
	}
	return node.RenderedName
}

// getRenderedSuffix returns cached suffix (stats + progress + elapsed).
// Rebuilds cache if invalid or terminal width changed.
func (v TreeView) getRenderedSuffix(node *model.TestNode) string {
	// Check cache validity
	if node.SuffixCacheValid && node.SuffixCacheWidth == v.width {
		return node.RenderedSuffix
	}

	// Rebuild suffix
	var suffix string

	// Stats + progress bar (only for nodes with children)
	if node.HasChildren() {
		passed, failed, skipped, total := node.CountByStatus()
		doneCount := passed + failed + skipped
		suffix = v.styles.elapsed.Render(" " + strconv.Itoa(doneCount) + "/" + strconv.Itoa(total))
		if total > 0 {
			suffix += " " + v.renderProgressBar(passed, failed, skipped, total, 20)
		}
	}

	// Elapsed time
	if node.Elapsed > 0 {
		suffix += v.styles.elapsed.Render(" " + time.Duration(node.Elapsed*float64(time.Second)).Round(time.Millisecond*10).String())
	}

	// Cache result
	node.RenderedSuffix = suffix
	node.SuffixCacheValid = true
	node.SuffixCacheWidth = v.width

	return node.RenderedSuffix
}

func (v TreeView) renderNode(node *model.TestNode, selected bool) string {
	depth := node.Depth           // Use cached depth (O(1))
	availableWidth := v.width - 3 // Reserve 3 for "..."

	// Get cached suffix first (we need its width for truncation calculation)
	suffix := v.getRenderedSuffix(node)

	// Calculate widths of plain text components BEFORE styling
	indentWidth := depth * 4
	coreFixedWidth := 1 + 1 + 1 + 2 // space + chevron + space + icon(2 chars with space)
	nameWidth := node.NameWidth

	// Calculate suffix width for truncation check
	// Suffix contains: stats (" 106/140") + progress bar (21 chars) + elapsed (" 4.00s")
	// We estimate based on node properties to avoid measuring styled string
	suffixWidth := 0
	if node.HasChildren() {
		passed, failed, skipped, total := node.CountByStatus()
		doneCount := passed + failed + skipped
		suffixWidth = 2 + numDigits(doneCount) + numDigits(total) // " " + digits + "/" + digits
		if total > 0 {
			suffixWidth += 1 + 20 // " " + progress bar (20 chars)
		}
	}
	if node.Elapsed > 0 {
		suffixWidth += 1 + 9 // " " + elapsed time (e.g., "10h30m0s")
	}

	// Total width calculation
	totalWidth := indentWidth + coreFixedWidth + nameWidth + suffixWidth

	// Check if truncation is needed
	needsTruncation := availableWidth > 0 && totalWidth > availableWidth

	// Calculate display name (possibly truncated)
	var displayName string
	if needsTruncation {
		fixedWidth := indentWidth + coreFixedWidth + suffixWidth
		availableForName := availableWidth - fixedWidth
		if availableForName < 3 {
			availableForName = 3
		}
		displayName = truncatePlainText(node.Name, availableForName)
	} else {
		displayName = node.Name
	}

	// Build the styled string
	indent := getIndent(depth)

	// Expand indicator (always fresh - changes on user toggle)
	expandIndicator := " "
	if node.HasChildren() {
		if node.Expanded {
			expandIndicator = "▼"
		} else {
			expandIndicator = "▶"
		}
	}

	// Status icon (always fresh - spinner animates)
	var icon string
	if selected {
		icon = v.getStatusIconRaw(node)
	} else {
		icon = v.renderStatusIcon(node)
	}

	// Name - use cache for non-truncated packages
	styledName := v.getRenderedName(node, selected, displayName)

	// Core content: space + chevron + space + icon + name
	coreContent := " " + expandIndicator + " " + icon + styledName

	// Add truncation indicator if needed
	if needsTruncation {
		suffix += "..."
	}

	// Apply selection style (only on core content)
	if selected {
		return indent + v.styles.selectedRow.Render(coreContent) + suffix
	}

	return indent + coreContent + suffix
}

// numDigits returns the number of digits in a non-negative integer (fast path for small numbers)
func numDigits(n int) int {
	if n < 10 {
		return 1
	}
	if n < 100 {
		return 2
	}
	if n < 1000 {
		return 3
	}
	if n < 10000 {
		return 4
	}
	// Fallback for larger numbers
	return len(strconv.Itoa(n))
}

// truncatePlainText truncates a plain text string (no ANSI codes) to a visual width.
// Uses runewidth for accurate width calculation of Unicode characters.
// This is O(n) with minimal allocations using strings.Builder.
func truncatePlainText(s string, maxWidth int) string {
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}

	var sb strings.Builder
	sb.Grow(maxWidth + 4) // Pre-allocate approximate size
	currentWidth := 0

	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if currentWidth+rw > maxWidth {
			break
		}
		sb.WriteRune(r)
		currentWidth += rw
	}

	return sb.String()
}

func (v TreeView) renderStatusIcon(node *model.TestNode) string {
	switch node.Status {
	case model.StatusPassed:
		if node.Cached {
			return IconCached // Pre-rendered "↯ " with color
		}
		return IconPassed // Pre-rendered "✓ " with color
	case model.StatusFailed:
		return IconFailed // Pre-rendered "✗ " with color
	case model.StatusSkipped:
		return IconSkipped // Pre-rendered "⊘ " with color
	case model.StatusRunning:
		return GetSpinnerIcon(v.animFrame) // Pre-rendered spinner with color cycling
	case model.StatusPending:
		return IconPending // Pre-rendered "○ " with color
	default:
		return "? "
	}
}

// getStatusIconRaw returns the raw icon character without ANSI styling (for use in inverted selection)
func (v TreeView) getStatusIconRaw(node *model.TestNode) string {
	switch node.Status {
	case model.StatusPassed:
		if node.Cached {
			return IconCachedRaw
		}
		return IconPassedRaw
	case model.StatusFailed:
		return IconFailedRaw
	case model.StatusSkipped:
		return IconSkippedRaw
	case model.StatusRunning:
		return GetSpinnerIconRaw(v.animFrame)
	case model.StatusPending:
		return IconPendingRaw
	default:
		return "? "
	}
}

func (v TreeView) renderProgressBar(passed, failed, skipped, total, width int) string {
	if total == 0 {
		return v.styles.barRemaining[width] // Pre-rendered empty bar
	}

	passedW := (passed * width) / total
	failedW := (failed * width) / total
	skippedW := (skipped * width) / total
	remaining := width - passedW - failedW - skippedW

	// Use pre-rendered bar segments - no Repeat() or Render() calls
	return v.styles.barPassed[passedW] +
		v.styles.barFailed[failedW] +
		v.styles.barSkipped[skippedW] +
		v.styles.barRemaining[remaining]
}

// GetSelectedNode returns the currently selected node
func (v TreeView) GetSelectedNode() *model.TestNode {
	nodes := v.getVisibleNodes()
	if v.cursor < len(nodes) {
		return nodes[v.cursor]
	}
	return nil
}

// GetTree returns the underlying test tree
func (v TreeView) GetTree() *model.TestTree {
	return v.tree
}
