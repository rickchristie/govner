package workload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAllHostAgentStateRootsRejectsEachBuiltInRoot(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	home := t.TempDir()
	roots, err := HostAgentStateRoots(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, stateRoot := range roots {
		stateRoot := stateRoot
		t.Run(filepath.Base(stateRoot), func(t *testing.T) {
			cooperDir := filepath.Join(stateRoot, "cooper-owned")
			if err := ValidateAllHostAgentStateRoots(home, cooperDir); err == nil || !strings.Contains(err.Error(), "host-owned agent state") {
				t.Fatalf("ValidateAllHostAgentStateRoots() error = %v, want overlap error", err)
			}
		})
	}
}

func TestValidateAllHostAgentStateRootsRejectsResolvedOverlap(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, ".codex")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "state-link")
	if err := os.Symlink(state, link); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAllHostAgentStateRoots(home, filepath.Join(link, "cooper")); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("ValidateAllHostAgentStateRoots() error = %v, want resolved overlap error", err)
	}
}

func TestValidateAllHostAgentStateRootsAllowsSiblingDirectory(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cooperDir := filepath.Join(root, "cooper")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAllHostAgentStateRoots(home, cooperDir); err != nil {
		t.Fatalf("ValidateAllHostAgentStateRoots() error = %v", err)
	}
}

func TestResolveExistingPathKeepsMissingLinkTargets(t *testing.T) {
	for _, relative := range []bool{false, true} {
		t.Run(map[bool]string{false: "absolute", true: "relative"}[relative], func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "missing", "state")
			linkTarget := target
			if relative {
				linkTarget = "missing/state"
			}
			link := filepath.Join(root, "link")
			if err := os.Symlink(linkTarget, link); err != nil {
				t.Fatal(err)
			}
			chain := filepath.Join(root, "chain")
			if err := os.Symlink("link", chain); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{link, chain} {
				for _, child := range []string{"", "sessions/new"} {
					got, err := resolveExistingPath(filepath.Join(path, child))
					if err != nil || got != filepath.Join(target, child) {
						t.Fatalf("resolve %s: %q, %v", path, got, err)
					}
				}
			}
			if err := ValidateHostOwnedPath(link, filepath.Join(target, "cooper")); err == nil {
				t.Fatal("missing link target hid a Cooper directory overlap")
			}
		})
	}
}

func TestResolveExistingPathRejectsLinkCycle(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("second", filepath.Join(root, "first")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("first", filepath.Join(root, "second")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveExistingPath(filepath.Join(root, "first", "missing")); err == nil {
		t.Fatal("accepted a link cycle")
	}
}
