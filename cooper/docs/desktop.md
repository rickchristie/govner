# ChatGPT desktop

Cooper installs the official Linux ChatGPT application in a selected workload
image. `cooper vm chatgpt` starts it and opens a private viewer in the host
browser. The desktop and all app processes run inside the VM.

## Install and open

1. Run `cooper configure`. In **AI Tools**, enable **ChatGPT (desktop)**.
2. Select **Mirror**, **Latest**, or **Pin**, then save and build. You can also
   run `cooper build` after saving.
3. Keep `cooper up` running. From the project directory, run:

   ```sh
   cooper vm chatgpt
   ```

The app opens directly. No command in a guest terminal is required. The first
VM launch prepares the guest and imports its image; later launches reuse it.
The host terminal prints a private link if it cannot open the browser.

Use **Terminal** for a shell in the launch workspace. `chatgpt` in that shell
opens or raises the app. **Clipboard** sends text to the desktop and lets you
copy text back. To paste a host image, copy the image, open **Clipboard**, and
select **Use host image** before pasting in the app. Normal copy and paste
between guest apps remains available. **Full screen** gives the display more
room. A window can be moved with its title bar; double-click the title bar to
maximize it.

Closing the app leaves the desktop running. Use **ChatGPT** to open it again.
Closing the viewer leaves both the desktop and VM running. Run the launch
command again to reconnect, or select **Reconnect** after a display interruption.
Stop or restart the VM from Runtimes, or use:

```sh
cooper vm list
cooper vm restart <runtime-id>
cooper vm stop <runtime-id>
```

After a VM restart, run `cooper vm chatgpt` again to open its new private link.
A link from the old VM lifetime loses access.

Cooper 0.7.0 installed through `go install` has incomplete viewer assets.
Update to 0.7.1 or later, then stop the ChatGPT runtime and start it again.
An existing viewer keeps the old Cooper binary until its runtime stops.

`cooper vm chatgpt -c 'command'` runs a command without opening the viewer.
`cooper cli chatgpt` uses the same image and viewer in a Docker barrel. A host
that blocks unprivileged user namespaces can refuse the native app sandbox;
Cooper reports that condition and directs the user to VM mode. Cooper does
not disable the app sandbox to get past a host kernel policy.

## Versions and build cost

**Mirror** reads the installed Linux package metadata without starting the
host app. **Latest** resolves the current official package index. **Pin**
keeps the requested official version, including an older package that has
left the moving index. There is no version allowlist.

Cooper saves the exact package path, architecture, size, and index SHA256 for
both amd64 and arm64 before building. For older packages without an index
record, it checks the exact official HTTPS path and size. The build checks
package name, architecture, and version in all cases. An unavailable pin
fails; it never changes to Latest. Rebuild to update the installed app.

The desktop needs a browser, fonts, window manager, private session bus, and
display server. These packages use a separate Debian desktop base. Users who
do not select ChatGPT do not build that base. Login data is never included in
an image. The Cooper VM backend currently requires Linux x86-64; package
availability for arm64 does not add another supported VM platform.

## Full access and state

The bundled Codex engine starts with `danger-full-access` and approval policy
`never`. These are process settings; Cooper does not rewrite the selected
host `config.toml`. Existing tasks can retain explicit per-task permission
settings in the app. Select Full access there when continuing such a task.

Full access includes the writable workspace, complete selected state roots,
and, in VM mode, the guest Docker daemon. The ordinary host Docker socket is
not mounted. The host proxy, read-only Git hook mounts, and selected-root
limits still apply. The app can change or delete writable data in that scope.

The shared state catalog selects these complete roots:

| State | Default or override |
| --- | --- |
| Engine, history, configuration, skills and plugins | `CODEX_HOME`, or `~/.codex` |
| Desktop databases, login cookies, settings and browser partitions | `CODEX_ELECTRON_USER_DATA_PATH`, or `$XDG_CONFIG_HOME/Codex` with `~/.config` as the config default |
| Chromium cache | The desktop path relative to XDG config, below XDG cache; a profile outside XDG config keeps its cache inside the profile |
| Shared skills | `~/.agents`, kept global |
| Plugin marketplaces | `~/.claude-plugin` and `~/.cursor-plugin` |

Sources and targets use the same absolute paths in CLI and VM modes. No
unselected agent roots or parent state directories are added. Runtime and
cache cleanup preserve these host roots.

Close a host app that uses the same desktop profile before starting Cooper.
Chromium must have one writer for a profile. Cooper rejects a live host
instance and removes only a proven stale lock from that host. Each Cooper
desktop has a distinct stable hostname, so a foreign live app lock is not
mistaken for a stale local process.

