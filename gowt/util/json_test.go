package util

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

// Test styles - must match the styles in json.go
var (
	sK  = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))               // key
	sS  = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))              // string
	sN  = lipgloss.NewStyle().Foreground(lipgloss.Color("176"))              // number
	sBT = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))               // bool true
	sBF = lipgloss.NewStyle().Foreground(lipgloss.Color("209"))              // bool false
	sNL = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true) // null
	sP  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))              // punct
	sLD = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))              // level debug
	sLI = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))               // level info
	sLW = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))              // level warn
	sLE = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)   // level error
)

// Helper to build "key: value" pairs
func kv(key, val string) string {
	return sK.Render(key) + sP.Render(": ") + val
}

func TestTryFormatJSON_NotJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"plain text", "hello world"},
		{"starts with brace but not json", "{not json"},
		{"ends with brace but not json", "not json}"},
		{"array not object", `["a", "b", "c"]`},
		{"number", "123"},
		{"boolean", "true"},
		{"malformed json missing closing brace", `{"key": "value"`},
		{"malformed json missing quote", `{"key: "value"}`},
		{"empty object", "{}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TryFormatJSON(tt.input)
			assert.Equal(t, "", result, "should return empty string for non-JSON input")
		})
	}
}

func TestTryFormatJSON_SimpleInline(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "single key-value string",
			input:  `{"name": "test"}`,
			expect: kv("name", sS.Render("test")) + "\n",
		},
		{
			name:   "single key-value number",
			input:  `{"count": 42}`,
			expect: kv("count", sN.Render("42")) + "\n",
		},
		{
			name:   "single key-value boolean true",
			input:  `{"enabled": true}`,
			expect: kv("enabled", sBT.Render("true")) + "\n",
		},
		{
			name:   "single key-value boolean false",
			input:  `{"enabled": false}`,
			expect: kv("enabled", sBF.Render("false")) + "\n",
		},
		{
			name:   "single key-value null",
			input:  `{"value": null}`,
			expect: kv("value", sNL.Render("null")) + "\n",
		},
		{
			name:   "single key-value empty string uses tilde",
			input:  `{"name": ""}`,
			expect: kv("name", sS.Render("~")) + "\n",
		},
		{
			name:   "single key-value float",
			input:  `{"price": 19.99}`,
			expect: kv("price", sN.Render("19.99")) + "\n",
		},
		{
			name:   "multiple simple keys ordered alphabetically",
			input:  `{"zebra": "z", "apple": "a"}`,
			expect: kv("apple", sS.Render("a")) + "  " + kv("zebra", sS.Render("z")) + "\n",
		},
		{
			name:  "log with level and message priority ordering",
			input: `{"zebra": "z", "level": "info", "message": "hello"}`,
			expect: kv("level", sLI.Render("info")) + "  " +
				kv("message", sS.Render("hello")) + "  " +
				kv("zebra", sS.Render("z")) + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TryFormatJSON(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestTryFormatJSON_Multiline_NestedObject(t *testing.T) {
	input := `{"outer": {"inner": "value"}}`
	expect := sP.Render("{") + "\n" +
		"  " + kv("outer", sP.Render("{")) + "\n" +
		"    " + kv("inner", sS.Render("value")) + "\n" +
		"  " + sP.Render("}") + "\n" +
		sP.Render("}") + "\n"

	result := TryFormatJSON(input)
	assert.Equal(t, expect, result)
}

func TestTryFormatJSON_Multiline_SimpleArray(t *testing.T) {
	input := `{"items": ["a", "b"]}`
	expect := sP.Render("{") + "\n" +
		"  " + kv("items", sP.Render("[")+sS.Render("a")+sP.Render(", ")+sS.Render("b")+sP.Render("]")) + "\n" +
		sP.Render("}") + "\n"

	result := TryFormatJSON(input)
	assert.Equal(t, expect, result)
}

func TestTryFormatJSON_Multiline_LongArray(t *testing.T) {
	input := `{"items": ["apple", "banana", "cherry", "date", "elderberry", "fig"]}`
	expect := sP.Render("{") + "\n" +
		"  " + kv("items", sP.Render("[")) + "\n" +
		"    " + sS.Render("apple") + sP.Render(",") + "\n" +
		"    " + sS.Render("banana") + sP.Render(",") + "\n" +
		"    " + sS.Render("cherry") + sP.Render(",") + "\n" +
		"    " + sS.Render("date") + sP.Render(",") + "\n" +
		"    " + sS.Render("elderberry") + sP.Render(",") + "\n" +
		"    " + sS.Render("fig") + "\n" +
		"  " + sP.Render("]") + "\n" +
		sP.Render("}") + "\n"

	result := TryFormatJSON(input)
	assert.Equal(t, expect, result)
}

func TestTryFormatJSON_Multiline_ArrayOfObjects(t *testing.T) {
	input := `{"users": [{"name": "alice"}, {"name": "bob"}]}`
	expect := sP.Render("{") + "\n" +
		"  " + kv("users", sP.Render("[")) + "\n" +
		"    " + sP.Render("{") + "\n" +
		"      " + kv("name", sS.Render("alice")) + "\n" +
		"    " + sP.Render("}") + sP.Render(",") + "\n" +
		"    " + sP.Render("{") + "\n" +
		"      " + kv("name", sS.Render("bob")) + "\n" +
		"    " + sP.Render("}") + "\n" +
		"  " + sP.Render("]") + "\n" +
		sP.Render("}") + "\n"

	result := TryFormatJSON(input)
	assert.Equal(t, expect, result)
}

func TestTryFormatJSON_LogLevelStyling(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "level debug",
			input:  `{"level": "debug"}`,
			expect: kv("level", sLD.Render("debug")) + "\n",
		},
		{
			name:   "level info",
			input:  `{"level": "info"}`,
			expect: kv("level", sLI.Render("info")) + "\n",
		},
		{
			name:   "level warn",
			input:  `{"level": "warn"}`,
			expect: kv("level", sLW.Render("warn")) + "\n",
		},
		{
			name:   "level warning",
			input:  `{"level": "warning"}`,
			expect: kv("level", sLW.Render("warning")) + "\n",
		},
		{
			name:   "level error",
			input:  `{"level": "error"}`,
			expect: kv("level", sLE.Render("error")) + "\n",
		},
		{
			name:   "level fatal",
			input:  `{"level": "fatal"}`,
			expect: kv("level", sLE.Render("fatal")) + "\n",
		},
		{
			name:   "lvl field",
			input:  `{"lvl": "info"}`,
			expect: kv("lvl", sLI.Render("info")) + "\n",
		},
		{
			name:   "severity field",
			input:  `{"severity": "error"}`,
			expect: kv("severity", sLE.Render("error")) + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TryFormatJSON(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestTryFormatJSON_StructuredLog(t *testing.T) {
	input := `{"level": "error", "time": "2025-01-01T00:00:00Z", "message": "connection failed", "error": "timeout"}`
	expect := kv("level", sLE.Render("error")) + "  " +
		kv("time", sS.Render("2025-01-01T00:00:00Z")) + "  " +
		kv("message", sS.Render("connection failed")) + "  " +
		kv("error", sS.Render("timeout")) + "\n"

	result := TryFormatJSON(input)
	assert.Equal(t, expect, result)
}

func TestTryFormatJSON_WhitespaceHandling(t *testing.T) {
	expect := kv("key", sS.Render("value")) + "\n"

	tests := []struct {
		name  string
		input string
	}{
		{"leading whitespace", `  {"key": "value"}`},
		{"trailing whitespace", `{"key": "value"}  `},
		{"both whitespace", `  {"key": "value"}  `},
		{"newline prefix", "\n{\"key\": \"value\"}"},
		{"tab prefix", "\t{\"key\": \"value\"}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TryFormatJSON(tt.input)
			assert.Equal(t, expect, result)
		})
	}
}

func TestTryFormatJSON_RealWorldLog(t *testing.T) {
	input := `{"level":"error","_server":"","_logger":"PgStorage","_err":[{"_msg":"failed to deallocate cached statement(s): conn closed","_debug":{},"_stack":["goroutine 463495 [running]:","runtime/debug.Stack()","    /home/ricky/.gobrew/current/go/src/runtime/debug/stack.go:26 +0x5e","github.com/example/lib/mend.getStackTrace()","    /path/to/the/repository/lib/mend/err.go:134 +0x17","github.com/example/lib/mend.Wrap({0xc73320, 0xc006a471e0?}, 0x1)","    /path/to/the/repository/lib/mend/err.go:127 +0x165","github.com/example/service/lib/ssproc.(*PgStorage).rollback(0xc003bb1d80, {0x9781e0?, 0xc00692b720?}, 0x9781e0?, {0xc7fd78, 0xc00c941e00})","    /path/to/the/repository/service/lib/ssproc/storage_pg.go:151 +0x146","github.com/example/service/lib/ssproc.(*PgStorage).SendHeartbeat(0xc003bb1d80, {0xc7a6e0?, 0xc007c981c0?}, {0xc000839ef0, 0x2e}, {0xc007ca7860, 0x24}, 0x1dcd6500)","    /path/to/the/repository/service/lib/ssproc/storage_pg.go:818 +0x578","github.com/example/service/lib/ssproc.(*Executor[...]).pingHeartbeat(0xc7a6e0, {0xc7a6e0?, 0xc007c981c0}, 0x44a552, 0xc00077db20?, {0xc000839ef0, 0x2e}, {0xc007ca7860, 0x24})","    /path/to/the/repository/service/lib/ssproc/executor.go:611 +0x3ca","created by github.com/example/service/lib/ssproc.(*Executor[...]).execute in goroutine 449822","    /path/to/the/repository/service/lib/ssproc/executor.go:443 +0x81f",""]},"failed to deallocate cached statement(s): conn closed","conn closed"],"time":"2025-11-28T19:16:54.057919774+07:00","level":"error","message":"failed to rollback for table: public.ssproc_test_main"}`

	// Build expected output programmatically
	// Key order: level, time, message, _err, _logger, _server
	expect := sP.Render("{") + "\n" +
		"  " + kv("level", sLE.Render("error")) + sP.Render(",") + "\n" +
		"  " + kv("time", sS.Render("2025-11-28T19:16:54.057919774+07:00")) + sP.Render(",") + "\n" +
		"  " + kv("message", sS.Render("failed to rollback for table: public.ssproc_test_main")) + sP.Render(",") + "\n" +
		"  " + kv("_err", sP.Render("[")) + "\n" +
		// First element: object with _debug, _msg, _stack
		"    " + sP.Render("{") + "\n" +
		"      " + kv("_debug", sP.Render("{")) + "\n" +
		"      " + sP.Render("}") + sP.Render(",") + "\n" +
		"      " + kv("_msg", sS.Render("failed to deallocate cached statement(s): conn closed")) + sP.Render(",") + "\n" +
		"      " + kv("_stack", sP.Render("[")) + "\n" +
		"        " + sS.Render("goroutine 463495 [running]:") + sP.Render(",") + "\n" +
		"        " + sS.Render("runtime/debug.Stack()") + sP.Render(",") + "\n" +
		"        " + sS.Render("    /home/ricky/.gobrew/current/go/src/runtime/debug/stack.go:26 +0x5e") + sP.Render(",") + "\n" +
		"        " + sS.Render("github.com/example/lib/mend.getStackTrace()") + sP.Render(",") + "\n" +
		"        " + sS.Render("    /path/to/the/repository/lib/mend/err.go:134 +0x17") + sP.Render(",") + "\n" +
		"        " + sS.Render("github.com/example/lib/mend.Wrap({0xc73320, 0xc006a471e0?}, 0x1)") + sP.Render(",") + "\n" +
		"        " + sS.Render("    /path/to/the/repository/lib/mend/err.go:127 +0x165") + sP.Render(",") + "\n" +
		"        " + sS.Render("github.com/example/service/lib/ssproc.(*PgStorage).rollback(0xc003bb1d80, {0x9781e0?, 0xc00692b720?}, 0x9781e0?, {0xc7fd78, 0xc00c941e00})") + sP.Render(",") + "\n" +
		"        " + sS.Render("    /path/to/the/repository/service/lib/ssproc/storage_pg.go:151 +0x146") + sP.Render(",") + "\n" +
		"        " + sS.Render("github.com/example/service/lib/ssproc.(*PgStorage).SendHeartbeat(0xc003bb1d80, {0xc7a6e0?, 0xc007c981c0?}, {0xc000839ef0, 0x2e}, {0xc007ca7860, 0x24}, 0x1dcd6500)") + sP.Render(",") + "\n" +
		"        " + sS.Render("    /path/to/the/repository/service/lib/ssproc/storage_pg.go:818 +0x578") + sP.Render(",") + "\n" +
		"        " + sS.Render("github.com/example/service/lib/ssproc.(*Executor[...]).pingHeartbeat(0xc7a6e0, {0xc7a6e0?, 0xc007c981c0}, 0x44a552, 0xc00077db20?, {0xc000839ef0, 0x2e}, {0xc007ca7860, 0x24})") + sP.Render(",") + "\n" +
		"        " + sS.Render("    /path/to/the/repository/service/lib/ssproc/executor.go:611 +0x3ca") + sP.Render(",") + "\n" +
		"        " + sS.Render("created by github.com/example/service/lib/ssproc.(*Executor[...]).execute in goroutine 449822") + sP.Render(",") + "\n" +
		"        " + sS.Render("    /path/to/the/repository/service/lib/ssproc/executor.go:443 +0x81f") + sP.Render(",") + "\n" +
		"        " + sS.Render("~") + "\n" + // empty string renders as ~
		"      " + sP.Render("]") + "\n" +
		"    " + sP.Render("}") + sP.Render(",") + "\n" +
		// Second element: string
		"    " + sS.Render("failed to deallocate cached statement(s): conn closed") + sP.Render(",") + "\n" +
		// Third element: string
		"    " + sS.Render("conn closed") + "\n" +
		"  " + sP.Render("]") + sP.Render(",") + "\n" +
		"  " + kv("_logger", sS.Render("PgStorage")) + sP.Render(",") + "\n" +
		"  " + kv("_server", sS.Render("~")) + "\n" + // empty string renders as ~
		sP.Render("}") + "\n"

	result := TryFormatJSON(input)
	assert.Equal(t, expect, result)
}

func TestTryFormatJSON_KeyOrdering(t *testing.T) {
	input := `{"zebra": 1, "message": "hello", "apple": 2, "level": "info", "time": "now"}`
	expect := kv("level", sLI.Render("info")) + "  " +
		kv("time", sS.Render("now")) + "  " +
		kv("message", sS.Render("hello")) + "  " +
		kv("apple", sN.Render("2")) + "  " +
		kv("zebra", sN.Render("1")) + "\n"

	result := TryFormatJSON(input)
	assert.Equal(t, expect, result)
}

func TestOrderJSONKeys(t *testing.T) {
	data := map[string]any{
		"zebra":   1,
		"message": "hello",
		"level":   "info",
		"apple":   2,
		"time":    "now",
	}

	keys := orderJSONKeys(data)
	expectOrder := []string{"level", "time", "message", "apple", "zebra"}

	assert.Equal(t, expectOrder, keys)
}

func TestIsSimpleJSON(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want bool
	}{
		{
			name: "single key",
			data: map[string]any{"a": "b"},
			want: true,
		},
		{
			name: "few keys short values",
			data: map[string]any{"a": "1", "b": "2", "c": "3"},
			want: true,
		},
		{
			name: "7 keys exceeds limit",
			data: map[string]any{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6, "g": 7},
			want: false,
		},
		{
			name: "nested object not simple",
			data: map[string]any{"a": map[string]any{"b": "c"}},
			want: false,
		},
		{
			name: "array value not simple",
			data: map[string]any{"a": []any{"b", "c"}},
			want: false,
		},
		{
			name: "string over 60 chars not simple",
			data: map[string]any{"a": strings.Repeat("x", 61)},
			want: false,
		},
		{
			name: "string exactly 60 chars is simple",
			data: map[string]any{"a": strings.Repeat("x", 60)},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSimpleJSON(tt.data)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitIntoLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
		expect   []string
	}{
		{
			name:     "short line unchanged",
			input:    "hello",
			maxWidth: 80,
			expect:   []string{"hello"},
		},
		{
			name:     "line exactly at max unchanged",
			input:    "12345678901234567890",
			maxWidth: 20,
			expect:   []string{"12345678901234567890"},
		},
		{
			name:     "long line wraps at word boundary",
			input:    "the quick brown fox jumps over the lazy dog",
			maxWidth: 20,
			expect:   []string{"the quick brown fox", "jumps over the lazy", "dog"},
		},
		{
			name:     "preserves existing newlines",
			input:    "line1\nline2\nline3",
			maxWidth: 80,
			expect:   []string{"line1", "line2", "line3"},
		},
		{
			name:     "empty line preserved",
			input:    "a\n\nb",
			maxWidth: 80,
			expect:   []string{"a", "", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitIntoLines(tt.input, tt.maxWidth)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestNeedsBlockScalar(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"short string", "short", false},
		{"exactly 80 chars", strings.Repeat("x", 80), false},
		{"81 chars needs block", strings.Repeat("x", 81), true},
		{"multiline needs block", "line1\nline2", true},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := needsBlockScalar(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
