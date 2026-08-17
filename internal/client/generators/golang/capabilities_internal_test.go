package golang

import "testing"

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
