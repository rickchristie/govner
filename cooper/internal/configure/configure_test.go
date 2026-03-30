package configure

import (
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

// ---------------------------------------------------------------------------
// textInput.handleKey tests
// ---------------------------------------------------------------------------

func TestTextInput_TypeLetters(t *testing.T) {
	ti := newTextInput("placeholder", 30)
	ti.Focus()

	for _, ch := range "hello" {
		ti.handleKey(string(ch))
	}

	if ti.Value() != "hello" {
		t.Errorf("expected %q, got %q", "hello", ti.Value())
	}
	if ti.cursorPos != 5 {
		t.Errorf("expected cursorPos=5, got %d", ti.cursorPos)
	}
}

func TestTextInput_Backspace(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("abc")

	ti.handleKey("backspace")
	if ti.Value() != "ab" {
		t.Errorf("expected %q after backspace, got %q", "ab", ti.Value())
	}
	if ti.cursorPos != 2 {
		t.Errorf("expected cursorPos=2, got %d", ti.cursorPos)
	}
}

func TestTextInput_BackspaceAtStart(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("abc")
	ti.cursorPos = 0

	ti.handleKey("backspace")
	if ti.Value() != "abc" {
		t.Errorf("backspace at start should be no-op, got %q", ti.Value())
	}
	if ti.cursorPos != 0 {
		t.Errorf("expected cursorPos=0, got %d", ti.cursorPos)
	}
}

func TestTextInput_BackspaceEmpty(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()

	handled := ti.handleKey("backspace")
	if !handled {
		t.Error("expected backspace to be handled even on empty input")
	}
	if ti.Value() != "" {
		t.Errorf("expected empty, got %q", ti.Value())
	}
}

func TestTextInput_Delete(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("abc")
	ti.cursorPos = 1

	ti.handleKey("delete")
	if ti.Value() != "ac" {
		t.Errorf("expected %q after delete, got %q", "ac", ti.Value())
	}
	if ti.cursorPos != 1 {
		t.Errorf("expected cursorPos=1, got %d", ti.cursorPos)
	}
}

func TestTextInput_DeleteAtEnd(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("abc")

	ti.handleKey("delete")
	if ti.Value() != "abc" {
		t.Errorf("delete at end should be no-op, got %q", ti.Value())
	}
}

func TestTextInput_LeftRight(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("abcde")

	// Cursor starts at end (5). Move left twice.
	ti.handleKey("left")
	ti.handleKey("left")
	if ti.cursorPos != 3 {
		t.Errorf("expected cursorPos=3 after 2 lefts, got %d", ti.cursorPos)
	}

	// Move right once.
	ti.handleKey("right")
	if ti.cursorPos != 4 {
		t.Errorf("expected cursorPos=4 after right, got %d", ti.cursorPos)
	}
}

func TestTextInput_LeftAtStart(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("abc")
	ti.cursorPos = 0

	ti.handleKey("left")
	if ti.cursorPos != 0 {
		t.Errorf("left at start should be no-op, got cursorPos=%d", ti.cursorPos)
	}
}

func TestTextInput_RightAtEnd(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("abc")

	ti.handleKey("right")
	if ti.cursorPos != 3 {
		t.Errorf("right at end should be no-op, got cursorPos=%d", ti.cursorPos)
	}
}

func TestTextInput_Home(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("hello")

	ti.handleKey("home")
	if ti.cursorPos != 0 {
		t.Errorf("expected cursorPos=0 after home, got %d", ti.cursorPos)
	}
}

func TestTextInput_End(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("hello")
	ti.cursorPos = 2

	ti.handleKey("end")
	if ti.cursorPos != 5 {
		t.Errorf("expected cursorPos=5 after end, got %d", ti.cursorPos)
	}
}

func TestTextInput_CtrlA(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("hello")

	ti.handleKey("ctrl+a")
	if ti.cursorPos != 0 {
		t.Errorf("expected cursorPos=0 after ctrl+a, got %d", ti.cursorPos)
	}
}

func TestTextInput_CtrlE(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("hello")
	ti.cursorPos = 0

	ti.handleKey("ctrl+e")
	if ti.cursorPos != 5 {
		t.Errorf("expected cursorPos=5 after ctrl+e, got %d", ti.cursorPos)
	}
}

func TestTextInput_InsertMiddle(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("ac")
	ti.cursorPos = 1

	ti.handleKey("b")
	if ti.Value() != "abc" {
		t.Errorf("expected %q after inserting 'b' at pos 1, got %q", "abc", ti.Value())
	}
	if ti.cursorPos != 2 {
		t.Errorf("expected cursorPos=2, got %d", ti.cursorPos)
	}
}

func TestTextInput_BackspaceMiddle(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("abcd")
	ti.cursorPos = 2

	ti.handleKey("backspace")
	if ti.Value() != "acd" {
		t.Errorf("expected %q, got %q", "acd", ti.Value())
	}
	if ti.cursorPos != 1 {
		t.Errorf("expected cursorPos=1, got %d", ti.cursorPos)
	}
}

func TestTextInput_NonPrintableIgnored(t *testing.T) {
	ti := newTextInput("", 30)
	ti.Focus()
	ti.SetValue("abc")

	handled := ti.handleKey("ctrl+z")
	if handled {
		t.Error("expected ctrl+z to not be handled")
	}
	if ti.Value() != "abc" {
		t.Errorf("expected value unchanged, got %q", ti.Value())
	}
}

func TestTextInput_FocusBlur(t *testing.T) {
	ti := newTextInput("placeholder", 30)
	if ti.focused {
		t.Error("expected unfocused by default")
	}

	ti.Focus()
	if !ti.focused {
		t.Error("expected focused after Focus()")
	}

	ti.Blur()
	if ti.focused {
		t.Error("expected unfocused after Blur()")
	}
}

func TestTextInput_SetValue(t *testing.T) {
	ti := newTextInput("", 30)
	ti.SetValue("hello world")
	if ti.Value() != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", ti.Value())
	}
	if ti.cursorPos != 11 {
		t.Errorf("expected cursorPos=11 after SetValue, got %d", ti.cursorPos)
	}
}

