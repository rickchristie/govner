package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Go version resolution tests ---

func TestResolveGoLatest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"version": "go1.22.5", "stable": true},
			{"version": "go1.22.4", "stable": true},
			{"version": "go1.21.12", "stable": true}
		]`)
	}))
	defer server.Close()

	origURL := goLatestURL
	goLatestURL = server.URL
	defer func() { goLatestURL = origURL }()

	version, err := ResolveGoLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.22.5" {
		t.Errorf("got %q, want \"1.22.5\"", version)
	}
}

func TestResolveGoLatestSkipsUnstable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"version": "go1.23rc1", "stable": false},
			{"version": "go1.22.5", "stable": true}
		]`)
	}))
	defer server.Close()

	origURL := goLatestURL
	goLatestURL = server.URL
	defer func() { goLatestURL = origURL }()

	version, err := ResolveGoLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.22.5" {
		t.Errorf("got %q, want \"1.22.5\"", version)
	}
}

func TestResolveGoLatestNoStable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"version": "go1.23rc1", "stable": false}]`)
	}))
	defer server.Close()

	origURL := goLatestURL
	goLatestURL = server.URL
	defer func() { goLatestURL = origURL }()

	_, err := ResolveGoLatest()
	if err == nil {
		t.Fatal("expected error when no stable version is available")
	}
}

func TestResolveGoLatestInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	defer server.Close()

	origURL := goLatestURL
	goLatestURL = server.URL
	defer func() { goLatestURL = origURL }()

	_, err := ResolveGoLatest()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestResolveGoplsLatest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"Version":"v0.21.1"}`)
	}))
	defer server.Close()

	origURL := goplsLatestURL
	goplsLatestURL = server.URL
	defer func() { goplsLatestURL = origURL }()

	version, err := ResolveGoplsLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "v0.21.1" {
		t.Fatalf("got %q, want v0.21.1", version)
	}
}

func TestResolveGoplsLatestInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not-json`)
	}))
	defer server.Close()

	origURL := goplsLatestURL
	goplsLatestURL = server.URL
	defer func() { goplsLatestURL = origURL }()

	if _, err := ResolveGoplsLatest(); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

// --- Node.js version resolution tests ---

func TestResolveNodeLatest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"version": "v21.6.1", "lts": false},
			{"version": "v20.11.0", "lts": "Iron"},
			{"version": "v18.19.0", "lts": "Hydrogen"}
		]`)
	}))
	defer server.Close()

	origURL := nodeLatestURL
	nodeLatestURL = server.URL
	defer func() { nodeLatestURL = origURL }()

	version, err := ResolveNodeLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "20.11.0" {
		t.Errorf("got %q, want \"20.11.0\"", version)
	}
}

func TestResolveNodeLatestNoLTS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"version": "v21.6.1", "lts": false},
			{"version": "v21.5.0", "lts": false}
		]`)
	}))
	defer server.Close()

	origURL := nodeLatestURL
	nodeLatestURL = server.URL
	defer func() { nodeLatestURL = origURL }()

	_, err := ResolveNodeLatest()
	if err == nil {
		t.Fatal("expected error when no LTS version is available")
	}
}

func TestResolveNodeLatestInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{{{broken`)
	}))
	defer server.Close()

	origURL := nodeLatestURL
	nodeLatestURL = server.URL
	defer func() { nodeLatestURL = origURL }()

	_, err := ResolveNodeLatest()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- Python version resolution tests ---

func TestResolvePythonLatest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"cycle": "3.12", "latest": "3.12.1", "eol": false},
			{"cycle": "3.11", "latest": "3.11.7", "eol": false},
			{"cycle": "2.7", "latest": "2.7.18", "eol": "2020-01-01"}
		]`)
	}))
	defer server.Close()

	origURL := pythonLatestURL
	pythonLatestURL = server.URL
	defer func() { pythonLatestURL = origURL }()

	version, err := ResolvePythonLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "3.12.1" {
		t.Errorf("got %q, want \"3.12.1\"", version)
	}
}

func TestResolvePythonLatestSkipsEOL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// First entry is EOL (past date), second is active
		fmt.Fprint(w, `[
			{"cycle": "3.8", "latest": "3.8.20", "eol": "2024-10-01"},
			{"cycle": "3.12", "latest": "3.12.1", "eol": false}
		]`)
	}))
	defer server.Close()

	origURL := pythonLatestURL
	pythonLatestURL = server.URL
	defer func() { pythonLatestURL = origURL }()

	version, err := ResolvePythonLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "3.12.1" {
		t.Errorf("got %q, want \"3.12.1\"", version)
	}
}

func TestResolvePythonLatestAllEOL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"cycle": "2.7", "latest": "2.7.18", "eol": "2020-01-01"},
			{"cycle": "2.6", "latest": "2.6.9", "eol": "2013-10-29"}
		]`)
	}))
	defer server.Close()

	origURL := pythonLatestURL
	pythonLatestURL = server.URL
	defer func() { pythonLatestURL = origURL }()

	_, err := ResolvePythonLatest()
	if err == nil {
		t.Fatal("expected error when all Python versions are EOL")
	}
}

