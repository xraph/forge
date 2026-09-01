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

	return Segment{Kind: KindStatic, Literal: part}, nil
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
