// Package shellassets carries the built dashboard shell.
//
// The directory below is a PLACEHOLDER. The real artifact is a tarball
// published by github.com/xraph/forge-dashboard on release, which this repo's
// release CI downloads and unpacks over the top before the release build.
// Nothing built is ever committed here: an earlier design committed an 11MB
// dist and //go:embed all:dist pulled every byte of it into every binary that
// imported the dashboard extension, 6MB of which was sourcemaps.
//
// The placeholder exists because //go:embed needs the files present at build
// time. Without it a plain `go build` fails, which would make the repo
// unbuildable offline and in every consumer's CI. It is a few KB.
package shellassets

import (
	"bytes"
	"embed"
	"io/fs"
)

//go:embed dist
var distFS embed.FS

// FS returns the shell's files rooted at dist/.
func FS() (fs.FS, error) { return fs.Sub(distFS, "dist") }

// placeholderSentinel is the marker committed into the placeholder's own
// index.html. It is the only reliable signal: a real build's size, file count
// and asset names all change from release to release, so none of them can tell
// a placeholder apart from a genuine artifact.
const placeholderSentinel = `name="forge-dashboard-shell-placeholder"`

// IsPlaceholder reports whether the embedded content is the committed
// placeholder rather than a real build. Callers use it to log a warning at
// startup instead of serving a page that looks broken for no visible reason.
func IsPlaceholder() bool {
	raw, err := distFS.ReadFile("dist/index.html")
	if err != nil {
		// No index at all is not a real build either.
		return true
	}

	return bytes.Contains(raw, []byte(placeholderSentinel))
}
