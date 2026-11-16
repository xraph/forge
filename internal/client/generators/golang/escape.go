package golang

import (
	"go/token"
	"strconv"
	"strings"
	"unicode"
)

// Text from the specification is written by whoever authored it, and a client
// is often generated from a document the user did not write. The helpers here
// put that text into generated Go so it can only ever be what its position
// says it is: a comment stays a comment, a string stays a string, a name stays
// a name. Before them, a description containing "\nfunc init() {...}" became a
// real init function that ran when the generated package was imported.

// normalizeNewlines turns "\r\n" and a bare "\r" into "\n" so text splits into
// the lines a reader would see.
func normalizeNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// goComment renders text as a block of line comments, every line prefixed with
// "// " and ending in a newline. A blank line inside the text becomes a bare
// "//" so paragraphs survive.
func goComment(text string) string {
	var buf strings.Builder

	for line := range strings.SplitSeq(normalizeNewlines(text), "\n") {
		if line == "" {
			buf.WriteString("//\n")

			continue
		}

		buf.WriteString("// ")
		buf.WriteString(line)
		buf.WriteString("\n")
	}

	return buf.String()
}

// goInlineComment flattens text onto one line, for a comment that shares its
// line with code or with a fixed prefix.
func goInlineComment(text string) string {
	return strings.ReplaceAll(normalizeNewlines(text), "\n", " ")
}

// goIdent keeps the characters a Go identifier may hold and prefixes an "X"
// when what is left starts with a digit. It returns "" when nothing usable
// remains, so the caller picks its own fallback.
func goIdent(s string) string {
	out := strings.Map(func(r rune) rune {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}

		return -1
	}, s)

	if out != "" && unicode.IsDigit([]rune(out)[0]) {
		out = "X" + out
	}

	return out
}

// methodScopeNames are the names a generated REST method already uses in its
// own scope: the receiver, its locals, and the packages its body calls into.
// A parameter with one of these names would shadow it or be redeclared, and
// the method would not compile.
var methodScopeNames = map[string]bool{
	"c": true, "ctx": true, "path": true, "q": true, "req": true,
	"result": true, "err": true, "url": true, "fmt": true, "context": true,
}

// goParamIdent is goIdent for a function parameter: a keyword, or a name the
// method body already uses, gets a "Param" suffix, and a name with nothing
// usable left becomes "param".
func goParamIdent(s string) string {
	out := goIdent(s)

	switch {
	case out == "":
		return "param"
	case token.IsKeyword(out), methodScopeNames[out]:
		return out + "Param"
	}

	return out
}

// isGoTypeName reports whether a schema name can be printed as a Go type name
// as it stands.
func isGoTypeName(name string) bool {
	return token.IsIdentifier(name)
}

// goJSONTag renders a struct tag literal for one JSON field. The tag value is
// quoted with strconv.Quote, which is exactly how reflect.StructTag reads it
// back, and the whole tag is written as a raw string unless it contains a
// backtick, in which case it is written as an interpreted string instead. A
// property name holding `"` or "`" could otherwise close the tag and add a
// field of its own.
func goJSONTag(value string) string {
	tag := "json:" + strconv.Quote(value)
	if strings.Contains(tag, "`") {
		return strconv.Quote(tag)
	}

	return "`" + tag + "`"
}
