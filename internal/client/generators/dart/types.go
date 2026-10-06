package dart

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// dartType is a resolved Dart type and how a value of it moves between the
// client-shaped JSON the store holds and the typed value a model exposes.
type dartType struct {
	// name is the non-nullable spelling, such as "List<LineItem>". The one
	// exception is "Object?", which is already nullable; dynamic marks it.
	name    string
	dynamic bool
	// cast marks a type whose decode is a bare `as` cast, so its nullable
	// form is a cast to the nullable type rather than a decodeNullable call.
	cast bool
	// deep marks a type whose equality needs deepEquals and deepHash.
	deep bool
	// identity marks a type whose client-shaped form is the value itself.
	identity bool
	// typedData marks a type that names Uint8List.
	typedData bool
	// imports are the components whose model files the type needs.
	imports []string

	decodeFn func(expr string, depth int) string
	encodeFn func(expr string, depth int) string

	// paramFn renders a path, query or header value when it differs from the
	// body encoding. Nil means the two agree.
	paramFn func(expr string, depth int) string

	// promotedDecodeFn decodes expr when a type test has already promoted it
	// to the JSON type the decode would cast to, so repeating the cast would
	// be an unnecessary_cast. Nil means the type has no such shortcut.
	promotedDecodeFn func(expr string) string
}

func (t dartType) decode(expr string, depth int) string { return t.decodeFn(expr, depth) }

func (t dartType) encode(expr string, depth int) string {
	if t.identity {
		return expr
	}

	return t.encodeFn(expr, depth)
}

// paramEncode renders expr as a path, query or header value: the body
// encoding, unless the type keeps a representation of its own there.
func (t dartType) paramEncode(expr string, depth int) string {
	if t.paramFn != nil {
		return t.paramFn(expr, depth)
	}

	return t.encode(expr, depth)
}

// paramEncodeNullable is paramEncode for a value that may be null.
func (t dartType) paramEncodeNullable(expr string, depth int) string {
	if t.paramFn == nil {
		return t.encodeNullable(expr, depth)
	}

	v := fmt.Sprintf("v%d", depth)

	return fmt.Sprintf("encodeNullable(%s, (%s) => %s)", expr, v, t.paramFn(v, depth+1))
}

// nullableName is the type a nullable field of this type declares.
func (t dartType) nullableName() string {
	if t.dynamic {
		return t.name
	}

	return t.name + "?"
}

// decodeNullable decodes expr, which may be null.
func (t dartType) decodeNullable(expr string, depth int) string {
	switch {
	case t.dynamic:
		return expr
	case t.cast:
		return expr + " as " + t.name + "?"
	}

	v := fmt.Sprintf("v%d", depth)

	return fmt.Sprintf("decodeNullable(%s, (%s) => %s)", expr, v, t.decode(v, depth+1))
}

// encodeNullable encodes expr, which may be null, writing null for null.
func (t dartType) encodeNullable(expr string, depth int) string {
	if t.identity {
		return expr
	}

	v := fmt.Sprintf("v%d", depth)

	return fmt.Sprintf("encodeNullable(%s, (%s) => %s)", expr, v, t.encode(v, depth+1))
}

// withImports returns t carrying extra component imports.
func (t dartType) withImports(names ...string) dartType {
	t.imports = append(append([]string(nil), t.imports...), names...)

	return t
}

func castType(name string) dartType {
	return dartType{
		name: name, cast: true, identity: true,
		decodeFn: func(expr string, _ int) string { return expr + " as " + name },
	}
}

func helperType(name, decoder string, encode func(string, int) string) dartType {
	return dartType{
		name:     name,
		identity: encode == nil,
		decodeFn: func(expr string, _ int) string { return decoder + "(" + expr + ")" },
		encodeFn: encode,
	}
}

func dynamicType() dartType {
	return dartType{
		name: "Object?", dynamic: true, identity: true, deep: true,
		decodeFn: func(expr string, _ int) string { return expr },
	}
}

func jsonObjectType() dartType {
	t := helperType("Map<String, Object?>", "decodeObject", nil)
	t.deep = true

	return t
}

func modelType(name string, imports ...string) dartType {
	return dartType{
		name:     name,
		imports:  imports,
		decodeFn: func(expr string, _ int) string { return name + ".fromClient(" + expr + ")" },
		encodeFn: func(expr string, _ int) string { return expr + ".toClient()" },
	}
}

