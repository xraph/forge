package client_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// TestCollectCapabilitiesIsSortedAndDistinct pins the property the generated
// capability file depends on and CI would otherwise catch as drift.
//
// Endpoint.Security is built by ranging a Go map, so the scopes reach this
// function in an order that changes between runs. The scopes below are
// declared in an order that is neither sorted nor duplicate-free precisely so
// an unsorted or non-deduplicating implementation fails here rather than in a
// byte-diff nobody can reproduce locally.
func TestCollectCapabilitiesIsSortedAndDistinct(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				Method: "POST", Path: "/orders",
				Security: []client.SecurityRequirement{
					{SchemeName: "jwt", Scopes: []string{"orders.write", "admin"}},
				},
			},
			{
				Method: "GET", Path: "/orders",
				Security: []client.SecurityRequirement{
					{SchemeName: "jwt", Scopes: []string{"orders.read", "admin"}},
				},
			},
		},
	}

	got := client.NewAuthCodeGenerator().CollectCapabilities(spec)
	want := []string{"admin", "orders.read", "orders.write"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectCapabilities() = %v, want %v", got, want)
	}
}

// TestCollectCapabilitiesSpansEveryEndpointKind covers the four endpoint
// collections that carry security requirements.
//
// WebTransport is the one worth stating: requiresScopes does not walk it, so a
// capability union built by reusing that walker would silently omit a scope the
// spec declares. A scope is a scope whatever transport announces it.
func TestCollectCapabilitiesSpansEveryEndpointKind(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{Security: []client.SecurityRequirement{{SchemeName: "jwt", Scopes: []string{"rest.scope"}}}},
		},
		WebSockets: []client.WebSocketEndpoint{
			{Security: []client.SecurityRequirement{{SchemeName: "jwt", Scopes: []string{"ws.scope"}}}},
		},
		SSEs: []client.SSEEndpoint{
			{Security: []client.SecurityRequirement{{SchemeName: "jwt", Scopes: []string{"sse.scope"}}}},
		},
		WebTransports: []client.WebTransportEndpoint{
			{Security: []client.SecurityRequirement{{SchemeName: "jwt", Scopes: []string{"wt.scope"}}}},
		},
	}

	got := client.NewAuthCodeGenerator().CollectCapabilities(spec)
	want := []string{"rest.scope", "sse.scope", "ws.scope", "wt.scope"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectCapabilities() = %v, want %v", got, want)
	}
}

func TestCollectCapabilitiesIgnoresScopelessSecurity(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{Security: []client.SecurityRequirement{{SchemeName: "jwt"}}},
		},
	}

	if got := client.NewAuthCodeGenerator().CollectCapabilities(spec); len(got) != 0 {
		t.Fatalf("CollectCapabilities() = %v, want none", got)
	}
}

// TestEndpointCapabilitiesSortsWithinAndAcrossAlternatives pins both axes of
// the ordering the emitted table depends on.
func TestEndpointCapabilitiesSortsWithinAndAcrossAlternatives(t *testing.T) {
	endpoint := client.Endpoint{
		Security: []client.SecurityRequirement{
			{SchemeName: "session", Scopes: []string{"orders.write", "admin"}},
			{SchemeName: "jwt", Scopes: []string{"billing", "accounts.read"}},
		},
	}

	got := client.NewAuthCodeGenerator().EndpointCapabilities(endpoint)
	want := [][]string{
		{"accounts.read", "billing"},
		{"admin", "orders.write"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EndpointCapabilities() = %v, want %v", got, want)
	}
}

// TestEndpointCapabilitiesDedupesIdenticalAlternatives covers the shape Forge's
// own OpenAPI emitter produces.
//
// processSecurityRequirements in OR mode writes one requirement per provider
// carrying the SAME scope list, so a route declaring two providers arrives here
// as two alternatives that mean one thing. Emitting both would make the
// generated table say an operation can be reached two ways when it can be
// reached one way through two doors.
func TestEndpointCapabilitiesDedupesIdenticalAlternatives(t *testing.T) {
	endpoint := client.Endpoint{
		Security: []client.SecurityRequirement{
			{SchemeName: "jwt", Scopes: []string{"orders.write"}},
			{SchemeName: "apiKey", Scopes: []string{"orders.write"}},
		},
	}

	got := client.NewAuthCodeGenerator().EndpointCapabilities(endpoint)
	want := [][]string{{"orders.write"}}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EndpointCapabilities() = %v, want %v", got, want)
	}
}

// TestEndpointCapabilitiesUngatedWhenAnyAlternativeIsEmpty covers the OR
// semantics that make a scopeless alternative dominate its siblings.
//
// WithRequiredAuth("jwt") -- authentication required, no particular scope --
// produces a requirement with an empty scope list. Because alternatives are
// ORed, satisfying that one permits the operation, so the endpoint is not
// scope-gated however many scopes a sibling alternative demands. Returning the
// sibling instead would hide an action the server would have allowed.
func TestEndpointCapabilitiesUngatedWhenAnyAlternativeIsEmpty(t *testing.T) {
	endpoint := client.Endpoint{
		Security: []client.SecurityRequirement{
			{SchemeName: "jwt", Scopes: []string{"orders.write"}},
			{SchemeName: "session"},
		},
	}

	if got := client.NewAuthCodeGenerator().EndpointCapabilities(endpoint); got != nil {
		t.Fatalf("EndpointCapabilities() = %v, want nil", got)
	}
}

