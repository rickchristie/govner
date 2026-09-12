package docker

import "github.com/rickchristie/govner/cooper/internal/workload"

// Canonical runtime paths inside Cooper barrel containers.
//
// Docker mount wiring and generated image environment variables must share
// these constants so cache locations cannot drift from the paths that tools
// actually use at runtime.
const (
	BarrelGoPath          = workload.GoPath
	BarrelGoBinDir        = workload.GoBinDir
	BarrelGoModCacheDir   = workload.GoModCacheDir
	BarrelGoBuildCacheDir = workload.GoBuildCacheDir

	BarrelNPMCacheDir        = workload.NPMCacheDir
	BarrelPIPCacheDir        = workload.PIPCacheDir
	BarrelPlaywrightCacheDir = workload.PlaywrightCacheDir
	BarrelFontsDir           = workload.FontsDir

	// BarrelGrokLeaderSocket keeps Grok process transport in the per-barrel
	// /tmp mount. A barrel must not attach to a leader process on the host.
	BarrelGrokLeaderSocket = workload.GrokLeaderSocket
)