// cachedModel decodes a model through decodeCached instead of calling its
// constructor directly.
func cachedModel(t dartType) dartType {
	name := t.name
	t.decodeFn = func(expr string, _ int) string { return "decodeCached(" + name + ".fromClient, " + expr + ")" }

	return t
}

func listType(item dartType, itemNullable bool) dartType {
	elem := item.name
	if itemNullable {
		elem = item.nullableName()
	}

	t := dartType{
		name:      "List<" + elem + ">",
		deep:      true,
		identity:  item.identity,
		typedData: item.typedData,
		imports:   item.imports,
	}

	t.decodeFn = func(expr string, depth int) string {
		v := fmt.Sprintf("v%d", depth)

		inner := item.decode(v, depth+1)
		if itemNullable {
			inner = item.decodeNullable(v, depth+1)
		}

		return fmt.Sprintf("decodeList(%s, (%s) => %s)", expr, v, inner)
	}

	t.encodeFn = func(expr string, depth int) string {
		e := fmt.Sprintf("e%d", depth)

		inner := item.encode(e, depth+1)
		if itemNullable {
			inner = item.encodeNullable(e, depth+1)
		}

		return fmt.Sprintf("[for (final %s in %s) %s]", e, expr, inner)
	}

	// A list parameter keeps each item in the item's parameter form, so a
	// List<Int64> in a query stays decimal strings in the URL and the key.
	if item.paramFn != nil {
		t.paramFn = func(expr string, depth int) string {
			e := fmt.Sprintf("e%d", depth)

			inner := item.paramEncode(e, depth+1)
			if itemNullable {
				inner = item.paramEncodeNullable(e, depth+1)
			}

			return fmt.Sprintf("[for (final %s in %s) %s]", e, expr, inner)
		}
	}

	return t
}

func mapType(value dartType, valueNullable bool) dartType {
	elem := value.name
	if valueNullable {
		elem = value.nullableName()
	}

	t := dartType{
		name:      "Map<String, " + elem + ">",
		deep:      true,
		identity:  value.identity,
		typedData: value.typedData,
		imports:   value.imports,
	}

	t.decodeFn = func(expr string, depth int) string {
		v := fmt.Sprintf("v%d", depth)

		inner := value.decode(v, depth+1)
		if valueNullable {
			inner = value.decodeNullable(v, depth+1)
		}

		return fmt.Sprintf("decodeMap(%s, (%s) => %s)", expr, v, inner)
	}

	t.encodeFn = func(expr string, depth int) string {
		e := fmt.Sprintf("e%d", depth)

		inner := value.encode(e+".value", depth+1)
		if valueNullable {
			inner = value.encodeNullable(e+".value", depth+1)
		}

		return fmt.Sprintf("{for (final %s in %s.entries) %s.key: %s}", e, expr, e, inner)
	}

	// As listType: a map parameter keeps each value in its parameter form.
	if value.paramFn != nil {
		t.paramFn = func(expr string, depth int) string {
			e := fmt.Sprintf("e%d", depth)

			inner := value.paramEncode(e+".value", depth+1)
			if valueNullable {
				inner = value.paramEncodeNullable(e+".value", depth+1)
			}

			return fmt.Sprintf("{for (final %s in %s.entries) %s.key: %s}", e, expr, e, inner)
		}
	}

	return t
}

// enumRep is the representation type of a generated enum: String when every
// declared value is a string, Object when any is a number or a boolean.
func enumRep(s *client.Schema) string {
	for _, v := range s.Enum {
		if _, ok := v.(string); !ok && v != nil {
			return "Object"
		}
	}

	return "String"
}

// enumType is an enum's extension type over its wire value. An unknown value
// a newer server sends is still a value of the type, so it decodes by
// wrapping and encodes by unwrapping, and nothing is lost on a round trip.
func enumType(name, rep string, imports ...string) dartType {
	return dartType{
		name:     name,
		imports:  imports,
		decodeFn: func(expr string, _ int) string { return name + "(" + expr + " as " + rep + ")" },
		encodeFn: func(expr string, _ int) string { return expr + ".wire" },

		promotedDecodeFn: func(expr string) string { return name + "(" + expr + ")" },
	}
}

