package dart

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestCodecFilesRenameWireFieldsToClientNames(t *testing.T) {
	order := file(t, generate(t, fixture(t, "default")), "lib/src/codecs/order_codec.dart")

	assertContains(t, "order_codec.dart", order,
		"const orderCodec = _OrderCodec();",
		"WireCodec orderCodecRef() => orderCodec;",
		"final class _OrderCodec extends TableCodec {",
		"'order_number': CodecField('orderNumber'),",
		"'customer': CodecField('customer', customerCodecRef),",
		"'lines': CodecField('lines', orderLinesCodecRef),",
		"required: ['id', 'lines', 'order_number', 'status']",
		"const orderLinesCodec = _OrderLinesCodec();",
		"super(const ArrayNode(lineItemCodecRef))",
		"const orderListCodec = _OrderListCodec();",
		"import 'customer_codec.dart';",
		"import 'line_item_codec.dart';",
	)
}

func TestPreserveNamingKeepsWireNames(t *testing.T) {
	order := file(t, generate(t, fixture(t, "preserve")), "lib/src/codecs/order_codec.dart")

	assertContains(t, "order_codec.dart", order, "'order_number': CodecField('order_number'),")
}

func TestUnionCodecCarriesItsDiscriminatorMapping(t *testing.T) {
	pet := file(t, generate(t, fixture(t, "default")), "lib/src/codecs/pet_codec.dart")

	assertContains(t, "pet_codec.dart", pet,
		"UnionNode([catCodecRef, dogCodecRef], discriminator: 'pet_type', mapping: {'cat': catCodecRef, 'dog': dogCodecRef})")
}

func TestSelfReferenceGoesThroughALazyRef(t *testing.T) {
	node := file(t, generate(t, fixture(t, "default")), "lib/src/codecs/node_codec.dart")

	assertContains(t, "node_codec.dart", node, "'children': CodecField('children', nodeChildrenCodecRef)", "super(const ArrayNode(nodeCodecRef))")
}

func TestCodecReferenceToAMissingComponentIsDropped(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Dangling", Version: "1"},
		Schemas: map[string]*client.Schema{
			"Holder": {Type: "object", Properties: map[string]*client.Schema{"ghost": ref("Ghost")}},
		},
	}

	out := generate(t, gateFixture{Name: "dangling", Spec: spec, Config: baseConfig()})
	holder := file(t, out, "lib/src/codecs/holder_codec.dart")

	if strings.Contains(holder, "ghost") && strings.Contains(holder, "ghostCodecRef") {
		t.Errorf("a reference to a component that does not exist must not name a codec:\n%s", holder)
	}

	assertContains(t, "holder.dart", file(t, out, "lib/src/models/holder.dart"), "final Object? ghost;")
}

func TestCodecRuntimeIsAlwaysEmitted(t *testing.T) {
	out := generate(t, fixture(t, "minimal"))

	assertContains(t, "codec_runtime.dart", file(t, out, "lib/src/codecs/codec_runtime.dart"),
		"base class TableCodec implements WireCodec {", "Object? _walk(")
}

