package docker

// Canonical runtime paths inside Cooper barrel containers.
//
// Docker mount wiring and generated image environment variables must share
// these constants so cache locations cannot drift from the paths that tools
// actually use at runtime.
const (
	BarrelHomeDir = "/home/user"
	// BarrelSessionContainerDir is a host-controlled read-only bind mount used
	// for runtime session control files. Cooper writes per-session env/timezone
	// files on the host here so barrels cannot pre-create or tamper with them.
	BarrelSessionContainerDir = "/run/cooper/session"

	BarrelGoPath          = "/go"
	BarrelGoBinDir        = BarrelGoPath + "/bin"
	BarrelGoModCacheDir   = BarrelGoPath + "/pkg/mod"
	BarrelGoBuildCacheDir = BarrelHomeDir + "/.cache/go-build"

	BarrelNPMCacheDir        = BarrelHomeDir + "/.npm"
	BarrelPIPCacheDir        = BarrelHomeDir + "/.cache/pip"
	BarrelPlaywrightCacheDir = BarrelHomeDir + "/.cache/ms-playwright"
	BarrelFontsDir           = BarrelHomeDir + "/.local/share/fonts"
)