// ---------------------------------------------------------------------------
// newProgrammingModel tests
// ---------------------------------------------------------------------------

func TestNewProgrammingModel_NoExisting_AutoEnable(t *testing.T) {
	// When no existing config is provided, tools with detected host versions
	// should be auto-enabled with mirror mode.
	m := newProgrammingModel(nil)

	// All tools should start with mode mirror if they have a host version.
	for _, tool := range m.tools {
		if tool.hostVersion != "" {
			if !tool.enabled {
				t.Errorf("tool %q has hostVersion=%q but is not auto-enabled", tool.name, tool.hostVersion)
			}
			if tool.mode != config.ModeMirror {
				t.Errorf("tool %q has hostVersion but mode=%v, want mirror", tool.name, tool.mode)
			}
		}
	}
}

func TestNewProgrammingModel_ExistingConfig_Preserved(t *testing.T) {
	existing := []config.ToolConfig{
		{Name: "go", Enabled: true, Mode: config.ModePin, PinnedVersion: "1.22.0", HostVersion: "1.23.0"},
		{Name: "node", Enabled: false, Mode: config.ModeOff},
	}

	m := newProgrammingModel(existing)

	// Find go tool.
	var goTool *toolEntry
	for i := range m.tools {
		if m.tools[i].name == "go" {
			goTool = &m.tools[i]
			break
		}
	}
	if goTool == nil {
		t.Fatal("expected go tool in model")
	}
	if !goTool.enabled {
		t.Error("expected go to be enabled per existing config")
	}
	if goTool.mode != config.ModePin {
		t.Errorf("expected go mode=pin, got %v", goTool.mode)
	}
	if goTool.pinVersion != "1.22.0" {
		t.Errorf("expected go pinVersion=1.22.0, got %q", goTool.pinVersion)
	}

	// Find node tool.
	var nodeTool *toolEntry
	for i := range m.tools {
		if m.tools[i].name == "node" {
			nodeTool = &m.tools[i]
			break
		}
	}
	if nodeTool == nil {
		t.Fatal("expected node tool in model")
	}
	if nodeTool.enabled {
		t.Error("expected node to be disabled per existing config")
	}
}

func TestNewProgrammingModel_DefaultToolList(t *testing.T) {
	m := newProgrammingModel(nil)

	expected := map[string]bool{"go": true, "node": true, "python": true, "rust": true}
	for _, tool := range m.tools {
		if _, ok := expected[tool.name]; !ok {
			t.Errorf("unexpected tool %q in default programming tools", tool.name)
		}
		delete(expected, tool.name)
	}
	for name := range expected {
		t.Errorf("missing expected tool %q in default programming tools", name)
	}
}

// ---------------------------------------------------------------------------
// newAICLIModel tests
// ---------------------------------------------------------------------------

func TestNewAICLIModel_NoExisting_AutoEnable(t *testing.T) {
	m := newAICLIModel(nil)

	for _, tool := range m.tools {
		if tool.hostVersion != "" {
			if !tool.enabled {
				t.Errorf("AI tool %q has hostVersion=%q but is not auto-enabled", tool.name, tool.hostVersion)
			}
			if tool.mode != config.ModeMirror {
				t.Errorf("AI tool %q has hostVersion but mode=%v, want mirror", tool.name, tool.mode)
			}
		}
	}
}

