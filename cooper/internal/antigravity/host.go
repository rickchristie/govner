package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/rickchristie/govner/cooper/internal/hostpath"
	"github.com/rickchristie/govner/cooper/internal/statelock"
)

// FileBusAddress selects the native Linux file fallback without exposing the
// desktop bus. Apply it only to agy and its children, never to the host shell.
const FileBusAddress = "unix:path=/dev/null"

const shellBlockStart = "# >>> Cooper Antigravity file authentication >>>"
const shellBlockEnd = "# <<< Cooper Antigravity file authentication <<<"

type hostRecord struct {
	Schema     int    `json:"schema"`
	Executable string `json:"executable"`
}

type HostSetup struct {
	Wrapper string
	Shell   string
}

// HostPaths are independent of runtime configuration and account profiles.
// Cleanup of a barrel or a Cooper config must not change the host login mode.
func HostPaths(home string) HostSetup {
	dir := filepath.Join(home, ".local", "share", "cooper", "antigravity")
	return HostSetup{Wrapper: filepath.Join(dir, "bin", "agy"), Shell: filepath.Join(dir, "shell.sh")}
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// FileWrapper leaves the native executable in its installation directory so
// its updater can replace it without replacing Cooper's storage selection.
func FileWrapper(executable string) string {
	return "#!/bin/sh\n# Cooper Antigravity file authentication v1.\n" +
		"export DBUS_SESSION_BUS_ADDRESS=" + FileBusAddress + "\n" +
		"exec " + shellQuote(executable) + " \"$@\"\n"
}

// FileWrapperCommand writes the same wrapper into the selected agent image.
func FileWrapperCommand(executable, target string) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSuffix(FileWrapper(executable), "\n"), "\n") {
		lines = append(lines, shellQuote(line))
	}
	return "printf '%s\\n' " + strings.Join(lines, " ") + " > " + shellQuote(target) +
		" && chmod 0755 " + shellQuote(target)
}

// InstallHostFileAuth only changes Cooper-owned files and appends a marked
// block to shell startup files. It never reads, copies, or deletes credentials.
// zshDir is optional; an empty value selects the host home.
func InstallHostFileAuth(ctx context.Context, home, zshDir string) (HostSetup, error) {
	if runtime.GOOS != "linux" {
		return HostSetup{}, errors.New("Antigravity file authentication setup requires Linux")
	}
	if !filepath.IsAbs(home) || filepath.Clean(home) != home || home == "/" || strings.ContainsAny(home, "\x00\r\n:") {
		return HostSetup{}, errors.New("Antigravity setup requires a clean absolute host home")
	}
	if zshDir == "" {
		zshDir = home
	}
	if !filepath.IsAbs(zshDir) || filepath.Clean(zshDir) != zshDir {
		return HostSetup{}, errors.New("ZDOTDIR must be a clean absolute path for Antigravity shell setup")
	}
	lock, err := statelock.Acquire(ctx, true)
	if err != nil {
		return HostSetup{}, err
	}
	defer lock.Close()
	setup := HostPaths(home)
	if err := checkHostSetupPath(home); err != nil {
		return HostSetup{}, err
	}
	recordPath := filepath.Join(filepath.Dir(setup.Shell), "host.json")
	previous, err := readHostRecord(recordPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return HostSetup{}, err
	}
	executable, err := exec.LookPath("agy")
	if err != nil && previous.Executable == "" {
		return HostSetup{}, fmt.Errorf("install native agy on the host, then run cooper build: %w", err)
	}
	if err != nil || executable == setup.Wrapper {
		executable = previous.Executable
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return HostSetup{}, err
	}
	if err := checkHostExecutable(home, executable); err != nil {
		return HostSetup{}, err
	}
	record := hostRecord{Schema: 1, Executable: executable}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return HostSetup{}, err
	}
	var oldWrapper, oldRecord []byte
	if previous.Executable != "" {
		oldWrapper = []byte(FileWrapper(previous.Executable))
		oldRecord, _ = json.MarshalIndent(previous, "", "  ")
	}
	shell := hostShell(setup)
	for _, file := range []struct {
		path      string
		data, old []byte
		mode      os.FileMode
	}{
		{setup.Wrapper, []byte(FileWrapper(executable)), oldWrapper, 0o700},
		{setup.Shell, []byte(shell), []byte(shell), 0o600},
		{recordPath, data, oldRecord, 0o600},
	} {
		if err := writeHostFile(file.path, file.data, file.old, file.mode); err != nil {
			return HostSetup{}, err
		}
	}
	if err := os.MkdirAll(zshDir, 0o700); err != nil {
		return HostSetup{}, err
	}
	login := filepath.Join(home, ".profile")
	for _, name := range []string{".bash_profile", ".bash_login", ".profile"} {
		path := filepath.Join(home, name)
		if _, err := os.Stat(path); err == nil {
			login = path
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return HostSetup{}, err
		}
	}
	for _, path := range []string{filepath.Join(home, ".bashrc"), login, filepath.Join(zshDir, ".zshrc")} {
		if err := appendShellSetup(path, setup.Shell); err != nil {
			return HostSetup{}, err
		}
	}
	return setup, nil
}

