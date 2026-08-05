# Forge Web Client Phase 1: Go Declaration Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `forge client generate` emit an operation manifest (`src/ops.ts`) and one-line typed hook facades (`src/hooks.ts`) carrying entity identity, invalidation tags and stream bindings, driven by type inference plus a small set of new route options.

**Architecture:** Entity identity is inferred in Go from the response schema of each endpoint, never guessed at runtime. Route options write declarations into `RouteConfig.Metadata`; the OpenAPI and AsyncAPI generators surface them as `x-forge-*` extensions; the introspector reads them back into new IR fields; two new TypeScript emitters render them. No behaviour reaches the browser in this phase — the manifest is data that Phase 3's runtime consumes.

**Tech Stack:** Go 1.26, existing `internal/router` + `internal/client` packages, standard `testing` with table-driven tests.

## Global Constraints

- Go version floor: **1.26.0** (per `go.mod`). Generics are available and used by `Emits[T]`.
- Generated output must be **byte-identical across runs**. Every map iterated during emission must be sorted first. This is enforced by existing `determinism_test.go` conventions and extended here.
- **No `Co-Authored-By` trailers** in commit messages (user global rule).
- The IR field `Endpoint.Tags` is **already taken** by OpenAPI tags. Cache tags use `Endpoint.CacheTags`.
- `router.EntityDef.IDField` holds a **Go field name**. `client.EntityRef.IDField` holds a **JSON property name**. They are different types on purpose; do not unify them.
- Identity-shaped means: JSON property name is exactly `id` (case-insensitive), **or** the Go field carries `forge:"id"`. A field named `TenantID` serialises to `tenant_id` and therefore never matches.
- Existing public surface of `rest.ts`, `websocket.ts`, `sse.ts`, `webtransport.ts` must not change.

---

## File Structure

**Create:**
- `internal/router/client_meta.go` — declaration types: `EntityDef`, `ForgeEntity`, `StreamBinding`, `StreamIntent`, `Emits[T]`
- `internal/router/router_opts_client.go` — `RouteOption` implementations
- `client_meta.go` (repo root) — `forge.*` re-exports
- `internal/client/entity.go` + `entity_test.go` — inference
- `internal/client/tags.go` + `tags_test.go` — tag derivation
- `internal/client/generators/typescript/opsmanifest.go` + `opsmanifest_test.go` — `src/ops.ts`
- `internal/client/generators/typescript/facades.go` + `facades_test.go` — `src/hooks.ts`

**Modify:**
- `internal/client/ir.go` — new IR fields
- `internal/router/openapi_generator.go` — emit `x-forge-entity`, `x-forge-tags`
- `internal/router/openapi_request_schema.go` — emit `x-forge-id` from the struct tag
- `internal/router/asyncapi_generator.go` — emit `x-forge-stream`
- `internal/client/introspector.go` — read extensions into the IR
- `internal/client/generators/typescript/generator.go` — wire new emitters, drop `query.ts`

**Delete:**
- `internal/client/generators/typescript/query.go`, `query_internal_test.go`

---

### Task 1: IR fields for entity, tags and stream bindings

**Files:**
- Modify: `internal/client/ir.go`
- Test: `internal/client/ir_client_meta_test.go` (create)

**Interfaces:**
- Consumes: nothing (pure data)
- Produces: `client.EntityRef{Type, IDField string}`, `client.TagSet{Provides, Invalidates []string}`, `client.StreamIntent` with constants `StreamUpsert`/`StreamPatch`/`StreamEvict`, `client.StreamBinding{Message, EntityType string; Intent StreamIntent; Invalidates []string}`. New fields: `Endpoint.Entity *EntityRef`, `Endpoint.CacheTags TagSet`, `APISpec.Entities map[string]*EntityRef`, `WebSocketEndpoint.StreamBindings []StreamBinding`, `SSEEndpoint.StreamBindings []StreamBinding`, `Schema.Extensions map[string]any`.

- [ ] **Step 1: Write the failing test**

```go
// internal/client/ir_client_meta_test.go
package client

import "testing"

func TestEndpointCarriesEntityAndCacheTags(t *testing.T) {
	ep := Endpoint{
		Method: "GET",
		Path:   "/orders/{id}",
		Tags:   []string{"orders"}, // OpenAPI tags, unrelated to cache tags
		Entity: &EntityRef{Type: "Order", IDField: "id"},
		CacheTags: TagSet{
			Provides: []string{"Order:{id}"},
		},
	}

	if ep.Entity.Type != "Order" {
		t.Fatalf("Entity.Type = %q, want Order", ep.Entity.Type)
	}

	if len(ep.CacheTags.Provides) != 1 || ep.CacheTags.Provides[0] != "Order:{id}" {
		t.Fatalf("CacheTags.Provides = %v, want [Order:{id}]", ep.CacheTags.Provides)
	}

	// OpenAPI tags and cache tags must remain independent fields.
	if len(ep.Tags) != 1 || ep.Tags[0] != "orders" {
		t.Fatalf("Tags = %v, want [orders]", ep.Tags)
	}
}

func TestStreamBindingIntents(t *testing.T) {
	b := StreamBinding{
		Message:     "order.created",
		EntityType:  "Order",
		Intent:      StreamUpsert,
		Invalidates: []string{"Order[]"},
	}

	if b.Intent != "upsert" {
		t.Fatalf("StreamUpsert = %q, want upsert", b.Intent)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run 'TestEndpointCarriesEntityAndCacheTags|TestStreamBindingIntents' -v`
Expected: FAIL — `undefined: EntityRef`, `undefined: TagSet`, `undefined: StreamBinding`

- [ ] **Step 3: Add the types and fields**

Append to `internal/client/ir.go`:

```go
// EntityRef names the entity a payload carries and the JSON property that
// identifies it. Resolved in Go at generation time; the browser runtime never
// re-derives identity from a response.
type EntityRef struct {
	Type    string // typename, e.g. "Order"
	IDField string // JSON property name, e.g. "id"
}

// TagSet is one operation's invalidation contract.
type TagSet struct {
	Provides    []string
	Invalidates []string
}

// StreamIntent is what a stream message does to the cache.
type StreamIntent string

const (
	StreamUpsert StreamIntent = "upsert"
	StreamPatch  StreamIntent = "patch"
	StreamEvict  StreamIntent = "evict"
)

// StreamBinding binds one channel message to an entity type.
type StreamBinding struct {
	Message     string
	EntityType  string
	Intent      StreamIntent
	Invalidates []string
}
```

Add these fields to the existing structs in the same file:

```go
// In Endpoint, after Metadata:
	Entity    *EntityRef
	CacheTags TagSet

// In APISpec, after Schemas:
	Entities map[string]*EntityRef

// In WebSocketEndpoint, after Metadata:
	StreamBindings []StreamBinding

// In SSEEndpoint, after Metadata:
	StreamBindings []StreamBinding

// In Schema, after AdditionalProperties:
	Extensions map[string]any
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run 'TestEndpointCarriesEntityAndCacheTags|TestStreamBindingIntents' -v`
Expected: PASS

- [ ] **Step 5: Verify nothing else broke**

Run: `go build ./... && go test ./internal/client/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/client/ir.go internal/client/ir_client_meta_test.go
git commit -m "feat(client): IR fields for entity identity, cache tags and stream bindings"
```

---

### Task 2: Entity inference from response schemas

**Files:**
- Create: `internal/client/entity.go`
- Test: `internal/client/entity_test.go`

**Interfaces:**
- Consumes: `client.EntityRef`, `client.Schema` (Task 1)
- Produces: `func InferEntity(name string, schema *Schema) *EntityRef`

- [ ] **Step 1: Write the failing test**

```go
// internal/client/entity_test.go
package client

import "testing"

func strSchema() *Schema { return &Schema{Type: "string"} }

func TestInferEntity(t *testing.T) {
	tests := []struct {
		name    string
		typeNm  string
		schema  *Schema
		want    *EntityRef
	}{
		{
			name:   "object with id is an entity",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"id":    strSchema(),
				"total": {Type: "integer"},
			}},
			want: &EntityRef{Type: "Order", IDField: "id"},
		},
		{
			name:   "integer id is accepted",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"id": {Type: "integer"},
			}},
			want: &EntityRef{Type: "Order", IDField: "id"},
		},
		{
			name:   "tenant_id alone is not identity",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"tenant_id": strSchema(),
			}},
			want: nil,
		},
		{
			name:   "forge id extension wins over name",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"order_number": {Type: "string", Extensions: map[string]any{"x-forge-id": true}},
			}},
			want: &EntityRef{Type: "Order", IDField: "order_number"},
		},
		{
			name:   "two identity fields refuse inference",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"id":           strSchema(),
				"order_number": {Type: "string", Extensions: map[string]any{"x-forge-id": true}},
			}},
			want: nil,
		},
		{
			name:   "object id is not identity-shaped",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"id": {Type: "object"},
			}},
			want: nil,
		},
		{
			name:   "unnamed schema is never an entity",
			typeNm: "",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{"id": strSchema()}},
			want:   nil,
		},
		{
			name:   "non-object is never an entity",
			typeNm: "Order",
			schema: &Schema{Type: "array"},
			want:   nil,
		},
		{
			name:   "nil schema is safe",
			typeNm: "Order",
			schema: nil,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferEntity(tt.typeNm, tt.schema)

			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("InferEntity = %+v, want nil", got)
			case tt.want != nil && got == nil:
				t.Fatalf("InferEntity = nil, want %+v", tt.want)
			case tt.want != nil && (*got != *tt.want):
				t.Fatalf("InferEntity = %+v, want %+v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run TestInferEntity -v`
