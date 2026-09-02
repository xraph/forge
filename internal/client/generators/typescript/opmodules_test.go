package typescript

import (
	"strings"
	"testing"
)

func TestOpFileStemKeepsDottedKeysResolvable(t *testing.T) {
	for key, want := range map[string]string{
		"agents.list":           "agents.list",
		"getManifest":           "getManifest",
		"schema.datasets.list":  "schema.datasets.list",
		"list-orders":           "list-orders",
		"weird/key with spaces": "weird_key_with_spaces",
		".internal":             "_internal",
		"":                      "operation",
	} {
		if got := opFileStem(key); got != want {
			t.Errorf("opFileStem(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestOpConstNameIsAlwaysAnIdentifier(t *testing.T) {
	for key, want := range map[string]string{
		"agents.list": "op_agents_list",
		"getManifest": "op_getManifest",
		"list-orders": "op_list_orders",
		"2fa.verify":  "op_2fa_verify",
	} {
		if got := opConstName(key); got != want {
			t.Errorf("opConstName(%q) = %q, want %q", key, got, want)
		}
	}
}

// Sanitising maps distinct keys onto one identifier; the suffix is what keeps
// the emitted module from declaring the same const twice.
func TestOpConstNamesUniquifySanitisedCollisions(t *testing.T) {
	got := newOpModuleNaming([]string{"a.b", "a_b", "a-b"}).consts

	seen := map[string]bool{}
	for _, name := range got {
		if seen[name] {
			t.Fatalf("duplicate const name %q in %v", name, got)
		}

		seen[name] = true
	}
}

// A case-insensitive filesystem would resolve both of these to one file, and
// the second write would silently replace the first.
func TestOpFileStemsUniquifyCaseInsensitively(t *testing.T) {
	got := newOpModuleNaming([]string{"getUser", "getuser"}).files

	if got[0] != "getUser" {
		t.Errorf("first arrival should keep its casing, got %q", got[0])
	}

	if strings.EqualFold(got[0], got[1]) {
		t.Fatalf("stems %q and %q collide on a case-insensitive filesystem", got[0], got[1])
	}
}

// Identifiers are case-SENSITIVE in TypeScript, so the same pair must not be
// suffixed there -- that would be churn for a collision that does not exist.
func TestOpConstNamesDoNotFoldCase(t *testing.T) {
	got := newOpModuleNaming([]string{"getUser", "getuser"}).consts

	if got[0] != "op_getUser" || got[1] != "op_getuser" {
		t.Fatalf("expected both casings kept unsuffixed, got %v", got)
	}
}

// An operation key is a dotted path and a filename is a dotted name, so a key
// whose last segment is a tooling convention emits a file that tooling claims.
//
// /hooks/{id}/test derives hooks.test, which lands as ops/hooks.test.ts. Every
// test runner globs that, finds no tests, and fails the suite. This is not
// hypothetical: it turned three files red in the consumer the first time the
// split was generated there.
func TestOpFileStemDoesNotEndInAToolingSuffix(t *testing.T) {
	for key, want := range map[string]string{
		"hooks.test":                  "hooks_test",
		"automations.test":            "automations_test",
		"portal.admin.databases.test": "portal.admin.databases_test",
		"schema.spec":                 "schema_spec",
		// An ambient declaration file is the dangerous one: `export const` in
		// a .d.ts declares a value that does not exist at runtime.
		"types.d": "types_d",
		// Only the last segment counts, because these are suffix globs.
		"test.hooks": "test.hooks",
		// A key that is only the reserved word matches no glob on its own.
		"test": "test",
	} {
		if got := opFileStem(key); got != want {
			t.Errorf("opFileStem(%q) = %q, want %q", key, got, want)
		}
	}
}

// The guard must not disturb the ordinary case.
func TestOpFileStemLeavesNormalKeysAlone(t *testing.T) {
	for _, key := range []string{"agents.list", "schema.datasets.list", "getManifest", "hooks.retry"} {
		if got := opFileStem(key); got != key {
			t.Errorf("opFileStem(%q) = %q, want it unchanged", key, got)
		}
	}
}
