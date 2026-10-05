package dart

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// decl is one top-level declaration in a model file.
type decl interface {
	render() string
	// usesSupport reports whether the declaration names anything from
	// support.dart, which decides whether its file imports it.
	usesSupport() bool
	typeNames() []string
}

// field is one property of a generated class.
type field struct {
	wire     string
	client   string
	member   string
	typ      dartType
	nullable bool
	required bool
	doc      string
}

func (f field) declType() string {
	if f.nullable {
		return f.typ.nullableName()
	}

	return f.typ.name
}

// classDecl is an immutable model class.
type classDecl struct {
	name   string
	doc    string
	fields []field
}

// buildClass collects a class's fields. An allOf is flattened layer by layer
// exactly as the codec table's allOfEntry flattens it, so the class and its
// codec agree on every field and every client name: later layers win, and
// required lists are unioned.
func (r *registry) buildClass(name string, s *client.Schema, c rctx) decl {
	type layer struct {
		schema *client.Schema
		nsID   string
	}

	var layers []layer

	if len(s.AllOf) > 0 {
		flat, _ := flattenAllOfLayers(s, "", r.spec, map[*client.Schema]bool{})
		for _, l := range flat {
			ns := l.nsID
			if ns == "" {
				ns = c.nsID
			}

			layers = append(layers, layer{l.schema, ns})
		}
	} else {
		layers = []layer{{s, c.nsID}}
	}

	props := map[string]*client.Schema{}
	nsOf := map[string]string{}
	required := map[string]bool{}

	for _, l := range layers {
		for prop, ps := range l.schema.Properties {
			props[prop] = ps
			nsOf[prop] = l.nsID
		}

		for _, req := range l.schema.Required {
			required[req] = true
		}
	}

	members := map[string]bool{}
	d := &classDecl{name: name, doc: s.Description}

	for _, wire := range sortedKeys(props) {
		ps := props[wire]
		ns := nsOf[wire]
		clientName := clientFieldName(ns, wire, r.config)
		member := uniqueNames([]string{clientName}, func(s string) string { return memberIdent(s, modelReserved) }, members, false)[0]

		typ := r.resolve(ps, rctx{owner: c.owner, nsID: ns + "." + wire, hint: name + typeIdent(wire), imports: c.imports})

		doc := ""
		if ps != nil {
			doc = ps.Description
		}

		d.fields = append(d.fields, field{
			wire:     wire,
			client:   clientName,
			member:   member,
			typ:      typ,
			required: required[wire],
			nullable: !required[wire] || (ps != nil && ps.Nullable) || typ.dynamic,
			doc:      doc,
		})
	}

	return d
}

func (d *classDecl) usesSupport() bool { return true }

func (d *classDecl) typeNames() []string {
	out := make([]string, 0, len(d.fields))
	for _, f := range d.fields {
		out = append(out, f.typ.name)
	}

	return out
}