Expected: FAIL — `undefined: InferEntity`

- [ ] **Step 3: Write the implementation**

```go
// internal/client/entity.go
package client

import "strings"

// InferEntity reports how a named schema is identified, or nil when the schema
// is not an entity.
//
// Refusing is the important half. A schema carrying two identity-shaped fields
// is ambiguous, and picking one collides two records under a single cache key.
// Where that second field is a tenant discriminator the result is a data leak
// wearing a caching bug's clothes, so ambiguity returns nil and the developer
// declares the identity explicitly.
func InferEntity(name string, schema *Schema) *EntityRef {
	if name == "" || schema == nil || schema.Type != "object" {
		return nil
	}

	found := ""

	for prop, ps := range schema.Properties {
		if !isIdentityField(prop, ps) {
			continue
		}

		if found != "" {
			return nil // ambiguous
		}

		found = prop
	}

	if found == "" {
		return nil
	}

	return &EntityRef{Type: name, IDField: found}
}

// isIdentityField reports whether a property identifies its containing object.
//
// The name test is exact rather than suffixed: `tenant_id` ends in "id" but
// identifies a tenant, not this record.
func isIdentityField(prop string, s *Schema) bool {
	if s == nil || !isIdentityType(s) {
		return false
	}

	if v, ok := s.Extensions["x-forge-id"].(bool); ok && v {
		return true
	}

	return strings.EqualFold(prop, "id")
}

// isIdentityType reports whether a schema can serve as a cache key component.
func isIdentityType(s *Schema) bool {
	return s.Type == "string" || s.Type == "integer"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run TestInferEntity -v`
Expected: PASS (9 subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/client/entity.go internal/client/entity_test.go
git commit -m "feat(client): infer entity identity from response schemas"
```

---

### Task 3: Tag derivation

**Files:**
- Create: `internal/client/tags.go`
- Test: `internal/client/tags_test.go`

**Interfaces:**
- Consumes: `client.EntityRef`, `client.TagSet` (Task 1)
- Produces: `func DeriveTags(method string, entity *EntityRef, isList bool) TagSet`, `func ApplyTagOverrides(base TagSet, extra, suppressed []string) TagSet`

- [ ] **Step 1: Write the failing test**

```go
// internal/client/tags_test.go
package client

import (
	"reflect"
	"testing"
)

func TestDeriveTags(t *testing.T) {
	order := &EntityRef{Type: "Order", IDField: "id"}

	tests := []struct {
		name   string
		method string
		entity *EntityRef
		isList bool
		want   TagSet
	}{
		{
			name: "get one provides the item", method: "GET", entity: order,
			want: TagSet{Provides: []string{"Order:{id}"}},
		},
		{
			name: "get list provides item and collection", method: "GET", entity: order, isList: true,
			want: TagSet{Provides: []string{"Order:{id}", "Order[]"}},
		},
		{
			name: "post provides item and invalidates collection", method: "POST", entity: order,
			want: TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}},
		},
		{
			name: "patch invalidates the collection too", method: "PATCH", entity: order,
			want: TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}},
		},
		{
			name: "delete only invalidates", method: "DELETE", entity: order,
			want: TagSet{Invalidates: []string{"Order[]"}},
		},
		{
			name: "no entity means no tags", method: "POST", entity: nil,
			want: TagSet{},
		},
		{
			name: "method case is normalised", method: "post", entity: order,
			want: TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveTags(tt.method, tt.entity, tt.isList)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("DeriveTags = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestApplyTagOverrides(t *testing.T) {
	base := TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}}

	got := ApplyTagOverrides(base, []string{"Inventory[]"}, []string{"Order[]"})

	want := TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Inventory[]"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ApplyTagOverrides = %+v, want %+v", got, want)
	}
}

// Output order must not depend on map iteration; generated files are diffed in CI.
func TestApplyTagOverridesIsSorted(t *testing.T) {
	got := ApplyTagOverrides(TagSet{}, []string{"Zebra[]", "Alpha[]", "Middle[]"}, nil)

	want := []string{"Alpha[]", "Middle[]", "Zebra[]"}
	if !reflect.DeepEqual(got.Invalidates, want) {
		t.Fatalf("Invalidates = %v, want %v", got.Invalidates, want)
	}
}

func TestApplyTagOverridesDeduplicates(t *testing.T) {
	base := TagSet{Invalidates: []string{"Order[]"}}

	got := ApplyTagOverrides(base, []string{"Order[]"}, nil)

	if len(got.Invalidates) != 1 {
		t.Fatalf("Invalidates = %v, want one entry", got.Invalidates)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run 'TestDeriveTags|TestApplyTagOverrides' -v`
Expected: FAIL — `undefined: DeriveTags`

- [ ] **Step 3: Write the implementation**

```go
// internal/client/tags.go
package client

import (
	"sort"
	"strings"
)

// DeriveTags computes an operation's invalidation contract from its method and
// the entity it touches.
//
// Every non-GET invalidates the collection, PATCH included. A patch only
// changes list membership when it touches a filtered field, and the server
// cannot know which lists a browser has mounted. Over-refetching is a
// performance defect a profiler finds; under-refetching is a stale row a user
// reports three weeks later. The default is correct and the escape is explicit.
func DeriveTags(method string, entity *EntityRef, isList bool) TagSet {
	if entity == nil {
		return TagSet{}
	}

	item := entity.Type + ":{" + entity.IDField + "}"
	collection := entity.Type + "[]"

	switch strings.ToUpper(method) {
	case "GET", "HEAD":
		provides := []string{item}
		if isList {
			provides = append(provides, collection)
		}

		return TagSet{Provides: provides}

	case "DELETE":
		return TagSet{Invalidates: []string{collection}}

	default:
		return TagSet{Provides: []string{item}, Invalidates: []string{collection}}
	}
}

// ApplyTagOverrides folds route-declared additions and suppressions into a
// derived contract. Output is sorted and deduplicated so generated files do not
// churn between runs.
func ApplyTagOverrides(base TagSet, extra, suppressed []string) TagSet {
	drop := make(map[string]bool, len(suppressed))
	for _, s := range suppressed {
		drop[s] = true
	}

	return TagSet{
		Provides:    normalizeTags(base.Provides, nil, drop),
		Invalidates: normalizeTags(base.Invalidates, extra, drop),
	}
}

// normalizeTags merges, removes suppressed entries, deduplicates and sorts.
// Returns nil rather than an empty slice so reflect.DeepEqual against a zero
// TagSet behaves as a reader expects.
func normalizeTags(base, extra []string, drop map[string]bool) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))

	for _, tag := range append(append([]string{}, base...), extra...) {
		if drop[tag] || seen[tag] {
			continue
		}

		seen[tag] = true

		out = append(out, tag)
	}

	if len(out) == 0 {
		return nil
	}

	sort.Strings(out)

	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run 'TestDeriveTags|TestApplyTagOverrides' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/client/tags.go internal/client/tags_test.go
git commit -m "feat(client): derive invalidation tags from method and entity"
```

---

### Task 4: Declaration types and the Emits builder

**Files:**
- Create: `internal/router/client_meta.go`
- Test: `internal/router/client_meta_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `router.EntityDef{Type, IDField string}`, `router.ForgeEntity` interface with method `ForgeEntity() EntityDef`, `router.StreamIntent` + constants `StreamUpsert`/`StreamPatch`/`StreamEvict`, `router.StreamBinding{Message, EntityType string; Intent StreamIntent; Invalidates []string}`, `func Emits[T any](message string) *EmitsBuilder`, `(*EmitsBuilder).As(StreamIntent) *EmitsBuilder`, `(*EmitsBuilder).Invalidates(...string) *EmitsBuilder`, `(*EmitsBuilder).Build() StreamBinding`

- [ ] **Step 1: Write the failing test**