func TestNewAICLIModel_ExistingConfig_Preserved(t *testing.T) {
	existing := []config.ToolConfig{
		{Name: "claude", Enabled: true, Mode: config.ModeLatest},
		{Name: "codex", Enabled: false, Mode: config.ModeOff},
	}

	m := newAICLIModel(existing)

	var claudeTool *toolEntry
	for i := range m.tools {
		if m.tools[i].name == "claude" {
			claudeTool = &m.tools[i]
			break
		}
	}
	if claudeTool == nil {
		t.Fatal("expected claude tool in model")
	}
	if !claudeTool.enabled {
		t.Error("expected claude to be enabled per existing config")
	}
	if claudeTool.mode != config.ModeLatest {
		t.Errorf("expected claude mode=latest, got %v", claudeTool.mode)
	}

	var codexTool *toolEntry
	for i := range m.tools {
		if m.tools[i].name == "codex" {
			codexTool = &m.tools[i]
			break
		}
	}
	if codexTool == nil {
		t.Fatal("expected codex tool in model")
	}
	if codexTool.enabled {
		t.Error("expected codex to be disabled per existing config")
	}
}

func TestNewAICLIModel_DefaultToolList(t *testing.T) {
	m := newAICLIModel(nil)

	expected := map[string]bool{"claude": true, "copilot": true, "codex": true, "opencode": true}
	for _, tool := range m.tools {
		if _, ok := expected[tool.name]; !ok {
			t.Errorf("unexpected tool %q in default AI tools", tool.name)
		}
		delete(expected, tool.name)
	}
	for name := range expected {
		t.Errorf("missing expected tool %q in default AI tools", name)
	}
}

// ---------------------------------------------------------------------------
// overlayModal tests
// ---------------------------------------------------------------------------

func TestOverlayModal_ResultHasCorrectHeight(t *testing.T) {
	bg := strings.Repeat("background line\n", 20)
	bg = strings.TrimSuffix(bg, "\n")
	modal := "modal line 1\nmodal line 2\nmodal line 3"

	result := overlayModal(bg, modal, 80, 24)
	lines := strings.Split(result, "\n")

	if len(lines) != 24 {
		t.Errorf("expected 24 lines, got %d", len(lines))
	}
}

func TestOverlayModal_SmallTerminal(t *testing.T) {
	bg := "small bg"
	modal := "m1\nm2"

	result := overlayModal(bg, modal, 30, 5)
	lines := strings.Split(result, "\n")

	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
}

func TestOverlayModal_ModalLargerThanBg(t *testing.T) {
	bg := "short"
	modal := "line1\nline2\nline3\nline4\nline5"

	result := overlayModal(bg, modal, 40, 6)
	lines := strings.Split(result, "\n")

	if len(lines) != 6 {
		t.Errorf("expected 6 lines, got %d", len(lines))
	}
}

// ---------------------------------------------------------------------------
// splitAndPad tests
// ---------------------------------------------------------------------------

func TestSplitAndPad_PadsBothDimensions(t *testing.T) {
	lines := splitAndPad("ab\ncd", 10, 5)
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
	// Each line should be at least 10 visible chars wide (plain text, no ANSI).
	for i, line := range lines {
		if len(line) < 10 {
			t.Errorf("line %d length %d < 10: %q", i, len(line), line)
		}
	}
}

func TestSplitAndPad_EmptyInput(t *testing.T) {
	lines := splitAndPad("", 5, 3)
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
	for _, line := range lines {
		if len(line) != 5 {
			t.Errorf("expected line of length 5, got %d", len(line))
		}
	}
}

// ---------------------------------------------------------------------------
// stripAnsi tests
// ---------------------------------------------------------------------------

func TestStripAnsi_PlainText(t *testing.T) {
	result := stripAnsi("hello world")
	if result != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", result)
	}
}

