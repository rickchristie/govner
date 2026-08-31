package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigBarrelEnvVarsEmpty(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.BarrelEnvVars == nil {
		t.Fatal("expected BarrelEnvVars to be an empty slice, not nil")
	}
	if len(cfg.BarrelEnvVars) != 0 {
		t.Fatalf("len(BarrelEnvVars) = %d, want 0", len(cfg.BarrelEnvVars))
	}
}

func TestLoadConfigMissingBarrelEnvVarsBackwardCompatible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"programming_tools":[],"ai_tools":[],"proxy_port":3128,"bridge_port":4343}`), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}
	if loaded.BarrelEnvVars == nil {
		t.Fatal("expected missing barrel_env_vars to load as an empty slice, not nil")
	}
	if len(loaded.BarrelEnvVars) != 0 {
		t.Fatalf("len(BarrelEnvVars) = %d, want 0", len(loaded.BarrelEnvVars))
	}
}

func TestCloneConfigDeepCopiesBarrelEnvVars(t *testing.T) {
	original := DefaultConfig()
	original.BarrelEnvVars = []BarrelEnvVar{{Name: "FOO", Value: "a"}}

	clone := CloneConfig(original)
	clone.BarrelEnvVars[0].Value = "b"

	if got := original.BarrelEnvVars[0].Value; got != "a" {
		t.Fatalf("original BarrelEnvVars[0].Value = %q, want %q", got, "a")
	}
}

func TestSaveLoadConfigPreservesBarrelEnvVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := DefaultConfig()
	original.BarrelEnvVars = []BarrelEnvVar{
		{Name: "A", Value: "1"},
		{Name: "B", Value: "two words"},
		{Name: "C", Value: ""},
	}

	if err := SaveConfig(path, original); err != nil {
		t.Fatalf("SaveConfig() failed: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}
	if len(loaded.BarrelEnvVars) != len(original.BarrelEnvVars) {
		t.Fatalf("len(loaded.BarrelEnvVars) = %d, want %d", len(loaded.BarrelEnvVars), len(original.BarrelEnvVars))
	}
	for i := range original.BarrelEnvVars {
		if loaded.BarrelEnvVars[i] != original.BarrelEnvVars[i] {
			t.Fatalf("loaded.BarrelEnvVars[%d] = %+v, want %+v", i, loaded.BarrelEnvVars[i], original.BarrelEnvVars[i])
		}
	}
}

func TestValidateBarrelEnvVarsRejectsEmptyName(t *testing.T) {
	err := ValidateBarrelEnvVars([]BarrelEnvVar{{Name: "", Value: "x"}})
	if err == nil {
		t.Fatal("expected error for empty barrel env name")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "barrel env") || !strings.Contains(strings.ToLower(err.Error()), "name") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestCanonicalizeBarrelEnvVarsTrimsNameWithoutMutating(t *testing.T) {
	input := []BarrelEnvVar{{Name: "  FOO  ", Value: "x"}}
	canonical := CanonicalizeBarrelEnvVars(input)

	if got := canonical[0].Name; got != "FOO" {
		t.Fatalf("canonical name = %q, want %q", got, "FOO")
	}
	if got := input[0].Name; got != "  FOO  " {
		t.Fatalf("input name mutated to %q", got)
	}
}

func TestValidateBarrelEnvVarsValidatesTrimmedNameWithoutMutating(t *testing.T) {
	input := []BarrelEnvVar{{Name: "  FOO  ", Value: "x"}}
	if err := ValidateBarrelEnvVars(input); err != nil {
		t.Fatalf("ValidateBarrelEnvVars() failed: %v", err)
	}
	if got := input[0].Name; got != "  FOO  " {
		t.Fatalf("input name mutated to %q", got)
	}
}

func TestValidateBarrelEnvVarsRejectsMalformedNames(t *testing.T) {
	for _, name := range []string{"1BAD", "BAD-NAME", "BAD NAME", "BAD=NAME", "BAD.NAME"} {
		t.Run(name, func(t *testing.T) {
			err := ValidateBarrelEnvVars([]BarrelEnvVar{{Name: name, Value: "x"}})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("expected error to mention %q, got %v", name, err)
			}
		})
	}
}

func TestValidateBarrelEnvVarsRejectsCaseInsensitiveDuplicates(t *testing.T) {
	err := ValidateBarrelEnvVars([]BarrelEnvVar{{Name: "FOO", Value: "1"}, {Name: "foo", Value: "2"}})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Fatalf("expected duplicate error text, got %v", err)
	}
}

func TestValidateBarrelEnvVarsRejectsProtectedNames(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "COOPER_PROXY_HOST", "COOPER_ANYTHING", "OPENAI_API_KEY", "TERM_PROGRAM", "COLORTERM", "COLORFGBG", "WEZTERM_PANE", "KITTY_WINDOW_ID", "VTE_VERSION", "NO_COLOR", "FORCE_COLOR", "FORCE_HYPERLINK", "CLICOLOR_FORCE", "PATH", "TZ"} {
		t.Run(name, func(t *testing.T) {
			err := ValidateBarrelEnvVars([]BarrelEnvVar{{Name: name, Value: "x"}})
			if err == nil {
				t.Fatal("expected protected-name error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "protected") {
				t.Fatalf("expected protected-name error, got %v", err)
			}
		})
	}
}

func TestValidateBarrelEnvVarsAllowsEmptyValue(t *testing.T) {
	if err := ValidateBarrelEnvVars([]BarrelEnvVar{{Name: "EMPTY", Value: ""}}); err != nil {
		t.Fatalf("ValidateBarrelEnvVars() failed: %v", err)
	}
}

func TestValidateBarrelEnvVarsRejectsInvalidValueBytes(t *testing.T) {
	for _, value := range []string{"line1\nline2", "line1\rline2", "abc\x00def"} {
		t.Run(strings.ReplaceAll(value, "\x00", "NUL"), func(t *testing.T) {
			err := ValidateBarrelEnvVars([]BarrelEnvVar{{Name: "BAD", Value: value}})
			if err == nil {
				t.Fatal("expected invalid-value error")
			}
		})
	}
}

func TestNormalizeBarrelEnvVarsForRuntimeSkipsInvalidEntries(t *testing.T) {
	usable, warnings := NormalizeBarrelEnvVarsForRuntime([]BarrelEnvVar{
		{Name: "GOOD", Value: "1"},
		{Name: "HTTP_PROXY", Value: "http://bad"},
		{Name: "BAD-NAME", Value: "x"},
		{Name: "ALSO_GOOD", Value: "2"},
	})

	if len(usable) != 2 {
		t.Fatalf("len(usable) = %d, want 2", len(usable))
	}
	if usable[0].Name != "GOOD" || usable[1].Name != "ALSO_GOOD" {
		t.Fatalf("usable = %+v, want GOOD then ALSO_GOOD", usable)
	}
	if len(warnings) != 2 {
		t.Fatalf("len(warnings) = %d, want 2", len(warnings))
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "HTTP_PROXY") || !strings.Contains(joined, "BAD-NAME") {
		t.Fatalf("warnings = %v, want HTTP_PROXY and BAD-NAME", warnings)
	}
}

func TestGrokProtectedEnvOnlyAtRuntimeForGrok(t *testing.T) {
	if err := ValidateBarrelEnvVars([]BarrelEnvVar{{Name: "GROK_WEB_FETCH", Value: "1"}, {Name: "XAI_API_KEY", Value: "k"}}); err != nil {
		t.Fatalf("configure-time validation should allow GROK_* and XAI_API_KEY: %v", err)
	}
	if IsProtectedBarrelEnvNameForTool("GROK_WEB_FETCH", "grok") {
		t.Fatal("GROK_WEB_FETCH should remain user-controlled for grok")
	}
	if IsProtectedBarrelEnvNameForTool("XAI_API_KEY", "grok") {
		t.Fatal("XAI_API_KEY should remain user-controlled for grok")
	}
	if !IsProtectedBarrelEnvNameForTool("GROK_HOME", "grok") {
		t.Fatal("GROK_HOME should be protected for grok")
	}
	if !IsProtectedBarrelEnvNameForTool("GROK_LEADER_SOCKET", "grok") {
		t.Fatal("GROK_LEADER_SOCKET should be protected for grok")
	}

	usable, warnings := NormalizeBarrelEnvVarsForRuntimeForTool([]BarrelEnvVar{
		{Name: "GROK_WEB_FETCH", Value: "1"},
		{Name: "PROJECT_TOKEN", Value: "ok"},
	}, "grok")
	if len(usable) != 2 || usable[0].Name != "GROK_WEB_FETCH" || usable[1].Name != "PROJECT_TOKEN" {
		t.Fatalf("usable = %+v", usable)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}

	usable, warnings = NormalizeBarrelEnvVarsForRuntimeForTool([]BarrelEnvVar{
		{Name: "GROK_HOME", Value: "/tmp/not-the-mounted-root"},
	}, "grok")
	if len(usable) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "GROK_HOME") {
		t.Fatalf("GROK_HOME filter = %+v warnings=%v", usable, warnings)
	}

	usable, warnings = NormalizeBarrelEnvVarsForRuntimeForTool([]BarrelEnvVar{
		{Name: "GROK_LEADER_SOCKET", Value: "/home/user/.grok/leader.sock"},
	}, "grok")
	if len(usable) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "GROK_LEADER_SOCKET") {
		t.Fatalf("GROK_LEADER_SOCKET filter = %+v warnings=%v", usable, warnings)
	}

	usable, warnings = NormalizeBarrelEnvVarsForRuntimeForTool([]BarrelEnvVar{
		{Name: "GROK_WEB_FETCH", Value: "1"},
	}, "custom-xai")
	if len(usable) != 1 || usable[0].Name != "GROK_WEB_FETCH" {
		t.Fatalf("custom tool should keep GROK_WEB_FETCH, got %+v warnings=%v", usable, warnings)
	}
	usable, warnings = NormalizeBarrelEnvVarsForRuntimeForTool([]BarrelEnvVar{
		{Name: "GROK_LEADER_SOCKET", Value: "/custom/leader.sock"},
	}, "custom-xai")
	if len(usable) != 1 || usable[0].Name != "GROK_LEADER_SOCKET" {
		t.Fatalf("custom tool should keep GROK_LEADER_SOCKET, got %+v warnings=%v", usable, warnings)
	}
}

func TestNormalizeBarrelEnvVarsForRuntimePreservesDuplicateOrder(t *testing.T) {
	usable, warnings := NormalizeBarrelEnvVarsForRuntime([]BarrelEnvVar{{Name: "FOO", Value: "1"}, {Name: "FOO", Value: "2"}})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(usable) != 2 {
		t.Fatalf("len(usable) = %d, want 2", len(usable))
	}
	if usable[0].Value != "1" || usable[1].Value != "2" {
		t.Fatalf("usable order = %+v, want FOO=1 then FOO=2", usable)
	}
}

func TestSaveLoadJSONRoundTripWithBarrelEnvVars(t *testing.T) {
	original := DefaultConfig()
	original.BarrelEnvVars = []BarrelEnvVar{{Name: "FOO", Value: "bar"}, {Name: "EMPTY", Value: ""}}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() failed: %v", err)
	}

	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal() failed: %v", err)
	}
	if strings.Join([]string{restored.BarrelEnvVars[0].Name, restored.BarrelEnvVars[1].Name}, ",") != "FOO,EMPTY" {
		t.Fatalf("restored order = %+v", restored.BarrelEnvVars)
	}
}
