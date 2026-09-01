// Package pathspec parses and renders forge route path patterns.
//
// It is the single source of truth for path syntax. Adapters and the OpenAPI
// generator consume a parsed Pattern instead of re-deriving "what is a
// parameter" from a raw string, which is what four separate implementations
// used to do, disagreeing with each other about wildcards.
//
// This package is a leaf. It must not import anything else in this repository.
package pathspec

// Kind classifies one path segment.
type Kind uint8

// Constraint restricts which segment values a parameter matches. The type is
// declared here so Segment can name it; the vocabulary, ordering and match
// predicates live in constraint.go.
type Constraint uint8

const (
	// KindStatic is a literal segment that matches only itself.
	KindStatic Kind = iota
	// KindParam matches exactly one segment and captures it.
	KindParam
	// KindWildcard matches the remainder of the path and captures it. It can
	// only appear as the final segment.
	KindWildcard
)

// Segment is one "/"-delimited piece of a pattern.
type Segment struct {
	Kind Kind

	// Literal is set for KindStatic only.
	Literal string

	// Name is set for KindParam and KindWildcard. An unnamed wildcard is
	// given DefaultWildcardName at parse time, so this is never empty for
	// those kinds.
	Name string

	// Constraint is set for KindParam only. ConstraintNone means the
	// parameter matches any non-empty segment.
	Constraint Constraint

	// Enum holds the permitted values for ConstraintEnum, and is nil
	// otherwise.
	Enum []string
}

// Pattern is a parsed route path.
type Pattern struct {
	// Raw is the path exactly as registered, kept for diagnostics. It is not
	// normalized, so it may differ from Render output.
	Raw string

	// Segments is empty for the root path "/".
	Segments []Segment

	// Params holds parameter and wildcard names in the order they appear.
	// The matcher relies on this order to bind captured values to names.
	Params []string
}

// DefaultWildcardName is the name given to an unnamed wildcard.
//
// The value matters beyond aesthetics. internal/router/bunrouter.go maps a
// param literally named "filepath" onto the "*" lookup key, and
// extras/httprouter.go mounts sub-handlers on "*filepath". Changing it breaks
// wildcard parameter lookup in both.
const DefaultWildcardName = "filepath"