func TestStripAnsi_WithEscapes(t *testing.T) {
	// Simulate a basic ANSI escape: ESC[31m (red) ... ESC[0m (reset).
	input := "\x1b[31mred text\x1b[0m"
	result := stripAnsi(input)
	if result != "red text" {
		t.Errorf("expected %q, got %q", "red text", result)
	}
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestResolvedVersion(t *testing.T) {
	tests := []struct {
		name string
		tool toolEntry
		want string
	}{
		{
			name: "mirror with host version",
			tool: toolEntry{mode: config.ModeMirror, hostVersion: "1.22.5"},
			want: "1.22.5",
		},
		{
			name: "mirror without host version",
			tool: toolEntry{mode: config.ModeMirror, hostVersion: ""},
			want: "\u2500", // theme.BorderH
		},
		{
			name: "pin with version",
			tool: toolEntry{mode: config.ModePin, pinVersion: "1.23.0"},
			want: "1.23.0",
		},
		{
			name: "latest",
			tool: toolEntry{mode: config.ModeLatest},
			want: "latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvedVersion(tt.tool)
			if got != tt.want {
				t.Errorf("resolvedVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModeToIndex(t *testing.T) {
	if modeToIndex(config.ModeMirror) != 0 {
		t.Error("ModeMirror should map to 0")
	}
	if modeToIndex(config.ModeLatest) != 1 {
		t.Error("ModeLatest should map to 1")
	}
	if modeToIndex(config.ModePin) != 2 {
		t.Error("ModePin should map to 2")
	}
	if modeToIndex(config.ModeOff) != 0 {
		t.Error("ModeOff should default to 0")
	}
}

func TestAiModeToIndex(t *testing.T) {
	// AI tools: Latest=0, Mirror=1, Pin=2.
	if aiModeToIndex(config.ModeLatest) != 0 {
		t.Error("ModeLatest should map to 0 for AI tools")
	}
	if aiModeToIndex(config.ModeMirror) != 1 {
		t.Error("ModeMirror should map to 1 for AI tools")
	}
	if aiModeToIndex(config.ModePin) != 2 {
		t.Error("ModePin should map to 2 for AI tools")
	}
}

func TestModeMatchesIndex(t *testing.T) {
	if !modeMatchesIndex(config.ModeMirror, 0) {
		t.Error("ModeMirror should match index 0")
	}
	if modeMatchesIndex(config.ModeLatest, 0) {
		t.Error("ModeLatest should not match index 0")
	}
}

func TestAiModeMatchesIndex(t *testing.T) {
	if !aiModeMatchesIndex(config.ModeLatest, 0) {
		t.Error("ModeLatest should match index 0 for AI")
	}
	if aiModeMatchesIndex(config.ModeMirror, 0) {
		t.Error("ModeMirror should not match index 0 for AI")
	}
}

func TestToToolConfigs_Programming(t *testing.T) {
	m := programmingModel{
		tools: []toolEntry{
			{name: "go", enabled: true, mode: config.ModePin, pinVersion: "1.22.5", hostVersion: "1.23.0"},
			{name: "node", enabled: false, mode: config.ModeLatest, hostVersion: "22.0.0"},
		},
	}

	configs := m.toToolConfigs()
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}

	goConfig := configs[0]
	if goConfig.Name != "go" || !goConfig.Enabled || goConfig.Mode != config.ModePin {
		t.Errorf("unexpected go config: %+v", goConfig)
	}
	if goConfig.PinnedVersion != "1.22.5" {
		t.Errorf("expected PinnedVersion=1.22.5, got %q", goConfig.PinnedVersion)
	}

	nodeConfig := configs[1]
	if nodeConfig.Name != "node" || nodeConfig.Enabled {
		t.Errorf("unexpected node config: %+v", nodeConfig)
	}
	// ModeLatest should not set PinnedVersion.
	if nodeConfig.PinnedVersion != "" {
		t.Errorf("expected empty PinnedVersion for latest mode, got %q", nodeConfig.PinnedVersion)
	}
}

func TestToToolConfigs_AICLI(t *testing.T) {
	m := aicliModel{
		tools: []toolEntry{
			{name: "claude", enabled: true, mode: config.ModeLatest, hostVersion: "2.0.0"},
			{name: "copilot", enabled: true, mode: config.ModePin, pinVersion: "0.7.2"},
		},
	}

	configs := m.toToolConfigs()
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}

	claudeConfig := configs[0]
	if claudeConfig.PinnedVersion != "" {
		t.Errorf("claude in latest mode should not have PinnedVersion, got %q", claudeConfig.PinnedVersion)
	}

	copilotConfig := configs[1]
	if copilotConfig.PinnedVersion != "0.7.2" {
		t.Errorf("copilot in pin mode should have PinnedVersion=0.7.2, got %q", copilotConfig.PinnedVersion)
	}
}

func TestDisplayOrDash(t *testing.T) {
	if displayOrDash("1.22") != "1.22" {
		t.Error("non-empty string should return as-is")
	}
	if displayOrDash("") != "\u2500" {
		t.Error("empty string should return dash")
	}
}

func TestRepeatStr(t *testing.T) {
	if repeatStr("ab", 3) != "ababab" {
		t.Errorf("expected %q, got %q", "ababab", repeatStr("ab", 3))
	}
	if repeatStr("x", 0) != "" {
		t.Error("repeatStr with n=0 should return empty")
	}
}

func TestMin(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("min(3,5) should be 3")
	}
	if min(5, 3) != 3 {
		t.Error("min(5,3) should be 3")
	}
	if min(4, 4) != 4 {
		t.Error("min(4,4) should be 4")
	}
}
