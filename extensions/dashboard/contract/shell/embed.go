package shell

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the production-built shell's static files. Files live under "dist/"
// within the embedded FS; the returned fs.FS strips that prefix so the static
// handler sees a flat root.
//
// The dist/ directory must exist at build time. Run `pnpm build` inside this
// directory before `go build` from the repo root.
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