```go
// internal/router/client_meta_test.go
package router

import "testing"

type testOrder struct {
	ID string `json:"id"`
}

func TestEmitsInfersEntityTypeAndIntent(t *testing.T) {
	tests := []struct {
		message string
		want    StreamIntent
	}{
		{"order.created", StreamUpsert},
		{"order.updated", StreamPatch},
		{"order.changed", StreamPatch},
		{"order.deleted", StreamEvict},
		{"order.removed", StreamEvict},
		{"order.fulfilled", StreamPatch}, // unrecognised suffix falls back to patch
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			b := Emits[testOrder](tt.message).Build()

			if b.EntityType != "testOrder" {
				t.Fatalf("EntityType = %q, want testOrder", b.EntityType)
			}

			if b.Intent != tt.want {
				t.Fatalf("Intent = %q, want %q", b.Intent, tt.want)
			}
		})
	}
}

func TestEmitsCreatedInvalidatesCollection(t *testing.T) {
	b := Emits[testOrder]("order.created").Build()

	if len(b.Invalidates) != 1 || b.Invalidates[0] != "testOrder[]" {
		t.Fatalf("Invalidates = %v, want [testOrder[]]", b.Invalidates)
	}
}

func TestEmitsUpdatedInvalidatesNothing(t *testing.T) {
	b := Emits[testOrder]("order.updated").Build()

	if len(b.Invalidates) != 0 {
		t.Fatalf("Invalidates = %v, want empty: a patch needs no refetch", b.Invalidates)
	}
}

func TestEmitsExplicitOverrides(t *testing.T) {
	b := Emits[testOrder]("order.fulfilled").
		As(StreamPatch).
		Invalidates("Shipment[]").
		Build()

	if b.Intent != StreamPatch {
		t.Fatalf("Intent = %q, want patch", b.Intent)
	}

	if len(b.Invalidates) != 1 || b.Invalidates[0] != "Shipment[]" {
		t.Fatalf("Invalidates = %v, want [Shipment[]]", b.Invalidates)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/router/ -run TestEmits -v`
Expected: FAIL — `undefined: Emits`

- [ ] **Step 3: Write the implementation**

```go
// internal/router/client_meta.go
package router

import (
	"reflect"
	"strings"
)

// EntityDef declares how a type is identified in a client-side normalized cache.
// IDField is the Go field name; the schema generator resolves it to a JSON
// property name.
type EntityDef struct {
	Type    string
	IDField string
}

// ForgeEntity is implemented by types that override entity inference. Reach for
// it when a type has no field named `id`, or when two fields are both
// identity-shaped and inference therefore refuses to guess.
type ForgeEntity interface {
	ForgeEntity() EntityDef
}

// StreamIntent is what a stream message does to the cache.
type StreamIntent string

const (
	StreamUpsert StreamIntent = "upsert"
	StreamPatch  StreamIntent = "patch"
	StreamEvict  StreamIntent = "evict"
)

// StreamBinding binds one channel message to an entity type.
type StreamBinding struct {
	Message     string
	EntityType  string
	Intent      StreamIntent
	Invalidates []string
}

// EmitsBuilder accumulates one binding. Build resolves the defaults.
type EmitsBuilder struct {
	binding      StreamBinding
	intentSet    bool
	invalidesSet bool
}

// Emits declares that a channel emits `message` carrying entity T.
//
// Intent is inferred from the message-name suffix, so the common three-message
// channel needs no further configuration:
//
//	forge.Emits[Order]("order.created")
//	forge.Emits[Order]("order.updated")
//	forge.Emits[Order]("order.deleted")
func Emits[T any](message string) *EmitsBuilder {
	return &EmitsBuilder{
		binding: StreamBinding{
			Message:    message,
			EntityType: reflect.TypeOf((*T)(nil)).Elem().Name(),
		},
	}
}

// As overrides the inferred intent for messages outside the naming convention.
func (e *EmitsBuilder) As(intent StreamIntent) *EmitsBuilder {
	e.binding.Intent = intent
	e.intentSet = true

	return e
}

// Invalidates overrides the inferred tag invalidations.
func (e *EmitsBuilder) Invalidates(tags ...string) *EmitsBuilder {
	e.binding.Invalidates = tags
	e.invalidesSet = true

	return e
}

// Build resolves defaults and returns the binding.
func (e *EmitsBuilder) Build() StreamBinding {
	out := e.binding

	if !e.intentSet {
		out.Intent = intentFromMessage(out.Message)
	}

	if !e.invalidesSet {
		// A patch reaches every view through the entity store, so only
		// membership changes need the collection refetched.
		if out.Intent != StreamPatch && out.EntityType != "" {
			out.Invalidates = []string{out.EntityType + "[]"}
		}
	}

	return out
}

// intentFromMessage reads intent from the message-name suffix, defaulting to a
// patch because merging a payload is the safe reading of an unrecognised name:
// it updates what is already cached without inventing or destroying membership.
func intentFromMessage(message string) StreamIntent {
	suffix := message
	if i := strings.LastIndex(message, "."); i >= 0 {
		suffix = message[i+1:]
	}

	switch strings.ToLower(suffix) {
	case "created", "added":
		return StreamUpsert
	case "deleted", "removed":
		return StreamEvict
	default:
		return StreamPatch
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/router/ -run TestEmits -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/router/client_meta.go internal/router/client_meta_test.go
git commit -m "feat(router): entity and stream binding declaration types"
```

---

### Task 5: Route options

**Files:**
- Create: `internal/router/router_opts_client.go`
- Test: `internal/router/router_opts_client_test.go`

**Interfaces:**
- Consumes: Task 4's types; existing `RouteOption`/`RouteConfig` from `internal/router/router.go:87` and `:108`
- Produces: `WithEntity(EntityDef) RouteOption`, `WithoutEntity() RouteOption`, `WithInvalidates(...string) RouteOption`, `WithoutInvalidation(...string) RouteOption`, `WithStreamBinding(...*EmitsBuilder) RouteOption`. Metadata keys: `forge.client.entity`, `forge.client.noEntity`, `forge.client.invalidates`, `forge.client.noInvalidation`, `forge.client.streamBindings`.

- [ ] **Step 1: Write the failing test**

```go
// internal/router/router_opts_client_test.go
package router

import "testing"

func applyOpts(opts ...RouteOption) *RouteConfig {
	cfg := &RouteConfig{}
	for _, o := range opts {
		o.Apply(cfg)
	}

	return cfg
}

func TestWithEntityStoresDefinition(t *testing.T) {
	cfg := applyOpts(WithEntity(EntityDef{Type: "Order", IDField: "OrderNumber"}))

	def, ok := cfg.Metadata["forge.client.entity"].(EntityDef)
	if !ok {
		t.Fatalf("metadata missing entity, got %#v", cfg.Metadata)
	}

	if def.IDField != "OrderNumber" {
		t.Fatalf("IDField = %q, want OrderNumber", def.IDField)
	}
}

func TestWithoutEntityStoresFlag(t *testing.T) {
	cfg := applyOpts(WithoutEntity())

	if v, _ := cfg.Metadata["forge.client.noEntity"].(bool); !v {
		t.Fatalf("noEntity = %#v, want true", cfg.Metadata["forge.client.noEntity"])
	}
}

func TestWithInvalidatesAccumulates(t *testing.T) {
	cfg := applyOpts(
		WithInvalidates("Inventory[]"),
		WithInvalidates("Customer:{req.customerId}"),
	)

	tags, _ := cfg.Metadata["forge.client.invalidates"].([]string)
	if len(tags) != 2 {
		t.Fatalf("invalidates = %v, want two entries", tags)
	}
}

func TestWithoutInvalidationAccumulates(t *testing.T) {
	cfg := applyOpts(WithoutInvalidation("Order[]"))

	tags, _ := cfg.Metadata["forge.client.noInvalidation"].([]string)
	if len(tags) != 1 || tags[0] != "Order[]" {
		t.Fatalf("noInvalidation = %v, want [Order[]]", tags)
	}
}

func TestWithStreamBindingBuildsBindings(t *testing.T) {
	cfg := applyOpts(WithStreamBinding(
		Emits[testOrder]("order.created"),
		Emits[testOrder]("order.updated"),
	))

	bindings, _ := cfg.Metadata["forge.client.streamBindings"].([]StreamBinding)
	if len(bindings) != 2 {
		t.Fatalf("bindings = %v, want two", bindings)
	}

	if bindings[0].Intent != StreamUpsert || bindings[1].Intent != StreamPatch {
		t.Fatalf("intents = %q/%q, want upsert/patch", bindings[0].Intent, bindings[1].Intent)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/router/ -run 'TestWith(Entity|outEntity|Invalidates|outInvalidation|StreamBinding)' -v`
Expected: FAIL — `undefined: WithEntity`

- [ ] **Step 3: Write the implementation**

Follow the existing `metadataOpt` pattern at `internal/router/router_opts.go:46`.

