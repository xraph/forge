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
