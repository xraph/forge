package dart

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators"
)

// fixture returns the named gate fixture.
func fixture(t *testing.T, name string) gateFixture {
	t.Helper()

	for _, f := range gateFixtures() {
		if f.Name == name {
			return f
		}
	}

	t.Fatalf("no fixture %q", name)

	return gateFixture{}
}

// generate runs the generator over a fixture and fails the test on error.
func generate(t *testing.T, f gateFixture) *generators.GeneratedClient {
	t.Helper()

	out, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
	if err != nil {
		t.Fatalf("%s: %v", f.Name, err)
	}

	return out
}

// file returns one generated file, failing when it is absent.
func file(t *testing.T, out *generators.GeneratedClient, name string) string {
	t.Helper()

	content, ok := out.Files[name]
	if !ok {
		t.Fatalf("%s not generated", name)
	}

	return content
}

func assertContains(t *testing.T, name, content string, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Errorf("%s lacks %q:\n%s", name, want, content)
		}
	}
}

func TestModelClassCarriesTypedFieldsAndValueCopyWith(t *testing.T) {
	order := file(t, generate(t, fixture(t, "default")), "lib/src/models/order.dart")

	assertContains(t, "order.dart", order,
		"final class Order {",
		"final Int64 id;",
		"final String orderNumber;",
		"final String? note;",
		"final List<LineItem> lines;",
		"final DateTime? createdAt;",
		"final Map<String, String>? metadata;",
		"final Uint8List? attachment;",
		"Value<String>? note,",
		"note: switch (note) { Assign(:final value) => value, _ => this.note },",
		"'orderNumber': orderNumber,",
		"if (note case final v?) 'note': v,",
		"deepEquals(lines, other.lines)",
		"import 'dart:typed_data';",
		"import 'line_item.dart';",
	)
}

func TestInlineTypesAreDeclaredInTheOwnersFile(t *testing.T) {
	order := file(t, generate(t, fixture(t, "default")), "lib/src/models/order.dart")

	assertContains(t, "order.dart", order,
		"final class OrderShipping {",
		"final String? streetName;",
		"extension type const OrderStatus(String wire) implements Object {",
		"static const pending = OrderStatus('pending');",
		"enum OrderStatusKnown {",
	)
}

// A schema member named unknown has no special role: it is one more declared
// value, and a server value the schema does not declare is kept as it came.
func TestEnumMemberNamedUnknownIsAnOrdinaryValue(t *testing.T) {
	state := file(t, generate(t, fixture(t, "default")), "lib/src/models/order_state.dart")

	assertContains(t, "order_state.dart", state,
		"extension type const OrderState(String wire) implements Object {",
		"static const unknown = OrderState('unknown');",
		"static const values = <OrderState>[open, closed, unknown];",
		"'unknown' => OrderStateKnown.unknown,",
	)

	for _, gone := range []string{"unknown(null)", "orElse", "firstWhere"} {
		if strings.Contains(state, gone) {
			t.Errorf("order_state.dart still has %q:\n%s", gone, state)
		}
	}

	if strings.Contains(state, "support.dart") {
		t.Errorf("an enum names nothing from support.dart, so importing it is an unused import:\n%s", state)
	}
}

func TestInt64FlagSwitchesTheRepresentation(t *testing.T) {
	order := file(t, generate(t, fixture(t, "int64-int")), "lib/src/models/order.dart")

	assertContains(t, "order.dart", order, "final int id;", "id: decodeInt(json['id']),")
}

func TestUnionsAreSealedWithAnUnknownVariant(t *testing.T) {
	out := generate(t, fixture(t, "default"))

	assertContains(t, "pet.dart", file(t, out, "lib/src/models/pet.dart"),
		"sealed class Pet {",
		"return switch (json?['petType']) {",
		"'cat' => PetCat(Cat.fromClient(client)),",
		"_ => PetUnknown(client),",
		"final class PetDog extends Pet {",
		"final class PetUnknown extends Pet {",
	)

	assertContains(t, "shape.dart", file(t, out, "lib/src/models/shape.dart"),
		"It declares no discriminator mapping",
		"if (json != null && json.containsKey('radius')) return ShapeCircle(Circle.fromClient(client));",
		"if (client is String) return ShapeOption2(client);",
		"return ShapeUnknown(json);",
	)
}

func TestAliasesBecomeTypedefs(t *testing.T) {
	out := generate(t, fixture(t, "default"))

	assertContains(t, "orders.dart", file(t, out, "lib/src/models/orders.dart"), "typedef Orders = List<Order>;", "import 'order.dart';")
	assertContains(t, "labels.dart", file(t, out, "lib/src/models/labels.dart"), "typedef Labels = Map<String, Int64>;", "import '../support.dart';")
}