```go
// internal/router/router_opts_client.go
package router

// setMeta writes one client-generation key, allocating the map on first use.
func setMeta(cfg *RouteConfig, key string, value any) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	cfg.Metadata[key] = value
}

// appendMeta appends to a []string metadata key so repeated options accumulate
// rather than the last one silently winning.
func appendMeta(cfg *RouteConfig, key string, values []string) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	existing, _ := cfg.Metadata[key].([]string)
	cfg.Metadata[key] = append(existing, values...)
}

type entityOpt struct{ def EntityDef }

func (o *entityOpt) Apply(cfg *RouteConfig) { setMeta(cfg, "forge.client.entity", o.def) }

// WithEntity overrides inferred identity for this endpoint's response.
//
// Prefer implementing ForgeEntity on the type: identity is intrinsic to a type,
// and declaring it per route repeats it on every endpoint returning an Order.
// This option exists for types you cannot add a method to.
func WithEntity(def EntityDef) RouteOption { return &entityOpt{def} }

type noEntityOpt struct{}

func (o *noEntityOpt) Apply(cfg *RouteConfig) { setMeta(cfg, "forge.client.noEntity", true) }

// WithoutEntity keeps this endpoint's response out of the normalized store.
// Use it for projections and snapshots that must not merge with the canonical
// record.
func WithoutEntity() RouteOption { return &noEntityOpt{} }

type invalidatesOpt struct{ tags []string }

func (o *invalidatesOpt) Apply(cfg *RouteConfig) {
	appendMeta(cfg, "forge.client.invalidates", o.tags)
}

// WithInvalidates declares cross-entity effects. Same-entity invalidation is
// derived, so this is only for edges a reader would not predict.
func WithInvalidates(tags ...string) RouteOption { return &invalidatesOpt{tags} }

type noInvalidationOpt struct{ tags []string }

func (o *noInvalidationOpt) Apply(cfg *RouteConfig) {
	appendMeta(cfg, "forge.client.noInvalidation", o.tags)
}

// WithoutInvalidation suppresses a derived invalidation for endpoints that
// cannot change list membership.
func WithoutInvalidation(tags ...string) RouteOption { return &noInvalidationOpt{tags} }

type streamBindingOpt struct{ builders []*EmitsBuilder }

func (o *streamBindingOpt) Apply(cfg *RouteConfig) {
	bindings := make([]StreamBinding, 0, len(o.builders))
	for _, b := range o.builders {
		bindings = append(bindings, b.Build())
	}

	setMeta(cfg, "forge.client.streamBindings", bindings)
}

// WithStreamBinding declares which entity updates a channel emits.
func WithStreamBinding(builders ...*EmitsBuilder) RouteOption {
	return &streamBindingOpt{builders}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/router/ -run 'TestWith(Entity|outEntity|Invalidates|outInvalidation|StreamBinding)' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/router/router_opts_client.go internal/router/router_opts_client_test.go
git commit -m "feat(router): route options for entity, tags and stream binding"
```

---

### Task 6: forge package re-exports

**Files:**
- Create: `client_meta.go` (repo root, package `forge`)
- Test: `client_meta_test.go` (repo root)

**Interfaces:**
- Consumes: Tasks 4 and 5
- Produces: `forge.EntityDef`, `forge.ForgeEntity`, `forge.StreamBinding`, `forge.StreamIntent`, `forge.StreamUpsert`/`StreamPatch`/`StreamEvict`, `forge.Emits[T]`, `forge.WithEntity`, `forge.WithoutEntity`, `forge.WithInvalidates`, `forge.WithoutInvalidation`, `forge.WithStreamBinding`

- [ ] **Step 1: Write the failing test**

```go
// client_meta_test.go
package forge

import "testing"

type reexportOrder struct {
	ID string `json:"id"`
}

func TestClientMetaReExports(t *testing.T) {
	// Compiling is most of the assertion: these are the names users type.
	_ = WithEntity(EntityDef{Type: "Order", IDField: "ID"})
	_ = WithoutEntity()
	_ = WithInvalidates("Inventory[]")
	_ = WithoutInvalidation("Order[]")
	_ = WithStreamBinding(Emits[reexportOrder]("order.created"))

	if StreamUpsert != "upsert" {
		t.Fatalf("StreamUpsert = %q, want upsert", StreamUpsert)
	}

	b := Emits[reexportOrder]("order.deleted").Build()
	if b.Intent != StreamEvict {
		t.Fatalf("Intent = %q, want evict", b.Intent)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestClientMetaReExports -v`
Expected: FAIL — `undefined: WithEntity`

- [ ] **Step 3: Write the implementation**

Match the aliasing style already used at `router.go:26-45`.

```go
// client_meta.go
package forge

import "github.com/xraph/forge/internal/router"

// EntityDef declares how a type is identified in a client-side normalized cache.
type EntityDef = router.EntityDef

// ForgeEntity is implemented by types that override entity inference.
type ForgeEntity = router.ForgeEntity

// StreamIntent is what a stream message does to the cache.
type StreamIntent = router.StreamIntent

// StreamBinding binds one channel message to an entity type.
type StreamBinding = router.StreamBinding

// EmitsBuilder accumulates one stream binding.
type EmitsBuilder = router.EmitsBuilder

const (
	StreamUpsert = router.StreamUpsert
	StreamPatch  = router.StreamPatch
	StreamEvict  = router.StreamEvict
)

// Emits declares that a channel emits `message` carrying entity T.
//
// Example:
//
//	router.WebSocket("/ws/orders", handler,
//	    forge.WithStreamBinding(
//	        forge.Emits[Order]("order.created"),
//	        forge.Emits[Order]("order.updated"),
//	        forge.Emits[Order]("order.deleted"),
//	    ),
//	)
func Emits[T any](message string) *EmitsBuilder { return router.Emits[T](message) }

// WithEntity overrides inferred identity for this endpoint's response.
func WithEntity(def EntityDef) RouteOption { return router.WithEntity(def) }

// WithoutEntity keeps this endpoint's response out of the normalized store.
//
// Example:
//
//	router.GET("/orders/{id}/audit-snapshot", h, forge.WithoutEntity())
func WithoutEntity() RouteOption { return router.WithoutEntity() }

// WithInvalidates declares cross-entity invalidation effects.
//
// Example:
//
//	router.POST("/orders", createOrder,
//	    forge.WithInvalidates("Inventory[]", "Customer:{req.customerId}"),
//	)
func WithInvalidates(tags ...string) RouteOption { return router.WithInvalidates(tags...) }

// WithoutInvalidation suppresses a derived invalidation.
func WithoutInvalidation(tags ...string) RouteOption {
	return router.WithoutInvalidation(tags...)
}

// WithStreamBinding declares which entity updates a channel emits.
func WithStreamBinding(builders ...*EmitsBuilder) RouteOption {
	return router.WithStreamBinding(builders...)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestClientMetaReExports -v && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add client_meta.go client_meta_test.go
git commit -m "feat(forge): re-export client generation route options"
```

---

### Task 7a: Inline `x-*` extension marshalling on shared spec types

**Inserted during execution.** This plan assumed `shared.Schema` and `shared.Operation` could
carry extensions that reach the serialized document. They could not: `shared.Schema` had no
`Extensions` field, `shared.AsyncAPISpec` had one tagged `json:"-"`, and no custom
`MarshalJSON` existed anywhere in `internal/shared/`. Without this task, `forge client
generate` against a checked-in `openapi.json` would emit a client silently missing all entity
metadata — and spec files being the contract is what lets generation run in CI and in frontend
repos that cannot import the Go module.

Full task text: `.superpowers/sdd/2026-08-03-web-client-phase1-go-declarations/task-7a-brief.md`

Summary: `Extensions map[string]any` plus `MarshalJSON`/`UnmarshalJSON` on `shared.Schema`,
`shared.Operation` and `shared.AsyncAPIChannel`, hoisting `x-`-prefixed keys to the object's
top level. Marshalling goes through a method-shedding local type alias rather than
hand-enumerated fields, so a field added later cannot be silently dropped. An extension-free
object returns early and marshals byte-identically to before, because merging through a map
reorders keys and would otherwise produce a spurious CI diff on every run.

**Known limitation, deliberately not fixed here:** these methods govern JSON only.
`gopkg.in/yaml.v3` does not consult `MarshalJSON`, so extensions do not round-trip through
YAML spec files. Scheduled for Plan 2.

---

### Task 7: Emit `x-forge-id` from the struct tag

**Files:**
- Modify: `internal/router/openapi_request_schema.go`
- Test: `internal/router/openapi_forge_id_test.go` (create)

**Interfaces:**
- Consumes: nothing from prior tasks
- Produces: property-level `x-forge-id: true` in generated OpenAPI schemas, consumed by Task 9

**Context for the implementer:** the schema builder already reads custom struct tags — `json`, `query`, `header`, `required`, `optional`, `default` (see `internal/router/openapi_params.go:91-128` and `internal/router/asyncapi_schema.go:289-340` for the established pattern). Adding `forge:"id"` follows that convention rather than introducing a new mechanism. Locate the function that converts a struct field into a property schema and set the extension there.

- [ ] **Step 1: Write the failing test**

