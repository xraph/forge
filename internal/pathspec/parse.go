package pathspec

import (
	"fmt"
	"strings"
)

// Parse converts a raw forge path into a Pattern.
//
// Trailing slashes are normalized away on non-root paths, so "/users/" and
// "/users" parse identically. This matches what the router already does to
// incoming request paths, and makes a route registered with a trailing slash
// reachable instead of dead.
func Parse(raw string) (Pattern, error) {
	if raw == "" {
		return Pattern{}, fmt.Errorf("pathspec: empty path")
	}

	if raw[0] != '/' {
		return Pattern{}, fmt.Errorf("pathspec: path %q must start with a slash", raw)
	}

	p := Pattern{Raw: raw}

	trimmed := raw
	if len(trimmed) > 1 && strings.HasSuffix(trimmed, "/") {
		trimmed = strings.TrimRight(trimmed, "/")
	}

	if trimmed == "" || trimmed == "/" {
		return p, nil
	}

	for _, part := range strings.Split(strings.TrimPrefix(trimmed, "/"), "/") {
		seg, err := parseSegment(part, raw)
		if err != nil {
			return Pattern{}, err
		}

		p.Segments = append(p.Segments, seg)

		if seg.Kind != KindStatic {
			p.Params = append(p.Params, seg.Name)
		}
	}

	return p, nil
}

// parseSegment classifies a single segment. raw is threaded through only so
// error messages can name the whole path the user actually wrote.
func parseSegment(part, raw string) (Segment, error) {
	if name, ok := strings.CutPrefix(part, ":"); ok {
		if err := validName(name, raw); err != nil {
			return Segment{}, err
		}

		return Segment{Kind: KindParam, Name: name}, nil
	}

	if inner, ok := strings.CutPrefix(part, "{"); ok {
		inner, closed := strings.CutSuffix(inner, "}")
		if !closed {
			return Segment{}, fmt.Errorf("pathspec: path %q has an unclosed brace in segment %q", raw, part)
		}

		name, spec, hasSpec := strings.Cut(inner, ":")

		if err := validName(name, raw); err != nil {
			return Segment{}, err
		}

		seg := Segment{Kind: KindParam, Name: name}

		if hasSpec {
			constraint, enum, err := parseConstraint(spec, raw)
			if err != nil {
				return Segment{}, err
			}

			seg.Constraint, seg.Enum = constraint, enum
		}

		return seg, nil
	}

	return Segment{Kind: KindStatic, Literal: part}, nil
}

// parseConstraint resolves the text after the colon in "{name:spec}".
func parseConstraint(spec, raw string) (Constraint, []string, error) {
	if body, ok := strings.CutPrefix(spec, "enum("); ok {
		body, closed := strings.CutSuffix(body, ")")
		if !closed {
			return ConstraintNone, nil, fmt.Errorf("pathspec: path %q has an unclosed enum(...)", raw)
		}

		values := strings.Split(body, "|")
		for _, v := range values {
			if v == "" {
				return ConstraintNone, nil, fmt.Errorf("pathspec: path %q has an empty enum value", raw)
			}
		}

		return ConstraintEnum, values, nil
	}

	constraint, ok := constraintByName(spec)
	if !ok {
		return ConstraintNone, nil, fmt.Errorf(
			"pathspec: path %q uses unknown constraint %q; the vocabulary is int, uint, uuid, alpha, alnum, enum(...)",
			raw, spec,
		)
	}

	return constraint, nil, nil
}

// validName enforces an identifier-shaped parameter name. Permitting slashes,
// braces or colons here would let a name round-trip into a different pattern
// when rendered.
func validName(name, raw string) error {
	if name == "" {
		return fmt.Errorf("pathspec: path %q has an unnamed parameter", raw)
	}

	for i := range len(name) {
		c := name[i]

		switch {
		case c == '_':
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return fmt.Errorf(
				"pathspec: path %q has invalid parameter name %q; names must match [A-Za-z_][A-Za-z0-9_]*",
				raw, name,
			)
		}
	}

	return nil
}
