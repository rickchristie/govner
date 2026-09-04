package model

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	util "github.com/rickchristie/govner/gowt/util"
)

// TestStatus represents the status of a test
type TestStatus string

const (
	StatusPending TestStatus = "pending"
	StatusRunning TestStatus = "running"
	StatusPassed  TestStatus = "pass"
	StatusFailed  TestStatus = "fail"
	StatusSkipped TestStatus = "skip"
)

// TestEvent represents a single test event from go test -json output
type TestEvent struct {
	Time       time.Time `json:"Time"`
	Action     string    `json:"Action"`
	Package    string    `json:"Package"`
	ImportPath string    `json:"ImportPath"` // Used for build errors
	Test       string    `json:"Test"`
	Elapsed    float64   `json:"Elapsed"`
	Output     string    `json:"Output"`
}

// TestNode represents a node in the test tree (package, subtest, or test)
type TestNode struct {
	Name         string      // Short name (e.g., "TestFoo" or "subtest1")
	FullPath     string      // Full path (e.g., "pkg/foo/TestFoo/subtest1")
	Package      string      // Package path
	Status       TestStatus  // Aggregate status shown in the tree
	Elapsed      float64     // Duration in seconds
	RawLog       *NodeLog    // Raw log output refs (points to shared RawLogBuffer)
	ProcessedLog *NodeLog    // Processed log refs (filtered & styled, points to ProcessedLogBuffer)
	Children     []*TestNode // Child tests/subtests
	Parent       *TestNode   // Parent node (nil for root packages)
	Expanded     bool        // UI state: is this node expanded
	Cached       bool        // Whether this result is from cache
	Depth        int         // Cached depth in tree (0 for packages, 1+ for tests/subtests)
	NameWidth    int         // Cached runewidth of Name (0 = not computed yet)

	// eventStatus is the status from this node's own test2json event. Status can
	// also become failed because a child failed. Keep these values separate so a
	// child result cannot hide this node's running state or corrupt its counters.
	eventStatus TestStatus

	// Aggregated counts (includes self + all descendants)
	PassedCount  int // Count of passed tests
	FailedCount  int // Count of failed tests
	SkippedCount int // Count of skipped tests
	RunningCount int // Count of running tests
	CachedCount  int // Count of cached tests
	TotalCount   int // Total test count (excludes packages)
}

// TestTree holds the entire test hierarchy
type TestTree struct {
	Packages           map[string]*TestNode // Top-level packages
	NodeIndex          map[string]*TestNode // Index for O(1) lookup by FullPath
	Elapsed            float64              // Total elapsed time
	RawLogBuffer       *LogBuffer           // Shared buffer for raw log output
	ProcessedLogBuffer *LogBuffer           // Shared buffer for processed log output (filtered & styled)

	// Output line buffer: go test -json can split long lines across multiple Output events.
	// We buffer incomplete lines (no trailing newline) until complete.
	// Key is node FullPath, value is the partial line being accumulated.
	OutputLineBuffer map[string]string

	// Global aggregated counts (sum of all packages)
	PassedCount  int // Count of passed tests
	FailedCount  int // Count of failed tests
	SkippedCount int // Count of skipped tests
	RunningCount int // Count of running tests
	CachedCount  int // Count of cached tests
	TotalCount   int // Total test count
}

// NewTestTree creates a new empty test tree
func NewTestTree() *TestTree {
	return &TestTree{
		Packages:           make(map[string]*TestNode),
		NodeIndex:          make(map[string]*TestNode),
		RawLogBuffer:       NewLogBuffer(),
		ProcessedLogBuffer: NewLogBuffer(),
		OutputLineBuffer:   make(map[string]string),
	}
}

// GetNode returns a node by its full path in O(1) time
func (t *TestTree) GetNode(fullPath string) *TestNode {
	return t.NodeIndex[fullPath]
}

