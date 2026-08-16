package util

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTryFormatJSONInputEnvelopes(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "UTF-8 BOM", input: "\xef\xbb\xbf{\"level\":\"info\",\"message\":\"ready\"}"},
		{
			name:  "go test framework prefix",
			input: "    storage_test.go:123: {\"level\":\"info\",\"message\":\"ready\"}",
		},
		{
			name:  "leading ANSI already removed by caller",
			input: "  {\"level\":\"info\",\"message\":\"ready\"}",
		},
		{
			name:  "zero-width suffix",
			input: "{\"level\":\"info\",\"message\":\"ready\"}\u200b",
		},
		{
			name:  "trailing diagnostic text",
			input: "{\"level\":\"info\",\"message\":\"ready\"} extra",
		},
		{
			name:  "trailing newline and text",
			input: "{\"level\":\"info\",\"message\":\"ready\"}\nextra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TryFormatJSON(tt.input)
			assert.NotEmpty(t, result)
			assert.Contains(t, result, "ready")
		})
	}
}

func TestTryFormatJSONPreservesNumbersBeyondFloat64IntegerPrecision(t *testing.T) {
	formatted := stripStyles(TryFormatJSON(
		"{\"request_id\":9007199254740993,\"nested\":{\"sequence\":9223372036854775807}}",
	))
	assert.Contains(t, formatted, "9007199254740993")
	assert.Contains(t, formatted, "9223372036854775807")
	assert.NotContains(t, formatted, "9007199254740992")
}

func TestFormatJSONArrayEdgeCases(t *testing.T) {
	assert.Equal(t, "[]", stripStyles(formatJSONArray(nil, 0)))

	nested := []interface{}{
		[]interface{}{"one", "two"},
		map[string]interface{}{"name": "value"},
	}
	formatted := stripStyles(formatJSONArray(nested, 0))
	assert.Contains(t, formatted, "[\n")
	assert.Contains(t, formatted, "[one, two]")
	assert.Contains(t, formatted, "name: value")
}

func TestFormatJSONValueFallbackAndUnknownLevel(t *testing.T) {
	assert.Equal(t, "custom", stripStyles(formatJSONValue("custom", "name")))
	assert.Equal(t, `"NOTICE"`, stripStyles(formatLogLevel("NOTICE")))
	assert.Equal(t, "{x}",
		formatJSONValue(struct{ Value string }{Value: "x"}, ""))
}

func TestFormatBlockScalarWrapsMultilineText(t *testing.T) {
	formatted := stripStyles(formatBlockScalar(
		"first line\n"+strings.Repeat("word ", 25),
		2,
	))

	assert.True(t, strings.HasPrefix(formatted, "|\n"))
	assert.Contains(t, formatted, "    first line")
	assert.Greater(t, strings.Count(formatted, "\n"), 2)
	assert.False(t, strings.HasSuffix(formatted, "\n"))
}

func TestOrderJSONKeysIncludesEveryPriorityField(t *testing.T) {
	data := map[string]interface{}{
		"other":      1,
		"message":    "message",
		"msg":        "msg",
		"@timestamp": "at",
		"timestamp":  "timestamp",
		"ts":         "ts",
		"time":       "time",
		"severity":   "severity",
		"lvl":        "lvl",
		"level":      "level",
	}

	assert.Equal(t, []string{
		"level", "lvl", "severity", "time", "ts", "timestamp", "@timestamp",
		"msg", "message", "other",
	}, orderJSONKeys(data))
}

func stripStyles(value string) string {
	var result strings.Builder
	inEscape := false
	for _, char := range value {
		if char == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(char)
	}
	return result.String()
}
