package dart

import (
	"fmt"
	"strings"
)

// codecRef is where one codec table entry is rendered: the const holding the
// codec, the function resolving it lazily, its private class and its file.
type codecRef struct {
	id       string
	constant string
	ref      string
	class    string
	owner    string
	file     string
}

// codecNaming names every codec table entry that has a component to live
// with. An entry belongs to the component its id starts with: "Order",
// "Order.lines.items" and "[]Order" all live in order_codec.dart. An entry
// whose component does not exist (a list codec over a dangling reference) is
// not rendered, and any reference to it is dropped rather than left naming a
// const that is never declared.
type codecNaming struct {
	byID    map[string]codecRef
	byOwner map[string][]codecRef
}

func newCodecNaming(table *codecTable, reg *registry) codecNaming {
	n := codecNaming{byID: map[string]codecRef{}, byOwner: map[string][]codecRef{}}
	taken := map[string]bool{}

	for _, id := range sortedCodecIDs(table.entries) {
		owner, rest := codecOwner(id, reg)
		if owner == "" {
			continue
		}

		m := reg.models[owner]
		base := toCamel(m.dartName)

		switch {
		case strings.HasPrefix(id, "[]"):
			base += "List"
		case rest != "":
			var name strings.Builder

			name.WriteString(base)

			for seg := range strings.SplitSeq(rest, ".") {
				name.WriteString(typeIdent(seg))
			}

			base = name.String()
		}

		constant := uniqueNames([]string{base + "Codec"}, sanitize, taken, false)[0]
		ref := codecRef{
			id:       id,
			constant: constant,
			ref:      constant + "Ref",
			class:    "_" + upperFirst(constant),
			owner:    owner,
			file:     m.file + "_codec",
		}

		n.byID[id] = ref
		n.byOwner[owner] = append(n.byOwner[owner], ref)
	}

	return n
}

// codecOwner finds the component an entry id belongs to, preferring the
// longest component name that prefixes it, so a component whose own name
// contains a dot still owns its entries.
func codecOwner(id string, reg *registry) (string, string) {
	bare := strings.TrimPrefix(id, "[]")

	best := ""

	for name := range reg.models {
		if (bare == name || strings.HasPrefix(bare, name+".")) && len(name) > len(best) {
			best = name
		}
	}

	if best == "" {
		return "", ""
	}

	return best, strings.TrimPrefix(strings.TrimPrefix(bare, best), ".")
}

// refTo renders a lazy reference to a codec, or "" when it is not rendered.
func (n codecNaming) refTo(id string) string {
	if r, ok := n.byID[id]; ok {
		return r.ref
	}

	return ""
}

// renderCodecFiles renders lib/src/codecs/: the runtime and one file per
// component.
func renderCodecFiles(table *codecTable, naming codecNaming) map[string]string {
	files := map[string]string{"lib/src/codecs/codec_runtime.dart": codecRuntime}

	for owner, refs := range naming.byOwner {
		files["lib/src/codecs/"+refs[0].file+".dart"] = renderCodecFile(owner, refs, table, naming)
	}

	return files
}

func renderCodecFile(owner string, refs []codecRef, table *codecTable, naming codecNaming) string {
	var body strings.Builder

	imports := map[string]bool{"import 'codec_runtime.dart';": true}

	for _, ref := range refs {
		entry := table.entries[ref.id]

		node := renderCodecNode(entry, func(id string) string {
			target, ok := naming.byID[id]
			if !ok {
				return ""
			}

			if target.owner != owner {
				imports[fmt.Sprintf("import '%s.dart';", target.file)] = true
			}

			return target.ref
		})

		fmt.Fprintf(&body, "\n/// Wire codec for the `%s` schema.\n", strings.ReplaceAll(ref.id, "`", "'"))
		fmt.Fprintf(&body, "const %s = %s();\n\n", ref.constant, ref.class)
		fmt.Fprintf(&body, "/// Resolves [%s] lazily.\n", ref.constant)
		fmt.Fprintf(&body, "WireCodec %s() => %s;\n\n", ref.ref, ref.constant)
		fmt.Fprintf(&body, "final class %s extends TableCodec {\n", ref.class)
		fmt.Fprintf(&body, "  const %s() : super(const %s);\n", ref.class, node)
		body.WriteString("}\n")
	}

	var b strings.Builder

	b.WriteString(generatedHeader)
	b.WriteString(importBlock(nil, nil, append([]string{"import '../support.dart' show WireCodec;"}, sortedKeys(imports)...)))
	b.WriteString(body.String())

	return b.String()
}