// ProcessEvent updates the tree based on a test event.
// Returns true if the event changed tree visibility (status, counts, icons).
// Returns false for log-only events that don't affect the display.
func (t *TestTree) ProcessEvent(event TestEvent) bool {
	// Use ImportPath if Package is empty (for build errors)
	pkgPath := event.Package
	if pkgPath == "" {
		pkgPath = event.ImportPath
	}

	// Get or create package node. Output is normally preceded by a start or run
	// event, but build tools and alternative runners can emit output first.
	// Creating a node changes tree visibility even when its event is log-only.
	_, packageExisted := t.Packages[pkgPath]
	pkgNode := t.getOrCreatePackage(pkgPath)
	if pkgNode == nil {
		return false // Skip events with empty package
	}
	packageCreated := !packageExisted

	// Handle build-specific events
	switch event.Action {
	case "build-output":
		t.appendOutput(pkgNode, event.Output)
		return packageCreated
	case "build-fail":
		t.flushOutput(pkgNode)
		if pkgNode.eventStatus != StatusFailed {
			// A build failure has no test node, but it must still appear in the
			// global failure total. Repeated build-fail events are one result.
			t.propagateCountDelta(pkgNode, 1, "failed")
		}
		pkgNode.eventStatus = StatusFailed
		t.refreshStatus(pkgNode)
		return true // Status change
	}

	// Package-level event (no test name)
	if event.Test == "" {
		changed := t.handlePackageEvent(pkgNode, event)
		return packageCreated || changed
	}

	// Test-level event
	testPath := pkgPath + "/" + event.Test
	_, testExisted := t.NodeIndex[testPath]
	testNode := t.getOrCreateTest(pkgNode, event.Test)
	if testNode == nil {
		return false // Skip invalid test names
	}
	changed := t.handleTestEvent(testNode, event)
	return packageCreated || !testExisted || changed
}

func (t *TestTree) getOrCreatePackage(pkgPath string) *TestNode {
	// Skip empty package paths
	if pkgPath == "" {
		return nil
	}

	if node, exists := t.Packages[pkgPath]; exists {
		return node
	}

	shortName := shortPackageName(pkgPath)
	node := &TestNode{
		Name:        shortName,
		NameWidth:   runewidth.StringWidth(shortName),
		FullPath:    pkgPath,
		Package:     pkgPath,
		Status:      StatusPending,
		eventStatus: StatusPending,
		Expanded:    false, // Packages start collapsed for stable view during test runs
		Children:    make([]*TestNode, 0),
		Depth:       0, // Package nodes are at root level
	}
	t.Packages[pkgPath] = node
	t.NodeIndex[pkgPath] = node // Add to index for O(1) lookup
	return node
}

func (t *TestTree) getOrCreateTest(pkgNode *TestNode, testName string) *TestNode {
	// The go test event stream is authoritative. Besides Test*, it legitimately
	// emits Benchmark*, Fuzz*, Example*, and custom synthetic names.
	if testName == "" {
		return nil
	}

	// Handle subtests: TestFoo/subtest1/subtest2
	parts := strings.Split(testName, "/")

	current := pkgNode
	for i, part := range parts {
		fullPath := pkgNode.Package + "/" + strings.Join(parts[:i+1], "/")
		child := findChild(current, part)
		if child == nil {
			child = &TestNode{
				Name:        part,
				NameWidth:   runewidth.StringWidth(part),
				FullPath:    fullPath,
				Package:     pkgNode.Package,
				Status:      StatusPending,
				eventStatus: StatusPending,
				Parent:      current,
				Children:    make([]*TestNode, 0),
				Expanded:    false,
				Depth:       i + 1, // Depth relative to package (TestFoo=1, TestFoo/sub=2, etc.)
			}
			current.Children = append(current.Children, child)
			t.NodeIndex[fullPath] = child // Add to index for O(1) lookup
			// Propagate TotalCount to node and all ancestors
			t.propagateCountDelta(child, 1, "total")
		}
		current = child
	}
	return current
}

func findChild(parent *TestNode, name string) *TestNode {
	for _, child := range parent.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}

