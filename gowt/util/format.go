package util

import (
	"fmt"
	"math"
)

// FormatDuration formats seconds as a duration string with consistent decimal places.
// Always shows one decimal place to prevent visual jumpiness (e.g., "5.0s" not "5s").
func FormatDuration(seconds float64) string {
	// Round to 1 decimal place first to handle boundary cases correctly
	// e.g., 59.99 rounds to 60.0, which should display as "1m0.0s" not "59m60.0s"
	rounded := math.Round(seconds*10) / 10

	if rounded < 60 {
		return fmt.Sprintf("%.1fs", rounded)
	}
	if rounded < 3600 {
		mins := int(rounded) / 60
		secs := rounded - float64(mins*60)
		return fmt.Sprintf("%dm%.1fs", mins, secs)
	}
	hours := int(rounded) / 3600
	mins := (int(rounded) % 3600) / 60
	secs := rounded - float64(hours*3600+mins*60)
	return fmt.Sprintf("%dh%dm%.1fs", hours, mins, secs)
}
