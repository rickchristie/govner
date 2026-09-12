package templates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestCustomOnlyImageUsesRuntimeClipboardMode(t *testing.T) {
	script, err := RenderEntrypoint(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	const guard = `if [ "${COOPER_CLIPBOARD_ENABLED:-0}" = 1 ]; then`
	start := strings.Index(script, guard)
	end := strings.Index(script, "# WELCOME BANNER")
	if start < 0 || end <= start {
		t.Fatal("base entrypoint has no runtime clipboard guard; custom-only images lose clipboard support")
	}
	for _, test := range []struct {
		name, enabled, mode string
		want                bool
	}{
		{"custom-shim", "1", "shim", true},
		{"disabled", "0", "shim", false},
		{"off", "1", "off", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "shims")
			target := filepath.Join(root, "bin")
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			const contents = "#!/bin/sh\nprintf clipboard-fixture\n"
			if err := os.WriteFile(filepath.Join(source, "xclip"), []byte(contents), 0700); err != nil {
				t.Fatal(err)
			}
			block := strings.ReplaceAll(script[start:end], "/etc/cooper/shims", source)
			block = strings.ReplaceAll(block, "/opt/cooper/bin", target)
			command := exec.Command("bash", "-c", block)
			command.Env = append(os.Environ(), "COOPER_CLIPBOARD_ENABLED="+test.enabled, "COOPER_CLIPBOARD_MODE="+test.mode)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("clipboard setup: %v\n%s", err, output)
			}
			data, err := os.ReadFile(filepath.Join(target, "xclip"))
			if test.want {
				if err != nil || string(data) != contents {
					t.Fatalf("custom image shim: %q %v", data, err)
				}
				return
			}
			if !os.IsNotExist(err) {
				t.Fatal("disabled runtime installed a shim")
			}
		})
	}
}