func (d *classDecl) render() string {
	var b strings.Builder

	b.WriteString(docComment(d.doc, fmt.Sprintf("The `%s` schema.", d.name), ""))
	fmt.Fprintf(&b, "final class %s {\n", d.name)
	fmt.Fprintf(&b, "  /// Creates a [%s].\n", d.name)

	if len(d.fields) == 0 {
		fmt.Fprintf(&b, "  const %s();\n\n", d.name)
		fmt.Fprintf(&b, "  /// Decodes a [%s] from its client-shaped JSON.\n", d.name)
		fmt.Fprintf(&b, "  factory %s.fromClient(Object? client) => const %s();\n\n", d.name, d.name)
		b.WriteString("  /// Returns a copy with the given fields replaced.\n")
		fmt.Fprintf(&b, "  %s copyWith() => const %s();\n\n", d.name, d.name)
		b.WriteString("  /// Encodes this value as client-shaped JSON.\n")
		b.WriteString("  Json toClient() => <String, Object?>{};\n\n")
		b.WriteString("  @override\n")
		fmt.Fprintf(&b, "  bool operator ==(Object other) => other is %s;\n\n", d.name)
		b.WriteString("  @override\n")
		b.WriteString("  int get hashCode => 0;\n")
		b.WriteString("}\n")

		return b.String()
	}

	fmt.Fprintf(&b, "  const %s({\n", d.name)

	for _, f := range d.fields {
		if f.required {
			fmt.Fprintf(&b, "    required this.%s,\n", f.member)
		} else {
			fmt.Fprintf(&b, "    this.%s,\n", f.member)
		}
	}

	b.WriteString("  });\n\n")

	fmt.Fprintf(&b, "  /// Decodes a [%s] from its client-shaped JSON.\n", d.name)
	fmt.Fprintf(&b, "  factory %s.fromClient(Object? client) {\n", d.name)
	b.WriteString("    final json = decodeObject(client);\n")
	fmt.Fprintf(&b, "    return %s(\n", d.name)

	for _, f := range d.fields {
		key := "json[" + dartString(f.client) + "]"

		value := f.typ.decode(key, 0)
		if f.nullable {
			value = f.typ.decodeNullable(key, 0)
		}

		fmt.Fprintf(&b, "      %s: %s,\n", f.member, value)
	}

	b.WriteString("    );\n  }\n\n")

	for _, f := range d.fields {
		b.WriteString(docComment(f.doc, fmt.Sprintf("The `%s` field.", f.wire), "  "))
		fmt.Fprintf(&b, "  final %s %s;\n\n", f.declType(), f.member)
	}

	b.WriteString("  /// Returns a copy with the given fields replaced.\n")
	fmt.Fprintf(&b, "  %s copyWith({\n", d.name)

	for _, f := range d.fields {
		if f.nullable {
			fmt.Fprintf(&b, "    Value<%s>? %s,\n", valueArg(f.typ), f.member)
		} else {
			fmt.Fprintf(&b, "    %s? %s,\n", f.typ.name, f.member)
		}
	}

	fmt.Fprintf(&b, "  }) => %s(\n", d.name)

	for _, f := range d.fields {
		if f.nullable {
			fmt.Fprintf(&b, "    %s: switch (%s) { Assign(:final value) => value, _ => this.%s },\n", f.member, f.member, f.member)
		} else {
			fmt.Fprintf(&b, "    %s: %s ?? this.%s,\n", f.member, f.member, f.member)
		}
	}

	b.WriteString("  );\n\n")

	b.WriteString("  /// Encodes this value as client-shaped JSON.\n")
	b.WriteString("  Json toClient() => <String, Object?>{\n")

	for _, f := range d.fields {
		key := dartString(f.client)

		switch {
		case !f.nullable:
			fmt.Fprintf(&b, "    %s: %s,\n", key, f.typ.encode(f.member, 0))
		case f.required:
			fmt.Fprintf(&b, "    %s: %s,\n", key, f.typ.encodeNullable(f.member, 0))
		default:
			fmt.Fprintf(&b, "    if (%s case final v?) %s: %s,\n", f.member, key, f.typ.encode("v", 0))
		}
	}

	b.WriteString("  };\n\n")

	b.WriteString("  @override\n")
	b.WriteString("  bool operator ==(Object other) =>\n")
	b.WriteString("      identical(this, other) ||\n")
	fmt.Fprintf(&b, "      other is %s", d.name)

	for _, f := range d.fields {
		if f.typ.deep {
			fmt.Fprintf(&b, " &&\n          deepEquals(%s, other.%s)", f.member, f.member)
		} else {
			fmt.Fprintf(&b, " &&\n          %s == other.%s", f.member, f.member)
		}
	}

	b.WriteString(";\n\n")

	b.WriteString("  @override\n")
	b.WriteString("  int get hashCode => Object.hashAll([\n")

	for _, f := range d.fields {
		if f.typ.deep {
			fmt.Fprintf(&b, "    deepHash(%s),\n", f.member)
		} else {
			fmt.Fprintf(&b, "    %s,\n", f.member)
		}
	}

	b.WriteString("  ]);\n}\n")

	return b.String()
}

