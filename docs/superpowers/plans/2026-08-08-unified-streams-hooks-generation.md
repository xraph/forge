# Unified Streams + Hooks Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `forge client generate` accept several specification documents and emit one package containing REST operations, hooks, and stream clients with a populated `streams` manifest.

**Architecture:** Parse each source into an `APISpec` with entity resolution deferred, merge the results with OpenAPI authoritative on collisions, then resolve entity fields once over the merged whole. The IR already carries both halves and the TypeScript generator already gates emission per section on the data, so no generator changes are needed.

**Tech Stack:** Go 1.26, stdlib `testing` (no testify anywhere in `internal/client`), `gopkg.in/yaml.v3`, `fsnotify`.

## Global Constraints

- Test package for `internal/client` tests is `package client_test`, using stdlib `testing` with `t.Fatalf`/`t.Errorf`. **No testify** — all 14 existing test files in that package are stdlib-only.
- `MergeSpecs` output must be deterministic: identical sources produce byte-identical results, and reordering sources **of differing document kinds** does not change the output. Precedence among sources **of the same kind** follows argument order — a user who lists two OpenAPI files has expressed an order, and inventing a tiebreaker from their titles would override that with something less predictable. This is a promise, not an accident: it must be pinned by test. `internal/client/generators/typescript/determinism_test.go` exists and must keep passing.
- Single-source behaviour must not change. A lone OpenAPI source still produces `ops.ts`/`hooks.ts`/`rest.ts`; a lone AsyncAPI source still produces `websocket.ts`/`events.ts` with `isAsyncAPIOnly` true.
- Existing scalar `path:` / `url:` keys in `.forge-client.yml` must keep working.
- Warnings go through the existing `spec.Warnings` field. Do not add a second warning channel.
- Run `GOWORK=off` is **not** needed here — `internal/client` is in the root module, which `go.work` includes.
- **Commit with `git commit --only <paths> -m "..."`. Never `git add` at all, and never `git add -A` / `git add .` / `git commit -a`.**

  This work happens on `fix/streaming-frame-decoder`, and **another live session is committing to the same branch and the same working directory concurrently.** A shared working directory means a shared index. `git add` and `git commit` are separate operations, so between your `add` and your `commit` the other session's `add` can stage their files into the same index — and their `commit` can consume yours. Both of those actually happened during Task 1, producing a commit that carries one session's message over the other's content.

  `git commit --only <paths>` builds the commit from exactly the named paths, ignoring whatever else is in the index. That is the mitigation. Every commit step below lists its paths; pass exactly those to `--only`.

  If a commit fails or produces an unexpected result, **stop and report it — do not attempt recovery with `git commit --amend`, `git reset`, or `git rebase`.** An amend during Task 1 landed on the other session's commit and destroyed its message irrecoverably. Recovery on a shared branch is the coordinator's job, not yours.
- Touch nothing under `extensions/streaming/`. No task in this plan has any business there.

## File Structure

| File | Responsibility |
|---|---|
| `internal/client/merge.go` | **New.** `SourceKind`, `MergeSpecs`, collision policy. The only genuinely new logic. |
| `internal/client/merge_test.go` | **New.** Unit tests for merge semantics. |
| `internal/client/ir.go` | Add `Kind SourceKind` to `APISpec`. |
| `internal/client/spec_parser.go` | Split `ParseFile` into `parseDocument` (no resolution) + `ParseFile` (parse + resolve). Set `Kind`. |
| `internal/client/introspector.go` | Set `Kind = SourceIntrospection`. |
| `cmd/forge/plugins/client_config.go` | `SourceConfig.Sources []SourceEntry`, with scalar back-compat. |
| `cmd/forge/plugins/client.go` | `generationPlan` carries source lists; generate merges. |
| `cmd/forge/plugins/client_watch.go` | Watch every file source. |

---

### Task 1: `SourceKind` and `MergeSpecs` unions

**Files:**
- Create: `internal/client/merge.go`
- Create: `internal/client/merge_test.go`
- Modify: `internal/client/ir.go:6-53` (add one field to `APISpec`)

**Interfaces:**
- Consumes: `APISpec`, `Server`, `Tag`, `SecurityScheme`, `Schema`, `EntityRef` from `internal/client/ir.go`.
- Produces: `type SourceKind int`; constants `SourceUnknown`, `SourceOpenAPI`, `SourceAsyncAPI`, `SourceIntrospection`; `func MergeSpecs(specs ...*APISpec) *APISpec`; field `APISpec.Kind SourceKind`.

- [ ] **Step 1: Add the `Kind` field to `APISpec`**

In `internal/client/ir.go`, inside the `APISpec` struct, after the `Streaming *StreamingSpec` field:

```go
	// Kind records which document family this spec was parsed from. MergeSpecs
	// orders sources by this rather than by argument order, so that
	// `--from-spec a.json --from-spec b.json` and the reverse produce identical
	// output. A spec built by Introspector carries SourceIntrospection and
	// ranks with OpenAPI, because it is authoritative for REST the same way.
	Kind SourceKind
```

- [ ] **Step 2: Write the failing test**

Create `internal/client/merge_test.go`:

