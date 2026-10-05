package dart

import (
	"fmt"
	"regexp"
	"strings"
)

// renderBinding renders lib/src/bindings/<file>.dart: the one-line binding
// and the operation's Args class.
func renderBinding(op *operation, reg *registry) string {
	var body strings.Builder

	forge := map[string]bool{}

	argsType := "NoArgs"
	if op.hasArgs() {
		argsType = op.args
		forge["OperationArgs"] = true
		forge["TagContext"] = true
	} else {
		forge["NoArgs"] = true
	}

	resultType, fromClient, decoder := bindingResult(op)

	summary := fmt.Sprintf("`%s %s`", strings.ToUpper(op.ep.Method), strings.ReplaceAll(op.ep.Path, "`", "'"))
	if op.ep.Summary != "" {
		summary += ": " + op.ep.Summary
	}

	body.WriteString("\n")
	body.WriteString(docComment(summary, "", ""))

	if isReadMethod(op.ep.Method) {
		forge["query"] = true

		fmt.Fprintf(&body, "final %s = query<%s, %s>(%s, %s);\n", op.binding, resultType, argsType, op.constant, fromClient)
	} else {
		forge["mutation"] = true

		entityType, entityArgs := "Object?", ""
		if op.entity != nil {
			entityType = op.entity.dartName
			entityArgs = fmt.Sprintf(", entityFromClient: %s.fromClient, entityToClient: (e) => e.toClient()", entityType)
		}

		fmt.Fprintf(&body, "final %s = mutation<%s, %s, %s>(%s, %s%s);\n",
			op.binding, resultType, argsType, entityType, op.constant, fromClient, entityArgs)
	}

	if decoder != "" {
		body.WriteString("\n")
		body.WriteString(decoder)
	}

	if op.hasArgs() {
		body.WriteString("\n")
		body.WriteString(renderArgs(op, forge))
	}

	text := body.String()

	var b strings.Builder

	b.WriteString(generatedHeader)

	var dartImports []string
	if strings.Contains(text, "Uint8List") {
		dartImports = append(dartImports, "import 'dart:typed_data';")
	}

	local := reg.importsFor(text, "../models/")
	local = append(local, fmt.Sprintf("import '../ops.dart' show %s;", op.constant))

	if shown := usedSymbols(text, supportSymbols); len(shown) > 0 {
		local = append(local, "import '../support.dart' show "+strings.Join(shown, ", ")+";")
	}

	b.WriteString(importBlock(dartImports,
		[]string{"import 'package:forge_client/forge_client.dart'\n    show " + strings.Join(sortedKeys(forge), ", ") + ";"},
		local))
	b.WriteString(text)

	return b.String()
}

// tearOffCall matches a decode expression that only applies one named
// function to the client value, such as `Order.fromClient(client)`.
var tearOffCall = regexp.MustCompile(`^([A-Za-z_]\w*(?:\.\w+)?)\(client\)$`)

// bindingResult renders the binding's result type, the FromClient it passes,
// and the private top-level decoder that FromClient names when no existing
// function decodes the response on its own ("" when one does).
//
// forge_client memoizes decoded models in an Expando keyed by the FromClient,
// so it is always a function with a stable identity: a constructor tear-off,
// a support helper, or the file's own `_fromClient`. A closure would work
// only while nobody rebuilt the binding, and two closures over the same
// decode never share a memo.
func bindingResult(op *operation) (string, string, string) {
	resultType, decode := "Object?", "client"

	switch op.response {
	case "json":
		if !op.responseType.dynamic {
			resultType, decode = op.responseType.name, op.responseType.decode("client", 0)
		}
	case "none":
		return "void", "_fromClient", fmt.Sprintf("/// Discards the empty response of [%s].\nvoid _fromClient(Object? client) {}\n", op.binding)
	}

	if m := tearOffCall.FindStringSubmatch(decode); m != nil {
		return resultType, m[1], ""
	}

	return resultType, "_fromClient", fmt.Sprintf("/// Decodes the response of [%s].\n%s _fromClient(Object? client) => %s;\n", op.binding, resultType, decode)
}

// argsMember is one field of an Args class and how it takes part in
// equality.
type argsMember struct {
	name, decl, doc, equal, hash string
}

