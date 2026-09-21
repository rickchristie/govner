package buildflow

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestBuildNeverSetsUpOrConvertsProfiles(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "old-format"}[existing], func(t *testing.T) {
			cooperDir := t.TempDir()
			path := filepath.Join(cooperDir, "profiles", "index.json")
			if existing {
				if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("untouched old profile data"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			prepared := &Prepared{cfg: config.DefaultConfig(), cooperDir: cooperDir, configPath: filepath.Join(cooperDir, "config.json"), baseDir: filepath.Join(cooperDir, "base"), proxyDir: filepath.Join(cooperDir, "proxy"), cliDir: filepath.Join(cooperDir, "cli")}
			calls := 0
			var output bytes.Buffer
			for range 2 {
				err := prepared.Build(Options{Out: &output, imageBuild: func(string, string, string, map[string]string, bool) (<-chan string, <-chan error) {
					calls++
					lines, result := make(chan string), make(chan error, 1)
					close(lines)
					result <- nil
					close(result)
					return lines, result
				}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if calls != 4 || strings.Contains(output.String(), "Setting up live profiles") {
				t.Fatal("build retained profile conversion")
			}
			data, err := os.ReadFile(path)
			if existing && (err != nil || string(data) != "untouched old profile data") {
				t.Fatal("build changed old profile state")
			}
			if !existing && !os.IsNotExist(err) {
				t.Fatal("build created a profile store")
			}
		})
	}
}
