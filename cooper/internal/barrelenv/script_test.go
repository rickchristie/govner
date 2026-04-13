package barrelenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestRenderUserEnvFileSimpleExport(t *testing.T) {
	data, err := RenderUserEnvFile([]config.BarrelEnvVar{{Name: "FOO", Value: "bar"}})
	if err != nil {
		t.Fatalf("RenderUserEnvFile() failed: %v", err)
	}
	if !strings.Contains(string(data), "export FOO='bar'") {
		t.Fatalf("unexpected render output: %q", string(data))
	}
}

func TestRenderUserEnvFileEmptyValue(t *testing.T) {
	data, err := RenderUserEnvFile([]config.BarrelEnvVar{{Name: "EMPTY", Value: ""}})
	if err != nil {
		t.Fatalf("RenderUserEnvFile() failed: %v", err)
	}
	if !strings.Contains(string(data), "export EMPTY=''") {
		t.Fatalf("unexpected render output: %q", string(data))
	}
}

func TestRenderUserEnvFileEscapesSingleQuotes(t *testing.T) {
	data, err := RenderUserEnvFile([]config.BarrelEnvVar{{Name: "KEY", Value: "it's fine"}})
	if err != nil {
		t.Fatalf("RenderUserEnvFile() failed: %v", err)
	}
	if !strings.Contains(string(data), `export KEY='it'"'"'s fine'`) {
		t.Fatalf("unexpected render output: %q", string(data))
	}
}

func TestRenderUserEnvFilePreservesLiteralCharacters(t *testing.T) {
	value := ` a $HOME path\with\slashes and=x `
	data, err := RenderUserEnvFile([]config.BarrelEnvVar{{Name: "KEY", Value: value}})
	if err != nil {
		t.Fatalf("RenderUserEnvFile() failed: %v", err)
	}
	want := `export KEY=' a $HOME path\with\slashes and=x '`
	if !strings.Contains(string(data), want) {
		t.Fatalf("render output = %q, want substring %q", string(data), want)
	}
}

func TestProtectedRuntimeEnvNamesDedupesAndPreservesStableOrder(t *testing.T) {
	first := ProtectedRuntimeEnvNames([]string{"OPENAI_API_KEY", "TERM", "OPENAI_API_KEY"})
	second := ProtectedRuntimeEnvNames([]string{"OPENAI_API_KEY", "TERM", "OPENAI_API_KEY"})
	if strings.Join(first, "\n") != strings.Join(second, "\n") {
		t.Fatalf("ProtectedRuntimeEnvNames() order is not stable\nfirst=%v\nsecond=%v", first, second)
	}
	joined := strings.Join(first, "\n")
	if !strings.Contains(joined, "HTTP_PROXY") || !strings.Contains(joined, "OPENAI_API_KEY") || !strings.Contains(joined, "TERM") {
		t.Fatalf("ProtectedRuntimeEnvNames() = %v, want static names plus extras", first)
	}
	if strings.Count(joined, "OPENAI_API_KEY") != 1 || strings.Count(joined, "TERM") != 1 {
		t.Fatalf("ProtectedRuntimeEnvNames() should de-duplicate extras, got %v", first)
	}
}

func TestBuildExecWrapperCommandInteractiveShape(t *testing.T) {
	argv, err := BuildExecWrapperCommand("/tmp/cooper-cli-env-demo.sh", []string{"HTTP_PROXY", "OPENAI_API_KEY"}, []string{"bash", "-l"})
	if err != nil {
		t.Fatalf("BuildExecWrapperCommand() failed: %v", err)
	}
	if len(argv) < 7 {
		t.Fatalf("argv too short: %v", argv)
	}
	if argv[0] != "bash" || argv[1] != "-c" {
		t.Fatalf("argv prefix = %v, want bash -c", argv[:2])
	}
	if argv[3] != "cooper-env-wrapper" {
		t.Fatalf("argv[3] = %q, want %q", argv[3], "cooper-env-wrapper")
	}
	if argv[4] != "/tmp/cooper-cli-env-demo.sh" {
		t.Fatalf("argv[4] = %q, want env file path", argv[4])
	}
	if argv[len(argv)-2] != "bash" || argv[len(argv)-1] != "-l" {
		t.Fatalf("tail argv = %v, want [bash -l]", argv[len(argv)-2:])
	}
}

func TestBuildExecWrapperCommandOneShotKeepsCommandAsSeparateArgv(t *testing.T) {
	oneShot := `printf "%s" "$FOO"`
	argv, err := BuildExecWrapperCommand("/tmp/cooper-cli-env-demo.sh", []string{"HTTP_PROXY"}, []string{"bash", "-c", oneShot})
	if err != nil {
		t.Fatalf("BuildExecWrapperCommand() failed: %v", err)
	}
	if got := argv[len(argv)-3:]; strings.Join(got, "\n") != strings.Join([]string{"bash", "-c", oneShot}, "\n") {
		t.Fatalf("tail argv = %v, want [bash -c %q]", got, oneShot)
	}
	if strings.Contains(argv[2], oneShot) {
		t.Fatalf("wrapper script should not contain one-shot command text: %q", argv[2])
	}
}

func TestBuildExecWrapperCommandPreservesProtectedSetVsUnset(t *testing.T) {
	envFile := filepathForTest(t, "barrel-env.sh")
	if err := os.WriteFile(envFile, []byte("export HTTP_PROXY='http://bad:1'\nexport FOO='ok'\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	argv, err := BuildExecWrapperCommand(envFile, []string{"HTTP_PROXY"}, []string{"bash", "-c", `printf '%s|%s' "${HTTP_PROXY-}" "${FOO-}"`})
	if err != nil {
		t.Fatalf("BuildExecWrapperCommand() failed: %v", err)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(filterEnv(os.Environ(), "HTTP_PROXY"), "HTTP_PROXY=http://good:2")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wrapper execution failed: %v\n%s", err, string(out))
	}
	if got := string(out); got != "http://good:2|ok" {
		t.Fatalf("output = %q, want %q", got, "http://good:2|ok")
	}

	cmd = exec.Command(argv[0], argv[1:]...)
	cmd.Env = filterEnv(os.Environ(), "HTTP_PROXY")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wrapper execution with unset proxy failed: %v\n%s", err, string(out))
	}
	if got := string(out); got != "|ok" {
		t.Fatalf("output = %q, want %q", got, "|ok")
	}
}

func filepathForTest(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func filterEnv(env []string, name string) []string {
	prefix := name + "="
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