func (t *TestTree) handlePackageEvent(node *TestNode, event TestEvent) bool {
	switch event.Action {
	case "start":
		node.eventStatus = StatusRunning
		t.refreshStatus(node)
		return true
	case "pass":
		t.flushOutput(node)
		t.finalizeUnfinishedTests(node, StatusPassed)
		node.eventStatus = StatusPassed
		node.Elapsed = event.Elapsed
		t.refreshStatus(node)
		return true
	case "fail":
		t.flushOutput(node)
		t.finalizeUnfinishedTests(node, StatusFailed)
		node.eventStatus = StatusFailed
		node.Elapsed = event.Elapsed
		t.refreshStatus(node)
		return true
	case "skip":
		t.flushOutput(node)
		t.finalizeUnfinishedTests(node, StatusSkipped)
		node.eventStatus = StatusSkipped
		t.refreshStatus(node)
		return true
	case "output":
		t.appendOutput(node, event.Output)
		// Detect cached package: format is "ok  \tpackage/path\t(cached)\n"
		// Use strict matching to avoid false positives from log output
		if isCachedOutput(event.Output, node.Package) {
			t.markCached(node)
			return true // Cached icon change
		}
		return false // Log-only, no visual change
	}
	return false
}

// finalizeUnfinishedTests reconciles named events when test2json terminates a
// workload only at package scope. Benchmarks are the common case: some Go
// versions emit run and output records but no test-level pass or bench record.
// A terminal package event is authoritative, so no descendant may remain in a
// pending or running state after it arrives.
func (t *TestTree) finalizeUnfinishedTests(node *TestNode, status TestStatus) {
	for _, child := range node.Children {
		t.finalizeUnfinishedTests(child, status)
		if child.eventStatus == StatusPending || child.eventStatus == StatusRunning {
			t.flushOutput(child)
			t.finishTest(child, status, child.Elapsed)
		}
	}
}

// isCachedOutput detects Go's cached test output format.
// Format: "ok  \tpackage/path\t(cached)\n"
// Uses strict matching to avoid false positives from user log output.
func isCachedOutput(output, pkg string) bool {
	fields := strings.Split(strings.TrimSpace(output), "\t")
	return len(fields) == 3 && strings.TrimSpace(fields[0]) == "ok" &&
		strings.TrimSpace(fields[1]) == pkg && strings.TrimSpace(fields[2]) == "(cached)"
}

// markCached marks a node and all its children as cached. The canonical cache
// summary is output text, not a result event, so it must not change Status.
// Go emits explicit pass events for cached tests and the package.
// Uses TotalCount to set CachedCount in O(1) instead of propagating per-node.
func (t *TestTree) markCached(node *TestNode) {
	if node.CachedCount > 0 {
		return // Already marked as cached
	}

	// Set CachedCount = TotalCount for the package (all tests are cached)
	cachedCount := node.TotalCount
	node.CachedCount = cachedCount
	t.CachedCount += cachedCount

	// Mark cache metadata only. Result actions remain the sole status authority.
	node.Cached = true
	for _, child := range node.Children {
		markChildCachedFlag(child)
	}
}

// markChildCachedFlag recursively sets cache metadata on descendants. It does
// not propagate because the parent's CachedCount already includes its subtree.
func markChildCachedFlag(node *TestNode) {
	node.Cached = true
	node.CachedCount = node.TotalCount
	for _, child := range node.Children {
		markChildCachedFlag(child)
	}
}

// ComputeAllStats returns the pre-computed global stats (O(1) operation)
// Stats are updated incrementally as events are processed
func (t *TestTree) ComputeAllStats() (passed, failed, skipped, running, cached int) {
	return t.PassedCount, t.FailedCount, t.SkippedCount, t.RunningCount, t.CachedCount
}

