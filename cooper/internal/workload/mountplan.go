package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/runtimefs"
)

// MountPlanDigest returns a stable identity for the complete authorized mount
// set. It lets a reusable runtime detect state-root or policy changes without
// putting the mount list in a Docker label.
func MountPlanDigest(mounts []MountSpec) (string, error) {
	data, err := json.Marshal(mounts)
	if err != nil {
		return "", fmt.Errorf("encode mount plan: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// MountInput contains all values that can affect the shared mount policy.
// GrokStateRoot is required only for Grok because GROK_HOME can move it.
type MountInput struct {
	WorkspaceDir  string
	HomeDir       string
	CooperDir     string
	RuntimeID     string
	ToolName      string
	GrokStateRoot string
	Config        *config.Config
}

// DirectorySpec is a directory that Cooper must create before it renders the
// mount plan. Host state remains host-owned after creation.
type DirectorySpec struct {
	Path string
	Mode fs.FileMode
}

// BuildMountPlan returns the complete ordered mount policy for one workload.
// It does not create or remove a path.
func BuildMountPlan(in MountInput) ([]MountSpec, error) {
	if err := validateHostOwnedRoots(in); err != nil {
		return nil, err
	}

	mounts := []MountSpec{{
		ID: "workspace", Source: in.WorkspaceDir, Target: in.WorkspaceDir,
		Access: ReadWrite, Kind: Directory, Ownership: HostWorkspace,
	}}

	hooks, hasHooks, err := gitHooksMount(in.WorkspaceDir)
	if err != nil {
		return nil, err
	}
	if hasHooks {
		mounts = append(mounts, hooks)
	}

	mounts = append(mounts, agentStateMounts(in)...)
	gitconfig := filepath.Join(in.HomeDir, ".gitconfig")
	if pathIsFile(gitconfig) {
		mounts = append(mounts, MountSpec{
			ID: "git-config", Source: gitconfig,
			Target: filepath.Join(HomeDir, ".gitconfig"),
			Access: ReadOnly, Kind: File, Ownership: HostConfig,
		})
	}

	mounts = append(mounts, LanguageCacheSpecs(in.CooperDir, in.Config)...)

	optionalFiles := []MountSpec{
		{ID: "ca", Source: filepath.Join(in.CooperDir, "ca", "cooper-ca.pem"), Target: "/etc/cooper/cooper-ca.pem", Access: ReadOnly, Kind: File, Ownership: CooperRuntime},
		{ID: "clipboard-token", Source: filepath.Join(in.CooperDir, "tokens", in.RuntimeID), Target: "/etc/cooper/clipboard-token", Access: ReadOnly, Kind: File, Ownership: CooperRuntime},
	}
	for _, spec := range optionalFiles {
		if pathIsFile(spec.Source) {
			mounts = append(mounts, spec)
		}
	}

	shims := filepath.Join(in.CooperDir, "base", "shims")
	if pathIsDirectory(shims) {
		mounts = append(mounts, MountSpec{ID: "clipboard-shims", Source: shims, Target: "/etc/cooper/shims", Access: ReadOnly, Kind: Directory, Ownership: CooperRuntime})
	}
	liveConfig := filepath.Join(in.CooperDir, "live")
	if pathIsDirectory(liveConfig) {
		mounts = append(mounts, MountSpec{ID: "live-config", Source: liveConfig, Target: "/etc/cooper/live", Access: ReadOnly, Kind: Directory, Ownership: CooperRuntime})
	}

	mounts = append(mounts,
		MountSpec{ID: "fonts", Source: filepath.Join(in.CooperDir, "fonts"), Target: FontsDir, Access: ReadOnly, Kind: Directory, Ownership: CooperCache},
		MountSpec{ID: "playwright", Source: filepath.Join(in.CooperDir, "cache", "ms-playwright"), Target: PlaywrightCacheDir, Access: ReadWrite, Kind: Directory, Ownership: CooperCache},
		MountSpec{ID: "tmp", Source: runtimefs.TempDir(in.CooperDir, in.RuntimeID), Target: "/tmp", Access: ReadWrite, Kind: Directory, Ownership: CooperRuntime},
		MountSpec{ID: "session", Source: runtimefs.SessionDir(in.CooperDir, in.RuntimeID), Target: SessionContainerDir, Access: ReadOnly, Kind: Directory, Ownership: CooperRuntime},
	)

	timezone := filepath.Join(runtimefs.SessionDir(in.CooperDir, in.RuntimeID), runtimefs.TimezoneFilename)
	if pathIsFile(timezone) {
		mounts = append(mounts, MountSpec{ID: "timezone", Source: timezone, Target: TimezoneContainerPath, Access: ReadOnly, Kind: File, Ownership: CooperRuntime})
	}

	if err := ValidateMountPlan(mounts, in.CooperDir); err != nil {
		return nil, err
	}
	// Parent targets must mount before child overlays in the guest host
	// namespace. This is necessary when a workspace is below /tmp and for the
	// read-only .git/hooks overlay. Keep sibling order stable for reproducible
	// manifests and mount-plan digests.
	sort.SliceStable(mounts, func(left, right int) bool {
		return pathDepth(mounts[left].Target) < pathDepth(mounts[right].Target)
	})
	return mounts, nil
}

func pathDepth(path string) int {
	return len(strings.Split(strings.Trim(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator)))
}

// RequiredDirectories returns the known directories that can be created for
// the mount plan. It does not include optional host configuration files.
func RequiredDirectories(in MountInput) []DirectorySpec {
	dirs := make([]DirectorySpec, 0, 16)
	for _, mount := range agentStateMounts(in) {
		if mount.Kind != Directory {
			continue
		}
		mode := fs.FileMode(0o755)
		if in.ToolName == "grok" {
			mode = 0o700
		}
		dirs = append(dirs, DirectorySpec{Path: mount.Source, Mode: mode})
	}
	for _, mount := range LanguageCacheSpecs(in.CooperDir, in.Config) {
		dirs = append(dirs, DirectorySpec{Path: mount.Source, Mode: 0o755})
	}
	dirs = append(dirs,
		DirectorySpec{Path: filepath.Join(in.CooperDir, "live"), Mode: 0o755},
		DirectorySpec{Path: filepath.Join(in.CooperDir, "fonts"), Mode: 0o755},
		DirectorySpec{Path: filepath.Join(in.CooperDir, "cache", "ms-playwright"), Mode: 0o755},
		DirectorySpec{Path: runtimefs.TempDir(in.CooperDir, in.RuntimeID), Mode: 0o755},
		DirectorySpec{Path: runtimefs.SessionDir(in.CooperDir, in.RuntimeID), Mode: 0o755},
	)
	return dirs
}

// EnsureDirectories creates only the known state, cache, and runtime
// directories required by a mount plan.
func EnsureDirectories(in MountInput) error {
	// Reject deletion-root overlap before Cooper creates any host-owned state.
	// A later validation error must not leave a new state directory below a
	// root that cooper down or cooper cleanup can remove.
	if err := validateHostOwnedRoots(in); err != nil {
		return err
	}
	if err := ensureGitHooksDirectory(in.WorkspaceDir); err != nil {
		return err
	}
	for _, dir := range RequiredDirectories(in) {
		if err := os.MkdirAll(dir.Path, dir.Mode); err != nil {
			return fmt.Errorf("create mount directory %s: %w", dir.Path, err)
		}
	}
	return nil
}

// ensureGitHooksDirectory creates the mount point only for a normal Git
// directory. A Git worktree uses a .git file and has no hooks path below the
// selected workspace. Symbolic links are rejected because Docker would follow
// them on the physical host before it creates the read-only overlay.
func ensureGitHooksDirectory(workspaceDir string) error {
	gitDir := filepath.Join(workspaceDir, ".git")
	gitInfo, err := os.Lstat(gitDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect workspace Git directory: %w", err)
	}
	if gitInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("workspace .git must not be a symbolic link")
	}
	if !gitInfo.IsDir() {
		return nil
	}

	hooks := filepath.Join(gitDir, "hooks")
	hooksInfo, err := os.Lstat(hooks)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(hooks, 0o755); err != nil {
			return fmt.Errorf("create protected Git hooks directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect workspace Git hooks: %w", err)
	}
	if hooksInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("workspace .git/hooks must not be a symbolic link")
	}
	if !hooksInfo.IsDir() {
		return errors.New("workspace .git/hooks must be a directory")
	}
	return nil
}

func gitHooksMount(workspaceDir string) (MountSpec, bool, error) {
	gitDir := filepath.Join(workspaceDir, ".git")
	gitInfo, err := os.Lstat(gitDir)
	if errors.Is(err, os.ErrNotExist) {
		return MountSpec{}, false, nil
	}
	if err != nil {
		return MountSpec{}, false, fmt.Errorf("inspect workspace Git directory: %w", err)
	}
	if gitInfo.Mode()&os.ModeSymlink != 0 {
		return MountSpec{}, false, errors.New("workspace .git must not be a symbolic link")
	}
	if !gitInfo.IsDir() {
		return MountSpec{}, false, nil
	}

	hooks := filepath.Join(gitDir, "hooks")
	hooksInfo, err := os.Lstat(hooks)
	if errors.Is(err, os.ErrNotExist) {
		return MountSpec{}, false, errors.New("workspace .git/hooks is missing; prepare mount directories first")
	}
	if err != nil {
		return MountSpec{}, false, fmt.Errorf("inspect workspace Git hooks: %w", err)
	}
	if hooksInfo.Mode()&os.ModeSymlink != 0 {
		return MountSpec{}, false, errors.New("workspace .git/hooks must not be a symbolic link")
	}
	if !hooksInfo.IsDir() {
		return MountSpec{}, false, errors.New("workspace .git/hooks must be a directory")
	}

	return MountSpec{
		ID: "git-hooks", Source: hooks, Target: hooks,
		Access: ReadOnly, Kind: Directory, Ownership: HostWorkspace,
	}, true, nil
}

// LanguageCacheSpecs returns the Cooper-owned cache mounts for enabled tools.
func LanguageCacheSpecs(cooperDir string, cfg *config.Config) []MountSpec {
	var mounts []MountSpec
	for _, tool := range cfg.ProgrammingTools {
		if !tool.Enabled {
			continue
		}
		switch tool.Name {
		case "go":
			mounts = append(mounts,
				MountSpec{ID: "go-mod-cache", Source: filepath.Join(cooperDir, "cache", "go-mod"), Target: GoModCacheDir, Access: ReadWrite, Kind: Directory, Ownership: CooperCache},
				MountSpec{ID: "go-build-cache", Source: filepath.Join(cooperDir, "cache", "go-build"), Target: GoBuildCacheDir, Access: ReadWrite, Kind: Directory, Ownership: CooperCache},
			)
		case "node":
			mounts = append(mounts, MountSpec{ID: "npm-cache", Source: filepath.Join(cooperDir, "cache", "npm"), Target: NPMCacheDir, Access: ReadWrite, Kind: Directory, Ownership: CooperCache})
		case "python":
			mounts = append(mounts, MountSpec{ID: "pip-cache", Source: filepath.Join(cooperDir, "cache", "pip"), Target: PIPCacheDir, Access: ReadWrite, Kind: Directory, Ownership: CooperCache})
		}
	}
	return mounts
}

// ValidateMountPlan rejects unsafe targets and host-state overlap with the
// Cooper deletion root. It resolves existing symlink components first.
func ValidateMountPlan(mounts []MountSpec, cooperDir string) error {
	if !filepath.IsAbs(cooperDir) || filepath.Clean(cooperDir) == string(filepath.Separator) {
		return errors.New("cooper directory must be an absolute non-root path")
	}
	targets := make(map[string]MountSpec, len(mounts))
	ids := make(map[string]bool, len(mounts))
	for _, mount := range mounts {
		if strings.TrimSpace(mount.ID) == "" || ids[mount.ID] {
			return fmt.Errorf("mount ID %q is empty or duplicated", mount.ID)
		}
		ids[mount.ID] = true
		if !filepath.IsAbs(mount.Source) || !filepath.IsAbs(mount.Target) {
			return fmt.Errorf("mount %s must use absolute paths", mount.ID)
		}
		if filepath.Clean(mount.Source) != mount.Source || filepath.Clean(mount.Target) != mount.Target || mount.Target == string(filepath.Separator) {
			return fmt.Errorf("mount %s must use clean non-root paths", mount.ID)
		}
		if mount.Access != ReadOnly && mount.Access != ReadWrite {
			return fmt.Errorf("mount %s has invalid access %q", mount.ID, mount.Access)
		}
		if mount.Kind != File && mount.Kind != Directory {
			return fmt.Errorf("mount %s has invalid path kind %q", mount.ID, mount.Kind)
		}
		switch mount.Ownership {
		case HostWorkspace, HostState, HostConfig, CooperCache, CooperRuntime:
		default:
			return fmt.Errorf("mount %s has invalid ownership %q", mount.ID, mount.Ownership)
		}
		if mount.Ownership == HostState || mount.Ownership == HostWorkspace {
			overlaps, err := pathsOverlapAfterSymlinks(mount.Source, cooperDir)
			if err != nil {
				return fmt.Errorf("validate host-owned path %s: %w", mount.Source, err)
			}
			if overlaps {
				return fmt.Errorf("host-owned path %q overlaps Cooper-owned directory %q", mount.Source, cooperDir)
			}
		}
		info, err := os.Stat(mount.Source)
		if err != nil {
			return fmt.Errorf("inspect mount source %s: %w", mount.Source, err)
		}
		if (mount.Kind == Directory && !info.IsDir()) || (mount.Kind == File && !info.Mode().IsRegular()) {
			return fmt.Errorf("mount %s source does not match path kind %s", mount.ID, mount.Kind)
		}
		target := filepath.Clean(mount.Target)
		if previous, exists := targets[target]; exists {
			return fmt.Errorf("mounts %s and %s use target %s", previous.ID, mount.ID, target)
		}
		targets[target] = mount
	}
	for _, left := range mounts {
		for _, right := range mounts {
			if left.ID == right.ID || !pathContains(left.Target, right.Target) || left.Target == right.Target {
				continue
			}
			if allowedTargetOverlay(left, right, mounts) {
				continue
			}
			return fmt.Errorf("mount targets %s and %s overlap at %s and %s", left.ID, right.ID, left.Target, right.Target)
		}
	}
	return nil
}

func allowedTargetOverlay(parent, child MountSpec, mounts []MountSpec) bool {
	if parent.ID == "tmp" && child.ID == "workspace" && child.Access == ReadWrite {
		return true
	}
	if parent.ID == "workspace" && child.ID == "git-hooks" && child.Access == ReadOnly {
		return true
	}
	if parent.ID != "tmp" || child.ID != "git-hooks" || child.Access != ReadOnly {
		return false
	}
	for _, mount := range mounts {
		if mount.ID == "workspace" && mount.Access == ReadWrite &&
			pathContains(parent.Target, mount.Target) && pathContains(mount.Target, child.Target) {
			return true
		}
	}
	return false
}

func validateMountInput(in MountInput) error {
	if in.Config == nil {
		return errors.New("mount config is required")
	}
	for name, value := range map[string]string{
		"workspace":        in.WorkspaceDir,
		"home":             in.HomeDir,
		"Cooper directory": in.CooperDir,
		"runtime ID":       in.RuntimeID,
		"tool":             in.ToolName,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if !filepath.IsAbs(in.WorkspaceDir) || !filepath.IsAbs(in.HomeDir) || !filepath.IsAbs(in.CooperDir) {
		return errors.New("workspace, home, and Cooper paths must be absolute")
	}
	if filepath.Clean(in.WorkspaceDir) == string(filepath.Separator) || filepath.Clean(in.CooperDir) == string(filepath.Separator) {
		return errors.New("workspace and Cooper directory must not be the file-system root")
	}
	if in.ToolName == "grok" && !filepath.IsAbs(in.GrokStateRoot) {
		return errors.New("grok state root must be absolute")
	}
	return nil
}

func validateHostOwnedRoots(in MountInput) error {
	if err := validateMountInput(in); err != nil {
		return err
	}
	paths := []string{in.WorkspaceDir}
	for _, mount := range agentStateMounts(in) {
		paths = append(paths, mount.Source)
	}
	for _, path := range paths {
		if err := ValidateHostOwnedPath(path, in.CooperDir); err != nil {
			return err
		}
	}
	return nil
}

func agentStateMounts(in MountInput) []MountSpec {
	state := func(id, source, target string, kind PathKind) MountSpec {
		return MountSpec{ID: id, Source: source, Target: target, Access: ReadWrite, Kind: kind, Ownership: HostState}
	}
	switch in.ToolName {
	case "claude":
		mounts := []MountSpec{state("claude-state", filepath.Join(in.HomeDir, ".claude"), filepath.Join(HomeDir, ".claude"), Directory)}
		jsonPath := filepath.Join(in.HomeDir, ".claude.json")
		if pathIsFile(jsonPath) {
			mounts = append(mounts, state("claude-config", jsonPath, filepath.Join(HomeDir, ".claude.json"), File))
		}
		return mounts
	case "copilot":
		return []MountSpec{state("copilot-state", filepath.Join(in.HomeDir, ".copilot"), filepath.Join(HomeDir, ".copilot"), Directory)}
	case "codex":
		return []MountSpec{state("codex-state", filepath.Join(in.HomeDir, ".codex"), filepath.Join(HomeDir, ".codex"), Directory)}
	case "opencode":
		return []MountSpec{
			state("opencode-cache", filepath.Join(in.HomeDir, ".cache", "opencode"), filepath.Join(HomeDir, ".cache", "opencode"), Directory),
			state("opencode-config", filepath.Join(in.HomeDir, ".config", "opencode"), filepath.Join(HomeDir, ".config", "opencode"), Directory),
			state("opencode-share", filepath.Join(in.HomeDir, ".local", "share", "opencode"), filepath.Join(HomeDir, ".local", "share", "opencode"), Directory),
			state("opencode-local-state", filepath.Join(in.HomeDir, ".local", "state", "opencode"), filepath.Join(HomeDir, ".local", "state", "opencode"), Directory),
			state("opencode-compat", filepath.Join(in.HomeDir, ".opencode"), filepath.Join(HomeDir, ".opencode"), Directory),
		}
	case "grok":
		return []MountSpec{state("grok-state", in.GrokStateRoot, GrokStateRoot, Directory)}
	default:
		return nil
	}
}

func pathIsDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func pathIsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func pathsOverlapAfterSymlinks(left, right string) (bool, error) {
	leftResolved, err := resolveExistingPath(left)
	if err != nil {
		return false, err
	}
	rightResolved, err := resolveExistingPath(right)
	if err != nil {
		return false, err
	}
	return pathsOverlap(left, right) || pathsOverlap(leftResolved, rightResolved), nil
}

func pathsOverlap(left, right string) bool {
	return pathContains(left, right) || pathContains(right, left)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
