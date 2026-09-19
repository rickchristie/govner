//go:build linux

package profiles

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManagedNativeCodexResume(t *testing.T) {
	binary := os.Getenv("COOPER_NATIVE_PROFILE_CODEX")
	if binary == "" {
		t.Skip("set COOPER_NATIVE_PROFILE_CODEX to the reviewed native Codex executable")
	}
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.service.options.Environment["CODEX_HOME"] = filepath.Join(f.home, ".codex")
	f.save("codex")
	f.migrate()
	turn := func(thread string) string {
		t.Helper()
		args := []string{"testdata/codex-native.mjs", binary, f.home}
		if thread != "" {
			args = append(args, thread)
		}
		command := exec.CommandContext(t.Context(), "node", args...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("native Codex: %v\n%s", err, output)
		}
		var result struct {
			Thread string `json:"thread"`
		}
		if err := json.Unmarshal(output, &result); err != nil || result.Thread == "" {
			t.Fatalf("native result: %v %s", err, output)
		}
		return result.Thread
	}
	thread := turn("")
	f.load("codex", "Work")
	f.write(".codex/account", "work")
	f.save("codex")
	f.load("codex", "Default")
	turn(thread)
	backup := filepath.Join(t.TempDir(), "backup")
	if err := f.service.Backup(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Restore(t.Context(), "codex", "Default", backup); err != nil {
		t.Fatal(err)
	}
	turn(thread)
	if _, err := f.service.Detach(t.Context()); err != nil {
		t.Fatal(err)
	}
	turn(thread)
	f.load("codex", "Work")
	f.load("codex", "Default")
	turn(thread)
	if err := f.service.PruneRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	turn(thread)
	f.migrate()
	turn(thread)
	// Relocation retains the small old-path aliases, while all live bytes move
	// to the new store. Native absolute paths must still reach those new bytes.
	relocated := t.TempDir()
	if err := f.service.Backup(t.Context(), filepath.Join(relocated, "profiles")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Detach(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.service.options.CooperDir = relocated
	f.migrate()
	turn(thread)
}
