# TypeScript Client Generator — Phase 2: Type-Generation Depth

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Emit the type information the IR already carries but the generator throws away, and produce the per-schema codec table that Phase 3's naming codec consumes.

**Architecture:** All work is in `internal/client/generators/typescript/`. Every task is guarded by the `tsc --noEmit` gate built in Phase 1, which now covers 8 fixtures and fails on any generated file that does not compile under `strict`.

**Tech Stack:** Go 1.26, TypeScript 5.8, `stretchr/testify`.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-24-ts-client-generator-design.md`.
- Continue on branch `fix/ts-client-generator-phase1`. Do not create a new branch. PR #33 is open against `main` and will pick up these commits.
- **Strict TDD.** No implementation code before a failing test exists for it. Run the test, confirm it fails for the expected reason, then implement.
- The 8 gate fixtures must report **zero** tsc errors after every task. `TestGeneratedClientsTypeCheck` is the backstop — never weaken it.
- Generation must stay deterministic. Any new map iteration that reaches output must go through `sortedKeys`.
- Auth-enabled generated output must not change except where a task explicitly changes types. When in doubt, diff the emitted tree against the previous commit.
- Conventional-commit prefixes (CI enforces them). No `Co-Authored-By` trailers.
- Commit after every task.

## Scoping Decision: `readOnly`/`writeOnly` is deferred

The spec lists a `readOnly`/`writeOnly` request/response type split under Phase 2. It is
deliberately **not** in this plan.

Splitting `User` into `UserRequest`/`UserResponse` doubles every affected schema, changes every
return type and body parameter name, and — critically — would require **two codecs per schema**
in Task 9, before the codec has been proven against a single one. That is a large blast radius
stacked on an unproven mechanism.

It moves to Phase 2b, after Phase 3 lands and the codec is exercised. Record this in the ledger so
it is not silently lost.

## Current State

Verified before writing this plan. In the TypeScript generator these IR fields have **zero**
readers: `Schema.Discriminator`, `Schema.AdditionalProperties`, `Schema.ReadOnly`,
`Schema.WriteOnly`, `Schema.Format`, and per-property `Description`/`Deprecated`. Only
endpoint-level `Deprecated` is consumed (`rest.go:239`).

`generator.go:453,472,489,508,524,542` emit hardcoded `Message`, `Member`, `Room`, `RoomOptions`,
`HistoryQuery`, `UserPresence` interfaces that collide with identically-named user schemas.

---

### Task 1: Schema-name collisions

Everything downstream keys off schema names, so stabilise them first. Two hazards: a user schema
colliding with a hardcoded streaming interface, and a schema name that is not a valid TypeScript
identifier.

**Files:**
- Modify: `internal/client/generators/typescript/generator.go` (`generateTypes`, `generateStreamingTypes`)
- Test: `internal/client/generators/typescript/gate_test.go`, `fixtures_test.go`

**Interfaces:**
- Produces: `func reservedStreamingTypeNames() []string` returning the six hardcoded names, so later tasks and tests share one list rather than duplicating string literals.

- [ ] **Step 1: Write the failing test**

```go
func TestSchemaNameCollidesWithStreamingType(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Message"] = &client.Schema{
		Type:       "object",
		Properties: map[string]*client.Schema{"body": {Type: "string"}},
	}

	cfg := baseConfig() // streaming on by default

	_, err := NewGenerator().Generate(context.Background(), spec, cfg)

	require.Error(t, err, "a user schema named Message collides with the generated streaming type")
	assert.Contains(t, err.Error(), "Message")
	assert.Contains(t, err.Error(), "collides")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestSchemaNameCollidesWithStreamingType -v`
Expected: FAIL — `Generate` returns nil error and emits two `export interface Message`, which is a duplicate-identifier error in TypeScript.

- [ ] **Step 3: Write minimal implementation**

In `generator.go`:

```go
// reservedStreamingTypeNames are interface names generateStreamingTypes emits
// verbatim. A user schema with one of these names would produce a duplicate
// identifier, so generation fails rather than emitting code that cannot compile.
func reservedStreamingTypeNames() []string {
	return []string{"Message", "Member", "Room", "RoomOptions", "HistoryQuery", "UserPresence"}
}