func (t *TestTree) handleTestEvent(node *TestNode, event TestEvent) bool {
	switch event.Action {
	case "run":
		return t.transitionTest(node, StatusRunning)
	case "pause":
		return t.transitionTest(node, StatusPending)
	case "cont":
		return t.transitionTest(node, StatusRunning)
	case "pass", "bench":
		t.flushOutput(node)
		return t.finishTest(node, StatusPassed, event.Elapsed)
	case "fail":
		t.flushOutput(node)
		return t.finishTest(node, StatusFailed, event.Elapsed)
	case "skip":
		t.flushOutput(node)
		return t.finishTest(node, StatusSkipped, event.Elapsed)
	case "output":
		t.appendOutput(node, event.Output)
		return false // Log-only, no visual change
	}
	return false
}

// finishTest applies one terminal transition without double-counting repeated
// or corrected terminal records. test2json normally emits one result, but the
// package-level reconciliation path can race a late explicit record in loaded
// or synthetic streams, so counters must remain internally consistent.
func (t *TestTree) finishTest(node *TestNode, status TestStatus, elapsed float64) bool {
	changed := t.transitionTest(node, status)
	elapsedChanged := node.Elapsed != elapsed
	node.Elapsed = elapsed
	return changed || elapsedChanged
}

// transitionTest applies a direct test2json state transition. Status is an
// aggregate presentation value, so only eventStatus can identify the counter
// that belongs to this node.
func (t *TestTree) transitionTest(node *TestNode, status TestStatus) bool {
	previous := node.eventStatus
	if previous == status {
		return false
	}

	t.changeTestCount(node, previous, -1)
	node.eventStatus = status
	t.changeTestCount(node, status, 1)
	t.refreshStatus(node)
	return true
}

func (t *TestTree) changeTestCount(node *TestNode, status TestStatus, delta int) {
	switch status {
	case StatusPassed:
		t.propagateCountDelta(node, delta, "passed")
	case StatusFailed:
		t.propagateCountDelta(node, delta, "failed")
	case StatusSkipped:
		t.propagateCountDelta(node, delta, "skipped")
	case StatusRunning:
		t.propagateCountDelta(node, delta, "running")
	}
}

// Styles for processed log output
var (
	logStylePassed  = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	logStyleFailed  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	logStyleSkipped = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	logStyleBold    = lipgloss.NewStyle().Bold(true)
	logStyleDim     = lipgloss.NewStyle().Faint(true)
)

const (
	iconPassed  = "✓"
	iconFailed  = "✗"
	iconSkipped = "⊘"
)

// stripAnsi removes ANSI escape sequences from a string
func stripAnsi(s string) string {
	return util.StripANSI(s)
}

// processOutput transforms raw test output for display:
// - Strips ANSI codes from raw output (prevents bleeding from test frameworks)
// - Skips === RUN/PAUSE/CONT markers
// - Styles --- PASS/FAIL/SKIP lines with colored icon, bold name, dim duration
// - Formats JSON lines with syntax highlighting
func processOutput(output string) string {
	cleaned := stripAnsi(output)
	trimmed := strings.TrimSpace(cleaned)

	if strings.HasPrefix(trimmed, "=== RUN") ||
		strings.HasPrefix(trimmed, "=== PAUSE") ||
		strings.HasPrefix(trimmed, "=== CONT") {
		return ""
	}

	if strings.HasPrefix(trimmed, "--- PASS:") {
		return formatTestResult(trimmed, "--- PASS:", logStylePassed, iconPassed)
	}
	if strings.HasPrefix(trimmed, "--- FAIL:") {
		return formatTestResult(trimmed, "--- FAIL:", logStyleFailed, iconFailed)
	}
	if strings.HasPrefix(trimmed, "--- SKIP:") {
		return formatTestResult(trimmed, "--- SKIP:", logStyleSkipped, iconSkipped)
	}

	// Try to format as JSON (quick bail-out for non-JSON)
	if formatted := util.TryFormatJSON(trimmed); formatted != "" {
		return formatted
	}

	return cleaned
}

