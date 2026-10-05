package dart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func queryParam(name, typ string, required bool) client.Parameter {
	return client.Parameter{Name: name, In: "query", Required: required, Schema: &client.Schema{Type: typ}}
}

func listOf(item *client.Schema) *client.Schema { return &client.Schema{Type: "array", Items: item} }

// paginationFixture walks the three pagination styles and the shapes that
// must be left alone. Hooks are off so the package resolves with package:http
// alone.
func paginationFixture() gateFixture {
	cfg := baseConfig()
	cfg.PackageName = "paging_client"
	cfg.Hooks = false

	str := func(name string) client.Parameter {
		return client.Parameter{Name: name, In: "header", Required: true, Schema: &client.Schema{Type: "string"}}
	}

	json := func(name string) map[int]*client.Response {
		return map[int]*client.Response{200: {Content: jsonContent(ref(name))}}
	}

	return gateFixture{
		Name:   "pagination",
		Config: cfg,
		Spec: &client.APISpec{
			Info: client.APIInfo{Title: "Paging API", Version: "1"},
			Schemas: map[string]*client.Schema{
				"Item": {Type: "object", Required: []string{"id"}, Properties: map[string]*client.Schema{"id": {Type: "string"}}},
				// A required flag, a cursor under "next".
				"CursorPage": {
					Type: "object", Required: []string{"data", "has_more"},
					Properties: map[string]*client.Schema{"data": listOf(ref("Item")), "next": {Type: "string"}, "has_more": {Type: "boolean"}},
				},
				// An optional flag, items under "results".
				"NumberedPage": {
					Type: "object", Required: []string{"results"},
					Properties: map[string]*client.Schema{"results": listOf(ref("Item")), "has_more": {Type: "boolean"}},
				},
				"OffsetPage": {
					Type: "object", Required: []string{"items", "hasMore"},
					Properties: map[string]*client.Schema{"items": listOf(ref("Item")), "hasMore": {Type: "boolean"}},
				},
				// No items list at all.
				"Summary": {Type: "object", Properties: map[string]*client.Schema{"total": {Type: "integer"}}},
			},
			Endpoints: []client.Endpoint{
				{
					Method: "GET", Path: "/teams/{teamId}/items", OperationID: "teams.items.list",
					PathParams:  []client.Parameter{{Name: "teamId", In: "path", Required: true, Schema: &client.Schema{Type: "string"}}},
					QueryParams: []client.Parameter{queryParam("cursor", "string", false), queryParam("page_size", "integer", false)},
					Responses:   json("CursorPage"),
				},
				{
					Method: "GET", Path: "/pages", OperationID: "pages.list",
					QueryParams: []client.Parameter{queryParam("page", "integer", false), queryParam("per_page", "integer", false)},
					Responses:   json("NumberedPage"),
				},
				// A path parameter named like the namespace it is reached
				// through, and headers named like the walker's own locals.
				{
					Method: "GET", Path: "/offsets/{offsets}", OperationID: "offsets.list",
					PathParams:  []client.Parameter{{Name: "offsets", In: "path", Required: true, Schema: &client.Schema{Type: "string"}}},
					QueryParams: []client.Parameter{queryParam("offset", "integer", false), queryParam("limit", "integer", false), str("p"), str("params")},
					Responses:   json("OffsetPage"),
				},
				// Names the walker's locals would otherwise take: a path
				// parameter called page, and call paths rooted at page, p and
				// params.
				{
					Method: "GET", Path: "/pages/{page}/revisions", OperationID: "pages.revisions.list",
					PathParams:  []client.Parameter{{Name: "page", In: "path", Required: true, Schema: &client.Schema{Type: "string"}}},
					QueryParams: []client.Parameter{queryParam("cursor", "string", false)},
					Responses:   json("CursorPage"),
				},
				{
					Method: "GET", Path: "/page", OperationID: "page.list",
					QueryParams: []client.Parameter{queryParam("cursor", "string", false)},
					Responses:   json("CursorPage"),
				},
				{
					Method: "GET", Path: "/p", OperationID: "p.list",
					QueryParams: []client.Parameter{queryParam("cursor", "string", false)},
					Responses:   json("CursorPage"),
				},
				{
					Method: "GET", Path: "/params", OperationID: "params.list",
					QueryParams: []client.Parameter{queryParam("cursor", "string", false)},
					Responses:   json("CursorPage"),
				},
				// Left out: a required query parameter the walker cannot fill.
				{
					Method: "GET", Path: "/search", OperationID: "search.list",
					QueryParams: []client.Parameter{queryParam("cursor", "string", false), queryParam("q", "string", true)},
					Responses:   json("CursorPage"),
				},
				// Left out: the paging parameter itself is required.
				{
					Method: "GET", Path: "/mandatory", OperationID: "mandatory.list",
					QueryParams: []client.Parameter{queryParam("cursor", "string", true)},
					Responses:   json("CursorPage"),
				},
				// Left out: only a page size, nothing to advance.
				{
					Method: "GET", Path: "/sized", OperationID: "sized.list",
					QueryParams: []client.Parameter{queryParam("limit", "integer", false)},
					Responses:   json("CursorPage"),
				},
				// Left out: no items list in the response.
				{
					Method: "GET", Path: "/summary", OperationID: "summary.get",
					QueryParams: []client.Parameter{queryParam("cursor", "string", false)},
					Responses:   json("Summary"),
				},
				// Left out: not a read.
				{
					Method: "POST", Path: "/actions", OperationID: "actions.run",
					QueryParams: []client.Parameter{queryParam("cursor", "string", false)},
					Responses:   json("CursorPage"),
				},
			},
		},
	}
}

