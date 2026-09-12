package workload

import "github.com/rickchristie/govner/cooper/internal/runtimefs"

// CanonicalPath values are shared by all Cooper execution back ends.
const (
	BinDir                = "/opt/cooper/bin"
	RuntimeDir            = "/var/lib/cooper"
	ClipboardDir          = RuntimeDir + "/clipboard"
	SessionContainerDir   = runtimefs.SessionContainerDir
	TimezoneContainerPath = runtimefs.TimezoneContainerPath

	GoPath          = "/go"
	GoBinDir        = GoPath + "/bin"
	GoModCacheDir   = GoPath + "/pkg/mod"
	GoBuildCacheDir = RuntimeDir + "/cache/go-build"

	NPMCacheDir        = RuntimeDir + "/cache/npm"
	PIPCacheDir        = RuntimeDir + "/cache/pip"
	PlaywrightCacheDir = RuntimeDir + "/cache/ms-playwright"
	FontsDir           = RuntimeDir + "/fonts"

	GrokLeaderSocket = "/tmp/cooper-grok-leader.sock"
)
