package util

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Styles for JSON syntax highlighting - beautiful TUI color scheme
var (
	// Keys: Cyan - easy to scan, stands out
	jsonKeyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	// Strings: Soft green - traditional, readable
	jsonStringStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	// Numbers: Purple/magenta - distinct from strings
	jsonNumberStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("176"))
	// Booleans: Blue - logical values
	jsonBoolTrueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	jsonBoolFalseStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("209"))
	// Null: Dim italic - indicates absence
	jsonNullStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	// Punctuation: Subtle gray - structural, not prominent
	jsonPunctStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	// Special log level colors
	jsonLevelDebug = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	jsonLevelInfo  = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	jsonLevelWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	jsonLevelError = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
)

// TryFormatJSON attempts to parse and format a line as JSON.
// Returns empty string if not valid JSON, formatted output otherwise.
// Handles:
// - UTF-8 BOM at start
// - Go test framework prefix (e.g., "    file.go:123: ")
// - Trailing invisible characters (zero-width spaces, etc.)
// - Whitespace padding
func TryFormatJSON(line string) string {
	trimmed := strings.TrimSpace(line)

	// Strip UTF-8 BOM if present
	trimmed = strings.TrimPrefix(trimmed, "\xef\xbb\xbf")

	// Find JSON boundaries - first '{' and last '}'
	// This handles:
	// - Prefixes before JSON (test framework: "file.go:123: {...")
	// - Suffixes after JSON (invisible chars, trailing text)
	jsonStart := strings.Index(trimmed, "{")
	jsonEnd := strings.LastIndex(trimmed, "}")

	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		return ""
	}

	// Extract JSON portion (inclusive of both braces)
	jsonStr := trimmed[jsonStart : jsonEnd+1]

	// Try to parse as JSON object
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return ""
	}

	// Empty object - skip
	if len(data) == 0 {
		return ""
	}

	// Decide format based on complexity
	if isSimpleJSON(data) {
		return formatJSONInline(data) + "\n"
	}
	return formatJSONMultiline(data, 0) + "\n"
}

// isSimpleJSON returns true if the JSON is simple enough for inline display.
// Simple means: few keys, no nesting, short values.
func isSimpleJSON(data map[string]interface{}) bool {
	if len(data) > 6 {
		return false
	}

	totalLen := 0
	for k, v := range data {
		// Check for nested structures
		switch val := v.(type) {
		case map[string]interface{}, []interface{}:
			return false
		case string:
			totalLen += len(k) + len(val) + 5 // key: "value",
			if len(val) > 60 {
				return false // Long strings go multi-line
			}
		default:
			totalLen += len(k) + 10 // Approximate
		}
	}

	return totalLen < 100
}

// formatJSONInline formats a simple JSON object in a single line.
// Format: key: value  key2: value2  key3: value3
func formatJSONInline(data map[string]interface{}) string {
	var sb strings.Builder

	// Order keys for consistent output, prioritizing common log fields
	keys := orderJSONKeys(data)

	for i, key := range keys {
		if i > 0 {
			sb.WriteString("  ")
		}
		sb.WriteString(jsonKeyStyle.Render(key))
		sb.WriteString(jsonPunctStyle.Render(": "))
		sb.WriteString(formatJSONValue(data[key], key))
	}

	return sb.String()
}