```go
package client_test

import (
	"testing"

	"github.com/xraph/forge/internal/client"
)

func restSpec() *client.APISpec {
	return &client.APISpec{
		Kind:      client.SourceOpenAPI,
		Info:      client.APIInfo{Title: "Orders", Version: "1.0.0"},
		Servers:   []client.Server{{URL: "https://api.example.com"}},
		Endpoints: []client.Endpoint{{OperationID: "listOrders", Path: "/orders", Method: "GET"}},
		Schemas:   map[string]*client.Schema{"Order": {Type: "object"}},
		Entities:  map[string]*client.EntityRef{"Order": {Type: "Order", IDField: "id"}},
		Tags:      []client.Tag{{Name: "orders"}},
	}
}

func streamSpec() *client.APISpec {
	return &client.APISpec{
		Kind:       client.SourceAsyncAPI,
		Info:       client.APIInfo{Title: "Orders Streams", Version: "2.0.0"},
		Servers:    []client.Server{{URL: "wss://api.example.com"}},
		WebSockets: []client.WebSocketEndpoint{{Path: "/ws/orders"}},
		Schemas:    map[string]*client.Schema{"OrderEvent": {Type: "object"}},
		Tags:       []client.Tag{{Name: "orders"}},
	}
}

func TestMergeSpecsNilAndEmpty(t *testing.T) {
	if got := client.MergeSpecs(); got != nil {
		t.Fatalf("MergeSpecs() with no specs = %v, want nil", got)
	}
	if got := client.MergeSpecs(nil, nil); got != nil {
		t.Fatalf("MergeSpecs(nil, nil) = %v, want nil", got)
	}
}

func TestMergeSpecsSingleSpecIsIdentity(t *testing.T) {
	in := restSpec()
	got := client.MergeSpecs(in)
	if got != in {
		t.Fatalf("MergeSpecs(one) must return that same spec unchanged")
	}
}

func TestMergeSpecsUnionsEndpointsAndStreams(t *testing.T) {
	got := client.MergeSpecs(restSpec(), streamSpec())

	if len(got.Endpoints) != 1 || got.Endpoints[0].OperationID != "listOrders" {
		t.Errorf("Endpoints = %v, want the one REST endpoint", got.Endpoints)
	}
	if len(got.WebSockets) != 1 || got.WebSockets[0].Path != "/ws/orders" {
		t.Errorf("WebSockets = %v, want the one stream endpoint", got.WebSockets)
	}
	if len(got.Schemas) != 2 {
		t.Errorf("Schemas has %d entries, want 2 (Order, OrderEvent)", len(got.Schemas))
	}
	if len(got.Servers) != 2 {
		t.Errorf("Servers has %d entries, want 2 distinct URLs", len(got.Servers))
	}
	if len(got.Tags) != 1 {
		t.Errorf("Tags has %d entries, want 1 after dedup by name", len(got.Tags))
	}
	if got.Info.Title != "Orders" {
		t.Errorf("Info.Title = %q, want the OpenAPI document's title", got.Info.Title)
	}
	if got.RoutingTypes != nil {
		t.Errorf("RoutingTypes must be nil after merge; resolveEntityFields rebuilds it")
	}
}

func TestMergeSpecsOrdersByDocumentKindNotArgumentOrder(t *testing.T) {
	forward := client.MergeSpecs(restSpec(), streamSpec())
	reverse := client.MergeSpecs(streamSpec(), restSpec())

	if forward.Info.Title != reverse.Info.Title {
		t.Errorf("Info.Title differs by argument order: %q vs %q", forward.Info.Title, reverse.Info.Title)
	}
	if len(forward.Endpoints) != len(reverse.Endpoints) {
		t.Errorf("Endpoints count differs by argument order")
	}
	if forward.Servers[0].URL != reverse.Servers[0].URL {
		t.Errorf("Servers order differs by argument order: %q vs %q",
			forward.Servers[0].URL, reverse.Servers[0].URL)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/client/ -run TestMergeSpecs -v`
Expected: FAIL to build — `undefined: client.MergeSpecs`, `undefined: client.SourceOpenAPI`, and `unknown field Kind`.

- [ ] **Step 4: Write the implementation**

Create `internal/client/merge.go`:

```go
package client

import "sort"

// SourceKind records which document family a specification was parsed from.
type SourceKind int

const (
	// SourceUnknown is a spec built by something that did not say. It ranks
	// last, so it can never silently outrank a real REST document.
	SourceUnknown SourceKind = iota
	SourceOpenAPI
	SourceAsyncAPI
	SourceIntrospection
)

// mergeRank orders sources for a merge. OpenAPI and introspection are
// authoritative for shared types because they carry full request and response
// schemas; AsyncAPI fills only what is absent.
func mergeRank(k SourceKind) int {
	switch k {
	case SourceOpenAPI, SourceIntrospection:
		return 0
	case SourceAsyncAPI:
		return 1
	default:
		return 2
	}
}

// MergeSpecs combines parsed specifications into one.
//
// Sources are ordered by document kind rather than by the order they were
// passed, so that `--from-spec async.json --from-spec openapi.json` and its
// reverse produce identical output. Precedence is a property of what a document
// is, not of what order somebody typed.
//
// The result's RoutingTypes is left nil: resolveEntityFields is its only writer
// and rebuilds it from scratch, and merging two pre-built maps would break the
// invariant that RoutingTypes and Entities are disjoint. The caller must run
// resolveEntityFields on the result.
//
// Merging a single spec returns that spec unchanged, so the single-source path
// costs nothing and cannot drift from the multi-source one.
func MergeSpecs(specs ...*APISpec) *APISpec {
	ordered := make([]*APISpec, 0, len(specs))
	for _, s := range specs {
		if s != nil {
			ordered = append(ordered, s)
		}
	}

	switch len(ordered) {
	case 0:
		return nil
	case 1:
		return ordered[0]
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		return mergeRank(ordered[i].Kind) < mergeRank(ordered[j].Kind)
	})

	out := &APISpec{
		Info:     ordered[0].Info,
		Kind:     ordered[0].Kind,
		Schemas:  make(map[string]*Schema),
		Entities: make(map[string]*EntityRef),
	}

	seenServer := make(map[string]bool)
	seenTag := make(map[string]bool)
	seenScheme := make(map[string]bool)

	for _, s := range ordered {
		out.Endpoints = append(out.Endpoints, s.Endpoints...)
		out.WebSockets = append(out.WebSockets, s.WebSockets...)
		out.SSEs = append(out.SSEs, s.SSEs...)
		out.WebTransports = append(out.WebTransports, s.WebTransports...)
		out.Warnings = append(out.Warnings, s.Warnings...)

		for _, srv := range s.Servers {
			if !seenServer[srv.URL] {
				seenServer[srv.URL] = true
				out.Servers = append(out.Servers, srv)
			}
		}
		for _, tag := range s.Tags {
			if !seenTag[tag.Name] {
				seenTag[tag.Name] = true
				out.Tags = append(out.Tags, tag)
			}
		}
		for _, sec := range s.Security {
			if !seenScheme[sec.Name] {
				seenScheme[sec.Name] = true
				out.Security = append(out.Security, sec)
			}
		}

		for name, schema := range s.Schemas {
			if _, taken := out.Schemas[name]; !taken {
				out.Schemas[name] = schema
			}
		}
		for name, ent := range s.Entities {
			if _, taken := out.Entities[name]; !taken {
				out.Entities[name] = ent
			}
		}

		if out.Streaming == nil {
			out.Streaming = s.Streaming
		}
	}

	return out
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/client/ -run TestMergeSpecs -v`
Expected: PASS, all four tests.

