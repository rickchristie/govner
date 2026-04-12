package docker

import (
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/config"
)

// cacheMountSpec describes a single language cache volume mount: where it
// lives on the host (under cooperDir) and where it appears inside the
// barrel container. All specs are read-write — Cooper owns these caches
// and the barrel fills them during normal package-manager usage.
type cacheMountSpec struct {
	Name          string // human-readable label, e.g. "go-mod"
	HostPath      string // absolute path on the host
	ContainerPath string // absolute path inside the container
}

// languageCacheSpecs returns the cache mount specs for every enabled
// programming tool. All host paths live under cooperDir/cache/ so Cooper
// fully owns the cache lifecycle — no host tool caches are mounted.
//
// These caches are intentionally runtime-only: build/update stay image-
// focused, barrel startup just creates and mounts the directories, and the
// caches start empty and fill naturally during normal package-manager usage.
// Cooper does not seed or import host caches during build, update, or barrel
// startup.
//
// This is the single source of truth for language cache paths. It is used
// by appendLanguageCacheMounts (volume flags), barrelMountDirs (directory
// pre-creation), and unit tests.
func languageCacheSpecs(cooperDir string, cfg *config.Config) []cacheMountSpec {
	var specs []cacheMountSpec

	for _, tool := range cfg.ProgrammingTools {
		if !tool.Enabled {
			continue
		}
		switch tool.Name {
		case "go":
			specs = append(specs,
				cacheMountSpec{
					Name:          "go-mod",
					HostPath:      filepath.Join(cooperDir, "cache", "go-mod"),
					ContainerPath: BarrelGoModCacheDir,
				},
				cacheMountSpec{
					Name:          "go-build",
					HostPath:      filepath.Join(cooperDir, "cache", "go-build"),
					ContainerPath: BarrelGoBuildCacheDir,
				},
			)
		case "node":
			specs = append(specs, cacheMountSpec{
				Name:          "npm",
				HostPath:      filepath.Join(cooperDir, "cache", "npm"),
				ContainerPath: BarrelNPMCacheDir,
			})
		case "python":
			specs = append(specs, cacheMountSpec{
				Name:          "pip",
				HostPath:      filepath.Join(cooperDir, "cache", "pip"),
				ContainerPath: BarrelPIPCacheDir,
			})
		}
	}

	return specs
}

// barrelMountDirs returns every host directory that must exist before
// Docker bind-mounts them into a barrel. This includes auth dirs (tool-
// specific), language cache dirs (from languageCacheSpecs), Playwright
// support dirs, and the per-barrel /tmp directory.
//
// The list is computed purely from arguments — no I/O. The caller
// (ensureBarrelMountDirs) handles os.MkdirAll.
func barrelMountDirs(homeDir, toolName, cooperDir, containerName string, cfg *config.Config) []string {
	var dirs []string

	// Tool-specific auth directories.
	switch toolName {
	case "claude":
		dirs = append(dirs, filepath.Join(homeDir, ".claude"))
	case "copilot":
		dirs = append(dirs, filepath.Join(homeDir, ".copilot"))
	case "codex":
		dirs = append(dirs, filepath.Join(homeDir, ".codex"))
	case "opencode":
		dirs = append(dirs,
			filepath.Join(homeDir, ".cache", "opencode"),
			filepath.Join(homeDir, ".config", "opencode"),
			filepath.Join(homeDir, ".local", "share", "opencode"),
			filepath.Join(homeDir, ".local", "state", "opencode"),
			filepath.Join(homeDir, ".opencode"),
		)
	}

	// Cooper-managed language caches.
	for _, spec := range languageCacheSpecs(cooperDir, cfg) {
		dirs = append(dirs, spec.HostPath)
	}

	// Playwright support dirs — must exist before Docker mounts them
	// so Docker does not create them as root-owned directories.
	dirs = append(dirs,
		filepath.Join(cooperDir, "fonts"),
		filepath.Join(cooperDir, "cache", "ms-playwright"),
	)

	// Per-barrel /tmp directory — isolated per container to avoid
	// collisions between barrels sharing a workspace. Cooper resets the
	// shared tmp root at cooper-up startup and shutdown.
	dirs = append(dirs, filepath.Join(cooperDir, "tmp", containerName))

	return dirs
}
