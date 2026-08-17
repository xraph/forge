package golang

import (
	"strings"
	"testing"
)

// TestCapabilityIdent covers why this helper exists rather than a direct call
// to goFieldName: goFieldName does not split on ':', so "users:write" would
// come out "Userswrite" instead of "UsersWrite". See capabilityIdent's own
// doc comment for the full reasoning.
func TestCapabilityIdent(t *testing.T) {
	cases := map[string]string{
		"users:write": "UsersWrite",
		"read:users":  "ReadUsers",
		"admin":       "Admin",
		// Entirely punctuation: nothing for goFieldName to build an
		// identifier out of on either side of the split, so the result must
		// be the empty string -- callers skip this rather than emit a bare
		// "Permission" with no suffix.
		":::": "",
	}

	for in, want := range cases {
		if got := capabilityIdent(in); got != want {
			t.Errorf("capabilityIdent(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestResolveCapabilityConstsWarnsOnCollision is the regression test for the
// defect a count-only assertion cannot catch: "users:write" and "users-write"
// both derive to the identifier "UsersWrite" (capabilityIdent keeps only
// letters and digits), so before this fix the second was dropped with no
// warning at all. A caller could compile a client where PermissionUsersWrite
// silently meant "users-write" and never learn that "users:write" had gone
// missing.
//
// The two values collide only after sorting puts "users-write" ahead of
// "users:write" ('-' is 0x2D, ':' is 0x3A), which is also why the input here
// is passed pre-sorted -- resolveCapabilityConsts does not sort its input,
// CollectPermissions does that upstream, and this test's ordering has to
// match what a real caller would hand it for the "first wins" comment on
// resolveCapabilityConsts to be checked at all.
func TestResolveCapabilityConstsWarnsOnCollision(t *testing.T) {
	consts, warnings := resolveCapabilityConsts("permission", []string{"users-write", "users:write"})

	if len(consts) != 1 {
		t.Fatalf("consts = %v, want exactly 1", consts)
	}

	if consts[0].ident != "UsersWrite" || consts[0].value != "users-write" {
		t.Errorf("consts[0] = %+v, want {ident: UsersWrite, value: users-write}", consts[0])
	}

	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warnings)
	}

	for _, want := range []string{"users-write", "users:write", "UsersWrite"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning %q does not name %q", warnings[0], want)
		}
	}
}

// TestResolveCapabilityConstsWarnsOnEmptyIdent is the regression test for a
// value that collapses to no usable identifier at all ("" or all-punctuation
// values like ":::"): before this fix it was dropped silently, same as the
// collision case above.
func TestResolveCapabilityConstsWarnsOnEmptyIdent(t *testing.T) {
	consts, warnings := resolveCapabilityConsts("role", []string{":::"})

	if len(consts) != 0 {
		t.Fatalf("consts = %v, want none", consts)
	}

	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warnings)
	}

	if !strings.Contains(warnings[0], ":::") {
		t.Errorf("warning %q does not name the offending value", warnings[0])
	}
}