// walkSpec holds one schema for every rule of the codec walk: renames through
// nested and cyclic references, open maps, an object that is both declared and
// open, a discriminated and a structural union, and an enum.
func walkSpec() *client.APISpec {
	str := &client.Schema{Type: "string"}

	return &client.APISpec{
		Info: client.APIInfo{Title: "Walk API", Version: "1"},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type: "object", Required: []string{"order_number"},
				Properties: map[string]*client.Schema{
					"order_number": str,
					"status":       {Type: "string", Enum: []any{"pending", "shipped"}},
					"color":        ref("Color"),
					"customer":     ref("Customer"),
					"lines":        {Type: "array", Items: ref("LineItem")},
					"by_sku":       {Type: "object", AdditionalProperties: ref("LineItem")},
					"shipping":     {Type: "object", Properties: map[string]*client.Schema{"street_name": str}},
				},
			},
			"Color": {Type: "string", Enum: []any{"red", "green"}},
			"Customer": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"display_name": str,
					"past_orders":  {Type: "array", Items: ref("Order")},
				},
			},
			"LineItem": {
				Type: "object", Required: []string{"item_qty"},
				Properties: map[string]*client.Schema{"item_qty": {Type: "integer"}, "unit_price": {Type: "number"}},
			},
			"Node": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"node_label": str,
					"child_nodes": {
						Type: "array", Items: ref("Node"),
					},
				},
			},
			"Bag": {
				Type:                 "object",
				Properties:           map[string]*client.Schema{"bag_name": str},
				AdditionalProperties: ref("LineItem"),
			},
			"Pet": {
				OneOf: []*client.Schema{ref("Cat"), ref("Dog")},
				Discriminator: &client.Discriminator{
					PropertyName: "pet_type",
					Mapping:      map[string]string{"cat": "#/components/schemas/Cat", "dog": "#/components/schemas/Dog"},
				},
			},
			"Cat": {
				Type: "object", Required: []string{"pet_type"},
				Properties: map[string]*client.Schema{"pet_type": str, "meow_volume": {Type: "integer"}},
			},
			"Dog": {
				Type: "object", Required: []string{"pet_type"},
				Properties: map[string]*client.Schema{"pet_type": str, "bark_volume": {Type: "integer"}},
			},
			"Thing": {OneOf: []*client.Schema{ref("Wide"), ref("Tall")}},
			"Wide": {
				Type: "object", Required: []string{"wide_mm"},
				Properties: map[string]*client.Schema{"wide_mm": {Type: "integer"}, "color_name": str},
			},
			"Tall": {
				Type: "object", Required: []string{"tall_mm"},
				Properties: map[string]*client.Schema{"tall_mm": {Type: "integer"}},
			},
			"Loose":  {Type: "object", Properties: map[string]*client.Schema{"loose_name": str}},
			"Gadget": {OneOf: []*client.Schema{ref("Loose"), ref("Wide")}},
		},
		Endpoints: []client.Endpoint{{
			Method: "POST", Path: "/orders/bulk", OperationID: "orders.bulk",
			RequestBody: &client.RequestBody{Required: true, Content: jsonContent(&client.Schema{Type: "array", Items: ref("Order")})},
			Responses:   map[int]*client.Response{204: {Description: "ok"}},
		}},
	}
}

type walkCase struct {
	Name   string
	Codec  string
	Wire   any
	Client any

	// Only limits a case to one direction: "decode" or "encode". Empty runs both.
	Only string
}

