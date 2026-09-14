package antigravity

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type hostFixture struct {
	home, native, bin string
	setup             HostSetup
}

func newHostFixture(t *testing.T) hostFixture {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the host wrapper selects the Linux Secret Service fallback")
	}
	home := filepath.Join(t.TempDir(), "home with 'quotes' and $values")
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(bin, "agy")
	writeTestFile(t, native, "#!/bin/sh\nprintf '%s\\n' \"$DBUS_SESSION_BUS_ADDRESS\" \"$@\"\nexit 23\n", 0o700)
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/cooper-fake-desktop-bus")
	return hostFixture{home: home, native: native, bin: bin}
}

func writeTestFile(t *testing.T, path, value string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), mode); err != nil {
		t.Fatal(err)
	}
}

func (f *hostFixture) install(t *testing.T) {
	t.Helper()
	setup, err := InstallHostFileAuth(t.Context(), f.home, "")
	if err != nil {
		t.Fatal(err)
	}
	f.setup = setup
}

func TestHostWrapperPreservesArgumentsExitAndDesktopBus(t *testing.T) {
	f := newHostFixture(t)
	f.install(t)
	output, err := exec.Command(f.setup.Wrapper, "argument with spaces", "", "'literal'", "$(touch should-not-run)").CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 23 {
		t.Fatalf("native exit was not preserved: %v, %s", err, output)
	}
	want := FileBusAddress + "\nargument with spaces\n\n'literal'\n$(touch should-not-run)\n"
	if string(output) != want {
		t.Fatalf("native inputs changed: %q", output)
	}
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "unix:path=/cooper-fake-desktop-bus" {
		t.Fatal("wrapper changed the parent desktop bus")
	}
	if HostFileAuthActive(f.home) {
		t.Fatal("installation alone claimed that the parent PATH selected the wrapper")
	}
	t.Setenv("PATH", filepath.Dir(f.setup.Wrapper)+":"+os.Getenv("PATH"))
	if !HostFileAuthActive(f.home) {
		t.Fatal("installed wrapper was not recognized")
	}
}

func TestHostSetupPreservesShellFilesAndIsRepeatable(t *testing.T) {
	f := newHostFixture(t)
	original := "# User settings must stay intact.\nexport USER_SETTING=kept\n"
	dotfile := filepath.Join(f.home, "bash-settings")
	writeTestFile(t, dotfile, original, 0o640)
	if err := os.Symlink(dotfile, filepath.Join(f.home, ".bashrc")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(f.home, ".bash_profile"), original, 0o600)
	f.install(t)
	t.Setenv("PATH", filepath.Dir(f.setup.Wrapper)+":"+os.Getenv("PATH"))
	f.install(t)
	for _, path := range []string{dotfile, filepath.Join(f.home, ".bash_profile"), filepath.Join(f.home, ".zshrc")} {
		data, err := os.ReadFile(path)
		if err != nil || strings.Count(string(data), shellBlockStart) != 1 {
			t.Fatalf("shell setup duplicated or missing: %s: %v", path, err)
		}
		if path != filepath.Join(f.home, ".zshrc") && !strings.HasPrefix(string(data), original) {
			t.Fatal("existing shell settings changed")
		}
	}
	info, err := os.Lstat(filepath.Join(f.home, ".bashrc"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("shell configuration symlink changed")
	}
	info, err = os.Stat(dotfile)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatal("shell configuration permissions changed")
	}
	if _, err := os.Stat(filepath.Join(f.home, ".profile")); !os.IsNotExist(err) {
		t.Fatal("setup ignored the existing Bash login profile")
	}
}

