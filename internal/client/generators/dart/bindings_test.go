package dart

import (
	"regexp"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestOpsTableRowsMatchTheContract(t *testing.T) {
	out := generate(t, fixture(t, "default"))
	ops := file(t, out, "lib/src/ops.dart")

	assertContains(t, "ops.dart", ops,
		"const opOrdersGet = OperationMeta(",
		"  id: 'op_orders_get',",
		"  method: 'GET',",
		"  path: '/orders/{id}',",
		"  entity: 'Order',",
		"  rootType: 'Order',",
		"  staleTime: Duration(milliseconds: 30000),",
		"  provides: ['Order:{id}'],",
		"  security: ['bearerAuth'],",
		"  responseCodec: orderCodec,",
		"const opOrdersUpdate = OperationMeta(",
		"  method: 'PATCH',",
		"  invalidates: ['Order[]'],",
		"  bodyCodec: updateOrderRequestCodec,",
		"const Map<String, OperationMeta> operations = {",
		"  'op_orders_delete': opOrdersDelete,",
		"const EntitySchema entities = {",
		"  'Order': EntityMeta(idField: 'id'),",
		"  'OrderPage': EntityMeta(fields: {'items': 'Order'}),",
		"intent: StreamIntent.upsert,",
		"DuplexStreamBinding(channel: '/ws/chat/{roomId}', send: 'say', receive: 'said'),",
		"'bearerAuth': SecurityScheme(type: 'http', scheme: 'bearer'),",
		"export 'sync.dart' show sync;",
	)

	if got := strings.Count(ops, "idempotent: true"); got != 1 {
		t.Errorf("ops.dart carries %d idempotent rows, want 1 (orders.delete)", got)
	}
}

// The streams table comes with hooks, as the TypeScript manifest does; the
// streaming flag governs only the typed streaming clients.
func TestStreamsTableDoesNotDependOnTheStreamingFlag(t *testing.T) {
	f := fixture(t, "default")
	f.Config.IncludeStreaming = false

	out := generate(t, f)
	assertContains(t, "ops.dart", file(t, out, "lib/src/ops.dart"), "EntityStreamBinding(", "channel: '/ws/orders',")

	for name := range out.Files {
		if strings.HasPrefix(name, "lib/src/streaming/") {
			t.Errorf("%s emitted with streaming off", name)
		}
	}
}

func TestSyncTableNeedsPullAndPush(t *testing.T) {
	out := generate(t, fixture(t, "default"))
	syncFile := file(t, out, "lib/src/sync.dart")

	assertContains(t, "sync.dart", syncFile,
		"const List<SyncDeclaration> sync = [",
		"entity: 'Document',",
		"table: 'documents',",
		"socket: '/datasets/{id}/sync/ws',",
		"dataset: '{id}',",
	)

	if strings.Contains(syncFile, "Draft") {
		t.Errorf("an entity with no push endpoint must stay out of the sync table:\n%s", syncFile)
	}

	assertContains(t, "warnings", strings.Join(out.Warnings, "\n"), `entity "Draft" has no push endpoint`)
}

func TestSyncWarningNamesEveryMissingEndpoint(t *testing.T) {
	spec := minimalSpec()
	spec.Sync = []client.SyncDecl{{Protocol: "grove-crdt", Entity: "Orphan", Table: "orphans"}}

	_, warnings := renderSync(spec)
	assertContains(t, "warnings", strings.Join(warnings, "\n"), `entity "Orphan" has no pull or push endpoint`)
}

// A sync-only entity, named only by x-forge-sync and a schema, gets an
// entities row from the IR, and a row with no table omits it.
func TestSyncOnlyEntityIsKeyedAndMayOmitItsTable(t *testing.T) {
	spec := minimalSpec()
	spec.Schemas = map[string]*client.Schema{
		"DatasetRow": {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}, "cells": {Type: "object"}}},
	}
	spec.Sync = []client.SyncDecl{{Protocol: "grove-crdt", Entity: "DatasetRow", Dataset: "{id}", Pull: "/d/{id}/pull", Push: "/d/{id}/push"}}
	client.ResolveEntityFields(spec)

	out := generate(t, gateFixture{Name: "sync-only", Spec: spec, Config: baseConfig()})

	assertContains(t, "ops.dart", file(t, out, "lib/src/ops.dart"), "  'DatasetRow': EntityMeta(idField: 'id'),")

	syncFile := file(t, out, "lib/src/sync.dart")
	assertContains(t, "sync.dart", syncFile, "entity: 'DatasetRow',", "dataset: '{id}',")

	if strings.Contains(syncFile, "table:") {
		t.Errorf("a row with no table must omit it:\n%s", syncFile)
	}
}

