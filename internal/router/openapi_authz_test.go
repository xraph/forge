package router

import (
	"reflect"
	"testing"
)

func TestProcessAuthzWritesExtension(t *testing.T) {
	op := &Operation{}

	(&openAPIGenerator{}).processAuthzRequirements(op, map[string]any{
		"auth.roles":       []string{"editor", "admin", "admin"},
		"auth.permissions": []string{"users:write"},
	})

	ext, ok := op.Extensions["x-forge-authz"].(map[string]any)
	if !ok {
		t.Fatalf("x-forge-authz missing: %#v", op.Extensions)
	}

	// Sorted and deduplicated, because generated capability files are
	// byte-diffed by CI and route metadata order is not stable.
	if got := ext["roles"]; !reflect.DeepEqual(got, []string{"admin", "editor"}) {
		t.Errorf("roles = %#v, want [admin editor]", got)
	}

	if got := ext["permissions"]; !reflect.DeepEqual(got, []string{"users:write"}) {
		t.Errorf("permissions = %#v, want [users:write]", got)
	}
}

func TestProcessAuthzOmitsEmptyKeys(t *testing.T) {
	op := &Operation{}

	(&openAPIGenerator{}).processAuthzRequirements(op, map[string]any{
		"auth.roles": []string{"admin"},
	})

	ext := op.Extensions["x-forge-authz"].(map[string]any)

	if _, present := ext["permissions"]; present {
		t.Error("permissions key emitted with nothing in it")
	}
}

func TestProcessAuthzOmitsExtensionEntirely(t *testing.T) {
	op := &Operation{}

	(&openAPIGenerator{}).processAuthzRequirements(op, map[string]any{
		"auth.providers": []string{"jwt"},
	})

	if _, present := op.Extensions["x-forge-authz"]; present {
		t.Errorf("x-forge-authz emitted for a route declaring no authz: %#v", op.Extensions)
	}
}

// TestSortedUniqueStrings is T7: sortedUniqueStrings was only ever exercised
// through processAuthzRequirements above, and every one of those call sites
// hands it a []string literal. Route metadata written by a Go producer
// (WithAnyRole, WithAllPermissions) is indeed []string, but a value that
// round-tripped through JSON -- or an embedded/remote contributor's manifest
// -- decodes as []any, and sortedUniqueStrings has to coerce that shape too.
func TestSortedUniqueStrings(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want []string
	}{
		{"[]string sorted and deduplicated", []string{"editor", "admin", "admin"}, []string{"admin", "editor"}},
		{"[]any of strings", []any{"editor", "admin", "admin"}, []string{"admin", "editor"}},
		{
			"[]any with non-strings filtered out",
			[]any{"admin", 42, "editor", true, nil, "admin"},
			[]string{"admin", "editor"},
		},
		{"[]any entirely non-strings", []any{1, 2, 3}, nil},
		{"wrong type entirely", "not-a-slice", nil},
		{"nil", nil, nil},
		{"empty []string", []string{}, nil},
		{"[]string of only empties", []string{"", ""}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sortedUniqueStrings(tc.in)

			if tc.want == nil {
				// Asserted explicitly rather than via len(got) == 0: the
				// caller's omit-the-key logic in processAuthzRequirements
				// tests the pointer's nilness, and a non-nil empty slice
				// would pass a length check while still breaking that
				// caller.
				if got != nil {
					t.Errorf("sortedUniqueStrings(%#v) = %#v, want nil", tc.in, got)
				}

				return
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("sortedUniqueStrings(%#v) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}