// valueArg is the type argument of a nullable field's Value: the type with
// its nullability removed, since Assign already carries a nullable value.
func valueArg(t dartType) string {
	if t.dynamic {
		return "Object"
	}

	return t.name
}

// enumValue is one member of a generated enum.
type enumValue struct {
	member  string
	literal string
	wire    string
}

// enumDecl is an extension type over an enum's wire value, with a static
// constant per value the schema declares. A value the schema does not declare
// is still a value of the type, so it survives a decode and an encode.
type enumDecl struct {
	name   string
	doc    string
	rep    string
	values []enumValue
	// known names the plain Dart enum of the declared values, which gives a
	// caller an exhaustive switch.
	known string
}

// buildEnum names one member per declared value. A null entry is dropped:
// it means the schema is nullable, not that null is a member. A value named
// unknown is an ordinary member.
func (r *registry) buildEnum(name string, s *client.Schema) decl {
	d := &enumDecl{name: name, doc: s.Description, rep: enumRep(s), known: r.claim(name + "Known")}
	members := map[string]bool{}
	literals := map[string]bool{}

	for _, v := range s.Enum {
		if v == nil {
			continue
		}

		wire, literal := enumLiteral(v)
		if literals[literal] {
			continue
		}

		literals[literal] = true

		member := uniqueNames([]string{wire}, func(s string) string { return memberIdent(s, enumReserved) }, members, false)[0]
		d.values = append(d.values, enumValue{member: member, literal: literal, wire: wire})
	}

	return d
}

// enumLiteral renders an enum value as text for its member name and as a
// Dart literal for its wire value.
func enumLiteral(v any) (string, string) {
	switch tv := v.(type) {
	case string:
		return tv, dartString(tv)
	case bool:
		return strconv.FormatBool(tv), strconv.FormatBool(tv)
	case float64:
		if tv == math.Trunc(tv) && math.Abs(tv) < 1<<53 {
			s := strconv.FormatInt(int64(tv), 10)

			return s, s
		}

		s := strconv.FormatFloat(tv, 'f', -1, 64)

		return s, s
	case int, int64, int32:
		s := fmt.Sprintf("%d", tv)

		return s, s
	}

	s := fmt.Sprintf("%v", v)

	return s, dartString(s)
}

func (d *enumDecl) usesSupport() bool   { return false }
func (d *enumDecl) typeNames() []string { return nil }

func (d *enumDecl) render() string {
	var b strings.Builder

	b.WriteString(docComment(d.doc, fmt.Sprintf("The `%s` enum.", d.name), ""))
	fmt.Fprintf(&b, "extension type const %s(%s wire) implements Object {\n", d.name, d.rep)

	for _, v := range d.values {
		fmt.Fprintf(&b, "  /// Wire value `%s`.\n", strings.ReplaceAll(v.wire, "`", "'"))
		fmt.Fprintf(&b, "  static const %s = %s(%s);\n\n", v.member, d.name, v.literal)
	}

	members := make([]string, len(d.values))
	for i, v := range d.values {
		members[i] = v.member
	}

	b.WriteString("  /// Every value this client knows.\n")
	fmt.Fprintf(&b, "  static const values = <%s>[%s];\n\n", d.name, strings.Join(members, ", "))
	b.WriteString("  /// Whether this is a value the schema declares. A newer server may send\n")
	b.WriteString("  /// others, which are kept as they arrived.\n")
	b.WriteString("  bool get isKnown => values.contains(this);\n\n")
	b.WriteString("  /// The declared value as a plain enum for an exhaustive `switch`, or null\n")
	b.WriteString("  /// for a value this client does not know.\n")
	fmt.Fprintf(&b, "  %s? get known => switch (wire) {\n", d.known)

	for _, v := range d.values {
		fmt.Fprintf(&b, "    %s => %s.%s,\n", v.literal, d.known, v.member)
	}

	b.WriteString("    _ => null,\n  };\n}\n\n")

	fmt.Fprintf(&b, "/// The values of [%s] this client knows, for an exhaustive `switch`.\n", d.name)
	fmt.Fprintf(&b, "enum %s {\n", d.known)

	for _, v := range d.values {
		fmt.Fprintf(&b, "  /// Wire value `%s`.\n", strings.ReplaceAll(v.wire, "`", "'"))
		fmt.Fprintf(&b, "  %s,\n\n", v.member)
	}

	out := strings.TrimSuffix(b.String(), ",\n\n") + ";\n}\n"

	return out
}

