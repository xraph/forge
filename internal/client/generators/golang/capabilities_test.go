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