// formatTestResult transforms "--- STATUS: TestName (duration)" to styled output
// For long hierarchical test names (with /), displays the last 2 levels on separate lines
func formatTestResult(line, prefix string, iconStyle lipgloss.Style, icon string) string {
	rest := strings.TrimPrefix(line, prefix)
	rest = strings.TrimSpace(rest)

	testName := rest
	duration := ""
	if idx := strings.LastIndex(rest, " ("); idx != -1 && strings.HasSuffix(rest, ")") {
		testName = rest[:idx]
		duration = rest[idx:]
	}

	var result strings.Builder

	// Split test name by "/" to format hierarchically
	parts := strings.Split(testName, "/")
	if len(parts) >= 3 {
		// 3+ parts: show prefix on first line, last 2 parts indented
		prefixParts := strings.Join(parts[:len(parts)-2], "/") + "/"
		secondLast := parts[len(parts)-2] + "/"
		last := parts[len(parts)-1]

		result.WriteString(iconStyle.Render(icon))
		result.WriteString(" ")
		result.WriteString(logStyleBold.Render(prefixParts))
		result.WriteString("\n")
		result.WriteString("      ")
		result.WriteString(logStyleBold.Render(secondLast))
		result.WriteString("\n")
		result.WriteString("        ")
		result.WriteString(logStyleBold.Render(last))
		if duration != "" {
			result.WriteString(logStyleDim.Render(duration))
		}
	} else if len(parts) == 2 {
		// 2 parts: show first on first line, second indented
		result.WriteString(iconStyle.Render(icon))
		result.WriteString(" ")
		result.WriteString(logStyleBold.Render(parts[0] + "/"))
		result.WriteString("\n")
		result.WriteString("      ")
		result.WriteString(logStyleBold.Render(parts[1]))
		if duration != "" {
			result.WriteString(logStyleDim.Render(duration))
		}
	} else {
		// Single part: show as-is
		result.WriteString(iconStyle.Render(icon))
		result.WriteString(" ")
		result.WriteString(logStyleBold.Render(testName))
		if duration != "" {
			result.WriteString(logStyleDim.Render(duration))
		}
	}
	result.WriteString("\n\n")

	return result.String()
}

// appendOutput appends output to a node and all its ancestors.
// Handles line reassembly: go test -json can split long lines across multiple Output events.
func (t *TestTree) appendOutput(node *TestNode, output string) {
	// Prepend any buffered partial line from previous chunks
	if buffered, ok := t.OutputLineBuffer[node.FullPath]; ok {
		output = buffered + output
		delete(t.OutputLineBuffer, node.FullPath)
	}

	// Check if output ends with newline (complete line) or not (partial)
	endsWithNewline := len(output) > 0 && output[len(output)-1] == '\n'

	// Split into lines. If doesn't end with newline, last element is incomplete.
	lines := strings.Split(output, "\n")

	// If output doesn't end with newline, buffer the last incomplete part
	if !endsWithNewline && len(lines) > 0 {
		lastIdx := len(lines) - 1
		if lines[lastIdx] != "" {
			t.OutputLineBuffer[node.FullPath] = lines[lastIdx]
		}
		lines = lines[:lastIdx] // Remove incomplete last element
	} else if endsWithNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		// Remove empty string from trailing newline split
		lines = lines[:len(lines)-1]
	}

	// Process each complete line, including empty lines. Blank output is part of
	// test diagnostics and must remain byte-for-byte present in Raw mode.
	for _, line := range lines {
		t.appendCompleteOutput(node, line+"\n")
	}
}

// FlushOutputBuffers commits final chunks that do not end in a newline. go
// test permits a process to exit after writing such a chunk, and hiding it is
// particularly harmful when it contains the only failure diagnostic.
func (t *TestTree) FlushOutputBuffers() {
	for fullPath, output := range t.OutputLineBuffer {
		delete(t.OutputLineBuffer, fullPath)
		if node := t.NodeIndex[fullPath]; node != nil {
			t.appendCompleteOutput(node, output)
		}
	}
}

func (t *TestTree) flushOutput(node *TestNode) {
	if node == nil {
		return
	}
	output, ok := t.OutputLineBuffer[node.FullPath]
	if !ok {
		return
	}
	delete(t.OutputLineBuffer, node.FullPath)
	t.appendCompleteOutput(node, output)
}

