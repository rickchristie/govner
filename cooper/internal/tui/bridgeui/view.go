package bridgeui

import (
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// ----- Shared rendering utilities -----
// Used by both the logs tab (model.go) and the routes tab (routes.go).

// sectionDivider returns a styled section divider line with a centered label.
func sectionDivider(label string, width int) string {
	labelRendered := theme.PaneLabelStyle.Render(" " + label + " ")
	lineLen := width - len(label) - 4
	if lineLen < 0 {
		lineLen = 0
	}
	left := theme.DividerStyle.Render(theme.BorderH + theme.BorderH)
	right := theme.DividerStyle.Render("")
	for i := 0; i < lineLen; i++ {
		right += theme.DividerStyle.Render(theme.BorderH)
	}
	return left + labelRendered + right
}
