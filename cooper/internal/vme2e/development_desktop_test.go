package vme2e

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vm"
)

// desktop uses the real package, an empty home, and the prepared image. It
// does not sign in, read host credentials, or send a paid model request.
func (f *developmentFixture) desktop() {
	f.desktopKeyring()
	f.startVM()
	f.t.Log(execVM(f.t, f.manager, f.state, "! python3 -c 'import os; os.chroot(\"/\")' 2>/dev/null && unshare -Ur python3 -c 'import os; os.chroot(\"/\")' && echo DESKTOP_USER_NAMESPACE_OK"))
	for _, name := range []string{"desktop_core.mjs", "desktop_cookie.mjs"} {
		data, err := os.ReadFile(filepath.Join(f.root, "dev", name))
		if err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.run.Workspace, name), data, 0600); err != nil {
			f.t.Fatal(err)
		}
	}
	f.t.Log(execVM(f.t, f.manager, f.state, "node "+shellQuote(filepath.Join(f.run.Workspace, "desktop_core.mjs"))))
	f.t.Log(execVM(f.t, f.manager, f.state, "dbus-run-session -- node "+shellQuote(filepath.Join(f.run.Workspace, "desktop_cookie.mjs"))+" seed"))
	if err := f.manager.StartDesktop(f.ctx, f.state); err != nil {
		f.t.Fatalf("start desktop: %v\n%s", err, execVM(f.t, f.manager, f.state, "cat /var/lib/cooper/desktop/session.log /var/lib/cooper/desktop/app.log"))
	}
	viewerURL, err := vm.OpenDesktopViewer(f.ctx, f.state, f.binary, f.run.CooperDir)
	if err != nil {
		f.t.Fatal(err)
	}
	again, err := vm.OpenDesktopViewer(f.ctx, f.state, f.binary, f.run.CooperDir)
	if err != nil || again != viewerURL {
		f.t.Fatalf("viewer did not reconnect: %v", err)
	}
	base, _, _ := strings.Cut(viewerURL, "#")
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	response, err := client.Get(base + "vnc.html")
	if err != nil {
		f.t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		f.t.Fatalf("unauthorized viewer: %s", response.Status)
	}
	artifacts := filepath.Join(os.TempDir(), "cooper-desktop-"+f.run.ID)
	if err := os.MkdirAll(artifacts, 0700); err != nil {
		f.t.Fatal(err)
	}
	command := exec.CommandContext(f.ctx, "node", filepath.Join(f.root, "dev", "desktop_browser.cjs"), viewerURL, artifacts, f.run.Workspace)
	stageClipboard(f.t, f.driver)
	// Wait for the actual focused terminal, not an unrelated canvas update
	// while ChatGPT loads. The browser still supplies all keyboard input.
	terminalContext, terminalCancel := context.WithTimeout(f.ctx, 90*time.Second)
	defer terminalCancel()
	terminalDone := make(chan error, 1)
	go func() {
		var output bytes.Buffer
		script := `set -eu
for attempt in {1..400}; do
    active=$(xdotool getactivewindow 2>/dev/null || true)
    if xdotool search --onlyvisible --class XTerm 2>/dev/null | grep -qx "$active"; then
        printf ready > ` + shellQuote(filepath.Join(f.run.Workspace, "desktop-terminal-ready")) + `
        break
    fi
    sleep 0.1
done
for attempt in {1..400}; do
    if [ -f ` + shellQuote(filepath.Join(f.run.Workspace, "desktop-request-app")) + ` ]; then
        active=$(xdotool getactivewindow 2>/dev/null || true)
        active_pid=$(xdotool getwindowpid "$active" 2>/dev/null || true)
        native_lock=$(readlink "$CODEX_ELECTRON_USER_DATA_PATH/SingletonLock" 2>/dev/null || true)
        if [ -n "$active_pid" ] && [ "$active_pid" = "${native_lock##*-}" ]; then
            geometry=$(xdotool getwindowgeometry --shell "$active")
            printf '%s\n' "$geometry" > ` + shellQuote(filepath.Join(f.run.Workspace, "desktop-app-ready")) + `
            exit 0
        fi
    fi
    sleep 0.1
done
exit 1`

		terminalDone <- f.manager.ExecCommand(terminalContext, f.state, []string{"bash", "-c", script}, nil, false, nil, &output, &output)
	}()
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		f.t.Log(execVM(f.t, f.manager, f.state, "tail -n 100 /var/lib/cooper/desktop/session.log /var/lib/cooper/desktop/app.log"))
		f.t.Fatalf("desktop browser checks: %v", err)
	}
	if err := <-terminalDone; err != nil {
		f.t.Fatalf("terminal focus: %v", err)
	}
	if got := readFile(f.t, filepath.Join(f.run.Workspace, "cooper-desktop-input")); got != "cooper-gui-ok\n" {
		f.t.Fatalf("terminal input: %q", got)
	}
	image, err := os.ReadFile(filepath.Join(f.run.Workspace, "cooper-desktop-image"))
	if err != nil || !bytes.Equal(image, minimalPNG(f.t)) {
		f.t.Fatalf("host clipboard image changed: %v", err)
	}
	f.checkDesktopCookie()
	if err := f.manager.StartDesktop(f.ctx, f.state); err != nil {
		f.t.Fatal(err)
	}
	f.desktopNetwork()
	execVM(f.t, f.manager, f.state, fmt.Sprintf("test -S /var/run/docker.sock; docker run --rm --network none --entrypoint sh %s -c 'echo desktop-docker-ok'", shellQuote(f.request.ImageRef)))
	execVM(f.t, f.manager, f.state, "printf keep > \"$CODEX_ELECTRON_USER_DATA_PATH/cooper-desktop-persistence\"")
	f.captureRuntime()
	f.state, err = f.manager.Restart(f.ctx, f.state.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	f.observeLifetime()
	execVM(f.t, f.manager, f.state, "test \"$(cat \"$CODEX_ELECTRON_USER_DATA_PATH/cooper-desktop-persistence\")\" = keep; xdotool search --onlyvisible --name ChatGPT")
	f.checkDesktopCookie()
	if f.starts != 2 || f.loads != 2 {
		f.t.Fatalf("desktop check used %d starts and %d imports; want two", f.starts, f.loads)
	}
	f.report["checks"] = []string{"real-app-window", "private-viewer", "viewer-reconnect", "keyboard-and-mouse", "terminal-workspace-write", "text-and-image-clipboard", "full-access-engine-execution", "guest-Docker", "selected-state-after-restart", "native-encrypted-cookie-restore", "no-guest-NIC-or-route", "proxy-allow-and-deny"}
	f.report["desktop_artifacts"] = artifacts
	f.stopVM()
}

func (f *developmentFixture) checkDesktopCookie() {
	script := `set -eu
source /var/lib/cooper/desktop/bus.env
native_lock=$(readlink "$CODEX_ELECTRON_USER_DATA_PATH/SingletonLock")
native_pid=${native_lock##*-}
for attempt in {1..100}; do
    if ! kill -0 "$native_pid" 2>/dev/null; then break; fi
    timeout 1s xdotool search --onlyvisible --all --pid "$native_pid" '.*' windowactivate --sync key --clearmodifiers ctrl+q 2>/dev/null || true
    sleep 0.2
done
xdotool keyup q Control_L Control_R
! kill -0 "$native_pid" 2>/dev/null
node ` + shellQuote(filepath.Join(f.run.Workspace, "desktop_cookie.mjs")) + ` check`
	f.t.Log(execVM(f.t, f.manager, f.state, script))
}
