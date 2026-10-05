// Package dart generates a Dart client package from an API specification.
package dart

// The language-neutral helpers (casing.go, opkeys.go, fieldname.go,
// codectable.go, tagrename.go, manifest.go) are copies of the TypeScript
// generator's, built by portgen. Never edit them by hand: change the
// TypeScript original or portgen, then regenerate. port_test.go fails when a
// committed copy differs from what portgen produces now.
//
//go:generate go run gen_ports.go

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// dartReserved are Dart's reserved words, which no identifier may be.
var dartReserved = map[string]bool{
	"assert": true, "break": true, "case": true, "catch": true, "class": true, "const": true,
	"continue": true, "default": true, "do": true, "else": true, "enum": true, "extends": true,
	"false": true, "final": true, "finally": true, "for": true, "if": true, "in": true, "is": true,
	"new": true, "null": true, "rethrow": true, "return": true, "super": true, "switch": true,
	"this": true, "throw": true, "true": true, "try": true, "var": true, "void": true,
	"while": true, "with": true, "await": true, "yield": true,
}

// dartBuiltIns are Dart's built-in identifiers: legal as member names, never
// as type names.
var dartBuiltIns = map[string]bool{
	"abstract": true, "as": true, "covariant": true, "deferred": true, "dynamic": true,
	"export": true, "extension": true, "external": true, "factory": true, "Function": true,
	"get": true, "implements": true, "import": true, "interface": true, "late": true,
	"library": true, "mixin": true, "operator": true, "part": true, "required": true,
	"set": true, "static": true, "typedef": true,
}

// Members a generated identifier must not take, per kind of declaration.
// Every Dart object already has the first four; models add their codec
// members, enums the members Dart gives every enum value, and argument
// classes their tag-context method. Models and argument classes also keep
// clear of Dart's lowercase built-in type names: a field declared
// `final int int;` hides the type from every later declaration in the class.
var (
	modelReserved = map[string]bool{
		"hashCode": true, "runtimeType": true, "toString": true, "noSuchMethod": true,
		"copyWith": true, "toClient": true, "fromClient": true,
		"int": true, "double": true, "bool": true, "num": true,
	}
	enumReserved = map[string]bool{
		"hashCode": true, "runtimeType": true, "toString": true, "noSuchMethod": true,
		"toClient": true, "fromClient": true, "index": true, "name": true, "values": true, "wire": true,
		"isKnown": true, "known": true,
	}
	argsReserved = map[string]bool{
		"hashCode": true, "runtimeType": true, "toString": true, "noSuchMethod": true, "toTagContext": true,
		"int": true, "double": true, "bool": true, "num": true,
		// The REST method's own named parameters; a parameter is named
		// the same in its Args class and its RestClient method.
		"cancel": true, "maxAttempts": true,
	}
)

// sanitize keeps the characters a Dart identifier may hold. Underscores are
// dropped too: a leading one makes a name library-private, and the casing
// functions have already used them as word breaks.
func sanitize(s string) string {
	var b strings.Builder

	for _, r := range s {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '$') {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// typeIdent renders an UpperCamel type name from any schema or operation name.
func typeIdent(raw string) string {
	name := sanitize(toPascal(raw))

	switch {
	case name == "":
		return "Model"
	case name[0] >= '0' && name[0] <= '9':
		return "T" + name
	case dartReserved[name] || dartBuiltIns[name]:
		return name + "$"
	}

	return name
}

// memberIdent renders a lowerCamel member name, escaping a keyword or a name
// in reserved with a trailing dollar sign.
func memberIdent(raw string, reserved map[string]bool) string {
	name := sanitize(toCamel(raw))

	switch {
	case name == "":
		return "value"
	case name[0] >= '0' && name[0] <= '9':
		name = "v" + name
	}

	if dartReserved[name] || reserved[name] {
		return name + "$"
	}

	return name
}

// fileStem renders a snake_case file name, without its extension.
func fileStem(raw string) string {
	var b strings.Builder

	for _, r := range toSnake(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(unicode.ToLower(r))
		}
	}

	stem := strings.Trim(b.String(), "_")

	switch {
	case stem == "":
		return "model"
	case stem[0] >= '0' && stem[0] <= '9':
		return "t_" + stem
	}

	return stem
}

// uniqueNames renders each input with render and suffixes repeats with 2, 3,
// and so on. taken seeds names already in use and is updated. fold compares
// case-insensitively, for file names on case-insensitive file systems.
func uniqueNames(in []string, render func(string) string, taken map[string]bool, fold bool) []string {
	out := make([]string, len(in))

	key := func(s string) string {
		if fold {
			return strings.ToLower(s)
		}

		return s
	}

	for i, s := range in {
		base := render(s)

		name := base
		for n := 2; taken[key(name)]; n++ {
			name = base + strconv.Itoa(n)
		}

		taken[key(name)] = true
		out[i] = name
	}

	return out
}

// dartString renders a single-quoted Dart string literal. `$` is escaped
// because Dart interpolates it.
func dartString(s string) string {
	var b strings.Builder

	b.WriteByte('\'')

	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '$':
			b.WriteString(`\$`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u{%x}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}

	b.WriteByte('\'')

	return b.String()
}

// dartStringList renders a const-compatible list of string literals.
func dartStringList(items []string) string {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = dartString(item)
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

// docComment renders text as `///` lines at indent, or fallback when text is
// empty. Every generated public member carries one: the gate test runs the
// analyzer with public_member_api_docs on.
func docComment(text, fallback, indent string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		text = fallback
	}

	var b strings.Builder

	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			b.WriteString(indent + "///\n")

			continue
		}

		b.WriteString(indent + "/// " + line + "\n")
	}

	return b.String()
}

// sortedKeys returns the keys of m in ascending order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// contains reports whether slice holds item.
func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}
