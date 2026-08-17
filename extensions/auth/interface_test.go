package auth

import "testing"

func TestAuthContextRolePredicates(t *testing.T) {
	a := &AuthContext{Roles: []string{"admin", "editor"}}

	if !a.HasRole("admin") {
		t.Error("HasRole(admin) = false, want true")
	}

	if a.HasRole("viewer") {
		t.Error("HasRole(viewer) = true, want false")
	}

	// Roles are any-of: holding one of the listed roles is enough.
	if !a.HasAnyRole("viewer", "editor") {
		t.Error("HasAnyRole(viewer, editor) = false, want true")
	}

	if a.HasAnyRole("viewer", "owner") {
		t.Error("HasAnyRole(viewer, owner) = true, want false")
	}

	// An empty requirement is satisfied by everyone, which is what makes an
	// unguarded route stay unguarded when the requirement is threaded through
	// generically.
	if !a.HasAnyRole() {
		t.Error("HasAnyRole() = false, want true")
	}
}

func TestAuthContextPermissionPredicates(t *testing.T) {
	a := &AuthContext{Permissions: []string{"users:write", "users:read"}}

	if !a.HasPermission("users:write") {
		t.Error("HasPermission(users:write) = false, want true")
	}

	// Permissions are all-of.
	if !a.HasAllPermissions("users:read", "users:write") {
		t.Error("HasAllPermissions(read, write) = false, want true")
	}

	if a.HasAllPermissions("users:read", "users:delete") {
		t.Error("HasAllPermissions(read, delete) = true, want false")
	}

	if !a.HasAllPermissions() {
		t.Error("HasAllPermissions() = false, want true")
	}
}

// A nil receiver is reachable: FromContext returns (nil, false) and a caller
// that ignores the bool would otherwise panic inside the predicate rather
// than simply being denied.
func TestAuthContextNilReceiverDenies(t *testing.T) {
	var a *AuthContext

	if a.HasRole("admin") {
		t.Error("nil HasRole = true, want false")
	}

	if a.HasAnyRole("admin") {
		t.Error("nil HasAnyRole = true, want false")
	}

	if a.HasAllPermissions("users:write") {
		t.Error("nil HasAllPermissions = true, want false")
	}

	if a.HasScope("read:users") {
		t.Error("nil HasScope = true, want false")
	}

	if a.HasScopes("read:users") {
		t.Error("nil HasScopes = true, want false")
	}

	// A nil receiver with no required scopes still returns true: an empty
	// requirement must not deny, matching HasAnyRole/HasAllPermissions.
	if !a.HasScopes() {
		t.Error("nil HasScopes() = false, want true")
	}
}
