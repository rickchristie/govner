package desktop

import (
	"embed"
	"io/fs"
	"net/http"
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
	return http.StripPrefix("/assets/", http.FileServer(http.FS(files)))
}
