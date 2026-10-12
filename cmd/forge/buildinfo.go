package main

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// buildMeta is what `forge -v` and `forge version` report.
type buildMeta struct {
	Version string
	Commit  string
	Date    string
}

// shortRevLen matches the revision length Go puts in pseudo-versions.
const shortRevLen = 12

// pseudoRevPattern pulls the commit out of any pseudo-version form
// (v0.0.0-<ts>-<rev>, vX.Y.Z-pre.0.<ts>-<rev>, vX.Y.Z-0.<ts>-<rev>),
// ignoring build metadata such as +dirty or +incompatible.
var pseudoRevPattern = regexp.MustCompile(`[-.]\d{14}-([0-9a-f]{12})(?:\+.*)?$`)

// currentBuildMeta returns the version info for this binary.
//
// Release builds (GoReleaser, make install) set version, commit and buildDate
// through ldflags. `go install github.com/xraph/forge/cmd/forge@latest` builds
// from the module proxy and applies no ldflags, so the values come from the
// build info the Go toolchain embeds in every binary instead.
func currentBuildMeta() buildMeta {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		info = nil
	}

	return resolveBuildMeta(buildMeta{Version: version, Commit: commit, Date: buildDate}, info)
}

// resolveBuildMeta keeps every value ldflags set and fills the rest from info.
// The build date is never guessed: proxy builds don't record one, and a
// commit timestamp is not a build time.
func resolveBuildMeta(ld buildMeta, info *debug.BuildInfo) buildMeta {
	out := ld

	if isSet(out.Version, "dev") {
		out.Version = strings.TrimPrefix(out.Version, "v")
	}

	if info == nil {
		return out
	}

	if !isSet(out.Version, "dev") {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			out.Version = strings.TrimPrefix(v, "v")
		}
	}

	if !isSet(out.Commit, "unknown") {
		if rev := vcsRevision(info); rev != "" {
			out.Commit = rev
		} else if m := pseudoRevPattern.FindStringSubmatch(info.Main.Version); m != nil {
			out.Commit = m[1]
		}
	}

	return out
}

// vcsRevision returns the short VCS revision stamped by a local `go build`,
// with -dirty appended when the tree had uncommitted changes.
func vcsRevision(info *debug.BuildInfo) string {
	var rev, modified string

	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}

	if rev == "" {
		return ""
	}

	if len(rev) > shortRevLen {
		rev = rev[:shortRevLen]
	}

	if modified == "true" {
		rev += "-dirty"
	}

	return rev
}

func isSet(value, placeholder string) bool {
	return value != "" && value != placeholder
}