// walkCases are the wire and client forms of each value. A case runs both
// ways: decoding Wire must give Client, and encoding Client must give Wire.
func walkCases() []walkCase {
	return []walkCase{
		{
			Name: "renames declared keys and leaves an unknown key alone", Codec: "orderCodec",
			Wire:   map[string]any{"order_number": "A-1", "extra_key": 1},
			Client: map[string]any{"orderNumber": "A-1", "extra_key": 1},
		},
		{
			Name: "walks into a nested reference, an array of references and an inline object", Codec: "orderCodec",
			Wire: map[string]any{
				"order_number": "A-1",
				"customer":     map[string]any{"display_name": "Ada"},
				"lines":        []any{map[string]any{"item_qty": 2, "unit_price": 1.5}},
				"shipping":     map[string]any{"street_name": "Main"},
			},
			Client: map[string]any{
				"orderNumber": "A-1",
				"customer":    map[string]any{"displayName": "Ada"},
				"lines":       []any{map[string]any{"itemQty": 2, "unitPrice": 1.5}},
				"shipping":    map[string]any{"streetName": "Main"},
			},
		},
		{
			Name: "keeps the keys of an open map and renames inside its values", Codec: "orderCodec",
			Wire:   map[string]any{"by_sku": map[string]any{"snake_sku": map[string]any{"item_qty": 3}}},
			Client: map[string]any{"bySku": map[string]any{"snake_sku": map[string]any{"itemQty": 3}}},
		},
		{
			Name: "walks the value of an undeclared key of an object that is also open", Codec: "bagCodec",
			Wire:   map[string]any{"bag_name": "b", "other_key": map[string]any{"item_qty": 1}},
			Client: map[string]any{"bagName": "b", "other_key": map[string]any{"itemQty": 1}},
		},
		{
			Name: "walks a cycle to the depth the value has", Codec: "nodeCodec",
			Wire: map[string]any{"node_label": "a", "child_nodes": []any{
				map[string]any{"node_label": "b", "child_nodes": []any{map[string]any{"node_label": "c"}}},
			}},
			Client: map[string]any{"nodeLabel": "a", "childNodes": []any{
				map[string]any{"nodeLabel": "b", "childNodes": []any{map[string]any{"nodeLabel": "c"}}},
			}},
		},
		{
			Name: "walks a cycle between two schemas", Codec: "customerCodec",
			Wire: map[string]any{"past_orders": []any{
				map[string]any{"order_number": "1", "customer": map[string]any{"display_name": "x"}},
			}},
			Client: map[string]any{"pastOrders": []any{
				map[string]any{"orderNumber": "1", "customer": map[string]any{"displayName": "x"}},
			}},
		},
		{
			Name: "passes an enum value through, known or not", Codec: "orderCodec",
			Wire:   map[string]any{"order_number": "1", "status": "refunded"},
			Client: map[string]any{"orderNumber": "1", "status": "refunded"},
		},
		{
			Name: "passes the value of a field that references a named enum through, known or not", Codec: "orderCodec",
			Wire:   map[string]any{"order_number": "1", "color": "ultraviolet"},
			Client: map[string]any{"orderNumber": "1", "color": "ultraviolet"},
		},
		{
			Name: "passes null through", Codec: "orderCodec",
			Wire:   map[string]any{"order_number": nil, "customer": nil},
			Client: map[string]any{"orderNumber": nil, "customer": nil},
		},
		{
			Name: "renames every element of a list codec", Codec: "orderListCodec",
			Wire:   []any{map[string]any{"order_number": "1"}, map[string]any{"order_number": "2"}},
			Client: []any{map[string]any{"orderNumber": "1"}, map[string]any{"orderNumber": "2"}},
		},
		{
			Name: "picks a discriminated member by the wire tag and renames its fields", Codec: "petCodec",
			Wire:   map[string]any{"pet_type": "dog", "bark_volume": 9},
			Client: map[string]any{"petType": "dog", "barkVolume": 9},
		},
		{
			Name: "reads the tag under the name a member gives it when members disagree", Codec: "petCodec",
			Wire:   map[string]any{"pet_type": "cat", "meow_volume": 2},
			Client: map[string]any{"kind": "cat", "meowVolume": 2},
		},
		{
			Name: "passes a discriminated value with an unmapped tag through", Codec: "petCodec",
			Wire:   map[string]any{"pet_type": "fish", "fin_count": 2},
			Client: map[string]any{"pet_type": "fish", "fin_count": 2},
		},
		{
			Name: "passes an encoded discriminated value through when two names carry different tags", Codec: "petCodec", Only: "encode",
			Client: map[string]any{"kind": "cat", "petType": "dog", "meowVolume": 1},
			Wire:   map[string]any{"kind": "cat", "petType": "dog", "meowVolume": 1},
		},
		{
			Name: "does not call an encoded tag ambiguous when two names resolve to the same member", Codec: "petCodec", Only: "encode",
			Client: map[string]any{"kind": "cat", "petType": "cat", "meowVolume": 1},
			Wire:   map[string]any{"pet_type": "cat", "petType": "cat", "meow_volume": 1},
		},
		{
			Name: "falls back to the wire name of the tag when encoding", Codec: "petCodec", Only: "encode",
			Client: map[string]any{"pet_type": "dog", "barkVolume": 1},
			Wire:   map[string]any{"pet_type": "dog", "bark_volume": 1},
		},
		{
			Name: "picks a structural member by its required fields", Codec: "thingCodec",
			Wire:   map[string]any{"tall_mm": 4},
			Client: map[string]any{"tallMm": 4},
		},
		{
			Name: "picks the first structural member that matches", Codec: "thingCodec",
			Wire:   map[string]any{"wide_mm": 4, "color_name": "red"},
			Client: map[string]any{"wideMm": 4, "colorName": "red"},
		},
		{
			Name: "takes the first structural member when two match", Codec: "thingCodec",
			Wire:   map[string]any{"wide_mm": 4, "tall_mm": 5},
			Client: map[string]any{"wideMm": 4, "tall_mm": 5},
			Only:   "decode",
		},
		{
			Name: "skips a structural member with no required fields", Codec: "gadgetCodec",
			Wire:   map[string]any{"wide_mm": 4},
			Client: map[string]any{"wideMm": 4},
		},
		{
			Name: "passes a structural value that matches no member through", Codec: "thingCodec",
			Wire:   map[string]any{"some_key": 4},
			Client: map[string]any{"some_key": 4},
		},
		{
			Name: "passes a value of the wrong shape through", Codec: "orderCodec",
			Wire:   []any{"not", "an", "object"},
			Client: []any{"not", "an", "object"},
		},
	}
}

var codecConstDecl = regexp.MustCompile(`(?m)^const (\w+Codec) = _`)

// runCodecs compiles the generated package for f against the real forge_client
// and runs every request through the generated codecs in Dart, returning one
// JSON result per request. The values never pass through Go's idea of what a
// codec should do: the Dart walker produces them.
func runCodecs(t *testing.T, f gateFixture, requests []map[string]any) []any {
	t.Helper()

	var results []any
	if err := json.Unmarshal(runCodecsRaw(t, f, requests), &results); err != nil {
		t.Fatal(err)
	}

	return results
}