func (t *TestTree) appendCompleteOutput(node *TestNode, output string) {
	lineWithNewline := output

	// Raw means unformatted, not unsafe. Strip terminal control sequences so a
	// test cannot inject hyperlinks, clipboard commands, or cursor movement.
	rawRef := t.RawLogBuffer.Append(stripAnsi(lineWithNewline))

	// Add raw ref to this node
	if node.RawLog == nil {
		node.RawLog = NewNodeLog()
	}
	node.RawLog.Append(rawRef)

	// Process output for display (filter and style)
	processed := processOutput(lineWithNewline)
	var processedRef BufferRef
	if processed != "" {
		processedRef = t.ProcessedLogBuffer.Append(processed)

		// Add processed ref to this node
		if node.ProcessedLog == nil {
			node.ProcessedLog = NewNodeLog()
		}
		node.ProcessedLog.Append(processedRef)
	}

	// Add refs to package node (if this is a test node, not a package)
	if node.Parent != nil {
		pkg := t.Packages[node.Package]
		if pkg != nil {
			if pkg.RawLog == nil {
				pkg.RawLog = NewNodeLog()
			}
			pkg.RawLog.Append(rawRef)

			if processed != "" {
				if pkg.ProcessedLog == nil {
					pkg.ProcessedLog = NewNodeLog()
				}
				pkg.ProcessedLog.Append(processedRef)
			}
		}
	}

	// Add refs to all ancestor test nodes by walking FullPath
	testPath := strings.TrimPrefix(node.FullPath, node.Package)
	testPath = strings.TrimPrefix(testPath, "/")

	if testPath == "" {
		return // This is a package node, already handled
	}

	parts := strings.Split(testPath, "/")

	// Add ref to each ancestor (all prefixes except the full path itself)
	for i := 1; i < len(parts); i++ {
		ancestorTestPath := strings.Join(parts[:i], "/")
		ancestorFullPath := node.Package + "/" + ancestorTestPath

		ancestor := t.NodeIndex[ancestorFullPath]
		if ancestor != nil {
			if ancestor.RawLog == nil {
				ancestor.RawLog = NewNodeLog()
			}
			ancestor.RawLog.Append(rawRef)

			if processed != "" {
				if ancestor.ProcessedLog == nil {
					ancestor.ProcessedLog = NewNodeLog()
				}
				ancestor.ProcessedLog.Append(processedRef)
			}
		}
	}
}

// propagateCountDelta adds delta to a count field on node and all ancestors, plus tree global
func (t *TestTree) propagateCountDelta(node *TestNode, delta int, field string) {
	current := node
	for current != nil {
		switch field {
		case "passed":
			current.PassedCount += delta
		case "failed":
			current.FailedCount += delta
		case "skipped":
			current.SkippedCount += delta
		case "running":
			current.RunningCount += delta
		case "cached":
			current.CachedCount += delta
		case "total":
			current.TotalCount += delta
		}
		current = current.Parent
	}
	// Update tree global counts
	switch field {
	case "passed":
		t.PassedCount += delta
	case "failed":
		t.FailedCount += delta
	case "skipped":
		t.SkippedCount += delta
	case "running":
		t.RunningCount += delta
	case "cached":
		t.CachedCount += delta
	case "total":
		t.TotalCount += delta
	}
}

// refreshStatus recomputes aggregate presentation status from the changed node
// through the package root. Recalculation is necessary because a corrected
// child result must clear a failure that the child previously propagated.
func (t *TestTree) refreshStatus(node *TestNode) {
	for current := node; current != nil; current = current.Parent {
		current.Status = aggregateStatus(current)
	}
}

