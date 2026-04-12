package about

import (
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestAboutView_RendersImplicitLanguageServersSection(t *testing.T) {
	cfg := &config.Config{
		ImplicitTools: []config.ImplicitToolConfig{{Name: "gopls", Kind: config.ImplicitToolKindLSP, ParentTool: "go", Binary: "gopls", ContainerVersion: "v0.21.1"}},
	}
	view := New(cfg).View(120, 40)
	if !strings.Contains(view, "Implicit Language Servers") {
		t.Fatalf("expected implicit language servers section, got:\n%s", view)
	}
	if !strings.Contains(view, "gopls") {
		t.Fatalf("expected gopls row, got:\n%s", view)
	}
}

func TestAboutView_OmitsSupportEntries(t *testing.T) {
	cfg := &config.Config{
		ImplicitTools: []config.ImplicitToolConfig{
			{Name: "typescript", Kind: config.ImplicitToolKindSupport, ParentTool: "node", Binary: "tsc", ContainerVersion: "5.8.3"},
			{Name: "typescript-language-server", Kind: config.ImplicitToolKindLSP, ParentTool: "node", Binary: "typescript-language-server", ContainerVersion: "5.1.3"},
		},
	}
	view := New(cfg).View(120, 40)
	if strings.Contains(view, "typescript  ") || strings.Contains(view, "tsc") {
		t.Fatalf("support tool should not be rendered in About view:\n%s", view)
	}
	if !strings.Contains(view, "typescript-language-server") {
		t.Fatalf("expected LSP row to remain visible:\n%s", view)
	}
}

func TestAboutView_ShowsImplicitToolParentAndBinary(t *testing.T) {
	cfg := &config.Config{
		ImplicitTools: []config.ImplicitToolConfig{{Name: "python-lsp-server", Kind: config.ImplicitToolKindLSP, ParentTool: "python", Binary: "pylsp", ContainerVersion: "1.14.0"}},
	}
	view := New(cfg).View(120, 40)
	if !strings.Contains(view, "python") || !strings.Contains(view, "pylsp") {
		t.Fatalf("expected parent tool and binary columns in About view:\n%s", view)
	}
}

func TestAboutView_ShowsStartupWarningsForImplicitToolMismatches(t *testing.T) {
	m := New(&config.Config{ImplicitTools: []config.ImplicitToolConfig{{Name: "gopls", Kind: config.ImplicitToolKindLSP, ParentTool: "go", Binary: "gopls", ContainerVersion: "v0.15.3"}}})
	updated, _ := m.Update(StartupWarningsMsg{Warnings: []string{"gopls (for go): container=v0.15.3, expected=v0.21.1"}})
	view := updated.(*Model).View(120, 40)
	if !strings.Contains(view, "gopls (for go): container=v0.15.3, expected=v0.21.1") {
		t.Fatalf("expected startup warning banner to include implicit mismatch:\n%s", view)
	}
	if !strings.Contains(view, "Version mismatches detected") {
		t.Fatalf("expected mismatch banner:\n%s", view)
	}
}

func TestAboutView_NoImplicitToolsSectionWhenEmpty(t *testing.T) {
	view := New(&config.Config{}).View(120, 40)
	if strings.Contains(view, "Implicit Language Servers") {
		t.Fatalf("did not expect implicit tools section when config has none:\n%s", view)
	}
}