func TestEndpointCapabilitiesNilWithoutSecurity(t *testing.T) {
	if got := client.NewAuthCodeGenerator().EndpointCapabilities(client.Endpoint{}); got != nil {
		t.Fatalf("EndpointCapabilities() = %v, want nil", got)
	}
}

// TestGenerateAuthDocumentationNamesTheWireParameterNotTheSchemeKey pins the
// fix for the defect this whole change exists to remove: a scheme's document
// key (e.g. "tenantKey") is an internal identifier, not the header or query
// parameter a caller has to set. Documentation that told a reader to send
// "tenantKey" would be exactly as wrong as generated code that did.
func TestGenerateAuthDocumentationNamesTheWireParameterNotTheSchemeKey(t *testing.T) {
	schemes := []client.DetectedAuthScheme{
		{Key: "tenantKey", ParamName: "X-Tenant-Key", Type: "apiKey", In: "header"},
		{Key: "sessionAuth", ParamName: "session_id", Type: "apiKey", In: "cookie"},
	}

	got := client.NewAuthCodeGenerator().GenerateAuthDocumentation(schemes)

	if !strings.Contains(got, "**Header Name**: X-Tenant-Key") {
		t.Errorf("doc missing header parameter name X-Tenant-Key:\n%s", got)
	}

	if strings.Contains(got, "**Header Name**: tenantKey") {
		t.Errorf("doc presents the scheme key as the header name:\n%s", got)
	}

	if !strings.Contains(got, "**Cookie Name**: session_id") {
		t.Errorf("doc missing cookie parameter name session_id:\n%s", got)
	}

	if strings.Contains(got, "**Cookie Name**: sessionAuth") {
		t.Errorf("doc presents the scheme key as the cookie name:\n%s", got)
	}

	// The section heading is the one place the scheme key belongs: it's what
	// endpoint security requirements refer to, not a wire value.
	if !strings.Contains(got, "### tenantKey") {
		t.Errorf("doc missing scheme key heading ### tenantKey:\n%s", got)
	}
}

// TestCollectRolesWalksEveryTransport covers the four endpoint collections
// that can carry an Authorization, WebTransport included -- a role declared
// on a WebTransport route is still a role this API has.
func TestCollectRolesWalksEveryTransport(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{Authorization: &client.Authorization{Roles: []string{"editor"}}},
		},
		WebSockets: []client.WebSocketEndpoint{
			{Authorization: &client.Authorization{Roles: []string{"admin"}}},
		},
		SSEs: []client.SSEEndpoint{
			{Authorization: &client.Authorization{Roles: []string{"admin"}}},
		},
		WebTransports: []client.WebTransportEndpoint{
			{Authorization: &client.Authorization{Roles: []string{"viewer"}}},
		},
	}

	got := client.NewAuthCodeGenerator().CollectRoles(spec)

	// Sorted and deduplicated across all four transports. A role declared on
	// a WebTransport route is still a role this API has.
	want := []string{"admin", "editor", "viewer"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CollectRoles = %v, want %v", got, want)
	}
}

func TestCollectPermissions(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{Authorization: &client.Authorization{Permissions: []string{"users:write"}}},
			{Authorization: &client.Authorization{Permissions: []string{"users:read"}}},
			{}, // unguarded, contributes nothing and must not panic
		},
	}

	got := client.NewAuthCodeGenerator().CollectPermissions(spec)

	want := []string{"users:read", "users:write"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CollectPermissions = %v, want %v", got, want)
	}
}

func TestCollectRolesEmptyWhenNoneDeclared(t *testing.T) {
	got := client.NewAuthCodeGenerator().CollectRoles(&client.APISpec{
		Endpoints: []client.Endpoint{{}},
	})

	if len(got) != 0 {
		t.Errorf("CollectRoles = %v, want empty", got)
	}
}

// TestRequiresScopesCoversWebTransport is a regression test for the gap the
// review found: requiresScopes walked Endpoints, WebSockets and SSEs but not
// WebTransports, unlike CollectCapabilities right below it which deliberately
// walks all four.
//
// requiresScopes itself is unexported, so this goes through the exported
// DetectAuthSchemes: the spec's only scoped security requirement sits on a
// WebTransport endpoint, and a matching SecurityScheme entry makes
// DetectAuthSchemes look it up by the same Key/SchemeName. Before the fix,
// DetectedAuthScheme.RequiresScope comes back false because the WebTransport
// loop is missing; after the fix it comes back true.
func TestRequiresScopesCoversWebTransport(t *testing.T) {
	spec := &client.APISpec{
		Security: []client.SecurityScheme{
			{Key: "jwt", Type: "http", Scheme: "bearer"},
		},
		WebTransports: []client.WebTransportEndpoint{
			{
				Security: []client.SecurityRequirement{
					{SchemeName: "jwt", Scopes: []string{"wt.scope"}},
				},
			},
		},
	}

	detected := client.NewAuthCodeGenerator().DetectAuthSchemes(spec)

	if len(detected) != 1 {
		t.Fatalf("DetectAuthSchemes() = %v, want exactly one scheme", detected)
	}

	if !detected[0].RequiresScope {
		t.Errorf("DetectedAuthScheme.RequiresScope = false, want true for a scope declared only on a WebTransport endpoint")
	}
}
