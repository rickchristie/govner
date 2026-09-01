package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestAppendVolumeMountsGrokSharesCompleteHostState(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	homeDir := t.TempDir()
	cooperDir := t.TempDir()
	absWorkspace := filepath.Join(t.TempDir(), "ws")
	containerName := "barrel-ws-grok"

	got := appendVolumeMounts(nil, absWorkspace, homeDir, &config.Config{}, cooperDir, "grok", containerName)
	joined := strings.Join(got, " ")

	hostState := filepath.Join(homeDir, ".grok")
	wantState := hostState + ":" + BarrelGrokStateRoot + ":rw"
	if !containsArg(got, wantState) {
		t.Fatalf("missing complete Grok state mount %q\n%s", wantState, joined)
	}
	if strings.Count(joined, BarrelGrokStateRoot) != 1 {
		t.Fatalf("Grok state must use one mount: %s", joined)
	}
	for _, legacy := range []string{".cooper-grok-auth", filepath.Join(cooperDir, "secrets", "grok"), filepath.Join(cooperDir, "state", "grok")} {
		if strings.Contains(joined, legacy) {
			t.Fatalf("legacy split Grok state is still mounted: %s", joined)
		}
	}
}

func TestGrokHostStateRootHonorsOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "shared-grok")
	t.Setenv("GROK_HOME", override)
	if got := GrokHostStateRoot(t.TempDir()); got != override {
		t.Fatalf("GrokHostStateRoot() = %q, want %q", got, override)
	}
}

func TestValidateGrokHostStateRootRejectsCooperOwnedPath(t *testing.T) {
	cooperDir := t.TempDir()
	stateRoot := filepath.Join(cooperDir, "tmp", "grok")
	t.Setenv("GROK_HOME", stateRoot)

	err := ValidateGrokHostStateRoot(t.TempDir(), cooperDir)
	if err == nil || !strings.Contains(err.Error(), "overlaps Cooper-owned directory") {
		t.Fatalf("ValidateGrokHostStateRoot() error = %v, want overlap error", err)
	}
}

func TestValidateGrokHostStateRootRejectsSymlinkedParent(t *testing.T) {
	cooperDir := t.TempDir()
	ownedTarget := filepath.Join(cooperDir, "tmp")
	if err := os.MkdirAll(ownedTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "grok-link")
	if err := os.Symlink(ownedTarget, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GROK_HOME", filepath.Join(link, "new-state"))

	err := ValidateGrokHostStateRoot(t.TempDir(), cooperDir)
	if err == nil || !strings.Contains(err.Error(), "overlaps Cooper-owned directory") {
		t.Fatalf("ValidateGrokHostStateRoot() error = %v, want symlink overlap error", err)
	}
}

func TestValidateGrokHostStateRootRejectsSymlinkInsideCooperDir(t *testing.T) {
	cooperDir := t.TempDir()
	outsideState := t.TempDir()
	stateLink := filepath.Join(cooperDir, "grok-link")
	if err := os.Symlink(outsideState, stateLink); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GROK_HOME", stateLink)

	err := ValidateGrokHostStateRoot(t.TempDir(), cooperDir)
	if err == nil || !strings.Contains(err.Error(), "overlaps Cooper-owned directory") {
		t.Fatalf("ValidateGrokHostStateRoot() error = %v, want direct overlap error", err)
	}
}

func TestValidateGrokHostStateRootRejectsStateParentOfCooperDir(t *testing.T) {
	stateRoot := t.TempDir()
	cooperDir := filepath.Join(stateRoot, "cooper")
	t.Setenv("GROK_HOME", stateRoot)

	err := ValidateGrokHostStateRoot(t.TempDir(), cooperDir)
	if err == nil || !strings.Contains(err.Error(), "overlaps Cooper-owned directory") {
		t.Fatalf("ValidateGrokHostStateRoot() error = %v, want parent overlap error", err)
	}
}

func TestValidateGrokHostStateRootAllowsSiblingPath(t *testing.T) {
	parent := t.TempDir()
	cooperDir := filepath.Join(parent, ".cooper")
	t.Setenv("GROK_HOME", filepath.Join(parent, ".cooper-grok"))

	if err := ValidateGrokHostStateRoot(t.TempDir(), cooperDir); err != nil {
		t.Fatalf("ValidateGrokHostStateRoot() error = %v, want nil", err)
	}
}

func TestSameHostPathResolvesSymlinks(t *testing.T) {
	realDir := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	if !sameHostPath(realDir, link) {
		t.Fatalf("sameHostPath(%q, %q) = false", realDir, link)
	}
	if sameHostPath(realDir, filepath.Join(t.TempDir(), "other")) {
		t.Fatal("different paths matched")
	}
}

func TestHasGrokStateMountRequiresOneCompleteReadWriteRoot(t *testing.T) {
	hostRoot := filepath.Join(t.TempDir(), ".grok")
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "complete root",
			output: hostRoot + "\t/home/user/.grok\ttrue\n",
			want:   true,
		},
		{
			name:   "read only root",
			output: hostRoot + "\t/home/user/.grok\tfalse\n",
		},
		{
			name:   "different source",
			output: filepath.Join(t.TempDir(), ".grok") + "\t/home/user/.grok\ttrue\n",
		},
		{
			name:   "legacy split mounts",
			output: hostRoot + "/auth.json\t/home/user/.grok/auth.json\ttrue\n" + hostRoot + "/sessions\t/home/user/.grok/sessions\ttrue\n",
		},
		{
			name:   "root plus split mount",
			output: hostRoot + "\t/home/user/.grok\ttrue\n" + hostRoot + "/sessions\t/home/user/.grok/sessions\ttrue\n",
		},
		{
			name:   "similar destination",
			output: hostRoot + "\t/home/user/.grok-other\ttrue\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasGrokStateMount(tc.output, hostRoot); got != tc.want {
				t.Fatalf("hasGrokStateMount() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestBarrelMountDirsGrokCreatesCompleteHostRoot(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	homeDir := t.TempDir()
	cooperDir := t.TempDir()
	dirs := barrelMountDirs(homeDir, "grok", cooperDir, "barrel-ws-grok", &config.Config{})
	want := filepath.Join(homeDir, ".grok")
	count := 0
	for _, dir := range dirs {
		if dir == want {
			count++
		}
		if strings.Contains(dir, filepath.Join(cooperDir, "state", "grok")) || strings.Contains(dir, filepath.Join(cooperDir, "secrets", "grok")) {
			t.Fatalf("found legacy Cooper-owned Grok path: %s", dir)
		}
	}
	if count != 1 {
		t.Fatalf("complete host Grok root count = %d, dirs=%v", count, dirs)
	}
}

func TestEnsureBarrelMountDirsCreatesPrivateGrokRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared-grok")
	t.Setenv("GROK_HOME", root)
	if err := ensureBarrelMountDirs("grok", t.TempDir(), "barrel-ws-grok", &config.Config{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("new Grok state root mode = %o, want 0700", info.Mode().Perm())
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