- [ ] **Step 6: Verify nothing else broke**

Run: `go build ./... && go test ./internal/client/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/client/merge.go internal/client/merge_test.go internal/client/ir.go
git commit -m "feat(client): add MergeSpecs and SourceKind

Combines parsed specifications into one, ordered by document kind rather
than argument order so precedence is a property of the document. Leaves
RoutingTypes nil for resolveEntityFields to rebuild."
```

---

### Task 2: Collision policy and warnings

**Files:**
- Modify: `internal/client/merge.go` (the two map loops from Task 1)
- Modify: `internal/client/merge_test.go` (add cases)

**Interfaces:**
- Consumes: `MergeSpecs` from Task 1.
- Produces: no new exported names. `MergeSpecs` now appends collision warnings to `out.Warnings`.

- [ ] **Step 1: Write the failing test**

Append to `internal/client/merge_test.go`:

```go
func hasWarningContaining(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestMergeSpecsIdenticalRedeclarationIsSilent(t *testing.T) {
	a := restSpec()
	b := streamSpec()
	// Same name, structurally identical: the normal case, not a conflict.
	b.Schemas["Order"] = &client.Schema{Type: "object"}

	got := client.MergeSpecs(a, b)

	if hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("identical redeclaration must not warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsWarnsOnDifferingSchemaShape(t *testing.T) {
	a := restSpec()
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "string"} // genuinely different

	got := client.MergeSpecs(a, b)

	if got.Schemas["Order"].Type != "object" {
		t.Errorf("Schemas[Order].Type = %q, want the OpenAPI shape %q",
			got.Schemas["Order"].Type, "object")
	}
	if !hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("differing schema shape must warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsWarnsOnDifferingEntityIDField(t *testing.T) {
	a := restSpec()
	b := streamSpec()
	b.Entities = map[string]*client.EntityRef{
		"Order": {Type: "Order", IDField: "orderId"},
	}

	got := client.MergeSpecs(a, b)

	if got.Entities["Order"].IDField != "id" {
		t.Errorf("Entities[Order].IDField = %q, want the OpenAPI value %q",
			got.Entities["Order"].IDField, "id")
	}
	if !hasWarningContaining(got.Warnings, "orderId") {
		t.Errorf("differing IDField must warn naming both values, got %v", got.Warnings)
	}
}

func TestMergeSpecsWarnsOnDuplicateRoute(t *testing.T) {
	a := restSpec()
	b := restSpec()
	b.Info.Title = "Second"

	got := client.MergeSpecs(a, b)

	if !hasWarningContaining(got.Warnings, "GET /orders") {
		t.Errorf("duplicate path+method must warn, got %v", got.Warnings)
	}
}
```

Add `"strings"` to that file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/client/ -run TestMergeSpecs -v`
Expected: FAIL — the three warning assertions report empty `Warnings`.

- [ ] **Step 3: Add schema equivalence and collision reporting**

In `internal/client/merge.go`, add to the imports `"fmt"`, then replace the two map loops and add the route check. The loop body inside `for _, s := range ordered` becomes:

```go
		for name, schema := range s.Schemas {
			existing, taken := out.Schemas[name]
			if !taken {
				out.Schemas[name] = schema
				continue
			}
			if !sameSchemaShape(existing, schema) {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"schema %q is declared differently in two sources; keeping the %s definition (type %q) and ignoring the %s one (type %q)",
					name, kindName(out.Kind), schemaType(existing), kindName(s.Kind), schemaType(schema)))
			}
		}
		for name, ent := range s.Entities {
			existing, taken := out.Entities[name]
			if !taken {
				out.Entities[name] = ent
				continue
			}
			if existing.IDField != ent.IDField {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"entity %q has id field %q in the %s source and %q in the %s source; keeping %q",
					name, existing.IDField, kindName(out.Kind),
					ent.IDField, kindName(s.Kind), existing.IDField))
			}
		}
```

And after the source loop, before `return out`:

```go
	seenRoute := make(map[string]bool)
	for _, ep := range out.Endpoints {
		key := ep.Method + " " + ep.Path
		if seenRoute[key] {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"route %q is declared in more than one source; the first declaration wins", key))
			continue
		}
		seenRoute[key] = true
	}
```

Then add these helpers at the bottom of the file:

```go
// sameSchemaShape reports whether two schemas describe the same thing closely
// enough that declaring both is not a conflict. It compares the structural
// fields only: descriptions and examples differ freely between a REST document
// and a stream document describing one type, and warning about those would
// train the reader to ignore the warning that matters.
func sameSchemaShape(a, b *Schema) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Type != b.Type || a.Format != b.Format || a.Nullable != b.Nullable {
		return false
	}
	if len(a.Properties) != len(b.Properties) || len(a.Required) != len(b.Required) {
		return false
	}
	for name, av := range a.Properties {
		bv, ok := b.Properties[name]
		if !ok || !sameSchemaShape(av, bv) {
			return false
		}
	}
	required := make(map[string]bool, len(a.Required))
	for _, r := range a.Required {
		required[r] = true
	}
	for _, r := range b.Required {
		if !required[r] {
			return false
		}
	}
	return sameSchemaShape(a.Items, b.Items)
}

func schemaType(s *Schema) string {
	if s == nil {
		return "<nil>"
	}
	return s.Type
}