```go
// internal/router/openapi_forge_id_test.go
package router

import "testing"

type taggedOrder struct {
	OrderNumber string `json:"order_number" forge:"id"`
	Total       int    `json:"total"`
}

func TestForgeIDTagBecomesExtension(t *testing.T) {
	schema := generateSchemaFromType(reflectTypeOf(taggedOrder{}))

	prop, ok := schema.Properties["order_number"]
	if !ok {
		t.Fatalf("order_number missing from properties: %#v", schema.Properties)
	}

	if v, _ := prop.Extensions["x-forge-id"].(bool); !v {
		t.Fatalf("x-forge-id = %#v, want true", prop.Extensions["x-forge-id"])
	}

	if _, present := schema.Properties["total"].Extensions["x-forge-id"]; present {
		t.Fatal("x-forge-id must not be set on untagged fields")
	}
}
```

**Implementer note:** replace `generateSchemaFromType`/`reflectTypeOf` with whatever the actual entry point is in this package. Find it with:

```bash
grep -rn "func.*[Ss]chemaFrom\|func.*[Ss]tructTo" internal/router/*.go | grep -v _test
```

If `shared.Schema` has no `Extensions` field yet, add `Extensions map[string]any` to it in the same task, mirroring the field added to `client.Schema` in Task 1.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/router/ -run TestForgeIDTagBecomesExtension -v`
Expected: FAIL — extension absent

- [ ] **Step 3: Write the implementation**

In the struct-field-to-property conversion, after the JSON name is resolved:

```go
	if field.Tag.Get("forge") == "id" {
		if propSchema.Extensions == nil {
			propSchema.Extensions = make(map[string]any)
		}

		propSchema.Extensions["x-forge-id"] = true
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/router/ -run TestForgeIDTagBecomesExtension -v && go test ./internal/router/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/router/openapi_request_schema.go internal/router/openapi_forge_id_test.go
git commit -m "feat(router): surface the forge id struct tag as x-forge-id"
```

---

### Task 8: Emit route declarations as OpenAPI and AsyncAPI extensions

**Files:**
- Modify: `internal/router/openapi_generator.go` (near the metadata handling at `:250-260`)
- Modify: `internal/router/asyncapi_generator.go`
- Test: `internal/router/openapi_client_meta_test.go` (create)

**Interfaces:**
- Consumes: metadata keys from Task 5
- Produces: operation-level `x-forge-entity` (object with `type`, `idField`), `x-forge-no-entity` (bool), `x-forge-invalidates` (string array), `x-forge-no-invalidation` (string array); channel-level `x-forge-stream` (array of objects with `message`, `entityType`, `intent`, `invalidates`)

- [ ] **Step 1: Write the failing test**

```go
// internal/router/openapi_client_meta_test.go
package router

import "testing"

func TestOperationCarriesForgeExtensions(t *testing.T) {
	route := RouteInfo{
		Method: "POST",
		Path:   "/orders",
		Metadata: map[string]any{
			"forge.client.entity":         EntityDef{Type: "Order", IDField: "OrderNumber"},
			"forge.client.invalidates":    []string{"Inventory[]"},
			"forge.client.noInvalidation": []string{"Order[]"},
		},
	}

	op := &Operation{}
	applyForgeExtensions(op, route.Metadata)

	ent, ok := op.Extensions["x-forge-entity"].(map[string]any)
	if !ok {
		t.Fatalf("x-forge-entity missing: %#v", op.Extensions)
	}

	if ent["idField"] != "OrderNumber" {
		t.Fatalf("idField = %v, want OrderNumber", ent["idField"])
	}

	inv, _ := op.Extensions["x-forge-invalidates"].([]string)
	if len(inv) != 1 || inv[0] != "Inventory[]" {
		t.Fatalf("x-forge-invalidates = %v, want [Inventory[]]", inv)
	}

	sup, _ := op.Extensions["x-forge-no-invalidation"].([]string)
	if len(sup) != 1 || sup[0] != "Order[]" {
		t.Fatalf("x-forge-no-invalidation = %v, want [Order[]]", sup)
	}
}

func TestOperationWithoutForgeMetadataGetsNoExtensions(t *testing.T) {
	op := &Operation{}
	applyForgeExtensions(op, map[string]any{"unrelated": true})

	for key := range op.Extensions {
		if len(key) > 8 && key[:8] == "x-forge-" {
			t.Fatalf("unexpected extension %q on a route that declared nothing", key)
		}
	}
}

func TestNoEntityFlagIsEmitted(t *testing.T) {
	op := &Operation{}
	applyForgeExtensions(op, map[string]any{"forge.client.noEntity": true})

	if v, _ := op.Extensions["x-forge-no-entity"].(bool); !v {
		t.Fatalf("x-forge-no-entity = %#v, want true", op.Extensions["x-forge-no-entity"])
	}
}
```

**Implementer note:** if `Operation` has no `Extensions map[string]any` field, add it and ensure it is marshalled inline (the OpenAPI spec requires `x-` keys at the object's top level, not nested under an `extensions` key). Check how existing extensions are serialised first:

```bash
grep -rn "x-" internal/router/openapi.go | head
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/router/ -run 'TestOperationCarriesForgeExtensions|TestOperationWithoutForgeMetadata|TestNoEntityFlag' -v`
Expected: FAIL — `undefined: applyForgeExtensions`

- [ ] **Step 3: Write the implementation**

Add to `internal/router/openapi_generator.go`:

```go
// applyForgeExtensions copies client-generation declarations from route
// metadata onto an operation as x-forge-* extensions.
//
// Routes that declared nothing get no extensions at all, so a spec generated by
// an application that never opts in is unchanged from today's output.
func applyForgeExtensions(op *Operation, metadata map[string]any) {
	if metadata == nil {
		return
	}

	set := func(key string, value any) {
		if op.Extensions == nil {
			op.Extensions = make(map[string]any)
		}

		op.Extensions[key] = value
	}

	if def, ok := metadata["forge.client.entity"].(EntityDef); ok {
		set("x-forge-entity", map[string]any{
			"type":    def.Type,
			"idField": def.IDField,
		})
	}

	if v, ok := metadata["forge.client.noEntity"].(bool); ok && v {
		set("x-forge-no-entity", true)
	}

	if tags, ok := metadata["forge.client.invalidates"].([]string); ok && len(tags) > 0 {
		set("x-forge-invalidates", tags)
	}

	if tags, ok := metadata["forge.client.noInvalidation"].([]string); ok && len(tags) > 0 {
		set("x-forge-no-invalidation", tags)
	}
}
```

Call it from `buildOperation` alongside the existing `deprecated` handling near `internal/router/openapi_generator.go:253`:

```go
	applyForgeExtensions(operation, route.Metadata)
```

Add the AsyncAPI counterpart in `internal/router/asyncapi_generator.go`, called where a channel is built:

```go
// applyForgeStreamExtension copies stream bindings onto an AsyncAPI channel.
func applyForgeStreamExtension(channel *AsyncAPIChannel, metadata map[string]any) {
	bindings, ok := metadata["forge.client.streamBindings"].([]StreamBinding)
	if !ok || len(bindings) == 0 {
		return
	}

	out := make([]map[string]any, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, map[string]any{
			"message":     b.Message,
			"entityType":  b.EntityType,
			"intent":      string(b.Intent),
			"invalidates": b.Invalidates,
		})
	}

	if channel.Extensions == nil {
		channel.Extensions = make(map[string]any)
	}

	channel.Extensions["x-forge-stream"] = out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/router/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/router/openapi_generator.go internal/router/asyncapi_generator.go internal/router/openapi_client_meta_test.go
git commit -m "feat(router): emit client generation declarations as x-forge extensions"
```

---

### Task 9: Introspector resolves entities, tags and bindings into the IR

**Files:**
- Modify: `internal/client/introspector.go` (`operationToEndpoint` at `:504`, `convertSchema` at `:698`, `channelToWebSocket` at `:608`, `channelToSSE` at `:649`)
- Test: `internal/client/introspector_client_meta_test.go` (create)

**Interfaces:**
- Consumes: `InferEntity` (Task 2), `DeriveTags`/`ApplyTagOverrides` (Task 3), extensions from Tasks 7 and 8
- Produces: populated `Endpoint.Entity`, `Endpoint.CacheTags`, `APISpec.Entities`, `WebSocketEndpoint.StreamBindings`, `SSEEndpoint.StreamBindings`, `Schema.Extensions`

**Resolution order for an endpoint's entity — implement exactly this:**
1. `x-forge-no-entity` present and true → `Entity` is nil, `CacheTags` is zero. Stop.
2. `x-forge-entity` present → use it directly.
3. Otherwise run `InferEntity` against the resolved 2xx response schema (dereferencing `$ref` via `APISpec.ResolveSchemaRef`, `ir.go:460`). If the response is an array, infer from its item schema and set `isList`.

- [ ] **Step 1: Write the failing test**

```go
// internal/client/introspector_client_meta_test.go
package client

import "testing"

func orderSchema() *Schema {
	return &Schema{Type: "object", Properties: map[string]*Schema{
		"id":    {Type: "string"},
		"total": {Type: "integer"},
	}}
}

func TestResolveEntityInfersFromResponse(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders/{id}",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, nil)

	if ep.Entity == nil || ep.Entity.Type != "Order" {
		t.Fatalf("Entity = %+v, want Order", ep.Entity)
	}

	if len(ep.CacheTags.Provides) != 1 || ep.CacheTags.Provides[0] != "Order:{id}" {
		t.Fatalf("Provides = %v, want [Order:{id}]", ep.CacheTags.Provides)
	}
}

func TestResolveEntityDetectsListResponses(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{
				Type:  "array",
				Items: &Schema{Ref: "#/components/schemas/Order"},
			}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, nil)

	if len(ep.CacheTags.Provides) != 2 {
		t.Fatalf("Provides = %v, want item and collection", ep.CacheTags.Provides)
	}
}

