package vme2e

import (
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

// Use a physical workspace below the selected root. Every public and
// historical path must enforce the same hook limits after startup and restart.
func (f *developmentFixture) prepareProfileWorktreeHooks(selection profiles.Selection) (string, string) {
	for _, mount := range selection.Paths.Mounts {
		if mount.ID != f.tool+"-state" {
			continue
		}
		workspace := filepath.Join(mount.Source, "worktrees", "project")
		writeFile(f.t, filepath.Join(workspace, ".git", "hooks", "kept"), "hook-ok")
		canonical, err := filepath.EvalSymlinks(workspace)
		if err != nil {
			f.t.Fatal(err)
		}
		var paths []string
		for _, root := range append([]string{mount.Target}, mount.CanonicalPaths...) {
			paths = append(paths, shellQuote(filepath.Join(root, "worktrees", "project")))
		}
		script := "for project in " + strings.Join(paths, " ") + `; do
    test "$(cat "$project/.git/hooks/kept")" = hook-ok
    printf workspace-ok > "$project/writable"
    if touch "$project/.git/hooks/new" 2>/dev/null; then echo "created hook: $project"; exit 1; fi
    if (printf changed > "$project/.git/hooks/kept") 2>/dev/null; then echo "changed hook: $project"; exit 1; fi
    if mv "$project/.git/hooks/kept" "$project/.git/hooks/moved" 2>/dev/null; then echo "moved hook: $project"; exit 1; fi
    if rm "$project/.git/hooks/kept" 2>/dev/null; then echo "removed hook: $project"; exit 1; fi
done
`
		return canonical, script
	}
	f.t.Fatal("selected profile has no main state root")
	return "", ""
}

// Test the source of each virtiofs export outside the guest. Protection here
// still applies when guest root mounts a tag without its guest hook overlay.
func (f *developmentFixture) checkProfileHookExports() {
	f.command("docker", "exec", f.state.ContainerName, "sh", "-ec", `
count=0
for hooks in /cooper/mounts/*/.git/hooks /cooper/mounts/*/worktrees/project/.git/hooks; do
    test -f "$hooks/kept" || continue
    test "$(cat "$hooks/kept")" = hook-ok
    printf export-ok > "$hooks/../writable"
    if touch "$hooks/export-write" 2>/dev/null; then echo "writable export: $hooks"; exit 1; fi
    count=$((count + 1))
done
test "$count" -ge 3
`)
}
