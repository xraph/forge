package typescript

import (
	"strings"
	"unicode"
)

// lowerFirst returns w with its first rune lowercased, leaving the rest
// untouched. Operates on runes, not bytes, so multi-byte leading characters
// are not corrupted.
func lowerFirst(w string) string {
	r := []rune(w)
	if len(r) == 0 {
		return w
	}

	r[0] = unicode.ToLower(r[0])

	return string(r)
}

// upperFirst returns w with its first rune uppercased, leaving the rest
// untouched. Operates on runes, not bytes, so multi-byte leading characters
// are not corrupted.
func upperFirst(w string) string {
	r := []rune(w)
	if len(r) == 0 {
		return w
	}

	r[0] = unicode.ToUpper(r[0])

	return string(r)
}

// splitWords breaks name on separators and on lower-to-upper boundaries, so an
// already-camelCase name round-trips instead of being flattened. Only non-empty
// words are ever appended, so callers may safely pass words[i] to lowerFirst /
// upperFirst without an emptiness check.
func splitWords(name string) []string {
	var (
		words []string
		cur   strings.Builder
	)

	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}

	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.':
			flush()
		case i > 0 && r >= 'A' && r <= 'Z' && runes[i-1] >= 'a' && runes[i-1] <= 'z':
			flush()
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
	}

	flush()

	return words
}

// toCamel converts name to camelCase, preserving interior capitalisation.
func toCamel(name string) string {
	words := splitWords(name)
	if len(words) == 0 {
		return name
	}

	var out strings.Builder

	out.WriteString(lowerFirst(words[0]))

	for _, w := range words[1:] {
		out.WriteString(upperFirst(w))
	}

	return out.String()
}

// toPascal converts name to PascalCase, preserving interior capitalisation.
func toPascal(name string) string {
	words := splitWords(name)

	var out strings.Builder

	for _, w := range words {
		out.WriteString(upperFirst(w))
	}

	return out.String()
}
