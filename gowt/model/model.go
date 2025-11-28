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
	Status       TestStatus  // Current status
	Elapsed      float64     // Duration in seconds
	RawLog       *NodeLog    // Raw log output refs (points to shared RawLogBuffer)
	ProcessedLog *NodeLog    // Processed log refs (filtered & styled, points to ProcessedLogBuffer)
	Children     []*TestNode // Child tests/subtests
	Parent       *TestNode   // Parent node (nil for root packages)
	Expanded     bool        // UI state: is this node expanded
	Cached       bool        // Whether this result is from cache
	Depth        int         // Cached depth in tree (0 for packages, 1+ for tests/subtests)
	NameWidth    int         // Cached runewidth of Name (0 = not computed yet)

	// Render cache
	RenderedName     string // Styled package name (permanent, never changes)
	RenderedSuffix   string // Stats + progress + elapsed
	SuffixCacheValid bool   // Is RenderedSuffix valid?
	SuffixCacheWidth int    // Terminal width when suffix was cached

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

	// Get or create package node
	pkgNode := t.getOrCreatePackage(pkgPath)
	if pkgNode == nil {
		return false // Skip events with empty package
	}

	// Handle build-specific events
	switch event.Action {
	case "build-output":
		t.appendOutput(pkgNode, event.Output)
		return false // Log-only, no visual change
	case "build-fail":
		pkgNode.Status = StatusFailed
		return true // Status change
	}

	// Package-level event (no test name)
	if event.Test == "" {
		return t.handlePackageEvent(pkgNode, event)
	}

	// Test-level event
	testNode := t.getOrCreateTest(pkgNode, event.Test)
	if testNode == nil {
		return false // Skip invalid test names
	}
	return t.handleTestEvent(testNode, event)
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
		Name:      shortName,
		NameWidth: runewidth.StringWidth(shortName),
		FullPath:  pkgPath,
		Package:   pkgPath,
		Status:    StatusPending,
		Expanded:  false, // Packages start collapsed for stable view during test runs
		Children:  make([]*TestNode, 0),
		Depth:     0, // Package nodes are at root level
	}
	t.Packages[pkgPath] = node
	t.NodeIndex[pkgPath] = node // Add to index for O(1) lookup
	return node
}

func (t *TestTree) getOrCreateTest(pkgNode *TestNode, testName string) *TestNode {
	// Skip invalid test names (must start with "Test")
	if testName == "" || !strings.HasPrefix(testName, "Test") {
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
				Name:      part,
				NameWidth: runewidth.StringWidth(part),
				FullPath:  fullPath,
				Package:   pkgNode.Package,
				Status:    StatusPending,
				Parent:    current,
				Children:  make([]*TestNode, 0),
				Expanded:  false,
				Depth:     i + 1, // Depth relative to package (TestFoo=1, TestFoo/sub=2, etc.)
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
		node.Status = StatusRunning
		node.SuffixCacheValid = false
		return true
	case "pass":
		node.Status = StatusPassed
		node.Elapsed = event.Elapsed
		node.SuffixCacheValid = false
		return true
	case "fail":
		node.Status = StatusFailed
		node.Elapsed = event.Elapsed
		node.SuffixCacheValid = false
		return true
	case "skip":
		node.Status = StatusSkipped
		node.SuffixCacheValid = false
		return true
	case "output":
		t.appendOutput(node, event.Output)
		// Detect cached package: format is "ok  \tpackage/path\t(cached)\n"
		// Use strict matching to avoid false positives from log output
		if isCachedOutput(event.Output) {
			t.markCached(node)
			return true // Cached icon change
		}
		return false // Log-only, no visual change
	}
	return false
}

// isCachedOutput detects Go's cached test output format.
// Format: "ok  \tpackage/path\t(cached)\n"
// Uses strict matching to avoid false positives from user log output.
func isCachedOutput(output string) bool {
	trimmed := strings.TrimSpace(output)
	return strings.HasPrefix(trimmed, "ok") && strings.HasSuffix(trimmed, "(cached)")
}

// markCached marks a node and all its children as cached
// Uses TotalCount to set CachedCount in O(1) instead of propagating per-node
func (t *TestTree) markCached(node *TestNode) {
	if node.CachedCount > 0 {
		return // Already marked as cached
	}

	// Set CachedCount = TotalCount for the package (all tests are cached)
	cachedCount := node.TotalCount
	node.CachedCount = cachedCount
	t.CachedCount += cachedCount

	// Mark individual nodes as Cached=true and Passed for UI display
	// Cached tests are always passing tests (Go only caches passing results)
	node.Cached = true
	node.Status = StatusPassed
	node.SuffixCacheValid = false // Invalidate render cache
	for _, child := range node.Children {
		markChildCachedFlag(child)
	}
}

