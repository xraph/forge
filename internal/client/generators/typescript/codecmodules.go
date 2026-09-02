package typescript

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// The codec table is emitted a second time as one module per schema, for the
// same reason the operation manifest is: a consumer that decodes one response
// should not carry the description of every other.
//
// It is a harder split than the operations were, for two reasons that only
// showed up against a real document.
//
// The table is reached through `decode`, which fetch.ts imports, so ANY
// request pulled all 333KB of it however the entries were arranged. Splitting
// the entries alone would have moved nothing. The runtime is therefore emitted
// twice: once beside the table in codecs.ts, and once with no table at all in
// codec-runtime.ts, which is the copy fetch.ts imports.
//
// And the reference graph has cycles. A PredicateSpec holds an array of
// PredicateSpec, and eight such loops exist in the twinos document alone.
// Written as direct object references those modules initialise in terms of
// each other, and the second one to evaluate reads the first in its temporal
// dead zone: a ReferenceError at import time, on a client that typechecked.
// Every reference is a thunk for that reason, resolved when walk needs it
// rather than when the module loads.

// codecConstName renders a codec id as the identifier its module exports.
//
// Prefixed because a schema id may begin with a digit, and sanitised because
// synthetic ids carry the dotted parent path ("AppResponse.tags") that names
// the property they describe. Collisions are settled by the caller: sanitising
// "A.b" and "A_b" produces one identifier from two distinct ids.
func codecConstName(id string) string {
	var b strings.Builder

	b.WriteString("codec_")

	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '$':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}

	return b.String()
}

// codecModuleNaming assigns every codec id a filename and an identifier,
// computed once so the modules, the operations that import them and the table
// that lists them all spell the same codec the same way.
type codecModuleNaming struct {
	ids    []string
	files  map[string]string
	consts map[string]string
}

func newCodecModuleNaming(ids []string) codecModuleNaming {
	files := uniqueFold(ids, opFileStem)
	consts := unique(ids, codecConstName)

	naming := codecModuleNaming{
		ids:    ids,
		files:  make(map[string]string, len(ids)),
		consts: make(map[string]string, len(ids)),
	}

	for i, id := range ids {
		naming.files[id] = files[i]
		naming.consts[id] = consts[i]
	}

	return naming
}

// codecRefs returns the ids one entry names, sorted and deduplicated.
//
// Every position that can carry another codec is read, because a reference
// missed here is a module that never gets imported and a `codec_X` that does
// not resolve -- a compile error rather than a silent one, which is the only
// reason this is safe to write by enumeration.
func codecRefs(entry codecEntry) []string {
	seen := map[string]bool{}

	add := func(id string) {
		if id != "" {
			seen[id] = true
		}
	}

	for _, field := range entry.Fields {
		add(field.Codec)
	}

	add(entry.Items)
	add(entry.Values)

	for _, member := range entry.Members {
		add(member)
	}

	if entry.Discriminator != nil {
		for _, member := range entry.Discriminator.Map {
			add(member)
		}
	}

	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}

	sort.Strings(out)

	return out
}