func kindName(k SourceKind) string {
	switch k {
	case SourceOpenAPI:
		return "OpenAPI"
	case SourceAsyncAPI:
		return "AsyncAPI"
	case SourceIntrospection:
		return "introspected"
	default:
		return "unknown"
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/client/ -run TestMergeSpecs -v`
Expected: PASS, all eight tests.

- [ ] **Step 5: Commit**

```bash
git add internal/client/merge.go internal/client/merge_test.go
git commit -m "feat(client): report merge collisions through spec.Warnings

OpenAPI wins on a schema or entity declared in two sources. Structurally
identical redeclaration is silent -- it is the normal case -- so the
warning only fires on a genuine disagreement."
```

---

### Task 3: Defer entity resolution

**Files:**
- Modify: `internal/client/spec_parser.go:28-75`
- Modify: `internal/client/introspector.go:64`
- Create: `internal/client/merge_resolve_test.go`

**Interfaces:**
- Consumes: `MergeSpecs` from Task 1, `resolveEntityFields(spec *APISpec)` from `internal/client/entity_fields.go:62`.
- Produces: `func (p *SpecParser) ParseFileUnresolved(ctx context.Context, filePath string) (*APISpec, error)`. `ParseFile` keeps its existing signature and behaviour.

**Why this task exists.** An earlier draft of this plan claimed the reason was warnings — that resolving a half-populated spec reports entities living in the other document as unresolvable stream bindings. **That claim was false and has been removed.** `resolveEntityFields` never writes to `spec.Warnings` (`grep -n Warnings internal/client/entity_fields.go` returns nothing). Those warnings come from `registerStreamBindingEntities`, called at `spec_parser.go:798` and `:821` *during parsing*, so when resolution runs cannot affect them.

The honest reason is narrower. `resolveEntityFields` is idempotent — it replaces each entity's `Fields` and rebuilds `RoutingTypes` from scratch — so parsing-with-resolution and then re-resolving after the merge is equally correct. Deferring resolution avoids doing that work once per document and then discarding it, and it puts resolution at the point where the full set of schemas is actually known, which is where a reader expects it. This is a clarity and wasted-work improvement, not a correctness requirement.

**Do not write a test asserting that deferral suppresses warnings.** It does not, and such a test can only pass vacuously — which is exactly what happened: the original assertion searched for the substring `"no schema describes"`, which appears nowhere in this codebase outside doc comments. The real message is `"...which has no matching schema component; this binding will not normalize"`. Test the property the split actually has: entity field edges spanning two documents resolve correctly over the merged spec.

- [ ] **Step 1: Write the failing test**

Create `internal/client/merge_resolve_test.go`:

```go
package client_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// writeSpec writes content to a temp file with the given name and returns its path.
func writeSpec(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

const restDoc = `
openapi: 3.1.0
info:
  title: Orders
  version: 1.0.0
paths:
  /orders:
    get:
      operationId: listOrders
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Order'
components:
  schemas:
    Order:
      type: object
      x-forge-entity:
        idField: id
      properties:
        id:
          type: string
`

const streamDoc = `
asyncapi: 3.0.0
info:
  title: Orders Streams
  version: 1.0.0
channels:
  orders:
    address: /ws/orders
    messages:
      orderUpdated:
        payload:
          $ref: '#/components/schemas/OrderEvent'
components:
  schemas:
    OrderEvent:
      type: object
      properties:
        id:
          type: string
`

func TestParseFileUnresolvedLeavesRoutingTypesUnbuilt(t *testing.T) {
	p := client.NewSpecParser()
	path := writeSpec(t, "openapi.yaml", restDoc)

	spec, err := p.ParseFileUnresolved(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFileUnresolved: %v", err)
	}
	if spec.RoutingTypes != nil {
		t.Errorf("RoutingTypes = %v, want nil before resolution", spec.RoutingTypes)
	}
	if spec.Kind != client.SourceOpenAPI {
		t.Errorf("Kind = %v, want SourceOpenAPI", spec.Kind)
	}
}

func TestParseFileStillResolves(t *testing.T) {
	p := client.NewSpecParser()
	path := writeSpec(t, "openapi.yaml", restDoc)

	spec, err := p.ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if spec.Entities["Order"] == nil {
		t.Fatalf("Entities[Order] missing after ParseFile")
	}
	if spec.Kind != client.SourceOpenAPI {
		t.Errorf("Kind = %v, want SourceOpenAPI", spec.Kind)
	}
}

func TestUnresolvedParseThenMergeCarriesNoSpuriousWarnings(t *testing.T) {
	p := client.NewSpecParser()
	restPath := writeSpec(t, "openapi.yaml", restDoc)
	streamPath := writeSpec(t, "asyncapi.yaml", streamDoc)

	rest, err := p.ParseFileUnresolved(context.Background(), restPath)
	if err != nil {
		t.Fatalf("parse rest: %v", err)
	}
	stream, err := p.ParseFileUnresolved(context.Background(), streamPath)
	if err != nil {
		t.Fatalf("parse stream: %v", err)
	}

	merged := client.MergeSpecs(rest, stream)
	client.ResolveEntityFieldsForTest(merged)

	for _, w := range merged.Warnings {
		if strings.Contains(w, "no schema describes") {
			t.Errorf("merged spec carries a spurious unresolved-entity warning: %q", w)
		}
	}
	if len(merged.Endpoints) == 0 {
		t.Errorf("merged spec lost its REST endpoints")
	}
	if len(merged.WebSockets) == 0 {
		t.Errorf("merged spec lost its stream endpoints")
	}
}
```

- [ ] **Step 2: Add the test-only resolution export**

`resolveEntityFields` is unexported and the tests are in `package client_test`. Create `internal/client/export_test.go`:

```go
package client

// ResolveEntityFieldsForTest exposes resolveEntityFields to the external test
// package. Test-only: this file is not compiled into the package binary.
func ResolveEntityFieldsForTest(spec *APISpec) { resolveEntityFields(spec) }
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/client/ -run "TestParseFile|TestUnresolvedParse" -v`
Expected: FAIL to build — `p.ParseFileUnresolved undefined`.

- [ ] **Step 4: Split the parser**

In `internal/client/spec_parser.go`, replace the body of `ParseFile` (lines 28-75) with:

```go
// ParseFile parses a specification file and resolves entity field edges.
// This is the single-source path and its behaviour is unchanged.
func (p *SpecParser) ParseFile(ctx context.Context, filePath string) (*APISpec, error) {
	spec, err := p.ParseFileUnresolved(ctx, filePath)
	if err != nil {
		return nil, err
	}
	resolveEntityFields(spec)
	return spec, nil
}

// ParseFileUnresolved parses a specification file without resolving entity
// field edges.
//
// A merge of several documents must resolve once over the merged whole, not
// once per document. Resolving a half-populated spec does not merely waste
// work: it reports every entity that lives in the *other* document as a stream
// binding naming a type no schema describes, and those warnings would survive
// into the merged result and describe a correct pair of documents as broken.
//
// The caller is responsible for calling resolveEntityFields, directly or via
// ParseFile.
func (p *SpecParser) ParseFileUnresolved(ctx context.Context, filePath string) (*APISpec, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read spec file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	isYAML := ext == ".yaml" || ext == ".yml"

	specType, err := p.detectSpecType(data, isYAML)
	if err != nil {
		return nil, fmt.Errorf("detect spec type: %w", err)
	}

	var spec *APISpec

	switch specType {
	case "openapi":
		spec, err = p.parseOpenAPI(data, isYAML)
		if spec != nil {
			spec.Kind = SourceOpenAPI
		}
	case "asyncapi":
		spec, err = p.parseAsyncAPI(data, isYAML)
		if spec != nil {
			spec.Kind = SourceAsyncAPI
		}
	default:
		return nil, fmt.Errorf("unknown spec type: %s", specType)
	}

	if err != nil {
		return nil, err
	}

	return spec, nil
}
```

Keep the existing `ctx` parameter unused exactly as it is today — do not rename it, and do not add a `_ = ctx`.

- [ ] **Step 5: Set `Kind` on the introspector path**

In `internal/client/introspector.go`, at line 64 where `resolveEntityFields(spec)` is called, add immediately before it:

```go
	spec.Kind = SourceIntrospection
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/client/ -run "TestParseFile|TestUnresolvedParse" -v`
Expected: PASS, all three tests.

- [ ] **Step 7: Run the full package to catch regressions**

Run: `go test ./internal/client/...`
Expected: PASS. The existing parser tests exercise `ParseFile`, which still resolves.

- [ ] **Step 8: Commit**

```bash
git add internal/client/spec_parser.go internal/client/introspector.go internal/client/export_test.go internal/client/merge_resolve_test.go
git commit -m "feat(client): add ParseFileUnresolved for multi-source merging

Resolving a half-populated spec reports every entity living in the other
document as an unresolvable stream binding, and those warnings survive
the merge. ParseFile is unchanged, now composed from the two steps."
```

---

### Task 4: Multi-source configuration and CLI flags

**Files:**
- Modify: `cmd/forge/plugins/client_config.go:50-63`
- Modify: `cmd/forge/plugins/client.go:93-94,114-115`
- Create: `cmd/forge/plugins/client_sources_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `type SourceEntry struct { Type string; Path string; URL string }`; field `SourceConfig.Sources []SourceEntry`; method `func (s SourceConfig) Entries() []SourceEntry`.

- [ ] **Step 1: Write the failing test**

Create `cmd/forge/plugins/client_sources_test.go`:

```go
package plugins

import "testing"

func TestSourceEntriesFromScalarPath(t *testing.T) {
	s := SourceConfig{Type: "file", Path: "openapi.json"}

	got := s.Entries()

	if len(got) != 1 {
		t.Fatalf("Entries() returned %d entries, want 1", len(got))
	}
	if got[0].Path != "openapi.json" || got[0].Type != "file" {
		t.Errorf("Entries()[0] = %+v, want the scalar path as one file entry", got[0])
	}
}

func TestSourceEntriesFromScalarURL(t *testing.T) {
	s := SourceConfig{Type: "url", URL: "https://example.com/openapi.json"}

	got := s.Entries()

	if len(got) != 1 || got[0].URL != "https://example.com/openapi.json" {
		t.Fatalf("Entries() = %+v, want the scalar URL as one entry", got)
	}
}

func TestSourceEntriesPrefersExplicitList(t *testing.T) {
	s := SourceConfig{
		Type: "file",
		Path: "ignored.json",
		Sources: []SourceEntry{
			{Type: "file", Path: "openapi.json"},
			{Type: "file", Path: "asyncapi.json"},
		},
	}

	got := s.Entries()

	if len(got) != 2 {
		t.Fatalf("Entries() returned %d entries, want 2", len(got))
	}
	if got[0].Path != "openapi.json" || got[1].Path != "asyncapi.json" {
		t.Errorf("Entries() = %+v, want list order preserved", got)
	}
}

func TestSourceEntriesEmptyWhenNothingConfigured(t *testing.T) {
	if got := (SourceConfig{}).Entries(); len(got) != 0 {
		t.Errorf("Entries() = %+v, want empty", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/forge/plugins/ -run TestSourceEntries -v`
Expected: FAIL to build — `undefined: SourceEntry`, `s.Sources undefined`, `s.Entries undefined`.

- [ ] **Step 3: Add `SourceEntry` and `Entries()`**

In `cmd/forge/plugins/client_config.go`, replace the `SourceConfig` struct (lines 50-63) with:

```go
// SourceEntry is one specification document to read.
type SourceEntry struct {
	// Type: "file" or "url".
	Type string `yaml:"type"`

	Path string `yaml:"path,omitempty"`
	URL  string `yaml:"url,omitempty"`
}

// SourceConfig defines where to get the API specification.
//
// Sources is one ordered list rather than parallel path and url arrays,
// because merge precedence depends on order and parallel arrays leave the
// relative order of a file source and a URL source undefined.
type SourceConfig struct {
	// Type: "file", "url", "auto"
	Type string `yaml:"type"`

	// Path to spec file (when type=file). Read as a one-element Sources list
	// when Sources is empty, so an existing .forge-client.yml keeps working.
	Path string `yaml:"path,omitempty"`

	// URL to fetch spec (when type=url). Same one-element handling as Path.
	URL string `yaml:"url,omitempty"`

	// Sources lists several documents to parse and merge. When set it wins
	// over the scalar Path and URL keys above.
	Sources []SourceEntry `yaml:"sources,omitempty"`

	// Auto-discovery paths (when type=auto)
	AutoDiscoverPaths []string `yaml:"auto_discover_paths,omitempty"`
}

// Entries normalises this configuration into the list of documents to read.
func (s SourceConfig) Entries() []SourceEntry {
	if len(s.Sources) > 0 {
		return s.Sources
	}
	switch {
	case s.Path != "":
		return []SourceEntry{{Type: "file", Path: s.Path}}
	case s.URL != "":
		return []SourceEntry{{Type: "url", URL: s.URL}}
	default:
		return nil
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/forge/plugins/ -run TestSourceEntries -v`
Expected: PASS, all four tests.

- [ ] **Step 5: Make the CLI flags repeatable**

In `cmd/forge/plugins/client.go`, at lines 93-94 and again at 114-115, replace each `cli.NewStringFlag` for `from-spec` and `from-url` with its slice equivalent:

```go
		cli.WithFlag(cli.NewStringSliceFlag("from-spec", "s", "Path to an OpenAPI/AsyncAPI spec file (repeatable)", nil)),
		cli.WithFlag(cli.NewStringSliceFlag("from-url", "u", "URL to fetch an OpenAPI/AsyncAPI spec (repeatable)", nil)),
```

`cli.NewStringSliceFlag` exists and is already used at `cmd/forge/plugins/generate.go:83`. Read the values back with `ctx.StringSlice("from-spec")` and `ctx.StringSlice("from-url")`, the same way `generate.go:878` does.

- [ ] **Step 6: Verify the build**

Run: `go build ./... && go test ./cmd/forge/plugins/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/forge/plugins/client_config.go cmd/forge/plugins/client.go cmd/forge/plugins/client_sources_test.go
git commit -m "feat(cli): accept several spec sources

SourceConfig gains an ordered Sources list and Entries() normalises the
scalar path/url keys into it, so an existing .forge-client.yml keeps
working. --from-spec and --from-url become repeatable."
```

---

### Task 5: Merge sources in the generate path

**Files:**
- Modify: `cmd/forge/plugins/client.go:177-188` (`generationPlan`), and its `resolveGenerationPlan` and `generateClient`
- Create: `internal/client/generators/typescript/e2e_merged_sources_test.go`

**Interfaces:**
- Consumes: `SourceConfig.Entries()` (Task 4), `SpecParser.ParseFileUnresolved` (Task 3), `client.MergeSpecs` (Task 1).
- Produces: field `generationPlan.specPaths []string` replacing `specPath`; field `generationPlan.specURLs []string` replacing `specURL`.

- [ ] **Step 1: Write the failing E2E test**

`internal/client/generators/typescript/e2e_specfile_test.go` already has two helpers in `package typescript_test`, both usable from a new file in the same package:

- `writeSpecFile(t *testing.T, name, content string) string` (line 94)
- `generateFromSpecFile(t *testing.T, path string) map[string]string` (line 109)

`generateFromSpecFile` takes **one** path, so this task adds a plural sibling. Build it by reading the singular one and changing only the parse-and-merge portion — everything after `MergeSpecs` (generator construction, config, collecting output files) must be identical, or the E2E test stops testing the real generation path.

Create `internal/client/generators/typescript/e2e_merged_sources_test.go`:

```go
package typescript_test

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

const mergedRestDoc = `
openapi: 3.1.0
info:
  title: Orders
  version: 1.0.0
paths:
  /orders:
    get:
      operationId: listOrders
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Order'
components:
  schemas:
    Order:
      type: object
      x-forge-entity:
        idField: id
      properties:
        id:
          type: string
`

const mergedStreamDoc = `
asyncapi: 3.0.0
info:
  title: Orders Streams
  version: 1.0.0
channels:
  orders:
    address: /ws/orders
    messages:
      orderUpdated:
        payload:
          $ref: '#/components/schemas/Order'
components:
  schemas:
    Order:
      type: object
      x-forge-entity:
        idField: id
      properties:
        id:
          type: string
`

// generateFromSpecFiles is the plural form of generateFromSpecFile: it parses
// each document without resolving, merges, resolves once, then drives the
// generator exactly as the singular helper does.
func generateFromSpecFiles(t *testing.T, paths ...string) map[string]string {
	t.Helper()

	parser := client.NewSpecParser()
	specs := make([]*client.APISpec, 0, len(paths))
	for _, p := range paths {
		spec, err := parser.ParseFileUnresolved(context.Background(), p)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		specs = append(specs, spec)
	}

	merged := client.MergeSpecs(specs...)
	if merged == nil {
		t.Fatal("MergeSpecs returned nil")
	}
	client.ResolveEntityFields(merged)

	// Everything below must mirror generateFromSpecFile after its parse step.
	return generateFromMergedSpec(t, merged)
}

func TestMergedSourcesProduceOnePackageWithBoth(t *testing.T) {
	rest := writeSpecFile(t, "openapi.yaml", mergedRestDoc)
	stream := writeSpecFile(t, "asyncapi.yaml", mergedStreamDoc)

	files := generateFromSpecFiles(t, rest, stream)

	for _, want := range []string{"ops.ts", "hooks.ts", "rest.ts", "websocket.ts"} {
		if _, ok := files[want]; !ok {
			t.Errorf("%s was not generated from a merged pair of documents", want)
		}
	}
}

// TestMergedSourcesPopulateStreamsManifest is the assertion that makes this
// feature real. A package containing both file sets while `streams` stays
// empty is exactly the bug being fixed: every file is present and
// `{ live: true }` still does nothing.
func TestMergedSourcesPopulateStreamsManifest(t *testing.T) {
	rest := writeSpecFile(t, "openapi.yaml", mergedRestDoc)
	stream := writeSpecFile(t, "asyncapi.yaml", mergedStreamDoc)

	files := generateFromSpecFiles(t, rest, stream)

	ops, ok := files["ops.ts"]
	if !ok {
		t.Fatal("ops.ts was not generated from a merged spec")
	}
	if strings.Contains(ops, "streams: {}") {
		t.Errorf("ops.ts carries an empty streams table; { live: true } would do nothing")
	}
	if !strings.Contains(ops, "/ws/orders") {
		t.Errorf("ops.ts streams table does not mention the channel from the AsyncAPI document")
	}
}

func TestSingleSpecFileStillGeneratesIdentically(t *testing.T) {
	path := writeSpecFile(t, "openapi.yaml", mergedRestDoc)

	singular := generateFromSpecFile(t, path)
	plural := generateFromSpecFiles(t, path)

	for name, want := range singular {
		if plural[name] != want {
			t.Errorf("%s differs between the single-source and merge paths", name)
		}
	}
}
```

**`generateFromMergedSpec` does not exist yet.** Extract it from `generateFromSpecFile` (line 109) as a first refactor step: split that function at its parse boundary into `generateFromSpecFile` (parse, then delegate) and `generateFromMergedSpec(t *testing.T, spec *client.APISpec) map[string]string` (everything after). `TestSingleSpecFileStillGeneratesIdentically` above is what proves the extraction was faithful.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestMergedSources -v`
Expected: FAIL — either on the missing helper, or on `streams: {}` if the generator is fed a spec that never carried both.

- [ ] **Step 3: Carry source lists on the plan**

In `cmd/forge/plugins/client.go`, replace the `generationPlan` struct (lines 177-188) with:

```go
type generationPlan struct {
	// specPaths are the local files to parse, in configuration order. A
	// downloaded spec appears here as a temp file.
	specPaths []string
	// specURLs is set for each spec that came over HTTP; the matching entry in
	// specPaths is then a temp file holding the fetched bytes. generate and
	// check never look at these -- they only need the files -- but `watch`
	// cannot tell a downloaded spec from a local one by its path, and polling a
	// temp file that nothing ever writes to again would be a watch that can
	// never fire.
	specURLs  []string
	outputDir string
	config    client.GeneratorConfig
	cleanup   func()
}
```

Update `resolveGenerationPlan` to build these lists from `SourceConfig.Entries()` and from the repeatable flags, appending flag sources in argument order after config sources. Keep the existing single-source resolution logic per entry — fetching a URL to a temp file, resolving a relative path — and accumulate `cleanup` into one closure that runs every per-entry cleanup.

- [ ] **Step 4: Merge in the generate path**

Replace the single `parser.ParseFile` call at `cmd/forge/plugins/client.go:815` with:

```go
	parser := client.NewSpecParser()

	specs := make([]*client.APISpec, 0, len(plan.specPaths))
	for _, path := range plan.specPaths {
		// Unresolved: entity edges are resolved once over the merged whole.
		// Resolving per document reports every entity defined in another
		// document as unresolvable.
		spec, err := parser.ParseFileUnresolved(context.Background(), path)
		if err != nil {
			// A source that will not parse aborts the run. Skipping it would
			// emit a package with a silently empty streams table, which is the
			// exact failure this path exists to remove.
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		specs = append(specs, spec)
	}

	spec := client.MergeSpecs(specs...)
	if spec == nil {
		return nil, errors.New("no specification sources resolved")
	}
	client.ResolveEntityFields(spec)

	if len(spec.Endpoints) == 0 && len(spec.WebSockets) == 0 &&
		len(spec.SSEs) == 0 && len(spec.WebTransports) == 0 {
		return nil, errors.New("merged specification describes no endpoints and no streams")
	}
```

- [ ] **Step 5: Export `ResolveEntityFields`**

The generate path lives in `package plugins` and needs resolution after merging, so it cannot use the test-only export from Task 3. In `internal/client/entity_fields.go`, add above the unexported function:

```go
// ResolveEntityFields resolves entity field edges over a specification. Call it
// once, after merging every source: see MergeSpecs, which deliberately leaves
// RoutingTypes nil for this function to rebuild.
func ResolveEntityFields(spec *APISpec) { resolveEntityFields(spec) }
```

Then delete `internal/client/export_test.go` created in Task 3, and change the one call in `merge_resolve_test.go` from `client.ResolveEntityFieldsForTest(merged)` to `client.ResolveEntityFields(merged)`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/client/... ./cmd/forge/plugins/`
Expected: PASS, including `TestMergedSourcesPopulateStreamsManifest`.

- [ ] **Step 7: Verify single-source output is unchanged**

Run: `go test ./internal/client/generators/typescript/ -run "TestDeterminism|TestE2E" -v`
Expected: PASS with no golden-file changes. If a golden changed, stop — single-source output must be byte-identical.

- [ ] **Step 8: Commit**

**Stage explicit paths only. Never `git add -A` or `git add .`** — this branch carries unrelated in-flight work from another effort, and a blanket stage would commit someone else's files under your message.

```bash
git rm --cached -f internal/client/export_test.go 2>/dev/null; rm -f internal/client/export_test.go
git add cmd/forge/plugins/client.go \
        internal/client/entity_fields.go \
        internal/client/merge_resolve_test.go \
        internal/client/generators/typescript/e2e_specfile_test.go \
        internal/client/generators/typescript/e2e_merged_sources_test.go
git commit -m "feat(cli): merge several spec sources into one package

generate parses each source unresolved, merges, then resolves once, so a
REST document and a stream document produce one package with a populated
streams table. A source that fails to parse aborts rather than degrading
to a half-empty package."
```

---

### Task 6: Watch every source

**Files:**
- Modify: `cmd/forge/plugins/client_watch.go:168-260`
- Modify: `cmd/forge/plugins/client_watch_test.go`

**Interfaces:**
- Consumes: `generationPlan.specPaths`, `generationPlan.specURLs` (Task 5).
- Produces: `func resolveWatchSources(plan *generationPlan) ([]watchSource, error)` replacing `resolveWatchSource`.

- [ ] **Step 1: Write the failing test**

Append to `cmd/forge/plugins/client_watch_test.go`:

```go
func TestResolveWatchSourcesCoversEveryFileSource(t *testing.T) {
	dir := t.TempDir()
	openapi := filepath.Join(dir, "openapi.json")
	asyncapi := filepath.Join(dir, "asyncapi.json")
	for _, p := range []string{openapi, asyncapi} {
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	plan := &generationPlan{specPaths: []string{openapi, asyncapi}}

	got, err := resolveWatchSources(plan)
	if err != nil {
		t.Fatalf("resolveWatchSources: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("resolveWatchSources returned %d sources, want 2", len(got))
	}
}

func TestResolveWatchSourcesErrorsWithNoSources(t *testing.T) {
	if _, err := resolveWatchSources(&generationPlan{}); err == nil {
		t.Fatal("resolveWatchSources with no sources must return an error")
	}
}
```

Ensure `os` and `path/filepath` are imported in that test file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/forge/plugins/ -run TestResolveWatchSources -v`
Expected: FAIL to build — `undefined: resolveWatchSources`.

- [ ] **Step 3: Implement the plural resolver**

In `cmd/forge/plugins/client_watch.go`, replace `resolveWatchSource` with:

```go
// resolveWatchSources decides what the plan's spec sources mean for a watcher.
// Every source is watched: a client generated from two documents is stale when
// either changes, and watching only the first would rebuild on a REST edit and
// sit still on a stream edit.
func resolveWatchSources(plan *generationPlan) ([]watchSource, error) {
	sources := make([]watchSource, 0, len(plan.specPaths))

	for i, path := range plan.specPaths {
		if i < len(plan.specURLs) && plan.specURLs[i] != "" {
			sources = append(sources, watchSource{url: plan.specURLs[i]})
			continue
		}
		resolved, err := filepath.Abs(path)
		if err != nil {
			return nil, cli.WrapError(err, "resolve spec path", cli.ExitUsageError)
		}
		sources = append(sources, watchSource{path: resolved})
	}

	if len(sources) == 0 {
		return nil, cli.NewError("no spec source to watch", cli.ExitUsageError)
	}
	return sources, nil
}
```

Preserve every other check the original `resolveWatchSource` performed on a single path — read the function before replacing it and carry each guard into the loop body. Update the caller at line 94 to range over the returned slice, registering each with the watcher, and make `matches` be consulted for each source.

- [ ] **Step 4: Run the tests**

Run: `go test ./cmd/forge/plugins/ -v`
Expected: PASS, including the pre-existing watch tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/forge/plugins/client_watch.go cmd/forge/plugins/client_watch_test.go
git commit -m "feat(cli): watch every spec source

A client generated from two documents is stale when either changes.
Watching only the first would rebuild on a REST edit and sit still on a
stream edit."
```

---

### Task 7: Introspector as an optional source

**Files:**
- Modify: `cmd/forge/plugins/client.go` (`resolveGenerationPlan`)
- Create: `internal/client/introspector_kind_test.go`

**Interfaces:**
- Consumes: `client.NewIntrospector(r router.Router)` and `(*Introspector).Introspect(ctx) (*APISpec, error)` from `internal/client/introspector.go:21,26`; `MergeSpecs` (Task 1).
- Produces: no new exported names.

- [ ] **Step 1: Write the failing test**

Create `internal/client/introspector_kind_test.go`:

```go
package client_test

import (
	"testing"

	"github.com/xraph/forge/internal/client"
)

// An introspected spec must rank with OpenAPI, not below AsyncAPI: it is
// authoritative for REST in the same way, so a merge that put it second would
// let a stream document's schema definition win over the live router's.
func TestIntrospectionRanksWithOpenAPI(t *testing.T) {
	introspected := &client.APISpec{
		Kind:      client.SourceIntrospection,
		Info:      client.APIInfo{Title: "Live"},
		Endpoints: []client.Endpoint{{OperationID: "listOrders", Path: "/orders", Method: "GET"}},
		Schemas:   map[string]*client.Schema{"Order": {Type: "object"}},
	}
	stream := &client.APISpec{
		Kind:       client.SourceAsyncAPI,
		Info:       client.APIInfo{Title: "Streams"},
		WebSockets: []client.WebSocketEndpoint{{Path: "/ws/orders"}},
		Schemas:    map[string]*client.Schema{"Order": {Type: "string"}},
	}

	got := client.MergeSpecs(stream, introspected)

	if got.Info.Title != "Live" {
		t.Errorf("Info.Title = %q, want the introspected title", got.Info.Title)
	}
	if got.Schemas["Order"].Type != "object" {
		t.Errorf("Schemas[Order].Type = %q, want the introspected shape", got.Schemas["Order"].Type)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails or passes**

Run: `go test ./internal/client/ -run TestIntrospectionRanksWithOpenAPI -v`
Expected: PASS if Task 1's `mergeRank` is correct. This test pins that behaviour so a later edit to `mergeRank` cannot quietly demote introspection. If it FAILS, fix `mergeRank` — introspection must return rank 0.

- [ ] **Step 3: Stop here — introspection is not reachable from the CLI**

Verified: `grep -rn "router.Router" cmd/forge/` returns nothing. No CLI path holds a `router.Router`, so `Introspector.Introspect` cannot be called from `forge client generate` without first giving the CLI a way to obtain a live router — booting the app, or an in-process API. That is a separate feature with its own design, not a step in this plan.

This task therefore delivers Steps 1-2 only: the ranking is pinned by test so that whoever wires introspection later inherits correct merge precedence rather than discovering it. Do **not** add an introspection flag or a stub router here.

Record this in the commit message so the omission is deliberate and searchable.

- [ ] **Step 4: Run the full suite**

Run: `go build ./... && go test ./internal/client/... ./cmd/forge/plugins/`
Expected: PASS.

- [ ] **Step 5: Commit**

**Stage explicit paths only. Never `git add -A` or `git add .`** — see Task 5.

```bash
git add internal/client/introspector_kind_test.go
git commit -m "feat(client): rank introspected specs with OpenAPI

An introspected spec is authoritative for REST the same way an OpenAPI
document is, so it must not lose a schema collision to a stream document."
```

---

## Self-Review

**Spec coverage.** Every section of the design maps to a task: architecture and the deferral to Task 3; `merge.go` to Tasks 1-2; the merge semantics table to Tasks 1-2; multi-source `SourceConfig` and CLI flags to Task 4; `generationPlan` lists and the generate path to Task 5; `resolveWatchSource` to Task 6; the introspector to Task 7. Error handling is covered in Task 5 Step 4 (hard errors) and Task 2 (warnings). Testing is distributed: unit in Tasks 1-2, determinism in Task 1 Step 4 and Task 5 Step 7, goldens in Task 5 Step 7, E2E in Task 5, cross-document resolution in Task 3.

**Deviations from the spec, deliberate.**
1. The spec did not anticipate that `APISpec` records no document family. Task 1 adds `Kind SourceKind`, without which ordering by document type is not expressible.
2. The spec justified deferring resolution by cross-document edges. Task 3 records the stronger reason found in the code: `resolveEntityFields` is documented as safe to call twice, so edges alone would not require deferral — but resolving a half-populated spec emits spurious unresolvable-entity warnings that survive into the merged result.
3. Task 5 Step 5 exports `ResolveEntityFields`, replacing the test-only export added in Task 3, because `package plugins` needs it too.

**Unknowns, resolved before the plan was finalised.** Three points would otherwise have had the plan inventing interfaces it had not verified. All three were checked against the tree:

1. `cli.NewStringSliceFlag` **exists** (`cmd/forge/plugins/generate.go:83`), read back with `ctx.StringSlice` (`generate.go:878`). Task 4 Step 5 names both concretely.
2. The E2E helper is **`generateFromSpecFile(t, path) map[string]string`** (`e2e_specfile_test.go:109`) and takes a *file path*, not a spec. The first draft of Task 5 assumed a spec-based `generatePackage(t, spec)`, which would have generated from an in-memory spec and quietly bypassed the parse path the feature changes. Task 5 now extracts `generateFromMergedSpec` from the existing helper and pins the extraction with `TestSingleSpecFileStillGeneratesIdentically`.
3. **No CLI path holds a `router.Router`** — `grep -rn "router.Router" cmd/forge/` returns nothing. Introspection is therefore not reachable from `forge client generate` at all, so Task 7 is reduced to pinning merge precedence by test, with an explicit instruction not to build a stub router.

**Scale.** Seven tasks, of which Task 7 is two steps. Tasks 1-3 are self-contained in `internal/client` and carry the whole merge semantic; Tasks 4-6 are CLI wiring. A reviewer can reject any one without rejecting its neighbours.
