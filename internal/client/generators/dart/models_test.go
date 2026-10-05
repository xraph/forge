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