func TestResolveEntityHonoursNoEntity(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders/{id}/snapshot",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{"x-forge-no-entity": true})

	if ep.Entity != nil {
		t.Fatalf("Entity = %+v, want nil", ep.Entity)
	}

	if ep.CacheTags.Provides != nil || ep.CacheTags.Invalidates != nil {
		t.Fatalf("CacheTags = %+v, want zero", ep.CacheTags)
	}
}

func TestResolveEntityAppliesOverrides(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "POST", Path: "/orders",
		Responses: map[int]*Response{201: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-invalidates":     []any{"Inventory[]"},
		"x-forge-no-invalidation": []any{"Order[]"},
	})

	want := []string{"Inventory[]"}
	if len(ep.CacheTags.Invalidates) != 1 || ep.CacheTags.Invalidates[0] != want[0] {
		t.Fatalf("Invalidates = %v, want %v", ep.CacheTags.Invalidates, want)
	}
}

func TestExplicitEntityBeatsInference(t *testing.T) {
	spec := &APISpec{Schemas: map[string]*Schema{"Order": orderSchema()}}
	ep := &Endpoint{
		Method: "GET", Path: "/orders/{id}",
		Responses: map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Order"}},
		}}},
	}

	resolveEndpointCacheMeta(spec, ep, map[string]any{
		"x-forge-entity": map[string]any{"type": "PurchaseOrder", "idField": "order_number"},
	})

	if ep.Entity.Type != "PurchaseOrder" || ep.Entity.IDField != "order_number" {
		t.Fatalf("Entity = %+v, want PurchaseOrder/order_number", ep.Entity)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run 'TestResolveEntity|TestExplicitEntity' -v`
Expected: FAIL — `undefined: resolveEndpointCacheMeta`

- [ ] **Step 3: Write the implementation**

Add to `internal/client/introspector.go`:

```go
// resolveEndpointCacheMeta fills in an endpoint's entity and cache tags.
//
// Explicit declarations always beat inference, and an opt-out beats both. The
// order matters: an endpoint returning a projection must not be normalized just
// because its schema happens to carry an id.
func resolveEndpointCacheMeta(spec *APISpec, ep *Endpoint, ext map[string]any) {
	if v, ok := ext["x-forge-no-entity"].(bool); ok && v {
		return
	}

	entity, isList := endpointEntity(spec, ep, ext)
	if entity == nil {
		return
	}

	ep.Entity = entity

	base := DeriveTags(ep.Method, entity, isList)
	ep.CacheTags = ApplyTagOverrides(
		base,
		stringSlice(ext["x-forge-invalidates"]),
		stringSlice(ext["x-forge-no-invalidation"]),
	)

	if spec.Entities == nil {
		spec.Entities = make(map[string]*EntityRef)
	}

	spec.Entities[entity.Type] = entity
}

// endpointEntity resolves the entity an endpoint's success response carries,
// and reports whether that response is a collection.
func endpointEntity(spec *APISpec, ep *Endpoint, ext map[string]any) (*EntityRef, bool) {
	schema, isList := successResponseSchema(spec, ep)

	if raw, ok := ext["x-forge-entity"].(map[string]any); ok {
		typ, _ := raw["type"].(string)
		idField, _ := raw["idField"].(string)

		if typ != "" && idField != "" {
			return &EntityRef{Type: typ, IDField: idField}, isList
		}
	}

	if schema == nil {
		return nil, false
	}

	return InferEntity(schemaName(schema), spec.ResolveSchemaRef(schema.Ref)), isList
}

// successResponseSchema returns the lowest 2xx JSON schema and whether it is an
// array. The array's item schema is returned, since that is what carries the
// entity.
func successResponseSchema(spec *APISpec, ep *Endpoint) (*Schema, bool) {
	codes := make([]int, 0, len(ep.Responses))
	for code := range ep.Responses {
		if code >= 200 && code < 300 {
			codes = append(codes, code)
		}
	}

	if len(codes) == 0 {
		return nil, false
	}

	sort.Ints(codes)

	resp := ep.Responses[codes[0]]

	mt, ok := resp.Content["application/json"]
	if !ok || mt.Schema == nil {
		return nil, false
	}

	if mt.Schema.Type == "array" && mt.Schema.Items != nil {
		return mt.Schema.Items, true
	}

	return mt.Schema, false
}

// schemaName extracts a component name from a $ref. An inline schema has no
// name and therefore cannot be an entity: a cache key needs a stable typename,
// and an anonymous struct has none.
func schemaName(s *Schema) string {
	if s == nil || s.Ref == "" {
		return ""
	}

	if i := strings.LastIndex(s.Ref, "/"); i >= 0 {
		return s.Ref[i+1:]
	}

	return s.Ref
}

// stringSlice coerces a JSON-decoded extension value to []string. Extensions
// arrive as []any when parsed from a spec file and []string when read from a
// live router, so both are accepted.
func stringSlice(v any) []string {
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))

		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}

		return out
	default:
		return nil
	}
}
```

Ensure `sort` and `strings` are imported. Call `resolveEndpointCacheMeta(spec, &endpoint, op.Extensions)` at the end of `operationToEndpoint` (`:504`), and copy `x-forge-stream` into `StreamBindings` in `channelToWebSocket` (`:608`) and `channelToSSE` (`:649`). Also copy `Extensions` through `convertSchema` (`:698`) so `x-forge-id` survives into `client.Schema`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/client/introspector.go internal/client/introspector_client_meta_test.go
git commit -m "feat(client): resolve entity identity and cache tags into the IR"
```

---

### Task 10: The operation manifest emitter

**Files:**
- Create: `internal/client/generators/typescript/opsmanifest.go`
- Test: `internal/client/generators/typescript/opsmanifest_test.go`

**Interfaces:**
- Consumes: populated IR from Task 9
- Produces: `type OpsManifestGenerator struct{}`, `func NewOpsManifestGenerator() *OpsManifestGenerator`, `func (g *OpsManifestGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string` returning the contents of `src/ops.ts`

**Naming note:** this file is `opsmanifest.go`, not `manifest.go`. `manifest` already means `package.json` in this package (see `manifest_internal_test.go`), and reusing it would make both harder to find.

- [ ] **Step 1: Write the failing test**

