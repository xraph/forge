package dart

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// topLevelReserved are the lowerCamel top-level names a binding must not take:
// forge_client's functions, which an app imports beside the generated
// package, and the generated package's own top-level declarations.
var topLevelReserved = map[string]bool{
	"query": true, "mutation": true, "configureClient": true, "setClient": true, "getClient": true,
	"entityKey": true, "isRef": true, "normalize": true, "resolveTag": true, "resolveTags": true,
	"queryKey": true, "microtaskScheduler": true, "retryable": true, "statusOf": true,
	"operationUrl": true, "realSleep": true, "realClock": true, "targetOf": true,
	"microtaskCommitScheduler": true, "revalidateOnFocus": true, "revalidateOnReconnect": true,
	"poll": true, "webSocketConnection": true, "eventSourceConnection": true,
	"webTransportConnection": true, "forgeStreamingDecoder": true, "dehydrate": true,
	"hydrate": true, "foldSyncStatus": true, "memoryStorage": true,
	"operations": true, "entities": true, "streams": true, "securitySchemes": true, "sync": true,
	"requiredCapabilities": true, "requiredAuthorization": true, "setPrincipal": true,
	"capabilitiesKnown": true, "can": true, "hasRole": true, "hasPermission": true,
	"missingCapabilities": true, "canCall": true, "paginateAll": true, "collectAll": true,
	"apiErrorOf": true, "decodeCached": true, "makeRef": true, "isOptimistic": true,
	"operationQueryKey": true, "operationName": true,
	"streamUri": true, "bearerToken": true, "liveOpen": true,
}

// bindingReserved is every top-level name a binding must not take:
// topLevelReserved, the op constants ops.dart declares (the barrel exports
// both, so a binding named opWidgets is an ambiguous export), and the support
// helpers a binding file calls (a binding named deepEquals would shadow the
// one its Args == calls, and one named decodeList would make its file show a
// helper it never uses).
func bindingReserved(constants []string) map[string]bool {
	out := copySet(topLevelReserved)

	for _, c := range constants {
		out[c] = true
	}

	for _, s := range supportSymbols {
		out[s] = true
	}

	return out
}

// param is one path, query or header parameter of an operation.
type param struct {
	wire     string
	member   string
	in       string
	typ      dartType
	required bool
	doc      string
}

func (p param) declType() string {
	if p.required {
		return p.typ.name
	}

	return p.typ.nullableName()
}

// bodyParam is an operation's request body.
type bodyParam struct {
	// kind is "json", "text", "bytes", "form" or "multipart".
	kind     string
	member   string
	typ      dartType
	required bool
	codec    string
	// contentType is the declared request content type, sent instead of the
	// default for the kind.
	contentType string
	// flatten holds a PATCH body's properties when they become argument
	// fields of their own, so an optional one can be Unchanged or Assign.
	flatten []flatField
}

// flatField is one PATCH body property as an argument field.
type flatField struct {
	field

	member string
}

// operation is everything every Dart emitter needs to know about one
// endpoint, resolved once.
type operation struct {
	ep       *client.Endpoint
	key      string
	id       string
	constant string
	binding  string
	args     string
	file     string
	params   []param
	body     *bodyParam
	// response is "json", "text", "bytes" or "none".
	response      string
	responseType  dartType
	responseCodec string
	entity        *componentModel
	row           client.TableOp
	imports       map[string]bool
}

// formContentType is the type a form body goes out as, from the RestClient
// and the transport alike. A multipart body is sent as these fields too.
const formContentType = "application/x-www-form-urlencoded"

// requestContentType is the row's OperationMeta.requestContentType: the type
// forge_client's transport sends the body as. It is empty, which the runtime
// reads as JSON, for no body and for a body sent as plain application/json.
// It is a Dart runtime field only: TypeScript dispatches on the body's
// runtime type instead, so it is not a column of the shared tables.
func (op *operation) requestContentType() string {
	if op.body == nil {
		return ""
	}

	switch op.body.kind {
	case "form", "multipart":
		return formContentType
	case "json":
		if op.body.contentType == defaultBodyType["json"] {
			return ""
		}
	}

	return op.body.contentType
}

// hasArgs reports whether the operation needs an Args class rather than
// NoArgs.
func (op *operation) hasArgs() bool { return len(op.params) > 0 || op.body != nil }

