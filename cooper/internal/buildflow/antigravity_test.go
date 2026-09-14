package buildflow

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/antigravity"
)

func TestBuildSetsUpOnlyHostAntigravity(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("host setup uses the Linux file fallback")
	}
	home := t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("COOPER_VM_CONTEXT", filepath.Join(t.TempDir(), "absent-context"))
	t.Setenv("COOPER_CLI_TOOL", "")
	t.Setenv("ZDOTDIR", "")
	var out bytes.Buffer
	prepared := &Prepared{plan: plan{enabledAITools: []string{"codex"}}}
	if err := prepared.prepareHostAuth(Options{Out: &out}, home); err != nil {
		t.Fatal(err)
	}
	setup := antigravity.HostPaths(home)
	if _, err := os.Stat(setup.Wrapper); !os.IsNotExist(err) {
		t.Fatal("Codex build installed an Antigravity wrapper")
	}
	prepared.plan.enabledAITools = []string{"antigravity"}
	t.Setenv("COOPER_CLI_TOOL", "codex")
	if err := prepared.prepareHostAuth(Options{Out: &out}, home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(setup.Wrapper); !os.IsNotExist(err) {
		t.Fatal("an inner Cooper build changed host shell setup")
	}
	t.Setenv("COOPER_CLI_TOOL", "")
	if err := prepared.prepareHostAuth(Options{Out: &out}, home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(setup.Wrapper); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"physical host", "Activate it in this shell:", "cooper save/load"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("build output omitted %q", want)
		}
	}
}
