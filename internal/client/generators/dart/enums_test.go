package dart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// enumsFixture holds an enum in every position a model decodes one: a direct
// required field, a nullable field, a list element and a map value (nullable
// and not), a numeric enum, a mixed-type enum and a declared `unknown` member.
func enumsFixture() gateFixture {
	color := ref("Color")
	nullableColor := &client.Schema{Ref: color.Ref, Nullable: true}

	cfg := baseConfig()
	cfg.PackageName = "enums_client"

	return gateFixture{
		Name:   "enums",
		Config: cfg,
		Spec: &client.APISpec{
			Info: client.APIInfo{Title: "Enums API", Version: "1"},
			Schemas: map[string]*client.Schema{
				"Color": {Type: "string", Enum: []any{"red", "green"}},
				"Holder": {
					Type:     "object",
					Required: []string{"status", "level"},
					Properties: map[string]*client.Schema{
						"status":  {Type: "string", Enum: []any{"pending", "shipped", "unknown"}},
						"level":   {Type: "integer", Enum: []any{float64(1), float64(2), float64(3)}},
						"note":    color,
						"colors":  {Type: "array", Items: color},
						"by_name": {Type: "object", AdditionalProperties: nullableColor},
						"labels":  {Type: "object", AdditionalProperties: &client.Schema{Type: "string", Nullable: true}},
						"mixed":   {Enum: []any{"a", float64(1), true}},
					},
				},
			},
			Endpoints: []client.Endpoint{{
				Method: "GET", Path: "/holder", OperationID: "holder.get",
				Responses: map[int]*client.Response{200: {Content: jsonContent(ref("Holder"))}},
			}},
		},
	}
}

func TestEnumIsAnExtensionTypeOverItsWireValue(t *testing.T) {
	out := generate(t, enumsFixture())

	assertContains(t, "color.dart", file(t, out, "lib/src/models/color.dart"),
		"extension type const Color(String wire) implements Object {",
		"static const red = Color('red');",
		"static const values = <Color>[red, green];",
		"bool get isKnown => values.contains(this);",
		"ColorKnown? get known => switch (wire) {",
		"'red' => ColorKnown.red,",
		"enum ColorKnown {",
	)

	holder := file(t, out, "lib/src/models/holder.dart")
	assertContains(t, "holder.dart", holder,
		"extension type const HolderLevel(Object wire) implements Object {",
		"static const v1 = HolderLevel(1);",
		"extension type const HolderMixed(Object wire) implements Object {",
		"static const true$ = HolderMixed(true);",
		// A member literally named unknown is an ordinary known value.
		"static const unknown = HolderStatus('unknown');",
		"status: HolderStatus(json['status'] as String),",
		"level: HolderLevel(json['level'] as Object),",
		"note: decodeNullable(json['note'], (v0) => Color(v0 as String)),",
		"colors: decodeNullable(json['colors'], (v0) => decodeList(v0, (v1) => Color(v1 as String))),",
		"'status': status.wire,",
		"if (note case final v?) 'note': v.wire,",
	)

	for _, gone := range []string{"unknown(null)", "orElse", "firstWhere"} {
		if strings.Contains(holder, gone) {
			t.Errorf("holder.dart still has %q:\n%s", gone, holder)
		}
	}
}

func TestNullableMapValuesDecodeAndEncodeNull(t *testing.T) {
	holder := file(t, generate(t, enumsFixture()), "lib/src/models/holder.dart")

	assertContains(t, "holder.dart", holder,
		"final Map<String, String?>? labels;",
		"labels: decodeNullable(json['labels'], (v0) => decodeMap(v0, (v1) => v1 as String?)),",
		"final Map<String, Color?>? byName;",
		"decodeMap(v0, (v1) => decodeNullable(v1, (v2) => Color(v2 as String)))",
		"encodeNullable(e0.value, (v1) => v1.wire)",
	)
}

// probe is a Dart program run against the generated package. It exits
// non-zero when a value an enum does not know fails to survive fromClient and
// toClient unchanged.
const enumsProbe = `import 'dart:convert';

import 'package:enums_client/enums_client.dart';

void check(bool ok, String what) {
  if (!ok) throw StateError(what);
}

String colorName(Color color) => switch (color.known) {
  null => 'unknown:${color.wire}',
  ColorKnown.red => 'red',
  ColorKnown.green => 'green',
};

void main() {
  final source = <String, Object?>{
    'byName': {'a': 'teal', 'b': null, 'c': 'red'},
    'colors': ['red', 'teal'],
    'labels': {'x': null, 'y': 'z'},
    'level': 9,
    'mixed': 'zzz',
    'note': 'teal',
    'status': 'archived',
  };

  final holder = Holder.fromClient(source);

  check(!holder.status.isKnown && holder.status.known == null, 'status is unknown');
  check(holder.note?.isKnown == false, 'note is unknown');
  check(holder.colors?.map((c) => c.isKnown).toList().toString() == '[true, false]', 'list known flags');
  check(colorName(holder.colors![1]) == 'unknown:teal', 'switch over known');
  check(holder.byName!['b'] == null && holder.byName!.containsKey('b'), 'null map value kept');
  check(holder.labels!['x'] == null && holder.labels!['y'] == 'z', 'nullable string map');

  check(jsonEncode(holder.toClient()) == jsonEncode(source), 'unknown values round trip: ${jsonEncode(holder.toClient())}');

  final known = Holder.fromClient({'level': 2, 'status': 'unknown'});
  check(known.status == HolderStatus.unknown && known.status.isKnown, 'declared unknown is a known value');
  check(known.status.known == HolderStatusKnown.unknown, 'declared unknown maps to its enum member');
  check(known.level == HolderLevel.v2 && known.level.known == HolderLevelKnown.v2, 'numeric enum');
  check(known.toClient()['status'] == 'unknown', 'declared unknown encodes as itself');
  check(known.copyWith(note: const Assign(Color.green)).note == Color.green, 'copyWith');
}
`

// TestUnknownEnumValuesRoundTripUnchanged runs the generated package: an
// unknown value reaches the model and goes back out as it came in, in a direct
// field, a nullable field, a list element and a map value.
func TestUnknownEnumValuesRoundTripUnchanged(t *testing.T) {
	fvm := requireDart(t)
	dir := writePackage(t, enumsFixture())

	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "bin", "probe.dart"), []byte(enumsProbe), 0o644); err != nil {
		t.Fatal(err)
	}

	runFvm(t, fvm, dir, "pub", "get")
	runFvm(t, fvm, dir, "analyze", "--fatal-infos")
	runFvm(t, fvm, dir, "run", "bin/probe.dart")
}