func TestNewShellSelectsWrapperAndKeepsOtherCommands(t *testing.T) {
	f := newHostFixture(t)
	writeTestFile(t, filepath.Join(f.home, ".bashrc"), "agy() { false; }\nalias agy='false'\n", 0o600)
	f.install(t)
	command := exec.Command("bash", "--noprofile", "--rcfile", filepath.Join(f.home, ".bashrc"), "-ic", "command -v agy; printf '%s\\n' \"$DBUS_SESSION_BUS_ADDRESS\"; agy 'shell argument'")
	output, err := command.Output()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 23 {
		t.Fatalf("new shell did not use the wrapper: %v, %s", err, output)
	}
	want := f.setup.Wrapper + "\nunix:path=/cooper-fake-desktop-bus\n" + FileBusAddress + "\nshell argument\n"
	if string(output) != want {
		t.Fatalf("new shell environment: %q", output)
	}
}

func TestHostWrapperSurvivesNativeReplacementAndDetectsDamage(t *testing.T) {
	f := newHostFixture(t)
	f.install(t)
	t.Setenv("PATH", filepath.Dir(f.setup.Wrapper)+":"+os.Getenv("PATH"))
	writeTestFile(t, f.native, "#!/bin/sh\nprintf '%s\\n' updated\n", 0o700)
	output, err := exec.Command("agy").Output()
	if err != nil || string(output) != "updated\n" || !HostFileAuthActive(f.home) {
		t.Fatalf("native replacement broke the wrapper: %v, %q", err, output)
	}
	writeTestFile(t, f.setup.Wrapper, "#!/bin/sh\n# User change.\nexit 0\n", 0o700)
	if HostFileAuthActive(f.home) {
		t.Fatal("changed wrapper authorized file credentials")
	}
	if _, err := InstallHostFileAuth(t.Context(), f.home, ""); err == nil {
		t.Fatal("setup replaced a changed wrapper")
	}
	data, err := os.ReadFile(f.setup.Wrapper)
	if err != nil || !bytes.Contains(data, []byte("User change")) {
		t.Fatal("changed wrapper was not preserved")
	}
}

func TestHostSetupRejectsExecutableInStateAndPreservesUnownedFiles(t *testing.T) {
	f := newHostFixture(t)
	stateBin := filepath.Join(f.home, ".gemini", "bin")
	if err := os.MkdirAll(stateBin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.native, filepath.Join(stateBin, "agy")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stateBin+":"+os.Getenv("PATH"))
	if _, err := InstallHostFileAuth(t.Context(), f.home, ""); err == nil {
		t.Fatal("profile replacement could remove the native executable path")
	}
	t.Setenv("PATH", f.bin+":/usr/bin:/bin")
	setup := HostPaths(f.home)
	if err := os.MkdirAll(filepath.Dir(setup.Wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, setup.Wrapper, "user content", 0o600)
	if _, err := InstallHostFileAuth(t.Context(), f.home, ""); err == nil {
		t.Fatal("setup overwrote an unowned file")
	}
	data, err := os.ReadFile(setup.Wrapper)
	if err != nil || string(data) != "user content" {
		t.Fatal("unowned file was changed")
	}
}

func TestImageWrapperCommandUsesTheSameScript(t *testing.T) {
	f := newHostFixture(t)
	target := filepath.Join(f.home, "image agy")
	command := FileWrapperCommand(f.native, target)
	if strings.Contains(command, "\n") {
		t.Fatal("image RUN command contains an unescaped line break")
	}
	if output, err := exec.Command("sh", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("write image wrapper: %v, %s", err, output)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != FileWrapper(f.native) {
		t.Fatal("host and image wrapper policies differ")
	}
}

func TestHostSetupRejectsStateOverlapThroughParentSymlink(t *testing.T) {
	f := newHostFixture(t)
	state := filepath.Join(f.home, ".gemini")
	parent := filepath.Join(f.home, ".local", "share")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(state, filepath.Join(parent, "cooper")); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallHostFileAuth(t.Context(), f.home, ""); err == nil {
		t.Fatal("profile replacement could remove host shell setup")
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected setup changed the agent state root")
	}
}

func TestHostSetupUsesCustomZshDirectory(t *testing.T) {
	f := newHostFixture(t)
	zshDir := filepath.Join(f.home, ".config", "zsh")
	if _, err := InstallHostFileAuth(t.Context(), f.home, zshDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(zshDir, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatal("custom ZDOTDIR was ignored")
	}
}