// HostFileAuthActive checks files on every observation. A changed wrapper or
// PATH must not make a desktop keyring account look like the saved file account.
// This check never executes agy or connects to a session bus.
func HostFileAuthActive(home string) bool {
	if runtime.GOOS != "linux" || checkHostSetupPath(home) != nil {
		return false
	}
	setup := HostPaths(home)
	executable, err := exec.LookPath("agy")
	if err != nil || executable != setup.Wrapper {
		return false
	}
	record, err := readHostRecord(filepath.Join(filepath.Dir(setup.Shell), "host.json"))
	if err != nil || checkHostExecutable(home, record.Executable) != nil {
		return false
	}
	data, err := readHostFile(setup.Wrapper)
	return err == nil && string(data) == FileWrapper(record.Executable)
}

func checkHostSetupPath(home string) error {
	setup, err := hostpath.Resolve(filepath.Dir(HostPaths(home).Shell))
	if err != nil {
		return err
	}
	state, err := hostpath.Resolve(filepath.Join(home, ".gemini"))
	if err != nil {
		return err
	}
	if within(state, setup) || within(setup, state) {
		return errors.New("Antigravity host setup and its profile state root must not overlap")
	}
	return nil
}

func readHostRecord(path string) (hostRecord, error) {
	data, err := readHostFile(path)
	if err != nil {
		return hostRecord{}, err
	}
	var record hostRecord
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil || record.Schema != 1 || record.Executable == "" {
		return hostRecord{}, errors.New("Antigravity host setup record is not supported; existing files were retained")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return hostRecord{}, errors.New("Antigravity host setup record has extra data")
	}
	return record, nil
}

func checkHostExecutable(home, executable string) error {
	if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable || strings.ContainsAny(executable, "\x00\r\n") {
		return errors.New("native agy requires a clean absolute executable path")
	}
	info, err := os.Stat(executable)
	if err != nil {
		return fmt.Errorf("read native agy executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("native agy path is not an executable file")
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	for _, root := range []string{filepath.Join(home, ".gemini"), filepath.Dir(HostPaths(home).Shell)} {
		for _, candidate := range []string{executable, resolved} {
			if within(root, candidate) {
				return errors.New("native agy must be outside its state root and Cooper's wrapper directory")
			}
		}
		if realRoot, err := filepath.EvalSymlinks(root); err == nil && within(realRoot, resolved) {
			return errors.New("native agy is inside a resolved state or wrapper directory")
		}
	}
	return nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func hostShell(setup HostSetup) string {
	dir := shellQuote(filepath.Dir(setup.Wrapper))
	return "# Cooper selects file authentication for the host agy command.\n" +
		"unalias agy 2>/dev/null || :\nunset -f agy 2>/dev/null || :\n" +
		"case ${PATH-} in\n  " + dir + "|" + dir + ":*) ;;\n" +
		"  *) PATH=" + dir + "${PATH:+:$PATH}; export PATH ;;\nesac\n" +
		"hash -r 2>/dev/null || :\n"
}

func readHostFile(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16384 {
		return nil, fmt.Errorf("unsupported Antigravity setup file %q; existing files were retained", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if len(data) > 16384 {
		return nil, errors.New("Antigravity setup file exceeds its size limit")
	}
	return data, err
}

func writeHostFile(path string, data, previous []byte, mode os.FileMode) error {
	existing, err := readHostFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && !bytes.Equal(existing, data) && (previous == nil || !bytes.Equal(existing, previous)) {
		return fmt.Errorf("Antigravity setup file %q was changed; existing files were retained", path)
	}
	if err == nil && bytes.Equal(existing, data) {
		return os.Chmod(path, mode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".agy-setup-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func appendShellSetup(path, shell string) error {
	block := shellBlockStart + "\nif [ -r " + shellQuote(shell) + " ]; then\n    . " + shellQuote(shell) + "\nfi\n" + shellBlockEnd + "\n"
	// Follow an existing shell-config symlink, but retain the link itself and
	// every existing byte. Dotfile managers commonly use these links.
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return fmt.Errorf("shell startup file %q is not supported; existing content was retained", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, 2<<20+1))
	if err != nil {
		return err
	}
	if strings.Contains(string(data), block) {
		return nil
	}
	if strings.Contains(string(data), shellBlockStart) || strings.Contains(string(data), shellBlockEnd) {
		return fmt.Errorf("Cooper's Antigravity block in %q was changed; existing content was retained", path)
	}
	_, err = file.WriteString("\n" + block)
	return err
}
