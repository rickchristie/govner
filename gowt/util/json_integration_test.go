package util

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stripAnsi replicates the function from model.go for testing
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

// simulateProcessOutput replicates the processOutput flow
func simulateProcessOutput(output string) string {
	cleaned := stripAnsi(output)
	trimmed := strings.TrimSpace(cleaned)
	return TryFormatJSON(trimmed)
}

func TestProcessOutputFlow_PlainJSON(t *testing.T) {
	input := `{"level":"error","message":"test"}`
	result := simulateProcessOutput(input)
	assert.NotEmpty(t, result, "plain JSON should be formatted")
}

func TestProcessOutputFlow_JSONWithNewline(t *testing.T) {
	input := "{\"level\":\"error\",\"message\":\"test\"}\n"
	result := simulateProcessOutput(input)
	assert.NotEmpty(t, result, "JSON with trailing newline should be formatted")
}

func TestProcessOutputFlow_JSONWithANSI(t *testing.T) {
	// JSON wrapped in ANSI color codes
	input := "\x1b[31m{\"level\":\"error\",\"message\":\"test\"}\x1b[0m"
	result := simulateProcessOutput(input)
	assert.NotEmpty(t, result, "JSON wrapped in ANSI codes should be formatted")
}

func TestProcessOutputFlow_JSONWithLeadingANSI(t *testing.T) {
	input := "\x1b[31m{\"level\":\"error\",\"message\":\"test\"}"
	result := simulateProcessOutput(input)
	assert.NotEmpty(t, result, "JSON with leading ANSI should be formatted")
}

func TestProcessOutputFlow_JSONWithBOM(t *testing.T) {
	// UTF-8 BOM (Byte Order Mark) at the start
	input := "\xef\xbb\xbf{\"level\":\"error\",\"message\":\"test\"}"
	result := simulateProcessOutput(input)
	// This will likely FAIL - BOM is not stripped!
	assert.NotEmpty(t, result, "JSON with BOM should be formatted")
}

func TestProcessOutputFlow_JSONWithCarriageReturn(t *testing.T) {
	input := "{\"level\":\"error\",\"message\":\"test\"}\r\n"
	result := simulateProcessOutput(input)
	assert.NotEmpty(t, result, "JSON with CRLF should be formatted")
}

func TestProcessOutputFlow_JSONWithTab(t *testing.T) {
	input := "\t{\"level\":\"error\",\"message\":\"test\"}"
	result := simulateProcessOutput(input)
	assert.NotEmpty(t, result, "JSON with leading tab should be formatted")
}

func TestProcessOutputFlow_RealWorldJSON(t *testing.T) {
	input := `{"level":"error","_server":"","_logger":"PgStorage","_err":[{"_msg":"failed to deallocate cached statement(s): conn closed","_debug":{},"_stack":["goroutine 463495 [running]:","runtime/debug.Stack()","    /home/ricky/.gobrew/current/go/src/runtime/debug/stack.go:26 +0x5e","github.com/example/lib/mend.getStackTrace()","    /path/to/the/repository/lib/mend/err.go:134 +0x17","github.com/example/lib/mend.Wrap({0xc73320, 0xc006a471e0?}, 0x1)","    /path/to/the/repository/lib/mend/err.go:127 +0x165","github.com/example/service/lib/ssproc.(*PgStorage).rollback(0xc003bb1d80, {0x9781e0?, 0xc00692b720?}, 0x9781e0?, {0xc7fd78, 0xc00c941e00})","    /path/to/the/repository/service/lib/ssproc/storage_pg.go:151 +0x146","github.com/example/service/lib/ssproc.(*PgStorage).SendHeartbeat(0xc003bb1d80, {0xc7a6e0?, 0xc007c981c0?}, {0xc000839ef0, 0x2e}, {0xc007ca7860, 0x24}, 0x1dcd6500)","    /path/to/the/repository/service/lib/ssproc/storage_pg.go:818 +0x578","github.com/example/service/lib/ssproc.(*Executor[...]).pingHeartbeat(0xc7a6e0, {0xc7a6e0?, 0xc007c981c0}, 0x44a552, 0xc00077db20?, {0xc000839ef0, 0x2e}, {0xc007ca7860, 0x24})","    /path/to/the/repository/service/lib/ssproc/executor.go:611 +0x3ca","created by github.com/example/service/lib/ssproc.(*Executor[...]).execute in goroutine 449822","    /path/to/the/repository/service/lib/ssproc/executor.go:443 +0x81f",""]},"failed to deallocate cached statement(s): conn closed","conn closed"],"time":"2025-11-28T19:16:54.057919774+07:00","level":"error","message":"failed to rollback for table: public.ssproc_test_main"}`
	result := simulateProcessOutput(input)
	assert.NotEmpty(t, result, "real-world JSON should be formatted")
}

