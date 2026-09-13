package workload

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAntigravityStateAndADCSelection(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	for _, test := range []struct {
		name string
		env  map[string]string
		adc  string
	}{
		{"consumer", nil, ""},
		{"unused credentials", map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": "private/key.json"}, ""},
		{"default ADC", map[string]string{"AGY_ADC_AUTH": "true", "CLOUDSDK_CONFIG": "/ignored"}, filepath.Join(home, ".config/gcloud")},
		{"numeric ADC", map[string]string{"AGY_ADC_AUTH": "1"}, filepath.Join(home, ".config/gcloud")},
		{"relative ADC", map[string]string{"AGY_ADC_AUTH": "true", "GOOGLE_APPLICATION_CREDENTIALS": "private/key.json"}, filepath.Join(workspace, "private")},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths, err := ResolveAgentPaths("antigravity", home, workspace, test.env)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{filepath.Join(home, ".gemini")}
			if test.adc != "" {
				want = append(want, test.adc)
			}
			var got []string
			for _, mount := range paths.Mounts {
				got = append(got, mount.Source)
				if mount.Source != mount.Target || mount.Access != ReadWrite {
					t.Fatalf("mount changed host path: %#v", mount)
				}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("roots = %v, want %v", got, want)
			}
			if test.name == "relative ADC" && !slices.Contains(RenderEnvironment(paths.Environment), "GOOGLE_APPLICATION_CREDENTIALS="+filepath.Join(workspace, "private/key.json")) {
				t.Fatal("relative credential path will change after the shell changes directory")
			}
		})
	}
}

func TestAntigravityADCRejectsHomeAndProtectsCleanup(t *testing.T) {
	home := t.TempDir()
	for _, credential := range []string{filepath.Join(home, "key.json"), filepath.Join(home, "alias/key.json")} {
		if err := os.Symlink(home, filepath.Join(home, "alias")); err != nil && !os.IsExist(err) {
			t.Fatal(err)
		}
		if _, err := ResolveAgentPaths("antigravity", home, t.TempDir(), map[string]string{"AGY_ADC_AUTH": "true", "GOOGLE_APPLICATION_CREDENTIALS": credential}); err == nil {
			t.Fatal("ADC parent exposed the complete home")
		}
	}
	t.Setenv("AGY_ADC_AUTH", "false")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "keys/credential.json"))
	for _, root := range []string{filepath.Join(home, ".gemini"), filepath.Join(home, ".config/gcloud"), filepath.Dir(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))} {
		if err := ValidateAllHostAgentStateRoots(home, filepath.Join(root, "cooper")); err == nil {
			t.Fatalf("cleanup accepted host state %s", root)
		}
	}
}