func TestReservedSchemaNamesAreRenamedAndReported(t *testing.T) {
	out := generate(t, fixture(t, "reserved"))

	for path, decl := range map[string]string{
		"lib/src/models/string_model.dart":      "final class StringModel {",
		"lib/src/models/not_found_model.dart":   "final class NotFoundModel {",
		"lib/src/models/assign_model.dart":      "final class AssignModel {",
		"lib/src/models/query_state_model.dart": "extension type const QueryStateModel(String wire) implements Object {",
		"lib/src/models/class.dart":             "final class Class {",
	} {
		assertContains(t, path, file(t, out, path), decl)
	}

	warnings := strings.Join(out.Warnings, "\n")
	assertContains(t, "warnings", warnings, `schema "NotFound" is generated as NotFoundModel`, `schema "Assign" is generated as AssignModel`)
}

func TestKeywordAndObjectMemberNamesAreEscaped(t *testing.T) {
	out := generate(t, fixture(t, "reserved"))

	assertContains(t, "class.dart", file(t, out, "lib/src/models/class.dart"),
		"final String? default$;",
		"final int? hashCode$;",
		"default$: json['default'] as String?,",
	)

	assertContains(t, "query_state_model.dart", file(t, out, "lib/src/models/query_state_model.dart"),
		"static const index$ = QueryStateModel('index');", "static const name$ = QueryStateModel('name');",
		"static const values$ = QueryStateModel('values');", "static const v1st = QueryStateModel('1st');")
}

func TestSelfReferencingSchemaDecodesRecursively(t *testing.T) {
	node := file(t, generate(t, fixture(t, "default")), "lib/src/models/node.dart")

	assertContains(t, "node.dart", node,
		"final List<Node>? children;",
		"children: decodeNullable(json['children'], (v0) => decodeList(v0, (v1) => decodeCached(Node.fromClient, v1))),",
	)
}

func TestFieldNameCollisionAbortsGeneration(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Clash", Version: "1"},
		Schemas: map[string]*client.Schema{
			"Clash": {Type: "object", Properties: map[string]*client.Schema{"user_id": {Type: "string"}, "userId": {Type: "string"}}},
		},
	}

	_, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	if err == nil || !strings.Contains(err.Error(), "userId") {
		t.Fatalf("Generate = %v, want a field-name collision error", err)
	}
}

func TestSupportDependsOnForgeClientOnlyWithHooks(t *testing.T) {
	withHooks := file(t, generate(t, fixture(t, "default")), "lib/src/support.dart")
	assertContains(t, "support.dart (hooks)", withHooks, "export 'package:forge_client/forge_client.dart'")

	standalone := file(t, generate(t, fixture(t, "no-hooks")), "lib/src/support.dart")
	assertContains(t, "support.dart (no hooks)", standalone, "sealed class Value<T> {", "typedef Json = Map<String, Object?>;")

	if strings.Contains(standalone, "forge_client") {
		t.Errorf("a package without hooks must not mention forge_client:\n%s", standalone)
	}
}

// With hooks a list of models decodes each row through forge_client's
// identity memo; without hooks there is no forge_client to call.
func TestListElementsUseTheIdentityMemoOnlyWithHooks(t *testing.T) {
	withHooks := file(t, generate(t, fixture(t, "default")), "lib/src/models/order.dart")
	assertContains(t, "order.dart (hooks)", withHooks, "lines: decodeList(json['lines'], (v0) => decodeCached(LineItem.fromClient, v0)),")

	standalone := file(t, generate(t, fixture(t, "no-hooks")), "lib/src/models/order.dart")
	assertContains(t, "order.dart (no hooks)", standalone, "lines: decodeList(json['lines'], (v0) => LineItem.fromClient(v0)),")
}

// An int64 goes back to the wire in its schema's shape: an integer schema as
// a JSON number, a string schema as a JSON string, in both --int64 modes. A
// path or query value keeps the decimal string either way, so cache keys do
// not move with the schema shape.
func TestInt64EncodesByItsSchemaShape(t *testing.T) {
	spec := codecParitySpec()

	str := baseConfig()
	str.PackageName = "int64_string"
	out := generate(t, gateFixture{Name: "int64-string", Spec: spec, Config: str})

	assertContains(t, "order.dart", file(t, out, "lib/src/models/order.dart"), "'id': id.toInt(),")
	assertContains(t, "book.dart", file(t, out, "lib/src/models/book.dart"), "'pages': v.toInt(),", "'isbnCode': v.value,")
	assertContains(t, "pets_get.dart", file(t, out, "lib/src/bindings/pets_get.dart"), "path: {'petId': petId.value},")

	asInt := baseConfig()
	asInt.PackageName = "int64_int"
	asInt.Int64 = client.Int64Int
	out = generate(t, gateFixture{Name: "int64-int", Spec: codecParitySpec(), Config: asInt})

	book := file(t, out, "lib/src/models/book.dart")
	assertContains(t, "book.dart", book, "decodeIntOrString(", "'isbnCode': v.toString(),", "'pages': v,")
	assertContains(t, "pets_get.dart", file(t, out, "lib/src/bindings/pets_get.dart"), "path: {'petId': petId},")
}