// primitive maps a scalar schema to its Dart type, per the type-mapping table
// in the spec. An enum resolved without an owner to hold a generated enum
// declaration falls back to its base scalar here.
func (r *registry) primitive(s *client.Schema) (dartType, bool) {
	switch s.Type {
	case "string":
		switch s.Format {
		case "date-time":
			return helperType("DateTime", "decodeDateTime", func(expr string, _ int) string {
				return expr + ".toIso8601String()"
			}), true
		case "date":
			return helperType("DateTime", "decodeDateTime", func(expr string, _ int) string {
				return "encodeDate(" + expr + ")"
			}), true
		case "binary", "byte":
			t := helperType("Uint8List", "decodeBytes", func(expr string, _ int) string {
				return "encodeBytes(" + expr + ")"
			})
			t.deep, t.typedData = true, true

			return t, true
		case "int64", "uint64":
			return r.int64Type(false), true
		}

		return castType("String"), true

	case "integer":
		if s.Format == "int64" || s.Format == "uint64" {
			return r.int64Type(true), true
		}

		return helperType("int", "decodeInt", nil), true

	case "number":
		return helperType("double", "decodeDouble", nil), true

	case "boolean":
		return castType("bool"), true
	}

	return dartType{}, false
}

// int64Type is the extension type over String by default, so a web build
// keeps every digit, or a plain int under --int64=int.
//
// The wire follows the schema, as TypeScript's does: an integer schema goes
// out as a JSON number and a string schema as a JSON string, whichever the
// Dart type. TypeScript's codecs pass every value through unconverted, so a
// value decoded from the wire leaves in the form it arrived in; for an
// Int64 that is int.parse, exact on native and as lossy as JavaScript above
// 2^53 on the web. Parameters keep the representation they always had, so a
// cache key does not change with the schema shape.
func (r *registry) int64Type(integer bool) dartType {
	if r.config.Int64 == client.Int64Int {
		if integer {
			return helperType("int", "decodeInt", nil)
		}

		t := helperType("int", "decodeIntOrString", func(expr string, _ int) string { return expr + ".toString()" })
		t.paramFn = func(expr string, _ int) string { return expr }

		return t
	}

	value := func(expr string, _ int) string { return expr + ".value" }

	if !integer {
		return helperType("Int64", "decodeInt64", value)
	}

	t := helperType("Int64", "decodeInt64", func(expr string, _ int) string { return expr + ".toInt()" })
	t.paramFn = value

	return t
}

// rctx is where a schema is being resolved: which model file owns any type
// the resolution has to declare, the codec namespace id its properties are
// named under, the Dart name a declared type should take, and the set that
// collects the model files the result needs imported.
//
// A nil owner means nothing may be declared: operation arguments and REST
// signatures resolve this way, and an inline enum, object or union there
// falls back to its base scalar, a JSON object, or Object?.
type rctx struct {
	owner   *componentModel
	nsID    string
	hint    string
	imports map[string]bool
}

func (c rctx) child(nsSuffix, hintSuffix string) rctx {
	c.nsID += nsSuffix
	c.hint += hintSuffix

	return c
}

// resolve maps a schema to its Dart type.
func (r *registry) resolve(s *client.Schema, c rctx) dartType {
	t := r.resolveType(s, c)

	for _, name := range t.imports {
		if c.imports != nil && (c.owner == nil || name != c.owner.schemaName) {
			c.imports[name] = true
		}
	}

	return t
}

func (r *registry) resolveType(s *client.Schema, c rctx) dartType {
	if s == nil {
		return dynamicType()
	}

	if s.Ref != "" {
		return r.refType(client.ComponentRefName(s.Ref))
	}

	if len(s.OneOf) > 0 || len(s.AnyOf) > 0 {
		if c.owner == nil {
			return dynamicType()
		}

		return r.declare(c, func(name string) decl { return r.buildUnion(name, s, c) })
	}

	if len(s.AllOf) > 0 {
		if c.owner == nil {
			return jsonObjectType()
		}

		return r.declare(c, func(name string) decl { return r.buildClass(name, s, c) })
	}

	if hasEnumValues(s) && c.owner != nil {
		return r.declare(c, func(name string) decl { return r.buildEnum(name, s) })
	}

	if t, ok := r.primitive(s); ok {
		return t
	}

	switch s.Type {
	case "array":
		item := r.resolveType(s.Items, c.child(".items", "Item"))

		// With hooks, each element of a list of models goes through
		// forge_client's identity memo, so a row the store kept unchanged
		// decodes to the identical model even when the list around it moved.
		if r.config.HooksEnabled() && item.decode("x", 0) == item.name+".fromClient(x)" {
			item = cachedModel(item)
		}

		return listType(item, s.Items != nil && s.Items.Nullable && !item.dynamic)

	case "object", "":
		if len(s.Properties) > 0 {
			if c.owner == nil {
				return jsonObjectType()
			}

			return r.declare(c, func(name string) decl { return r.buildClass(name, s, c) })
		}

		if values, ok := additionalPropsSchema(s.AdditionalProperties); ok {
			if values == nil {
				return mapType(dynamicType(), false)
			}

			value := r.resolveType(values, c.child("."+additionalPropertiesSegment, "Value"))

			return mapType(value, values.Nullable && !value.dynamic)
		}

		if s.Type == "object" {
			return jsonObjectType()
		}
	}

	return dynamicType()
}