func TestPaginationWalksEachStyleAndLeavesTheRestAlone(t *testing.T) {
	pages := file(t, generate(t, paginationFixture()), "lib/src/pagination.dart")

	assertContains(t, "pagination.dart", pages,
		"Stream<Item> teamsItemsListPaginated({required String teamId, PageParams params = const PageParams()}) =>",
		"final page = await teams.items.list(teamId: teamId, cursor: p.cursor, pageSize: p.limit);",
		"return Page(page.data, nextCursor: page.next, hasMore: page.hasMore);",
		"Stream<Item> pagesListPaginated({PageParams params = const PageParams()}) =>",
		"final page = await pages.list(page: p.page, perPage: p.limit);",
		// An absent flag stays null, which the walker reads differently from false.
		"return Page(page.results, hasMore: page.hasMore);",
		// A path parameter named like the namespace needs `this`, and two
		// headers named like the walker's locals move it aside.
		"Stream<Item> offsetsListPaginated({required String offsets, required String p, required String params, PageParams params2 = const PageParams()}) =>",
		"paginateAll((p2) async {",
		"final page = await this.offsets.list(offsets: offsets, offset: p2.offset, limit: p2.limit, p: p, params: params);",
		"}, initial: params2);",
	)

	assertContains(t, "pagination.dart", pages,
		// A path parameter named page moves the response local aside.
		"Stream<Item> pagesRevisionsListPaginated({required String page, PageParams params = const PageParams()}) =>",
		"final page2 = await pages.revisions.list(page: page, cursor: p.cursor);",
		"return Page(page2.data, nextCursor: page2.next, hasMore: page2.hasMore);",
		// A call path rooted at page, p or params moves the local that would
		// shadow it, and needs no `this`.
		"final page2 = await page.list(cursor: p.cursor);",
		"Stream<Item> pListPaginated({PageParams params = const PageParams()}) =>",
		"paginateAll((p2) async {\n        final page = await p.list(cursor: p2.cursor);",
		"Stream<Item> paramsListPaginated({PageParams params2 = const PageParams()}) =>",
		"final page = await params.list(cursor: p.cursor);",
		"}, initial: params2);",
	)

	if strings.Contains(pages, "this.page.") || strings.Contains(pages, "this.p.") || strings.Contains(pages, "this.params.") {
		t.Error("a call path that no argument shadows is prefixed with this")
	}

	for _, left := range []string{"searchList", "mandatoryList", "sizedList", "summaryGet", "actionsRun"} {
		if strings.Contains(pages, left) {
			t.Errorf("pagination.dart streams %q, which the walker cannot page", left)
		}
	}
}

