package pathspec

import "strings"

// Syntax selects the dialect Render emits.
type Syntax uint8

const (
	// SyntaxColon is the bunrouter and httprouter dialect: "/users/:id", with
	// a NAMED terminal wildcard, "/files/*filepath". bunrouter panics on an
	// unnamed wildcard, so the name is not optional here.
	SyntaxColon Syntax = iota

	// SyntaxBrace is the chi dialect: "/users/{id}", with a bare terminal
	// wildcard, "/files/*".
	SyntaxBrace

	// SyntaxOpenAPI is the OpenAPI path template: "/users/{id}". The wildcard
	// is rendered as a named parameter so the document can carry a parameter
	// object for it, which is what the old generator failed to do.
	SyntaxOpenAPI
)

// Render writes the pattern in the given dialect.
//
// Constraints are dropped by every dialect. Callers that care whether a
// backend can honor them must check Capabilities first; Render will not warn.
func (p Pattern) Render(s Syntax) string {
	if len(p.Segments) == 0 {
		return "/"
	}

	var b strings.Builder

	for _, seg := range p.Segments {
		b.WriteByte('/')

		switch seg.Kind {
		case KindStatic:
			b.WriteString(seg.Literal)

		case KindParam:
			if s == SyntaxColon {
				b.WriteByte(':')
				b.WriteString(seg.Name)
			} else {
				b.WriteByte('{')
				b.WriteString(seg.Name)
				b.WriteByte('}')
			}

		case KindWildcard:
			switch s {
			case SyntaxColon:
				b.WriteByte('*')
				b.WriteString(seg.Name)
			case SyntaxBrace:
				b.WriteByte('*')
			case SyntaxOpenAPI:
				b.WriteByte('{')
				b.WriteString(seg.Name)
				b.WriteByte('}')
			}
		}
	}

	return b.String()
}