// variant is one branch of a generated union.
type variant struct {
	name    string
	typ     dartType
	refName string
	// match is the structural test that selects this variant when there is
	// no discriminator, or "" when the branch offers nothing to test.
	match string
}

// unionDecl is a sealed base with one wrapper subclass per branch and one for
// a value no branch matches, so decoding never throws.
type unionDecl struct {
	name     string
	doc      string
	variants []variant
	unknown  string
	// discKey and cases are set when the schema declares a discriminator
	// with a mapping: the client-side key, and tag to variant index.
	discKey string
	cases   [][2]string
}

// buildUnion mirrors the codec table's unionEntry: members in declared order,
// a discriminator only when it maps tags to members, and otherwise the
// required-wire-field match the codec walker uses, translated to client
// names through the codec entry of each member.
func (r *registry) buildUnion(name string, s *client.Schema, c rctx) decl {
	members, token := s.OneOf, "oneOf"
	if len(members) == 0 {
		members, token = s.AnyOf, "anyOf"
	}

	d := &unionDecl{name: name, doc: s.Description}
	codecOf := map[string]string{}

	for i, member := range members {
		if member == nil {
			continue
		}

		var (
			v       variant
			codecID string
			target  = member
		)

		if ref := client.ComponentRefName(member.Ref); ref != "" {
			m := r.models[ref]
			if m == nil {
				continue
			}

			v = variant{name: r.claim(name + m.dartName), typ: r.resolve(member, c), refName: ref}
			codecID, target = ref, m.schema
		} else {
			codecID = fmt.Sprintf("%s.%s%d", c.nsID, token, i)
			label := fmt.Sprintf("%sOption%d", name, i)
			v = variant{
				name: r.claim(label),
				typ:  r.resolve(member, rctx{owner: c.owner, nsID: codecID, hint: label + "Value", imports: c.imports}),
			}
		}

		v.match = r.structuralMatch(codecID, target)
		codecOf[v.name] = codecID
		d.variants = append(d.variants, v)
	}

	d.unknown = r.claim(name + "Unknown")

	if disc := s.Discriminator; disc != nil && disc.PropertyName != "" && len(disc.Mapping) > 0 {
		d.discKey = disc.PropertyName

		for _, v := range d.variants {
			if entry, ok := r.codecs.entries[codecOf[v.name]]; ok && entry.Kind == "object" {
				if f, ok := entry.Fields[disc.PropertyName]; ok {
					d.discKey = f.Client

					break
				}
			}
		}

		for _, tag := range sortedKeys(disc.Mapping) {
			target := client.ComponentRefName(disc.Mapping[tag])

			for _, v := range d.variants {
				if v.refName != "" && v.refName == target {
					d.cases = append(d.cases, [2]string{tag, v.name})

					break
				}
			}
		}
	}

	return d
}