// renderCodecEntry renders one entry as a TypeScript object literal, with
// every reference rendered through ref.
//
// Written by hand rather than through encoding/json because the whole point is
// that a reference stops being a string: json.Marshal would quote the
// identifier and the module would go back to needing a table to resolve it.
//
// Map keys are sorted for the reason every other table here is: Go randomises
// map iteration and CI byte-diffs the output.
func renderCodecEntry(entry codecEntry, ref func(string) string) string {
	parts := []string{fmt.Sprintf("\"kind\": %s", jsonString(entry.Kind))}

	if len(entry.Fields) > 0 {
		wires := make([]string, 0, len(entry.Fields))
		for wire := range entry.Fields {
			wires = append(wires, wire)
		}

		sort.Strings(wires)

		rendered := make([]string, 0, len(wires))

		for _, wire := range wires {
			field := entry.Fields[wire]

			inner := fmt.Sprintf("\"ts\": %s", jsonString(field.TS))
			if field.Codec != "" {
				inner += fmt.Sprintf(", \"codec\": %s", ref(field.Codec))
			}

			rendered = append(rendered, fmt.Sprintf("%s: {%s}", codecKey(wire), inner))
		}

		parts = append(parts, "\"fields\": {"+strings.Join(rendered, ", ")+"}")
	}

	if len(entry.Required) > 0 {
		required := make([]string, 0, len(entry.Required))
		for _, name := range entry.Required {
			required = append(required, jsonString(name))
		}

		parts = append(parts, "\"required\": ["+strings.Join(required, ", ")+"]")
	}

	if entry.Items != "" {
		parts = append(parts, "\"items\": "+ref(entry.Items))
	}

	if entry.Values != "" {
		parts = append(parts, "\"values\": "+ref(entry.Values))
	}

	if entry.Discriminator != nil {
		tags := make([]string, 0, len(entry.Discriminator.Map))
		for tag := range entry.Discriminator.Map {
			tags = append(tags, tag)
		}

		sort.Strings(tags)

		mapped := make([]string, 0, len(tags))
		for _, tag := range tags {
			mapped = append(mapped, fmt.Sprintf("%s: %s", codecKey(tag), ref(entry.Discriminator.Map[tag])))
		}

		parts = append(parts, fmt.Sprintf("\"discriminator\": {\"wire\": %s, \"map\": {%s}}",
			jsonString(entry.Discriminator.Wire), strings.Join(mapped, ", ")))
	}

	if len(entry.Members) > 0 {
		members := make([]string, 0, len(entry.Members))
		for _, member := range entry.Members {
			members = append(members, ref(member))
		}

		parts = append(parts, "\"members\": ["+strings.Join(members, ", ")+"]")
	}

	return "{" + strings.Join(parts, ", ") + "}"
}

// codecKey renders a property key, guarding the one name that would set a
// prototype instead of a property.
//
// The table used to run every entry through protectDunderProto after
// marshalling. Rendering a key at a time makes the same guard a direct check
// rather than substring surgery on already-encoded text.
func codecKey(name string) string {
	if name == "__proto__" {
		return "[\"__proto__\"]"
	}

	return jsonString(name)
}

// GenerateModules produces the reference-only runtime and one module per
// codec.
//
// The runtime comes with them rather than from codecs.ts because codecs.ts
// declares the table: a module that imported `decode` from there would reach
// every codec in the document, which is the cost this split exists to remove.
func (g *CodecGenerator) GenerateModules(
	spec *client.APISpec, config client.GeneratorConfig,
) map[string]string {
	table := buildCodecTable(spec, config)

	ids := sortedCodecIDs(table.entries)
	naming := newCodecModuleNaming(ids)

	files := make(map[string]string, len(ids)+1)

	files["src/codec-runtime.ts"] = "// Generated codec runtime, with no table behind it.\n" +
		"//\n" +
		"// The same walk codecs.ts carries, rendered without the branch that resolves\n" +
		"// a string id, so importing it reaches no codec at all. fetch.ts imports this\n" +
		"// copy and receives the codec itself on the request config.\n\n" +
		codecTypes + "\n" + codecRuntimeFor(false)

	for _, id := range ids {
		entry := table.entries[id]

		var buf strings.Builder

		buf.WriteString("import type { Codec } from '../codec-runtime';\n")

		refs := codecRefs(entry)
		for _, dep := range refs {
			// A self-reference needs no import, and importing a module from
			// itself does not resolve.
			if dep == id {
				continue
			}

			buf.WriteString(fmt.Sprintf("import { %s } from './%s';\n",
				naming.consts[dep], naming.files[dep]))
		}

		buf.WriteString("\n")
		buf.WriteString(fmt.Sprintf("export const %s: Codec = %s;\n",
			naming.consts[id],
			renderCodecEntry(entry, func(dep string) string {
				return "() => " + naming.consts[dep]
			})))

		files["src/codecs/"+naming.files[id]+".ts"] = buf.String()
	}

	return files
}
