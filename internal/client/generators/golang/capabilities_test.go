package golang_test

import (
	"context"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators/golang"
)

func TestGoGeneratorEmitsCapabilitiesWhenDeclared(t *testing.T) {
	result, err := golang.NewGenerator().Generate(
		context.Background(), specWithRolesAndPermissions(), authStreamingConfig())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	src, ok := result.Files["capabilities.go"]
	if !ok {
		t.Fatal("capabilities.go not emitted")
	}

	for _, want := range []string{
		"type Role string",
		"type Permission string",
		"RoleAdmin Role = \"admin\"",
		"PermissionUsersWrite Permission = \"users:write\"",
		"func (c *Client) SetPrincipal(",
		"func (c *Client) HasRole(",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("capabilities.go missing %q\n%s", want, src)
		}
	}

	if _, err := parser.ParseFile(token.NewFileSet(), "capabilities.go", src, parser.AllErrors); err != nil {
		t.Errorf("capabilities.go does not parse: %v\n%s", err, src)
	}
}

// No file rather than an empty one, matching how the TypeScript generator
// treats the same case: a spec declaring nothing gets no module, and a client
// that does not need the feature carries none of it.
func TestGoGeneratorOmitsCapabilitiesWhenNothingDeclared(t *testing.T) {
	result, err := golang.NewGenerator().Generate(
		context.Background(), specSchemalessTransports(), schemalessStreamingConfig())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if _, present := result.Files["capabilities.go"]; present {
		t.Error("capabilities.go emitted for a spec declaring no roles, permissions or scopes")
	}
}

// TestGoGeneratorWarnsOnCollidingCapabilityIdentifiers is the end-to-end
// counterpart of TestResolveCapabilityConstsWarnsOnCollision (in
// capabilities_internal_test.go, which checks resolveCapabilityConsts
// directly): it confirms the warning actually reaches the caller through
// generators.GeneratedClient.Warnings, the same path generateClientFile's
// auth warnings already use, and that the generated file still only declares
// one PermissionUsersWrite -- the build gate proves that compiles, but not
// that a reader would notice one of their two permissions vanished. A
// count-only assertion here would pass even if the surviving constant held
// the wrong one of the two colliding strings, so this checks the exact
// PermissionUsersWrite line too.
func TestGoGeneratorWarnsOnCollidingCapabilityIdentifiers(t *testing.T) {
	result, err := golang.NewGenerator().Generate(
		context.Background(), specWithCollidingPermissions(), authStreamingConfig())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	src, ok := result.Files["capabilities.go"]
	if !ok {
		t.Fatal("capabilities.go not emitted")
	}

	// "users-write" sorts before "users:write" ('-' < ':'), so it is the one
	// that claims the shared identifier and survives.
	if !strings.Contains(src, `PermissionUsersWrite Permission = "users-write"`) {
		t.Errorf("capabilities.go missing the surviving constant\n%s", src)
	}

	if strings.Count(src, "PermissionUsersWrite ") != 1 {
		t.Errorf("capabilities.go declares PermissionUsersWrite more than once\n%s", src)
	}

	var found string

	for _, w := range result.Warnings {
		if strings.Contains(w, "UsersWrite") {
			found = w

			break
		}
	}

	if found == "" {
		t.Fatalf("no warning naming the collision; got %v", result.Warnings)
	}

	for _, want := range []string{"users-write", "users:write"} {
		if !strings.Contains(found, want) {
			t.Errorf("warning %q does not name %q", found, want)
		}
	}
}

// TestGoGeneratorWarnsOnUnusableCapabilityIdentifier is the end-to-end
// counterpart of TestResolveCapabilityConstsWarnsOnEmptyIdent: a role that is
// entirely punctuation must not produce a bare "Role" constant with no
// suffix, and must be reported rather than silently dropped.
func TestGoGeneratorWarnsOnUnusableCapabilityIdentifier(t *testing.T) {
	result, err := golang.NewGenerator().Generate(
		context.Background(), specWithUnusableRoleIdent(), authStreamingConfig())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	src, ok := result.Files["capabilities.go"]
	if !ok {
		t.Fatal("capabilities.go not emitted")
	}

	// The punctuation-only role must not surface as a bare-prefix constant.
	if strings.Contains(src, "Role Role = ") {
		t.Errorf("capabilities.go declares a bare-prefix Role constant\n%s", src)
	}

	var found string

	for _, w := range result.Warnings {
		if strings.Contains(w, ":::") {
			found = w

			break
		}
	}

	if found == "" {
		t.Fatalf("no warning naming the unusable role value; got %v", result.Warnings)
	}
}

// specWithCollidingPermissions declares two permissions, "users-write" and
// "users:write", that capabilityIdent both resolve to "UsersWrite" --
// capabilityIdent keeps only letters and digits, so '-' and ':' both vanish.
func specWithCollidingPermissions() *client.APISpec {
	return &client.APISpec{
		Info:    client.APIInfo{Title: "Collision API", Version: "1.0.0"},
		Servers: []client.Server{{URL: "https://api.example.com"}},
		Endpoints: []client.Endpoint{
			{
				ID:     "writeUser",
				Method: "POST",
				Path:   "/users",
				Authorization: &client.Authorization{
					Permissions: []string{"users-write", "users:write"},
				},
			},
		},
	}
}

// specWithUnusableRoleIdent declares a role that is entirely punctuation, so
// capabilityIdent has nothing to build an identifier out of.
func specWithUnusableRoleIdent() *client.APISpec {
	return &client.APISpec{
		Info:    client.APIInfo{Title: "Unusable Role API", Version: "1.0.0"},
		Servers: []client.Server{{URL: "https://api.example.com"}},
		Endpoints: []client.Endpoint{
			{
				ID:     "listOrders",
				Method: "GET",
				Path:   "/orders",
				Authorization: &client.Authorization{
					Roles: []string{":::"},
				},
			},
		},
	}
}

// specWithRolesAndPermissions declares roles and permissions across two REST
// endpoints rather than one. A fixture with a single role and a single
// permission cannot tell a sorted walk from an unsorted one -- both produce
// one-element output -- so TestGoGeneratorIsDeterministic in
// determinism_test.go needs several of each to have a chance of catching a
// regression in collectAuthz's sort.
func specWithRolesAndPermissions() *client.APISpec {
	return &client.APISpec{
		Info:    client.APIInfo{Title: "Authz API", Version: "1.0.0"},
		Servers: []client.Server{{URL: "https://api.example.com"}},
		Endpoints: []client.Endpoint{
			{
				ID:     "createUser",
				Method: "POST",
				Path:   "/users",
				Authorization: &client.Authorization{
					Roles:       []string{"admin"},
					Permissions: []string{"users:write"},
				},
			},
			{
				ID:     "deleteUser",
				Method: "DELETE",
				Path:   "/users/{id}",
				Authorization: &client.Authorization{
					Roles:       []string{"superadmin", "owner"},
					Permissions: []string{"users:delete", "users:read"},
				},
			},
		},
	}
}
