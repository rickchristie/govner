package antigravity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDriverVersionUsesSelectedBinary(t *testing.T) {
	for _, test := range []struct {
		name, data, version string
	}{
		{"current", "\x00.cache/ms-playwright-go/1.57.0OtherText\x00", "1.57.0"},
		{"new dependency", "\x00.cache/ms-playwright-go/2.14.6OtherText\x00", "2.14.6"},
		{"repeated path", ".cache/ms-playwright-go/1.57.0\x00.cache/ms-playwright-go/1.57.0", "1.57.0"},
		{"unrelated version", "native 9.1.0\x00.cache/ms-playwright-go/1.62.1\x001.57.0", "1.62.1"},
		{"missing", "native 9.1.0\x001.57.0", ""},
		{"incomplete", ".cache/ms-playwright-go/1.57", ""},
		{"ambiguous", ".cache/ms-playwright-go/1.57.0\x00.cache/ms-playwright-go/1.62.1", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			helper := filepath.Join(directory, "driver 'version")
			if output, err := exec.Command("sh", "-c", DriverVersionCommand(helper)).CombinedOutput(); err != nil {
				t.Fatalf("install helper: %v: %s", err, output)
			}
			binary := filepath.Join(directory, "native 'binary")
			// Mode 0600 proves that dependency discovery does not execute agy.
			if err := os.WriteFile(binary, []byte(test.data), 0600); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(helper, binary).CombinedOutput()
			if test.version == "" {
				if err == nil || !strings.Contains(string(output), "Cannot identify one Playwright driver version") {
					t.Fatalf("unresolved dependency accepted: %v: %s", err, output)
				}
				return
			}
			if err != nil || string(output) != test.version+"\n" {
				t.Fatalf("dependency = %q, %v; want %s", output, err, test.version)
			}
		})
	}
}