To keep the host app open, select separate state explicitly before launch:

```sh
CODEX_HOME="$HOME/.codex-cooper-desktop" \
CODEX_ELECTRON_USER_DATA_PATH="$HOME/.config/ChatGPT-Cooper" \
cooper vm chatgpt
```

These are persistent host directories. They start without the normal app's
login or history. Sign in through the guest app and browser. No rebuild is
needed to select different app state.

Linux cookies can use the host login keyring. If selected cookie databases
contain Chromium `v11` encryption, Cooper reads only the exact Chromium Safe
Storage key from an unlocked Secret Service. It sends that key through the
session exec channel into a private, in-memory guest keyring. It does not
mount the host keyring or forward the host session bus. The key is not saved
in Cooper config, Docker labels, image layers, or logs. Restart reads it
again. A missing, locked, or ambiguous key fails before app startup; unlock
the host keyring and retry. A different keyring backend needs a supported
adapter before its encrypted cookies can be reused. Fresh state uses the
native basic cookie store inside the selected private profile.

Named ChatGPT account profiles are not enabled for account replacement. The
app's account can differ from its local Codex engine account. Cooper refuses
to treat engine credentials as proof of desktop identity. Use ordinary state
or the explicit path selection above until a desktop identity adapter exists.

## Display and network boundary

QEMU has no network device. A host-initiated control stream reaches only the
selected workload's fixed display endpoint. Guest-initiated requests cannot
use that service as a host relay. All external app, browser, engine, Docker,
and guest-container requests remain subject to the host Cooper proxy.

The host viewer listens only on IPv4 loopback. Its random private token enters
through a URL fragment and stays in that viewer tab's session storage. The
port is part of the storage origin. Cookies cannot isolate local ports, so
the viewer uses an authorization header and a WebSocket protocol value.
Cooper removes the token before opening the guest connection. Host and Origin
checks reject other sites. Viewers stop when their runtime identity ends.

Cooper embeds noVNC and serves the viewer code from the host binary. The guest
supplies only RFB display data over the authenticated WebSocket. This keeps
guest HTML and JavaScript out of the host browser's network authority. No
host display, session bus, keyring socket, or arbitrary guest port is exposed.

The guest Chromium browser handles external sign-in pages and local callbacks.
The official app's native features and platform limits still apply; adding a
desktop does not add Linux Computer Use support to the app.

## Verification

Use the prepared development workflow from [TECHNICAL.md](../TECHNICAL.md#prepared-vm-development):

```sh
./cooper/test-vm-dev.sh unit
./cooper/test-vm-dev.sh prepare-agent chatgpt
./cooper/test-vm-dev.sh parity chatgpt
./cooper/test-vm-dev.sh desktop chatgpt
```

The desktop fixture uses an empty home, synthetic credentials, and local model
responses. It starts the real package, drives the host viewer with Playwright,
and saves screenshots under `/tmp`. It checks input, clipboard, resize,
reconnect, workspace writes, the real engine's full-access defaults and tool
execution, the private guest Docker daemon, encrypted native cookies after
app and VM restart, proxy policy, and refusal of direct networking. It does
not sign in to a real account or make a paid model request.

Native Chromium batches cookie writes. The fixture waits for the encrypted
database commit before quitting, rather than using a fixed delay. Private
Secret Service tests check locked, missing, and ambiguous keys without reading
the host keyring. Viewer tests cover tokens, host names, origins, cross-site
requests, WebSocket forwarding, and process lifetime.

The existing X11 image bridge normally retains clipboard ownership for CLI
agents. Desktop mode allows it to yield to other apps. **Use host image**
sends a guest window-manager shortcut which restores that ownership. This
keeps image access in the same authenticated bridge without discarding text
copied inside the desktop.

Run the full Go, shell E2E, and Docker build suites after the prepared checks.
Run these suites in sequence: VM tests hold the host state lock while another
build can need an exclusive lock for agent setup. The full VM release gate is
reserved for releases.

An uncached Go run also rebuilds account layers for its Docker fixtures. Run
packages in sequence so that time spent waiting for another package's Docker
lock does not consume the test timeout:

```sh
GOFLAGS='-p=1 -timeout=60m' go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1
```

## References

- [Official Linux installation](https://learn.chatgpt.com/docs/linux/linux-app)
- [noVNC API](https://novnc.com/noVNC/docs/API.html)
- [Electron proxy switches](https://www.electronjs.org/docs/latest/api/command-line-switches)