// TestPaginationDocumentsThatNumberedWalksNeedASeed pins the doc comment that
// tells a caller why a page or offset walk stops after the first page.
func TestPaginationDocumentsThatNumberedWalksNeedASeed(t *testing.T) {
	pages := file(t, generate(t, paginationFixture()), "lib/src/pagination.dart")

	assertContains(t, "pagination.dart", pages,
		"/// offset walk goes past the first page only when [initial] carries a page or\n/// an offset (and a limit, for an offset), because servers disagree on whether\n/// pages start at 0 or 1 and no default is right for all of them.",
	)
}

func TestPaginationIsOmittedWhenSwitchedOff(t *testing.T) {
	f := fixture(t, "default")
	f.Config.Pagination = false

	if _, ok := generate(t, f).Files["lib/src/pagination.dart"]; ok {
		t.Error("pagination.dart emitted with pagination switched off")
	}
}

func TestPaginationBarrelExportsBothFiles(t *testing.T) {
	out := generate(t, fixture(t, "default"))
	barrel := file(t, out, "lib/orders_forge_client.dart")

	assertContains(t, "barrel", barrel, "export 'src/capabilities.dart';", "export 'src/pagination.dart';")
}

// TestPaginationNamesDoNotShadowRestClientMembers keeps a Paginated variant
// from landing on a name RestClient itself already has, where the class member
// would silently win and the stream would be unreachable.
func TestPaginationNamesDoNotShadowRestClientMembers(t *testing.T) {
	f := paginationFixture()
	f.Spec.Endpoints = append(f.Spec.Endpoints, client.Endpoint{
		Method: "GET", Path: "/shadow", OperationID: "pagesListPaginated",
		QueryParams: []client.Parameter{queryParam("page", "integer", false)},
		Responses:   map[int]*client.Response{200: {Content: jsonContent(ref("NumberedPage"))}},
	})

	pages := file(t, generate(t, f), "lib/src/pagination.dart")

	assertContains(t, "pagination.dart", pages,
		"Stream<Item> pagesListPaginated2({PageParams params = const PageParams()}) =>",
		"Stream<Item> pagesListPaginatedPaginated({PageParams params = const PageParams()}) =>",
	)

	if strings.Contains(pages, "Stream<Item> pagesListPaginated(") {
		t.Error("a Paginated variant took the name of a RestClient member")
	}
}

const authzRuntimeTest = `import 'package:authz_client/authz_client.dart';
import 'package:test/test.dart';

void main() {
  tearDown(() => setPrincipal());

  test('nothing is held before a principal is declared', () {
    expect(capabilitiesKnown(), isFalse);
    expect(can(Scope.ordersRead), isFalse);
    expect(canCall(OperationName.ordersGet), isFalse);
    expect(missingCapabilities(OperationName.ordersGet), [Scope.ordersRead]);
    expect(canCall(OperationName.ordersBulk), isTrue);
  });

  test('a scope held opens the operation it gates', () {
    setPrincipal(capabilities: ['orders.read']);
    expect(capabilitiesKnown(), isTrue);
    expect(can(Scope.ordersRead), isTrue);
    expect(canCall(OperationName.ordersGet), isTrue);
    expect(missingCapabilities(OperationName.ordersGet), isEmpty);
  });

  test('the fewest missing scopes of any alternative are reported', () {
    setPrincipal(capabilities: ['orders.read']);
    expect(missingCapabilities(OperationName.ordersUpdate), hasLength(1));
    setPrincipal(capabilities: ['orders.read', 'orders.write']);
    expect(missingCapabilities(OperationName.ordersUpdate), isEmpty);
    setPrincipal(capabilities: ['orders.admin']);
    expect(missingCapabilities(OperationName.ordersUpdate), isEmpty);
    expect(canCall(OperationName.ordersUpdate), isTrue);
    setPrincipal(capabilities: <String>[]);
    expect(missingCapabilities(OperationName.ordersUpdate), hasLength(1));
  });

  test('any role and every permission are required', () {
    setPrincipal(roles: ['editor'], permissions: ['orders.delete']);
    expect(canCall(OperationName.ordersDelete), isTrue);
    setPrincipal(roles: ['viewer'], permissions: ['orders.delete']);
    expect(canCall(OperationName.ordersDelete), isFalse);
    setPrincipal(roles: ['admin'], permissions: <String>[]);
    expect(canCall(OperationName.ordersDelete), isFalse);
    expect(hasRole(Role.admin), isTrue);
    expect(hasPermission(Permission.ordersDelete), isFalse);
  });

  test('setPrincipal with no arguments forgets everything', () {
    setPrincipal(capabilities: ['orders.read'], roles: ['admin']);
    setPrincipal();
    expect(capabilitiesKnown(), isFalse);
    expect(hasRole(Role.admin), isFalse);
  });

  test('the vocabularies are sorted and the requirement tables agree', () {
    expect(Scope.values.map((s) => s.value), ['orders.admin', 'orders.read', 'orders.write']);
    expect(Role.values.map((r) => r.value), ['admin', 'editor']);
    expect(requiredAuthorization['orders.delete']!.roles, [Role.admin, Role.editor]);
    expect(requiredCapabilities.containsKey('orders.bulk'), isFalse);
  });
}
`

