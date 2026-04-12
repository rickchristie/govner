package config

import (
	"fmt"
	"testing"
	"time"
)

func TestEffectiveProgrammingToolVersion(t *testing.T) {
	t.Run("mirror", func(t *testing.T) {
		cfg := &Config{ProgrammingTools: []ToolConfig{{Name: "go", Enabled: true, Mode: ModeMirror, HostVersion: "1.24.10"}}}
		got, enabled, err := EffectiveProgrammingToolVersion(cfg, "go")
		if err != nil {
			t.Fatalf("EffectiveProgrammingToolVersion() error = %v", err)
		}
		if !enabled || got != "1.24.10" {
			t.Fatalf("got (%q, %v), want (1.24.10, true)", got, enabled)
		}
	})

	t.Run("pin", func(t *testing.T) {
		cfg := &Config{ProgrammingTools: []ToolConfig{{Name: "node", Enabled: true, Mode: ModePin, PinnedVersion: "22.12.0"}}}
		got, enabled, err := EffectiveProgrammingToolVersion(cfg, "node")
		if err != nil {
			t.Fatalf("EffectiveProgrammingToolVersion() error = %v", err)
		}
		if !enabled || got != "22.12.0" {
			t.Fatalf("got (%q, %v), want (22.12.0, true)", got, enabled)
		}
	})

	t.Run("latest uses pinned", func(t *testing.T) {
		cfg := &Config{ProgrammingTools: []ToolConfig{{Name: "python", Enabled: true, Mode: ModeLatest, PinnedVersion: "3.12.3"}}}
		got, enabled, err := EffectiveProgrammingToolVersion(cfg, "python")
		if err != nil {
			t.Fatalf("EffectiveProgrammingToolVersion() error = %v", err)
		}
		if !enabled || got != "3.12.3" {
			t.Fatalf("got (%q, %v), want (3.12.3, true)", got, enabled)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := &Config{ProgrammingTools: []ToolConfig{{Name: "go", Enabled: false, Mode: ModePin, PinnedVersion: "1.24.10"}}}
		got, enabled, err := EffectiveProgrammingToolVersion(cfg, "go")
		if err != nil {
			t.Fatalf("EffectiveProgrammingToolVersion() error = %v", err)
		}
		if enabled || got != "" {
			t.Fatalf("got (%q, %v), want (empty, false)", got, enabled)
		}
	})

	t.Run("enabled but missing concrete version", func(t *testing.T) {
		cfg := &Config{ProgrammingTools: []ToolConfig{{Name: "go", Enabled: true, Mode: ModeLatest}}}
		_, _, err := EffectiveProgrammingToolVersion(cfg, "go")
		if err == nil {
			t.Fatal("expected error for enabled tool without concrete desired version")
		}
	})
}

func TestEffectiveBaseNodeVersion(t *testing.T) {
	cfg := &Config{ProgrammingTools: []ToolConfig{{Name: "node", Enabled: true, Mode: ModePin, PinnedVersion: "20.11.1"}}}
	got, err := EffectiveBaseNodeVersion(cfg)
	if err != nil {
		t.Fatalf("EffectiveBaseNodeVersion() error = %v", err)
	}
	if got != "20.11.1" {
		t.Fatalf("got %q, want 20.11.1", got)
	}

	cfg = &Config{}
	got, err = EffectiveBaseNodeVersion(cfg)
	if err != nil {
		t.Fatalf("EffectiveBaseNodeVersion() error = %v", err)
	}
	if got != DefaultBaseNodeVersion {
		t.Fatalf("got %q, want %s", got, DefaultBaseNodeVersion)
	}
}

func TestResolveGoplsVersion(t *testing.T) {
	prev := GoplsLatestResolver
	GoplsLatestResolver = func() (string, error) { return "v0.21.1", nil }
	defer func() { GoplsLatestResolver = prev }()

	tests := []struct {
		name      string
		goVersion string
		want      string
		wantErr   bool
	}{
		{name: "go121plus uses latest", goVersion: "1.24.10", want: "v0.21.1"},
		{name: "go120", goVersion: "1.20.14", want: "v0.15.3"},
		{name: "go118", goVersion: "1.18.10", want: "v0.14.2"},
		{name: "go117", goVersion: "1.17.13", want: "v0.11.0"},
		{name: "go115", goVersion: "1.15.15", want: "v0.9.5"},
		{name: "go112", goVersion: "1.12.17", want: "v0.7.5"},
		{name: "unsupported", goVersion: "1.11.13", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveGoplsVersion(tt.goVersion)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveGoplsVersion() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTypeScriptLanguageServerVersion(t *testing.T) {
	prev := NPMPackageLatestResolver
	NPMPackageLatestResolver = func(name string) (string, error) {
		if name != "typescript-language-server" {
			return "", fmt.Errorf("unexpected package %s", name)
		}
		return "5.1.3", nil
	}
	defer func() { NPMPackageLatestResolver = prev }()

	tests := []struct {
		name        string
		nodeVersion string
		want        string
		wantErr     bool
	}{
		{name: "node20 uses latest", nodeVersion: "20.11.0", want: "5.1.3"},
		{name: "node18", nodeVersion: "18.19.0", want: "4.4.1"},
		{name: "node16", nodeVersion: "16.20.2", want: "3.3.2"},
		{name: "too old", nodeVersion: "14.16.0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveTypeScriptLanguageServerVersion(tt.nodeVersion)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveTypeScriptLanguageServerVersion() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTypeScriptPackageVersion(t *testing.T) {
	prevLatest := NPMPackageLatestResolver
	prevMeta := NPMPackageMetadataResolver
	defer func() {
		NPMPackageLatestResolver = prevLatest
		NPMPackageMetadataResolver = prevMeta
	}()

	NPMPackageLatestResolver = func(name string) (string, error) {
		if name != "typescript" {
			return "", fmt.Errorf("unexpected package %s", name)
		}
		return "6.0.2", nil
	}

	t.Run("uses latest when compatible", func(t *testing.T) {
		NPMPackageMetadataResolver = func(name, version string) (NPMPackageMetadata, error) {
			var meta NPMPackageMetadata
			meta.Version = version
			meta.Engines.Node = ">=20"
			return meta, nil
		}
		got, err := ResolveTypeScriptPackageVersion("20.11.0")
		if err != nil {
			t.Fatalf("ResolveTypeScriptPackageVersion() error = %v", err)
		}
		if got != "6.0.2" {
			t.Fatalf("got %q, want 6.0.2", got)
		}
	})

	t.Run("falls back when latest incompatible", func(t *testing.T) {
		NPMPackageMetadataResolver = func(name, version string) (NPMPackageMetadata, error) {
			var meta NPMPackageMetadata
			meta.Version = version
			switch version {
			case "6.0.2":
				meta.Engines.Node = ">=20"
			case "5.8.3":
				meta.Engines.Node = ">=14.17"
			}
			return meta, nil
		}
		got, err := ResolveTypeScriptPackageVersion("18.19.0")
		if err != nil {
			t.Fatalf("ResolveTypeScriptPackageVersion() error = %v", err)
		}
		if got != "5.8.3" {
			t.Fatalf("got %q, want 5.8.3", got)
		}
	})

	t.Run("too old even for fallback", func(t *testing.T) {
		NPMPackageMetadataResolver = func(name, version string) (NPMPackageMetadata, error) {
			var meta NPMPackageMetadata
			meta.Version = version
			meta.Engines.Node = ">=14.17"
			return meta, nil
		}
		if _, err := ResolveTypeScriptPackageVersion("14.16.0"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestResolvePythonLSPServerVersion(t *testing.T) {
	prevLatest := PyPIPackageLatestResolver
	prevMeta := PyPIPackageVersionMetadataResolver
	defer func() {
		PyPIPackageLatestResolver = prevLatest
		PyPIPackageVersionMetadataResolver = prevMeta
	}()
	PyPIPackageLatestResolver = func(name string) (string, error) {
		if name != "python-lsp-server" {
			return "", fmt.Errorf("unexpected package %s", name)
		}
		return "1.14.0", nil
	}
	PyPIPackageVersionMetadataResolver = func(name, version string) (PyPIPackageMetadata, error) {
		return PyPIPackageMetadata{Version: version, RequiresPython: ">=3.9"}, nil
	}

	tests := []struct {
		name          string
		pythonVersion string
		want          string
		wantErr       bool
	}{
		{name: "python39plus uses latest", pythonVersion: "3.12.1", want: "1.14.0"},
		{name: "python38", pythonVersion: "3.8.18", want: "1.12.2"},
		{name: "python37", pythonVersion: "3.7.17", want: "1.7.4"},
		{name: "python36", pythonVersion: "3.6.15", want: "1.3.3"},
		{name: "unsupported", pythonVersion: "3.5.10", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePythonLSPServerVersion(tt.pythonVersion)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolvePythonLSPServerVersion() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePyrightVersion(t *testing.T) {
	prevLatest := NPMPackageLatestResolver
	prevMeta := NPMPackageMetadataResolver
	defer func() {
		NPMPackageLatestResolver = prevLatest
		NPMPackageMetadataResolver = prevMeta
	}()
	NPMPackageLatestResolver = func(name string) (string, error) {
		if name != "pyright" {
			return "", fmt.Errorf("unexpected package %s", name)
		}
		return "1.1.500", nil
	}

	t.Run("uses latest when compatible", func(t *testing.T) {
		NPMPackageMetadataResolver = func(name, version string) (NPMPackageMetadata, error) {
			var meta NPMPackageMetadata
			meta.Version = version
			meta.Engines.Node = ">=14.0.0"
			return meta, nil
		}
		got, err := ResolvePyrightVersion("20.11.0")
		if err != nil {
			t.Fatalf("ResolvePyrightVersion() error = %v", err)
		}
		if got != "1.1.500" {
			t.Fatalf("got %q, want 1.1.500", got)
		}
	})

	t.Run("falls back when latest incompatible", func(t *testing.T) {
		NPMPackageMetadataResolver = func(name, version string) (NPMPackageMetadata, error) {
			var meta NPMPackageMetadata
			meta.Version = version
			switch version {
			case "1.1.500":
				meta.Engines.Node = ">=20"
			case "1.1.408":
				meta.Engines.Node = ">=14.0.0"
			}
			return meta, nil
		}
		got, err := ResolvePyrightVersion("18.19.0")
		if err != nil {
			t.Fatalf("ResolvePyrightVersion() error = %v", err)
		}
		if got != "1.1.408" {
			t.Fatalf("got %q, want 1.1.408", got)
		}
	})

	t.Run("too old node", func(t *testing.T) {
		if _, err := ResolvePyrightVersion("13.9.0"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestResolveImplicitTools(t *testing.T) {
	prevGopls := GoplsLatestResolver
	prevNPMLatest := NPMPackageLatestResolver
	prevNPMMeta := NPMPackageMetadataResolver
	prevPyPILatest := PyPIPackageLatestResolver
	prevPyPIMeta := PyPIPackageVersionMetadataResolver
	defer func() {
		GoplsLatestResolver = prevGopls
		NPMPackageLatestResolver = prevNPMLatest
		NPMPackageMetadataResolver = prevNPMMeta
		PyPIPackageLatestResolver = prevPyPILatest
		PyPIPackageVersionMetadataResolver = prevPyPIMeta
	}()

	GoplsLatestResolver = func() (string, error) { return "v0.21.1", nil }
	NPMPackageLatestResolver = func(name string) (string, error) {
		switch name {
		case "typescript-language-server":
			return "5.1.3", nil
		case "typescript":
			return "6.0.2", nil
		case "pyright":
			return "1.1.500", nil
		default:
			return "", fmt.Errorf("unexpected package %s", name)
		}
	}
	NPMPackageMetadataResolver = func(name, version string) (NPMPackageMetadata, error) {
		var meta NPMPackageMetadata
		meta.Version = version
		meta.Engines.Node = ">=14.0.0"
		if name == "typescript-language-server" {
			meta.Engines.Node = ">=20"
		}
		if name == "typescript" {
			meta.Engines.Node = ">=14.17"
		}
		return meta, nil
	}
	PyPIPackageLatestResolver = func(name string) (string, error) { return "1.14.0", nil }
	PyPIPackageVersionMetadataResolver = func(name, version string) (PyPIPackageMetadata, error) {
		return PyPIPackageMetadata{Version: version, RequiresPython: ">=3.9"}, nil
	}

	t.Run("all programming tools", func(t *testing.T) {
		cfg := &Config{ProgrammingTools: []ToolConfig{
			{Name: "go", Enabled: true, Mode: ModePin, PinnedVersion: "1.24.10"},
			{Name: "node", Enabled: true, Mode: ModePin, PinnedVersion: "22.12.0"},
			{Name: "python", Enabled: true, Mode: ModePin, PinnedVersion: "3.12.1"},
		}}
		got, err := ResolveImplicitTools(cfg)
		if err != nil {
			t.Fatalf("ResolveImplicitTools() error = %v", err)
		}
		if len(got) != 5 {
			t.Fatalf("len(got) = %d, want 5", len(got))
		}
		if got[0].Name != "gopls" || got[1].Name != "typescript-language-server" || got[4].Name != "python-lsp-server" {
			t.Fatalf("unexpected ordering: %+v", got)
		}
	})

	t.Run("python only uses base node", func(t *testing.T) {
		cfg := &Config{ProgrammingTools: []ToolConfig{{Name: "python", Enabled: true, Mode: ModePin, PinnedVersion: "3.12.1"}}}
		got, err := ResolveImplicitTools(cfg)
		if err != nil {
			t.Fatalf("ResolveImplicitTools() error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(got) = %d, want 2", len(got))
		}
		if got[0].Name != "pyright" || got[1].Name != "python-lsp-server" {
			t.Fatalf("unexpected python-only implicit tools: %+v", got)
		}
	})
}

func TestCompareImplicitToolsAndVisibility(t *testing.T) {
	built := []ImplicitToolConfig{{Name: "gopls", Kind: ImplicitToolKindLSP, ParentTool: "go", Binary: "gopls", ContainerVersion: "v0.15.3"}}
	target := []ImplicitToolConfig{
		{Name: "gopls", Kind: ImplicitToolKindLSP, ParentTool: "go", Binary: "gopls", ContainerVersion: "v0.21.1"},
		{Name: "typescript", Kind: ImplicitToolKindSupport, ParentTool: "node", Binary: "tsc", ContainerVersion: "5.8.3"},
	}
	warnings := CompareImplicitTools(built, target)
	if len(warnings) != 2 {
		t.Fatalf("len(warnings) = %d, want 2 (%v)", len(warnings), warnings)
	}
	visible := VisibleImplicitLSPs(target)
	if len(visible) != 1 || visible[0].Name != "gopls" {
		t.Fatalf("VisibleImplicitLSPs() = %+v, want only gopls", visible)
	}
}

func TestRefreshDesiredToolVersionsBestEffort(t *testing.T) {
	prevLatest := LatestVersionResolver
	prevHost := HostVersionDetector
	defer func() {
		LatestVersionResolver = prevLatest
		HostVersionDetector = prevHost
	}()

	LatestVersionResolver = func(name string) (string, error) {
		if name == "go" {
			return "1.24.10", nil
		}
		return "", fmt.Errorf("boom")
	}
	HostVersionDetector = func(name string) (string, error) {
		if name == "claude" {
			return "2.1.87", nil
		}
		return "", fmt.Errorf("boom")
	}

	cfg := &Config{
		ProgrammingTools: []ToolConfig{{Name: "go", Enabled: true, Mode: ModeLatest}},
		AITools: []ToolConfig{
			{Name: "claude", Enabled: true, Mode: ModeMirror},
			{Name: "codex", Enabled: true, Mode: ModeLatest},
		},
	}
	errs := RefreshDesiredToolVersionsBestEffort(cfg, time.Second)
	if cfg.ProgrammingTools[0].PinnedVersion != "1.24.10" {
		t.Fatalf("go pinned version = %q, want 1.24.10", cfg.ProgrammingTools[0].PinnedVersion)
	}
	if cfg.AITools[0].HostVersion != "2.1.87" {
		t.Fatalf("claude host version = %q, want 2.1.87", cfg.AITools[0].HostVersion)
	}
	if errs["codex"] == nil {
		t.Fatal("expected codex refresh error")
	}
}

func TestResolveImplicitToolsWithOptions_UsesBuiltFallbackWhenLatestLookupsFail(t *testing.T) {
	prevGopls := GoplsLatestResolver
	prevNPMLatest := NPMPackageLatestResolver
	prevNPMMeta := NPMPackageMetadataResolver
	prevPyPILatest := PyPIPackageLatestResolver
	defer func() {
		GoplsLatestResolver = prevGopls
		NPMPackageLatestResolver = prevNPMLatest
		NPMPackageMetadataResolver = prevNPMMeta
		PyPIPackageLatestResolver = prevPyPILatest
	}()

	GoplsLatestResolver = func() (string, error) { return "", fmt.Errorf("offline") }
	NPMPackageLatestResolver = func(name string) (string, error) { return "", fmt.Errorf("offline") }
	NPMPackageMetadataResolver = func(name, version string) (NPMPackageMetadata, error) {
		return NPMPackageMetadata{}, fmt.Errorf("offline")
	}
	PyPIPackageLatestResolver = func(name string) (string, error) { return "", fmt.Errorf("offline") }

	cfg := &Config{
		ProgrammingTools: []ToolConfig{
			{Name: "go", Enabled: true, Mode: ModeLatest, PinnedVersion: "1.24.10", ContainerVersion: "1.24.10"},
			{Name: "node", Enabled: true, Mode: ModeLatest, PinnedVersion: "22.12.0", ContainerVersion: "22.12.0"},
			{Name: "python", Enabled: true, Mode: ModeLatest, PinnedVersion: "3.12.1", ContainerVersion: "3.12.1"},
		},
		ImplicitTools: []ImplicitToolConfig{
			{Name: "gopls", Kind: ImplicitToolKindLSP, ParentTool: "go", Binary: "gopls", ContainerVersion: "v0.20.0"},
			{Name: "typescript-language-server", Kind: ImplicitToolKindLSP, ParentTool: "node", Binary: "typescript-language-server", ContainerVersion: "4.4.1"},
			{Name: "typescript", Kind: ImplicitToolKindSupport, ParentTool: "node", Binary: "tsc", ContainerVersion: "5.8.3"},
			{Name: "pyright", Kind: ImplicitToolKindLSP, ParentTool: "python", Binary: "pyright-langserver", ContainerVersion: "1.1.408"},
			{Name: "python-lsp-server", Kind: ImplicitToolKindLSP, ParentTool: "python", Binary: "pylsp", ContainerVersion: "1.14.0"},
		},
	}

	tools, warnings, err := ResolveImplicitToolsWithOptions(cfg, ImplicitToolResolveOptions{AllowStaleFallback: true})
	if err != nil {
		t.Fatalf("ResolveImplicitToolsWithOptions() error = %v", err)
	}
	if len(tools) != 5 {
		t.Fatalf("len(tools) = %d, want 5", len(tools))
	}
	if len(warnings) != 5 {
		t.Fatalf("len(warnings) = %d, want 5 (%v)", len(warnings), warnings)
	}
	versions := map[string]string{}
	for _, tool := range tools {
		versions[tool.Name] = tool.ContainerVersion
	}
	if versions["gopls"] != "v0.20.0" || versions["typescript-language-server"] != "4.4.1" || versions["pyright"] != "1.1.408" || versions["python-lsp-server"] != "1.14.0" {
		t.Fatalf("unexpected fallback versions: %+v", versions)
	}
}

func TestResolveImplicitToolsWithOptions_PyrightFallbackRequiresBuiltBaseNodeVersion(t *testing.T) {
	prevNPMLatest := NPMPackageLatestResolver
	prevPyPILatest := PyPIPackageLatestResolver
	defer func() {
		NPMPackageLatestResolver = prevNPMLatest
		PyPIPackageLatestResolver = prevPyPILatest
	}()

	NPMPackageLatestResolver = func(name string) (string, error) { return "", fmt.Errorf("offline") }
	PyPIPackageLatestResolver = func(name string) (string, error) { return "1.14.0", nil }

	t.Run("fails without persisted built base node version", func(t *testing.T) {
		cfg := &Config{
			ProgrammingTools: []ToolConfig{{Name: "python", Enabled: true, Mode: ModePin, PinnedVersion: "3.12.1", ContainerVersion: "3.12.1"}},
			ImplicitTools: []ImplicitToolConfig{
				{Name: "pyright", Kind: ImplicitToolKindLSP, ParentTool: "python", Binary: "pyright-langserver", ContainerVersion: "1.1.408"},
				{Name: "python-lsp-server", Kind: ImplicitToolKindLSP, ParentTool: "python", Binary: "pylsp", ContainerVersion: "1.14.0"},
			},
		}
		if _, _, err := ResolveImplicitToolsWithOptions(cfg, ImplicitToolResolveOptions{AllowStaleFallback: true}); err == nil {
			t.Fatal("expected pyright fallback to fail without built base node version")
		}
	})

	t.Run("succeeds when built base node version matches desired base node", func(t *testing.T) {
		cfg := &Config{
			ProgrammingTools: []ToolConfig{{Name: "python", Enabled: true, Mode: ModePin, PinnedVersion: "3.12.1", ContainerVersion: "3.12.1"}},
			BaseNodeVersion:  DefaultBaseNodeVersion,
			ImplicitTools: []ImplicitToolConfig{
				{Name: "pyright", Kind: ImplicitToolKindLSP, ParentTool: "python", Binary: "pyright-langserver", ContainerVersion: "1.1.408"},
				{Name: "python-lsp-server", Kind: ImplicitToolKindLSP, ParentTool: "python", Binary: "pylsp", ContainerVersion: "1.14.0"},
			},
		}
		tools, warnings, err := ResolveImplicitToolsWithOptions(cfg, ImplicitToolResolveOptions{AllowStaleFallback: true})
		if err != nil {
			t.Fatalf("ResolveImplicitToolsWithOptions() error = %v", err)
		}
		if len(tools) != 2 {
			t.Fatalf("len(tools) = %d, want 2", len(tools))
		}
		if tools[0].Name != "pyright" || tools[0].ContainerVersion != "1.1.408" {
			t.Fatalf("unexpected pyright fallback tool: %+v", tools)
		}
		if len(warnings) == 0 {
			t.Fatal("expected pyright fallback warning")
		}
	})
}

func TestBuiltBaseNodeVersion(t *testing.T) {
	t.Run("uses explicit persisted field first", func(t *testing.T) {
		cfg := &Config{
			BaseNodeVersion: "22.12.0",
			ProgrammingTools: []ToolConfig{
				{Name: "node", Enabled: true, Mode: ModePin, ContainerVersion: "20.11.1"},
			},
		}
		got, ok := BuiltBaseNodeVersion(cfg)
		if !ok || got != "22.12.0" {
			t.Fatalf("BuiltBaseNodeVersion() = (%q, %v), want (22.12.0, true)", got, ok)
		}
	})

	t.Run("falls back to built node tool version", func(t *testing.T) {
		cfg := &Config{ProgrammingTools: []ToolConfig{{Name: "node", Enabled: true, Mode: ModePin, ContainerVersion: "20.11.1"}}}
		got, ok := BuiltBaseNodeVersion(cfg)
		if !ok || got != "20.11.1" {
			t.Fatalf("BuiltBaseNodeVersion() = (%q, %v), want (20.11.1, true)", got, ok)
		}
	})

	t.Run("disabled node without persisted field is unknown", func(t *testing.T) {
		cfg := &Config{ProgrammingTools: []ToolConfig{{Name: "node", Enabled: false, Mode: ModeOff, ContainerVersion: "20.11.1"}}}
		got, ok := BuiltBaseNodeVersion(cfg)
		if ok || got != "" {
			t.Fatalf("BuiltBaseNodeVersion() = (%q, %v), want (empty, false)", got, ok)
		}
	})
}

func TestBaseNodeVersionDrift(t *testing.T) {
	t.Run("detects disabled-node drift", func(t *testing.T) {
		cfg := &Config{BaseNodeVersion: "20.11.1"}
		built, expected, mismatch, err := BaseNodeVersionDrift(cfg)
		if err != nil {
			t.Fatalf("BaseNodeVersionDrift() error = %v", err)
		}
		if !mismatch || built != "20.11.1" || expected != DefaultBaseNodeVersion {
			t.Fatalf("BaseNodeVersionDrift() = (%q, %q, %v), want (20.11.1, %s, true)", built, expected, mismatch, DefaultBaseNodeVersion)
		}
	})

	t.Run("old config with enabled node can still infer built runtime", func(t *testing.T) {
		cfg := &Config{ProgrammingTools: []ToolConfig{{Name: "node", Enabled: true, Mode: ModePin, PinnedVersion: "20.11.1", ContainerVersion: "20.11.1"}}}
		built, expected, mismatch, err := BaseNodeVersionDrift(cfg)
		if err != nil {
			t.Fatalf("BaseNodeVersionDrift() error = %v", err)
		}
		if mismatch || built != "20.11.1" || expected != "20.11.1" {
			t.Fatalf("BaseNodeVersionDrift() = (%q, %q, %v), want (20.11.1, 20.11.1, false)", built, expected, mismatch)
		}
	})
}