func aggregateStatus(node *TestNode) TestStatus {
	hasPassedChild := false
	hasSkippedChild := false
	hasRunningChild := false

	for _, child := range node.Children {
		switch child.Status {
		case StatusFailed:
			return StatusFailed
		case StatusRunning:
			hasRunningChild = true
		case StatusPassed:
			hasPassedChild = true
		case StatusSkipped:
			hasSkippedChild = true
		}
	}

	if node.eventStatus == StatusFailed {
		return StatusFailed
	}
	if node.eventStatus == StatusRunning || hasRunningChild {
		return StatusRunning
	}
	if node.eventStatus == StatusPassed {
		return StatusPassed
	}
	if node.eventStatus == StatusSkipped {
		return StatusSkipped
	}
	if hasPassedChild {
		return StatusPassed
	}
	if hasSkippedChild {
		return StatusSkipped
	}
	return StatusPending
}

// GetSortedPackages returns packages sorted by name
func (t *TestTree) GetSortedPackages() []*TestNode {
	packages := make([]*TestNode, 0, len(t.Packages))
	for _, pkg := range t.Packages {
		packages = append(packages, pkg)
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].FullPath < packages[j].FullPath
	})
	return packages
}

// Flatten returns a flat list of visible nodes for display
func (t *TestTree) Flatten() []*TestNode {
	var result []*TestNode
	for _, pkg := range t.GetSortedPackages() {
		// Skip packages with empty names
		if pkg.Name == "" {
			continue
		}
		result = append(result, FlattenNode(pkg, 0)...)
	}
	return result
}

// FlattenNode recursively flattens a node and its expanded children
func FlattenNode(node *TestNode, depth int) []*TestNode {
	result := []*TestNode{node}
	if node.Expanded {
		for _, child := range node.Children {
			result = append(result, FlattenNode(child, depth+1)...)
		}
	}
	return result
}

// GetDepth returns the cached depth of a node in the tree (O(1) operation).
// Depth is set when the node is created: 0 for packages, 1+ for tests/subtests.
func (n *TestNode) GetDepth() int {
	return n.Depth
}

// HasChildren returns true if the node has children
func (n *TestNode) HasChildren() bool {
	return len(n.Children) > 0
}

// IsLastChild returns true if this node is the last child of its parent
func (n *TestNode) IsLastChild() bool {
	if n.Parent == nil {
		return false
	}
	children := n.Parent.Children
	return len(children) > 0 && children[len(children)-1] == n
}

// HasExpandedSiblingBefore returns true if there's an expanded sibling before this node
func (n *TestNode) HasExpandedSiblingBefore() bool {
	if n.Parent == nil {
		return false
	}
	for _, sibling := range n.Parent.Children {
		if sibling == n {
			return false // reached ourselves
		}
		if sibling.Expanded {
			return true // found expanded sibling before us
		}
	}
	return false
}

// GetFullOutput returns all output lines concatenated from the shared buffer
func (n *TestNode) GetFullOutput(buffer *LogBuffer) string {
	if n.RawLog == nil || n.RawLog.IsEmpty() {
		return ""
	}
	var sb strings.Builder
	sb.Grow(n.RawLog.TotalSize())
	for _, ref := range n.RawLog.Refs {
		sb.Write(buffer.SliceBytes(ref))
	}
	return sb.String()
}

// CountByStatus returns pre-computed counts for this node's subtree (O(1) operation)
// Counts are updated incrementally as events are processed
func (n *TestNode) CountByStatus() (passed, failed, skipped, total int) {
	return n.PassedCount, n.FailedCount, n.SkippedCount, n.TotalCount
}

func shortPackageName(pkgPath string) string {
	return ShortPath(pkgPath)
}

// ShortPath strips the module prefix from a package path
// e.g., "github.com/example/accessor/asset" -> "accessor/asset"
// e.g., "github.com/example/lib/ssproc/TestFoo" -> "lib/ssproc/TestFoo"
func ShortPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) <= 1 {
		return path
	}

	if !strings.Contains(parts[0], ".") {
		return path
	}
	// A conventional hosted module is domain/owner/repository. For the module
	// root itself, the repository name is the useful label; otherwise retain the
	// package path below it.
	if len(parts) <= 3 {
		return parts[len(parts)-1]
	}
	return strings.Join(parts[3:], "/")
}
