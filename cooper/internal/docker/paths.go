package docker

// Canonical runtime paths inside Cooper barrel containers.
//
// Docker mount wiring and generated image environment variables must share
// these constants so cache locations cannot drift from the paths that tools
// actually use at runtime.
const (
	BarrelHomeDir = "/home/user"

	BarrelGoPath          = "/go"
	BarrelGoBinDir        = BarrelGoPath + "/bin"
	BarrelGoModCacheDir   = BarrelGoPath + "/pkg/mod"
	BarrelGoBuildCacheDir = BarrelHomeDir + "/.cache/go-build"

	BarrelNPMCacheDir        = BarrelHomeDir + "/.npm"
	BarrelPIPCacheDir        = BarrelHomeDir + "/.cache/pip"
	BarrelPlaywrightCacheDir = BarrelHomeDir + "/.cache/ms-playwright"
	BarrelFontsDir           = BarrelHomeDir + "/.local/share/fonts"
)