```go
// internal/client/generators/typescript/opsmanifest_test.go
package typescript

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func manifestSpec() *client.APISpec {
	return &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				ID: "orderList", Method: "GET", Path: "/orders",
				Entity:    &client.EntityRef{Type: "Order", IDField: "id"},
				CacheTags: client.TagSet{Provides: []string{"Order:{id}", "Order[]"}},
			},
			{
				ID: "orderCreate", Method: "POST", Path: "/orders",
				Entity:    &client.EntityRef{Type: "Order", IDField: "id"},
				CacheTags: client.TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}},
			},
		},
		Entities: map[string]*client.EntityRef{
			"Order": {Type: "Order", IDField: "id"},
		},
	}
}

func TestOpsManifestContainsOperations(t *testing.T) {
	out := NewOpsManifestGenerator().Generate(manifestSpec(), client.GeneratorConfig{})

	for _, want := range []string{
		"orderList",
		"orderCreate",
		`method: 'POST'`,
		`path: '/orders'`,
		`provides: ['Order:{id}', 'Order[]']`,
		`invalidates: ['Order[]']`,
		`entity: 'Order'`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ops.ts missing %q\n\n%s", want, out)
		}
	}
}

func TestOpsManifestContainsEntities(t *testing.T) {
	out := NewOpsManifestGenerator().Generate(manifestSpec(), client.GeneratorConfig{})

	if !strings.Contains(out, `Order: { idField: 'id' }`) {
		t.Fatalf("ops.ts missing entity table\n\n%s", out)
	}
}

// Generated output is diffed by CI; map iteration must not reach the file.
func TestOpsManifestIsDeterministic(t *testing.T) {
	gen := NewOpsManifestGenerator()

	first := gen.Generate(manifestSpec(), client.GeneratorConfig{})
	for i := 0; i < 50; i++ {
		if got := gen.Generate(manifestSpec(), client.GeneratorConfig{}); got != first {
			t.Fatal("ops.ts differs between runs: a map is being iterated unsorted")
		}
	}
}

func TestOpsManifestEscapesHostileValues(t *testing.T) {
	spec := &client.APISpec{Endpoints: []client.Endpoint{{
		ID: "x", Method: "GET", Path: `/orders'; evil()//`,
	}}}

	out := NewOpsManifestGenerator().Generate(spec, client.GeneratorConfig{})

	if strings.Contains(out, `'/orders'; evil()//'`) {
		t.Fatalf("unescaped quote broke out of the string literal\n\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestOpsManifest -v`
Expected: FAIL — `undefined: NewOpsManifestGenerator`

- [ ] **Step 3: Write the implementation**

```go
// internal/client/generators/typescript/opsmanifest.go
package typescript

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// OpsManifestGenerator emits src/ops.ts: the data the runtime needs to cache,
// invalidate and bind streams, with no logic in it.
//
// Keeping this a data file rather than generated code is what lets a runtime
// defect be fixed by publishing the runtime instead of regenerating every
// consuming repository.
type OpsManifestGenerator struct{}

func NewOpsManifestGenerator() *OpsManifestGenerator { return &OpsManifestGenerator{} }

// Generate produces ops.ts.
func (g *OpsManifestGenerator) Generate(spec *client.APISpec, _ client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(`/**
 * Operation manifest.
 *
 * Generated. Entity identity was resolved in Go against the response schema, so
 * the runtime never has to guess which field identifies a record -- the class of
 * guess that, made wrong on a type carrying both an id and a tenant id, keys two
 * tenants' records to one cache entry.
 */

export interface OperationMeta {
  readonly method: string;
  readonly path: string;
  readonly entity?: string;
  readonly provides: readonly string[];
  readonly invalidates: readonly string[];
}

export interface EntityMeta {
  readonly idField: string;
}

`)

	g.writeOps(&buf, spec)
	g.writeEntities(&buf, spec)
	g.writeStreams(&buf, spec)

	return buf.String()
}

// writeOps emits the operation table in declaration order, which the
// introspector already produces deterministically.
func (g *OpsManifestGenerator) writeOps(buf *strings.Builder, spec *client.APISpec) {
	buf.WriteString("export const ops = {\n")

	for i := range spec.Endpoints {
		ep := &spec.Endpoints[i]

		buf.WriteString(fmt.Sprintf("  %s: {\n", tsKey(ep.ID)))
		buf.WriteString(fmt.Sprintf("    method: %s,\n", tsString(ep.Method)))
		buf.WriteString(fmt.Sprintf("    path: %s,\n", tsString(ep.Path)))

		if ep.Entity != nil {
			buf.WriteString(fmt.Sprintf("    entity: %s,\n", tsString(ep.Entity.Type)))
		}

		buf.WriteString(fmt.Sprintf("    provides: %s,\n", tsStringArray(ep.CacheTags.Provides)))
		buf.WriteString(fmt.Sprintf("    invalidates: %s,\n", tsStringArray(ep.CacheTags.Invalidates)))
		buf.WriteString("  },\n")
	}

	buf.WriteString("} as const satisfies Record<string, OperationMeta>;\n\n")
}

// writeEntities emits the typename-to-id-field table, sorted by typename.
func (g *OpsManifestGenerator) writeEntities(buf *strings.Builder, spec *client.APISpec) {
	names := make([]string, 0, len(spec.Entities))
	for name := range spec.Entities {
		names = append(names, name)
	}

	sort.Strings(names)

	buf.WriteString("export const entities = {\n")

	for _, name := range names {
		buf.WriteString(fmt.Sprintf("  %s: { idField: %s },\n",
			tsKey(name), tsString(spec.Entities[name].IDField)))
	}

	buf.WriteString("} as const satisfies Record<string, EntityMeta>;\n\n")
}

// writeStreams emits channel bindings from both WebSocket and SSE endpoints.
func (g *OpsManifestGenerator) writeStreams(buf *strings.Builder, spec *client.APISpec) {
	type channel struct {
		path     string
		bindings []client.StreamBinding
	}

	channels := make([]channel, 0, len(spec.WebSockets)+len(spec.SSEs))

	for i := range spec.WebSockets {
		if b := spec.WebSockets[i].StreamBindings; len(b) > 0 {
			channels = append(channels, channel{spec.WebSockets[i].Path, b})
		}
	}

	for i := range spec.SSEs {
		if b := spec.SSEs[i].StreamBindings; len(b) > 0 {
			channels = append(channels, channel{spec.SSEs[i].Path, b})
		}
	}

	sort.Slice(channels, func(i, j int) bool { return channels[i].path < channels[j].path })

	buf.WriteString("export const streams = [\n")

	for _, ch := range channels {
		for _, b := range ch.bindings {
			buf.WriteString("  {\n")
			buf.WriteString(fmt.Sprintf("    channel: %s,\n", tsString(ch.path)))
			buf.WriteString(fmt.Sprintf("    message: %s,\n", tsString(b.Message)))
			buf.WriteString(fmt.Sprintf("    entity: %s,\n", tsString(b.EntityType)))
			buf.WriteString(fmt.Sprintf("    intent: %s,\n", tsString(string(b.Intent))))
			buf.WriteString(fmt.Sprintf("    invalidates: %s,\n", tsStringArray(b.Invalidates)))
			buf.WriteString("  },\n")
		}
	}

	buf.WriteString("] as const;\n")
}

// tsString renders a single-quoted TypeScript string literal.
//
// Escaping is not paranoia: a path or tag reaching the file unescaped closes the
// literal and the generated module stops parsing, which surfaces as a build
// error in a file nobody wrote by hand.
func tsString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`)

	return "'" + r.Replace(s) + "'"
}

// tsStringArray renders a string array literal.
func tsStringArray(items []string) string {
	if len(items) == 0 {
		return "[]"
	}

	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, tsString(item))
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

