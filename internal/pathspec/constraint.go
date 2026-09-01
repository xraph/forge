package pathspec

// The Constraint vocabulary is closed by design: there is no regex form and no
// registration hook. A constraint runs on the matcher's hot path, and the
// OpenAPI generator has to infer a schema from it, neither of which survives
// arbitrary user predicates.
//
// A constraint is not validation. It affects matching, so "/users/{id:int}"
// failing to match falls through to "/users/me". Validation affects
// responding, and produces a 400 on a route that already matched.
//
// The type itself is declared in pathspec.go, because Segment names it.
const (
	// ConstraintNone matches any non-empty segment.
	ConstraintNone Constraint = iota
	ConstraintAlnum
	ConstraintAlpha
	ConstraintInt
	ConstraintUint
	ConstraintUUID
	// ConstraintEnum matches a fixed list carried on Segment.Enum.
	ConstraintEnum
)

// constraintNames maps path syntax onto constraints. ConstraintEnum is absent
// because it is spelled "enum(a|b|c)" and parsed separately.
var constraintNames = map[string]Constraint{
	"alnum": ConstraintAlnum,
	"alpha": ConstraintAlpha,
	"int":   ConstraintInt,
	"uint":  ConstraintUint,
	"uuid":  ConstraintUUID,
}

// constraintByName resolves a constraint from its path-syntax name.
func constraintByName(name string) (Constraint, bool) {
	c, ok := constraintNames[name]

	return c, ok
}

// String returns the name used in path syntax, or "" for ConstraintNone.
func (c Constraint) String() string {
	switch c {
	case ConstraintAlnum:
		return "alnum"
	case ConstraintAlpha:
		return "alpha"
	case ConstraintInt:
		return "int"
	case ConstraintUint:
		return "uint"
	case ConstraintUUID:
		return "uuid"
	case ConstraintEnum:
		return "enum"
	case ConstraintNone:
		return ""
	}

	return ""
}

// Rank orders constraints from most specific to least.
//
// The matcher tries parameter edges in descending rank, so "/users/{id:uuid}"
// is attempted before "/users/{name:alpha}". Two constraints of equal rank on
// the same segment are ambiguous, and the matcher reports that as a conflict
// at registration.
func (c Constraint) Rank() int {
	switch c {
	case ConstraintEnum:
		return 5
	case ConstraintUUID:
		return 4
	case ConstraintInt, ConstraintUint:
		return 3
	case ConstraintAlpha, ConstraintAlnum:
		return 2
	case ConstraintNone:
		return 0
	}

	return 0
}

// Match reports whether value satisfies the constraint. enum is consulted only
// for ConstraintEnum and is otherwise ignored.
func (c Constraint) Match(value string, enum []string) bool {
	if value == "" {
		return false
	}

	switch c {
	case ConstraintNone:
		return true

	case ConstraintInt:
		digits := value
		if digits[0] == '-' || digits[0] == '+' {
			digits = digits[1:]
		}

		return digits != "" && allBytes(digits, isDigit)

	case ConstraintUint:
		return allBytes(value, isDigit)

	case ConstraintAlpha:
		return allBytes(value, isAlpha)

	case ConstraintAlnum:
		return allBytes(value, func(b byte) bool { return isAlpha(b) || isDigit(b) })

	case ConstraintUUID:
		return isUUID(value)

	case ConstraintEnum:
		for _, candidate := range enum {
			if value == candidate {
				return true
			}
		}

		return false
	}

	return false
}

func allBytes(s string, pred func(byte) bool) bool {
	for i := range len(s) {
		if !pred(s[i]) {
			return false
		}
	}

	return true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

func isHex(b byte) bool { return isDigit(b) || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F') }

// uuidGroups is the canonical 8-4-4-4-12 hyphenated layout. The unhyphenated
// form is deliberately rejected: accepting both would make "{id:uuid}" and
// "{id:alnum}" overlap, and the matcher's rank ordering assumes they do not.
var uuidGroups = [...]int{8, 4, 4, 4, 12}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}

	pos := 0

	for i, size := range uuidGroups {
		if i > 0 {
			if s[pos] != '-' {
				return false
			}

			pos++
		}

		for j := 0; j < size; j++ {
			if !isHex(s[pos+j]) {
				return false
			}
		}

		pos += size
	}

	return true
}
