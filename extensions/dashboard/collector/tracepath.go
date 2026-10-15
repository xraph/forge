package collector

import (
	"strings"
	"unicode/utf8"
)

// MaxAttrValueLen bounds a stored attribute value, header value or span name.
// They come from callers and live for the whole retention window.
const MaxAttrValueLen = 256

var staticSuffixes = []string{".css", ".js", ".ico", ".png", ".jpg", ".svg", ".woff2", ".woff"}

// SkipTracePath reports whether a request path is left out of the trace
// viewer: the dashboard's own bridge calls, SSE streams and static files
// anywhere. They are frequent and say nothing about the app.
func SkipTracePath(path, basePath string) bool {
	if path == "" {
		return false
	}

	if strings.HasPrefix(path, basePath+"/bridge/") ||
		strings.Contains(path, "/static/") ||
		strings.Contains(path, "/sse") {
		return true
	}

	for _, suffix := range staticSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}

	return false
}

// Truncate shortens s to at most limit bytes, ending on a rune boundary and
// marking the cut. Values already within the limit are returned unchanged.
func Truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	const marker = "..."
	if limit <= len(marker) {
		if limit <= 0 {
			return ""
		}

		return marker[:limit]
	}

	cut := limit - len(marker)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut] + marker
}
