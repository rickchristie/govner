package aitool

import (
	"slices"
	"strings"
	"testing"
)

func TestDefinitionsOrderAndUniqueness(t *testing.T) {
	defs := Definitions()
	wantNames := []string{"claude", "copilot", "codex", "opencode", "grok"}
	wantDisplay := []string{"Claude Code", "Copilot CLI", "Codex CLI", "OpenCode", "Grok Build"}

	if len(defs) != len(wantNames) {
		t.Fatalf("len(Definitions()) = %d, want %d", len(defs), len(wantNames))
	}

	seenName := make(map[string]bool, len(defs))
	seenDisplay := make(map[string]bool, len(defs))
	for i, def := range defs {
		if def.Name != wantNames[i] {
			t.Errorf("Definitions()[%d].Name = %q, want %q", i, def.Name, wantNames[i])
		}
		if def.DisplayName != wantDisplay[i] {
			t.Errorf("Definitions()[%d].DisplayName = %q, want %q", i, def.DisplayName, wantDisplay[i])
		}
		if seenName[def.Name] {
			t.Errorf("duplicate name %q", def.Name)
		}
		if seenDisplay[def.DisplayName] {
			t.Errorf("duplicate display name %q", def.DisplayName)
		}
		seenName[def.Name] = true
		seenDisplay[def.DisplayName] = true
		if def.Name != strings.ToLower(def.Name) {
			t.Errorf("name %q is not lowercase", def.Name)
		}
		if len(def.HostVersionCommand) == 0 {
			t.Errorf("%s: empty HostVersionCommand", def.Name)
		}
		switch def.ClipboardMode {
		case ClipboardShim, ClipboardX11, ClipboardAuto:
		default:
			t.Errorf("%s: invalid ClipboardMode %q", def.Name, def.ClipboardMode)
		}
	}
}

func TestNamesMatchesDefinitions(t *testing.T) {
	defs := Definitions()
	names := Names()
	if len(names) != len(defs) {
		t.Fatalf("len(Names()) = %d, want %d", len(names), len(defs))
	}
	for i, def := range defs {
		if names[i] != def.Name {
			t.Errorf("Names()[%d] = %q, want %q", i, names[i], def.Name)
		}
	}
}

func TestLookupAndIsBuiltin(t *testing.T) {
	for _, name := range []string{"claude", "copilot", "codex", "opencode", "grok"} {
		def, ok := Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) = false, want true", name)
		}
		if def.Name != name {
			t.Errorf("Lookup(%q).Name = %q", name, def.Name)
		}
		if !IsBuiltin(name) {
			t.Errorf("IsBuiltin(%q) = false, want true", name)
		}
	}
	if _, ok := Lookup("not-a-tool"); ok {
		t.Fatal("Lookup(not-a-tool) unexpectedly succeeded")
	}
	if IsBuiltin("not-a-tool") {
		t.Fatal("IsBuiltin(not-a-tool) unexpectedly true")
	}
}

func TestDefinitionsDefensiveCopies(t *testing.T) {
	first := Definitions()
	first[0].Name = "mutated"
	first[0].HostVersionCommand[0] = "mutated"
	first[len(first)-1].HomeDirs[0] = "mutated"

	second := Definitions()
	if second[0].Name != "claude" {
		t.Fatalf("Definitions() leaked mutation of Name: %q", second[0].Name)
	}
	if second[0].HostVersionCommand[0] != "claude" {
		t.Fatalf("Definitions() leaked mutation of HostVersionCommand: %v", second[0].HostVersionCommand)
	}
	grok, ok := Lookup("grok")
	if !ok {
		t.Fatal("Lookup(grok) failed")
	}
	if grok.HomeDirs[0] != ".grok" {
		t.Fatalf("Lookup leaked HomeDirs mutation: %v", grok.HomeDirs)
	}
	if slices.Contains(grok.HomeDirs, "mutated") {
		t.Fatal("HomeDirs copy was not defensive")
	}
}

