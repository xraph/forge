package dart

import "testing"

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
		{"escape applies after casing", modelReserved, "hash_code", "hashCode$"},
	}

	for _, c := range cases {
		if got := memberIdent(c.in, c.reserved); got != c.want {
			t.Errorf("%s: memberIdent(%q) = %q, want %q", c.kind, c.in, got, c.want)
		}
	}
}