// structuralMatch renders the test that selects a branch without a
// discriminator: every required field present for an object (by client
// name, from the member's codec entry), or a runtime type check for a scalar
// or list branch.
func (r *registry) structuralMatch(codecID string, target *client.Schema) string {
	if entry, ok := r.codecs.entries[codecID]; ok && entry.Kind == "object" && len(entry.Required) > 0 {
		parts := []string{"json != null"}

		for _, wire := range entry.Required {
			key := wire
			if f, ok := entry.Fields[wire]; ok {
				key = f.Client
			}

			parts = append(parts, "json.containsKey("+dartString(key)+")")
		}

		return strings.Join(parts, " && ")
	}

	if target == nil {
		return ""
	}

	switch target.Type {
	case "string":
		return "client is String"
	case "integer":
		return "client is int"
	case "number":
		return "client is num"
	case "boolean":
		return "client is bool"
	case "array":
		return "client is List<Object?>"
	}

	return ""
}

func (d *unionDecl) usesSupport() bool { return true }

func (d *unionDecl) typeNames() []string {
	out := make([]string, 0, len(d.variants))
	for _, v := range d.variants {
		out = append(out, v.typ.name)
	}

	return out
}

func (d *unionDecl) render() string {
	var b strings.Builder

	fallback := fmt.Sprintf("The `%s` union.", d.name)
	if d.discKey == "" {
		fallback += "\n\nIt declares no discriminator mapping, so a value is matched to the first\nvariant whose required fields it carries."
	}

	b.WriteString(docComment(d.doc, fallback, ""))
	fmt.Fprintf(&b, "sealed class %s {\n", d.name)
	b.WriteString("  /// Const base constructor.\n")
	fmt.Fprintf(&b, "  const %s();\n\n", d.name)

	if d.discKey != "" {
		fmt.Fprintf(&b, "  /// Decodes a [%s], choosing the variant by `%s`.\n", d.name, d.discKey)
		fmt.Fprintf(&b, "  factory %s.fromClient(Object? client) {\n", d.name)
		b.WriteString("    final json = client is Map<Object?, Object?> ? decodeObject(client) : null;\n")
		fmt.Fprintf(&b, "    return switch (json?[%s]) {\n", dartString(d.discKey))

		for _, c := range d.cases {
			v := d.variantNamed(c[1])
			fmt.Fprintf(&b, "      %s => %s(%s),\n", dartString(c[0]), v.name, v.typ.decode("client", 0))
		}

		fmt.Fprintf(&b, "      _ => %s(client),\n", d.unknown)
		b.WriteString("    };\n  }\n\n")
	} else {
		fmt.Fprintf(&b, "  /// Decodes a [%s] by matching required fields.\n", d.name)
		fmt.Fprintf(&b, "  factory %s.fromClient(Object? client) {\n", d.name)
		b.WriteString("    final json = client is Map<Object?, Object?> ? decodeObject(client) : null;\n")

		for _, v := range d.variants {
			if v.match == "" {
				continue
			}

			value := v.typ.decode("client", 0)
			if strings.HasPrefix(v.match, "client is ") {
				// The type test already promoted client; a cast would be
				// flagged as unnecessary.
				switch {
				case v.typ.cast:
					value = "client"
				case v.typ.promotedDecodeFn != nil:
					value = v.typ.promotedDecodeFn("client")
				}
			}

			fmt.Fprintf(&b, "    if (%s) return %s(%s);\n", v.match, v.name, value)
		}

		b.WriteString("    if (json == null) return " + d.unknown + "(client);\n")
		fmt.Fprintf(&b, "    return %s(json);\n", d.unknown)
		b.WriteString("  }\n\n")
	}

	b.WriteString("  /// Encodes this value as client-shaped JSON.\n")
	b.WriteString("  Object? toClient();\n}\n")

	for _, v := range d.variants {
		desc := v.typ.name
		fmt.Fprintf(&b, "\n/// The `%s` variant of [%s].\n", desc, d.name)
		fmt.Fprintf(&b, "final class %s extends %s {\n", v.name, d.name)
		b.WriteString("  /// Wraps [value].\n")
		fmt.Fprintf(&b, "  const %s(this.value);\n\n", v.name)
		b.WriteString("  /// The wrapped value.\n")
		fmt.Fprintf(&b, "  final %s value;\n\n", v.typ.name)
		b.WriteString("  @override\n")
		fmt.Fprintf(&b, "  Object? toClient() => %s;\n\n", v.typ.encode("value", 0))
		b.WriteString("  @override\n")

		if v.typ.deep {
			fmt.Fprintf(&b, "  bool operator ==(Object other) => other is %s && deepEquals(value, other.value);\n\n", v.name)
			b.WriteString("  @override\n")
			b.WriteString("  int get hashCode => deepHash(value);\n}\n")
		} else {
			fmt.Fprintf(&b, "  bool operator ==(Object other) => other is %s && value == other.value;\n\n", v.name)
			b.WriteString("  @override\n")
			b.WriteString("  int get hashCode => value.hashCode;\n}\n")
		}
	}

	fmt.Fprintf(&b, "\n/// A [%s] this client could not match to any variant.\n", d.name)
	fmt.Fprintf(&b, "final class %s extends %s {\n", d.unknown, d.name)
	b.WriteString("  /// Wraps the undecoded [value].\n")
	fmt.Fprintf(&b, "  const %s(this.value);\n\n", d.unknown)
	b.WriteString("  /// The client-shaped value as received.\n")
	b.WriteString("  final Object? value;\n\n")
	b.WriteString("  @override\n")
	b.WriteString("  Object? toClient() => value;\n\n")
	b.WriteString("  @override\n")
	fmt.Fprintf(&b, "  bool operator ==(Object other) => other is %s && deepEquals(value, other.value);\n\n", d.unknown)
	b.WriteString("  @override\n")
	b.WriteString("  int get hashCode => deepHash(value);\n}\n")

	return b.String()
}

