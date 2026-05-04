package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// TokenResult holds a resolved token with its name, value, and where it was found.
type TokenResult struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Source string `json:"source"` // "env", "cache", "shell", "file"
}

// toolTokenDefs maps AI tool names to the tokens they require.
// Each entry is a list of token definitions for that tool.
var toolTokenDefs = map[string][]tokenDef{
	// claude: auth handled via mounted ~/.claude directory, no env var needed.
	"claude": {},
	// copilot: GH_TOKEN or GITHUB_TOKEN env var, or ~/.copilot/.gh_token file.
	"copilot": {
		{envVars: []string{"GH_TOKEN", "GITHUB_TOKEN"}, outputName: "GH_TOKEN", fileSource: copilotTokenFile},
	},
	// codex: OPENAI_API_KEY env var.
	"codex": {
		{envVars: []string{"OPENAI_API_KEY"}, outputName: "OPENAI_API_KEY"},
	},
	// opencode: no additional token needed. Uses GH_TOKEN from copilot if both enabled.
	"opencode": {},
}

// tokenDef describes how to resolve a single token for an AI tool.
type tokenDef struct {
	// envVars is the list of environment variable names to check, in order.
	envVars []string
	// outputName is the canonical name used in TokenResult.Name and cache keys.
	outputName string
	// fileSource is an optional function that returns a token value from a file.
	// Used for copilot's ~/.copilot/.gh_token file.
	fileSource func() (string, error)
}

// copilotTokenFile reads the GitHub Copilot token from ~/.copilot/.gh_token.
func copilotTokenFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".copilot", ".gh_token")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

type forwardedSessionEnvVar struct {
	Name          string
	PreserveEmpty bool
}

// forwardedSessionEnvVars are host terminal, color-policy, hyperlink-policy,
// and IDE metadata forwarded when set. Docker's exec TTY preserves the byte
// stream, but not the host process env that many CLIs use to choose truecolor,
// dark/light palette, hyperlink behavior, and IDE integration behavior. Keep
// this list to metadata/policy values; do not forward host socket/path vars such
// as KITTY_LISTEN_ON, WEZTERM_EXECUTABLE, or TMUX into the container.
var forwardedSessionEnvVars = []forwardedSessionEnvVar{
	{Name: "TERM"},
	{Name: "COLORTERM"},
	{Name: "TERM_PROGRAM"},
	{Name: "TERM_PROGRAM_VERSION"},
	{Name: "COLORFGBG"},
	{Name: "LC_TERMINAL"},
	{Name: "LC_TERMINAL_VERSION"},
	{Name: "TERM_SESSION_ID"},
	{Name: "ITERM_PROFILE"},
	{Name: "ITERM_SESSION_ID"},
	{Name: "VTE_VERSION"},
	{Name: "KONSOLE_VERSION"},
	{Name: "KONSOLE_PROFILE_NAME"},
	{Name: "WT_SESSION"},
	{Name: "WT_PROFILE_ID"},
	{Name: "KITTY_WINDOW_ID"},
	{Name: "WEZTERM_PANE"},
	{Name: "TERMINAL_EMULATOR"},
	{Name: "DOMTERM"},
	{Name: "TERMINOLOGY"},
	{Name: "NO_COLOR", PreserveEmpty: true},
	{Name: "FORCE_COLOR", PreserveEmpty: true},
	{Name: "FORCE_HYPERLINK", PreserveEmpty: true},
	{Name: "CLICOLOR"},
	{Name: "CLICOLOR_FORCE", PreserveEmpty: true},
	{Name: "NODE_DISABLE_COLORS", PreserveEmpty: true},
	{Name: "CLAUDE_CODE_SSE_PORT"},
	{Name: "CLAUDE_CODE_ENTRYPOINT"},
	{Name: "ENABLE_IDE_INTEGRATION"},
}

// CLAUDECODE env var is explicitly NOT forwarded. Claude Code sets CLAUDECODE=1
// to detect nested sessions, but the container is an isolated sandbox (not a
// nested session). Forwarding it causes:
//
//	"Error: Claude Code cannot be launched inside another Claude Code session."
//
// The container needs to run Claude Code as a fresh top-level session.

// shellResolveTimeout bounds login-shell fallback lookups. User shell startup
// files can block indefinitely (prompting, networking, long-running hooks),
// and auth resolution must fail open rather than hanging the whole CLI/test.
const shellResolveTimeout = 3 * time.Second

// WorkspaceHash returns a short hash of the absolute workspace path.
// Uses the first 8 characters of the SHA-256 hex digest.
func WorkspaceHash(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	h := sha256.Sum256([]byte(abs))
	return fmt.Sprintf("%x", h)[:8]
}

