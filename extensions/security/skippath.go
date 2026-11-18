package security

import "strings"

// shouldSkipPath reports whether path is covered by one of the skip entries.
//
// An entry matches the path itself and anything below it, but only on a path
// segment boundary. A bare strings.HasPrefix would let a skip entry of
// "/public" also exempt "/publicity-admin", which is a different route that the
// operator never meant to open up.
//
// An entry that ends in "/" is treated the same as the entry without the
// trailing slash, so "/public/" covers "/public" and "/public/x". The entry "/"
// covers every path. Empty entries are ignored.
func shouldSkipPath(path string, skipPaths []string) bool {
	for _, skip := range skipPaths {
		if skipPathMatches(path, skip) {
			return true
		}
	}

	return false
}

// skipPathMatches reports whether a single skip entry covers path.
func skipPathMatches(path, skip string) bool {
	if skip == "" {
		return false
	}

	if skip == "/" {
		return true
	}

	skip = strings.TrimSuffix(skip, "/")

	if path == skip {
		return true
	}

	return len(path) > len(skip) && path[len(skip)] == '/' && strings.HasPrefix(path, skip)
}