func (d *unionDecl) variantNamed(name string) variant {
	for _, v := range d.variants {
		if v.name == name {
			return v
		}
	}

	return variant{}
}

// aliasDecl is a typedef for a component that is a list, a map or a scalar.
type aliasDecl struct {
	name       string
	schemaName string
	doc        string
	target     dartType
}

func (d *aliasDecl) usesSupport() bool   { return strings.Contains(d.target.name, "Int64") }
func (d *aliasDecl) typeNames() []string { return []string{d.target.name} }

func (d *aliasDecl) render() string {
	return docComment(d.doc, fmt.Sprintf("The `%s` schema.", d.schemaName), "") +
		fmt.Sprintf("typedef %s = %s;\n", d.name, d.target.name)
}

// renderModelFile renders one component's model file.
func (r *registry) renderModelFile(m *componentModel) string {
	var b strings.Builder

	b.WriteString(generatedHeader)

	support := false
	typedData := false

	for _, d := range m.decls {
		support = support || d.usesSupport()
		typedData = typedData || usesTypedData(d.typeNames()...)
	}

	var dartImports, localImports []string

	if typedData {
		dartImports = append(dartImports, "import 'dart:typed_data';")
	}

	if support {
		localImports = append(localImports, "import '../support.dart';")
	}

	localImports = append(localImports, r.typeImports(m.imports, "")...)
	b.WriteString(importBlock(dartImports, nil, localImports))

	for _, d := range m.decls {
		b.WriteString("\n")
		b.WriteString(d.render())
	}

	return b.String()
}

func sortStrings(s []string) { sort.Strings(s) }

// generatedHeader opens every generated Dart file.
const generatedHeader = "// Generated by forge. Do not edit.\n"

// importBlock renders dart:, package: and relative imports as three groups
// separated by blank lines, preceded by a blank line, or "" when there are
// none.
func importBlock(groups ...[]string) string {
	var parts []string

	for _, g := range groups {
		if len(g) > 0 {
			parts = append(parts, strings.Join(g, "\n"))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return "\n" + strings.Join(parts, "\n\n") + "\n"
}