const pagingRuntimeTest = `import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:paging_client/paging_client.dart';
import 'package:test/test.dart';

http.Response json(Object body) =>
    http.Response(jsonEncode(body), 200, headers: {'content-type': 'application/json'});

Map<String, Object?> item(String id) => {'id': id};

void main() {
  test('a cursor walk follows next until it is empty and sends the page size', () async {
    final seen = <Uri>[];
    final client = RestClient(
      baseUrl: Uri.parse('https://api.test'),
      httpClient: MockClient((request) async {
        seen.add(request.url);
        return switch (request.url.queryParameters['cursor']) {
          null => json({'data': [item('a'), item('b')], 'next': 'c2', 'has_more': true}),
          'c2' => json({'data': [item('c')], 'has_more': false}),
          final other => throw StateError('unexpected cursor $other'),
        };
      }),
    );
    final ids = await client.teamsItemsListPaginated(teamId: 't1', params: const PageParams(limit: 2)).map((i) => i.id).toList();
    expect(ids, ['a', 'b', 'c']);
    expect(seen.map((u) => u.path), everyElement('/teams/t1/items'));
    expect(seen.map((u) => u.queryParameters['page_size']), ['2', '2']);
  });

  test('a numbered walk advances the page while the server reports more', () async {
    final pages = <String?>[];
    final client = RestClient(
      baseUrl: Uri.parse('https://api.test'),
      httpClient: MockClient((request) async {
        final page = request.url.queryParameters['page'];
        pages.add(page);
        return switch (page) {
          '1' => json({'results': [item('a')], 'has_more': true}),
          '2' => json({'results': [item('b')], 'has_more': true}),
          _ => json({'results': [item('c')]}),
        };
      }),
    );
    final ids = await client.pagesListPaginated(params: const PageParams(page: 1)).map((i) => i.id).toList();
    expect(ids, ['a', 'b', 'c']);
    expect(pages, ['1', '2', '3']);
  });

  test('an offset walk steps by the page size, and renamed locals still reach the call', () async {
    final offsets = <String?>[];
    final client = RestClient(
      baseUrl: Uri.parse('https://api.test'),
      httpClient: MockClient((request) async {
        expect(request.url.path, '/offsets/o1');
        expect(request.headers['p'], 'x');
        expect(request.headers['params'], 'y');
        final offset = request.url.queryParameters['offset'];
        offsets.add(offset);
        return offset == '0'
            ? json({'items': [item('a'), item('b')], 'hasMore': true})
            : json({'items': [item('c')], 'hasMore': false});
      }),
    );
    final ids = await client
        .offsetsListPaginated(offsets: 'o1', p: 'x', params: 'y', params2: const PageParams(offset: 0, limit: 2))
        .map((i) => i.id)
        .toList();
    expect(ids, ['a', 'b', 'c']);
    expect(offsets, ['0', '2']);
  });

  test('the stream is lazy and collectAll stops at maxItems', () async {
    var requests = 0;
    final client = RestClient(
      baseUrl: Uri.parse('https://api.test'),
      httpClient: MockClient((request) async {
        requests++;
        return json({'results': [item('a'), item('b')], 'has_more': true});
      }),
    );
    expect(await client.pagesListPaginated(params: const PageParams(page: 1)).first.then((i) => i.id), 'a');
    expect(requests, 1);
    final firstThree = await collectAll<Item>(
      (p) async {
        final page = await client.pages.list(page: p.page);
        return Page(page.results, hasMore: page.hasMore ?? false);
      },
      initial: const PageParams(page: 1),
      maxItems: 3,
    );
    expect(firstThree, hasLength(3));
  });

  test('has_more false ends the walk even when a next cursor comes with it', () async {
    var requests = 0;
    final client = RestClient(
      baseUrl: Uri.parse('https://api.test'),
      httpClient: MockClient((request) async {
        requests++;
        return json({'data': [item('a')], 'next': 'c2', 'has_more': false});
      }),
    );
    final ids = await client.teamsItemsListPaginated(teamId: 't').map((i) => i.id).toList();
    expect(ids, ['a']);
    expect(requests, 1);
  });

  test('a server that echoes the cursor it was sent ends the walk', () async {
    final cursors = <String?>[];
    final client = RestClient(
      baseUrl: Uri.parse('https://api.test'),
      httpClient: MockClient((request) async {
        cursors.add(request.url.queryParameters['cursor']);
        if (cursors.length > 5) throw StateError('the cursor was echoed without end');
        return json({'data': [item('a')], 'next': 'c1', 'has_more': true});
      }),
    );
    final ids = await client.teamsItemsListPaginated(teamId: 't').map((i) => i.id).toList();
    expect(ids, ['a', 'a']);
    expect(cursors, [null, 'c1']);
  });

  test('an empty page with has_more true goes on to the next one', () async {
    final client = RestClient(
      baseUrl: Uri.parse('https://api.test'),
      httpClient: MockClient((request) async {
        return request.url.queryParameters['cursor'] == null
            ? json({'data': <Object?>[], 'next': 'c2', 'has_more': true})
            : json({'data': [item('x')], 'has_more': false});
      }),
    );
    final ids = await client.teamsItemsListPaginated(teamId: 't').map((i) => i.id).toList();
    expect(ids, ['x']);
  });

  test('an empty page with no has_more ends the walk', () async {
    var requests = 0;
    final client = RestClient(
      baseUrl: Uri.parse('https://api.test'),
      httpClient: MockClient((request) async {
        requests++;
        return json({'results': <Object?>[]});
      }),
    );
    expect(await client.pagesListPaginated(params: const PageParams(page: 1)).toList(), isEmpty);
    expect(requests, 1);
  });

  test('a server that reports more but gives no way to advance ends the stream', () async {
    final client = RestClient(
      baseUrl: Uri.parse('https://api.test'),
      httpClient: MockClient((request) async => json({'data': [item('a')], 'has_more': true})),
    );
    final ids = await client.teamsItemsListPaginated(teamId: 't').map((i) => i.id).toList();
    expect(ids, ['a']);
  });
}
`

