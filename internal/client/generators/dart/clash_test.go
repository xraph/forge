package dart

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// clashNames are property and parameter names that would break a generated
// class if it declared a member under the name as written: Dart reserved
// words, built-in identifiers, the members every Object has, the members the
// generated classes add, and the lowercase names their bodies call or annotate
// with. They are the client names; the wire names are their snake_case forms,
// so a renamed member is also told apart from its key.
var clashNames = []string{
	"override", "identical", "hashCode", "runtimeType", "toString", "noSuchMethod",
	"copyWith", "toJson", "fromJson", "toClient", "fromClient", "toTagContext",
	"deepEquals", "deepHash", "valueEquals", "valueHash", "decodeObject", "decodeList", "encodeDate",
	"dynamic", "int", "other",
	"class", "default", "new", "this", "null",
	"abstract", "as", "covariant", "deferred", "export", "extension", "external", "factory", "get",
	"implements", "import", "interface", "late", "library", "mixin", "operator", "part", "required",
	"set", "static", "typedef",
	"core", "dartCore", "json", "client", "value", "v",
}

// clashWire is a name's wire spelling.
func clashWire(name string) string { return toSnake(name) }

// clashKind picks a property type from the name's position, so the fixture
// covers a string, an integer and a list of strings.
func clashKind(i int) *client.Schema {
	switch i % 3 {
	case 0:
		return &client.Schema{Type: "string"}
	case 1:
		return &client.Schema{Type: "integer"}
	default:
		return &client.Schema{Type: "array", Items: &client.Schema{Type: "string"}}
	}
}

// clashFixture names a model's fields, an operation's query parameters and a
// PATCH body's fields after every name in clashNames. Four of the model's
// fields are required, the rest optional.
func clashFixture() gateFixture {
	props := map[string]*client.Schema{}
	patch := map[string]*client.Schema{}

	var query []client.Parameter

	for i, name := range clashNames {
		props[clashWire(name)] = clashKind(i)
		patch[clashWire(name)] = &client.Schema{Type: "string"}
		query = append(query, client.Parameter{Name: clashWire(name), In: "query", Schema: &client.Schema{Type: "string"}})
	}

	cfg := baseConfig()
	cfg.PackageName = "clash_client"

	return gateFixture{
		Name: "clash",
		Spec: &client.APISpec{
			Info: client.APIInfo{Title: "Clash API", Version: "1"},
			Schemas: map[string]*client.Schema{
				"Clash": {
					Type: "object", Properties: props,
					Required: []string{"override", "identical", clashWire("hashCode"), clashWire("toString")},
				},
				"ClashPatch": {Type: "object", Required: []string{"override"}, Properties: patch},
			},
			Endpoints: []client.Endpoint{
				{
					Method: "GET", Path: "/clash", OperationID: "clash.get",
					QueryParams: query,
					Responses:   map[int]*client.Response{200: {Content: jsonContent(ref("Clash"))}},
				},
				{
					Method: "PATCH", Path: "/clash", OperationID: "clash.update",
					RequestBody: &client.RequestBody{Required: true, Content: jsonContent(ref("ClashPatch"))},
					Responses:   map[int]*client.Response{200: {Content: jsonContent(ref("Clash"))}},
				},
			},
		},
		Config: cfg,
	}
}

// clashValue is the wire value the round trip gives the i-th clash name.
func clashValue(i int) any {
	switch i % 3 {
	case 0:
		return fmt.Sprintf("s%d", i)
	case 1:
		return i
	default:
		return []string{fmt.Sprintf("l%d", i)}
	}
}