func TestProcessOutputFlow_RealWorldJSONWithNewline(t *testing.T) {
	input := `{"level":"error","_server":"","_logger":"PgStorage","_err":[{"_msg":"failed to deallocate cached statement(s): conn closed","_debug":{},"_stack":["goroutine 463495 [running]:","runtime/debug.Stack()","    /home/ricky/.gobrew/current/go/src/runtime/debug/stack.go:26 +0x5e","github.com/example/lib/mend.getStackTrace()","    /path/to/the/repository/lib/mend/err.go:134 +0x17","github.com/example/lib/mend.Wrap({0xc73320, 0xc006a471e0?}, 0x1)","    /path/to/the/repository/lib/mend/err.go:127 +0x165","github.com/example/service/lib/ssproc.(*PgStorage).rollback(0xc003bb1d80, {0x9781e0?, 0xc00692b720?}, 0x9781e0?, {0xc7fd78, 0xc00c941e00})","    /path/to/the/repository/service/lib/ssproc/storage_pg.go:151 +0x146","github.com/example/service/lib/ssproc.(*PgStorage).SendHeartbeat(0xc003bb1d80, {0xc7a6e0?, 0xc007c981c0?}, {0xc000839ef0, 0x2e}, {0xc007ca7860, 0x24}, 0x1dcd6500)","    /path/to/the/repository/service/lib/ssproc/storage_pg.go:818 +0x578","github.com/example/service/lib/ssproc.(*Executor[...]).pingHeartbeat(0xc7a6e0, {0xc7a6e0?, 0xc007c981c0}, 0x44a552, 0xc00077db20?, {0xc000839ef0, 0x2e}, {0xc007ca7860, 0x24})","    /path/to/the/repository/service/lib/ssproc/executor.go:611 +0x3ca","created by github.com/example/service/lib/ssproc.(*Executor[...]).execute in goroutine 449822","    /path/to/the/repository/service/lib/ssproc/executor.go:443 +0x81f",""]},"failed to deallocate cached statement(s): conn closed","conn closed"],"time":"2025-11-28T19:16:54.057919774+07:00","level":"error","message":"failed to rollback for table: public.ssproc_test_main"}
`
	result := simulateProcessOutput(input)
	assert.NotEmpty(t, result, "real-world JSON with newline should be formatted")
}

func TestProcessOutputFlow_TestFrameworkPrefix(t *testing.T) {
	// go test adds "    filename:line: " prefix to t.Log output
	input := "    storage_pg_test.go:123: {\"level\":\"error\",\"message\":\"test\"}"
	result := simulateProcessOutput(input)
	// This will FAIL - the prefix prevents JSON detection!
	assert.NotEmpty(t, result, "JSON with test framework prefix should be formatted")
}

func TestProcessOutputFlow_IndentedTestOutput(t *testing.T) {
	// Test output is often indented with spaces
	input := "        {\"level\":\"error\",\"message\":\"test\"}"
	result := simulateProcessOutput(input)
	assert.NotEmpty(t, result, "indented JSON should be formatted")
}

