package dart

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gateAnalysisOptions holds generated code to the contracts' analyzer
// baseline plus the core lints that matter for generated output. The rules are
// listed directly, so the package needs no dev dependency on package:lints.
const gateAnalysisOptions = `analyzer:
  language:
    strict-casts: true
    strict-inference: true
    strict-raw-types: true
linter:
  rules:
    - annotate_overrides
    - avoid_dynamic_calls
    - avoid_init_to_null
    - camel_case_types
    - constant_identifier_names
    - empty_constructor_bodies
    - library_private_types_in_public_api
    - non_constant_identifier_names
    - prefer_collection_literals
    - prefer_const_constructors
    - prefer_final_fields
    - prefer_final_locals
    - prefer_is_empty
    - public_member_api_docs
    - type_init_formals
    - unawaited_futures
    - unnecessary_const
    - unnecessary_new
    - unnecessary_this
    - use_super_parameters
`

// forgeClientDir is dart-packages/forge_client in this repository. Generated
// code is analyzed against it through a pubspec_overrides.yaml, the same
// mechanism a consuming repository uses to point a generated package at a
// local runtime.
func forgeClientDir(t *testing.T) string {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "dart-packages", "forge_client"))
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

// requireDart skips the test only when fvm is not on PATH. The runtime ships
// in this repository, so a missing dart-packages/forge_client is a broken
// checkout and fails the test instead of skipping it: a gate that quietly
// stops compiling generated code against the real core is no gate. CI does
// not run this gate until plan 08 adds it, so until then it runs wherever a
// developer has fvm.
func requireDart(t *testing.T) string {
	t.Helper()

	fvm, err := exec.LookPath("fvm")
	if err != nil {
		t.Skip("fvm not found on PATH; skipping the Dart analyzer gate (install fvm to run it)")
	}

	if _, err := os.Stat(filepath.Join(forgeClientDir(t), "pubspec.yaml")); err != nil {
		t.Fatalf("dart-packages/forge_client/pubspec.yaml is missing, so generated code cannot be compiled against the real core: %v", err)
	}

	return fvm
}

// writePackage generates f into a temp directory set up to resolve and
// analyze: Flutter pinned through .fvmrc, forge_client overridden to the
// local package, and the gate's analysis options.
func writePackage(t *testing.T, f gateFixture) string {
	t.Helper()

	out, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
	if err != nil {
		t.Fatalf("%s: generate: %v", f.Name, err)
	}

	dir := t.TempDir()
	files := map[string]string{
		".fvmrc":                `{"flutter": "3.47.5"}` + "\n",
		"analysis_options.yaml": gateAnalysisOptions,
	}

	maps.Copy(files, out.Files)

	if f.Config.HooksEnabled() {
		files["pubspec_overrides.yaml"] = "dependency_overrides:\n  forge_client:\n    path: " + forgeClientDir(t) + "\n"
	}

	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// runFvm runs `fvm dart <args>` in dir and fails the test with its output
// when it exits non-zero.
func runFvm(t *testing.T, fvm, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), fvm, append([]string{"dart"}, args...)...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fvm dart %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}

	return string(out)
}

// TestGeneratedPackagesAnalyzeClean is the Dart counterpart of the
// TypeScript tsc gate: every fixture's output resolves and passes
// `dart analyze --fatal-infos`.
func TestGeneratedPackagesAnalyzeClean(t *testing.T) {
	fvm := requireDart(t)

	fixtures := append(gateFixtures(), enumsFixture(), restFixture(), restHooksFixture(), paginationFixture(), streamingFixture())
	fixtures = append(fixtures, capabilitiesFixtures()...)
	fixtures = append(fixtures, int64ListParamFixtures()...)

	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			dir := writePackage(t, f)
			runFvm(t, fvm, dir, "pub", "get")
			runFvm(t, fvm, dir, "analyze", "--fatal-infos")
		})
	}
}

