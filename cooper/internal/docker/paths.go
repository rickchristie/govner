package docker

import "github.com/rickchristie/govner/cooper/internal/workload"

// Canonical runtime paths inside Cooper barrel containers.
//
// Docker mount wiring and generated image environment variables must share
// these constants so cache locations cannot drift from the paths that tools
// actually use at runtime.
const (
	BarrelHomeDir = workload.HomeDir

	BarrelGoPath          = workload.GoPath
	BarrelGoBinDir        = workload.GoBinDir
	BarrelGoModCacheDir   = workload.GoModCacheDir
	BarrelGoBuildCacheDir = workload.GoBuildCacheDir

	BarrelNPMCacheDir        = workload.NPMCacheDir
	BarrelPIPCacheDir        = workload.PIPCacheDir
	BarrelPlaywrightCacheDir = workload.PlaywrightCacheDir
	BarrelFontsDir           = workload.FontsDir

	// BarrelGrokStateRoot is the container view of the host Grok state root.
	// The image binary stays outside this mount in ~/.local/bin.
	BarrelGrokStateRoot = workload.GrokStateRoot
	// BarrelGrokLeaderSocket keeps Grok process transport in the per-barrel
	// /tmp mount. A barrel must not attach to a leader process on the host.
	BarrelGrokLeaderSocket = workload.GrokLeaderSocket
)