func TestProcessOutputFlow_TrailingInvisibleChars(t *testing.T) {
	// Zero-width space and other invisible characters after JSON
	testCases := []struct {
		name  string
		input string
	}{
		{"zero-width space", "{\"level\":\"error\",\"message\":\"test\"}\u200b"},
		{"zero-width non-joiner", "{\"level\":\"error\",\"message\":\"test\"}\u200c"},
		{"zero-width joiner", "{\"level\":\"error\",\"message\":\"test\"}\u200d"},
		{"word joiner", "{\"level\":\"error\",\"message\":\"test\"}\u2060"},
		{"trailing text", "{\"level\":\"error\",\"message\":\"test\"} some trailing text"},
		{"trailing newline and text", "{\"level\":\"error\",\"message\":\"test\"}\nextra"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := TryFormatJSON(tc.input)
			assert.NotEmpty(t, result, "JSON with %s should be formatted", tc.name)
		})
	}
}

// Test various ANSI escape sequences
func TestStripAnsi_CSI(t *testing.T) {
	// Standard CSI (Control Sequence Introducer) - colors
	input := "\x1b[31mhello\x1b[0m"
	result := stripAnsi(input)
	assert.Equal(t, "hello", result)
}

func TestStripAnsi_SGR(t *testing.T) {
	// SGR (Select Graphic Rendition) - bold, underline, etc.
	input := "\x1b[1;31;40mhello\x1b[0m"
	result := stripAnsi(input)
	assert.Equal(t, "hello", result)
}

func TestStripAnsi_CursorMovement(t *testing.T) {
	// Cursor movement sequences
	input := "\x1b[2Jhello\x1b[H" // Clear screen, text, home cursor
	result := stripAnsi(input)
	assert.Equal(t, "hello", result)
}

func TestStripAnsi_OSC(t *testing.T) {
	// OSC (Operating System Command) - set title, etc.
	// OSC ends with BEL (\x07) or ST (\x1b\\)
	input := "\x1b]0;Title\x07hello"
	result := stripAnsi(input)
	// BUG: This will NOT work correctly! OSC doesn't end with a letter.
	// The current stripAnsi will eat "itle" (until 'T' which is a letter)
	// then output "itle\x07hello" - wait no, let me trace through:
	// \x1b -> inEscape=true
	// ] -> skip (not letter)
	// 0 -> skip (not letter)
	// ; -> skip (not letter)
	// T -> inEscape=false, skip (IS a letter!)
	// i -> output
	// t -> output
	// l -> output
	// e -> output
	// \x07 -> output (BEL character!)
	// h -> output
	// ... etc
	// Result would be "itle\x07hello" - WRONG!
	t.Logf("OSC result: %q", result)
	// This test documents the bug in stripAnsi
}

func TestStripAnsi_IncompleteSequence(t *testing.T) {
	// Incomplete escape sequence at end of string
	input := "hello\x1b[31"
	result := stripAnsi(input)
	// inEscape stays true, rest is eaten
	assert.Equal(t, "hello", result)
}

func TestStripAnsi_EscapeInJSON(t *testing.T) {
	// What if JSON string contains escape character?
	// JSON would have it escaped as \u001b, not literal \x1b
	// So this shouldn't be an issue in practice
	input := "{\"msg\":\"hello\x1b[31mworld\x1b[0m\"}"
	result := stripAnsi(input)
	assert.Equal(t, "{\"msg\":\"helloworld\"}", result)
}

// Debug test to show what's happening
func TestDebug_InspectBytes(t *testing.T) {
	input := `{"level":"error","message":"test"}`
	t.Logf("Input bytes: %v", []byte(input))
	t.Logf("Input length: %d", len(input))
	t.Logf("First char: %q (0x%02x)", input[0], input[0])
	t.Logf("Last char: %q (0x%02x)", input[len(input)-1], input[len(input)-1])

	cleaned := stripAnsi(input)
	t.Logf("After stripAnsi: %q", cleaned)

	trimmed := strings.TrimSpace(cleaned)
	t.Logf("After TrimSpace: %q", trimmed)
	t.Logf("Trimmed first char: %q (0x%02x)", trimmed[0], trimmed[0])
	t.Logf("Trimmed last char: %q (0x%02x)", trimmed[len(trimmed)-1], trimmed[len(trimmed)-1])

	result := TryFormatJSON(trimmed)
	t.Logf("TryFormatJSON result empty: %v", result == "")
}