// planOperations resolves every endpoint, in endpoint order.
func planOperations(spec *client.APISpec, config client.GeneratorConfig, reg *registry) ([]*operation, []string) {
	var warnings []string

	keys := operationKeys(spec.Endpoints)
	ids := uniqueNames(keys, func(k string) string { return "op_" + fileStem(k) }, map[string]bool{}, false)
	constants := uniqueNames(keys, func(k string) string { return "op" + typeIdent(k) }, map[string]bool{}, false)
	bindingTaken := bindingReserved(constants)
	bindings := uniqueNames(keys, func(k string) string { return memberIdent(k, bindingTaken) }, copySet(bindingTaken), false)
	argNames := uniqueNames(keys, func(k string) string { return typeIdent(k) + "Args" }, reg.taken, false)
	files := uniqueNames(keys, fileStem, map[string]bool{}, true)

	rows := entityRows(spec, config)

	known := make(map[string]bool, len(rows))
	for _, row := range rows {
		known[row.name] = true
	}

	ops := make([]*operation, len(spec.Endpoints))

	for i := range spec.Endpoints {
		ep := &spec.Endpoints[i]
		op := &operation{
			ep: ep, key: keys[i], id: ids[i], constant: constants[i], binding: bindings[i],
			args: argNames[i], file: files[i], imports: map[string]bool{},
			row: operationRow(ep, spec, config, known),
		}

		c := rctx{imports: op.imports}
		members := copySet(argsReserved)

		addParam := func(p client.Parameter, required bool) {
			typ := reg.resolve(p.Schema, c)
			if typ.dynamic {
				typ = castType("String")
			}

			op.params = append(op.params, param{
				wire:     p.Name,
				member:   uniqueNames([]string{p.Name}, func(s string) string { return memberIdent(s, argsReserved) }, members, false)[0],
				in:       p.In,
				typ:      typ,
				required: required,
				doc:      p.Description,
			})
		}

		for _, p := range ep.PathParams {
			addParam(p, true)
		}

		for _, p := range ep.QueryParams {
			addParam(p, p.Required)
		}

		for _, p := range ep.HeaderParams {
			addParam(p, p.Required)
		}

		for _, p := range ep.CookieParams {
			warnings = append(warnings, fmt.Sprintf(
				"operation %q: cookie parameter %q is not generated; the Dart client sends no cookies of its own",
				op.key, p.Name))
		}

		op.body = planBody(ep, reg, c, members)

		if op.body != nil && op.body.kind == "multipart" {
			warnings = append(warnings, fmt.Sprintf(
				"operation %q: a multipart body is sent as form fields; file parts need a hand-written request",
				op.key))
		}

		op.response, op.responseType = planResponse(ep, reg, c)
		op.responseCodec, _ = responseCodecRef(ep)

		if ep.Entity != nil {
			if m := reg.models[ep.Entity.Type]; m != nil && m.kind == kindClass {
				op.entity = m
				op.imports[m.schemaName] = true
			}
		}

		ops[i] = op
	}

	return ops, warnings
}

// planBody resolves the request body. A PATCH whose JSON body is a class
// component is flattened into argument fields so "leave unchanged" and "set
// to null" stay distinct; every other body is one argument.
func planBody(ep *client.Endpoint, reg *registry, c rctx, members map[string]bool) *bodyParam {
	contentType := requestBodyContentType(ep)
	if contentType == "" {
		return nil
	}

	required := ep.RequestBody.Required
	claim := func(name string) string {
		return uniqueNames([]string{name}, func(s string) string { return memberIdent(s, argsReserved) }, members, false)[0]
	}

	essence := mediaEssence(contentType)

	switch {
	case isJSONMediaType(contentType):
		var schema *client.Schema
		if media := ep.RequestBody.Content[contentType]; media != nil {
			schema = media.Schema
		}

		codec, _ := requestBodyCodecRef(ep)
		body := &bodyParam{kind: "json", required: required, codec: codec, contentType: contentType}

		if m := reg.models[client.ComponentRefName(schemaRef(schema))]; strings.EqualFold(ep.Method, "PATCH") && m != nil && m.kind == kindClass {
			if cls, ok := m.decls[0].(*classDecl); ok && len(cls.fields) > 0 {
				c.imports[m.schemaName] = true
				body.member = "body"
				body.typ = modelType(m.dartName, m.schemaName)

				for _, f := range cls.fields {
					for _, name := range f.typ.imports {
						c.imports[name] = true
					}

					member := f.member
					if members[member] || member == "body" {
						member = "body" + upperFirst(member)
					}

					body.flatten = append(body.flatten, flatField{field: f, member: claim(member)})
				}

				return body
			}
		}

		body.member = claim("body")

		if schema == nil {
			body.typ = dynamicType()
		} else {
			body.typ = reg.resolve(schema, c)
		}

		return body

	case essence == "multipart/form-data":
		return &bodyParam{kind: "multipart", member: claim("body"), typ: formFieldsType(), required: required}

	case essence == formContentType:
		return &bodyParam{kind: "form", member: claim("body"), typ: formFieldsType(), required: required}

	case isTextMediaType(contentType):
		return &bodyParam{kind: "text", member: claim("body"), typ: castType("String"), required: required, contentType: contentType}
	}

	// Bytes compare by content, like every other list an Args class holds.
	t := castType("Uint8List")
	t.typedData, t.deep = true, true

	return &bodyParam{kind: "bytes", member: claim("body"), typ: t, required: required, contentType: contentType}
}

// formFieldsType is a form or multipart body: string fields by name, compared
// by content so two Args built from equal fields are equal.
func formFieldsType() dartType {
	t := castType("Map<String, String>")
	t.deep = true

	return t
}

func schemaRef(s *client.Schema) string {
	if s == nil {
		return ""
	}

	return s.Ref
}

