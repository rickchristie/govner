package workload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileMountValidationRejectsStorageAndTargetTraps(t *testing.T) {
	store := t.TempDir()
	path := filepath.Join(store, "profiles", "harnesses", "codex", strings.Repeat("a", 24), strings.Repeat("b", 24), "roots", "codex-state")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	mount := MountSpec{ID: "codex-state", Source: path, Target: filepath.Join(t.TempDir(), ".codex"), Kind: Directory, Access: ReadWrite, Ownership: ProfileState}
	if err := ValidateMountPlan([]MountSpec{mount}, store); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*MountSpec){
		func(m *MountSpec) { m.Source = store },
		func(m *MountSpec) { m.Target = "/etc/cooper" },
		func(m *MountSpec) { m.Access = ReadOnly },
		func(m *MountSpec) { m.ID = "other-agent-state" },
	} {
		bad := mount
		change(&bad)
		if err := ValidateMountPlan([]MountSpec{bad}, store); err == nil {
			t.Fatalf("unsafe profile mount accepted: %+v", bad)
		}
	}
	outside := t.TempDir()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMountPlan([]MountSpec{mount}, store); err == nil {
		t.Fatal("profile root symlink accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMountPlan([]MountSpec{mount}, store); err == nil {
		t.Fatal("public profile storage accepted")
	}
}
