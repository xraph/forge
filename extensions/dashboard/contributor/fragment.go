package contributor

import (
	"bytes"
	"regexp"
)

// Precompiled patterns for HTML document wrapper extraction.
var (
	doctypePattern = regexp.MustCompile(`(?i)<!DOCTYPE[^>]*>`)
	htmlOpenTag    = regexp.MustCompile(`(?i)<html[^>]*>`)
	htmlCloseTag   = regexp.MustCompile(`(?i)</html\s*>`)
	headPattern    = regexp.MustCompile(`(?is)<head[^>]*>.*?</head\s*>`)
	bodyOpenTag    = regexp.MustCompile(`(?i)<body[^>]*>`)
	bodyCloseTag   = regexp.MustCompile(`(?i)</body\s*>`)
)

// ExtractBodyFragment extracts the inner content of the <body> element from
// a full HTML document, stripping <!DOCTYPE>, <html>, <head>, and <body>
// wrappers. If the input does not contain a <body> tag, it is returned as-is
// (assumed to already be a fragment).
//
// This is used by EmbeddedContributor to convert framework static export
// output (Astro, Next.js) into dashboard-embeddable HTML fragments.
func ExtractBodyFragment(html []byte) []byte {
	// Fast path: if there's no <body> tag, assume it's already a fragment.
	if !bodyOpenTag.Match(html) {
		return bytes.TrimSpace(html)
	}

	// Find the <body> opening tag and extract everything after it.
	bodyOpenLoc := bodyOpenTag.FindIndex(html)
	if bodyOpenLoc == nil {
		return bytes.TrimSpace(html)
	}

	content := html[bodyOpenLoc[1]:]

	// Find the </body> closing tag and extract everything before it.
	bodyCloseLoc := bodyCloseTag.FindIndex(content)
	if bodyCloseLoc != nil {
		content = content[:bodyCloseLoc[0]]
	}

	return bytes.TrimSpace(content)
}

// IsFragment returns true if the HTML content appears to be a fragment
// (no <html> or <body> wrapper), false if it's a full document.
func IsFragment(html []byte) bool {
	return !htmlOpenTag.Match(html) && !bodyOpenTag.Match(html)
}

// StripDocumentWrapper removes <!DOCTYPE>, <html>, </html>, <head>...</head>,
// <body>, and </body> tags but preserves all other content. Unlike
// ExtractBodyFragment, this preserves content outside <body> (e.g., inline
// styles in <head> are lost, but stray text between </head> and <body> is kept).
// Prefer ExtractBodyFragment for typical use cases.
func StripDocumentWrapper(html []byte) []byte {
	result := html
	result = doctypePattern.ReplaceAll(result, nil)
	result = headPattern.ReplaceAll(result, nil)
	result = htmlOpenTag.ReplaceAll(result, nil)
	result = htmlCloseTag.ReplaceAll(result, nil)
	result = bodyOpenTag.ReplaceAll(result, nil)
	result = bodyCloseTag.ReplaceAll(result, nil)

	return bytes.TrimSpace(result)
}