// tsKey renders an object key, quoting it when it is not a bare identifier.
func tsKey(s string) string {
	for i, r := range s {
		valid := r == '_' || r == '$' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(i > 0 && r >= '0' && r <= '9')
		if !valid {
			return tsString(s)
		}
	}

	if s == "" {
		return tsString(s)
	}

	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run TestOpsManifest -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/opsmanifest.go internal/client/generators/typescript/opsmanifest_test.go
git commit -m "feat(client): emit the operation manifest as src/ops.ts"
```

---

### Task 11: The hook facade emitter, and retiring query.ts

**Files:**
- Create: `internal/client/generators/typescript/facades.go`
- Test: `internal/client/generators/typescript/facades_test.go`
- Modify: `internal/client/generators/typescript/generator.go:299` (drop `src/query.ts`, add `src/ops.ts` and `src/hooks.ts`)
- Delete: `internal/client/generators/typescript/query.go`, `internal/client/generators/typescript/query_internal_test.go`

**Interfaces:**
- Consumes: `NewOpsManifestGenerator` (Task 10), IR from Task 9
- Produces: `type FacadeGenerator struct{}`, `func NewFacadeGenerator() *FacadeGenerator`, `func (g *FacadeGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string` returning the contents of `src/hooks.ts`

- [ ] **Step 1: Write the failing test**

```go
// internal/client/generators/typescript/facades_test.go
package typescript

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestFacadesEmitOneLinePerEndpoint(t *testing.T) {
	out := NewFacadeGenerator().Generate(manifestSpec(), client.GeneratorConfig{})

	for _, want := range []string{
		"import { query, mutation } from '@forge-go/client-core';",
		"import { ops } from './ops';",
		"export const useOrderList = query(ops.orderList);",
		"export const useOrderCreate = mutation(ops.orderCreate);",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("hooks.ts missing %q\n\n%s", want, out)
		}
	}
}

func TestFacadesUseQueryForReadsOnly(t *testing.T) {
	spec := &client.APISpec{Endpoints: []client.Endpoint{
		{ID: "a", Method: "GET"},
		{ID: "b", Method: "HEAD"},
		{ID: "c", Method: "POST"},
		{ID: "d", Method: "DELETE"},
	}}

	out := NewFacadeGenerator().Generate(spec, client.GeneratorConfig{})

	if strings.Count(out, "= query(") != 2 {
		t.Fatalf("want two queries (GET, HEAD)\n\n%s", out)
	}

	if strings.Count(out, "= mutation(") != 2 {
		t.Fatalf("want two mutations (POST, DELETE)\n\n%s", out)
	}
}

func TestFacadesAreDeterministic(t *testing.T) {
	gen := NewFacadeGenerator()

	first := gen.Generate(manifestSpec(), client.GeneratorConfig{})
	for i := 0; i < 50; i++ {
		if got := gen.Generate(manifestSpec(), client.GeneratorConfig{}); got != first {
			t.Fatal("hooks.ts differs between runs")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestFacades -v`
Expected: FAIL — `undefined: NewFacadeGenerator`

- [ ] **Step 3: Write the implementation**

```go
// internal/client/generators/typescript/facades.go
package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// FacadeGenerator emits src/hooks.ts: one typed line per endpoint, delegating to
// the runtime.
//
// No per-endpoint logic is generated. Everything a hook does lives in
// @forge-go/client-core, so a defect there is fixed by publishing a package rather
// than by regenerating every repository that consumes this client.
type FacadeGenerator struct{}

func NewFacadeGenerator() *FacadeGenerator { return &FacadeGenerator{} }

// Generate produces hooks.ts.
func (g *FacadeGenerator) Generate(spec *client.APISpec, _ client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(`/**
 * Typed hooks over the operation manifest.
 *
 * Generated. Each line is a binding, not an implementation.
 */

import { query, mutation } from '@forge-go/client-core';
import { ops } from './ops';

`)

	for i := range spec.Endpoints {
		ep := &spec.Endpoints[i]

		factory := "mutation"
		if isReadMethod(ep.Method) {
			factory = "query"
		}

		buf.WriteString(fmt.Sprintf("export const %s = %s(ops.%s);\n",
			hookName(ep.ID), factory, tsKey(ep.ID)))
	}

	return buf.String()
}

// isReadMethod reports whether an endpoint reads rather than writes. Caching a
// POST would serve a stale answer to a request whose entire purpose was to
// change something.
func isReadMethod(method string) bool {
	m := strings.ToUpper(method)

	return m == "GET" || m == "HEAD"
}

// hookName renders `orderList` as `useOrderList`.
func hookName(id string) string {
	if id == "" {
		return "use"
	}

	return "use" + strings.ToUpper(id[:1]) + id[1:]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run TestFacades -v`
Expected: PASS

- [ ] **Step 5: Wire into the generator and drop query.ts**

In `internal/client/generators/typescript/generator.go`, replace the `src/query.ts` block near `:299`:

```go
		genClient.Files["src/ops.ts"] = NewOpsManifestGenerator().Generate(spec, config)
		genClient.Files["src/hooks.ts"] = NewFacadeGenerator().Generate(spec, config)
```

Then remove the retired files and the now-dead dependency:

```bash
git rm internal/client/generators/typescript/query.go \
       internal/client/generators/typescript/query_internal_test.go
```

In `getDependencies` (`:1484`), replace the `@tanstack/react-query` peer dependency with `@forge-go/client-core`.

- [ ] **Step 6: Verify the whole package**

Run: `go build ./... && go test ./internal/client/... ./internal/router/... .`
Expected: PASS. If `determinism_test.go` or `tscheck_test.go` reference `query.ts`, update their expectations to `ops.ts` and `hooks.ts` in the same commit.

- [ ] **Step 7: Commit**

```bash
git add -A internal/client/generators/typescript/
git commit -m "feat(client): emit typed hook facades, retire the TanStack layer"
```

---

### Task 12: End-to-end generation test

**Files:**
- Test: `internal/client/generators/typescript/e2e_client_meta_test.go` (create)

**Interfaces:**
- Consumes: everything above
- Produces: nothing; this is the gate that proves the tasks agree with each other

**Why this task exists:** unit tests on either side of the IR can both pass while disagreeing about the IR. This test drives a spec through inference, derivation and emission in one pass.

- [ ] **Step 1: Write the test**

```go
// internal/client/generators/typescript/e2e_client_meta_test.go
package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// orderSchema is declared here as well as in package client's tests: test
// helpers do not cross package boundaries, and duplicating four lines beats
// exporting a fixture from production code.
func orderSchema() *client.Schema {
	return &client.Schema{Type: "object", Properties: map[string]*client.Schema{
		"id":    {Type: "string"},
		"total": {Type: "integer"},
	}}
}

// TestGenerationCarriesEntityMetaEndToEnd drives a spec with no explicit
// declarations at all and asserts the zero-config promise: normalization and
// correct same-entity invalidation with nothing annotated.
func TestGenerationCarriesEntityMetaEndToEnd(t *testing.T) {
	orderRef := &client.Schema{Ref: "#/components/schemas/Order"}

	spec := &client.APISpec{
		Info:    client.APIInfo{Title: "Orders", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{"Order": orderSchema()},
		Endpoints: []client.Endpoint{
			{
				ID: "orderList", Method: "GET", Path: "/orders",
				Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Type: "array", Items: orderRef}},
				}}},
			},
			{
				ID: "orderCreate", Method: "POST", Path: "/orders",
				Responses: map[int]*client.Response{201: {Content: map[string]*client.MediaType{
					"application/json": {Schema: orderRef},
				}}},
			},
		},
	}

	for i := range spec.Endpoints {
		resolveEndpointCacheMetaForTest(spec, &spec.Endpoints[i])
	}

	out, err := (&Generator{}).Generate(context.Background(), spec, client.DefaultConfig())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	opsFile, ok := out.Files["src/ops.ts"]
	if !ok {
		t.Fatal("src/ops.ts was not generated")
	}

	for _, want := range []string{
		`entity: 'Order'`,
		`provides: ['Order:{id}', 'Order[]']`,
		`invalidates: ['Order[]']`,
		`Order: { idField: 'id' }`,
	} {
		if !strings.Contains(opsFile, want) {
			t.Fatalf("ops.ts missing %q\n\n%s", want, opsFile)
		}
	}

	hooks := out.Files["src/hooks.ts"]
	if !strings.Contains(hooks, "export const useOrderList = query(ops.orderList);") {
		t.Fatalf("hooks.ts missing the list hook\n\n%s", hooks)
	}

	if _, present := out.Files["src/query.ts"]; present {
		t.Fatal("src/query.ts is still being generated; the TanStack layer was not retired")
	}
}
```

**Implementer note:** `resolveEndpointCacheMeta` is unexported in package `client`, so this test in package `typescript` cannot call it. Export a thin wrapper from `internal/client` as `func ResolveEndpointCacheMeta(spec *APISpec, ep *Endpoint, ext map[string]any)` in Task 9 and call that here, replacing `resolveEndpointCacheMetaForTest`. Prefer exporting the wrapper over moving this test into package `client`, because the assertion is about generated TypeScript.

- [ ] **Step 2: Run the test**

Run: `go test ./internal/client/generators/typescript/ -run TestGenerationCarriesEntityMetaEndToEnd -v`
Expected: PASS

- [ ] **Step 3: Run the full suite**

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/client/
git commit -m "test(client): end-to-end generation of entity metadata"
```

---

## Deferred to later plans

| Plan | Scope |
|---|---|
| 2 | `forge client check`, `forge client diff` with cache-breaking classification, `forge client watch` over the filesystem (not the debug hub — see the design doc) |
| 3 | `@forge-go/client-core`: entity store, normalizer, skeletons, tag graph, REST transport, auth and identity partitioning |
| 4 | `@forge-go/client-react` adapter, dogfooded on `extensions/dashboard` |
| 5 | Stream binding runtime: WS/SSE/WebTransport transports, live queries, gap recovery |
| 6 | `@forge-go/client-vue` and `@forge-go/client-angular` adapters |
| 7 | `@forge-go/client-devtools` |

Plan 3 depends only on `src/ops.ts` being stable, so it can begin as soon as Task 10 lands.

## Self-Review

**Spec coverage.** Entity identity inference (Tasks 2, 7, 9), the exactly-one refusal rule (Task 2), typename collision qualification — **gap: not covered**, see below. Tag derivation including the PATCH decision (Task 3), cross-entity declarations (Tasks 5, 9), stream binding with suffix inference (Tasks 4, 5, 8, 9), manifest and facade emission (Tasks 10, 11), retiring `query.ts` (Task 11), determinism (Tasks 3, 10, 11).

**Gap found and accepted:** package-qualified typenames on collision are specified but not implemented here. Component schema names arrive from `internal/router` already flattened to a single name, so collision handling belongs in the OpenAPI generator's naming, not in this phase's inference. Recorded as the first task of Plan 2 rather than added here, because it changes existing schema naming behaviour and deserves its own review.

**Gap found and accepted:** tag template resolution (`{req.customerId}` → a value) is a runtime concern. The manifest carries templates verbatim; Plan 3 resolves them. Generation-time failure for templates that resolve nowhere requires the resolver and moves to Plan 3 with it.

**Placeholder scan.** No TBD/TODO. Two tasks (7 and 12) carry explicit implementer notes where the exact local symbol must be discovered, each with the command to find it — these are directed lookups against a named file, not deferred decisions.

**Type consistency.** `EntityRef{Type, IDField}` used identically in Tasks 1, 2, 3, 9, 10. `TagSet{Provides, Invalidates}` in Tasks 1, 3, 9, 10. `StreamBinding{Message, EntityType, Intent, Invalidates}` in Tasks 1, 4, 8, 10 — note the router and client packages each define one; the introspector converts between them in Task 9. `tsString`/`tsStringArray`/`tsKey` defined in Task 10, reused in Task 11. `manifestSpec()` is defined in Task 10 (package `typescript`) and reused in Task 11. `orderSchema()` was originally defined only in Task 9 (package `client`) yet called from Task 12 (package `typescript`) — test helpers do not cross package boundaries, so Task 12 would not have compiled. **Fixed inline:** Task 12 now declares its own `orderSchema()` returning `*client.Schema`.
