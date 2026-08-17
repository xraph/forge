package client

import (
	"reflect"
	"testing"
)

func TestResolveEndpointAuthz(t *testing.T) {
	got := resolveEndpointAuthz(map[string]any{
		"x-forge-authz": map[string]any{
			"roles":       []any{"editor", "admin"},
			"permissions": []any{"users:write"},
		},
	})

	if got == nil {
		t.Fatal("resolveEndpointAuthz = nil, want a value")
	}

	if !reflect.DeepEqual(got.Roles, []string{"admin", "editor"}) {
		t.Errorf("Roles = %v, want [admin editor]", got.Roles)
	}

	if !reflect.DeepEqual(got.Permissions, []string{"users:write"}) {
		t.Errorf("Permissions = %v, want [users:write]", got.Permissions)
	}
}

// Absent rather than empty, matching how the entities and streams tables treat
// their own empty case. A non-nil Authorization with two empty slices would
// make every endpoint look guarded to the generators.
func TestResolveEndpointAuthzAbsentWhenNothingDeclared(t *testing.T) {
	for name, ext := range map[string]map[string]any{
		"no extension":  {},
		"empty object":  {"x-forge-authz": map[string]any{}},
		"empty lists":   {"x-forge-authz": map[string]any{"roles": []any{}}},
		"wrong type":    {"x-forge-authz": "nonsense"},
		"nil extension": nil,
	} {
		t.Run(name, func(t *testing.T) {
			if got := resolveEndpointAuthz(ext); got != nil {
				t.Errorf("resolveEndpointAuthz = %+v, want nil", got)
			}
		})
	}
}

// A YAML document decodes into []any; a Go producer writes []string. Both
// reach this resolver, so both have to work.
func TestResolveEndpointAuthzAcceptsBothSliceForms(t *testing.T) {
	fromGo := resolveEndpointAuthz(map[string]any{
		"x-forge-authz": map[string]any{"roles": []string{"admin"}},
	})

	fromYAML := resolveEndpointAuthz(map[string]any{
		"x-forge-authz": map[string]any{"roles": []any{"admin"}},
	})

	if !reflect.DeepEqual(fromGo, fromYAML) {
		t.Errorf("Go form %+v != YAML form %+v", fromGo, fromYAML)
	}
}
