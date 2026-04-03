package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultConfigValidates(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate successfully, got: %v", err)
	}
}

func TestDefaultConfigHasWhitelistedDomains(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.WhitelistedDomains) == 0 {
		t.Fatal("default config should have whitelisted domains")
	}

	// Check that some key domains are present
	domains := make(map[string]bool)
	for _, d := range cfg.WhitelistedDomains {
		domains[d.Domain] = true
	}

	expectedDomains := []string{
		".anthropic.com",
		".openai.com",
		"github.com",
		".githubcopilot.com",
		"raw.githubusercontent.com",
	}
	for _, expected := range expectedDomains {
		if !domains[expected] {
			t.Errorf("default config missing expected domain: %s", expected)
		}
	}
}

func TestDefaultConfigAllDomainsAreDefault(t *testing.T) {
	cfg := DefaultConfig()
	for _, d := range cfg.WhitelistedDomains {
		if d.Source != "default" {
			t.Errorf("domain %q has source %q, expected \"default\"", d.Domain, d.Source)
		}
	}
}

func TestValidatePortCollision(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ProxyPort = 3128
	cfg.BridgePort = 3128

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for proxy port == bridge port")
	}
	if got := err.Error(); got == "" {
		t.Fatal("error message should not be empty")
	}
}

func TestValidateProxyPortRange(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{"zero", 0, true},
		{"negative", -1, true},
		{"too high", 65536, true},
		{"min valid", 1, false},
		{"max valid", 65535, false},
		{"standard squid", 3128, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.ProxyPort = tt.port
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("expected error for proxy port %d", tt.port)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for proxy port %d: %v", tt.port, err)
			}
		})
	}
}

func TestValidateBridgePortRange(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BridgePort = 0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for bridge port 0")
	}
}

