package dart

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// Ported verbatim from typescript/opsmanifest.go, typescript/rest.go,
// typescript/facades.go and typescript/capabilities.go: the helpers that
// decide what a table row says. Kept identical so the parity test compares
// two renderings of one decision, not two decisions.

// entityRow is one line of the `entities` table: an entity, or a type that only
// routes typenames onward.
type entityRow struct {
	name    string
	idField string
	fields  map[string]string
}

// entityRows merges the spec's entities and routing types into the single table
// the runtime reads, sorted by typename.
//
// One table because the runtime has one question -- "given this typename, where
// do I descend and what identifies it" -- and the answer for a routing type is
// just that second half being empty. The two are kept apart in Go, where
// `spec.Entities[name]` is read as "is this an entity"; they are disjoint by
// construction there, so no name can arrive here twice.
//
// Go randomises map iteration and this file is byte-diffed by CI, so the sort
// is load-bearing rather than cosmetic.
//
// EntityRef.IDField and the KEYS of EntityRef.Fields arrive here as verbatim
// wire names -- they trace back to schema.Properties keys, i.e. pre-rename --
// and are renamed here, through clientFieldName, into the names the DECODED
// payload carries. See renameEntityField for why that must happen in the same
// change that lets the runtime decode at all.
func entityRows(spec *client.APISpec, config client.GeneratorConfig) []entityRow {
	rows := make([]entityRow, 0, len(spec.Entities)+len(spec.RoutingTypes))

	for _, table := range []map[string]*client.EntityRef{spec.Entities, spec.RoutingTypes} {
		for name, ref := range table {
			if ref == nil {
				continue
			}

			rows = append(rows, entityRow{
				name:    name,
				idField: renameEntityField(name, ref.IDField, config),
				fields:  renameEntityFields(name, ref.Fields, config),
			})
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	return rows
}

// renameEntityField resolves one wire property name of typeName into the
// client-side name the decoded payload carries.
//
// clientFieldName -- the SAME function generator.go renders every interface
// property through and codecs.go builds every codec entry from -- is what
// does the work, deliberately rather than a second copy of the camel/pascal/
// snake rule here. A second implementation of a naming rule is how the two
// drift, and a drift between the codec table and this one is invisible: the
// normalizer would look for a field the payload does not have, decide the type
// is not an entity, and store nothing, with no error anywhere.
//
// typeName is passed as clientFieldName's schema-name argument because an entity's
// typename IS its component-schema name -- the same id codecIDFor derives for
// that schema's own properties -- so a schema-scoped FieldOverrides entry
// ("Order.order_number") reaches the codec table and this table identically,
// which is the only way they can stay in step.
//
// An empty wireName means "this row has no identity" (an envelope, or a hop
// between entities) and is returned untouched rather than handed to
// clientFieldName, whose FieldOverrides lookup would otherwise consult the
// meaningless key "Order.".
func renameEntityField(typeName, wireName string, config client.GeneratorConfig) string {
	if wireName == "" {
		return ""
	}

	return clientFieldName(typeName, wireName, config)
}

// renameEntityFields renames the KEYS of a field-edge map and copies its
// VALUES verbatim.
//
// The asymmetry is the whole point. A key is a JSON property of typeName and
// gets renamed with everything else the payload carries; a value is a
// TYPENAME -- the name of another row in this very table, and of a generated
// TypeScript interface -- which is not a field name at all. Renaming a value
// would point the edge at a table key that does not exist ("Order.customer ->
// customer" instead of "Customer"), breaking the entities lookup outright for
// every nested entity.
//
// Returns nil for an empty input so writeEntities' `len(row.fields) > 0` check
// still omits the key entirely rather than emitting `fields: {}`.
func renameEntityFields(typeName string, fields map[string]string, config client.GeneratorConfig) map[string]string {
	if len(fields) == 0 {
		return nil
	}

	renamed := make(map[string]string, len(fields))
	for wireName, targetTypeName := range fields {
		renamed[renameEntityField(typeName, wireName, config)] = targetTypeName
	}

	return renamed
}

// renameDerivedIDTags rewrites the one cache tag whose placeholder is a
// schema property name -- the item tag DeriveTags builds as
// `Type:{IDField}` -- into the client-side name, for the same reason
// entityRows renames idField.
//
// The runtime resolves a `provides` template against the request arguments
// and then the RESPONSE (see resolveTags/QueryRegistry#settle), and the
// response it is handed is the one the codec already decoded. A template
// still saying `{order_number}` against a payload carrying `orderNumber`
// resolves to nothing, the query is registered under no item tag at all, and
// a later write to that order invalidates nothing -- the same silent
// stops-caching failure a wire-named idField causes, one table over.
//
// The rewrite is deliberately an EXACT match against the tag DeriveTags
// would have produced for this endpoint's entity, not a general pass over
// every placeholder. A route may declare arbitrary templates
// (`Customer:{req.customerId}`, `Shipment:{res.shipment.id}`), whose
// segments name properties of types this function has no way to resolve; a
// general rewrite would have to guess a namespace per segment, and guessing
// wrong here produces a tag that silently matches nothing. Matching only the
// derived form renames exactly what the generator itself wrote and leaves
// every hand-declared template alone. A declared template that happens to be
// byte-identical to the derived one means the same thing anyway.
//
// Under NamingPreserve with no FieldOverrides the replacement equals the
// original, so this is an identity pass and the emitted bytes do not change.
func renameDerivedIDTags(tags []string, entity *client.EntityRef, config client.GeneratorConfig) []string {
	if entity == nil || entity.IDField == "" || len(tags) == 0 {
		return tags
	}

	derived := entity.Type + ":{" + entity.IDField + "}"
	renamed := entity.Type + ":{" + renameEntityField(entity.Type, entity.IDField, config) + "}"

	if derived == renamed {
		return tags
	}

	out := make([]string, len(tags))

	for i, tag := range tags {
		if tag == derived {
			out[i] = renamed
		} else {
			out[i] = tag
		}
	}

	return out
}

// operationSecurityKeys flattens an endpoint's security requirements into the
// scheme keys `security` carries, sorted and deduplicated.
//
// A requirement's own Scopes are dropped here: this table exists to let an
// AuthProvider attach a credential, not to answer capability questions, and
// the scope-aware view of the same data already lives in capabilities.ts.
// Two SecurityRequirement entries naming the same scheme with different
// scopes -- a legal OpenAPI OR-of-scope-sets -- would otherwise emit the key
// twice, so the map dedupes before sorting.
func operationSecurityKeys(reqs []client.SecurityRequirement) []string {
	if len(reqs) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(reqs))

	keys := make([]string, 0, len(reqs))
	for _, req := range reqs {
		if req.SchemeName == "" || seen[req.SchemeName] {
			continue
		}

		seen[req.SchemeName] = true

		keys = append(keys, req.SchemeName)
	}

	sort.Strings(keys)

	return keys
}

// duplexMessageNames picks the lowest-sorted message name for each direction,
// the same first-message tie-break applyOperationMessages uses for directional
// schemas, so the output is stable across runs.
func duplexMessageNames(metadata map[string]any) (string, string) {
	names, _ := metadata["messages"].(map[string]string)
	send, receive := "", ""

	for _, name := range sortedKeys(names) {
		switch names[name] {
		case "send":
			if send == "" {
				send = name
			}
		case "receive":
			if receive == "" {
				receive = name
			}
		}
	}

	return send, receive
}

// mediaEssence is a content type without its parameters, trimmed and
// lower-cased: "application/problem+json; charset=utf-8" -> "application/problem+json".
func mediaEssence(contentType string) string {
	essence, _, _ := strings.Cut(contentType, ";")

	return strings.ToLower(strings.TrimSpace(essence))
}

// isJSONMediaType reports whether a content type carries JSON: application/json,
// text/json, or any structured-syntax +json type (application/problem+json,
// application/vnd.api+json). One rule for every generator, because a +json body
// is JSON on the wire whatever its media type is called.
func isJSONMediaType(contentType string) bool {
	essence := mediaEssence(contentType)

	return essence == "application/json" || essence == "text/json" || strings.HasSuffix(essence, "+json")
}

// jsonMediaKey picks the JSON entry of a body's content map, deterministically:
// application/json when declared, otherwise the first JSON-like key in sorted
// order. With withSchema set an entry that has no schema is skipped, as
// requestBodyContentType does for a schemaless application/json. Returns "" when
// no entry qualifies.
func jsonMediaKey(content map[string]*client.MediaType, withSchema bool) string {
	usable := func(key string) bool {
		media, ok := content[key]

		return ok && media != nil && (!withSchema || media.Schema != nil)
	}

	if usable("application/json") {
		return "application/json"
	}

	for _, key := range sortedKeys(content) {
		if isJSONMediaType(key) && usable(key) {
			return key
		}
	}

	return ""
}

// requestBodyContentType selects the single content type an endpoint's
// request body is generated for, following the precedence responseBodyType
// established for responses -- application/json, then text/*, then anything
// else -- with one deliberate difference. Here application/json only wins
// when it actually carries a schema; a schemaless entry is skipped, INCLUDING
// in the final fallback, because mapping it back to `any` would erase a
// sibling multipart/form-data or octet-stream body that the caller can
// actually use. responseBodyType has no equivalent hazard: its fallback
// returns a hard "Blob" and can never re-enter the JSON branch.
// RequestBody.Content is a map, so an endpoint
// COULD declare more than one content type (e.g. a spec offering both JSON
// and multipart upload for the same operation); a generated TypeScript
// method can only accept one shape for its `body` parameter, so exactly one
// content type must win, and it must win the same way every generation run
// picks it -- hence sorting rather than ranging the map directly. Returns ""
// when there is no usable request body at all.
func requestBodyContentType(endpoint *client.Endpoint) string {
	if endpoint.RequestBody == nil || len(endpoint.RequestBody.Content) == 0 {
		return ""
	}

	if key := jsonMediaKey(endpoint.RequestBody.Content, true); key != "" {
		return key
	}

	for _, contentType := range sortedKeys(endpoint.RequestBody.Content) {
		if strings.HasPrefix(contentType, "text/") {
			return contentType
		}
	}

	// Fall back to the first remaining content type, skipping
	// "application/json" -- reaching here means the JSON entry exists but
	// carries no schema (`content: {application/json: {}}` is legal
	// OpenAPI), so selecting it would map back to `any` via
	// requestBodyParamType and silently erase a perfectly good
	// multipart/form-data or application/octet-stream sibling. "a" sorts
	// first, so without this skip the schemaless JSON entry wins every
	// mixed-content body.
	keys := sortedKeys(endpoint.RequestBody.Content)
	for _, contentType := range keys {
		if !isJSONMediaType(contentType) {
			return contentType
		}
	}

	// Schemaless application/json really was the only option.
	if len(keys) == 0 {
		return ""
	}

	return keys[0]
}

// endpointLabel returns a short, human-identifiable name for an endpoint, for
// use in a generation-time warning: its OperationID when the spec declares
// one (the common case, and the same identifier the generated method itself
// is named after), or "METHOD path" as a fallback for an endpoint with no
// OperationID at all.
func endpointLabel(endpoint *client.Endpoint) string {
	if endpoint.OperationID != "" {
		return endpoint.OperationID
	}

	return endpoint.Method + " " + endpoint.Path
}

// schemaCodecRef returns the codec table id (see codecs.go) for a JSON
// body/response schema at an endpoint boundary, or "" when none applies.
//
// Two shapes resolve to something real:
//
//   - a direct $ref to a named component schema -- codecs.go's
//     CodecGenerator.Generate walks spec.Schemas (see its top-level loop),
//     registering entries keyed by component schema NAME;
//   - an array wrapping a direct $ref (`{type: array, items: $ref X}`), the
//     single most common OpenAPI "list of X" wire shape. This does NOT come
//     from the same top-level walk -- an endpoint body/response is not
//     itself a named schema -- so codecs.go's
//     registerEndpointArrayBodyCodecs registers a synthetic id for exactly
//     this shape (see arrayRefCodecID), and this function must return that
//     SAME id for the two sides to agree on what to look up.
//
// Anything else -- an inline object, oneOf/anyOf, allOf, or an array of
// anything but a direct $ref (an inline item schema, a nested array, etc.) --
// returns "": there is no codec-table entry for those shapes, and referencing
// a nonexistent id would be silently inert at best.
func schemaCodecRef(schema *client.Schema) string {
	if schema == nil {
		return ""
	}

	if name := refName(schema.Ref); name != "" {
		return name
	}

	if schema.Type == "array" && schema.Items != nil {
		if itemName := refName(schema.Items.Ref); itemName != "" {
			return arrayRefCodecID(itemName)
		}
	}

	return ""
}

// requestBodyCodecRef returns the codec table id (see schemaCodecRef) for an
// endpoint's request body, and a warning to append to RESTGenerator.warnings
// when one is needed.
//
// Only application/json ever gets an id at all: the codecs.go table renames
// JSON object shapes, and executeRequest's encode() call site
// (fetch_client.go) is itself gated to the JSON-serialisation branch only, so
// a codec ref on a FormData/URLSearchParams/Blob/octet-stream body would be
// inert at best -- simplest to never emit it, and never warn about it
// either, for those content types: there is no "declared as renamed but
// isn't" lie for a body whose declared TypeScript type was never
// schema-driven in the first place (FormData, Blob, etc. are fixed DOM
// types, not derived from the schema's shape).
//
// Within application/json, a warning is returned (id "") specifically when
// the body has a resolvable schema that schemaCodecRef could NOT turn into
// an id -- an inline schema, or an array of anything but a direct $ref.
// Silence there would be worse than the wrong-rename bug this fixes: the
// generated `body` parameter is still typed in its camelCase TypeScript
// shape (requestBodyParamType/getSchemaTypeName don't change), so it LOOKS
// renamed at the type level while actually being sent wire-cased and
// unrenamed -- exactly the "never renamed but still typed as if it were"
// failure a silent skip would leave in place.
//
// Package-level, not a *RESTGenerator method, because opsmanifest.go emits
// the SAME id into OperationMeta.bodyCodec so the runtime's generic
// `HTTPClient#request` caller applies the identical codec the typed method
// does. Two resolvers would be two answers to "which codec encodes this
// body", and the runtime would silently pick the other one. The warning
// return is the caller's to surface: only rest.go appends it to
// RESTGenerator.warnings, so the manifest reusing this cannot double-report.
func requestBodyCodecRef(endpoint *client.Endpoint) (id string, warning string) {
	contentType := requestBodyContentType(endpoint)
	if !isJSONMediaType(contentType) {
		return "", ""
	}

	media := endpoint.RequestBody.Content[contentType]
	if media == nil || media.Schema == nil {
		return "", ""
	}

	if ref := schemaCodecRef(media.Schema); ref != "" {
		return ref, ""
	}

	return "", fmt.Sprintf(
		"endpoint %q: request body is application/json but its schema is not a direct $ref (or an array of one) to a named component schema -- the generated body parameter is still declared in its camelCase TypeScript shape, but it will be sent wire-cased, unrenamed, because there is no codec-table entry to encode it with",
		endpointLabel(endpoint))
}

// responseCodecRef returns the codec table id (see schemaCodecRef) for an
// endpoint's response, and a warning to append to RESTGenerator.warnings when
// one is needed.
//
// generateReturnType unions every 2xx response into one TypeScript type, but
// decode() is applied unconditionally by executeRequest's JSON branch
// regardless of which status code the server actually returned -- there is no
// per-call information about which 2xx a given response is at the point
// decode() runs. That makes a SINGLE codec id safe only when every JSON 2xx
// response in the set agrees on it:
//
//   - a 2xx with no content (e.g. a 202 ack) contributes nothing to check --
//     it can never reach the JSON decode branch at all, so it needs no
//     warning either;
//   - a 2xx whose content is JSON but has no schema, or isn't JSON at all
//     (text/*, Blob), also contributes nothing and needs no warning -- decode()
//     never runs for those response shapes either (see fetch_client.go's
//     content-type branching), and their declared TypeScript type was never
//     schema-rename-shaped to begin with;
//   - a 2xx whose JSON schema resolves to no codec id at all (an inline
//     schema, or an array of anything but a direct $ref) returns a warning
//     immediately -- bailing out entirely rather than risk decoding some
//     OTHER status's differently-shaped response through an unrelated named
//     schema's codec;
//   - two DIFFERENT resolved ids across the 2xx set (e.g. 200 -> "User",
//     201 -> "Team") have no single id correct for every status this call
//     could resolve to -- a warning naming both is returned rather than
//     guessing and silently mis-rendering whichever status wasn't chosen.
//
// Both warning paths matter for the same reason requestBodyCodecRef's does:
// generateReturnType's declared union still promises a renamed shape
// (`Promise<types.User | types.Team>`), so silently emitting no
// responseCodec at all would leave that promise looking honored at the type
// level while nothing actually renames the value at runtime.
//
// Responses is a map[int]*client.Response, so status codes are collected and
// sorted before iterating, matching generateReturnType's own determinism
// requirement (ranging the map directly would make the emitted output
// non-deterministic across runs).
//
// Package-level for the same reason requestBodyCodecRef is: opsmanifest.go
// emits this id into OperationMeta.responseCodec, and the runtime decoding a
// response through a DIFFERENT codec than the typed method would is exactly
// the contradiction between a generated client and its own generated types
// this function is now shared to prevent.
func responseCodecRef(endpoint *client.Endpoint) (id string, warning string) {
	codes := make([]int, 0, len(endpoint.Responses))

	for code := range endpoint.Responses {
		if code >= 200 && code < 300 {
			codes = append(codes, code)
		}
	}

	sort.Ints(codes)

	var ref string
	sawJSON := false

	for _, code := range codes {
		resp := endpoint.Responses[code]
		if resp == nil || len(resp.Content) == 0 {
			continue
		}

		media, ok := resp.Content[jsonMediaKey(resp.Content, false)]
		if !ok || media == nil || media.Schema == nil {
			continue
		}

		name := schemaCodecRef(media.Schema)
		if name == "" {
			return "", fmt.Sprintf(
				"endpoint %q: response status %d is application/json but its schema is not a direct $ref (or an array of one) to a named component schema -- the declared return type is still a renamed-shaped TypeScript type, but this response will never actually be decoded",
				endpointLabel(endpoint), code)
		}

		if !sawJSON {
			sawJSON = true
			ref = name

			continue
		}

		if ref != name {
			return "", fmt.Sprintf(
				"endpoint %q: JSON 2xx responses resolve to more than one named schema (%q and %q) -- there is no single codec id correct for every status this call could resolve to, so none of them will be decoded",
				endpointLabel(endpoint), ref, name)
		}
	}

	return ref, ""
}

// isReadMethod reports whether an endpoint reads rather than writes. Caching
// a POST would serve a stale answer to a request whose entire purpose was to
// change something.
func isReadMethod(method string) bool {
	m := strings.ToUpper(method)

	return m == "GET" || m == "HEAD"
}

// sortedUniqueStrings returns values sorted, deduplicated, and with empty
// entries dropped.
//
// Roles and permissions arrive here already sorted, deduplicated and free of
// empty entries: the production paths that populate Endpoint.Authorization
// (resolveEndpointAuthz, routeToEndpoint) normalise before the Endpoint is
// built, and EndpointAuthorization normalises again on the way out, so even a
// hand-built Endpoint cannot reach this in a raw shape. The call below is
// therefore idempotent today and is kept anyway, for the same reason
// capabilityAlternatives re-sorts a SecurityRequirement's scopes rather than
// trusting them pre-sorted: the emitted table must not be able to become
// order-dependent, and CI's byte-diff has no way to tell "input arrived
// sorted" from "output happens to be sorted this run" apart.
//
// It is not the guard against cross-language divergence, though it once was
// the only one. Go's generator had no equivalent, so an unnormalised Endpoint
// produced two different tables; that is fixed at EndpointAuthorization now,
// where one normalisation serves every generator instead of each language
// remembering to do its own.
func sortedUniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))

	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}

		seen[value] = true

		out = append(out, value)
	}

	sort.Strings(out)

	return out
}