// renderCodecNode renders one entry as a const CodecNode expression. ref
// returns the lazy reference for a codec id, or "" to drop it.
func renderCodecNode(entry codecEntry, ref func(string) string) string {
	switch entry.Kind {
	case "object":
		var fields []string

		for _, wire := range sortedKeys(entry.Fields) {
			f := entry.Fields[wire]

			if r := ref(f.Codec); f.Codec != "" && r != "" {
				fields = append(fields, fmt.Sprintf("%s: CodecField(%s, %s)", dartString(wire), dartString(f.Client), r))
			} else {
				fields = append(fields, fmt.Sprintf("%s: CodecField(%s)", dartString(wire), dartString(f.Client)))
			}
		}

		out := "ObjectNode({\n          " + strings.Join(fields, ",\n          ") + ",\n        }"

		if len(entry.Required) > 0 {
			out += ", required: " + dartStringList(entry.Required)
		}

		if r := ref(entry.Values); entry.Values != "" && r != "" {
			out += ", values: " + r
		}

		return out + ")"

	case "array":
		if r := ref(entry.Items); entry.Items != "" && r != "" {
			return "ArrayNode(" + r + ")"
		}

		return "ArrayNode()"

	case "record":
		if r := ref(entry.Values); entry.Values != "" && r != "" {
			return "RecordNode(" + r + ")"
		}

		return "RecordNode()"

	case "union":
		var members []string

		for _, m := range entry.Members {
			if r := ref(m); r != "" {
				members = append(members, r)
			}
		}

		out := "UnionNode([" + strings.Join(members, ", ") + "]"

		if entry.Discriminator != nil {
			var cases []string

			for _, tag := range sortedKeys(entry.Discriminator.Map) {
				if r := ref(entry.Discriminator.Map[tag]); r != "" {
					cases = append(cases, dartString(tag)+": "+r)
				}
			}

			out += ", discriminator: " + dartString(entry.Discriminator.Wire) +
				", mapping: {" + strings.Join(cases, ", ") + "}"
		}

		return out + ")"
	}

	return "PassthroughNode()"
}

