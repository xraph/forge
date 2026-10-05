package dart

import (
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestCapabilitiesEmitVocabulariesAndRequirements(t *testing.T) {
	spec := ordersSpec()
	spec.Endpoints[3].Authorization = &client.Authorization{Roles: []string{"editor", "admin"}, Permissions: []string{"orders.delete"}}

	f := fixture(t, "default")
	f.Spec = spec

	caps := file(t, generate(t, f), "lib/src/capabilities.dart")

	assertContains(t, "capabilities.dart", caps,
		"A UX AFFORDANCE. NEVER A SECURITY BOUNDARY.",
		"extension type const Scope._(String value) implements Object {",
		"static const ordersRead = Scope._('orders.read');",
		"static const admin = Role._('admin');",
		"static const List<Permission> values = [ordersDelete];",
		"static const ordersGet = OperationName._('orders.get');",
		"'orders.get': [[Scope.ordersRead]],",
		"'orders.delete': (roles: [Role.admin, Role.editor], permissions: [Permission.ordersDelete]),",
		"bool canCall(OperationName operation) {",
		"List<Scope> missingCapabilities(OperationName operation) {",
	)
}

func TestCapabilitiesAreOmittedWithoutScopes(t *testing.T) {
	if _, ok := generate(t, fixture(t, "minimal")).Files["lib/src/capabilities.dart"]; ok {
		t.Error("capabilities.dart emitted for a spec that declares no scope, role or permission")
	}
}

func TestPaginationStreamsEveryItemOfAListOperation(t *testing.T) {
	pages := file(t, generate(t, fixture(t, "default")), "lib/src/pagination.dart")

	assertContains(t, "pagination.dart", pages,
		"Stream<T> paginateAll<T>(",
		"Future<List<T>> collectAll<T>(",
		"extension RestClientPagination on RestClient {",
		"Stream<Order> ordersListPaginated({required String xTenant, PageParams params = const PageParams()}) =>",
		"final page = await orders.list(cursor: p.cursor, limit: p.limit, xTenant: xTenant);",
		"return Page(page.items, nextCursor: page.nextCursor, hasMore: page.hasMore ?? false);",
		"import 'models/order.dart';",
	)
}

// capabilitiesFixtures cover the shapes the one-spec orders fixture does not:
// a document whose scopes come only from a streaming route, one that declares
// roles and nothing else, and one that uses all three vocabularies with
// alternatives.
func capabilitiesFixtures() []gateFixture {
	streaming := baseConfig()
	streaming.PackageName = "caps_stream_client"

	roles := baseConfig()
	roles.PackageName = "caps_roles_client"
	roles.Hooks = false

	authz := baseConfig()
	authz.PackageName = "authz_client"
	authz.Hooks = false

	authzSpec := ordersSpec()
	authzSpec.Endpoints[2].Security = []client.SecurityRequirement{
		{SchemeName: "bearerAuth", Scopes: []string{"orders.write", "orders.read"}},
		{SchemeName: "apiKey", Scopes: []string{"orders.admin"}},
	}
	authzSpec.Endpoints[3].Authorization = &client.Authorization{Roles: []string{"editor", "admin"}, Permissions: []string{"orders.delete"}}

	return []gateFixture{
		{
			Name:   "capabilities-streaming",
			Config: streaming,
			Spec: &client.APISpec{
				Info: client.APIInfo{Title: "Stream API", Version: "1"},
				WebSockets: []client.WebSocketEndpoint{{
					ID: "feed", Path: "/ws/feed",
					// Two scopes that collapse to one member name, a keyword and
					// a name the vocabulary type itself reserves.
					Security: []client.SecurityRequirement{{
						SchemeName: "bearerAuth",
						Scopes:     []string{"read:users", "orders.read", "orders_read", "class", "values", "1st"},
					}},
				}},
			},
		},
		{
			Name:   "capabilities-roles",
			Config: roles,
			Spec: &client.APISpec{
				Info: client.APIInfo{Title: "Roles API", Version: "1"},
				Endpoints: []client.Endpoint{
					{
						Method: "GET", Path: "/admin", OperationID: "values",
						Authorization: &client.Authorization{Roles: []string{"admin"}},
						Responses:     map[int]*client.Response{204: {Description: "ok"}},
					},
					{
						Method: "GET", Path: "/open", OperationID: "class",
						Responses: map[int]*client.Response{204: {Description: "ok"}},
					},
				},
			},
		},
		{Name: "capabilities-authz", Config: authz, Spec: authzSpec},
	}
}

func TestCapabilitiesEmptyVocabulariesAndReservedNames(t *testing.T) {
	var streaming, roles gateFixture

	for _, f := range capabilitiesFixtures() {
		switch f.Name {
		case "capabilities-streaming":
			streaming = f
		case "capabilities-roles":
			roles = f
		}
	}

	stream := file(t, generate(t, streaming), "lib/src/capabilities.dart")

	assertContains(t, "streaming capabilities.dart", stream,
		"static const readUsers = Scope._('read:users');",
		"static const ordersRead = Scope._('orders.read');",
		"static const ordersRead2 = Scope._('orders_read');",
		"static const class$ = Scope._('class');",
		"static const values$ = Scope._('values');",
		"static const v1st = Scope._('1st');",
		"bool can(Scope scope)",
	)

	for _, absent := range []string{"OperationName", "requiredCapabilities", "canCall"} {
		if strings.Contains(stream, absent) {
			t.Errorf("a spec with no REST endpoint emitted %q", absent)
		}
	}

	only := file(t, generate(t, roles), "lib/src/capabilities.dart")

	assertContains(t, "roles capabilities.dart", only,
		"static const List<Scope> values = [];",
		"static const List<Permission> values = [];",
		"static const admin = Role._('admin');",
		"static const values$ = OperationName._('values');",
		"static const class$ = OperationName._('class');",
		"'values': (roles: [Role.admin], permissions: []),",
		"const Map<String, List<List<Scope>>> requiredCapabilities = {};",
	)
}

func TestCapabilitiesSortAlternativesAndVocabularies(t *testing.T) {
	var authz gateFixture

	for _, f := range capabilitiesFixtures() {
		if f.Name == "capabilities-authz" {
			authz = f
		}
	}

	caps := file(t, generate(t, authz), "lib/src/capabilities.dart")

	assertContains(t, "authz capabilities.dart", caps,
		"'orders.update': [[Scope.ordersAdmin], [Scope.ordersRead, Scope.ordersWrite]],",
	)

	// The vocabularies come out sorted, so the file does not depend on the
	// order the document listed them in.
	scopes := strings.Index(caps, "static const ordersAdmin")
	read := strings.Index(caps, "static const ordersRead")

	if scopes < 0 || read < 0 || scopes > read {
		t.Errorf("scopes are not sorted: ordersAdmin at %d, ordersRead at %d", scopes, read)
	}
}

// TestCapabilityTablesAreSortedAndOmitUngatedOperations pins the table Task 10
// compares with the TypeScript one: sorted vocabularies, one key per
// operation, and only gated operations in the requirement maps.
func TestCapabilityTablesAreSortedAndOmitUngatedOperations(t *testing.T) {
	var authz gateFixture

	for _, f := range capabilitiesFixtures() {
		if f.Name == "capabilities-authz" {
			authz = f
		}
	}

	keys := operationKeys(authz.Spec.Endpoints)
	got := capabilityTables(authz.Spec, keys)

	if want := []string{"orders.admin", "orders.read", "orders.write"}; !reflect.DeepEqual(got.Scopes, want) {
		t.Errorf("scopes = %v, want %v", got.Scopes, want)
	}

	if want := []string{"admin", "editor"}; !reflect.DeepEqual(got.Roles, want) {
		t.Errorf("roles = %v, want %v", got.Roles, want)
	}

	if want := []string{"orders.delete"}; !reflect.DeepEqual(got.Permissions, want) {
		t.Errorf("permissions = %v, want %v", got.Permissions, want)
	}

	if !reflect.DeepEqual(got.Operations, keys) {
		t.Errorf("operations = %v, want %v", got.Operations, keys)
	}

	if _, gated := got.RequiredCapabilities["orders.bulk"]; gated {
		t.Error("an ungated operation appears in RequiredCapabilities")
	}

	if _, gated := got.RequiredAuthorization["orders.get"]; gated {
		t.Error("an operation with no roles or permissions appears in RequiredAuthorization")
	}

	want := client.TableAuthorization{Roles: []string{"admin", "editor"}, Permissions: []string{"orders.delete"}}
	if !reflect.DeepEqual(got.RequiredAuthorization["orders.delete"], want) {
		t.Errorf("orders.delete authorization = %+v, want %+v", got.RequiredAuthorization["orders.delete"], want)
	}
}
