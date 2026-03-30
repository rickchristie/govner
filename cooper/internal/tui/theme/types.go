// Package theme holds shared type definitions, constants, and pre-computed
// styles used by both the root tui package and the tui/components sub-package.
// Keeping them in a leaf package avoids import cycles.
package theme

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SubModel is the interface that every tab sub-model must satisfy.
// It lives in the theme package (a leaf) so both the root tui package
// and all tab sub-packages can reference the same type without import cycles.
type SubModel interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (SubModel, tea.Cmd)
	View(width, height int) string
}

// ----- Shared types -----

// TabID identifies a top-level tab in the TUI.
type TabID int

const (
	TabContainers TabID = iota
	TabMonitor
	TabBlocked
	TabAllowed
	TabBridgeLogs
	TabBridgeRoutes
	TabConfigure
	TabAbout
)

// ModalType identifies which modal dialog is active.
type ModalType int

const (
	ModalExit ModalType = iota
	ModalDeleteRoute
	ModalStopContainer
	ModalRestartContainer
	ModalUpdateInfo
	ModalReloadSocat
)

// TabInfo holds display metadata for a tab.
type TabInfo struct {
	ID          TabID
	Label       string
	Icon        string
	ShortcutKey string
}

// AllTabs is the ordered list of all top-level tabs with their metadata.
var AllTabs = []TabInfo{
	{ID: TabContainers, Label: "Containers", Icon: CaskEmoji, ShortcutKey: "1"},
	{ID: TabMonitor, Label: "Monitor", Icon: "🔍", ShortcutKey: "2"},
	{ID: TabBlocked, Label: "Blocked", Icon: IconCross, ShortcutKey: "3"},
	{ID: TabAllowed, Label: "Allowed", Icon: IconCheck, ShortcutKey: "4"},
	{ID: TabBridgeLogs, Label: "Bridge Logs", Icon: IconPlug, ShortcutKey: "5"},
	{ID: TabBridgeRoutes, Label: "Routes", Icon: IconGear, ShortcutKey: "6"},
	{ID: TabConfigure, Label: "Configure", Icon: IconGear, ShortcutKey: "7"},
	{ID: TabAbout, Label: "About", Icon: "ℹ", ShortcutKey: "8"},
}

// ----- Animation timing constants -----

const (
	BarrelRollInterval        = 150 * time.Millisecond
	StatusPulseInterval       = 300 * time.Millisecond
	PendingBadgePulseInterval = 500 * time.Millisecond
	ApprovalFlashOnInterval   = 200 * time.Millisecond
	ApprovalFlashOffInterval  = 100 * time.Millisecond
	CountdownTickInterval     = 100 * time.Millisecond
	UITickInterval            = 1 * time.Second
	CursorBlinkInterval       = 530 * time.Millisecond
	ProgressStaggerDelay      = 80 * time.Millisecond
	LoadingHoldDuration       = 800 * time.Millisecond
)

// ----- Unicode constants -- Brand -----

const (
	BarrelEmoji = "\U0001F943" // 🥃
	CaskEmoji   = "\U0001F4E6" // 📦
)

// ----- Unicode constants -- Status icons -----

const (
	IconCheck      = "\u2713" // ✓
	IconCross      = "\u2717" // ✗
	IconWarn       = "\u26A0" // ⚠
	IconArrowRight = "\u25B6" // ▶
	IconArrowDown  = "\u25BC" // ▼
	IconArrowUp    = "\u25B2" // ▲
	IconDot        = "\u25CF" // ●
	IconDotEmpty   = "\u25CB" // ○
	IconBlock      = "\u2588" // █
	IconBlockHalf  = "\u2593" // ▓
	IconShade      = "\u2591" // ░
	IconLock       = "\U0001F512" // 🔒
	IconUnlock     = "\U0001F513" // 🔓
	IconShield     = "\U0001F6E1\uFE0F" // 🛡️
	IconPlug       = "\U0001F50C" // 🔌
	IconGear       = "\u2699" // ⚙
	IconTimer      = "\u23F1" // ⏱
)

// ----- Unicode constants -- Box drawing -----

const (
	BorderH     = "\u2500" // ─
	BorderV     = "\u2502" // │
	BorderTL    = "\u250C" // ┌
	BorderTR    = "\u2510" // ┐
	BorderBL    = "\u2514" // └
	BorderBR    = "\u2518" // ┘
	BorderTee   = "\u252C" // ┬
	BorderBTee  = "\u2534" // ┴
	BorderLTee  = "\u251C" // ├
	BorderRTee  = "\u2524" // ┤
	BorderCross = "\u253C" // ┼
	BorderHBold = "\u2501" // ━
	BorderVBold = "\u2503" // ┃
)

// ----- Unicode constants -- Tab indicators -----

const (
	TabActive   = "\u2501" // ━  Bold horizontal, used under active tab
	TabInactive = "\u2500" // ─  Thin horizontal, under inactive tabs
)

// ----- Unicode constants -- Progress bar -----

const (
	ProgressFull  = "\u2501" // ━
	ProgressEmpty = "\u2500" // ─
	ProgressTip   = "\u2578" // ╸  Half-block at leading edge during animation
)

// ----- Animation frame helpers -----

// BarrelRollFrames returns the dot animation frames for the barrel roll.
func BarrelRollFrames() []string {
	return []string{
		"\u00B7 " + BarrelEmoji + " \u00B7",
		"\u00B7 " + BarrelEmoji + " \u00B7 \u00B7",
		"\u00B7 " + BarrelEmoji + " \u00B7 \u00B7 \u00B7",
		"\u00B7 " + BarrelEmoji + " \u00B7 \u00B7",
	}
}

// StatusPulseColors returns the color sequence for the status dot pulse.
func StatusPulseColors() []lipgloss.Color {
	return []lipgloss.Color{ColorAmber, ColorCopper, ColorAmber}
}

// PendingBadgePulseColors returns the color sequence for the pending badge.
func PendingBadgePulseColors() []lipgloss.Color {
	return []lipgloss.Color{ColorCopper, ColorAmber}
}
