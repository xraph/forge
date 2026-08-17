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

// TestWithGroupAnyRoleWritesMetadata mirrors TestWithAnyRoleWritesMetadata at
// group level. WithGroupAnyRole and WithGroupAllPermissions are shipped
// public options with no test anywhere before this.
func TestWithGroupAnyRoleWritesMetadata(t *testing.T) {
	config := &GroupConfig{}

	WithGroupAnyRole("admin", "editor").Apply(config)

	roles, ok := config.Metadata["auth.roles"].([]string)
	if !ok {
		t.Fatalf("auth.roles missing or wrong type: %#v", config.Metadata["auth.roles"])
	}

	if len(roles) != 2 || roles[0] != "admin" || roles[1] != "editor" {
		t.Errorf("auth.roles = %v, want [admin editor]", roles)
	}
}

// TestWithGroupAllPermissionsWritesMetadata mirrors
// TestWithAllPermissionsWritesMetadata at group level.
func TestWithGroupAllPermissionsWritesMetadata(t *testing.T) {
	config := &GroupConfig{}

	WithGroupAllPermissions("users:write").Apply(config)

	perms, ok := config.Metadata["auth.permissions"].([]string)
	if !ok {
		t.Fatalf("auth.permissions missing: %#v", config.Metadata)
	}

	if len(perms) != 1 || perms[0] != "users:write" {
		t.Errorf("auth.permissions = %v, want [users:write]", perms)
	}
}

// TestGroupRoleOptionDoesNotClobberProviders mirrors
// TestRoleOptionDoesNotClobberProviders at group level: group-level roles
// have to compose with WithGroupAuth's provider metadata rather than
// replacing it, the same way the route-level options do.
func TestGroupRoleOptionDoesNotClobberProviders(t *testing.T) {
	config := &GroupConfig{}

	WithGroupAuth("jwt").Apply(config)
	WithGroupAnyRole("admin").Apply(config)

	providers, ok := config.Metadata["auth.providers"].([]string)
	if !ok || len(providers) != 1 || providers[0] != "jwt" {
		t.Errorf("auth.providers = %#v, want [jwt]", config.Metadata["auth.providers"])
	}

	if _, ok := config.Metadata["auth.roles"].([]string); !ok {
		t.Error("auth.roles missing after composing with WithGroupAuth")
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
