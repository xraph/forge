package workbench

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed _ui/dist
var assets embed.FS

// Assets serves the bundled interface without a JavaScript runtime or CDN.
func Assets() http.Handler {
	files, err := fs.Sub(assets, "_ui/dist")
	if err != nil {
		panic(err)
	}

	return http.FileServerFS(files)
}