func TestResolvePythonLatestFutureEOLNotFiltered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// EOL date is far in the future -- should NOT be filtered
		fmt.Fprint(w, `[
			{"cycle": "3.12", "latest": "3.12.1", "eol": "2028-10-31"}
		]`)
	}))
	defer server.Close()

	origURL := pythonLatestURL
	pythonLatestURL = server.URL
	defer func() { pythonLatestURL = origURL }()

	version, err := ResolvePythonLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "3.12.1" {
		t.Errorf("got %q, want \"3.12.1\"", version)
	}
}

// --- npm registry resolution tests ---

func TestResolveNPMPackageLatest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Verify the request path includes the package name
		pkg := npmPackageResponse{
			DistTags: map[string]string{
				"latest": "1.0.12",
				"beta":   "1.1.0-beta.1",
			},
			Versions: map[string]json.RawMessage{
				"1.0.11": json.RawMessage(`{}`),
				"1.0.12": json.RawMessage(`{}`),
			},
		}
		json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()

	origURL := npmRegistryURL
	npmRegistryURL = server.URL
	defer func() { npmRegistryURL = origURL }()

	version, err := ResolveNPMPackageLatest("@anthropic-ai/claude-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.0.12" {
		t.Errorf("got %q, want \"1.0.12\"", version)
	}
}

func TestResolveNPMPackageLatestNoDistTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"dist-tags": {}, "versions": {}}`)
	}))
	defer server.Close()

	origURL := npmRegistryURL
	npmRegistryURL = server.URL
	defer func() { npmRegistryURL = origURL }()

	_, err := ResolveNPMPackageLatest("@anthropic-ai/claude-code")
	if err == nil {
		t.Fatal("expected error when no latest dist-tag")
	}
}

func TestResolveNPMPackageLatestInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json at all`)
	}))
	defer server.Close()

	origURL := npmRegistryURL
	npmRegistryURL = server.URL
	defer func() { npmRegistryURL = origURL }()

	_, err := ResolveNPMPackageLatest("@anthropic-ai/claude-code")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestResolveNPMPackageMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"version":"5.1.3","engines":{"node":">=20"}}`)
	}))
	defer server.Close()

	origURL := npmRegistryURL
	npmRegistryURL = server.URL
	defer func() { npmRegistryURL = origURL }()

	meta, err := ResolveNPMPackageMetadata("typescript-language-server", "5.1.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Version != "5.1.3" || meta.Engines.Node != ">=20" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestResolvePyPIPackageLatest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"info":{"version":"1.14.0","requires_python":">=3.9"}}`)
	}))
	defer server.Close()

	origURL := pyPIBaseURL
	pyPIBaseURL = server.URL
	defer func() { pyPIBaseURL = origURL }()

	version, err := ResolvePyPIPackageLatest("python-lsp-server")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.14.0" {
		t.Fatalf("got %q, want 1.14.0", version)
	}
}

func TestResolvePyPIPackageVersionMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"info":{"version":"1.12.2","requires_python":">=3.8"}}`)
	}))
	defer server.Close()

	origURL := pyPIBaseURL
	pyPIBaseURL = server.URL
	defer func() { pyPIBaseURL = origURL }()

	meta, err := ResolvePyPIPackageVersionMetadata("python-lsp-server", "1.12.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Version != "1.12.2" || meta.RequiresPython != ">=3.8" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

// --- npm URL construction tests ---

func TestResolveNPMURLConstruction(t *testing.T) {
	tests := []struct {
		toolName    string
		wantPackage string
	}{
		{"claude", "@anthropic-ai/claude-code"},
		{"copilot", "@github/copilot"},
		{"codex", "@openai/codex"},
		{"opencode", "opencode-ai"},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			var requestedPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestedPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				pkg := npmPackageResponse{
					DistTags: map[string]string{"latest": "1.0.0"},
					Versions: map[string]json.RawMessage{"1.0.0": json.RawMessage(`{}`)},
				}
				json.NewEncoder(w).Encode(pkg)
			}))
			defer server.Close()

			origURL := npmRegistryURL
			npmRegistryURL = server.URL
			defer func() { npmRegistryURL = origURL }()

			_, err := ResolveLatestVersion(tt.toolName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			wantPath := "/" + tt.wantPackage
			if requestedPath != wantPath {
				t.Errorf("requested path %q, want %q", requestedPath, wantPath)
			}
		})
	}
}

// --- ResolveLatestVersion dispatch tests ---

func TestResolveLatestVersionDispatch(t *testing.T) {
	// Set up a single mock server that handles all tool types
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		query := r.URL.RawQuery

		switch {
		case query == "mode=json":
			// Go
			fmt.Fprint(w, `[{"version": "go1.22.5", "stable": true}]`)
		case strings.Contains(path, "dist/index.json"):
			// Node (won't match because we override individual URLs)
			fmt.Fprint(w, `[{"version": "v20.11.0", "lts": "Iron"}]`)
		default:
			// npm fallback
			pkg := npmPackageResponse{
				DistTags: map[string]string{"latest": "2.0.0"},
				Versions: map[string]json.RawMessage{"2.0.0": json.RawMessage(`{}`)},
			}
			json.NewEncoder(w).Encode(pkg)
		}
	}))
	defer server.Close()

	// Override all URLs to point to our test server
	origGo := goLatestURL
	origNode := nodeLatestURL
	origPython := pythonLatestURL
	origNPM := npmRegistryURL

	goLatestURL = server.URL + "/?mode=json"
	nodeLatestURL = server.URL + "/dist/index.json"
	npmRegistryURL = server.URL

	defer func() {
		goLatestURL = origGo
		nodeLatestURL = origNode
		pythonLatestURL = origPython
		npmRegistryURL = origNPM
	}()

	// Test Go dispatch
	v, err := ResolveLatestVersion("go")
	if err != nil {
		t.Fatalf("go: unexpected error: %v", err)
	}
	if v != "1.22.5" {
		t.Errorf("go: got %q, want \"1.22.5\"", v)
	}

	// Test Node dispatch
	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"version": "v20.11.0", "lts": "Iron"}]`)
	}))
	defer nodeServer.Close()
	nodeLatestURL = nodeServer.URL

	v, err = ResolveLatestVersion("node")
	if err != nil {
		t.Fatalf("node: unexpected error: %v", err)
	}
	if v != "20.11.0" {
		t.Errorf("node: got %q, want \"20.11.0\"", v)
	}

	// Test npm-based tool dispatch (claude)
	v, err = ResolveLatestVersion("claude")
	if err != nil {
		t.Fatalf("claude: unexpected error: %v", err)
	}
	if v != "2.0.0" {
		t.Errorf("claude: got %q, want \"2.0.0\"", v)
	}
}

func TestResolveLatestVersionUnknownTool(t *testing.T) {
	_, err := ResolveLatestVersion("unknown-tool")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("error should mention 'unknown tool', got: %v", err)
	}
}

// --- ValidateVersion tests ---

func TestResolveValidateGoVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"version": "go1.22.5", "stable": true},
			{"version": "go1.22.4", "stable": true}
		]`)
	}))
	defer server.Close()

	origURL := goLatestURL
	goLatestURL = server.URL
	defer func() { goLatestURL = origURL }()

	exists, err := ValidateVersion("go", "1.22.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected version 1.22.5 to exist")
	}

	exists, err = ValidateVersion("go", "1.20.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected version 1.20.0 to not exist")
	}
}

func TestResolveValidateNodeVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"version": "v20.11.0", "lts": "Iron"},
			{"version": "v18.19.0", "lts": "Hydrogen"}
		]`)
	}))
	defer server.Close()

	origURL := nodeLatestURL
	nodeLatestURL = server.URL
	defer func() { nodeLatestURL = origURL }()

	exists, err := ValidateVersion("node", "20.11.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected version 20.11.0 to exist")
	}

	exists, err = ValidateVersion("node", "99.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected version 99.0.0 to not exist")
	}
}

func TestResolveValidatePythonVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"cycle": "3.12", "latest": "3.12.1", "eol": false},
			{"cycle": "3.11", "latest": "3.11.7", "eol": false}
		]`)
	}))
	defer server.Close()

	origURL := pythonLatestURL
	pythonLatestURL = server.URL
	defer func() { pythonLatestURL = origURL }()

	exists, err := ValidateVersion("python", "3.12.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected version 3.12.1 to exist")
	}

	exists, err = ValidateVersion("python", "3.10.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected version 3.10.0 to not exist")
	}
}

func TestResolveValidateNPMVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		pkg := npmPackageResponse{
			DistTags: map[string]string{"latest": "1.0.12"},
			Versions: map[string]json.RawMessage{
				"1.0.11": json.RawMessage(`{}`),
				"1.0.12": json.RawMessage(`{}`),
			},
		}
		json.NewEncoder(w).Encode(pkg)
	}))
	defer server.Close()

	origURL := npmRegistryURL
	npmRegistryURL = server.URL
	defer func() { npmRegistryURL = origURL }()

	exists, err := ValidateVersion("claude", "1.0.12")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected version 1.0.12 to exist")
	}

	exists, err = ValidateVersion("claude", "99.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected version 99.0.0 to not exist")
	}
}

func TestResolveValidateVersionUnknownTool(t *testing.T) {
	_, err := ValidateVersion("unknown-tool", "1.0.0")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// --- HTTP error handling tests ---

func TestResolveHTTPServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	origURL := goLatestURL
	goLatestURL = server.URL
	defer func() { goLatestURL = origURL }()

	_, err := ResolveGoLatest()
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should contain status code, got: %v", err)
	}
}

func TestResolveHTTPNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	origURL := npmRegistryURL
	npmRegistryURL = server.URL
	defer func() { npmRegistryURL = origURL }()

	_, err := ResolveNPMPackageLatest("nonexistent-package")
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should contain status code, got: %v", err)
	}
}

func TestResolveHTTPNetworkError(t *testing.T) {
	// Use a URL that will definitely fail to connect
	origURL := goLatestURL
	goLatestURL = "http://127.0.0.1:1" // port 1 should refuse connection
	defer func() { goLatestURL = origURL }()

	_, err := ResolveGoLatest()
	if err == nil {
		t.Fatal("expected error for network failure")
	}
}

// --- isNodeLTS helper tests ---

func TestResolveIsNodeLTS(t *testing.T) {
	tests := []struct {
		name string
		lts  any
		want bool
	}{
		{"false bool", false, false},
		{"true bool", true, true},
		{"string codename", "Iron", true},
		{"empty string", "", false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNodeLTS(tt.lts)
			if got != tt.want {
				t.Errorf("isNodeLTS(%v) = %v, want %v", tt.lts, got, tt.want)
			}
		})
	}
}

// --- isPythonEOL helper tests ---

func TestResolveIsPythonEOL(t *testing.T) {
	tests := []struct {
		name string
		eol  any
		want bool
	}{
		{"false bool", false, false},
		{"true bool", true, true},
		{"past date string", "2020-01-01", true},
		{"future date string", "2099-01-01", false},
		{"empty string", "", false},
		{"nil", nil, false},
		{"invalid date string", "not-a-date", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPythonEOL(tt.eol)
			if got != tt.want {
				t.Errorf("isPythonEOL(%v) = %v, want %v", tt.eol, got, tt.want)
			}
		})
	}
}

// --- npm package name mapping tests ---

func TestResolveNPMPackageNameMapping(t *testing.T) {
	tests := []struct {
		toolName string
		wantPkg  string
	}{
		{"claude", "@anthropic-ai/claude-code"},
		{"copilot", "@github/copilot"},
		{"codex", "@openai/codex"},
		{"opencode", "opencode-ai"},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			got, ok := npmPackageNames[tt.toolName]
			if !ok {
				t.Fatalf("no mapping for tool %q", tt.toolName)
			}
			if got != tt.wantPkg {
				t.Errorf("got %q, want %q", got, tt.wantPkg)
			}
		})
	}
}
