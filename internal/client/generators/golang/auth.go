package golang

import (
	"strings"
	"unicode"
)

// goFieldName turns a securityScheme key into an exported Go field name.
//
// Deliberately not `toPascalCase`, which lowercases everything after the first
// rune of each part and so turns `bearerAuth` into `Bearerauth`. camelCase is
// how OpenAPI documents conventionally key schemes, so reusing it would mangle
// nearly every real specification. `toPascalCase` is left alone because it has
// other callers whose generated identifiers would change, and that is not this
// change's business.
//
// Returns "" when nothing usable survives, which the caller reports as a spec
// problem rather than emitting a nameless field.
func goFieldName(key string) string {
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '/' || r == '.'
	})

	var out strings.Builder

	for _, part := range parts {
		// Keep the interior as written: `bearerAuth` -> `BearerAuth`.
		runes := []rune(part)
		out.WriteRune(unicode.ToUpper(runes[0]))

		for _, r := range runes[1:] {
			out.WriteRune(r)
		}
	}

	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}

		return -1
	}, out.String())

	if cleaned == "" {
		return ""
	}

	// A leading digit is not a Go identifier. Prefixing beats rejecting: `2fa`
	// is a plausible scheme key and `X2fa` is a usable field.
	if unicode.IsDigit(rune(cleaned[0])) {
		return "X" + cleaned
	}

	return strings.ToUpper(cleaned[:1]) + cleaned[1:]
}