// runCodecsRaw is runCodecs without decoding the output, for a caller that
// needs the number literals the Dart walker wrote.
func runCodecsRaw(t *testing.T, f gateFixture, requests []map[string]any) []byte {
	t.Helper()

	fvm := requireDart(t)
	dir := writePackage(t, f)

	var imports, consts []string

	files, err := filepath.Glob(filepath.Join(dir, "lib", "src", "codecs", "*_codec.dart"))
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		imports = append(imports, fmt.Sprintf("import 'package:%s/src/codecs/%s';", f.Config.PackageName, filepath.Base(path)))

		for _, m := range codecConstDecl.FindAllStringSubmatch(string(body), -1) {
			consts = append(consts, fmt.Sprintf("    '%s': %s,", m[1], m[1]))
		}
	}

	sort.Strings(imports)
	sort.Strings(consts)

	script := "import 'dart:convert';\nimport 'dart:io';\n\n" +
		fmt.Sprintf("import 'package:%s/src/support.dart' show WireCodec;\n", f.Config.PackageName) +
		strings.Join(imports, "\n") + "\n\n" +
		"void main(List<String> args) {\n" +
		"  final codecs = <String, WireCodec>{\n" + strings.Join(consts, "\n") + "\n  };\n" +
		"  final requests = jsonDecode(File(args[0]).readAsStringSync()) as List<Object?>;\n" +
		"  final results = <Object?>[];\n" +
		"  for (final request in requests) {\n" +
		"    final r = request! as Map<String, Object?>;\n" +
		"    final codec = codecs[r['codec']]!;\n" +
		"    results.add(r['direction'] == 'decode' ? codec.decode(r['input']) : codec.encode(r['input']));\n" +
		"  }\n" +
		"  stdout.write(jsonEncode(results));\n" +
		"}\n"

	payload, err := json.Marshal(requests)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "tool"), 0o755); err != nil {
		t.Fatal(err)
	}

	for name, content := range map[string]string{"tool/run_codecs.dart": script, "tool/requests.json": string(payload)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	runFvm(t, fvm, dir, "pub", "get")

	return []byte(runFvm(t, fvm, dir, "run", "tool/run_codecs.dart", "tool/requests.json"))
}

// TestGeneratedCodecsWalkLikeTheTypeScriptRuntime runs every case in both
// directions through the generated Dart codecs, compiled against the real
// forge_client. The cases are the rules of the TypeScript codec runtime.
func TestGeneratedCodecsWalkLikeTheTypeScriptRuntime(t *testing.T) {
	cfg := baseConfig()
	cfg.PackageName = "walk_client"

	// Cat and Dog disagree on the client name of the shared pet_type tag.
	cfg.FieldOverrides = map[string]string{"Cat.pet_type": "kind"}

	cases := walkCases()

	var requests []map[string]any

	for _, c := range cases {
		requests = append(requests,
			map[string]any{"codec": c.Codec, "direction": "decode", "input": c.Wire},
			map[string]any{"codec": c.Codec, "direction": "encode", "input": c.Client},
		)
	}

	results := runCodecs(t, gateFixture{Name: "walk", Spec: walkSpec(), Config: cfg}, requests)

	if len(results) != len(requests) {
		t.Fatalf("ran %d requests, got %d results", len(requests), len(results))
	}

	for i, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := roundTrip(t, c.Client); c.Only != "encode" && !reflect.DeepEqual(results[2*i], got) {
				t.Errorf("%s decode(wire):\n got  %v\n want %v", c.Codec, results[2*i], got)
			}

			if got := roundTrip(t, c.Wire); c.Only != "decode" && !reflect.DeepEqual(results[2*i+1], got) {
				t.Errorf("%s encode(client):\n got  %v\n want %v", c.Codec, results[2*i+1], got)
			}
		})
	}
}

// roundTrip normalises a Go value to what json.Unmarshal produces.
func roundTrip(t *testing.T, v any) any {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}

	return out
}

func TestCodecDirectoryIsOwnedByTheGenerator(t *testing.T) {
	out := generate(t, fixture(t, "default"))

	if slices.Contains(out.ExclusiveDirs, "lib/src/codecs") {
		return
	}

	t.Errorf("lib/src/codecs is not in ExclusiveDirs %v, so a stale codec file would survive regeneration", out.ExclusiveDirs)
}