// planResponse resolves the response the lowest 2xx with a body declares.
// JSON wins whenever any content type is JSON-like (application/json first,
// then the +json types in sorted order): to its schema's Dart type, or to
// Object? when the entry has no schema. Otherwise text types give String and
// anything else bytes. No body at all is none.
func planResponse(ep *client.Endpoint, reg *registry, c rctx) (string, dartType) {
	codes := make([]int, 0, len(ep.Responses))
	for code := range ep.Responses {
		if code >= 200 && code < 300 {
			codes = append(codes, code)
		}
	}

	sort.Ints(codes)

	for _, code := range codes {
		resp := ep.Responses[code]
		if resp == nil || len(resp.Content) == 0 {
			continue
		}

		if key := jsonMediaKey(resp.Content, false); key != "" {
			if schema := resp.Content[key].Schema; schema != nil {
				return "json", reg.resolve(schema, c)
			}

			return "json", dynamicType()
		}

		if slices.ContainsFunc(sortedKeys(resp.Content), isTextMediaType) {
			return "text", castType("String")
		}

		t := castType("Uint8List")
		t.typedData = true

		return "bytes", t
	}

	return "none", dynamicType()
}

// operationRow resolves every value one OperationMeta carries. Ported from
// the TypeScript generator's operationRow, with codec ids always resolved
// because a Dart client always carries its codecs. The method is upper case
// in both.
func operationRow(ep *client.Endpoint, spec *client.APISpec, config client.GeneratorConfig, known map[string]bool) client.TableOp {
	row := client.TableOp{
		Method:     strings.ToUpper(ep.Method),
		Path:       ep.Path,
		StaleTime:  ep.StaleTime,
		Idempotent: ep.Idempotent,
		Security:   operationSecurityKeys(ep.Security),
		Provides: renameDerivedIDTags(
			renameDeclaredTags(ep.CacheTags.Provides, ep, spec, config), ep.Entity, config),
		Invalidates: renameDerivedIDTags(
			renameDeclaredTags(ep.CacheTags.Invalidates, ep, spec, config), ep.Entity, config),
	}

	if ep.Entity != nil {
		row.Entity = ep.Entity.Type
	}

	if known[ep.RootType] {
		row.RootType = ep.RootType
	}

	row.BodyCodec, _ = requestBodyCodecRef(ep)
	row.ResponseCodec, _ = responseCodecRef(ep)

	return row
}

// streamRows lists the stream bindings, ported from the TypeScript
// generator: entity bindings by channel path, then duplex channels by path.
// A row whose entity is a component names that component's codec.
func streamRows(spec *client.APISpec) []client.TableStream {
	type channel struct {
		path     string
		bindings []client.StreamBinding
	}

	var (
		channels []channel
		duplexes []client.TableStream
	)

	for i := range spec.WebSockets {
		ws := &spec.WebSockets[i]

		if len(ws.StreamBindings) > 0 {
			channels = append(channels, channel{ws.Path, ws.StreamBindings})

			continue
		}

		if ws.SendSchema == nil || ws.ReceiveSchema == nil {
			continue
		}

		send, receive := duplexMessageNames(ws.Metadata)
		duplexes = append(duplexes, client.TableStream{Kind: "duplex", Channel: ws.Path, Send: send, Receive: receive})
	}

	for i := range spec.SSEs {
		if b := spec.SSEs[i].StreamBindings; len(b) > 0 {
			channels = append(channels, channel{spec.SSEs[i].Path, b})
		}
	}

	for i := range spec.WebTransports {
		if b := spec.WebTransports[i].StreamBindings; len(b) > 0 {
			channels = append(channels, channel{spec.WebTransports[i].Path, b})
		}
	}

	sort.SliceStable(channels, func(i, j int) bool { return channels[i].path < channels[j].path })
	sort.SliceStable(duplexes, func(i, j int) bool { return duplexes[i].Channel < duplexes[j].Channel })

	var rows []client.TableStream

	for _, ch := range channels {
		for _, b := range ch.bindings {
			row := client.TableStream{
				Kind: "entity", Channel: ch.path, Message: b.Message, Entity: b.EntityType,
				Intent: string(b.Intent), Invalidates: b.Invalidates,
			}

			if spec.Schemas[b.EntityType] != nil {
				row.Decode = b.EntityType
			}

			rows = append(rows, row)
		}
	}

	return append(rows, duplexes...)
}

// stringExpr renders a Dart expression that turns expr, of type t, into the
// string a URL or header carries.
func stringExpr(t dartType, expr string) string {
	switch t.name {
	case "String":
		return expr
	case "Int64":
		return expr + ".value"
	case "DateTime":
		return expr + ".toIso8601String()"
	}

	return "'$" + expr + "'"
}

// fromClientExpr renders the FromClient function for a type: a constructor
// tear-off for a model, a lambda otherwise.
func fromClientExpr(t dartType) string {
	if decoded := t.decode("c", 0); decoded == t.name+".fromClient(c)" {
		return t.name + ".fromClient"
	}

	return "(c) => " + t.decode("c", 0)
}

func copySet(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	maps.Copy(out, in)

	return out
}