// int64ListParamSpec is the codec parity spec plus one operation whose query
// parameters are lists of int64: integer items, string items, nullable items
// and a list of lists.
func int64ListParamSpec() *client.APISpec {
	spec := codecParitySpec()

	i64 := func(typ string, nullable bool) *client.Schema {
		return &client.Schema{Type: typ, Format: "int64", Nullable: nullable}
	}

	list := func(item *client.Schema) *client.Schema { return &client.Schema{Type: "array", Items: item} }

	spec.Endpoints = append(spec.Endpoints, client.Endpoint{
		Method: "GET", Path: "/books", OperationID: "books.find",
		QueryParams: []client.Parameter{
			{Name: "ids", In: "query", Schema: list(i64("integer", false))},
			{Name: "codes", In: "query", Required: true, Schema: list(i64("string", false))},
			{Name: "maybe", In: "query", Schema: list(i64("integer", true))},
			{Name: "grid", In: "query", Schema: list(list(i64("integer", false)))},
		},
		Responses: map[int]*client.Response{200: {Content: jsonContent(ref("Book"))}},
	})

	return spec
}

// int64ListParamFixtures runs int64ListParamSpec through the analyzer gate
// in both int64 modes.
func int64ListParamFixtures() []gateFixture {
	str := baseConfig()
	str.PackageName = "int64_list_string"

	asInt := baseConfig()
	asInt.PackageName = "int64_list_int"
	asInt.Int64 = client.Int64Int

	return []gateFixture{
		{Name: "int64-list-string", Spec: int64ListParamSpec(), Config: str},
		{Name: "int64-list-int", Spec: int64ListParamSpec(), Config: asInt},
	}
}

// A list of int64 in a query keeps each value in its parameter form, as a
// single int64 parameter does: the decimal string by default, so the URL
// keeps every digit on the web and the cache key does not flip from ["1"]
// to [1]; the plain int under --int64=int, whatever the item schema.
func TestInt64ListParamsKeepTheirParameterForm(t *testing.T) {
	str := baseConfig()
	str.PackageName = "int64_list_string"
	out := generate(t, gateFixture{Name: "int64-list-string", Spec: int64ListParamSpec(), Config: str})

	assertContains(t, "books_find.dart", file(t, out, "lib/src/bindings/books_find.dart"),
		"if (ids case final v?) 'ids': [for (final e0 in v) e0.value]",
		"'codes': [for (final e0 in codes) e0.value]",
		"if (maybe case final v?) 'maybe': [for (final e0 in v) encodeNullable(e0, (v1) => v1.value)]",
		"if (grid case final v?) 'grid': [for (final e0 in v) [for (final e1 in e0) e1.value]]",
	)
	assertContains(t, "rest.dart", file(t, out, "lib/src/rest.dart"),
		"'ids': encodeNullable(ids, (v0) => [for (final e1 in v0) e1.value])",
		"'codes': [for (final e0 in codes) e0.value]",
	)

	asInt := baseConfig()
	asInt.PackageName = "int64_list_int"
	asInt.Int64 = client.Int64Int
	out = generate(t, gateFixture{Name: "int64-list-int", Spec: int64ListParamSpec(), Config: asInt})

	binding := file(t, out, "lib/src/bindings/books_find.dart")
	assertContains(t, "books_find.dart", binding, "if (ids case final v?) 'ids': v", "'codes': [for (final e0 in codes) e0]")

	if strings.Contains(binding, "toString()") {
		t.Errorf("an int64 string-schema list parameter must keep its int in the tag context under --int64=int:\n%s", binding)
	}

	assertContains(t, "rest.dart", file(t, out, "lib/src/rest.dart"), "'codes': [for (final e0 in codes) e0]")

	// A map value carries its value's parameter form too.
	r := &registry{config: str}
	m := mapType(r.int64Type(true), false)

	if got := m.paramEncode("x", 0); got != "{for (final e0 in x.entries) e0.key: e0.value.value}" {
		t.Errorf("map of int64 param = %s", got)
	}

	if got := m.encode("x", 0); got != "{for (final e0 in x.entries) e0.key: e0.value.toInt()}" {
		t.Errorf("map of int64 body = %s", got)
	}
}

// The cache key a list of int64 query values produces holds strings, at
// runtime, in the default mode.
func TestInt64ListParamsKeyTheCacheAsStrings(t *testing.T) {
	cfg := baseConfig()
	cfg.PackageName = "int64_list_key"

	runGeneratedTest(t, gateFixture{Name: "int64-list-key", Spec: int64ListParamSpec(), Config: cfg}, "key", `import 'package:forge_client/forge_client.dart' show TagContext, queryKey;
import 'package:int64_list_key/int64_list_key.dart';
import 'package:test/test.dart';

void main() {
  test('int64 list query values stay decimal strings in the tag context and the key', () {
    final context = BooksFindArgs(
      codes: [Int64('1')],
      ids: [Int64('9007199254740993')],
      grid: [
        [Int64('2')],
      ],
    ).toTagContext();
    expect(context.query, {
      'ids': ['9007199254740993'],
      'codes': ['1'],
      'grid': [
        ['2'],
      ],
    });
    expect(
      queryKey(opBooksFind, context),
      queryKey(
        opBooksFind,
        const TagContext(query: {
          'ids': ['9007199254740993'],
          'codes': ['1'],
          'grid': [
            ['2'],
          ],
        }),
      ),
    );
  });
}
`)
}
