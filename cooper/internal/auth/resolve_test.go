package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-env-key")

	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{"codex"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	found := findToken(results, "OPENAI_API_KEY")
	if found == nil {
		t.Fatal("expected OPENAI_API_KEY in results")
	}
	if found.Value != "sk-test-env-key" {
		t.Errorf("got value %q, want %q", found.Value, "sk-test-env-key")
	}
	if found.Source != "env" {
		t.Errorf("got source %q, want %q", found.Source, "env")
	}
}

func TestResolveFromEnv_GH_TOKEN(t *testing.T) {
	t.Setenv("GH_TOKEN", "ghp-test-token")

	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{"copilot"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	found := findToken(results, "GH_TOKEN")
	if found == nil {
		t.Fatal("expected GH_TOKEN in results")
	}
	if found.Value != "ghp-test-token" {
		t.Errorf("got value %q, want %q", found.Value, "ghp-test-token")
	}
	if found.Source != "env" {
		t.Errorf("got source %q, want %q", found.Source, "env")
	}
}

func TestResolveFromEnv_GITHUB_TOKEN_Fallback(t *testing.T) {
	// GH_TOKEN not set, should fall back to GITHUB_TOKEN.
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "ghp-fallback-token")

	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{"copilot"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	found := findToken(results, "GH_TOKEN")
	if found == nil {
		t.Fatal("expected GH_TOKEN in results")
	}
	if found.Value != "ghp-fallback-token" {
		t.Errorf("got value %q, want %q", found.Value, "ghp-fallback-token")
	}
}

func TestResolveFromCache(t *testing.T) {
	cooperDir := t.TempDir()
	wsHash := WorkspaceHash("/tmp/test-workspace")

	// Pre-populate cache.
	err := CacheTokens(cooperDir, wsHash, []TokenResult{
		{Name: "OPENAI_API_KEY", Value: "sk-cached-key", Source: "cache"},
	})
	if err != nil {
		t.Fatalf("CacheTokens: %v", err)
	}

	// Ensure env var is not set so cache is used.
	t.Setenv("OPENAI_API_KEY", "")

	results, err := ResolveTokens("/tmp/test-workspace", cooperDir, []string{"codex"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	found := findToken(results, "OPENAI_API_KEY")
	if found == nil {
		t.Fatal("expected OPENAI_API_KEY in results")
	}
	if found.Value != "sk-cached-key" {
		t.Errorf("got value %q, want %q", found.Value, "sk-cached-key")
	}
	if found.Source != "cache" {
		t.Errorf("got source %q, want %q", found.Source, "cache")
	}
}

func TestWorkspaceHash_Consistent(t *testing.T) {
	hash1 := WorkspaceHash("/home/user/project")
	hash2 := WorkspaceHash("/home/user/project")
	if hash1 != hash2 {
		t.Errorf("hash not consistent: %q != %q", hash1, hash2)
	}
	if len(hash1) != 8 {
		t.Errorf("hash length %d, want 8", len(hash1))
	}
}

func TestWorkspaceHash_DifferentPaths(t *testing.T) {
	hash1 := WorkspaceHash("/home/user/project-a")
	hash2 := WorkspaceHash("/home/user/project-b")
	if hash1 == hash2 {
		t.Error("different paths should produce different hashes")
	}
}

func TestCacheTokens_FilePermissions(t *testing.T) {
	cooperDir := t.TempDir()
	wsHash := "abcd1234"

	err := CacheTokens(cooperDir, wsHash, []TokenResult{
		{Name: "TEST_KEY", Value: "test-value", Source: "shell"},
	})
	if err != nil {
		t.Fatalf("CacheTokens: %v", err)
	}

	path := filepath.Join(cooperDir, "secrets", wsHash)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat cache file: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("got permissions %o, want 0600", perm)
	}
}

func TestCacheTokens_SecretsDir_Permissions(t *testing.T) {
	cooperDir := t.TempDir()
	wsHash := "abcd1234"

	err := CacheTokens(cooperDir, wsHash, []TokenResult{
		{Name: "TEST_KEY", Value: "test-value", Source: "shell"},
	})
	if err != nil {
		t.Fatalf("CacheTokens: %v", err)
	}

	secretsDir := filepath.Join(cooperDir, "secrets")
	info, err := os.Stat(secretsDir)
	if err != nil {
		t.Fatalf("stat secrets dir: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0700 {
		t.Errorf("got secrets dir permissions %o, want 0700", perm)
	}
}

func TestCacheTokens_RoundTrip(t *testing.T) {
	cooperDir := t.TempDir()
	wsHash := "roundtrip"

	original := []TokenResult{
		{Name: "KEY_A", Value: "value-a", Source: "shell"},
		{Name: "KEY_B", Value: "value-b", Source: "env"},
	}

	err := CacheTokens(cooperDir, wsHash, original)
	if err != nil {
		t.Fatalf("CacheTokens: %v", err)
	}

	loaded, err := LoadCachedTokens(cooperDir, wsHash)
	if err != nil {
		t.Fatalf("LoadCachedTokens: %v", err)
	}

	// Build maps for comparison (order may differ).
	origMap := make(map[string]string)
	for _, t := range original {
		origMap[t.Name] = t.Value
	}
	loadedMap := make(map[string]string)
	for _, t := range loaded {
		loadedMap[t.Name] = t.Value
	}

	for k, v := range origMap {
		if loadedMap[k] != v {
			t.Errorf("key %q: got %q, want %q", k, loadedMap[k], v)
		}
	}
	if len(loadedMap) != len(origMap) {
		t.Errorf("loaded %d tokens, want %d", len(loadedMap), len(origMap))
	}
}

func TestCacheTokens_JSONFormat(t *testing.T) {
	cooperDir := t.TempDir()
	wsHash := "jsontest"

	err := CacheTokens(cooperDir, wsHash, []TokenResult{
		{Name: "MY_KEY", Value: "my-value", Source: "shell"},
	})
	if err != nil {
		t.Fatalf("CacheTokens: %v", err)
	}

	path := filepath.Join(cooperDir, "secrets", wsHash)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var store map[string]string
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatalf("cache file is not valid JSON: %v", err)
	}

	if store["MY_KEY"] != "my-value" {
		t.Errorf("got %q, want %q", store["MY_KEY"], "my-value")
	}
}

func TestLoadCachedTokens_MissingFile(t *testing.T) {
	cooperDir := t.TempDir()
	_, err := LoadCachedTokens(cooperDir, "nonexistent")
	if err == nil {
		t.Error("expected error for missing cache file, got nil")
	}
}

func TestCLAUDECODE_NotForwarded(t *testing.T) {
	// CLAUDECODE must never appear in forwarded env vars.
	// Verify it is not in the vsCodeEnvVars list.
	for _, name := range vsCodeEnvVars {
		if name == "CLAUDECODE" {
			t.Error("CLAUDECODE must not be in vsCodeEnvVars -- " +
				"forwarding it causes 'nested session' errors in the container")
		}
	}
}

func TestCLAUDECODE_NotInResolveResults(t *testing.T) {
	// Even if CLAUDECODE is set in the environment, it must not appear
	// in resolved results.
	t.Setenv("CLAUDECODE", "1")

	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{"claude"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	for _, r := range results {
		if r.Name == "CLAUDECODE" {
			t.Error("CLAUDECODE must not be in resolved tokens -- " +
				"forwarding it causes 'nested session' errors in the container")
		}
	}
}

func TestEmptyToolList(t *testing.T) {
	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	// With no tools and no VS Code env vars set, should be empty.
	// Clear all VS Code env vars to be safe.
	for _, name := range vsCodeEnvVars {
		t.Setenv(name, "")
	}

	results, err = ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty tool list, got %d: %+v", len(results), results)
	}
}

func TestEmptyToolList_NilSlice(t *testing.T) {
	// Clear VS Code env vars.
	for _, name := range vsCodeEnvVars {
		t.Setenv(name, "")
	}

	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for nil tool list, got %d", len(results))
	}
}

func TestClaudeTool_NoTokenNeeded(t *testing.T) {
	// Clear VS Code env vars so only tool tokens matter.
	for _, name := range vsCodeEnvVars {
		t.Setenv(name, "")
	}

	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{"claude"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("claude should not require tokens, got %d results: %+v", len(results), results)
	}
}

func TestVSCodeEnvVars_Forwarded(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "vscode")
	t.Setenv("CLAUDE_CODE_SSE_PORT", "12345")
	// Clear others to avoid interference.
	t.Setenv("TERM_PROGRAM_VERSION", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
	t.Setenv("ENABLE_IDE_INTEGRATION", "")

	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	found := findToken(results, "TERM")
	if found == nil {
		t.Error("expected TERM in results")
	} else if found.Value != "xterm-256color" {
		t.Errorf("TERM value %q, want %q", found.Value, "xterm-256color")
	}

	found = findToken(results, "TERM_PROGRAM")
	if found == nil {
		t.Error("expected TERM_PROGRAM in results")
	}

	found = findToken(results, "CLAUDE_CODE_SSE_PORT")
	if found == nil {
		t.Error("expected CLAUDE_CODE_SSE_PORT in results")
	}
}

func TestEnvTakesPrecedenceOverCache(t *testing.T) {
	cooperDir := t.TempDir()
	wsHash := WorkspaceHash("/tmp/test-workspace")

	// Pre-populate cache with a different value.
	err := CacheTokens(cooperDir, wsHash, []TokenResult{
		{Name: "OPENAI_API_KEY", Value: "sk-cached-old", Source: "cache"},
	})
	if err != nil {
		t.Fatalf("CacheTokens: %v", err)
	}

	// Set env var -- should take precedence.
	t.Setenv("OPENAI_API_KEY", "sk-env-fresh")

	results, err := ResolveTokens("/tmp/test-workspace", cooperDir, []string{"codex"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	found := findToken(results, "OPENAI_API_KEY")
	if found == nil {
		t.Fatal("expected OPENAI_API_KEY in results")
	}
	if found.Value != "sk-env-fresh" {
		t.Errorf("env should take precedence over cache: got %q, want %q", found.Value, "sk-env-fresh")
	}
	if found.Source != "env" {
		t.Errorf("source should be 'env', got %q", found.Source)
	}
}

func TestDeduplication(t *testing.T) {
	// If two tools need the same token, it should appear only once.
	t.Setenv("GH_TOKEN", "ghp-dedup")

	// Clear VS Code env vars.
	for _, name := range vsCodeEnvVars {
		t.Setenv(name, "")
	}

	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{"copilot", "copilot"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	count := 0
	for _, r := range results {
		if r.Name == "GH_TOKEN" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected GH_TOKEN exactly once, got %d times", count)
	}
}

func TestUnknownTool_Ignored(t *testing.T) {
	// Clear VS Code env vars.
	for _, name := range vsCodeEnvVars {
		t.Setenv(name, "")
	}

	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{"unknown-tool"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("unknown tool should be ignored, got %d results", len(results))
	}
}

// findToken searches for a token by name in a results slice.
func findToken(results []TokenResult, name string) *TokenResult {
	for _, r := range results {
		if r.Name == name {
			return &r
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// VS Code env var forwarding list completeness
// ---------------------------------------------------------------------------

func TestVSCodeEnvVars_ExpectedList(t *testing.T) {
	// Ensure all expected VS Code integration env vars are in the forwarding list.
	expected := map[string]bool{
		"TERM":                    true,
		"TERM_PROGRAM":            true,
		"TERM_PROGRAM_VERSION":    true,
		"CLAUDE_CODE_SSE_PORT":    true,
		"CLAUDE_CODE_ENTRYPOINT":  true,
		"ENABLE_IDE_INTEGRATION":  true,
	}

	for _, name := range vsCodeEnvVars {
		if !expected[name] {
			t.Errorf("unexpected env var in vsCodeEnvVars: %q", name)
		}
		delete(expected, name)
	}
	for name := range expected {
		t.Errorf("missing expected env var in vsCodeEnvVars: %q", name)
	}
}

func TestVSCodeEnvVars_NoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, name := range vsCodeEnvVars {
		if seen[name] {
			t.Errorf("duplicate env var in vsCodeEnvVars: %q", name)
		}
		seen[name] = true
	}
}

func TestVSCodeEnvVars_AllForwardedWhenSet(t *testing.T) {
	// Set all VS Code env vars and verify they all appear in results.
	for _, name := range vsCodeEnvVars {
		t.Setenv(name, "test-value-"+name)
	}

	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	for _, name := range vsCodeEnvVars {
		found := findToken(results, name)
		if found == nil {
			t.Errorf("expected %s in results when set", name)
			continue
		}
		expected := "test-value-" + name
		if found.Value != expected {
			t.Errorf("%s: got value %q, want %q", name, found.Value, expected)
		}
		if found.Source != "env" {
			t.Errorf("%s: got source %q, want %q", name, found.Source, "env")
		}
	}
}

func TestVSCodeEnvVars_NotForwardedWhenEmpty(t *testing.T) {
	// Clear all VS Code env vars.
	for _, name := range vsCodeEnvVars {
		t.Setenv(name, "")
	}

	results, err := ResolveTokens("/tmp/test-workspace", t.TempDir(), []string{})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	for _, name := range vsCodeEnvVars {
		found := findToken(results, name)
		if found != nil {
			t.Errorf("expected %s NOT in results when empty, but found with value %q", name, found.Value)
		}
	}
}

// ---------------------------------------------------------------------------
// Token resolution with all three sources
// ---------------------------------------------------------------------------

func TestResolveToken_EnvTakesPrecedence(t *testing.T) {
	// Test that env takes precedence even when cache has a value.
	cooperDir := t.TempDir()
	wsHash := WorkspaceHash("/tmp/precedence-test")

	// Set up cache with old value.
	err := CacheTokens(cooperDir, wsHash, []TokenResult{
		{Name: "OPENAI_API_KEY", Value: "sk-cached-old", Source: "cache"},
	})
	if err != nil {
		t.Fatalf("CacheTokens: %v", err)
	}

	// Set env var with fresh value.
	t.Setenv("OPENAI_API_KEY", "sk-env-fresh")

	results, err := ResolveTokens("/tmp/precedence-test", cooperDir, []string{"codex"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	found := findToken(results, "OPENAI_API_KEY")
	if found == nil {
		t.Fatal("expected OPENAI_API_KEY in results")
	}
	if found.Source != "env" {
		t.Errorf("expected source 'env', got %q", found.Source)
	}
	if found.Value != "sk-env-fresh" {
		t.Errorf("expected env value, got %q", found.Value)
	}
}

func TestResolveToken_CacheUsedWhenNoEnv(t *testing.T) {
	cooperDir := t.TempDir()
	wsHash := WorkspaceHash("/tmp/cache-only-test")

	err := CacheTokens(cooperDir, wsHash, []TokenResult{
		{Name: "GH_TOKEN", Value: "ghp-from-cache", Source: "cache"},
	})
	if err != nil {
		t.Fatalf("CacheTokens: %v", err)
	}

	// Ensure GH_TOKEN and GITHUB_TOKEN are not set.
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	results, err := ResolveTokens("/tmp/cache-only-test", cooperDir, []string{"copilot"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	found := findToken(results, "GH_TOKEN")
	if found == nil {
		t.Fatal("expected GH_TOKEN in results from cache")
	}
	if found.Source != "cache" {
		t.Errorf("expected source 'cache', got %q", found.Source)
	}
	if found.Value != "ghp-from-cache" {
		t.Errorf("expected cached value, got %q", found.Value)
	}
}

func TestResolveToken_FileSourceForCopilot(t *testing.T) {
	// Verify that copilot has a fileSource configured.
	defs, ok := toolTokenDefs["copilot"]
	if !ok {
		t.Fatal("expected copilot token definition")
	}
	if len(defs) == 0 {
		t.Fatal("expected copilot to have at least one token def")
	}
	if defs[0].fileSource == nil {
		t.Error("expected copilot token def to have a fileSource for ~/.copilot/.gh_token")
	}
}

func TestResolveToken_MultipleToolsSameToken(t *testing.T) {
	// When copilot is listed twice, GH_TOKEN should only appear once.
	t.Setenv("GH_TOKEN", "ghp-shared")

	// Clear VS Code env vars.
	for _, name := range vsCodeEnvVars {
		t.Setenv(name, "")
	}

	results, err := ResolveTokens("/tmp/dedup-test", t.TempDir(), []string{"copilot", "copilot"})
	if err != nil {
		t.Fatalf("ResolveTokens: %v", err)
	}

	count := 0
	for _, r := range results {
		if r.Name == "GH_TOKEN" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected GH_TOKEN exactly once, got %d", count)
	}
}

func TestResolveToken_ToolTokenDefs_Coverage(t *testing.T) {
	// Verify all expected tools are defined in toolTokenDefs.
	expectedTools := []string{"claude", "copilot", "codex", "opencode"}
	for _, tool := range expectedTools {
		if _, ok := toolTokenDefs[tool]; !ok {
			t.Errorf("expected tool %q in toolTokenDefs", tool)
		}
	}
}

func TestResolveToken_CopilotEnvVarOrder(t *testing.T) {
	// Verify GH_TOKEN is checked before GITHUB_TOKEN.
	defs := toolTokenDefs["copilot"]
	if len(defs) == 0 {
		t.Fatal("expected copilot to have token defs")
	}
	if len(defs[0].envVars) < 2 {
		t.Fatal("expected copilot to check at least 2 env vars")
	}
	if defs[0].envVars[0] != "GH_TOKEN" {
		t.Errorf("expected first env var to be GH_TOKEN, got %q", defs[0].envVars[0])
	}
	if defs[0].envVars[1] != "GITHUB_TOKEN" {
		t.Errorf("expected second env var to be GITHUB_TOKEN, got %q", defs[0].envVars[1])
	}
}

func TestResolveToken_CodexOutputName(t *testing.T) {
	defs := toolTokenDefs["codex"]
	if len(defs) == 0 {
		t.Fatal("expected codex to have token defs")
	}
	if defs[0].outputName != "OPENAI_API_KEY" {
		t.Errorf("expected codex output name OPENAI_API_KEY, got %q", defs[0].outputName)
	}
}

func TestResolveToken_CopilotOutputName(t *testing.T) {
	defs := toolTokenDefs["copilot"]
	if len(defs) == 0 {
		t.Fatal("expected copilot to have token defs")
	}
	if defs[0].outputName != "GH_TOKEN" {
		t.Errorf("expected copilot output name GH_TOKEN, got %q", defs[0].outputName)
	}
}

func TestResolveToken_ClaudeNoTokens(t *testing.T) {
	defs := toolTokenDefs["claude"]
	if len(defs) != 0 {
		t.Errorf("expected claude to have no token defs (uses mounted dir), got %d", len(defs))
	}
}

func TestResolveToken_OpenCodeNoTokens(t *testing.T) {
	defs := toolTokenDefs["opencode"]
	if len(defs) != 0 {
		t.Errorf("expected opencode to have no token defs, got %d", len(defs))
	}
}

func TestMapToTokens(t *testing.T) {
	m := map[string]string{
		"KEY_A": "val-a",
		"KEY_B": "val-b",
	}
	tokens := mapToTokens(m)
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}

	tokenMap := make(map[string]TokenResult)
	for _, tok := range tokens {
		tokenMap[tok.Name] = tok
	}

	for name, val := range m {
		tok, ok := tokenMap[name]
		if !ok {
			t.Errorf("missing token %q", name)
			continue
		}
		if tok.Value != val {
			t.Errorf("%s: got value %q, want %q", name, tok.Value, val)
		}
		if tok.Source != "cache" {
			t.Errorf("%s: got source %q, want %q", name, tok.Source, "cache")
		}
	}
}

func TestCacheTokens_Overwrite(t *testing.T) {
	cooperDir := t.TempDir()
	wsHash := "overwrite-test"

	// Write initial tokens.
	err := CacheTokens(cooperDir, wsHash, []TokenResult{
		{Name: "KEY1", Value: "old-value"},
	})
	if err != nil {
		t.Fatalf("first CacheTokens: %v", err)
	}

	// Overwrite with new tokens.
	err = CacheTokens(cooperDir, wsHash, []TokenResult{
		{Name: "KEY1", Value: "new-value"},
		{Name: "KEY2", Value: "extra-value"},
	})
	if err != nil {
		t.Fatalf("second CacheTokens: %v", err)
	}

	loaded, err := LoadCachedTokens(cooperDir, wsHash)
	if err != nil {
		t.Fatalf("LoadCachedTokens: %v", err)
	}

	loadedMap := make(map[string]string)
	for _, tok := range loaded {
		loadedMap[tok.Name] = tok.Value
	}

	if loadedMap["KEY1"] != "new-value" {
		t.Errorf("KEY1: got %q, want %q", loadedMap["KEY1"], "new-value")
	}
	if loadedMap["KEY2"] != "extra-value" {
		t.Errorf("KEY2: got %q, want %q", loadedMap["KEY2"], "extra-value")
	}
}
