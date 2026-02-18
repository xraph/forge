package security

import (
	"regexp"
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

	// data: URLs (non-image) in href and src attributes.
	// Matches data: URLs that are NOT data:image/ variants.
	dataHrefDoubleQuote = regexp.MustCompile(`(?i)href\s*=\s*"data:(?!image/)[^"]*"`)
	dataHrefSingleQuote = regexp.MustCompile(`(?i)href\s*=\s*'data:(?!image/)[^']*'`)
	dataSrcDoubleQuote  = regexp.MustCompile(`(?i)src\s*=\s*"data:(?!image/)[^"]*"`)
	dataSrcSingleQuote  = regexp.MustCompile(`(?i)src\s*=\s*'data:(?!image/)[^']*'`)

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
	if s.allowDataImageURLs {
		// Only strip non-image data: URLs. The regex uses a negative lookahead
		// to preserve data:image/ variants.
		result = dataHrefDoubleQuote.ReplaceAll(result, []byte(`href=""`))
		result = dataHrefSingleQuote.ReplaceAll(result, []byte(`href=''`))
		result = dataSrcDoubleQuote.ReplaceAll(result, []byte(`src=""`))
		result = dataSrcSingleQuote.ReplaceAll(result, []byte(`src=''`))
	} else {
		// Strip all data: URLs regardless of subtype.
		allDataHrefDouble := regexp.MustCompile(`(?i)href\s*=\s*"data:[^"]*"`)
		allDataHrefSingle := regexp.MustCompile(`(?i)href\s*=\s*'data:[^']*'`)
		allDataSrcDouble := regexp.MustCompile(`(?i)src\s*=\s*"data:[^"]*"`)
		allDataSrcSingle := regexp.MustCompile(`(?i)src\s*=\s*'data:[^']*'`)

		result = allDataHrefDouble.ReplaceAll(result, []byte(`href=""`))
		result = allDataHrefSingle.ReplaceAll(result, []byte(`href=''`))
		result = allDataSrcDouble.ReplaceAll(result, []byte(`src=""`))
		result = allDataSrcSingle.ReplaceAll(result, []byte(`src=''`))
	}

	// Strip external action attributes from <form> tags.
	// This removes the action attribute while preserving the rest of the form tag.
	result = formActionDoubleQuote.ReplaceAll(result, []byte(`$1`))
	result = formActionSingleQuote.ReplaceAll(result, []byte(`$1`))

	return result
}