// runGeneratedTest writes source as test/<name>_test.dart in the generated
// package of f and runs it with fvm dart test.
func runGeneratedTest(t *testing.T, f gateFixture, name, source string) {
	t.Helper()

	fvm := requireDart(t)
	dir := writePackage(t, f)

	pubspec := filepath.Join(dir, "pubspec.yaml")

	data, err := os.ReadFile(pubspec)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(pubspec, append(data, []byte("\ndev_dependencies:\n  test: ^1.32.0\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "test"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "test", name+"_test.dart"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	runFvm(t, fvm, dir, "pub", "get")
	t.Log(runFvm(t, fvm, dir, "test"))
}

// TestGeneratedCapabilitiesRunAtRuntime proves the predicates answer as the
// requirement tables say, not only that the file compiles.
func TestGeneratedCapabilitiesRunAtRuntime(t *testing.T) {
	for _, f := range capabilitiesFixtures() {
		if f.Name == "capabilities-authz" {
			runGeneratedTest(t, f, "capabilities", authzRuntimeTest)
		}
	}
}

// TestGeneratedPaginationRunsAtRuntime walks a mock server through each
// pagination style.
func TestGeneratedPaginationRunsAtRuntime(t *testing.T) {
	runGeneratedTest(t, paginationFixture(), "pagination", pagingRuntimeTest)
}
