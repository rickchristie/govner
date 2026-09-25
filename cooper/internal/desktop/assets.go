package desktop

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// The guest controls pixels, not code in the host browser. Keep noVNC and
// the viewer UI in the host binary, outside the writable guest filesystem.
//
//go:embed assets
var viewerAssets embed.FS

func assetHandler() http.Handler {
	files, err := fs.Sub(viewerAssets, "assets")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/assets/", http.FileServer(http.FS(assetFiles{files})))
}

type assetFiles struct{ fs.FS }

func (files assetFiles) Open(name string) (fs.File, error) {
	// Go module archives remove vendor directories. Keep the upstream import
	// URLs and file contents, but store dependencies under a module-safe path.
	if dependency, ok := strings.CutPrefix(name, "novnc/vendor/"); ok {
		name = "novnc/dependencies/" + dependency
	}
	return files.FS.Open(name)
}
