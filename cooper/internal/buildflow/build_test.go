package buildflow

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
)

func TestStepNamesSplitPreparationFromDockerBuilds(t *testing.T) {
	cooperDir := t.TempDir()
	customDir := filepath.Join(cooperDir, "cli", "custom-tool")
	if err := ensureTestDockerfile(customDir); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.AITools = []config.ToolConfig{{Name: "claude", Enabled: true}}

	all, err := StepNames(cfg, cooperDir)
	if err != nil {
		t.Fatalf("StepNames() failed: %v", err)
	}
	preparation := PreparationStepNames()
	if !reflect.DeepEqual(all[:len(preparation)], preparation) {
		t.Fatalf("preparation prefix = %v, want %v", all[:len(preparation)], preparation)
	}
	if got := preparation[len(preparation)-1]; got != "Staging CA files..." {
		t.Fatalf("last preparation step = %q", got)
	}
	for _, step := range preparation {
		if strings.HasPrefix(step, "Building ") {
			t.Fatalf("preparation unexpectedly contains Docker step %q", step)
		}
	}
	wantBuilds := []string{
		"Building proxy image...",
		"Building base image...",
		"Building claude image...",
		"Building custom image custom-tool...",
	}
	if got := all[len(preparation):]; !reflect.DeepEqual(got, wantBuilds) {
		t.Fatalf("Docker steps = %v, want %v", got, wantBuilds)
	}
}

func TestPreparedBuildStreamsOutputAndReportsEachImage(t *testing.T) {
	cooperDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.AITools = []config.ToolConfig{{Name: "codex", Enabled: true}}

	prepared := &Prepared{
		cfg:        cfg,
		cooperDir:  cooperDir,
		plan:       plan{enabledAITools: []string{"codex"}},
		baseDir:    filepath.Join(cooperDir, "base"),
		cliDir:     filepath.Join(cooperDir, "cli"),
		proxyDir:   filepath.Join(cooperDir, "proxy"),
		configPath: filepath.Join(cooperDir, "config.json"),
	}

	var calls []string
	fakeBuild := func(name, dockerfilePath, contextDir string, buildArgs map[string]string, noCache bool) (<-chan string, <-chan error) {
		calls = append(calls, name)
		lines := make(chan string, 2)
		errs := make(chan error, 1)
		lines <- name + " stdout"
		lines <- name + " stderr"
		close(lines)
		errs <- nil
		close(errs)
		return lines, errs
	}

	var out bytes.Buffer
	var streamed []string
	var completed []int
	err := prepared.Build(Options{
		NoCache:    true,
		Out:        &out,
		OnOutput:   func(line string) { streamed = append(streamed, line) },
		imageBuild: fakeBuild,
		OnProgress: func(step int, total int, name string, stepErr error) {
			if stepErr != nil {
				t.Fatalf("step %d failed: %v", step, stepErr)
			}
			if total != 3 {
				t.Fatalf("progress total = %d, want 3", total)
			}
			completed = append(completed, step)
		},
	})
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	wantCalls := []string{docker.GetImageProxy(), docker.GetImageBase(), docker.GetImageCLI("codex")}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("image calls = %v, want %v", calls, wantCalls)
	}
	if !reflect.DeepEqual(completed, []int{0, 1, 2}) {
		t.Fatalf("completed steps = %v, want [0 1 2]", completed)
	}
	for _, want := range []string{
		"Building proxy image...",
		docker.GetImageProxy() + " stdout",
		docker.GetImageProxy() + " stderr",
		"Building base image...",
		"Building codex image...",
		"Build complete.",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("writer output missing %q:\n%s", want, out.String())
		}
		if !containsLine(streamed, want) {
			t.Errorf("streamed output missing %q: %v", want, streamed)
		}
	}
}

func TestRunPreservesCombinedCLIFlowAcrossPhaseBoundary(t *testing.T) {
	cooperDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.ProgrammingTools = nil
	cfg.AITools = nil

	fakeBuild := func(name, dockerfilePath, contextDir string, buildArgs map[string]string, noCache bool) (<-chan string, <-chan error) {
		lines := make(chan string, 1)
		errs := make(chan error, 1)
		lines <- name + " output"
		close(lines)
		errs <- nil
		close(errs)
		return lines, errs
	}

	var progress []int
	err := Run(cfg, cooperDir, Options{
		imageBuild: fakeBuild,
		OnProgress: func(step int, total int, name string, stepErr error) {
			if stepErr != nil {
				t.Fatalf("step %d failed: %v", step, stepErr)
			}
			if total != 7 {
				t.Fatalf("combined progress total = %d, want 7", total)
			}
			progress = append(progress, step)
		},
	})
	if err != nil {
		t.Fatalf("Run() failed: %v", err)
	}
	if !reflect.DeepEqual(progress, []int{0, 1, 2, 3, 4, 5, 6}) {
		t.Fatalf("combined progress = %v, want [0 1 2 3 4 5 6]", progress)
	}
}

func TestStageFinishesSavedInputsWithoutRepeatingPreparation(t *testing.T) {
	cooperDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.ProgrammingTools = nil
	cfg.AITools = nil
	if _, _, err := config.EnsureCA(cooperDir); err != nil {
		t.Fatalf("EnsureCA() failed: %v", err)
	}

	var progress []int
	prepared, err := Stage(cfg, cooperDir, nil, Options{
		OnProgress: func(step int, total int, name string, stepErr error) {
			if stepErr != nil {
				t.Fatalf("stage %d failed: %v", step, stepErr)
			}
			if total != 2 {
				t.Fatalf("staging total = %d, want 2", total)
			}
			progress = append(progress, step)
		},
	})
	if err != nil {
		t.Fatalf("Stage() failed: %v", err)
	}
	if !reflect.DeepEqual(progress, []int{0, 1}) {
		t.Fatalf("staging progress = %v, want [0 1]", progress)
	}
	for _, path := range []string{
		filepath.Join(cooperDir, "proxy", "acl-helper", "go.mod"),
		filepath.Join(cooperDir, "proxy", "acl-helper", "cmd", "acl-helper", "main.go"),
		filepath.Join(cooperDir, "proxy", "cooper-ca.pem"),
		filepath.Join(cooperDir, "proxy", "cooper-ca-key.pem"),
		filepath.Join(cooperDir, "base", "cooper-ca.pem"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("staged file %s missing: %v", path, err)
		}
	}
	if got := prepared.StepNames(); !reflect.DeepEqual(got, []string{"Building proxy image...", "Building base image..."}) {
		t.Fatalf("prepared Docker steps = %v", got)
	}
}

func ensureTestDockerfile(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0644)
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}