// checkSchemaNameCollisions reports schema names that clash with the hardcoded
// streaming interfaces. Only meaningful when streaming types are emitted.
func checkSchemaNameCollisions(spec *client.APISpec, config client.GeneratorConfig) error {
	if !config.HasAnyStreamingFeature() {
		return nil
	}

	reserved := make(map[string]bool, 6)
	for _, n := range reservedStreamingTypeNames() {
		reserved[n] = true
	}

	for _, name := range sortedKeys(spec.Schemas) {
		if reserved[name] {
			return fmt.Errorf(
				"schema %q collides with a generated streaming type; rename the schema or disable streaming features",
				name)
		}
	}

	return nil
}
```

Call it at the top of `Generate`, after the spec/config type assertions:

```go
	if err := checkSchemaNameCollisions(spec, config); err != nil {
		return nil, err
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -count=1`
Expected: PASS. The 8 gate fixtures are unaffected — none uses a reserved name.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/
git commit -m "fix(client/typescript): fail generation on schema names reserved by streaming types"
```

---

### Task 2: Property JSDoc and `@deprecated`

`Schema.Description` and `Schema.Deprecated` are carried per-property and dropped. Emitting them
is what makes the generated types usable in an editor.

**Files:**
- Modify: `internal/client/generators/typescript/generator.go` (`schemaToTypeScript`)
- Test: `internal/client/generators/typescript/gate_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPropertyJSDocIsEmitted(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Doc"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"kept":    {Type: "string", Description: "The name of the thing."},
			"old":     {Type: "string", Description: "Legacy field.", Deprecated: true},
			"plain":   {Type: "string"},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, "/** The name of the thing. */\n  kept?: string;")
	assert.Contains(t, types, "@deprecated")
	// A property with no description gets no comment at all — no empty /** */.
	assert.NotContains(t, types, "/**  */")
}
```

Note: `client.Schema` has no `Deprecated` field today. Add it to `internal/client/ir.go` as part of this task:

```go
	Deprecated  bool
```
placed next to `ReadOnly`/`WriteOnly`. The introspector does not populate it yet; that is fine — the generator must handle it when a spec parser sets it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestPropertyJSDocIsEmitted -v`
Expected: FAIL to compile first (`unknown field Deprecated`), then FAIL on the assertions once the IR field is added.

- [ ] **Step 3: Write minimal implementation**

In `schemaToTypeScript`'s object branch, before writing each property:

```go
		for _, propName := range sortedKeys(schema.Properties) {
			prop := schema.Properties[propName]

			buf.WriteString(propertyJSDoc(prop, "  "))

			required := contains(schema.Required, propName)
			// ... unchanged
		}
```

and add:

```go
// propertyJSDoc renders a property's description and deprecation as a JSDoc
// block, or the empty string when there is nothing to say. An empty comment is
// worse than no comment, so both fields absent yields no output.
func propertyJSDoc(schema *client.Schema, indent string) string {
	if schema == nil || (schema.Description == "" && !schema.Deprecated) {
		return ""
	}

	// Single-line form when there is only a description and it has no newline.
	if schema.Description != "" && !schema.Deprecated && !strings.Contains(schema.Description, "\n") {
		return fmt.Sprintf("%s/** %s */\n", indent, schema.Description)
	}

	var buf strings.Builder

	fmt.Fprintf(&buf, "%s/**\n", indent)

	for _, line := range strings.Split(schema.Description, "\n") {
		if line == "" {
			continue
		}

		fmt.Fprintf(&buf, "%s * %s\n", indent, line)
	}

	if schema.Deprecated {
		fmt.Fprintf(&buf, "%s * @deprecated\n", indent)
	}

	fmt.Fprintf(&buf, "%s */\n", indent)

	return buf.String()
}
```

A description containing `*/` would terminate the comment early. Guard it by replacing `*/` with `*\/` before emission.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -count=1`
Expected: PASS, 8 fixtures still clean.

- [ ] **Step 5: Commit**

```bash
git add internal/client/ir.go internal/client/generators/typescript/
git commit -m "feat(client/typescript): emit property descriptions and deprecations as JSDoc"
```

---

### Task 3: Format-driven types

`Schema.Format` is ignored, so a binary upload is typed `string` and a `date-time` is
indistinguishable from free text.

**Files:**
- Modify: `internal/client/generators/typescript/generator.go` (`schemaToTSType`), `rest.go` (`schemaToTSType`)
- Test: `internal/client/generators/typescript/gate_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestFormatDrivenTypes(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Formats"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"blob":    {Type: "string", Format: "binary"},
			"when":    {Type: "string", Format: "date-time"},
			"big":     {Type: "integer", Format: "int64"},
			"ordinary": {Type: "string"},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, "blob?: Blob;")
	assert.Contains(t, types, "when?: string;")     // ISO-8601 string, not Date
	assert.Contains(t, types, "big?: string;")      // int64 exceeds Number.MAX_SAFE_INTEGER
	assert.Contains(t, types, "ordinary?: string;")
}
```

The two judgement calls, made explicitly so the implementer does not have to guess:
- `date-time` stays `string`. Emitting `Date` would be a lie — `JSON.parse` produces a string, and nothing in the generated runtime revives it.
- `int64`/`uint64` become `string`, because values beyond `Number.MAX_SAFE_INTEGER` lose precision as JS numbers. This matches what most generators do for 64-bit integers.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestFormatDrivenTypes -v`
Expected: FAIL — `blob` is `string`, `big` is `number`.

- [ ] **Step 3: Write minimal implementation**

Add to `generator.go`, and call it from both `schemaToTSType` implementations before the `switch schema.Type`:

```go
// formatTSType maps an OpenAPI format to a TypeScript type, returning "" when
// the format carries no type information and the base type should be used.
func formatTSType(schema *client.Schema) string {
	if schema == nil {
		return ""
	}

	switch schema.Format {
	case "binary":
		return "Blob"
	case "int64", "uint64":
		// Beyond Number.MAX_SAFE_INTEGER, so a JS number would silently lose
		// precision. Carried as a decimal string.
		return "string"
	}

	return ""
}
```

Apply nullability consistently: if `formatTSType` returns a type and `schema.Nullable` is set, append `" | null"` exactly as the existing branches do.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/
git commit -m "feat(client/typescript): map binary and 64-bit integer formats to precise types"
```

---

### Task 4: Numeric and mixed enums

Only string enums are handled; a numeric enum falls through to bare `number`.

**Files:**
- Modify: `internal/client/generators/typescript/generator.go` (`schemaToTSType`), `rest.go` (`schemaToTSType`)
- Test: `internal/client/generators/typescript/gate_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestEnumsOfEveryScalarType(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Enums"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"status": {Type: "string", Enum: []any{"active", "off"}},
			"code":   {Type: "integer", Enum: []any{1, 2, 3}},
			"flag":   {Type: "boolean", Enum: []any{true}},
			"quoted": {Type: "string", Enum: []any{"it's"}},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, `status?: 'active' | 'off';`)
	assert.Contains(t, types, `code?: 1 | 2 | 3;`)
	assert.Contains(t, types, `flag?: true;`)
	assert.Contains(t, types, `quoted?: 'it\'s';`)
}
```

The `quoted` case matters: the current code interpolates enum values with `'%v'` and would emit
`'it's'`, breaking the file — the same class of bug Phase 1 fixed in `tsPropertyKey`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestEnumsOfEveryScalarType -v`
Expected: FAIL — `code` is `number`, `flag` is `boolean`, and `quoted` emits a broken literal.

- [ ] **Step 3: Write minimal implementation**

```go
// enumTSType renders an enum as a union of literal types, or "" when the schema
// is not an enum. String values are escaped via json.Marshal for the same reason
// tsPropertyKey does: a value containing a quote would otherwise break the file.
func enumTSType(schema *client.Schema) string {
	if schema == nil || len(schema.Enum) == 0 {
		return ""
	}

	parts := make([]string, 0, len(schema.Enum))

	for _, v := range schema.Enum {
		switch tv := v.(type) {
		case string:
			b, _ := json.Marshal(tv)
			parts = append(parts, string(b))
		case bool:
			parts = append(parts, fmt.Sprintf("%t", tv))
		case nil:
			parts = append(parts, "null")
		default:
			parts = append(parts, fmt.Sprintf("%v", tv))
		}
	}

	return strings.Join(parts, " | ")
}
```

Call it early in both `schemaToTSType` implementations, before the type switch. Note this emits
double-quoted string literals (`"active"`) rather than the single-quoted form in the test above —
**pick one and make the test match the implementation**; double quotes via `json.Marshal` are
consistent with `tsPropertyKey` and require no hand-rolled escaping, so prefer them and update the
test's expected strings accordingly.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -count=1`
Expected: PASS. Check whether any existing test asserts a single-quoted enum union and update it.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/
git commit -m "feat(client/typescript): support numeric and boolean enums with escaped literals"
```

---

### Task 5: `additionalProperties`

`Schema.AdditionalProperties` is `any` in the IR (either a `bool` or a `*Schema`) and is entirely
ignored, so an open-ended map is emitted as a closed interface.

**Files:**
- Modify: `internal/client/generators/typescript/generator.go` (`schemaToTypeScript`, `schemaToTSType`)
- Test: `internal/client/generators/typescript/gate_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestAdditionalProperties(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["OpenTyped"] = &client.Schema{
		Type:                 "object",
		AdditionalProperties: &client.Schema{Type: "string"},
	}
	spec.Schemas["OpenAny"] = &client.Schema{
		Type:                 "object",
		AdditionalProperties: true,
	}
	spec.Schemas["Mixed"] = &client.Schema{
		Type:                 "object",
		Required:             []string{"id"},
		Properties:           map[string]*client.Schema{"id": {Type: "string"}},
		AdditionalProperties: &client.Schema{Type: "number"},
	}
	spec.Schemas["Closed"] = &client.Schema{
		Type:                 "object",
		Properties:           map[string]*clientient.Schema{"id": {Type: "string"}},
		AdditionalProperties: false,
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, "export type OpenTyped = Record<string, string>;")
	assert.Contains(t, types, "export type OpenAny = Record<string, any>;")
	assert.Contains(t, types, "[key: string]: number;")   // Mixed keeps id AND an index signature
	assert.Contains(t, types, "id: string;")
	assert.NotContains(t, types, "export type Closed = Record")
}
```

There is a typo in the fixture above (`client ient.Schema`) — fix it when transcribing; it is
`*client.Schema`.

**A TypeScript constraint the implementer must respect:** an index signature must be compatible
with every declared property. `Mixed` declares `id: string` alongside `[key: string]: number`,
which TypeScript rejects (`TS2411`). So when a schema has BOTH properties and a typed
`additionalProperties`, the index signature's type must be widened to a union of the value type and
all declared property types, or the declared properties must be intersected in. The simplest
correct emission is:

```ts
export type Mixed = { id: string } & Record<string, number>;
```

Use that shape — an intersection — rather than an interface with an index signature. Adjust the
test's expected strings to match, and verify with the gate that it type-checks.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestAdditionalProperties -v`
Expected: FAIL — all four schemas emit as empty or closed interfaces.

- [ ] **Step 3: Write minimal implementation**

Add a helper that normalises the `any`-typed field:

```go
// additionalPropsSchema interprets Schema.AdditionalProperties, which the IR
// types as `any` because JSON Schema allows either a bool or a schema.
// Returns (valueSchema, allowed). A nil valueSchema with allowed=true means
// "any value".
func additionalPropsSchema(v any) (*client.Schema, bool) {
	switch t := v.(type) {
	case nil:
		return nil, false
	case bool:
		return nil, t
	case *client.Schema:
		return t, true
	}

	return nil, false
}
```

Then branch in `schemaToTypeScript`'s object case: no declared properties plus additional allowed →
`export type X = Record<string, V>;`; declared properties plus additional allowed → the
intersection form; otherwise the existing interface.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -count=1`
Expected: PASS, gate clean.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/
git commit -m "feat(client/typescript): emit additionalProperties as index types"
```

---

### Task 6: Discriminated unions

`Schema.Discriminator` has no reader. A `oneOf` with a discriminator is the one case where
TypeScript can narrow automatically, and it is also what the Phase 3 codec needs to pick a member.

**Files:**
- Modify: `internal/client/generators/typescript/generator.go` (`schemaToTypeScript`, `schemaToTSType`)
- Test: `internal/client/generators/typescript/gate_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestDiscriminatedUnion(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Cat"] = &client.Schema{
		Type:     "object",
		Required: []string{"kind", "meows"},
		Properties: map[string]*client.Schema{
			"kind":  {Type: "string", Enum: []any{"cat"}},
			"meows": {Type: "boolean"},
		},
	}
	spec.Schemas["Dog"] = &client.Schema{
		Type:     "object",
		Required: []string{"kind", "barks"},
		Properties: map[string]*client.Schema{
			"kind":  {Type: "string", Enum: []any{"dog"}},
			"barks": {Type: "boolean"},
		},
	}
	spec.Schemas["Pet"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/Cat"},
			{Ref: "#/components/schemas/Dog"},
		},
		Discriminator: &client.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]string{"cat": "#/components/schemas/Cat", "dog": "#/components/schemas/Dog"},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, "export type Pet = Cat | Dog;")
}
```

Narrowing then works for free, because each member declares `kind` as a literal type — which is
why Task 4 (enum literals) must land before this task.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestDiscriminatedUnion -v`
Expected: FAIL — `Pet` currently falls to the `default` branch of `schemaToTypeScript` and emits `export type Pet = any;`.

- [ ] **Step 3: Write minimal implementation**

In `schemaToTypeScript`, handle `OneOf`/`AnyOf`/`AllOf` before the `switch schema.Type`, since a
polymorphic schema has no `Type`:

```go
	if len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 || len(schema.AllOf) > 0 {
		return fmt.Sprintf("export type %s = %s;\n", name, g.schemaToTSType(schema, spec))
	}
```

`schemaToTSType` already joins `OneOf`/`AnyOf` with `|` and `AllOf` with `&`, so this needs no new
union logic. Sort the `Discriminator.Mapping` iteration if you read it — that map reaches output in
Task 9.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/
git commit -m "feat(client/typescript): emit polymorphic schemas as unions"
```

---

### Task 7: Response types beyond 200/201 JSON

`generateReturnType` inspects only 200 and 201 with `application/json`, so every other success
shape degrades to `any`.

**Files:**
- Modify: `internal/client/generators/typescript/rest.go` (`generateReturnType`)
- Test: `internal/client/generators/typescript/rest_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestReturnTypeCoversAll2xxAndNonJSON(t *testing.T) {
	mk := func(responses map[int]*client.Response) string {
		return NewRESTGenerator().Generate(&client.APISpec{
			Info: client.APIInfo{Title: "T", Version: "1"},
			Endpoints: []client.Endpoint{{
				Method: "GET", Path: "/x", OperationID: "x.get", Responses: responses,
			}},
			Schemas: map[string]*client.Schema{"A": {Type: "object"}, "B": {Type: "object"}},
		}, client.DefaultConfig())
	}

	// 202 with a JSON body must not degrade to any.
	code := mk(map[int]*client.Response{202: {Content: map[string]*client.MediaType{
		"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/A"}}}}})
	assert.Contains(t, code, "Promise<types.A>")

	// Two success codes with different bodies produce a union.
	code = mk(map[int]*client.Response{
		200: {Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/A"}}}},
		201: {Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/B"}}}},
	})
	assert.Contains(t, code, "Promise<types.A | types.B>")

	// A non-JSON body is a Blob, not any.
	code = mk(map[int]*client.Response{200: {Content: map[string]*client.MediaType{
		"application/octet-stream": {Schema: &client.Schema{Type: "string", Format: "binary"}}}}})
	assert.Contains(t, code, "Promise<Blob>")

	// text/plain is a string.
	code = mk(map[int]*client.Response{200: {Content: map[string]*client.MediaType{
		"text/plain": {Schema: &client.Schema{Type: "string"}}}}})
	assert.Contains(t, code, "Promise<string>")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestReturnTypeCoversAll2xxAndNonJSON -v`
Expected: FAIL — 202 and both non-JSON cases return `any`, and there is no union.

- [ ] **Step 3: Write minimal implementation**

Rewrite `generateReturnType` to collect every 2xx response in ascending status order (via a sorted
key slice — `Responses` is a map and must not be ranged directly), map each to a type, dedupe while
preserving order, and join with `|`. Content-type precedence: `application/json` first, then
`text/*` → `string`, then anything else → `Blob`. A 2xx with no content contributes `void`; if the
only outcome is `void`, the return type is `void`.

**`fetch.ts` must agree with the type you emit.** `executeRequest` currently parses JSON when the
`content-type` says so, returns `{} as T` for 204, and otherwise `response.text()`. If a method's
declared return type is `Blob`, the runtime must call `response.blob()`. Extend the parse step in
`fetch_client.go` to branch on content-type — JSON, then `text/*`, then `blob()` — otherwise the
declared type is a lie. Assert this in the same test by checking the generated `fetch.ts` contains a
`blob()` call.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -count=1`
Expected: PASS, gate clean.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/
git commit -m "feat(client/typescript): type all 2xx responses including non-JSON bodies"
```

---

### Task 8: Request bodies beyond JSON

`hasBodyParam` accepts only `application/json`, so a multipart upload silently generates a method
with no body parameter — the request is sent empty.

**Files:**
- Modify: `internal/client/generators/typescript/rest.go` (`hasBodyParam`, `generateParameters`, `generateMethodBody`), `fetch_client.go`
- Test: `internal/client/generators/typescript/rest_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestNonJSONRequestBodies(t *testing.T) {
	mk := func(contentType string, schema *client.Schema) string {
		return NewRESTGenerator().Generate(&client.APISpec{
			Info: client.APIInfo{Title: "T", Version: "1"},
			Endpoints: []client.Endpoint{{
				Method: "POST", Path: "/up", OperationID: "up.post",
				RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
					contentType: {Schema: schema}}},
				Responses: map[int]*client.Response{204: {Description: "ok"}},
			}},
			Schemas: map[string]*client.Schema{},
		}, client.DefaultConfig())
	}

	code := mk("multipart/form-data", &client.Schema{Type: "object"})
	assert.Contains(t, code, "body: FormData")

	code = mk("application/octet-stream", &client.Schema{Type: "string", Format: "binary"})
	assert.Contains(t, code, "body: Blob")

	code = mk("text/plain", &client.Schema{Type: "string"})
	assert.Contains(t, code, "body: string")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestNonJSONRequestBodies -v`
Expected: FAIL — no `body` parameter is generated at all for any of the three.

- [ ] **Step 3: Write minimal implementation**

Generalise `hasBodyParam` to pick a content type by the same precedence as Task 7 and return both
the media type and its key. Map the key to a TypeScript parameter type: `application/json` → the
schema type; `multipart/form-data` → `FormData`; `text/*` → `string`; anything else → `Blob`.

`fetch.ts` must stop unconditionally `JSON.stringify`-ing the body and must not force
`Content-Type: application/json`. In `executeRequest`, serialise based on the body's runtime type:
`FormData` and `Blob` pass through untouched with **no** explicit `Content-Type` (the browser sets
the multipart boundary itself — setting it manually breaks the request), `string` passes through,
everything else is JSON-stringified with the JSON content type. Assert the generated `fetch.ts`
contains a `FormData` check.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -count=1`
Expected: PASS, gate clean.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/
git commit -m "feat(client/typescript): support multipart, binary and text request bodies"
```

---

### Task 9: Emit the codec table

The deliverable Phase 3 depends on. Types keep their wire-cased property names for now — this task
emits the table and the runtime; Phase 3 flips the type names to camelCase and wires encode/decode
into the request path.

**Files:**
- Create: `internal/client/generators/typescript/codecs.go`, `codecs_test.go`
- Modify: `internal/client/generators/typescript/generator.go` (emit `src/codecs.ts`, export from `src/index.ts`)
- Test: `internal/client/generators/typescript/gate_test.go`

**Interfaces:**
- Produces: `func NewCodecGenerator() *CodecGenerator` with `Generate(spec *client.APISpec, config client.GeneratorConfig) string`, mirroring the other generators' shape.

- [ ] **Step 1: Write the failing test**

```go
func TestCodecTableShape(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Nested"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"user":  {Ref: "#/components/schemas/User"},
			"items": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/User"}},
			"tags":  {Type: "object", AdditionalProperties: &client.Schema{Type: "string"}},
		},
	}

	code := NewCodecGenerator().Generate(spec, baseConfig())

	assert.Contains(t, code, "export const CODECS:")
	assert.Contains(t, code, `"User":`)
	assert.Contains(t, code, `"kind": "object"`)
	assert.Contains(t, code, `"kind": "array"`)
	assert.Contains(t, code, `"kind": "record"`)
	assert.Contains(t, code, "export function decode")
	assert.Contains(t, code, "export function encode")
}

func TestCodecTableIsDeterministic(t *testing.T) {
	spec := baseSpec()
	first := NewCodecGenerator().Generate(spec, baseConfig())

	for i := 0; i < 12; i++ {
		if got := NewCodecGenerator().Generate(spec, baseConfig()); got != first {
			t.Fatalf("run %d differs", i)
		}
	}
}

func TestCodecsEmittedAndExported(t *testing.T) {
	out, err := NewGenerator().Generate(context.Background(), baseSpec(), baseConfig())
	require.NoError(t, err)

	assert.Contains(t, out.Files, "src/codecs.ts")
	assert.Contains(t, out.Files["src/index.ts"], "export * from './codecs';")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestCodec -v`
Expected: FAIL to compile — `undefined: NewCodecGenerator`.

- [ ] **Step 3: Write minimal implementation**

Emit the `Codec` type and `CODECS` table exactly as the spec defines, plus `encode`/`decode`. Build
the table by walking `spec.Schemas` in `sortedKeys` order; for each schema emit one entry keyed by
schema name:

- object → `{kind:"object", fields:{<wire>:{ts:<wire>, codec?:<ref>}}}` — for now `ts` equals the
  wire name, because renaming is Phase 3. Emitting the field map now is the point.
- array → `{kind:"array", items:<ref>}`
- `additionalProperties` → `{kind:"record", values:<ref>}`
- `oneOf`/`anyOf` with a discriminator → `{kind:"union", discriminator:{wire, map}, members:[...]}`
- anything else → `{kind:"passthrough"}`

Nested inline (non-`$ref`) schemas need synthetic ids; derive them deterministically from the parent
schema name and property path, e.g. `Nested.items`, so the table stays stable across runs.

The runtime rules from the spec are non-negotiable and must be covered by the emitted code:
unknown keys pass through verbatim; `record` renames values but never keys; a union without a
discriminator falls back to passthrough.

Under `config.FieldNaming == preserve` — a field that does not exist until Phase 3 — the file would
not be emitted at all. Until then, always emit it.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -count=1`
Expected: PASS. The gate now type-checks `src/codecs.ts` for all 8 fixtures.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/
git commit -m "feat(client/typescript): generate the per-schema codec table"
```

---

## Self-Review

**Spec coverage.** Phase 2 items from the spec map as: schema-name sanitizing/dedup → Task 1;
JSDoc and `@deprecated` → Task 2; format mapping → Task 3; non-string enums → Task 4;
`additionalProperties` → Task 5; discriminated unions → Task 6; 2xx return unions and non-JSON
bodies → Task 7; non-JSON request bodies → Task 8 (an addition — the spec lists it under
type-generation depth); codec table → Task 9. **`readOnly`/`writeOnly` is deliberately deferred to
Phase 2b**, with rationale recorded above.

**Known transcription hazards in this plan.** Task 5's test fixture contains a deliberate typo
(`client ient.Schema`) flagged inline. Task 4's test asserts single-quoted enum literals while the
recommended implementation emits double-quoted — the task says explicitly to make the test match
`json.Marshal` output. Task 5's `Mixed` case asserts an index signature but the guidance directs an
intersection instead; follow the guidance and update the assertion.

**Type consistency.** `sortedKeys` (Phase 1, `generator.go`) is used by Tasks 1, 6, 7, 9.
`tsPropertyKey` (Phase 1, `rest.go`) is used by Tasks 2 and 5. `formatTSType` (Task 3) is consumed
by Tasks 7 and 8. `enumTSType` (Task 4) must land before Task 6, which relies on literal types for
narrowing. `additionalPropsSchema` (Task 5) is consumed by Task 9.

**Cross-task risk.** Tasks 7 and 8 both modify `fetch_client.go`'s request/response handling. Task 7
changes response parsing, Task 8 changes body serialisation. They touch adjacent code in
`executeRequest` — whichever runs second must re-read the file rather than working from the plan's
assumed contents.