// markChildCachedFlag recursively sets Cached=true flag, Status=Passed, and CachedCount on nodes
// Does NOT propagate - parent's CachedCount is already set via TotalCount
func markChildCachedFlag(node *TestNode) {
	node.Cached = true
	node.Status = StatusPassed         // Cached tests are always passing
	node.CachedCount = node.TotalCount // This node's subtree is all cached
	node.SuffixCacheValid = false      // Invalidate render cache
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
	prevStatus := node.Status

	switch event.Action {
	case "run":
		node.Status = StatusRunning
		node.SuffixCacheValid = false
		// Pending -> Running: increment running count
		if prevStatus != StatusRunning {
			t.propagateCountDelta(node, 1, "running")
		}
		return true
	case "pause":
		node.Status = StatusPending
		node.SuffixCacheValid = false
		// Running -> Pending: decrement running count
		if prevStatus == StatusRunning {
			t.propagateCountDelta(node, -1, "running")
		}
		return true
	case "cont":
		node.Status = StatusRunning
		node.SuffixCacheValid = false
		// Pending -> Running: increment running count
		if prevStatus != StatusRunning {
			t.propagateCountDelta(node, 1, "running")
		}
		return true
	case "pass":
		node.Status = StatusPassed
		node.Elapsed = event.Elapsed
		node.SuffixCacheValid = false
		// Decrement running if was running, increment passed
		if prevStatus == StatusRunning {
			t.propagateCountDelta(node, -1, "running")
		}
		t.propagateCountDelta(node, 1, "passed")
		t.propagateStatus(node)
		return true
	case "fail":
		node.Status = StatusFailed
		node.Elapsed = event.Elapsed
		node.SuffixCacheValid = false
		// Decrement running if was running, increment failed
		if prevStatus == StatusRunning {
			t.propagateCountDelta(node, -1, "running")
		}
		t.propagateCountDelta(node, 1, "failed")
		t.propagateStatus(node)
		return true
	case "skip":
		node.Status = StatusSkipped
		node.Elapsed = event.Elapsed
		node.SuffixCacheValid = false
		// Decrement running if was running, increment skipped
		if prevStatus == StatusRunning {
			t.propagateCountDelta(node, -1, "running")
		}
		t.propagateCountDelta(node, 1, "skipped")
		t.propagateStatus(node)
		return true
	case "output":
		t.appendOutput(node, event.Output)
		return false // Log-only, no visual change
	}
	return false
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
	var result strings.Builder
	inEscape := false

	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}

	return result.String()
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

	// Process each complete line
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Add newline back for raw output (preserves original format)
		lineWithNewline := line + "\n"

		// Append raw output to shared buffer
		rawRef := t.RawLogBuffer.Append(lineWithNewline)

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
			continue // This is a package node, already handled
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
}

// propagateCountDelta adds delta to a count field on node and all ancestors, plus tree global
func (t *TestTree) propagateCountDelta(node *TestNode, delta int, field string) {
	current := node
	for current != nil {
		current.SuffixCacheValid = false // Invalidate render cache for count changes
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

// propagateStatus updates parent status based on children
// Only propagates "bad" statuses (Failed, Running) upward - parents get Passed
// status when they receive their own "pass" event, not from children
func (t *TestTree) propagateStatus(node *TestNode) {
	parent := node.Parent
	for parent != nil {
		// Find worst status among children
		worstStatus := StatusPassed
		for _, child := range parent.Children {
			switch child.Status {
			case StatusFailed:
				worstStatus = StatusFailed
			case StatusRunning:
				if worstStatus != StatusFailed {
					worstStatus = StatusRunning
				}
			}
		}
		// Only propagate "bad" statuses (Failed, Running)
		// Don't set parent to Passed - that happens when parent's own "pass" event arrives
		// This preserves parent's Running status so count decrements work correctly
		if worstStatus == StatusFailed || worstStatus == StatusRunning {
			parent.Status = worstStatus
		}
		parent = parent.Parent
	}
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

	// Find where the module prefix ends
	// Module prefix typically starts with a domain (contains a dot)
	// and includes org/repo segments (e.g., "github.com/example")
	moduleEndIdx := 0

	// If first segment contains a dot, it's a domain - find where module ends
	if strings.Contains(parts[0], ".") {
		// Skip domain + org + repo segments (typically 3 total)
		// But also handle shorter modules like "github.com/user/repo"
		moduleEndIdx = 3
		if moduleEndIdx > len(parts) {
			moduleEndIdx = len(parts)
		}

		// Adjust: keep skipping if next segment looks like part of module path
		// (short segments without underscores that aren't common package names)
		for moduleEndIdx < len(parts) {
			segment := parts[moduleEndIdx]
			// Stop at common Go package directory names
			if isPackageDir(segment) {
				break
			}
			// Stop at Test names
			if strings.HasPrefix(segment, "Test") {
				break
			}
			moduleEndIdx++
		}
	}

	if moduleEndIdx >= len(parts) {
		// Fallback: return last 2 parts
		if len(parts) >= 2 {
			return strings.Join(parts[len(parts)-2:], "/")
		}
		return path
	}

	return strings.Join(parts[moduleEndIdx:], "/")
}

// isPackageDir returns true if the segment looks like a Go package directory
func isPackageDir(segment string) bool {
	// Common Go package directory patterns
	commonDirs := []string{
		"accessor", "service", "pservice", "lib", "pkg", "internal",
		"cmd", "api", "model", "data", "view", "controller", "handler",
		"middleware", "util", "utils", "helper", "helpers", "config",
		"test", "tests", "mock", "mocks", "gen", "generated",
	}
	segLower := strings.ToLower(segment)
	for _, dir := range commonDirs {
		if segLower == dir {
			return true
		}
	}
	// Also treat segments with underscores as package dirs (e.g., "my_package")
	if strings.Contains(segment, "_") {
		return true
	}
	return false
}
