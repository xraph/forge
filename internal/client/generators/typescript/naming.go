package typescript

import (
	"strings"
	"unicode"
)

// isAllUpper reports whether w contains at least one uppercase letter and no
// lowercase letters. Such a word is an acronym (USER, HTTP, ID) rather than a
// normally-cased word (User, Http, Id), and must be normalised as a whole —
// touching only its first rune, as lowerFirst/upperFirst otherwise do, would
// leave the rest of the acronym shouting (uSERID, hTTPStatus).
func isAllUpper(w string) bool {
	hasUpper := false

	for _, r := range w {
		if unicode.IsLower(r) {
			return false
		}

		if unicode.IsUpper(r) {
			hasUpper = true
		}
	}

	return hasUpper
}

// lowerFirst returns w lowercased. If w is an all-caps acronym (isAllUpper),
// the whole word is lowercased ("USER" -> "user"); otherwise only the first
// rune is, leaving the rest untouched ("User" -> "user"). Operates on runes,
// not bytes, so multi-byte leading characters are not corrupted.
func lowerFirst(w string) string {
	if isAllUpper(w) {
		return strings.ToLower(w)
	}

	r := []rune(w)
	if len(r) == 0 {
		return w
	}

	r[0] = unicode.ToLower(r[0])

	return string(r)
}

// upperFirst returns w title-cased: first rune uppercased. If w is an
// all-caps acronym (isAllUpper), the rest of the word is lowercased first
// ("ID" -> "Id"); otherwise only the first rune is touched, leaving the rest
// as-is ("user" -> "User", "userId" -> "UserId"). Operates on runes, not
// bytes, so multi-byte leading characters are not corrupted.
func upperFirst(w string) string {
	if isAllUpper(w) {
		w = strings.ToLower(w)
	}

	r := []rune(w)
	if len(r) == 0 {
		return w
	}

	r[0] = unicode.ToUpper(r[0])

	return string(r)
}

// splitWords breaks name on separators, on lower-to-upper boundaries (so an
// already-camelCase name round-trips instead of being flattened), and on the
// trailing edge of a run of capitals that is followed by a lowercase letter
// (so an acronym-then-word name like "HTTPStatus" splits into "HTTP" +
// "Status" rather than being read as one word). Only non-empty words are
// ever appended, so callers may safely pass words[i] to lowerFirst /
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

	isUpper := func(r rune) bool { return r >= 'A' && r <= 'Z' }
	isLower := func(r rune) bool { return r >= 'a' && r <= 'z' }

	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.':
			flush()
		case i > 0 && isUpper(r) && isLower(runes[i-1]):
			// lower-to-upper boundary: e.g. "user|Id".
			flush()
			cur.WriteRune(r)
		case i > 0 && isUpper(r) && isUpper(runes[i-1]) && i+1 < len(runes) && isLower(runes[i+1]):
			// trailing edge of a capital run, followed by a lowercase letter:
			// e.g. "HTTP|Status" splits before the "S", not before the "P".
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

// toSnake converts name to snake_case, reusing splitWords so all three
// converters agree on where word boundaries fall -- including the
// acronym-then-word split splitWords applies (e.g. "HTTPStatus" splits into
// "HTTP" + "Status" for toCamel, toPascal, and toSnake alike). Unlike
// lowerFirst/upperFirst, snake_case has no "first word" special case: every
// word is simply lowercased as a whole, since case only distinguishes
// acronyms in camel/Pascal, not in snake_case.
func toSnake(name string) string {
	words := splitWords(name)
	if len(words) == 0 {
		return name
	}

	lowered := make([]string, len(words))
	for i, w := range words {
		lowered[i] = strings.ToLower(w)
	}

	return strings.Join(lowered, "_")
}
