package workload

import "github.com/rickchristie/govner/cooper/internal/runtimefs"

// CanonicalPath values are shared by all Cooper execution back ends.
const (
	HomeDir               = "/home/user"
	SessionContainerDir   = runtimefs.SessionContainerDir
	TimezoneContainerPath = runtimefs.TimezoneContainerPath

	GoPath          = "/go"
	GoBinDir        = GoPath + "/bin"
	GoModCacheDir   = GoPath + "/pkg/mod"
	GoBuildCacheDir = HomeDir + "/.cache/go-build"

	NPMCacheDir        = HomeDir + "/.npm"
	PIPCacheDir        = HomeDir + "/.cache/pip"
	PlaywrightCacheDir = HomeDir + "/.cache/ms-playwright"
	FontsDir           = HomeDir + "/.local/share/fonts"

	GrokStateRoot    = HomeDir + "/.grok"
	GrokLeaderSocket = "/tmp/cooper-grok-leader.sock"
)
