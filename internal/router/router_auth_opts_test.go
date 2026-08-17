package router

import "testing"

func TestWithAnyRoleWritesMetadata(t *testing.T) {
	config := &RouteConfig{}

	WithAnyRole("admin", "editor").Apply(config)

	roles, ok := config.Metadata["auth.roles"].([]string)
	if !ok {
		t.Fatalf("auth.roles missing or wrong type: %#v", config.Metadata["auth.roles"])
	}

	if len(roles) != 2 || roles[0] != "admin" || roles[1] != "editor" {
		t.Errorf("auth.roles = %v, want [admin editor]", roles)
	}
}

func TestWithAllPermissionsWritesMetadata(t *testing.T) {
	config := &RouteConfig{}

	WithAllPermissions("users:write").Apply(config)

	perms, ok := config.Metadata["auth.permissions"].([]string)
	if !ok {
		t.Fatalf("auth.permissions missing: %#v", config.Metadata)
	}

	if len(perms) != 1 || perms[0] != "users:write" {
		t.Errorf("auth.permissions = %v, want [users:write]", perms)
	}
}

// Roles and permissions compose with the existing provider option rather than
// replacing its metadata.
func TestRoleOptionDoesNotClobberProviders(t *testing.T) {
	config := &RouteConfig{}

	WithRequiredAuth("jwt", "write:users").Apply(config)
	WithAnyRole("admin").Apply(config)

	providers, ok := config.Metadata["auth.providers"].([]string)
	if !ok || len(providers) != 1 || providers[0] != "jwt" {
		t.Errorf("auth.providers = %#v, want [jwt]", config.Metadata["auth.providers"])
	}

	if _, ok := config.Metadata["auth.roles"].([]string); !ok {
		t.Error("auth.roles missing after composing with WithRequiredAuth")
	}
}
