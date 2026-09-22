package typescript

import (
	"regexp"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// placeholderPattern matches one `{...}` placeholder of a cache tag template.
// The same shape the runtime's resolver reads, so the two agree on what a
// placeholder is.
var placeholderPattern = regexp.MustCompile(`\{([^{}]*)\}`)

// renameDeclaredTags rewrites the placeholders of hand-declared tag templates
// from wire property names to the client-side names the runtime resolves
// against.
//
// A route declares `WithInvalidates("Customer:{req.customer_id}")` naming the
// JSON property, because that is what the server can see. The runtime resolves
// that template against a body the caller built in TypeScript and a response
// the codec has already decoded, both of which carry the CLIENT-side name
// under FieldNaming. Under the default camelCase that is `customerId`, so the
// wire spelling names nothing, the template resolves to nothing, and the tag
// invalidates nothing. The declaration is correct, the manifest is what has to
// change, and this is the same rename the entities table and the derived item
// tag already go through -- see renameDerivedIDTags, which handles the one
// template the generator writes itself and leaves the declared ones to this.
//
// Each dotted segment is resolved through the schema graph, one hop at a time,
// so `{res.customer.external_id}` renames `external_id` under `Customer` --
// the type the hop landed on, which is also the namespace a FieldOverride for
// it is keyed under. A segment that names no property of the type reached
// stops the rewrite there: the rest of the path is left as written, because
// guessing at it would be inventing a name.
//
// Path and query parameters are NOT renamed, in either scope. The transport
// substitutes them into the URL by their wire name, and that is the key the
// caller supplies them under, so `{customer_id}` naming a path parameter
// stays `{customer_id}`. The one thing this cannot settle is a bare placeholder
// that names both a parameter and a body property under different spellings;
// the parameter wins here, exactly as it wins at runtime.
//
// Under NamingPreserve with no FieldOverrides the rename is the identity, and
// the input slice is returned as is, so the file the generator wrote before this
// existed is the file it writes now.
func renameDeclaredTags(
	tags []string, ep *client.Endpoint, spec *client.APISpec, config client.GeneratorConfig,
) []string {
	if len(tags) == 0 || ep == nil || spec == nil {
		return tags
	}

	var out []string

	for i, tag := range tags {
		renamed := placeholderPattern.ReplaceAllStringFunc(tag, func(match string) string {
			expr := strings.TrimSpace(match[1 : len(match)-1])

			if next := renamePlaceholder(expr, ep, spec, config); next != expr {
				return "{" + next + "}"
			}

			return match
		})

		if renamed == tag {
			if out != nil {
				out[i] = tag
			}

			continue
		}

		if out == nil {
			out = make([]string, len(tags))
			copy(out, tags[:i])
		}

		out[i] = renamed
	}

	if out == nil {
		return tags
	}

	return out
}

// renamePlaceholder renames one placeholder expression, `req.a.b`, `res.a.b`
// or a bare `a.b`, following the runtime's own lookup order: an explicit scope
// searches only its side; a bare expression searches the request first and the
// response second.
func renamePlaceholder(
	expr string, ep *client.Endpoint, spec *client.APISpec, config client.GeneratorConfig,
) string {
	segments := strings.Split(expr, ".")

	prefix := ""
	request, response := true, true

	switch segments[0] {
	case "req":
		prefix, response = "req.", false
		segments = segments[1:]
	case "res":
		prefix, request = "res.", false
		segments = segments[1:]
	}

	if len(segments) == 0 || segments[0] == "" {
		return expr
	}

	if request && isParameterName(ep, segments[0]) {
		return expr
	}

	root := ""

	if request {
		root = rootOf(spec, requestBodyRoot(ep), segments[0])
	}

	if root == "" && response {
		root = rootOf(spec, responseRoot(ep), segments[0])
	}

	if root == "" {
		return expr
	}

	return prefix + strings.Join(renameSegments(segments, root, spec, config), ".")
}

// renameSegments walks a dotted path from a named root type, renaming each
// segment under the type it belongs to and descending into the type the
// property names. Stops at the first segment the schema cannot answer for and
// leaves the remainder as written. Numeric segments index an array and pass
// through unchanged: the element type was already resolved at the hop before.
func renameSegments(
	segments []string, root string, spec *client.APISpec, config client.GeneratorConfig,
) []string {
	out := make([]string, len(segments))
	copy(out, segments)

	typ := root
	props := schemaProperties(spec, spec.Schemas[root])

	for i, segment := range segments {
		if props == nil {
			break
		}

		if isIndex(segment) {
			continue
		}

		prop, ok := props[segment]
		if !ok {
			break
		}

		out[i] = tsFieldName(typ, segment, config)
		typ, props = descend(spec, typ, segment, prop)
	}

	return out
}

// descend resolves the type a property lands on: the component a $ref names,
// the element of an array, or an inline object. The inline case keeps the
// parent's name qualified by the property, which is the namespace the codec
// table keys a nested inline shape under.
func descend(
	spec *client.APISpec, parent, property string, prop *client.Schema,
) (string, map[string]*client.Schema) {
	if prop == nil {
		return "", nil
	}

	if name := client.ComponentRefName(prop.Ref); name != "" {
		return name, schemaProperties(spec, spec.Schemas[name])
	}

	if prop.Type == "array" && prop.Items != nil {
		return descend(spec, parent, property, prop.Items)
	}

	if props := schemaProperties(spec, prop); props != nil {
		return parent + "." + property, props
	}

	return "", nil
}

// rootOf returns root when the named type carries a property called head, and
// "" otherwise -- the check that decides which side of a bare placeholder the
// rename follows.
func rootOf(spec *client.APISpec, root, head string) string {
	if root == "" {
		return ""
	}

	if _, ok := schemaProperties(spec, spec.Schemas[root])[head]; !ok {
		return ""
	}

	return root
}

// requestBodyRoot is the component the JSON request body is, or the element
// type when it is an array of one. "" when the body has no named type.
func requestBodyRoot(ep *client.Endpoint) string {
	id, _ := requestBodyCodecRef(ep)

	return strings.TrimPrefix(id, "[]")
}

// responseRoot is requestBodyRoot's twin for the success response.
func responseRoot(ep *client.Endpoint) string {
	id, _ := responseCodecRef(ep)

	return strings.TrimPrefix(id, "[]")
}

// schemaProperties is EntityProperties with a nil guard, so a root the schema
// table does not hold reads as "no properties" rather than a crash.
func schemaProperties(spec *client.APISpec, schema *client.Schema) map[string]*client.Schema {
	if schema == nil {
		return nil
	}

	return client.EntityProperties(spec, schema)
}

// isParameterName reports whether the endpoint has a path or query parameter
// with this exact wire name. Header and cookie parameters are not part of a
// tag context and are not consulted.
func isParameterName(ep *client.Endpoint, name string) bool {
	for _, params := range [][]client.Parameter{ep.PathParams, ep.QueryParams} {
		for _, p := range params {
			if p.Name == name {
				return true
			}
		}
	}

	return false
}

// isIndex reports whether a segment is an array index: all digits, non-empty.
func isIndex(segment string) bool {
	if segment == "" {
		return false
	}

	for _, r := range segment {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}