// renderArgs renders the Args class: one field per parameter and body
// argument, the TagContext the runtime resolves tags and URLs from, and value
// equality so the class can key a provider family.
func renderArgs(op *operation, forge map[string]bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "/// Arguments for [%s].\n", op.binding)

	if op.body != nil && len(op.body.flatten) > 0 {
		b.WriteString("///\n/// Optional body fields are [Value]s: leave one [Unchanged] to omit it, or\n")
		b.WriteString("/// [Assign] `null` to clear it on the server.\n")
	}

	fmt.Fprintf(&b, "final class %s implements OperationArgs {\n", op.args)
	fmt.Fprintf(&b, "  /// Creates the arguments for [%s].\n", op.binding)
	fmt.Fprintf(&b, "  const %s({\n", op.args)

	var members []argsMember

	for _, p := range op.params {
		if p.required {
			fmt.Fprintf(&b, "    required this.%s,\n", p.member)
		} else {
			fmt.Fprintf(&b, "    this.%s,\n", p.member)
		}

		members = append(members, plainMember(p.member, p.declType(), docOr(p.doc, fmt.Sprintf("%s parameter `%s`.", titleCase(p.in), p.wire)), p.typ.deep))
	}

	if op.body != nil {
		if len(op.body.flatten) > 0 {
			for _, f := range op.body.flatten {
				doc := docOr(f.doc, fmt.Sprintf("Body field `%s`.", f.wire))

				if f.required {
					fmt.Fprintf(&b, "    required this.%s,\n", f.member)
					members = append(members, plainMember(f.member, f.declType(), doc, f.typ.deep))

					continue
				}

				forge["Unchanged"] = true
				forge["Value"] = true

				fmt.Fprintf(&b, "    this.%s = const Unchanged(),\n", f.member)
				members = append(members, argsMember{
					name:  f.member,
					decl:  fmt.Sprintf("Value<%s>", valueArg(f.typ)),
					doc:   doc,
					equal: fmt.Sprintf("valueEquals(%s, other.%s)", f.member, f.member),
					hash:  fmt.Sprintf("valueHash(%s)", f.member),
				})
			}
		} else {
			decl := op.body.typ.name
			if !op.body.required {
				decl = op.body.typ.nullableName()
				fmt.Fprintf(&b, "    this.%s,\n", op.body.member)
			} else {
				fmt.Fprintf(&b, "    required this.%s,\n", op.body.member)
			}

			members = append(members, plainMember(op.body.member, decl, "The request body.", op.body.typ.deep))
		}
	}

	b.WriteString("  });\n")

	for _, m := range members {
		b.WriteString("\n")
		b.WriteString(docComment(m.doc, "", "  "))
		fmt.Fprintf(&b, "  final %s %s;\n", m.decl, m.name)
	}

	b.WriteString("\n  @override\n")
	b.WriteString("  TagContext toTagContext() => TagContext(\n")

	// A null optional parameter is left out rather than written as null: the
	// cache key counts a present-but-empty query map, so writing nulls would
	// key a call differently from the TypeScript runtime's.
	sections := map[string][]string{}

	for _, p := range op.params {
		section := p.in
		key := dartString(p.wire)

		var value string

		switch {
		case section == "header" && p.required:
			value = fmt.Sprintf("%s: %s", key, stringExpr(p.typ, p.member))
		case section == "header":
			value = fmt.Sprintf("if (%s case final v?) %s: %s", p.member, key, stringExpr(p.typ, "v"))
		case p.required:
			value = fmt.Sprintf("%s: %s", key, p.typ.encode(p.member, 0))
		default:
			value = fmt.Sprintf("if (%s case final v?) %s: %s", p.member, key, p.typ.encode("v", 0))
		}

		sections[section] = append(sections[section], value)
	}

	for _, section := range []string{"path", "query", "header"} {
		entries := sections[section]
		if len(entries) == 0 {
			continue
		}

		name := section
		if section == "header" {
			name = "headers"
		}

		fmt.Fprintf(&b, "    %s: {%s},\n", name, strings.Join(entries, ", "))
	}

	if op.body != nil {
		fmt.Fprintf(&b, "    body: %s,\n", argsBodyExpr(op.body, forge))
	}

	b.WriteString("  );\n")

	b.WriteString("\n  @override\n")
	b.WriteString("  bool operator ==(Object other) =>\n")
	fmt.Fprintf(&b, "      other is %s", op.args)

	for _, m := range members {
		fmt.Fprintf(&b, " &&\n      %s", m.equal)
	}

	b.WriteString(";\n")

	b.WriteString("\n  @override\n")

	hashes := make([]string, len(members))
	for i, m := range members {
		hashes[i] = m.hash
	}

	fmt.Fprintf(&b, "  int get hashCode => Object.hashAll([%s]);\n", strings.Join(hashes, ", "))
	b.WriteString("}\n")

	return b.String()
}

// argsBodyExpr renders the client-shaped body the Args class hands the
// runtime. A flattened PATCH body omits Unchanged fields and writes null for
// Assign(null).
func argsBodyExpr(body *bodyParam, forge map[string]bool) string {
	if len(body.flatten) == 0 {
		if body.required {
			return body.typ.encode(body.member, 0)
		}

		return body.typ.encodeNullable(body.member, 0)
	}

	var entries []string

	for _, f := range body.flatten {
		key := dartString(f.client)

		switch {
		case f.required && f.nullable:
			entries = append(entries, fmt.Sprintf("%s: %s", key, f.typ.encodeNullable(f.member, 0)))
		case f.required:
			entries = append(entries, fmt.Sprintf("%s: %s", key, f.typ.encode(f.member, 0)))
		default:
			forge["Assign"] = true

			entries = append(entries, fmt.Sprintf("if (%s case Assign(:final value)) %s: %s",
				f.member, key, f.typ.encodeNullable("value", 0)))
		}
	}

	return "<String, Object?>{\n      " + strings.Join(entries, ",\n      ") + ",\n    }"
}

// plainMember is a field compared with == or, for lists, maps and other
// deep types, with deepEquals.
func plainMember(name, decl, doc string, deep bool) argsMember {
	if deep {
		return argsMember{
			name, decl, doc,
			fmt.Sprintf("deepEquals(%s, other.%s)", name, name),
			fmt.Sprintf("deepHash(%s)", name),
		}
	}

	return argsMember{name, decl, doc, fmt.Sprintf("%s == other.%s", name, name), name}
}

func docOr(doc, fallback string) string {
	if strings.TrimSpace(doc) != "" {
		return doc
	}

	return fallback
}

func titleCase(s string) string {
	if s == "" {
		return s
	}

	return strings.ToUpper(s[:1]) + s[1:]
}
