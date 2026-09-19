#!/bin/sh
# Use already prepared native images, fake homes, and no network or host mounts.
# This checks local writes, not account login or provider token refresh.
set -eu
prefix=${1:-test-mirror}
for tool in codex claude opencode copilot grok antigravity; do
    image="$prefix-cooper-cli-$tool:latest"
    docker image inspect "$image" >/dev/null
    docker run --rm -i --network none --entrypoint /bin/sh -e "PROBE_TOOL=$tool" "$image" -s <<'PROBE'
set -eu
probe=/tmp/cooper-native-profile-probe
mkdir -p "$probe/home" "$probe/store" "$probe/work"
export HOME="$probe/home"
export XDG_CONFIG_HOME="$HOME/.config" XDG_DATA_HOME="$HOME/.local/share" XDG_CACHE_HOME="$HOME/.cache" XDG_STATE_HOME="$HOME/.local/state"
cd "$probe/work"
for name in .codex .claude .grok .copilot .gemini .opencode; do
    mkdir -p "$probe/store/$name"
    ln -s "$probe/store/$name" "$HOME/$name"
done
for path in "$XDG_CONFIG_HOME/opencode" "$XDG_DATA_HOME/opencode" "$XDG_CACHE_HOME/opencode" "$XDG_STATE_HOME/opencode"; do
    mkdir -p "$(dirname "$path")" "$probe/store/$(basename "$(dirname "$path")")-opencode"
    ln -s "$probe/store/$(basename "$(dirname "$path")")-opencode" "$path"
done
case "$PROBE_TOOL" in
    codex)
        codex --version
        CODEX_HOME="$HOME/.codex" timeout 20s codex mcp add fixture -- echo test
        test -f "$probe/store/.codex/config.toml"
        test -L "$HOME/.codex"
        printf 'codex: native config write through directory link passed\n'
        ;;
    claude)
        claude --version
        ln -s "$probe/store/settings.json" "$HOME/.claude.json"
        timeout 20s claude mcp add --scope user fixture echo test
        test -f "$probe/store/settings.json"
        test -L "$HOME/.claude.json"
        test -L "$HOME/.claude"
        printf 'claude: native write through directory and missing-file links passed\n'
        ;;
    opencode)
        opencode --version
        timeout 30s opencode session list
        test -L "$XDG_DATA_HOME/opencode"
        test -f "$XDG_DATA_HOME/opencode/opencode.db"
        printf 'opencode: native database creation through directory link passed\n'
        ;;
    copilot|grok)
        "$PROBE_TOOL" --version
        timeout 20s "$PROBE_TOOL" --help >/dev/null
        printf '%s: offline command startup passed; native account writes need host acceptance\n' "$PROBE_TOOL"
        ;;
    antigravity)
        agy --version
        DBUS_SESSION_BUS_ADDRESS=unix:path=/dev/null timeout 20s agy --help >/dev/null 2>&1
        printf 'antigravity: offline startup passed; real OAuth refresh needs host acceptance\n'
        ;;
esac
PROBE
done