// refType resolves a reference to a component. A dangling reference is
// Object?: ValidateRefs has already warned, and a model that compiles beats
// one that names a type nothing declares.
func (r *registry) refType(name string) dartType {
	m := r.models[name]
	if m == nil {
		return dynamicType()
	}

	switch m.kind {
	case kindEnum:
		return enumType(m.dartName, enumRep(m.schema), m.schemaName)
	case kindClass, kindUnion:
		return modelType(m.dartName, m.schemaName)
	}

	target := r.aliasTarget(m)
	target.name = m.dartName

	return target.withImports(m.schemaName)
}

// aliasTarget resolves the type an alias component stands for, once. A
// cycle (an alias that contains itself) resolves to Object?.
func (r *registry) aliasTarget(m *componentModel) dartType {
	if m.target != nil {
		return *m.target
	}

	if r.resolving[m.schemaName] {
		return dynamicType()
	}

	r.resolving[m.schemaName] = true
	t := r.resolve(m.schema, rctx{owner: m, nsID: m.schemaName, hint: m.dartName, imports: m.imports})
	delete(r.resolving, m.schemaName)

	m.target = &t

	return t
}

// declare claims a Dart type name for an inline schema, builds its
// declaration into the owner's file and returns the type that names it.
func (r *registry) declare(c rctx, build func(name string) decl) dartType {
	name := r.claim(c.hint)
	d := build(name)
	c.owner.decls = append(c.owner.decls, d)

	if e, ok := d.(*enumDecl); ok {
		return enumType(name, e.rep)
	}

	return modelType(name)
}

// claim reserves a unique Dart type name derived from hint.
func (r *registry) claim(hint string) string {
	return uniqueNames([]string{hint}, typeIdent, r.taken, false)[0]
}

func hasEnumValues(s *client.Schema) bool {
	for _, v := range s.Enum {
		if v != nil {
			return true
		}
	}

	return false
}

// typeImports renders the model-file imports a set of component names needs,
// relative to dir ("" for a file beside the models, "../models/" from
// elsewhere), sorted and deduplicated.
func (r *registry) typeImports(names map[string]bool, prefix string) []string {
	seen := map[string]bool{}

	var out []string

	for _, name := range sortedKeys(names) {
		m := r.models[name]
		if m == nil {
			continue
		}

		line := fmt.Sprintf("import '%s%s.dart';", prefix, m.file)
		if !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}

	return out
}

// importsFor renders the model-file imports a rendered file needs: every
// model file declaring a type the text names in code. Reading the text rather
// than threading import sets through every emitter means an import is present
// exactly when it is used, which the analyzer's unused_import check demands.
// Only code counts (see codeIdentifiers): an operation summary that happens to
// say "Get Chat Session" names no type, so it must not import one.
func (r *registry) importsFor(text, prefix string) []string {
	used := codeIdentifiers(text)

	var out []string

	for _, name := range sortedKeys(r.models) {
		m := r.models[name]

		for _, d := range m.decls {
			if anyUsed(used, declNames(d)) {
				out = append(out, fmt.Sprintf("import '%s%s.dart';", prefix, m.file))

				break
			}
		}
	}

	return out
}

func anyUsed(used map[string]bool, names []string) bool {
	for _, n := range names {
		if used[n] {
			return true
		}
	}

	return false
}

