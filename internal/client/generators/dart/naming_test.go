package dart

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestTypeIdent(t *testing.T) {
	cases := map[string]string{
		"order_item":    "OrderItem",
		"Studio_Grant":  "StudioGrant",
		"3d-tiles":      "T3dTiles",
		"":              "Model",
		"Function":      "Function$",
		"it's a thing!": "ItsAThing",
		"class":         "Class",
		"enum":          "Enum",
		"typedef":       "Typedef",
		"Object":        "Object",
	}

	for in, want := range cases {
		if got := typeIdent(in); got != want {
			t.Errorf("typeIdent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMemberIdent(t *testing.T) {
	cases := []struct {
		in       string
		reserved map[string]bool
		want     string
	}{
		{"order_number", modelReserved, "orderNumber"},
		{"class", modelReserved, "class$"},
		{"get", argsReserved, "get"},
		{"hashCode", modelReserved, "hashCode$"},
		{"name", modelReserved, "name"},
		{"name", enumReserved, "name$"},
		{"1st", enumReserved, "v1st"},
		{"_private", modelReserved, "private"},
		{"", modelReserved, "value"},
	}

	for _, c := range cases {
		if got := memberIdent(c.in, c.reserved); got != c.want {
			t.Errorf("memberIdent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFileStem(t *testing.T) {
	cases := map[string]string{
		"OrderItem":     "order_item",
		"HTTPRequest":   "http_request",
		"orders.list":   "orders_list",
		"3dModel":       "t_3d_model",
		"Weird--Name__": "weird_name",
	}

	for in, want := range cases {
		if got := fileStem(in); got != want {
			t.Errorf("fileStem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDartStringEscapes(t *testing.T) {
	cases := map[string]string{
		`plain`:      `'plain'`,
		`it's`:       `'it\'s'`,
		`$price`:     `'\$price'`,
		`back\slash`: `'back\\slash'`,
		"two\nlines": `'two\nlines'`,
		"bell\x07":   `'bell\u{7}'`,
	}

	for in, want := range cases {
		if got := dartString(in); got != want {
			t.Errorf("dartString(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestUniqueNamesFoldsCase(t *testing.T) {
	got := uniqueNames([]string{"Order", "order"}, func(s string) string { return s }, map[string]bool{}, true)
	if got[0] != "Order" || got[1] != "order2" {
		t.Errorf("uniqueNames = %v, want [Order order2]", got)
	}
}

// typeIdent escapes only what is illegal as a Dart type name. Names the
// generated package or forge_client already exports (Value, Assign,
// QueryState) and dart:core types (String) are legal identifiers, so the
// registry renames those with a Model suffix and a warning instead.
func TestTypeIdentLeavesExportedNamesToTheRegistry(t *testing.T) {
	for _, name := range []string{"Value", "Assign", "QueryState", "EntityRef", "String", "NotFound"} {
		if got := typeIdent(name); got != name {
			t.Errorf("typeIdent(%q) = %q, want it unchanged", name, got)
		}
	}
}

func TestTypeIdentEscapesEveryReservedClass(t *testing.T) {
	// toPascal capitalises a keyword into a legal name, so only a name that
	// stays reserved after casing needs the dollar sign: the built-in Function.
	for _, name := range []string{"Function", "function"} {
		if got := typeIdent(name); got != "Function$" {
			t.Errorf("typeIdent(%q) = %q, want Function$", name, got)
		}
	}
}

func TestMemberIdentEscapesEveryReservedClass(t *testing.T) {
	// Language keywords, Object members, model members, enum members and
	// argument-class members are each escaped for the kinds that own them
	// and left alone for the kinds that do not.
	cases := []struct {
		kind     string
		reserved map[string]bool
		in, want string
	}{
		{"keyword", modelReserved, "default", "default$"},
		{"keyword", enumReserved, "switch", "switch$"},
		{"keyword", argsReserved, "new", "new$"},
		{"object member", modelReserved, "runtimeType", "runtimeType$"},
		{"object member", enumReserved, "toString", "toString$"},
		{"object member", argsReserved, "noSuchMethod", "noSuchMethod$"},
		{"model member", modelReserved, "copyWith", "copyWith$"},
		{"model member", modelReserved, "toClient", "toClient$"},
		{"model member", enumReserved, "fromClient", "fromClient$"},
		{"enum member", enumReserved, "index", "index$"},
		{"enum member", enumReserved, "values", "values$"},
		{"enum member", enumReserved, "wire", "wire$"},
		{"enum member on a model", modelReserved, "values", "values"},
		{"args member", argsReserved, "toTagContext", "toTagContext$"},
		{"args member on a model", modelReserved, "toTagContext", "toTagContext"},
		{"built-in identifier is a legal member", modelReserved, "get", "get"},
		{"annotation the class body uses", modelReserved, "override", "override$"},
		{"annotation the class body uses", argsReserved, "override", "override$"},
		{"core function the class body calls", modelReserved, "identical", "identical$"},
		{"core function the class body calls", argsReserved, "identical", "identical$"},
		{"json hook", modelReserved, "toJson", "toJson$"},
		{"json hook", modelReserved, "fromJson", "fromJson$"},
		{"json hook on args", argsReserved, "toJson", "toJson"},
		{"support helper the class body calls", modelReserved, "deepEquals", "deepEquals$"},
		{"support helper the class body calls", modelReserved, "decodeObject", "decodeObject$"},
		{"support helper the class body calls", argsReserved, "valueEquals", "valueEquals$"},
		{"support helper the class body calls", argsReserved, "valueHash", "valueHash$"},
		{"support helper the enum does not call", enumReserved, "deepEquals", "deepEquals"},
		{"dynamic type", modelReserved, "dynamic", "dynamic$"},
		{"escape applies after casing", modelReserved, "to_json", "toJson$"},
		{"escape applies after casing", modelReserved, "hash_code", "hashCode$"},
	}

	for _, c := range cases {
		if got := memberIdent(c.in, c.reserved); got != c.want {
			t.Errorf("%s: memberIdent(%q) = %q, want %q", c.kind, c.in, got, c.want)
		}
	}
}

// Dart ends a `///` comment at "\r" as well as "\n". Splitting on "\n" alone
// left a bare "\r" inside a line, which ended the comment there and let the
// rest of the description compile as code.
func TestDocCommentKeepsCarriageReturnsInsideTheComment(t *testing.T) {
	for _, text := range []string{
		"pdesc\rfinal pwnDartProp = 1;",
		"pdesc\r\nfinal pwnDartProp = 1;",
		"pdesc\nfinal pwnDartProp = 1;",
	} {
		got := docComment(text, "fallback", "  ")
		want := "  /// pdesc\n  /// final pwnDartProp = 1;\n"

		if got != want {
			t.Errorf("docComment(%q) = %q, want %q", text, got, want)
		}

		if strings.Contains(got, "\r") {
			t.Errorf("docComment(%q) kept a carriage return: %q", text, got)
		}
	}
}

func TestGeneratedDartKeepsDescriptionPayloadsInComments(t *testing.T) {
	spec, err := client.NewSpecParser().ParseFile(context.Background(),
		filepath.Join("..", "..", "testdata", "injection", "evil.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	cfg := client.DefaultConfig()
	cfg.Language = "dart"
	cfg.PackageName = "probe"

	out, err := (&Generator{}).Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	for name, src := range out.Files {
		if strings.Contains(src, "\r") {
			t.Errorf("%s contains a carriage return", name)
		}

		// Split the way Dart does, on either line break.
		for line := range strings.SplitSeq(strings.ReplaceAll(src, "\r", "\n"), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "pwnDart") && !strings.HasPrefix(trimmed, "//") {
				t.Errorf("%s: description payload reached code: %s", name, line)
			}
		}
	}
}