func TestClashingFieldNamesGetSafeMembersAndKeepTheirKeys(t *testing.T) {
	out := generate(t, clashFixture())
	model := file(t, out, "lib/src/models/clash.dart")

	// Each of these is a name the class would otherwise break on: a member
	// every Object has, a member the class adds, or a name its own body calls.
	for name, member := range map[string]string{
		"override": "override$", "identical": "identical$", "hashCode": "hashCode$", "toString": "toString$",
		"copyWith": "copyWith$", "toJson": "toJson$", "fromJson": "fromJson$", "runtimeType": "runtimeType$",
		"noSuchMethod": "noSuchMethod$", "toClient": "toClient$", "fromClient": "fromClient$",
		"deepEquals": "deepEquals$", "deepHash": "deepHash$", "decodeObject": "decodeObject$",
		"dynamic": "dynamic$", "int": "int$",
	} {
		declared := regexp.MustCompile(`final [\w<>?]+ ` + regexp.QuoteMeta(member) + `;`)
		if !declared.MatchString(model) {
			t.Errorf("clash.dart declares no field %s for %q", member, name)
		}

		bare := regexp.MustCompile(`final [\w<>?]+ ` + name + `;`)
		if bare.MatchString(model) {
			t.Errorf("clash.dart declares a field named %q as written", name)
		}

		// The client-shaped key is the name itself, never the member.
		if !strings.Contains(model, "'"+name+"'") {
			t.Errorf("clash.dart never uses %q as a key", name)
		}
	}

	// The wire key lives in the codec, untouched by the member's name.
	codec := file(t, out, "lib/src/codecs/clash_codec.dart")
	assertContains(t, "clash_codec.dart", codec,
		"'override': CodecField('override')",
		"'copy_with': CodecField('copyWith'",
		"'to_json': CodecField('toJson'",
		"'runtime_type': CodecField('runtimeType'",
		"'hash_code': CodecField('hashCode'",
	)
}

// The generated classes reach dart:core through a prefix no member can take, so
// a field can never shadow what they annotate or compare with. Every model and
// Args file that uses the prefix declares it, and none writes the bare name.
func TestGeneratedClassesReachCoreThroughAPrefix(t *testing.T) {
	bareOverride := regexp.MustCompile(`@override\b`)
	bareIdentical := regexp.MustCompile(`(^|[^.\w])identical\(`)

	for _, f := range append(gateFixtures(), clashFixture(), restFixture(), paginationFixture()) {
		out := generate(t, f)

		for name, content := range out.Files {
			if !strings.HasPrefix(name, "lib/src/models/") && !strings.HasPrefix(name, "lib/src/bindings/") {
				continue
			}

			if bareOverride.MatchString(content) {
				t.Errorf("%s: %s writes a bare @override", f.Name, name)
			}

			if bareIdentical.MatchString(content) {
				t.Errorf("%s: %s calls a bare identical", f.Name, name)
			}

			uses := strings.Contains(content, "dart_core.")
			imports := strings.Contains(content, "import 'dart:core' as dart_core;")

			if uses != imports {
				t.Errorf("%s: %s uses the prefix: %v, imports it: %v", f.Name, name, uses, imports)
			}

			if imports && !strings.Contains(content, "import 'dart:core';\n") {
				t.Errorf("%s: %s prefixes dart:core without importing it plain, which hides int and String", f.Name, name)
			}
		}
	}

	model := file(t, generate(t, clashFixture()), "lib/src/models/clash.dart")
	assertContains(t, "clash.dart", model, "@dart_core.override", "dart_core.identical(this, other) ||")
}

// A member name never holds an underscore, which is what keeps the prefix out
// of reach of every name the API can supply.
func TestNoMemberNameCanBeTheCorePrefix(t *testing.T) {
	for _, raw := range []string{"dart_core", "dart-core", "dartCore", "DART_CORE", "_dart_core", "dart core"} {
		for _, reserved := range []map[string]bool{modelReserved, argsReserved, enumReserved} {
			if got := memberIdent(raw, reserved); got == corePrefix || strings.Contains(got, "_") {
				t.Errorf("memberIdent(%q) = %q, which can take the place of the %s prefix", raw, got, corePrefix)
			}
		}

		if got := typeIdent(raw); got == corePrefix || strings.Contains(got, "_") {
			t.Errorf("typeIdent(%q) = %q", raw, got)
		}
	}
}