// declNames lists the type names a declaration introduces.
func declNames(d decl) []string {
	switch d := d.(type) {
	case *classDecl:
		return []string{d.name}
	case *enumDecl:
		return []string{d.name, d.known}
	case *aliasDecl:
		return []string{d.name}
	case *unionDecl:
		names := []string{d.name, d.unknown}
		for _, v := range d.variants {
			names = append(names, v.name)
		}

		return names
	}

	return nil
}

// usesTypedData reports whether any type name in names mentions Uint8List.
func usesTypedData(names ...string) bool {
	for _, n := range names {
		if strings.Contains(n, "Uint8List") {
			return true
		}
	}

	return false
}

// supportSymbols are the support.dart helpers a file outside the models may
// call. Files import them with `show`, listing only the ones they use.
var supportSymbols = []string{
	"Int64", "decodeBytes", "decodeCached", "decodeDateTime", "decodeDouble", "decodeInt", "decodeInt64",
	"decodeIntOrString",
	"decodeList", "decodeMap", "decodeNullable", "decodeObject", "deepEquals", "deepHash",
	"encodeBytes", "encodeDate", "encodeNullable", "valueEquals", "valueHash",
}

// codeIdentifiers returns every identifier the Dart text uses as code: the
// names inside comments and string literals are left out, but a `$name` or
// `${...}` interpolation inside a string is code and counts. Import tracking
// reads this rather than the raw text, because a doc comment or a string
// literal can spell a model's name without the file ever referring to it.
func codeIdentifiers(text string) map[string]bool {
	out := map[string]bool{}
	scanCode(text, 0, false, out)

	return out
}

func identStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func identPart(c byte) bool { return identStart(c) || c >= '0' && c <= '9' }

// scanCode records the identifiers in s from i. Inside a `${...}` interpolation
// (interp) it returns just past the closing brace; otherwise at the end.
func scanCode(s string, i int, interp bool, out map[string]bool) int {
	depth := 0

	for i < len(s) {
		c := s[i]

		switch {
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			i = skipBlockComment(s, i)
		case c == '\'' || c == '"':
			i = scanString(s, i, false, out)
		case identStart(c):
			j := i
			for j < len(s) && identPart(s[j]) {
				j++
			}

			if s[i:j] == "r" && j < len(s) && (s[j] == '\'' || s[j] == '"') {
				i = scanString(s, j, true, out)

				continue
			}

			out[s[i:j]] = true
			i = j
		case c >= '0' && c <= '9':
			// A number, including a hex digit run or an exponent.
			for i < len(s) && identPart(s[i]) {
				i++
			}
		case interp && c == '{':
			depth++
			i++
		case interp && c == '}':
			if depth == 0 {
				return i + 1
			}

			depth--
			i++
		default:
			i++
		}
	}

	return i
}

// skipBlockComment returns the index just past the block comment starting at
// i. Dart block comments nest.
func skipBlockComment(s string, i int) int {
	depth := 0

	for i < len(s) {
		switch {
		case strings.HasPrefix(s[i:], "/*"):
			depth++
			i += 2
		case strings.HasPrefix(s[i:], "*/"):
			depth--
			i += 2

			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}

	return i
}

// scanString consumes the string literal whose opening quote is at i,
// recording the identifiers of its interpolations, and returns the index just
// past it. A raw string has no escapes and no interpolation.
func scanString(s string, i int, raw bool, out map[string]bool) int {
	quote := s[i]
	delim := string(quote)

	if strings.HasPrefix(s[i:], strings.Repeat(delim, 3)) {
		delim = strings.Repeat(delim, 3)
	}

	i += len(delim)

	for i < len(s) {
		switch {
		case !raw && s[i] == '\\':
			i += 2
		case strings.HasPrefix(s[i:], delim):
			return i + len(delim)
		case !raw && s[i] == '$' && i+1 < len(s) && s[i+1] == '{':
			i = scanCode(s, i+2, true, out)
		case !raw && s[i] == '$' && i+1 < len(s) && identStart(s[i+1]) && s[i+1] != '$':
			j := i + 1
			for j < len(s) && identPart(s[j]) && s[j] != '$' {
				j++
			}

			out[s[i+1:j]] = true
			i = j
		default:
			i++
		}
	}

	return i
}

// usedSymbols returns, sorted, which of candidates appear as identifiers in the
// code of text.
func usedSymbols(text string, candidates []string) []string {
	seen := codeIdentifiers(text)

	var out []string

	for _, c := range candidates {
		if seen[c] {
			out = append(out, c)
		}
	}

	sort.Strings(out)

	return out
}
