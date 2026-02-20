package security

import (
	"regexp"
	"strings"
)

// Precompiled patterns for dangerous HTML content.
var (
	// Tag patterns (case insensitive).
	scriptTagPattern = regexp.MustCompile(`(?i)<script[^>]*>[\s\S]*?</script>`)
	iframeTagPattern = regexp.MustCompile(`(?i)<iframe[^>]*>[\s\S]*?</iframe>`)
	objectTagPattern = regexp.MustCompile(`(?i)<object[^>]*>[\s\S]*?</object>`)
	embedTagPattern  = regexp.MustCompile(`(?i)<embed[^>]*\/?>`)

	// Event handler attributes (on*="..." and on*='...').
	eventHandlerDoubleQuote = regexp.MustCompile(`(?i)\bon\w+\s*=\s*"[^"]*"`)
	eventHandlerSingleQuote = regexp.MustCompile(`(?i)\bon\w+\s*=\s*'[^']*'`)

	// javascript: URLs in href and src attributes.
	jsHrefDoubleQuote = regexp.MustCompile(`(?i)href\s*=\s*"javascript:[^"]*"`)
	jsHrefSingleQuote = regexp.MustCompile(`(?i)href\s*=\s*'javascript:[^']*'`)
	jsSrcDoubleQuote  = regexp.MustCompile(`(?i)src\s*=\s*"javascript:[^"]*"`)
	jsSrcSingleQuote  = regexp.MustCompile(`(?i)src\s*=\s*'javascript:[^']*'`)

	// All data: URLs in href and src attributes (used for selective filtering).
	// RE2 (Go's regexp) does not support negative lookaheads, so we match all
	// data: URLs and filter out image/ variants in the replacement function.
	dataHrefDoubleQuote = regexp.MustCompile(`(?i)href\s*=\s*"data:[^"]*"`)
	dataHrefSingleQuote = regexp.MustCompile(`(?i)href\s*=\s*'data:[^']*'`)
	dataSrcDoubleQuote  = regexp.MustCompile(`(?i)src\s*=\s*"data:[^"]*"`)
	dataSrcSingleQuote  = regexp.MustCompile(`(?i)src\s*=\s*'data:[^']*'`)

	// <form> tags with external action URLs.
	// Matches action attributes starting with http:// or https:// (external).
	formActionDoubleQuote = regexp.MustCompile(`(?i)(<form\b[^>]*)\baction\s*=\s*"https?://[^"]*"`)
	formActionSingleQuote = regexp.MustCompile(`(?i)(<form\b[^>]*)\baction\s*=\s*'https?://[^']*'`)
)

// Sanitizer strips potentially dangerous HTML from remote fragments.
type Sanitizer struct {
	allowDataImageURLs bool
}

// NewSanitizer creates a new HTML sanitizer.
// By default, data:image/ URLs are preserved while all other data: URLs are stripped.
func NewSanitizer() *Sanitizer {
	return &Sanitizer{
		allowDataImageURLs: true,
	}
}

// SanitizeFragment sanitizes an HTML fragment from a remote contributor.
// It removes script tags, event handlers, javascript: URLs, and other dangerous content.
// The sanitizer preserves data:image/ URLs by default while stripping all other
// data: scheme URLs.
func (s *Sanitizer) SanitizeFragment(html []byte) []byte {
	result := html

	// Strip dangerous tags entirely (including their content).
	result = scriptTagPattern.ReplaceAll(result, nil)
	result = iframeTagPattern.ReplaceAll(result, nil)
	result = objectTagPattern.ReplaceAll(result, nil)
	result = embedTagPattern.ReplaceAll(result, nil)

	// Strip event handler attributes (onclick, onload, onerror, etc.).
	result = eventHandlerDoubleQuote.ReplaceAll(result, nil)
	result = eventHandlerSingleQuote.ReplaceAll(result, nil)

	// Strip javascript: URLs from href and src attributes.
	result = jsHrefDoubleQuote.ReplaceAll(result, []byte(`href=""`))
	result = jsHrefSingleQuote.ReplaceAll(result, []byte(`href=''`))
	result = jsSrcDoubleQuote.ReplaceAll(result, []byte(`src=""`))
	result = jsSrcSingleQuote.ReplaceAll(result, []byte(`src=''`))

	// Strip data: URLs (except data:image/ when allowed).
	// RE2 does not support negative lookaheads, so we match all data: URLs and
	// use a replacement function to preserve data:image/ variants when allowed.
	stripDataURL := func(attr, emptyVal string) func([]byte) []byte {
		return func(match []byte) []byte {
			if s.allowDataImageURLs && strings.Contains(strings.ToLower(string(match)), "data:image/") {
				return match
			}

			return []byte(attr + emptyVal)
		}
	}

	result = dataHrefDoubleQuote.ReplaceAllFunc(result, stripDataURL(`href=`, `""`))
	result = dataHrefSingleQuote.ReplaceAllFunc(result, stripDataURL(`href=`, `''`))
	result = dataSrcDoubleQuote.ReplaceAllFunc(result, stripDataURL(`src=`, `""`))
	result = dataSrcSingleQuote.ReplaceAllFunc(result, stripDataURL(`src=`, `''`))

	// Strip external action attributes from <form> tags.
	// This removes the action attribute while preserving the rest of the form tag.
	result = formActionDoubleQuote.ReplaceAll(result, []byte(`$1`))
	result = formActionSingleQuote.ReplaceAll(result, []byte(`$1`))

	return result
}