func TestValidatePortForwardCollision(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortForwardRules = []PortForwardRule{
		{
			ContainerPort: 3128, // same as proxy
			HostPort:      5432,
			Description:   "collides with proxy",
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for port forward colliding with proxy port")
	}
}

func TestValidatePortForwardRangeCollision(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortForwardRules = []PortForwardRule{
		{
			ContainerPort: 4340,
			HostPort:      4340,
			Description:   "range collides with bridge",
			IsRange:       true,
			RangeEnd:      4345, // includes 4343 (bridge port)
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for port forward range colliding with bridge port")
	}
}

func TestValidatePortForwardRangeInvalid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortForwardRules = []PortForwardRule{
		{
			ContainerPort: 5000,
			HostPort:      5000,
			Description:   "invalid range",
			IsRange:       true,
			RangeEnd:      4999, // end < start
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid port range")
	}
}

func TestValidateHistoryLimits(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{
			name:    "zero blocked limit",
			mutate:  func(c *Config) { c.BlockedHistoryLimit = 0 },
			wantErr: true,
		},
		{
			name:    "zero allowed limit",
			mutate:  func(c *Config) { c.AllowedHistoryLimit = 0 },
			wantErr: true,
		},
		{
			name:    "zero bridge log limit",
			mutate:  func(c *Config) { c.BridgeLogLimit = 0 },
			wantErr: true,
		},
		{
			name:    "zero monitor timeout",
			mutate:  func(c *Config) { c.MonitorTimeoutSecs = 0 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestValidateClipboardSettings(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{
			name:    "zero clipboard TTL",
			mutate:  func(c *Config) { c.ClipboardTTLSecs = 0 },
			wantErr: true,
		},
		{
			name:    "negative clipboard TTL",
			mutate:  func(c *Config) { c.ClipboardTTLSecs = -1 },
			wantErr: true,
		},
		{
			name:    "zero clipboard max bytes",
			mutate:  func(c *Config) { c.ClipboardMaxBytes = 0 },
			wantErr: true,
		},
		{
			name:    "negative clipboard max bytes",
			mutate:  func(c *Config) { c.ClipboardMaxBytes = -1 },
			wantErr: true,
		},
		{
			name:    "valid clipboard settings",
			mutate:  func(c *Config) { c.ClipboardTTLSecs = 600; c.ClipboardMaxBytes = 10485760 },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestDefaultConfigClipboardDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ClipboardTTLSecs != 300 {
		t.Errorf("expected default clipboard TTL 300, got %d", cfg.ClipboardTTLSecs)
	}
	if cfg.ClipboardMaxBytes != 20971520 {
		t.Errorf("expected default clipboard max bytes 20971520, got %d", cfg.ClipboardMaxBytes)
	}
}

func TestHasEnabledAITool(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.HasEnabledAITool() {
		t.Error("default config should have no enabled AI tools")
	}

	cfg.AITools = []ToolConfig{
		{Name: "claude", Enabled: false},
	}
	if cfg.HasEnabledAITool() {
		t.Error("config with only disabled AI tools should return false")
	}

	cfg.AITools = []ToolConfig{
		{Name: "claude", Enabled: true, Mode: ModeLatest},
	}
	if !cfg.HasEnabledAITool() {
		t.Error("config with enabled AI tool should return true")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	original := DefaultConfig()
	original.ProgrammingTools = []ToolConfig{
		{
			Name:          "go",
			Enabled:       true,
			Mode:          ModeMirror,
			HostVersion:   "1.22.5",
			ContainerVersion: "1.22.5",
		},
		{
			Name:          "node",
			Enabled:       true,
			Mode:          ModePin,
			PinnedVersion: "20.11.0",
		},
	}
	original.AITools = []ToolConfig{
		{
			Name:    "claude",
			Enabled: true,
			Mode:    ModeLatest,
		},
		{
			Name:    "copilot",
			Enabled: false,
			Mode:    ModeOff,
		},
	}
	original.PortForwardRules = []PortForwardRule{
		{
			ContainerPort: 5432,
			HostPort:      5432,
			Description:   "PostgreSQL",
		},
		{
			ContainerPort: 8000,
			HostPort:      8000,
			Description:   "Dev server range",
			IsRange:       true,
			RangeEnd:      8100,
		},
	}
	original.BridgeRoutes = []BridgeRoute{
		{
			APIPath:    "/deploy-staging",
			ScriptPath: "/home/user/scripts/deploy-staging.sh",
		},
	}

	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if !reflect.DeepEqual(*original, restored) {
		t.Errorf("round-trip mismatch.\nOriginal: %+v\nRestored: %+v", *original, restored)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := DefaultConfig()
	original.ProxyPort = 8080
	original.BridgePort = 9090
	original.AITools = []ToolConfig{
		{Name: "claude", Enabled: true, Mode: ModeLatest},
	}

	if err := SaveConfig(path, original); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if !reflect.DeepEqual(*original, *loaded) {
		t.Errorf("save/load mismatch.\nOriginal: %+v\nLoaded: %+v", *original, *loaded)
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("expected error for nonexistent config file")
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("not valid json{{{"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- Version comparison tests ---

func TestCompareVersionsMirror(t *testing.T) {
	tests := []struct {
		name      string
		container string
		expected  string
		want      VersionStatus
	}{
		{"match", "1.22.5", "1.22.5", VersionMatch},
		{"mismatch", "1.22.4", "1.22.5", VersionMismatch},
		{"empty container", "", "1.22.5", VersionUnknown},
		{"empty expected", "1.22.5", "", VersionUnknown},
		{"both empty", "", "", VersionUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareVersions(tt.container, tt.expected, ModeMirror)
			if got != tt.want {
				t.Errorf("CompareVersions(%q, %q, ModeMirror) = %v, want %v",
					tt.container, tt.expected, got, tt.want)
			}
		})
	}
}

func TestCompareVersionsPin(t *testing.T) {
	tests := []struct {
		name      string
		container string
		expected  string
		want      VersionStatus
	}{
		{"match", "20.11.0", "20.11.0", VersionMatch},
		{"mismatch", "20.10.0", "20.11.0", VersionMismatch},
		{"empty container", "", "20.11.0", VersionUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareVersions(tt.container, tt.expected, ModePin)
			if got != tt.want {
				t.Errorf("CompareVersions(%q, %q, ModePin) = %v, want %v",
					tt.container, tt.expected, got, tt.want)
			}
		})
	}
}

func TestCompareVersionsOff(t *testing.T) {
	// ModeOff always returns VersionMatch regardless of inputs
	got := CompareVersions("anything", "anything_else", ModeOff)
	if got != VersionMatch {
		t.Errorf("CompareVersions with ModeOff should always return VersionMatch, got %v", got)
	}

	got = CompareVersions("", "", ModeOff)
	if got != VersionMatch {
		t.Errorf("CompareVersions with ModeOff and empty strings should return VersionMatch, got %v", got)
	}
}

func TestCompareVersionsLatest(t *testing.T) {
	// ModeLatest compares container vs resolved latest.
	if got := CompareVersions("1.22.5", "1.22.5", ModeLatest); got != VersionMatch {
		t.Errorf("ModeLatest same versions: expected match, got %v", got)
	}
	if got := CompareVersions("1.22.4", "1.22.5", ModeLatest); got != VersionMismatch {
		t.Errorf("ModeLatest different versions: expected mismatch, got %v", got)
	}
	if got := CompareVersions("1.22.5", "", ModeLatest); got != VersionUnknown {
		t.Errorf("ModeLatest empty expected: expected unknown, got %v", got)
	}
	if got := CompareVersions("", "1.22.5", ModeLatest); got != VersionUnknown {
		t.Errorf("ModeLatest empty container: expected unknown, got %v", got)
	}
}

// --- VersionMode JSON marshaling tests ---

func TestVersionModeMarshalJSON(t *testing.T) {
	tests := []struct {
		mode VersionMode
		want string
	}{
		{ModeOff, `"off"`},
		{ModeMirror, `"mirror"`},
		{ModeLatest, `"latest"`},
		{ModePin, `"pin"`},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			data, err := json.Marshal(tt.mode)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}
			if string(data) != tt.want {
				t.Errorf("got %s, want %s", string(data), tt.want)
			}
		})
	}
}

func TestVersionModeUnmarshalJSON(t *testing.T) {
	tests := []struct {
		input string
		want  VersionMode
	}{
		{`"off"`, ModeOff},
		{`"mirror"`, ModeMirror},
		{`"latest"`, ModeLatest},
		{`"pin"`, ModePin},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var got VersionMode
			if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersionModeUnmarshalInvalid(t *testing.T) {
	var m VersionMode
	err := json.Unmarshal([]byte(`"bogus"`), &m)
	if err == nil {
		t.Fatal("expected error for unknown mode string")
	}

	err = json.Unmarshal([]byte(`123`), &m)
	if err == nil {
		t.Fatal("expected error for non-string JSON")
	}
}

func TestVersionModeRoundTrip(t *testing.T) {
	modes := []VersionMode{ModeOff, ModeMirror, ModeLatest, ModePin}
	for _, mode := range modes {
		data, err := json.Marshal(mode)
		if err != nil {
			t.Fatalf("marshal %v failed: %v", mode, err)
		}

		var restored VersionMode
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("unmarshal %s failed: %v", string(data), err)
		}

		if restored != mode {
			t.Errorf("round-trip mismatch: %v -> %s -> %v", mode, string(data), restored)
		}
	}
}

func TestVersionModeString(t *testing.T) {
	if ModeOff.String() != "off" {
		t.Errorf("ModeOff.String() = %q, want \"off\"", ModeOff.String())
	}
	if ModeMirror.String() != "mirror" {
		t.Errorf("ModeMirror.String() = %q, want \"mirror\"", ModeMirror.String())
	}

	// Unknown mode
	var unknown VersionMode = 99
	if unknown.String() != "unknown" {
		t.Errorf("unknown mode String() = %q, want \"unknown\"", unknown.String())
	}
}

func TestVersionStatusString(t *testing.T) {
	if VersionMatch.String() != "match" {
		t.Errorf("VersionMatch.String() = %q, want \"match\"", VersionMatch.String())
	}
	if VersionMismatch.String() != "mismatch" {
		t.Errorf("VersionMismatch.String() = %q, want \"mismatch\"", VersionMismatch.String())
	}
	if VersionUnknown.String() != "unknown" {
		t.Errorf("VersionUnknown.String() = %q, want \"unknown\"", VersionUnknown.String())
	}
}

// --- DetectHostVersion tests (with mocked exec) ---

func TestDetectHostVersionUnknownTool(t *testing.T) {
	_, err := DetectHostVersion("nonexistent-tool")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestDetectHostVersionGo(t *testing.T) {
	// Mock exec.Command to return a known output
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", "go version go1.22.5 linux/amd64")
	}

	version, err := DetectHostVersion("go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.22.5" {
		t.Errorf("got version %q, want \"1.22.5\"", version)
	}
}

func TestDetectHostVersionNode(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", "v20.11.0")
	}

	version, err := DetectHostVersion("node")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "20.11.0" {
		t.Errorf("got version %q, want \"20.11.0\"", version)
	}
}

func TestDetectHostVersionPython(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", "Python 3.12.1")
	}

	version, err := DetectHostVersion("python")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "3.12.1" {
		t.Errorf("got version %q, want \"3.12.1\"", version)
	}
}

func TestDetectHostVersionClaude(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", "1.0.12")
	}

	version, err := DetectHostVersion("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.0.12" {
		t.Errorf("got version %q, want \"1.0.12\"", version)
	}
}

func TestParseVersionNoMatch(t *testing.T) {
	_, err := parseVersion("no version here")
	if err == nil {
		t.Fatal("expected error when no version found")
	}
}

// --- ToolConfig in struct JSON round-trip ---

func TestToolConfigJSONRoundTrip(t *testing.T) {
	original := ToolConfig{
		Name:          "go",
		Enabled:       true,
		Mode:          ModeMirror,
		PinnedVersion: "",
		HostVersion:   "1.22.5",
		ContainerVersion: "1.22.4",
		InstallCmd:    "apt-get install -y golang",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var restored ToolConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if !reflect.DeepEqual(original, restored) {
		t.Errorf("round-trip mismatch.\nOriginal: %+v\nRestored: %+v", original, restored)
	}
}

func TestRefreshContainerVersion_Mirror(t *testing.T) {
	tc := ToolConfig{Name: "go", Enabled: true, Mode: ModeMirror, HostVersion: "1.24.10"}
	tc.RefreshContainerVersion()
	if tc.ContainerVersion != "1.24.10" {
		t.Errorf("mirror: expected ContainerVersion=1.24.10, got %q", tc.ContainerVersion)
	}
}

func TestRefreshContainerVersion_Pin(t *testing.T) {
	tc := ToolConfig{Name: "go", Enabled: true, Mode: ModePin, PinnedVersion: "1.23.0"}
	tc.RefreshContainerVersion()
	if tc.ContainerVersion != "1.23.0" {
		t.Errorf("pin: expected ContainerVersion=1.23.0, got %q", tc.ContainerVersion)
	}
}

func TestRefreshContainerVersion_LatestWithPinned(t *testing.T) {
	tc := ToolConfig{Name: "claude", Enabled: true, Mode: ModeLatest, PinnedVersion: "2.1.86", HostVersion: "2.0.0"}
	tc.RefreshContainerVersion()
	if tc.ContainerVersion != "2.1.86" {
		t.Errorf("latest with pinned: expected ContainerVersion=2.1.86 (from PinnedVersion), got %q", tc.ContainerVersion)
	}
}

func TestRefreshContainerVersion_LatestFallbackToHost(t *testing.T) {
	tc := ToolConfig{Name: "claude", Enabled: true, Mode: ModeLatest, HostVersion: "2.0.0"}
	tc.RefreshContainerVersion()
	if tc.ContainerVersion != "2.0.0" {
		t.Errorf("latest fallback: expected ContainerVersion=2.0.0 (from HostVersion), got %q", tc.ContainerVersion)
	}
}

func TestRefreshContainerVersion_LatestNoVersions(t *testing.T) {
	tc := ToolConfig{Name: "claude", Enabled: true, Mode: ModeLatest}
	tc.RefreshContainerVersion()
	if tc.ContainerVersion != "" {
		t.Errorf("latest no versions: expected ContainerVersion empty, got %q", tc.ContainerVersion)
	}
}

func TestRefreshContainerVersion_Disabled(t *testing.T) {
	tc := ToolConfig{Name: "go", Enabled: false, Mode: ModeMirror, HostVersion: "1.24.10"}
	tc.RefreshContainerVersion()
	if tc.ContainerVersion != "" {
		t.Errorf("disabled: expected ContainerVersion empty, got %q", tc.ContainerVersion)
	}
}