// formatJSONMultiline formats a complex JSON object with indentation.
// Uses YAML-like block scalars for long/multiline strings.
func formatJSONMultiline(data map[string]interface{}, indent int) string {
	var sb strings.Builder
	indentStr := strings.Repeat("  ", indent)
	innerIndent := strings.Repeat("  ", indent+1)

	sb.WriteString(jsonPunctStyle.Render("{"))
	sb.WriteString("\n")

	keys := orderJSONKeys(data)
	for i, key := range keys {
		sb.WriteString(innerIndent)
		sb.WriteString(jsonKeyStyle.Render(key))
		sb.WriteString(jsonPunctStyle.Render(": "))

		value := data[key]
		switch v := value.(type) {
		case map[string]interface{}:
			sb.WriteString(formatJSONMultiline(v, indent+1))
		case []interface{}:
			sb.WriteString(formatJSONArray(v, indent+1))
		case string:
			// Check if string needs block scalar format
			if needsBlockScalar(v) {
				sb.WriteString(formatBlockScalar(v, indent+2))
			} else {
				sb.WriteString(formatJSONValue(value, key))
			}
		default:
			sb.WriteString(formatJSONValue(value, key))
		}

		if i < len(keys)-1 {
			sb.WriteString(jsonPunctStyle.Render(","))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(indentStr)
	sb.WriteString(jsonPunctStyle.Render("}"))
	return sb.String()
}

// formatJSONArray formats a JSON array.
func formatJSONArray(arr []interface{}, indent int) string {
	if len(arr) == 0 {
		return jsonPunctStyle.Render("[]")
	}

	// Check if array is simple (primitives only, short)
	isSimple := true
	totalLen := 0
	for _, item := range arr {
		switch v := item.(type) {
		case map[string]interface{}, []interface{}:
			isSimple = false
		case string:
			totalLen += len(v)
		}
	}

	if isSimple && len(arr) <= 5 && totalLen < 50 {
		// Inline array
		var sb strings.Builder
		sb.WriteString(jsonPunctStyle.Render("["))
		for i, item := range arr {
			if i > 0 {
				sb.WriteString(jsonPunctStyle.Render(", "))
			}
			sb.WriteString(formatJSONValue(item, ""))
		}
		sb.WriteString(jsonPunctStyle.Render("]"))
		return sb.String()
	}

	// Multi-line array
	var sb strings.Builder
	indentStr := strings.Repeat("  ", indent)
	innerIndent := strings.Repeat("  ", indent+1)

	sb.WriteString(jsonPunctStyle.Render("["))
	sb.WriteString("\n")

	for i, item := range arr {
		sb.WriteString(innerIndent)
		switch v := item.(type) {
		case map[string]interface{}:
			sb.WriteString(formatJSONMultiline(v, indent+1))
		case []interface{}:
			sb.WriteString(formatJSONArray(v, indent+1))
		default:
			sb.WriteString(formatJSONValue(item, ""))
		}
		if i < len(arr)-1 {
			sb.WriteString(jsonPunctStyle.Render(","))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(indentStr)
	sb.WriteString(jsonPunctStyle.Render("]"))
	return sb.String()
}

// formatJSONValue formats a single JSON value with appropriate styling.
// The key parameter is used for special formatting (e.g., log levels).
// Uses YAML-like formatting: no quotes for simple strings.
func formatJSONValue(v interface{}, key string) string {
	switch val := v.(type) {
	case string:
		// Special handling for log level fields
		lowerKey := strings.ToLower(key)
		if lowerKey == "level" || lowerKey == "lvl" || lowerKey == "severity" {
			return formatLogLevel(val)
		}
		// YAML-like: no quotes for simple strings
		return formatStringValue(val)

	case float64:
		// Format as integer if whole number
		if val == float64(int64(val)) {
			return jsonNumberStyle.Render(fmt.Sprintf("%d", int64(val)))
		}
		return jsonNumberStyle.Render(fmt.Sprintf("%g", val))

	case bool:
		if val {
			return jsonBoolTrueStyle.Render("true")
		}
		return jsonBoolFalseStyle.Render("false")

	case nil:
		return jsonNullStyle.Render("null")

	default:
		return fmt.Sprintf("%v", val)
	}
}

// formatStringValue formats a string in YAML-like style (no quotes for simple strings).
func formatStringValue(s string) string {
	if s == "" {
		return jsonStringStyle.Render("~") // YAML-like empty indicator
	}
	// Simple string - no quotes, just styled
	return jsonStringStyle.Render(s)
}

// needsBlockScalar returns true if the string should use block scalar format.
// Block scalar is used for: multiline strings, very long strings (>80 chars).
func needsBlockScalar(s string) bool {
	return strings.Contains(s, "\n") || len(s) > 80
}

// formatBlockScalar formats a string as a YAML-like block scalar.
// Format:
//
//	|
//	  line 1
//	  line 2
func formatBlockScalar(s string, indent int) string {
	var sb strings.Builder
	indentStr := strings.Repeat("  ", indent)

	// Block scalar indicator
	sb.WriteString(jsonPunctStyle.Render("|"))
	sb.WriteString("\n")

	// Split into lines and handle long lines
	lines := splitIntoLines(s, 80)
	for _, line := range lines {
		sb.WriteString(indentStr)
		sb.WriteString(jsonStringStyle.Render(line))
		sb.WriteString("\n")
	}

	// Remove the trailing newline (will be added by caller)
	result := sb.String()
	if len(result) > 0 && result[len(result)-1] == '\n' {
		result = result[:len(result)-1]
	}
	return result
}

// splitIntoLines splits a string into lines, wrapping long lines at word boundaries.
func splitIntoLines(s string, maxWidth int) []string {
	// First split by existing newlines
	rawLines := strings.Split(s, "\n")
	var result []string

	for _, line := range rawLines {
		if len(line) <= maxWidth {
			result = append(result, line)
			continue
		}

		// Wrap long lines at word boundaries
		words := strings.Fields(line)
		if len(words) == 0 {
			result = append(result, "")
			continue
		}

		var currentLine strings.Builder
		for i, word := range words {
			if i == 0 {
				currentLine.WriteString(word)
				continue
			}

			// Check if adding this word would exceed max width
			if currentLine.Len()+1+len(word) > maxWidth {
				result = append(result, currentLine.String())
				currentLine.Reset()
				currentLine.WriteString(word)
			} else {
				currentLine.WriteString(" ")
				currentLine.WriteString(word)
			}
		}

		if currentLine.Len() > 0 {
			result = append(result, currentLine.String())
		}
	}

	return result
}

// formatLogLevel applies special styling based on log level value.
func formatLogLevel(level string) string {
	upper := strings.ToUpper(level)
	switch {
	case strings.Contains(upper, "DEBUG") || strings.Contains(upper, "TRACE"):
		return jsonLevelDebug.Render(level)
	case strings.Contains(upper, "INFO"):
		return jsonLevelInfo.Render(level)
	case strings.Contains(upper, "WARN"):
		return jsonLevelWarn.Render(level)
	case strings.Contains(upper, "ERR") || strings.Contains(upper, "FATAL") || strings.Contains(upper, "PANIC"):
		return jsonLevelError.Render(level)
	default:
		return jsonStringStyle.Render(fmt.Sprintf("%q", level))
	}
}

// orderJSONKeys returns keys in a logical order for log display.
// Prioritizes: level, time/ts, msg/message, then alphabetical.
func orderJSONKeys(data map[string]interface{}) []string {
	// Priority keys (common structured log fields)
	priority := []string{"level", "lvl", "severity", "time", "ts", "timestamp", "@timestamp", "msg", "message"}

	var ordered []string
	seen := make(map[string]bool)

	// Add priority keys first (if present)
	for _, key := range priority {
		if _, ok := data[key]; ok {
			ordered = append(ordered, key)
			seen[key] = true
		}
	}

	// Collect remaining keys
	var remaining []string
	for key := range data {
		if !seen[key] {
			remaining = append(remaining, key)
		}
	}

	// Sort remaining keys alphabetically
	sort.Strings(remaining)
	ordered = append(ordered, remaining...)

	return ordered
}
