package dart

import (
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// importsFixture has an operation whose summary names a model it does not use
// (Session), a support helper it does not call (decodeList) and a core type it
// does not touch (Uint8List), and a query parameter whose wire name is that
// model's name. The names sit in a doc comment and in a string literal, so
// none of them is code.
func importsFixture() gateFixture {
	cfg := baseConfig()
	cfg.PackageName = "imports_client"

	return gateFixture{
		Name: "imports",
		Spec: &client.APISpec{
			Info: client.APIInfo{Title: "Imports API", Version: "1"},
			Schemas: map[string]*client.Schema{
				"Session": {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
				"SessionDetail": {
					Type: "object", Required: []string{"id"},
					Properties: map[string]*client.Schema{"id": {Type: "string"}, "label": {Type: "string"}},
				},
			},
			Endpoints: []client.Endpoint{
				{
					Method: "GET", Path: "/sessions/{id}", OperationID: "sessions.get",
					Summary:     "Get Chat Session without decodeList, Uint8List or Int64",
					PathParams:  []client.Parameter{{Name: "id", In: "path", Required: true, Schema: &client.Schema{Type: "string"}}},
					QueryParams: []client.Parameter{{Name: "Session", In: "query", Schema: &client.Schema{Type: "string"}}},
					Responses:   map[int]*client.Response{200: {Content: jsonContent(ref("SessionDetail"))}},
				},
				{
					Method: "POST", Path: "/sessions", OperationID: "sessions.create",
					Summary:   "Create a Session",
					Responses: map[int]*client.Response{204: {Description: "created"}},
				},
			},
		},
		Config: cfg,
	}
}

func TestImportsNameOnlyWhatTheCodeUses(t *testing.T) {
	out := generate(t, importsFixture())

	for _, name := range []string{"lib/src/bindings/sessions_get.dart", "lib/src/bindings/sessions_create.dart", "lib/src/rest.dart"} {
		content := file(t, out, name)

		for _, unused := range []string{"models/session.dart'", "dart:typed_data'"} {
			if name == "lib/src/rest.dart" && unused == "dart:typed_data'" {
				continue // the REST client handles byte bodies whatever the spec says
			}

			if strings.Contains(content, unused) {
				t.Errorf("%s imports %s, which only a comment or a string literal names:\n%s", name, unused, content)
			}
		}

		for _, helper := range []string{"decodeList", "Int64"} {
			for line := range strings.SplitSeq(content, "\n") {
				if strings.Contains(line, "support.dart") && strings.Contains(line, helper) {
					t.Errorf("%s shows %s from support.dart, which no code calls: %s", name, helper, line)
				}
			}
		}
	}

	assertContains(t, "sessions_get.dart", file(t, out, "lib/src/bindings/sessions_get.dart"),
		"import '../models/session_detail.dart';",
		"'Session':", // the key is data, so it stays in the file
	)
}

func TestCodeIdentifiersSkipCommentsAndStringsButKeepInterpolation(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
		not  []string
	}{
		{"code", "final Order order = Order.fromClient(x);", []string{"Order", "order", "fromClient", "x"}, nil},
		{"line comment", "// Order\nfinal a = 1;", []string{"a"}, []string{"Order"}},
		{"doc comment", "/// Get Chat Session\nfinal a = 1;", []string{"a"}, []string{"Session", "Chat"}},
		{"block comment", "/* Order */ final a = 1;", []string{"a"}, []string{"Order"}},
		{"nested block comment", "/* a /* Order */ Cat */ final b = 1;", []string{"b"}, []string{"Order", "Cat"}},
		{"single quoted", "final a = 'Order';", []string{"a"}, []string{"Order"}},
		{"double quoted", `final a = "Order";`, []string{"a"}, []string{"Order"}},
		{"escaped quote", `final a = 'it\'s Order'; final b = 1;`, []string{"a", "b"}, []string{"Order", "s"}},
		{"slashes in a string", "final a = 'http://x/Order'; final b = 1;", []string{"a", "b"}, []string{"Order", "x"}},
		{"raw", `final a = r'$Order\'; final b = 1;`, []string{"a", "b"}, []string{"Order"}},
		{"triple quoted", "final a = '''\nOrder\n'it' '''; final b = 1;", []string{"a", "b"}, []string{"Order", "it"}},
		{"escaped dollar", `final a = '\$Order'; final b = 1;`, []string{"a", "b"}, []string{"Order"}},
		{"interpolated name", "final a = 'id: $order';", []string{"a", "order"}, nil},
		{"interpolated expression", "final a = 'id: ${Uri.encodeComponent(order.id)}';", []string{"a", "Uri", "encodeComponent", "order", "id"}, nil},
		{"braces inside an interpolation", "final a = 'x ${ {1: Order}[1] } y'; final b = 1;", []string{"a", "b", "Order"}, nil},
		{"string inside an interpolation", "final a = 'x ${f('Cat')} y';", []string{"a", "f"}, []string{"Cat"}},
		{"dollar suffix", "final class$ = 1;", []string{"class$"}, nil},
		{"number", "final a = 0x1F + 2e3;", []string{"a"}, []string{"x1F", "e3"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := codeIdentifiers(c.text)

			for _, w := range c.want {
				if !got[w] {
					t.Errorf("%q: %s is missing from %v", c.text, w, got)
				}
			}

			for _, n := range c.not {
				if got[n] {
					t.Errorf("%q: %s counted though it is not code: %v", c.text, n, got)
				}
			}
		})
	}
}

func TestUsedSymbolsReadCodeOnly(t *testing.T) {
	text := "/// decodeList and Int64 are documented here.\nfinal a = decodeInt(x, 'deepHash');\n"

	got := usedSymbols(text, supportSymbols)
	if want := []string{"decodeInt"}; !slices.Equal(got, want) {
		t.Errorf("usedSymbols = %v, want %v", got, want)
	}
}