// ResolveTokens resolves all needed tokens for the given enabled AI tools.
// Resolution order per token (first match wins):
//  1. Environment variable (os.Getenv)
//  2. Cache file at {cooperDir}/secrets/{workspaceHash}
//  3. Login shell profile: run `bash -ilc 'echo "${VAR_NAME}"'` and parse output
//  4. File source (e.g. ~/.copilot/.gh_token for copilot)
//
// If resolved from login shell, the value is cached for next time.
// Terminal color-policy/metadata and IDE env vars are always included when set.
func ResolveTokens(workspacePath, cooperDir string, enabledTools []string) ([]TokenResult, error) {
	wsHash := WorkspaceHash(workspacePath)
	cached, _ := LoadCachedTokens(cooperDir, wsHash)
	cachedMap := make(map[string]string, len(cached))
	for _, t := range cached {
		cachedMap[t.Name] = t.Value
	}

	var results []TokenResult
	seen := make(map[string]bool)

	// Resolve tokens for each enabled tool.
	for _, tool := range enabledTools {
		defs, ok := toolTokenDefs[tool]
		if !ok {
			continue
		}
		for _, def := range defs {
			if seen[def.outputName] {
				continue
			}
			seen[def.outputName] = true

			result, err := resolveToken(def, cachedMap)
			if err != nil {
				return nil, fmt.Errorf("resolving token %s for tool %s: %w", def.outputName, tool, err)
			}
			if result != nil {
				// If resolved from shell, cache it for next time.
				if result.Source == "shell" {
					cachedMap[result.Name] = result.Value
					if cacheErr := CacheTokens(cooperDir, wsHash, mapToTokens(cachedMap)); cacheErr != nil {
						return nil, fmt.Errorf("caching token %s: %w", result.Name, cacheErr)
					}
				}
				results = append(results, *result)
			}
		}
	}

	// Always forward terminal and IDE integration env vars when set.
	for _, variable := range forwardedSessionEnvVars {
		val, ok := os.LookupEnv(variable.Name)
		if !ok || (val == "" && !variable.PreserveEmpty) {
			continue
		}
		results = append(results, TokenResult{
			Name:   variable.Name,
			Value:  val,
			Source: "env",
		})
	}

	return results, nil
}

// resolveToken tries each resolution strategy in order for a single token definition.
func resolveToken(def tokenDef, cachedMap map[string]string) (*TokenResult, error) {
	// 1. Environment variable (try each candidate).
	for _, envVar := range def.envVars {
		if val := os.Getenv(envVar); val != "" {
			return &TokenResult{
				Name:   def.outputName,
				Value:  val,
				Source: "env",
			}, nil
		}
	}

	// 2. Cache file.
	if val, ok := cachedMap[def.outputName]; ok && val != "" {
		return &TokenResult{
			Name:   def.outputName,
			Value:  val,
			Source: "cache",
		}, nil
	}

	// 3. Login shell profile.
	for _, envVar := range def.envVars {
		val, err := resolveFromShell(envVar)
		if err == nil && val != "" {
			return &TokenResult{
				Name:   def.outputName,
				Value:  val,
				Source: "shell",
			}, nil
		}
	}

	// 4. File source (e.g. ~/.copilot/.gh_token).
	if def.fileSource != nil {
		val, err := def.fileSource()
		if err == nil && val != "" {
			return &TokenResult{
				Name:   def.outputName,
				Value:  val,
				Source: "file",
			}, nil
		}
	}

	return nil, nil
}

// resolveFromShell runs a login shell to resolve an environment variable
// from the user's shell profile (~/.bashrc, ~/.zshrc, etc.).
func resolveFromShell(varName string) (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	cmd := exec.Command(shell, "-ilc", fmt.Sprintf(`echo "${%s}"`, varName))
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Start(); err != nil {
		return "", err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			return "", err
		}
	case <-time.After(shellResolveTimeout):
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-done
		return "", fmt.Errorf("shell lookup timed out after %s", shellResolveTimeout)
	}

	// Take the last non-empty line -- shell profiles may print other output.
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line, nil
		}
	}
	return "", nil
}

// CacheTokens writes tokens to the secrets cache file with 0600 permissions.
// The cache file is stored at {cooperDir}/secrets/{workspaceHash}.
func CacheTokens(cooperDir, workspaceHash string, tokens []TokenResult) error {
	secretsDir := filepath.Join(cooperDir, "secrets")
	if err := os.MkdirAll(secretsDir, 0700); err != nil {
		return fmt.Errorf("creating secrets directory: %w", err)
	}

	// Build key-value map for storage (only store name and value).
	store := make(map[string]string, len(tokens))
	for _, t := range tokens {
		store[t.Name] = t.Value
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tokens: %w", err)
	}

	path := filepath.Join(secretsDir, workspaceHash)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing cache file: %w", err)
	}

	return nil
}

// LoadCachedTokens reads tokens from the secrets cache file.
func LoadCachedTokens(cooperDir, workspaceHash string) ([]TokenResult, error) {
	path := filepath.Join(cooperDir, "secrets", workspaceHash)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var store map[string]string
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parsing cache file: %w", err)
	}

	var results []TokenResult
	for name, value := range store {
		results = append(results, TokenResult{
			Name:   name,
			Value:  value,
			Source: "cache",
		})
	}

	return results, nil
}

// mapToTokens converts a name-value map into TokenResult slice with source "cache".
func mapToTokens(m map[string]string) []TokenResult {
	tokens := make([]TokenResult, 0, len(m))
	for name, value := range m {
		tokens = append(tokens, TokenResult{
			Name:   name,
			Value:  value,
			Source: "cache",
		})
	}
	return tokens
}