// codecRuntime is lib/src/codecs/codec_runtime.dart, the walker every codec
// delegates to. It ports the TypeScript codec runtime's rules: rename known
// fields, walk open maps with their value codec, pick a union member by
// discriminator or by required fields, and pass anything unrecognised
// through untouched.
const codecRuntime = `// Generated by forge. Do not edit.
//
// The table-driven walker every generated codec delegates to. Codec files
// describe each schema as a CodecNode; this file is the only place that
// interprets one.

import '../support.dart' show WireCodec;

/// Resolves a codec lazily, so a schema that refers to itself stays ` + "`const`" + `.
typedef CodecRef = WireCodec Function();

/// One wire field of an object codec.
final class CodecField {
  /// Creates a field renamed to [client], with an optional nested [codec].
  const CodecField(this.client, [this.codec]);

  /// The client-side name.
  final String client;

  /// The codec for the field's value, when it has one.
  final CodecRef? codec;
}

/// The shape a codec walks.
sealed class CodecNode {
  /// Const base constructor.
  const CodecNode();
}

/// An object whose wire fields are renamed one by one.
final class ObjectNode extends CodecNode {
  /// Creates an object node.
  const ObjectNode(this.fields, {this.required = const [], this.values});

  /// Wire name to field.
  final Map<String, CodecField> fields;

  /// Wire names every value carries, used for structural union matching.
  final List<String> required;

  /// The codec for undeclared keys, when the object is open.
  final CodecRef? values;
}

/// A list whose elements share one codec.
final class ArrayNode extends CodecNode {
  /// Creates an array node.
  const ArrayNode([this.items]);

  /// The element codec.
  final CodecRef? items;
}

/// A string-keyed map whose values share one codec.
final class RecordNode extends CodecNode {
  /// Creates a record node.
  const RecordNode([this.values]);

  /// The value codec.
  final CodecRef? values;
}

/// A oneOf or anyOf.
final class UnionNode extends CodecNode {
  /// Creates a union node.
  const UnionNode(this.members, {this.discriminator, this.mapping = const {}});

  /// Member codecs in declared order.
  final List<CodecRef> members;

  /// The wire discriminator property, when the schema declares one.
  final String? discriminator;

  /// Discriminator value to member codec.
  final Map<String, CodecRef> mapping;
}

/// A value that needs no renaming.
final class PassthroughNode extends CodecNode {
  /// Const constructor.
  const PassthroughNode();
}

/// A [WireCodec] described by a [CodecNode].
base class TableCodec implements WireCodec {
  /// Creates a codec over [node].
  const TableCodec(this.node);

  /// The shape this codec walks.
  final CodecNode node;

  @override
  Object? decode(Object? wire) => _walk(wire, node, toClient: true);

  @override
  Object? encode(Object? client) => _walk(client, node, toClient: false);
}

Object? _apply(CodecRef? ref, Object? value, {required bool toClient}) {
  if (ref == null) return value;
  final codec = ref();
  return toClient ? codec.decode(value) : codec.encode(value);
}

Object? _walk(Object? value, CodecNode node, {required bool toClient}) {
  if (value == null) return null;
  switch (node) {
    case PassthroughNode():
      return value;
    case ArrayNode(:final items):
      if (value is! List<Object?>) return value;
      return [for (final item in value) _apply(items, item, toClient: toClient)];
    case RecordNode(:final values):
      if (value is! Map<Object?, Object?>) return value;
      return <String, Object?>{
        for (final entry in value.entries)
          '${entry.key}': _apply(values, entry.value, toClient: toClient),
      };
    case ObjectNode(:final fields, :final values):
      if (value is! Map<Object?, Object?>) return value;
      final rename = <String, (String, CodecRef?)>{
        for (final MapEntry(key: wire, value: field) in fields.entries)
          if (toClient) wire: (field.client, field.codec) else field.client: (wire, field.codec),
      };
      final out = <String, Object?>{};
      for (final entry in value.entries) {
        final key = '${entry.key}';
        final mapped = rename[key];
        if (mapped != null) {
          out[mapped.$1] = _apply(mapped.$2, entry.value, toClient: toClient);
        } else {
          out[key] = _apply(values, entry.value, toClient: toClient);
        }
      }
      return out;
    case UnionNode(:final members, :final discriminator, :final mapping):
      if (value is! Map<Object?, Object?>) return value;
      if (discriminator != null) {
        final member = toClient
            ? _tagged(value, discriminator, mapping)
            : _taggedClient(value, discriminator, members, mapping);
        return member == null ? value : _apply(member, value, toClient: toClient);
      }
      for (final member in members) {
        final codec = member();
        if (codec is! TableCodec) continue;
        final shape = codec.node;
        if (shape is! ObjectNode || shape.required.isEmpty) continue;
        final keys = toClient
            ? shape.required
            : [for (final wire in shape.required) shape.fields[wire]?.client ?? wire];
        if (keys.every(value.containsKey)) {
          return toClient ? codec.decode(value) : codec.encode(value);
        }
      }
      return value;
  }
}

CodecRef? _tagged(
  Map<Object?, Object?> value,
  String key,
  Map<String, CodecRef> mapping,
) {
  final tag = value[key];
  return tag is String ? mapping[tag] : null;
}

CodecRef? _taggedClient(
  Map<Object?, Object?> value,
  String wire,
  List<CodecRef> members,
  Map<String, CodecRef> mapping,
) {
  final candidates = <String>{};
  for (final member in members) {
    final codec = member();
    if (codec is TableCodec) {
      final shape = codec.node;
      if (shape is ObjectNode) {
        final field = shape.fields[wire];
        if (field != null) candidates.add(field.client);
      }
    }
  }
  candidates.add(wire);
  CodecRef? found;
  for (final key in candidates) {
    final next = _tagged(value, key, mapping);
    if (next == null) continue;
    if (found != null && !identical(found, next)) return null;
    found = next;
  }
  return found;
}
`
