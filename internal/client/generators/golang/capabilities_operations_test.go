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

// specWithAuthorizationMatrix exercises every combination CanCall has to
// reason about: a single scope alternative, two ORed scope alternatives, a
// role-and-permission requirement stacked on top of a scope requirement, and
// an endpoint gated on nothing at all.
func specWithAuthorizationMatrix() *client.APISpec {
	return &client.APISpec{
		Info:    client.APIInfo{Title: "Authorization Matrix API", Version: "1.0.0"},
		Servers: []client.Server{{URL: "https://api.example.com"}},
		Endpoints: []client.Endpoint{
			{
				ID:     "listUsers",
				Method: "GET",
				Path:   "/users",
				Security: []client.SecurityRequirement{
					{SchemeName: "jwt", Scopes: []string{"users:read"}},
				},
			},
			{
				ID:     "createUser",
				Method: "POST",
				Path:   "/users",
				Security: []client.SecurityRequirement{
					{SchemeName: "jwt", Scopes: []string{"users:write", "admin"}},
				},
				Authorization: &client.Authorization{
					Roles:       []string{"editor", "admin"},
					Permissions: []string{"users:write"},
				},
			},
			{
				ID:     "uploadFile",
				Method: "POST",
				Path:   "/uploads",
				Security: []client.SecurityRequirement{
					{SchemeName: "apiKey", Scopes: []string{"upload:write"}},
					{SchemeName: "jwt", Scopes: []string{"admin"}},
				},
			},
			{
				ID:     "publicPing",
				Method: "GET",
				Path:   "/ping",
			},
		},
	}
}

// TestGoGeneratorEmitsOperationCapabilitySurface is the regression test for
// F1: the Go capability surface had Can, HasRole and HasPermission but no
// CanCall or MissingCapabilities, so a Go service calling another Go service
// had no way to ask "would this call be refused" the way the TypeScript
// client's canCall/missingCapabilities already can.
func TestGoGeneratorEmitsOperationCapabilitySurface(t *testing.T) {
	result, err := golang.NewGenerator().Generate(
		context.Background(), specWithAuthorizationMatrix(), authStreamingConfig())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	src, ok := result.Files["capabilities.go"]
	if !ok {
		t.Fatal("capabilities.go not emitted")
	}

	for _, want := range []string{
		"type OperationName string",
		"func (c *Client) MissingCapabilities(op OperationName) []Capability {",
		"func (c *Client) CanCall(op OperationName) bool {",
		"var operationRequirements = map[OperationName]operationRequirement{",
		// The scope-gated, role-and-permission-gated operation.
		`"createUser": {`,
		// The two-alternative operation.
		`"uploadFile": {`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("capabilities.go missing %q\n%s", want, src)
		}
	}

	// publicPing declares no requirement in any vocabulary and must be
	// ABSENT from the table entirely -- present with empty fields would be
	// indistinguishable from "gated on nothing", which reads identically to
	// "not gated" only by accident.
	if strings.Contains(src, `"publicPing":`) {
		t.Errorf("capabilities.go declares a table entry for an ungated operation\n%s", src)
	}

	if _, err := parser.ParseFile(token.NewFileSet(), "capabilities.go", src, parser.AllErrors); err != nil {
		t.Errorf("capabilities.go does not parse: %v\n%s", err, src)
	}
}

// TestGoGeneratorOmitsOperationTableWhenNoEndpoints mirrors the TypeScript
// generator's own gate (capabilities.ts's writeOperationUnion/writeRequirements
// call site): a spec whose only roles or permissions come from a non-REST
// transport still gets Capability/Role/Permission and the predicates over
// them, but OperationName, the requirement table, CanCall and
// MissingCapabilities are all keyed by REST operation and have nothing to be
// keyed by, so none of them are emitted.
func TestGoGeneratorOmitsOperationTableWhenNoEndpoints(t *testing.T) {
	spec := &client.APISpec{
		Info:    client.APIInfo{Title: "WebSocket Only API", Version: "1.0.0"},
		Servers: []client.Server{{URL: "https://api.example.com"}},
		WebSockets: []client.WebSocketEndpoint{
			{
				ID:   "chatRoom",
				Path: "/ws/chat",
				Authorization: &client.Authorization{
					Roles: []string{"member"},
				},
			},
		},
	}

	result, err := golang.NewGenerator().Generate(context.Background(), spec, authStreamingConfig())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	src, ok := result.Files["capabilities.go"]
	if !ok {
		t.Fatal("capabilities.go not emitted for a spec declaring a role")
	}

	for _, want := range []string{"type Role string", "func (c *Client) HasRole("} {
		if !strings.Contains(src, want) {
			t.Errorf("capabilities.go missing %q\n%s", want, src)
		}
	}

	for _, unwanted := range []string{
		"OperationName", "CanCall", "MissingCapabilities", "operationRequirements",
	} {
		if strings.Contains(src, unwanted) {
			t.Errorf("capabilities.go contains %q for a spec with no REST endpoints\n%s", unwanted, src)
		}
	}

	if _, err := parser.ParseFile(token.NewFileSet(), "capabilities.go", src, parser.AllErrors); err != nil {
		t.Errorf("capabilities.go does not parse: %v\n%s", err, src)
	}
}