func TestGrokMetadata(t *testing.T) {
	def, ok := Lookup("grok")
	if !ok {
		t.Fatal("Lookup(grok) failed")
	}
	if def.DisplayName != "Grok Build" {
		t.Errorf("DisplayName = %q, want Grok Build", def.DisplayName)
	}
	if got := slices.Clone(def.HostVersionCommand); !slices.Equal(got, []string{"grok", "--version"}) {
		t.Errorf("HostVersionCommand = %v, want [grok --version]", got)
	}
	if def.AutoApproveArgs != "--always-approve" {
		t.Errorf("AutoApproveArgs = %q", def.AutoApproveArgs)
	}
	if def.ClipboardMode != ClipboardX11 {
		t.Errorf("ClipboardMode = %q, want x11", def.ClipboardMode)
	}
	if !slices.Equal(def.HomeDirs, []string{".grok"}) {
		t.Errorf("HomeDirs = %v", def.HomeDirs)
	}
}

func TestExistingFourToolMetadataUnchanged(t *testing.T) {
	cases := []Definition{
		{
			Name:               "claude",
			DisplayName:        "Claude Code",
			HostVersionCommand: []string{"claude", "--version"},
			AutoApproveArgs:    "--dangerously-skip-permissions",
			ClipboardMode:      ClipboardShim,
			HomeDirs:           []string{".claude"},
		},
		{
			Name:               "copilot",
			DisplayName:        "Copilot CLI",
			HostVersionCommand: []string{"copilot", "--version"},
			AutoApproveArgs:    "--allow-all-tools",
			ClipboardMode:      ClipboardX11,
			HomeDirs:           []string{".copilot"},
		},
		{
			Name:               "codex",
			DisplayName:        "Codex CLI",
			HostVersionCommand: []string{"codex", "--version"},
			AutoApproveArgs:    "--dangerously-bypass-approvals-and-sandbox",
			ClipboardMode:      ClipboardX11,
			HomeDirs:           []string{".codex"},
		},
		{
			Name:               "opencode",
			DisplayName:        "OpenCode",
			HostVersionCommand: []string{"opencode", "--version"},
			AutoApproveArgs:    "",
			ClipboardMode:      ClipboardShim,
			HomeDirs: []string{
				".config/opencode",
				".local/share/opencode",
				".local/state/opencode",
				".opencode",
			},
		},
	}
	for _, want := range cases {
		got, ok := Lookup(want.Name)
		if !ok {
			t.Fatalf("Lookup(%q) failed", want.Name)
		}
		if got.DisplayName != want.DisplayName {
			t.Errorf("%s DisplayName = %q, want %q", want.Name, got.DisplayName, want.DisplayName)
		}
		if !slices.Equal(got.HostVersionCommand, want.HostVersionCommand) {
			t.Errorf("%s HostVersionCommand = %v, want %v", want.Name, got.HostVersionCommand, want.HostVersionCommand)
		}
		if got.AutoApproveArgs != want.AutoApproveArgs {
			t.Errorf("%s AutoApproveArgs = %q, want %q", want.Name, got.AutoApproveArgs, want.AutoApproveArgs)
		}
		if got.ClipboardMode != want.ClipboardMode {
			t.Errorf("%s ClipboardMode = %q, want %q", want.Name, got.ClipboardMode, want.ClipboardMode)
		}
		if !slices.Equal(got.HomeDirs, want.HomeDirs) {
			t.Errorf("%s HomeDirs = %v, want %v", want.Name, got.HomeDirs, want.HomeDirs)
		}
	}
}

func TestClipboardModeFallback(t *testing.T) {
	if got := ClipboardMode("grok"); got != ClipboardX11 {
		t.Fatalf("ClipboardMode(grok) = %q, want x11", got)
	}
	if got := ClipboardMode("custom-tool"); got != ClipboardAuto {
		t.Fatalf("ClipboardMode(custom) = %q, want auto", got)
	}
}