// clashRoundTripTest decodes a model whose every field is named after
// something Dart or the generated code uses, from the wire, and encodes it
// back: the wire keys must come out exactly as they went in.
func clashRoundTripTest(wire string) string {
	return `import 'dart:convert';

import 'package:clash_client/clash_client.dart';
import 'package:clash_client/src/codecs/clash_codec.dart';
import 'package:test/test.dart';

final Map<String, Object?> wire = jsonDecode(r'''` + wire + `''') as Map<String, Object?>;

void main() {
  test('a model with fields named like Object members decodes and encodes under its wire keys', () {
    final model = Clash.fromClient(clashCodec.decode(wire));
` + clashExpectations() + `    expect(clashCodec.encode(model.toClient()), wire);
    // The real Object members still work.
    expect(model.toString(), isA<String>());
    expect(model.hashCode, isA<int>());
    expect(model.runtimeType, Clash);
  });

  test('equality, hashCode and copyWith read the renamed members', () {
    final a = Clash.fromClient(clashCodec.decode(wire));
    final b = Clash.fromClient(clashCodec.decode(wire));
    expect(a, b);
    expect(a.hashCode, b.hashCode);
    expect(a.copyWith(), a);

    final changed = a.copyWith(override$: 'x', copyWith$: const Assign('y'));
    expect(changed.override$, 'x');
    expect(changed.copyWith$, 'y');
    expect(changed.identical$, a.identical$);
    expect(changed, isNot(a));
    expect(a.copyWith(toJson$: const Assign(null)).toJson$, isNull);
  });

  test('a member named other still takes part in equality', () {
    final a = Clash.fromClient(clashCodec.decode(wire));
    expect(a.other, isNotNull);
    expect(a.copyWith(other: const Assign('z')), isNot(a));
    expect(a.copyWith(other: Assign(a.other)), a);
  });

  test('Args named like core members build their tag context under the wire names', () {
    const args = ClashGetArgs(override$: 'o', copyWith: 'c', toJson: 'j', hashCode$: 'h', deepEquals$: 'd');
    expect(args.toTagContext().query, <String, Object?>{
      'override': 'o', 'copy_with': 'c', 'to_json': 'j', 'hash_code': 'h', 'deep_equals': 'd',
    });
    expect(args, const ClashGetArgs(override$: 'o', copyWith: 'c', toJson: 'j', hashCode$: 'h', deepEquals$: 'd'));
    expect(args.hashCode, const ClashGetArgs(override$: 'o', copyWith: 'c', toJson: 'j', hashCode$: 'h', deepEquals$: 'd').hashCode);
    expect(args, isNot(const ClashGetArgs(override$: 'o', copyWith: 'c', toJson: 'k', hashCode$: 'h', deepEquals$: 'd')));

    const patch = ClashUpdateArgs(override$: 'o', identical$: Assign('i'), valueEquals$: Assign(null));
    expect(patch.toTagContext().body, <String, Object?>{'override': 'o', 'identical': 'i', 'valueEquals': null});
    expect(patch, const ClashUpdateArgs(override$: 'o', identical$: Assign('i'), valueEquals$: Assign(null)));
    expect(patch, isNot(const ClashUpdateArgs(override$: 'o', identical$: Assign('j'), valueEquals$: Assign(null))));
  });
}
`
}

// clashExpectations asserts, in Dart, the value each of a few clash fields
// decodes to, under the member name it is generated with.
func clashExpectations() string {
	var b strings.Builder

	for _, name := range []string{"override", "identical", "hashCode", "toString", "runtimeType", "copyWith", "toJson", "fromJson", "get", "required", "other"} {
		i := slices.Index(clashNames, name)

		var literal string

		switch v := clashValue(i).(type) {
		case string:
			literal = "'" + v + "'"
		case int:
			literal = strconv.Itoa(v)
		case []string:
			literal = "['" + v[0] + "']"
		}

		fmt.Fprintf(&b, "    expect(model.%s, %s);\n", memberIdent(name, modelReserved), literal)
	}

	return b.String()
}

// writeDartTest adds a `dart test` file to a generated package.
func writeDartTest(t *testing.T, dir, name, source string) {
	t.Helper()

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

	if err := os.WriteFile(filepath.Join(dir, "test", name), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestClashingFieldsRoundTripUnderTheirWireKeys runs the clash fixture's
// generated code: renamed members decode, encode, compare and copy, and the
// JSON keys that come out are the ones that went in.
func TestClashingFieldsRoundTripUnderTheirWireKeys(t *testing.T) {
	fvm := requireDart(t)

	dir := writePackage(t, clashFixture())

	wire := map[string]any{}

	for i, name := range clashNames {
		wire[clashWire(name)] = clashValue(i)
	}

	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}

	writeDartTest(t, dir, "clash_test.dart", clashRoundTripTest(string(encoded)))

	runFvm(t, fvm, dir, "pub", "get")
	t.Log(runFvm(t, fvm, dir, "test"))
}