func TestBindingsAreOneLineEach(t *testing.T) {
	out := generate(t, fixture(t, "default"))

	assertContains(t, "orders_get.dart", file(t, out, "lib/src/bindings/orders_get.dart"),
		"final ordersGet = query<Order, OrdersGetArgs>(opOrdersGet, Order.fromClient);",
		"final class OrdersGetArgs implements OperationArgs {",
		"    path: {'id': id},",
		"    query: {if (includeLines case final v?) 'include_lines': v},",
	)

	assertContains(t, "orders_list.dart", file(t, out, "lib/src/bindings/orders_list.dart"),
		"    headers: {'X-Tenant': xTenant},",
	)

	assertContains(t, "orders_delete.dart", file(t, out, "lib/src/bindings/orders_delete.dart"),
		"final ordersDelete = mutation<void, OrdersDeleteArgs, Order>(opOrdersDelete, _fromClient, entityFromClient: Order.fromClient, entityToClient: (e) => e.toClient());",
		"void _fromClient(Object? client) {}",
	)

	assertContains(t, "orders_bulk.dart", file(t, out, "lib/src/bindings/orders_bulk.dart"),
		"final ordersBulk = mutation<Orders, OrdersBulkArgs, Object?>(opOrdersBulk, _fromClient);",
		"Orders _fromClient(Object? client) => decodeList(client, (v0) => decodeCached(Order.fromClient, v0));",
		"body: [for (final e0 in body) e0.toClient()],",
	)

	assertContains(t, "get_health.dart", file(t, out, "lib/src/bindings/get_health.dart"),
		"final getHealth = query<Map<String, Object?>, NoArgs>(opGetHealth, decodeObject);")
}

// bindingCall captures the FromClient argument of a generated binding.
var bindingCall = regexp.MustCompile(`= (?:query|mutation)<.*>\(op\w+, ([^,)]+)`)

// forge_client memoizes decoded models in an Expando keyed by the binding's
// FromClient, so every binding must pass a function with a stable identity:
// a constructor tear-off, a support helper or a private top-level decoder,
// never a closure.
func TestBindingsPassTearOffsAsFromClient(t *testing.T) {
	out := generate(t, fixture(t, "default"))
	tearOff := regexp.MustCompile(`^[A-Za-z_]\w*(\.\w+)?$`)

	seen := 0

	for name, content := range out.Files {
		if !strings.HasPrefix(name, "lib/src/bindings/") {
			continue
		}

		m := bindingCall.FindStringSubmatch(content)
		if m == nil {
			t.Errorf("%s has no binding:\n%s", name, content)

			continue
		}

		seen++

		if !tearOff.MatchString(m[1]) {
			t.Errorf("%s passes %q as fromClient, want a tear-off", name, m[1])
		}
	}

	if seen != len(fixture(t, "default").Spec.Endpoints) {
		t.Errorf("checked %d bindings, want one per endpoint (%d)", seen, len(fixture(t, "default").Spec.Endpoints))
	}
}

func TestPatchArgsWrapOptionalBodyFieldsInValue(t *testing.T) {
	update := file(t, generate(t, fixture(t, "default")), "lib/src/bindings/orders_update.dart")

	assertContains(t, "orders_update.dart", update,
		"required this.note,",
		"this.state = const Unchanged(),",
		"final Value<OrderState> state;",
		"'note': note,",
		"if (state case Assign(:final value)) 'state': encodeNullable(value, (v0) => v0.wire),",
		"if (total case Assign(:final value)) 'total': value,",
		"valueEquals(state, other.state)",
		"valueHash(total)",
	)
}

// Adapters key provider families and rebuild decisions on Args, so an Args
// rebuilt from equal values must compare equal. The runtime half of this,
// == and hashCode actually agreeing, runs in TestGeneratedPackageRunsItsRoundTrip.
func TestArgsHaveValueEquality(t *testing.T) {
	get := file(t, generate(t, fixture(t, "default")), "lib/src/bindings/orders_get.dart")

	assertContains(t, "orders_get.dart", get,
		"bool operator ==(Object other) =>",
		"other is OrdersGetArgs &&",
		"id == other.id &&",
		"includeLines == other.includeLines;",
		"int get hashCode => Object.hashAll([id, includeLines]);",
	)

	out := generate(t, fixture(t, "default"))

	// Lists, maps and bytes compare by content, whatever the body kind.
	for _, name := range []string{"orders_bulk", "uploads_create", "raw_create"} {
		assertContains(t, name+".dart", file(t, out, "lib/src/bindings/"+name+".dart"),
			"deepEquals(body, other.body)", "deepHash(body)")
	}
}

// A member named like the parameter of a generated == is read through this,
// in Args and models alike; the runtime half is in the round trip.
func TestEqualityQualifiesAMemberNamedOther(t *testing.T) {
	out := generate(t, fixture(t, "default"))

	assertContains(t, "pets_get.dart", file(t, out, "lib/src/bindings/pets_get.dart"), "this.other == other.other")
	assertContains(t, "orders_update.dart", file(t, out, "lib/src/bindings/orders_update.dart"), "valueEquals(this.other, other.other)")
	assertContains(t, "update_order_request.dart", file(t, out, "lib/src/models/update_order_request.dart"),
		"this.other == other.other", "note == other.note")
}

func TestNoHooksMeansNoOpsNoBindingsAndNoForgeClient(t *testing.T) {
	out := generate(t, fixture(t, "no-hooks"))

	for name, content := range out.Files {
		if name == "lib/src/ops.dart" || name == "lib/src/sync.dart" || strings.HasPrefix(name, "lib/src/bindings/") {
			t.Errorf("%s emitted without hooks", name)
		}

		if strings.Contains(content, "package:forge_client") {
			t.Errorf("%s imports forge_client without hooks", name)
		}
	}

	pubspec := file(t, out, "pubspec.yaml")
	if strings.Contains(pubspec, "  forge_client:") {
		t.Errorf("pubspec without hooks must depend on http only:\n%s", pubspec)
	}

	assertContains(t, "pubspec.yaml", pubspec, "  http: ^1.6.0")
}