// roundTripTest exercises the default fixture's generated code at runtime:
// codecs, models, unions, enums, int64, copyWith, PATCH arguments, Args
// equality and the tag context an Args hands the cache.
const roundTripTest = `import 'dart:typed_data';

import 'package:forge_client/forge_client.dart' show TagContext, queryKey;
import 'package:orders_forge_client/orders_forge_client.dart';
import 'package:orders_forge_client/src/codecs/node_codec.dart';
import 'package:orders_forge_client/src/codecs/order_codec.dart';
import 'package:orders_forge_client/src/codecs/pet_codec.dart';
import 'package:orders_forge_client/src/codecs/shape_codec.dart';
import 'package:test/test.dart';

Map<String, Object?> orderJson({String status = 'pending', int qty = 2}) => <String, Object?>{
  'id': '1',
  'orderNumber': 'n',
  'status': status,
  'lines': <Object?>[
    <String, Object?>{'sku': 'x', 'qty': qty},
  ],
  'metadata': <String, Object?>{'k': 'v'},
};

void main() {
  test('an order decodes from the wire and encodes back, int64 beyond 2^53 intact', () {
    // id is {type: integer, format: int64}: a JSON number on the wire, and a
    // JSON number again on the way out, exactly as it arrived.
    final wire = <String, Object?>{
      'id': 9007199254740993,
      'order_number': 'A-1',
      'status': 'shipped',
      'lines': [
        {'sku': 'x', 'qty': 2},
      ],
      'shipping': {'street_name': 'Main'},
    };
    final order = Order.fromClient(orderCodec.decode(wire));
    expect(order.id.value, '9007199254740993');
    expect(order.id.toBigInt(), BigInt.parse('9007199254740993'));
    expect(order.id.toBigInt() > BigInt.two.pow(53), isTrue);
    expect(order.orderNumber, 'A-1');
    expect(order.status, OrderStatus.shipped);
    expect(order.lines.single.qty, 2);
    expect(order.shipping?.streetName, 'Main');
    expect(order.toClient()['id'], isA<int>());
    expect(orderCodec.encode(order.toClient()), <String, Object?>{
      'id': 9007199254740993,
      'lines': [
        {'qty': 2, 'sku': 'x'},
      ],
      'order_number': 'A-1',
      'shipping': {'street_name': 'Main'},
      'status': 'shipped',
    });
  });

  test('an unknown enum value is kept and re-encoded as it arrived', () {
    final order = Order.fromClient(orderJson(status: 'lost'));
    expect(order.status.wire, 'lost');
    expect(order.status.isKnown, isFalse);
    expect(order.status.known, isNull);
    expect(order.toClient()['status'], 'lost');
    expect((orderCodec.encode(order.toClient())! as Map<Object?, Object?>)['status'], 'lost');
    expect(const OrderState('archived').isKnown, isFalse);
    expect(OrderState.unknown.isKnown, isTrue);
  });

  test('copyWith tells clearing from leaving unchanged', () {
    final order = Order.fromClient(<String, Object?>{
      'id': '1', 'orderNumber': 'n', 'status': 'pending', 'lines': <Object?>[], 'note': 'keep',
    });
    expect(order.copyWith().note, 'keep');
    expect(order.copyWith(note: const Assign(null)).note, isNull);
    expect(order.copyWith(), order);
    expect(order.copyWith().hashCode, order.hashCode);
  });

  test('list and map fields compare and hash deeply', () {
    final a = Order.fromClient(orderJson());
    final b = Order.fromClient(orderJson());
    expect(identical(a.lines, b.lines), isFalse);
    expect(a, b);
    expect(a.hashCode, b.hashCode);
    expect(Order.fromClient(orderJson(qty: 3)), isNot(a));
    expect(a.copyWith(metadata: const Assign({'k': 'w'})), isNot(a));
  });

  test('a recursive schema decodes and encodes', () {
    final wire = <String, Object?>{
      'label': 'root',
      'children': [
        {
          'label': 'leaf',
          'children': <Object?>[],
        },
      ],
    };
    final node = Node.fromClient(nodeCodec.decode(wire));
    expect(node.children!.single.label, 'leaf');
    expect(node.children!.single.children, isEmpty);
    expect(nodeCodec.encode(node.toClient()), wire);
  });

  test('a discriminated union picks its variant by tag', () {
    final pet = Pet.fromClient(petCodec.decode(<String, Object?>{'pet_type': 'dog', 'barks': true}));
    expect(pet, isA<PetDog>());
    expect(petCodec.encode(pet.toClient()), <String, Object?>{'barks': true, 'pet_type': 'dog'});
    final fish = Pet.fromClient(<String, Object?>{'petType': 'fish'});
    expect(fish, isA<PetUnknown>());
    expect(fish.toClient(), <String, Object?>{'petType': 'fish'});
  });

  test('an undiscriminated union matches structurally and never throws', () {
    expect(Shape.fromClient(shapeCodec.decode(<String, Object?>{'side': 2})), isA<ShapeSquare>());
    expect(Shape.fromClient('round'), isA<ShapeOption2>());
    expect(Shape.fromClient(<String, Object?>{'other': 1}), isA<ShapeUnknown>());
  });

  test('PATCH args omit unchanged fields and send null for Assign(null)', () {
    const args = OrdersUpdateArgs(id: '7', note: null, total: Assign(null));
    expect(args.toTagContext().body, <String, Object?>{'note': null, 'total': null});
    const state = OrdersUpdateArgs(id: '7', note: 'n', state: Assign(OrderState.closed));
    expect(state.toTagContext().body, <String, Object?>{'note': 'n', 'state': 'closed'});
  });

  test('a null optional parameter is left out of the tag context', () {
    final context = const OrdersGetArgs(id: '7').toTagContext();
    expect(queryKey(opOrdersGet, context), queryKey(opOrdersGet, const TagContext(path: {'id': '7'})));
    expect(context.path, <String, Object?>{'id': '7'});
    expect(context.query, isEmpty);
    expect(const OrdersGetArgs(id: '7', includeLines: true).toTagContext().query, <String, Object?>{'include_lines': true});
  });

  test('args built from equal values are equal', () {
    expect(const OrdersGetArgs(id: '7', includeLines: true), const OrdersGetArgs(id: '7', includeLines: true));
    expect(
      const OrdersGetArgs(id: '7', includeLines: true).hashCode,
      const OrdersGetArgs(id: '7', includeLines: true).hashCode,
    );
    const assigned = OrdersUpdateArgs(id: '7', note: 'n', total: Assign(null));
    expect(assigned, const OrdersUpdateArgs(id: '7', note: 'n', total: Assign(null)));
    expect(assigned.hashCode, const OrdersUpdateArgs(id: '7', note: 'n', total: Assign(null)).hashCode);
    expect(const OrdersUpdateArgs(id: '7', note: 'n'), isNot(assigned));
    final line = Order.fromClient(orderJson());
    expect(OrdersBulkArgs(body: [line]), OrdersBulkArgs(body: [Order.fromClient(orderJson())]));
    expect(OrdersBulkArgs(body: [line]).hashCode, OrdersBulkArgs(body: [Order.fromClient(orderJson())]).hashCode);
    expect(UploadsCreateArgs(body: {'a': 'b'}), UploadsCreateArgs(body: {'a': 'b'}));
    expect(UploadsCreateArgs(body: {'a': 'b'}).hashCode, UploadsCreateArgs(body: {'a': 'b'}).hashCode);
    expect(RawCreateArgs(body: Uint8List.fromList([1, 2])), RawCreateArgs(body: Uint8List.fromList([1, 2])));
    expect(RawCreateArgs(body: Uint8List.fromList([1, 2])).hashCode, RawCreateArgs(body: Uint8List.fromList([1, 2])).hashCode);
    expect(RawCreateArgs(body: Uint8List.fromList([1, 2])), isNot(RawCreateArgs(body: Uint8List.fromList([1, 3]))));
  });

  // Instances are built without const: equal const instances are identical,
  // and a model's == short-circuits on identical before reading any member.
  test('a member named other takes part in equality', () {
    expect(PetsGetArgs(petId: Int64('1'), other: 'a'), PetsGetArgs(petId: Int64('1'), other: 'a'));
    expect(PetsGetArgs(petId: Int64('1'), other: 'a').hashCode, PetsGetArgs(petId: Int64('1'), other: 'a').hashCode);
    expect(PetsGetArgs(petId: Int64('1'), other: 'a'), isNot(PetsGetArgs(petId: Int64('1'), other: 'b')));
    expect(OrdersUpdateArgs(id: '7', note: 'n', other: Assign('a')), OrdersUpdateArgs(id: '7', note: 'n', other: Assign('a')));
    expect(OrdersUpdateArgs(id: '7', note: 'n', other: Assign('a')), isNot(OrdersUpdateArgs(id: '7', note: 'n', other: Assign('b'))));
    expect(UpdateOrderRequest(note: 'n', other: 'a'), UpdateOrderRequest(note: 'n', other: 'a'));
    expect(UpdateOrderRequest(note: 'n', other: 'a').hashCode, UpdateOrderRequest(note: 'n', other: 'a').hashCode);
    expect(UpdateOrderRequest(note: 'n', other: 'a'), isNot(UpdateOrderRequest(note: 'n', other: 'b')));
  });

  test('bindings decode through stable tear-offs and cache list rows', () {
    expect(identical(ordersGet.fromClient, Order.fromClient), isTrue);
    final row = orderJson();
    final first = ordersBulk.fromClient(<Object?>[row]);
    final second = ordersBulk.fromClient(<Object?>[row, orderJson(qty: 9)]);
    expect(identical(first.single, second.first), isTrue);
  });
}
`

// TestGeneratedPackageRunsItsRoundTrip runs roundTripTest against the
// default fixture with `dart test`, proving the generated code behaves, not
// only that it compiles.
func TestGeneratedPackageRunsItsRoundTrip(t *testing.T) {
	fvm := requireDart(t)

	f := gateFixtures()[0]
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

	if err := os.WriteFile(filepath.Join(dir, "test", "roundtrip_test.dart"), []byte(roundTripTest), 0o644); err != nil {
		t.Fatal(err)
	}

	runFvm(t, fvm, dir, "pub", "get")
	t.Log(runFvm(t, fvm, dir, "test"))
}
