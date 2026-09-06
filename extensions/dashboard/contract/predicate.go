package contract

import (
	"strings"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
)

// Allow evaluates the boolean predicate against a UserInfo. The wardenResult
// argument is the optional second-pass Warden decision; pass nil to skip.
// An empty predicate (no all/any/not) always allows.
func (p *Predicate) Allow(user *dashauth.UserInfo, wardenResult *Decision) bool {
	if p == nil {
		return true
	}
	if len(p.All) == 0 && len(p.Any) == 0 && len(p.Not) == 0 && wardenResult == nil {
		// truly empty predicate; warden absence handled by caller's evaluation order
		return true
	}
	for _, tok := range p.All {
		if !match(tok, user) {
			return false
		}
	}
	if len(p.Any) > 0 {
		ok := false
		for _, tok := range p.Any {
			if match(tok, user) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, tok := range p.Not {
		if match(tok, user) {
			return false
		}
	}
	if wardenResult != nil && !wardenResult.Allow {
		return false
	}
	return true
}

// match parses one token (role:X, scope:X, claim:K=V) and tests it against user.
func match(token string, user *dashauth.UserInfo) bool {
	if user == nil {
		return false
	}
	kind, rest, ok := strings.Cut(token, ":")
	if !ok {
		return false
	}
	switch kind {
	case "role":
		return contains(user.Roles, rest)
	case "scope":
		return contains(user.Scopes, rest)
	case "claim":
		key, value, ok := strings.Cut(rest, "=")
		if !ok {
			return false
		}
		got, ok := user.Claims[key]
		if !ok {
			return false
		}
		return toString(got) == value
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "" // claims that aren't strings can't be matched by claim:K=V
}
