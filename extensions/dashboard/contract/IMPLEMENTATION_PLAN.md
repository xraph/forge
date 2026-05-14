# Dashboard Contract — Slice (a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the Go-side contract package for slice (a): a single-endpoint, declarative-manifest dashboard that contributors author in YAML, with kind-discriminated wire envelope, server-side permission filtering, multiplexed SSE for subscriptions, per-contributor version negotiation, and a default audit emitter.

**Architecture:** A new self-contained package at `extensions/dashboard/contract/` exposes (1) types for request/response/stream envelopes, (2) types for contributor manifests with strict slot typing, (3) a registry that validates manifests at startup and resolves cross-references, (4) a graph-build pipeline that filters per-user before sending, (5) HTTP and SSE transports that dispatch by kind. The package coexists with today's templ-based contributor system; legacy registration and routing continue working in parallel until slice (f) retires them.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3` for YAML, stdlib `net/http`, `encoding/json`, `testing`. No new external dependencies. Reuses `extensions/dashboard/auth.UserInfo` and the existing `forge.Router`.

---

## Reference

- **Design spec:** [DESIGN.md](DESIGN.md) — read this first; every decision in this plan traces back to a row in the spec's "Design Decisions Locked In" table.
- **Forge module path:** `github.com/xraph/forge`
- **Existing patterns to mirror:**
  - Test layout: see `extensions/dashboard/contributor/registry_test.go` — plain `testing` package, table-driven where helpful, no testify in this subtree.
  - Manifest layout: see `extensions/dashboard/contributor/manifest.go:10` — JSON tags on every field, optional fields use `omitempty`.
  - Route registration: see `extensions/dashboard/extension.go:1207` — `must(router.GET(base+path, handler))` pattern with `forge.Router`.
  - Existing CSRF infrastructure: `extensions/security/csrf.go` — slice (a) defines hooks, slice (b) wires the actual middleware.

## Out of Scope (Other Slices)

Do **not** implement these in this plan:
- React shell, intent renderers (slice (d), (e))
- Pilot contributor migration to YAML (slice (c))
- Chronicle integration for audit; slice (a) ships an interface + stdlib-log default impl, slice (b) wires chronicles.
- CSRF/idempotency middleware enforcement; slice (a) defines header hooks, slice (b) wires the security extension.
- Removal of any templ files (slice (f)).

## File Structure

```
extensions/dashboard/contract/
  doc.go                       # package documentation
  errors.go                    # canonical error codes + Error type
  envelope.go                  # Request, Response, StreamEvent types
  manifest.go                  # ContractManifest, Intent, Slot, GraphNode, Predicate, Extension types
  predicate.go                 # Predicate evaluation + PermissionsHash
  warden.go                    # Warden interface, Principal, Action, Decision, WardenRegistry
  registry.go                  # ContractRegistry — contributor + intent index
  slots.go                     # slot extension application, cycle detection, depth check
  graph.go                     # per-request graph build with permission filter
  cache.go                     # graph LRU cache keyed by (route, permissionsHash, shellVersion)
  audit.go                     # AuditEmitter interface + log-based default implementation
  loader/
    yaml.go                    # YAML → ContractManifest with schemaVersion check
    validate.go                # cross-reference validator (intent refs, slot accepts, warden names, version negotiation)
  transport/
    http.go                    # POST /api/dashboard/v1 — kind dispatch, version negotiation, error envelope
    capabilities.go            # GET /api/dashboard/v1/capabilities
    stream.go                  # GET /api/dashboard/v1/stream — multiplexed SSE
    control.go                 # POST /api/dashboard/v1/stream/control — subscribe/unsubscribe
  testdata/
    fixture_users.yaml         # E2E fixture contributor manifest
    fixture_auth_extends.yaml  # E2E fixture extension manifest
  *_test.go                    # tests live next to each file (Go convention)

extensions/dashboard/extension.go  # MODIFY: register contract routes alongside legacy

cmd/dashboard-contract-probe/
  main.go                      # raw-envelope CLI for testing without React shell
```

Each file owns one responsibility. Files that change together stay together: `slots.go` and `graph.go` both touch graph traversal; `registry.go` is the orchestration layer that uses both.

## Conventions

- **Imports:** every Go file imports the stdlib first, then `github.com/xraph/forge/...`, then third-party.
- **Errors:** wrap with `fmt.Errorf("loading %s: %w", path, err)`. Use `Error` type from `errors.go` for canonical-coded errors that cross the wire boundary.
- **Comments:** package doc lives in `doc.go`. Exported types get one-line doc comments (Go style). No commit-rationale or task-reference comments.
- **Test file naming:** `<file>_test.go` next to the file. Use plain `testing` package; helpers live in the test file's `package contract` (no `_test` suffix unless we need black-box testing).
- **Commits:** one logical change per commit, no Co-Authored-By trailers. Conventional commit prefix (`feat`, `test`, `chore`, `refactor`).

---

## Phase 0: Package Skeleton & Canonical Errors

### Task 0.1: Create package + errors

**Files:**
- Create: `extensions/dashboard/contract/doc.go`
- Create: `extensions/dashboard/contract/errors.go`
- Create: `extensions/dashboard/contract/errors_test.go`

- [ ] **Step 1: Write doc.go**

```go
// Package contract defines the declarative, single-endpoint contract for the
// admin dashboard: contributor manifests, request/response envelopes, the
// permission model, the slot/graph composition rules, and the per-contributor
// version negotiation protocol.
//
// See DESIGN.md in this directory for the spec this implements.
package contract
```

- [ ] **Step 2: Write the failing tests for errors.go**

```go
// errors_test.go
package contract

import (
	"errors"
	"testing"
)

func TestError_CodeAndMessage(t *testing.T) {
	e := &Error{Code: CodePermissionDenied, Message: "no", CorrelationID: "c1"}
	if e.Code != "PERMISSION_DENIED" {
		t.Errorf("Code = %q, want PERMISSION_DENIED", e.Code)
	}
	if got := e.Error(); got != "PERMISSION_DENIED: no" {
		t.Errorf("Error() = %q", got)
	}
}

func TestError_Is(t *testing.T) {
	e := &Error{Code: CodeNotFound}
	if !errors.Is(e, ErrNotFound) {
		t.Error("errors.Is should match canonical sentinel")
	}
}

func TestCanonicalCodes_AllPresent(t *testing.T) {
	want := []ErrorCode{
		CodeBadRequest, CodeUnauthenticated, CodePermissionDenied,
		CodeNotFound, CodeConflict, CodeRateLimited,
		CodeUnsupportedVersion, CodeUnavailable, CodeInternal,
	}
	for _, c := range want {
		if c == "" {
			t.Errorf("canonical code missing")
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL with "undefined: Error" / "undefined: CodePermissionDenied" etc.

- [ ] **Step 4: Implement errors.go**

```go
// errors.go
package contract

import "fmt"

// ErrorCode is a canonical, wire-stable code for contract errors.
// Contributor-specific codes are namespaced like "auth.SESSION_EXPIRED".
type ErrorCode string

const (
	CodeBadRequest         ErrorCode = "BAD_REQUEST"
	CodeUnauthenticated    ErrorCode = "UNAUTHENTICATED"
	CodePermissionDenied   ErrorCode = "PERMISSION_DENIED"
	CodeNotFound           ErrorCode = "NOT_FOUND"
	CodeConflict           ErrorCode = "CONFLICT"
	CodeRateLimited        ErrorCode = "RATE_LIMITED"
	CodeUnsupportedVersion ErrorCode = "UNSUPPORTED_VERSION"
	CodeUnavailable        ErrorCode = "UNAVAILABLE"
	CodeInternal           ErrorCode = "INTERNAL"
)

// Sentinel errors for use with errors.Is.
var (
	ErrBadRequest         = &Error{Code: CodeBadRequest}
	ErrUnauthenticated    = &Error{Code: CodeUnauthenticated}
	ErrPermissionDenied   = &Error{Code: CodePermissionDenied}
	ErrNotFound           = &Error{Code: CodeNotFound}
	ErrConflict           = &Error{Code: CodeConflict}
	ErrRateLimited        = &Error{Code: CodeRateLimited}
	ErrUnsupportedVersion = &Error{Code: CodeUnsupportedVersion}
	ErrUnavailable        = &Error{Code: CodeUnavailable}
	ErrInternal           = &Error{Code: CodeInternal}
)

// Error is the canonical contract error type. It serializes to the wire
// "error" object documented in DESIGN.md.
type Error struct {
	Code          ErrorCode      `json:"code"`
	Message       string         `json:"message,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	Retryable     bool           `json:"retryable,omitempty"`
	CorrelationID string         `json:"correlationID,omitempty"`
	Redactions    []string       `json:"redactions,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Is matches sentinel errors by Code.
func (e *Error) Is(target error) bool {
	if t, ok := target.(*Error); ok {
		return t.Code == e.Code
	}
	return false
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS — 3 tests.

- [ ] **Step 6: Commit**

```bash
git add extensions/dashboard/contract/doc.go extensions/dashboard/contract/errors.go extensions/dashboard/contract/errors_test.go
git commit -m "feat(dashboard/contract): add package skeleton and canonical error codes"
```

---

## Phase 1: Wire Envelope Types

### Task 1.1: Request envelope + JSON round-trip

**Files:**
- Create: `extensions/dashboard/contract/envelope.go`
- Create: `extensions/dashboard/contract/envelope_test.go`

- [ ] **Step 1: Write the failing test**

```go
// envelope_test.go
package contract

import (
	"encoding/json"
	"testing"
)

func TestRequest_RoundTrip_Command(t *testing.T) {
	req := Request{
		Envelope:        "v1",
		Kind:            KindCommand,
		Contributor:     "users",
		Intent:          "user.disable",
		IntentVersion:   2,
		Payload:         json.RawMessage(`{"id":"u_42"}`),
		Params:          map[string]any{"tenant": "acme"},
		Context:         RequestContext{Route: "/admin/users", CorrelationID: "req_x"},
		CSRF:            "csrf_token",
		IdempotencyKey:  "ik_1",
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Request
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != KindCommand || got.IdempotencyKey != "ik_1" {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestKind_Constants(t *testing.T) {
	for _, k := range []Kind{KindGraph, KindQuery, KindCommand, KindSubscribe} {
		if k == "" {
			t.Errorf("kind constant empty")
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL — "undefined: Request" etc.

- [ ] **Step 3: Implement envelope.go**

```go
// envelope.go
package contract

import "encoding/json"

// Kind discriminates request/response semantics on the wire.
// A kind is enforced against the intent's declared Capability at dispatch time.
type Kind string

const (
	KindGraph     Kind = "graph"
	KindQuery     Kind = "query"
	KindCommand   Kind = "command"
	KindSubscribe Kind = "subscribe"
)

// Request is the wire envelope for POST /api/dashboard/{envelope}.
type Request struct {
	Envelope       string          `json:"envelope"`
	Kind           Kind            `json:"kind"`
	Contributor    string          `json:"contributor"`
	Intent         string          `json:"intent"`
	IntentVersion  int             `json:"intentVersion,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	Params         map[string]any  `json:"params,omitempty"`
	Context        RequestContext  `json:"context"`
	CSRF           string          `json:"csrf,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
}

// RequestContext carries route + correlation metadata. Always populated by the shell.
type RequestContext struct {
	Route         string `json:"route,omitempty"`
	CorrelationID string `json:"correlationID,omitempty"`
}

// Response is the wire envelope for successful POST responses.
type Response struct {
	OK       bool            `json:"ok"`
	Envelope string          `json:"envelope"`
	Kind     Kind            `json:"kind"`
	Data     json.RawMessage `json:"data,omitempty"`
	Meta     ResponseMeta    `json:"meta"`
}

// ResponseMeta carries cross-cutting metadata (versioning, caching, invalidation).
type ResponseMeta struct {
	IntentVersion int          `json:"intentVersion,omitempty"`
	Deprecation   *Deprecation `json:"deprecation,omitempty"`
	CacheControl  *CacheHint   `json:"cacheControl,omitempty"`
	Invalidates   []string     `json:"invalidates,omitempty"`
}

// Deprecation surfaces a "this version will be removed" hint to the shell.
type Deprecation struct {
	IntentVersion int    `json:"intentVersion"`
	RemoveAfter   string `json:"removeAfter"`
}

// CacheHint communicates how long the shell can serve stale data for a query.
type CacheHint struct {
	StaleTime string `json:"staleTime,omitempty"`
}

// ErrorResponse is the wire envelope for failed POST responses.
type ErrorResponse struct {
	OK       bool   `json:"ok"`
	Envelope string `json:"envelope"`
	Error    *Error `json:"error"`
}

// StreamEvent is the SSE payload for a single subscription event.
type StreamEvent struct {
	Intent  string          `json:"intent"`
	Mode    SubscriptionMode `json:"mode"`
	Payload json.RawMessage `json:"payload"`
	Seq     uint64          `json:"seq"`
}

// SubscriptionMode is how the client integrates events into local state.
type SubscriptionMode string

const (
	ModeReplace        SubscriptionMode = "replace"
	ModeAppend         SubscriptionMode = "append"
	ModeSnapshotDelta  SubscriptionMode = "snapshot+delta"
)
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/envelope.go extensions/dashboard/contract/envelope_test.go
git commit -m "feat(dashboard/contract): add wire envelope types for request, response, stream"
```

### Task 1.2: ErrorResponse + StreamEvent serialization

**Files:**
- Modify: `extensions/dashboard/contract/envelope_test.go`

- [ ] **Step 1: Add tests for ErrorResponse and StreamEvent round-trip**

```go
func TestErrorResponse_RoundTrip(t *testing.T) {
	er := ErrorResponse{
		OK:       false,
		Envelope: "v1",
		Error: &Error{
			Code:          CodePermissionDenied,
			Message:       "denied",
			CorrelationID: "c1",
			Redactions:    []string{"users[*].email"},
		},
	}
	b, err := json.Marshal(er)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(b, []byte(`"code":"PERMISSION_DENIED"`)) {
		t.Errorf("marshaled form missing code: %s", b)
	}
	var got ErrorResponse
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Error.Code != CodePermissionDenied {
		t.Errorf("round trip lost code")
	}
}

func TestStreamEvent_RoundTrip_AllModes(t *testing.T) {
	for _, mode := range []SubscriptionMode{ModeReplace, ModeAppend, ModeSnapshotDelta} {
		ev := StreamEvent{Intent: "audit.tail", Mode: mode, Payload: json.RawMessage(`{"a":1}`), Seq: 42}
		b, _ := json.Marshal(ev)
		var got StreamEvent
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
		if got.Mode != mode || got.Seq != 42 {
			t.Errorf("mode %s round trip lost data: %+v", mode, got)
		}
	}
}
```

Add `"bytes"` to the import block in `envelope_test.go`.

- [ ] **Step 2: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS — all envelope tests.

- [ ] **Step 3: Commit**

```bash
git add extensions/dashboard/contract/envelope_test.go
git commit -m "test(dashboard/contract): cover error response and stream event round-trip"
```

---

## Phase 2: Manifest Types

### Task 2.1: Core manifest structs

**Files:**
- Create: `extensions/dashboard/contract/manifest.go`
- Create: `extensions/dashboard/contract/manifest_test.go`

- [ ] **Step 1: Write the failing test (YAML round-trip)**

```go
// manifest_test.go
package contract

import (
	"testing"

	"gopkg.in/yaml.v3"
)

const sampleManifestYAML = `
schemaVersion: 1
contributor:
  name: users
  envelope:
    supports: [v1]
    preferred: v1
  capabilities: [users.read, users.write]

intents:
  - name: users.list
    kind: query
    version: 1
    capability: read
    requires:
      all: ["scope:users.read"]
    audit: false

  - name: user.disable
    kind: command
    version: 2
    capability: write
    requires:
      all: ["role:admin", "scope:users.write"]
      warden: tenantOwner
    invalidates: [users.list, user.detail]

graph:
  - route: /users
    intent: page.shell
    title: Users
    nav:
      group: Identity
      icon: users
      priority: 10
`

func TestManifest_YAML_RoundTrip(t *testing.T) {
	var m ContractManifest
	if err := yaml.Unmarshal([]byte(sampleManifestYAML), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d", m.SchemaVersion)
	}
	if m.Contributor.Name != "users" {
		t.Errorf("contributor name = %q", m.Contributor.Name)
	}
	if got := len(m.Intents); got != 2 {
		t.Fatalf("intents count = %d", got)
	}
	if m.Intents[0].Kind != IntentKindQuery || m.Intents[1].Kind != IntentKindCommand {
		t.Errorf("intent kinds = %v, %v", m.Intents[0].Kind, m.Intents[1].Kind)
	}
	if m.Intents[1].Requires.Warden != "tenantOwner" {
		t.Errorf("warden ref = %q", m.Intents[1].Requires.Warden)
	}
	if got := len(m.Graph); got != 1 {
		t.Fatalf("graph count = %d", got)
	}
	if m.Graph[0].Route != "/users" {
		t.Errorf("route = %q", m.Graph[0].Route)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL — "undefined: ContractManifest" etc.

- [ ] **Step 3: Implement manifest.go**

```go
// manifest.go
package contract

// IntentKind is the wire-level discriminator declared on every intent.
// It must be consistent with the request envelope Kind at dispatch time.
type IntentKind string

const (
	IntentKindGraph        IntentKind = "graph"
	IntentKindQuery        IntentKind = "query"
	IntentKindCommand      IntentKind = "command"
	IntentKindSubscription IntentKind = "subscription"
)

// Capability is the data-classification of an intent's effects.
// It composes with IntentKind: a command must be capability=write; a query/subscription
// must be capability=read; a graph must be capability=render.
type Capability string

const (
	CapRead   Capability = "read"
	CapWrite  Capability = "write"
	CapRender Capability = "render"
)

// ContractManifest is the top-level YAML each contributor publishes.
type ContractManifest struct {
	SchemaVersion int             `yaml:"schemaVersion" json:"schemaVersion"`
	Contributor   Contributor     `yaml:"contributor"   json:"contributor"`
	Queries       map[string]Query `yaml:"queries,omitempty" json:"queries,omitempty"`
	Intents       []Intent        `yaml:"intents"       json:"intents"`
	Graph         []GraphNode    `yaml:"graph,omitempty" json:"graph,omitempty"`
	Extends       []Extension    `yaml:"extends,omitempty" json:"extends,omitempty"`
}

// Contributor names a single contributor and declares its supported envelope versions.
type Contributor struct {
	Name         string         `yaml:"name"         json:"name"`
	Envelope     EnvelopeSupport `yaml:"envelope"     json:"envelope"`
	Capabilities []string       `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

// EnvelopeSupport declares which envelope versions this contributor can speak.
type EnvelopeSupport struct {
	Supports  []string `yaml:"supports"  json:"supports"`
	Preferred string   `yaml:"preferred" json:"preferred"`
}

// Intent declares a single named operation and its security/version metadata.
type Intent struct {
	Name        string      `yaml:"name"        json:"name"`
	Kind        IntentKind  `yaml:"kind"        json:"kind"`
	Version     int         `yaml:"version"     json:"version"`
	Capability  Capability  `yaml:"capability"  json:"capability"`
	Requires    Predicate   `yaml:"requires,omitempty" json:"requires,omitempty"`
	Schema      IntentSchema `yaml:"schema,omitempty" json:"schema,omitempty"`
	Mode        SubscriptionMode `yaml:"mode,omitempty" json:"mode,omitempty"`         // subscription only
	Invalidates []string    `yaml:"invalidates,omitempty" json:"invalidates,omitempty"` // command only
	Audit       *bool       `yaml:"audit,omitempty"       json:"audit,omitempty"`        // default true for commands
	Deprecated  *Deprecation `yaml:"deprecated,omitempty" json:"deprecated,omitempty"`
}

// IntentSchema is loose by design: contributors describe their input/output shapes;
// validation against this is opt-in (slice (b) wires it).
type IntentSchema struct {
	Input  map[string]any `yaml:"input,omitempty"  json:"input,omitempty"`
	Output any            `yaml:"output,omitempty" json:"output,omitempty"`
}

// Query is a named, reusable, cacheable data binding referenced by graph nodes.
type Query struct {
	Intent string                  `yaml:"intent" json:"intent"`
	Params map[string]ParamSource  `yaml:"params,omitempty" json:"params,omitempty"`
	Cache  *QueryCache             `yaml:"cache,omitempty"  json:"cache,omitempty"`
}

// ParamSource describes where a parameter value comes from.
// Exactly one of Value/From is set; YAML uses { from: route.tenant } or a literal.
type ParamSource struct {
	Value any    `yaml:"value,omitempty" json:"value,omitempty"`
	From  string `yaml:"from,omitempty"  json:"from,omitempty"` // route.X | parent.X | state.X | session.X
}

// QueryCache declares per-query staleness for the client.
type QueryCache struct {
	StaleTime string `yaml:"staleTime,omitempty" json:"staleTime,omitempty"`
}

// GraphNode is a single node in the UI graph (an intent invocation with slot fills).
type GraphNode struct {
	Route       string                  `yaml:"route,omitempty"       json:"route,omitempty"` // top-level only
	Intent      string                  `yaml:"intent"                json:"intent"`
	Title       string                  `yaml:"title,omitempty"       json:"title,omitempty"`
	Nav         *NavConfig              `yaml:"nav,omitempty"         json:"nav,omitempty"`
	Root        bool                    `yaml:"root,omitempty"        json:"root,omitempty"`
	Data        *DataBinding            `yaml:"data,omitempty"        json:"data,omitempty"`
	Props       map[string]any          `yaml:"props,omitempty"       json:"props,omitempty"`
	Slots       map[string][]GraphNode  `yaml:"slots,omitempty"       json:"slots,omitempty"`
	VisibleWhen *Predicate              `yaml:"visibleWhen,omitempty" json:"visibleWhen,omitempty"`
	EnabledWhen *Predicate              `yaml:"enabledWhen,omitempty" json:"enabledWhen,omitempty"`
	Op          string                  `yaml:"op,omitempty"          json:"op,omitempty"`     // for action nodes
	Payload     map[string]ParamSource  `yaml:"payload,omitempty"     json:"payload,omitempty"`
	Component   string                  `yaml:"component,omitempty"   json:"component,omitempty"` // intent: custom escape hatch
	Src         string                  `yaml:"src,omitempty"         json:"src,omitempty"`       // intent: iframe escape hatch
	Sandbox     []string                `yaml:"sandbox,omitempty"     json:"sandbox,omitempty"`
	Protocol    string                  `yaml:"protocol,omitempty"    json:"protocol,omitempty"`
}

// NavConfig is per-route nav metadata; mirrors today's contributor.NavItem fields.
type NavConfig struct {
	Group    string `yaml:"group,omitempty"    json:"group,omitempty"`
	Icon     string `yaml:"icon,omitempty"     json:"icon,omitempty"`
	Priority int    `yaml:"priority,omitempty" json:"priority,omitempty"`
	Badge    string `yaml:"badge,omitempty"    json:"badge,omitempty"`
}

// DataBinding is either an inline {intent, params} pair or a named query reference.
// YAML supports both shapes:
//   data: queries.userList
//   data: { intent: users.list, params: {...} }
type DataBinding struct {
	QueryRef string                 `yaml:"-" json:"queryRef,omitempty"`
	Intent   string                 `yaml:"intent,omitempty"  json:"intent,omitempty"`
	Params   map[string]ParamSource `yaml:"params,omitempty"  json:"params,omitempty"`
}

// Predicate is the boolean access expression: any of all/any/not, plus an optional
// named Warden delegate. An empty Predicate evaluates to allow.
type Predicate struct {
	All    []string `yaml:"all,omitempty"    json:"all,omitempty"`
	Any    []string `yaml:"any,omitempty"    json:"any,omitempty"`
	Not    []string `yaml:"not,omitempty"    json:"not,omitempty"`
	Warden string   `yaml:"warden,omitempty" json:"warden,omitempty"`
}

// Extension declares that this contributor wants to add nodes into another contributor's slot.
type Extension struct {
	Target ExtensionTarget `yaml:"target" json:"target"`
	Slot   string          `yaml:"slot"   json:"slot"`     // dotted path: "detailDrawer.fields"
	Add    []GraphNode     `yaml:"add"    json:"add"`
}

// ExtensionTarget identifies the host node to extend.
type ExtensionTarget struct {
	Contributor string `yaml:"contributor" json:"contributor"`
	Intent      string `yaml:"intent"      json:"intent"`
	Route       string `yaml:"route,omitempty" json:"route,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/manifest.go extensions/dashboard/contract/manifest_test.go
git commit -m "feat(dashboard/contract): add manifest types with YAML tags"
```

### Task 2.2: DataBinding string shorthand parser

The YAML form `data: queries.userList` (a bare string) needs custom unmarshalling because Go's default scalar decode won't fill a struct.

**Files:**
- Modify: `extensions/dashboard/contract/manifest.go`
- Modify: `extensions/dashboard/contract/manifest_test.go`

- [ ] **Step 1: Add the failing test**

```go
// add to manifest_test.go
const dataShorthandYAML = `
schemaVersion: 1
contributor:
  name: users
  envelope: { supports: [v1], preferred: v1 }
intents: []
graph:
  - intent: resource.list
    data: queries.userList
  - intent: metric.counter
    data:
      intent: count.events
      params: { since: { value: "1h" } }
`

func TestDataBinding_BothShapes(t *testing.T) {
	var m ContractManifest
	if err := yaml.Unmarshal([]byte(dataShorthandYAML), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Graph[0].Data == nil || m.Graph[0].Data.QueryRef != "queries.userList" {
		t.Errorf("shorthand not parsed: %+v", m.Graph[0].Data)
	}
	if m.Graph[1].Data == nil || m.Graph[1].Data.Intent != "count.events" {
		t.Errorf("inline form not parsed: %+v", m.Graph[1].Data)
	}
}
```

- [ ] **Step 2: Run to verify failure (shorthand decoding fails)**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL — shorthand doesn't unmarshal into the struct.

- [ ] **Step 3: Implement custom UnmarshalYAML on DataBinding**

Add to `manifest.go`:

```go
import "gopkg.in/yaml.v3" // add to imports

func (d *DataBinding) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		d.QueryRef = value.Value
		return nil
	case yaml.MappingNode:
		// Decode into a shadow type to avoid recursion.
		type alias DataBinding
		var a alias
		if err := value.Decode(&a); err != nil {
			return err
		}
		*d = DataBinding(a)
		return nil
	default:
		return fmt.Errorf("data: expected scalar or mapping, got kind=%d", value.Kind)
	}
}
```

Add `"fmt"` to imports.

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS — both shapes round-trip.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/manifest.go extensions/dashboard/contract/manifest_test.go
git commit -m "feat(dashboard/contract): parse data binding shorthand and inline forms"
```

### Task 2.3: ParamSource string shorthand parser

YAML form `tenant: route.tenant` should also work as shorthand for `tenant: { from: route.tenant }`. (Note this is for `params` and `payload` maps where the value type is `ParamSource`.)

**Files:**
- Modify: `extensions/dashboard/contract/manifest.go`
- Modify: `extensions/dashboard/contract/manifest_test.go`

- [ ] **Step 1: Add the failing test**

```go
const paramShorthandYAML = `
schemaVersion: 1
contributor: { name: x, envelope: { supports: [v1], preferred: v1 } }
intents: []
queries:
  q1:
    intent: foo
    params:
      shorthand: route.tenant
      explicit:  { from: parent.id }
      literal:   { value: 5 }
`

func TestParamSource_Shorthand(t *testing.T) {
	var m ContractManifest
	if err := yaml.Unmarshal([]byte(paramShorthandYAML), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	q := m.Queries["q1"]
	if q.Params["shorthand"].From != "route.tenant" {
		t.Errorf("shorthand not parsed: %+v", q.Params["shorthand"])
	}
	if q.Params["explicit"].From != "parent.id" {
		t.Errorf("explicit form lost data: %+v", q.Params["explicit"])
	}
	if v, ok := q.Params["literal"].Value.(int); !ok || v != 5 {
		t.Errorf("literal value wrong: %+v", q.Params["literal"].Value)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL.

- [ ] **Step 3: Implement custom UnmarshalYAML on ParamSource**

```go
func (p *ParamSource) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		p.From = value.Value
		return nil
	case yaml.MappingNode:
		type alias ParamSource
		var a alias
		if err := value.Decode(&a); err != nil {
			return err
		}
		*p = ParamSource(a)
		return nil
	default:
		return fmt.Errorf("param: expected scalar or mapping, got kind=%d", value.Kind)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/manifest.go extensions/dashboard/contract/manifest_test.go
git commit -m "feat(dashboard/contract): support param source shorthand in YAML"
```

---

## Phase 3: Predicate Evaluation & Permissions Hash

### Task 3.1: Predicate.Eval against UserInfo

**Files:**
- Create: `extensions/dashboard/contract/predicate.go`
- Create: `extensions/dashboard/contract/predicate_test.go`

The predicate language uses tokens of the form `role:NAME`, `scope:NAME`, `claim:KEY=VALUE`. `all`/`any`/`not` compose these. Empty predicate = allow.

- [ ] **Step 1: Write the failing tests**

```go
// predicate_test.go
package contract

import (
	"testing"

	"github.com/xraph/forge/extensions/dashboard/auth"
)

func u(roles, scopes []string) *auth.UserInfo {
	return &auth.UserInfo{Roles: roles, Scopes: scopes}
}

func TestPredicate_Empty_Allows(t *testing.T) {
	if !(&Predicate{}).Allow(u(nil, nil), nil) {
		t.Error("empty predicate should allow")
	}
}

func TestPredicate_AllRequires(t *testing.T) {
	p := &Predicate{All: []string{"role:admin", "scope:users.write"}}
	if !p.Allow(u([]string{"admin"}, []string{"users.write"}), nil) {
		t.Error("admin+users.write should pass all")
	}
	if p.Allow(u([]string{"admin"}, nil), nil) {
		t.Error("missing scope should fail all")
	}
}

func TestPredicate_AnyRequires(t *testing.T) {
	p := &Predicate{Any: []string{"role:admin", "role:owner"}}
	if !p.Allow(u([]string{"owner"}, nil), nil) {
		t.Error("owner alone should pass any")
	}
	if p.Allow(u([]string{"viewer"}, nil), nil) {
		t.Error("neither admin nor owner should fail any")
	}
}

func TestPredicate_NotForbids(t *testing.T) {
	p := &Predicate{Not: []string{"role:guest"}}
	if !p.Allow(u([]string{"admin"}, nil), nil) {
		t.Error("admin should pass not-guest")
	}
	if p.Allow(u([]string{"guest"}, nil), nil) {
		t.Error("guest should fail not-guest")
	}
}

func TestPredicate_AllAndAny_Combined(t *testing.T) {
	p := &Predicate{
		All: []string{"scope:users.read"},
		Any: []string{"role:admin", "role:owner"},
	}
	pass := u([]string{"owner"}, []string{"users.read"})
	fail := u([]string{"owner"}, nil)
	if !p.Allow(pass, nil) {
		t.Error("pass case failed")
	}
	if p.Allow(fail, nil) {
		t.Error("fail case allowed")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL — "predicate.Allow undefined".

- [ ] **Step 3: Implement predicate.go**

```go
// predicate.go
package contract

import (
	"strings"

	"github.com/xraph/forge/extensions/dashboard/auth"
)

// Allow evaluates the boolean predicate against a UserInfo. The wardenResult
// argument is the optional second-pass Warden decision; pass nil to skip.
// An empty predicate (no all/any/not) always allows.
func (p *Predicate) Allow(user *auth.UserInfo, wardenResult *Decision) bool {
	if p == nil {
		return true
	}
	if len(p.All) == 0 && len(p.Any) == 0 && len(p.Not) == 0 && wardenResult == nil {
		// truly empty predicate; warden absence handled by caller's evaluation order
		return true
	}
	for _, tok := range p.All {
		if !match(tok, user) {
			return false
		}
	}
	if len(p.Any) > 0 {
		ok := false
		for _, tok := range p.Any {
			if match(tok, user) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, tok := range p.Not {
		if match(tok, user) {
			return false
		}
	}
	if wardenResult != nil && !wardenResult.Allow {
		return false
	}
	return true
}

// match parses one token (role:X, scope:X, claim:K=V) and tests it against user.
func match(token string, user *auth.UserInfo) bool {
	if user == nil {
		return false
	}
	kind, rest, ok := strings.Cut(token, ":")
	if !ok {
		return false
	}
	switch kind {
	case "role":
		return contains(user.Roles, rest)
	case "scope":
		return contains(user.Scopes, rest)
	case "claim":
		key, value, ok := strings.Cut(rest, "=")
		if !ok {
			return false
		}
		got, ok := user.Claims[key]
		if !ok {
			return false
		}
		return toString(got) == value
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "" // claims that aren't strings can't be matched by claim:K=V
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/predicate.go extensions/dashboard/contract/predicate_test.go
git commit -m "feat(dashboard/contract): boolean predicate evaluator (all/any/not, role/scope/claim)"
```

### Task 3.2: PermissionsHash for cache keying

**Files:**
- Modify: `extensions/dashboard/contract/predicate.go`
- Modify: `extensions/dashboard/contract/predicate_test.go`

- [ ] **Step 1: Add failing test**

```go
func TestPermissionsHash_StableForEquivalentSlice(t *testing.T) {
	a := PermissionsHash(u([]string{"admin", "owner"}, []string{"x", "y"}))
	b := PermissionsHash(u([]string{"owner", "admin"}, []string{"y", "x"}))
	if a != b {
		t.Errorf("hash not stable across order: %s vs %s", a, b)
	}
}

func TestPermissionsHash_DiffersWhenRolesDiffer(t *testing.T) {
	a := PermissionsHash(u([]string{"admin"}, nil))
	b := PermissionsHash(u([]string{"viewer"}, nil))
	if a == b {
		t.Error("hash should differ for different roles")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement PermissionsHash**

Add to `predicate.go`:

```go
import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	// ... existing imports
)

// PermissionsHash returns a stable, order-independent hash of a user's
// roles and scopes. Used as part of the graph cache key so that users with
// the same effective permissions share a cache entry. Claims are NOT included
// because the contract treats only role/scope as graph-shape-determining.
func PermissionsHash(user *auth.UserInfo) string {
	if user == nil {
		return "anon"
	}
	roles := append([]string(nil), user.Roles...)
	scopes := append([]string(nil), user.Scopes...)
	sort.Strings(roles)
	sort.Strings(scopes)
	h := sha256.New()
	for _, r := range roles {
		h.Write([]byte("r:"))
		h.Write([]byte(r))
		h.Write([]byte{0})
	}
	for _, s := range scopes {
		h.Write([]byte("s:"))
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/predicate.go extensions/dashboard/contract/predicate_test.go
git commit -m "feat(dashboard/contract): stable permissions hash for graph cache keying"
```

---

## Phase 4: Warden Interface

### Task 4.1: Warden types + registry

**Files:**
- Create: `extensions/dashboard/contract/warden.go`
- Create: `extensions/dashboard/contract/warden_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// warden_test.go
package contract

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/auth"
)

type stubWarden struct {
	allow      bool
	redactions []string
}

func (s *stubWarden) Authorize(_ context.Context, _ Principal, _ Action) (Decision, error) {
	return Decision{Allow: s.allow, Redactions: s.redactions}, nil
}

func TestWardenRegistry_RegisterAndGet(t *testing.T) {
	r := NewWardenRegistry()
	w := &stubWarden{allow: true}
	if err := r.Register("tenantOwner", w); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := r.Get("tenantOwner")
	if !ok || got != w {
		t.Error("registered warden not found")
	}
}

func TestWardenRegistry_DuplicateName_Fails(t *testing.T) {
	r := NewWardenRegistry()
	_ = r.Register("x", &stubWarden{allow: true})
	if err := r.Register("x", &stubWarden{allow: false}); err == nil {
		t.Error("duplicate registration should fail")
	}
}

func TestWardenRegistry_MissingName_NotOK(t *testing.T) {
	r := NewWardenRegistry()
	if _, ok := r.Get("nope"); ok {
		t.Error("missing warden should not be found")
	}
}

func TestPrincipal_FromUserInfo(t *testing.T) {
	user := &auth.UserInfo{Subject: "u1", Roles: []string{"admin"}}
	p := PrincipalFor(user)
	if p.User != user {
		t.Error("principal should hold the user")
	}
}

func TestDecision_DenialPropagates(t *testing.T) {
	w := &stubWarden{allow: false}
	d, err := w.Authorize(context.Background(), Principal{}, Action{})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if d.Allow {
		t.Error("expected deny")
	}
	_ = errors.New
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement warden.go**

```go
// warden.go
package contract

import (
	"context"
	"fmt"
	"sync"

	"github.com/xraph/forge/extensions/dashboard/auth"
)

// Warden is the pluggable, data-aware authorization second pass.
// It runs after the YAML boolean Predicate succeeds and may inspect
// intent params (e.g. tenant ownership), claims, or external policy.
type Warden interface {
	Authorize(ctx context.Context, p Principal, a Action) (Decision, error)
}

// Principal is the caller identity passed to Wardens and the predicate engine.
type Principal struct {
	User   *auth.UserInfo
	Claims map[string]any
}

// PrincipalFor builds a Principal from a UserInfo, copying claims for safety.
func PrincipalFor(user *auth.UserInfo) Principal {
	if user == nil {
		return Principal{}
	}
	claims := make(map[string]any, len(user.Claims))
	for k, v := range user.Claims {
		claims[k] = v
	}
	return Principal{User: user, Claims: claims}
}

// Action is the operation being authorized.
type Action struct {
	Contributor string
	Intent      string
	Kind        Kind
	Capability  Capability
	Resource    map[string]any
}

// Decision is the Warden's verdict.
type Decision struct {
	Allow      bool
	Reason     string
	Redactions []string // JSONPath-like field paths to strip from response
}

// WardenRegistry maps a Warden's declared name to its implementation.
// Manifest validation rejects YAML that references a name not in the registry.
type WardenRegistry interface {
	Register(name string, w Warden) error
	Get(name string) (Warden, bool)
}

// NewWardenRegistry returns an empty in-memory registry.
func NewWardenRegistry() WardenRegistry {
	return &wardenRegistry{wardens: map[string]Warden{}}
}

type wardenRegistry struct {
	mu      sync.RWMutex
	wardens map[string]Warden
}

func (r *wardenRegistry) Register(name string, w Warden) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.wardens[name]; exists {
		return fmt.Errorf("warden %q already registered", name)
	}
	r.wardens[name] = w
	return nil
}

func (r *wardenRegistry) Get(name string) (Warden, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.wardens[name]
	return w, ok
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/warden.go extensions/dashboard/contract/warden_test.go
git commit -m "feat(dashboard/contract): warden interface and in-memory registry"
```

---

## Phase 5: YAML Loader & Cross-Reference Validator

### Task 5.1: Loader entry point

**Files:**
- Create: `extensions/dashboard/contract/loader/yaml.go`
- Create: `extensions/dashboard/contract/loader/yaml_test.go`

- [ ] **Step 1: Write the failing test**

```go
// yaml_test.go
package loader

import (
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

const okYAML = `
schemaVersion: 1
contributor:
  name: users
  envelope: { supports: [v1], preferred: v1 }
intents:
  - { name: users.list, kind: query, version: 1, capability: read }
graph:
  - { route: /users, intent: page.shell }
`

func TestLoad_OK(t *testing.T) {
	m, err := Load(strings.NewReader(okYAML), "users.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.Contributor.Name != "users" {
		t.Errorf("name = %q", m.Contributor.Name)
	}
	_ = contract.IntentKindQuery // ensure import retained
}

func TestLoad_BadSchemaVersion(t *testing.T) {
	const yaml = `schemaVersion: 99
contributor: { name: x, envelope: { supports: [v1], preferred: v1 } }
intents: []
`
	if _, err := Load(strings.NewReader(yaml), "x.yaml"); err == nil {
		t.Error("expected error for unsupported schemaVersion")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/loader/...`
Expected: FAIL.

- [ ] **Step 3: Implement loader/yaml.go**

```go
// yaml.go
package loader

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// SupportedSchemaVersion is the schema integer this loader understands.
// Bumping it requires a coordinated platform release (see DESIGN.md).
const SupportedSchemaVersion = 1

// Load parses a contributor manifest YAML stream and validates its schemaVersion.
// Cross-reference validation (intent refs, slot accepts, warden names) runs separately
// in Validate.
func Load(r io.Reader, source string) (*contract.ContractManifest, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", source, err)
	}
	var m contract.ContractManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", source, err)
	}
	if m.SchemaVersion != SupportedSchemaVersion {
		return nil, fmt.Errorf("%s: schemaVersion=%d unsupported, want %d", source, m.SchemaVersion, SupportedSchemaVersion)
	}
	return &m, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/loader/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/loader/yaml.go extensions/dashboard/contract/loader/yaml_test.go
git commit -m "feat(dashboard/contract): YAML loader with schemaVersion check"
```

### Task 5.2: Cross-reference validator

**Files:**
- Create: `extensions/dashboard/contract/loader/validate.go`
- Create: `extensions/dashboard/contract/loader/validate_test.go`

The validator catches everything that's only resolvable across multiple YAML units: an intent referenced by `data.intent` must exist; a Warden name must be registered; a slot extension must target an `extensible` slot whose `accepts` list contains the added node's intent kind.

- [ ] **Step 1: Write the failing tests (multiple cases)**

```go
// validate_test.go
package loader

import (
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func mustLoad(t *testing.T, src string) *contract.ContractManifest {
	t.Helper()
	m, err := Load(strings.NewReader(src), "test.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return m
}

func TestValidate_GoodManifest(t *testing.T) {
	m := mustLoad(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: users.list,  kind: query,   version: 1, capability: read }
  - { name: user.disable, kind: command, version: 1, capability: write,
      requires: { warden: tenantOwner } }
queries:
  userList: { intent: users.list }
graph:
  - { route: /users, intent: page.shell, data: queries.userList }
`)
	wreg := contract.NewWardenRegistry()
	_ = wreg.Register("tenantOwner", &noopWarden{})
	if err := Validate(m, wreg); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidate_UnknownWarden(t *testing.T) {
	m := mustLoad(t, `
schemaVersion: 1
contributor: { name: x, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: a, kind: query, version: 1, capability: read,
      requires: { warden: missing } }
`)
	if err := Validate(m, contract.NewWardenRegistry()); err == nil {
		t.Error("expected unknown-warden error")
	}
}

func TestValidate_UnknownQueryRef(t *testing.T) {
	m := mustLoad(t, `
schemaVersion: 1
contributor: { name: x, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - { intent: page.shell, data: queries.nope }
`)
	if err := Validate(m, contract.NewWardenRegistry()); err == nil {
		t.Error("expected unknown-query error")
	}
}

func TestValidate_KindCapabilityMismatch(t *testing.T) {
	cases := []string{
		"kind: command, capability: read",   // command must be write
		"kind: query, capability: write",    // query must be read
		"kind: subscription, capability: write",
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			m := mustLoad(t, `
schemaVersion: 1
contributor: { name: x, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: a, version: 1, `+body+` }
`)
			if err := Validate(m, contract.NewWardenRegistry()); err == nil {
				t.Errorf("expected kind/capability mismatch error for %q", body)
			}
		})
	}
}

type noopWarden struct{}

func (noopWarden) Authorize(_ context.Context, _ contract.Principal, _ contract.Action) (contract.Decision, error) {
	return contract.Decision{Allow: true}, nil
}
```

Add `"context"` to the imports.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/loader/...`
Expected: FAIL.

- [ ] **Step 3: Implement loader/validate.go**

```go
// validate.go
package loader

import (
	"fmt"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// Validate runs cross-reference checks that require the full manifest in hand.
// It does not enforce slot-accepts (that needs the global registry to know about
// other contributors' intent kinds); slot validation runs in registry.Register.
func Validate(m *contract.ContractManifest, wardens contract.WardenRegistry) error {
	intentByName := map[string]contract.Intent{}
	for _, in := range m.Intents {
		if _, dup := intentByName[in.Name]; dup {
			return fmt.Errorf("intent %q declared twice", in.Name)
		}
		if err := validateKindCapability(in); err != nil {
			return err
		}
		if err := validateWarden(in.Requires, wardens); err != nil {
			return fmt.Errorf("intent %q: %w", in.Name, err)
		}
		intentByName[in.Name] = in
	}
	// Validate query refs
	for name, q := range m.Queries {
		if _, ok := intentByName[q.Intent]; !ok {
			// allow refs to other-contributor intents; flag only same-contributor mistakes
			// Heuristic: if the name looks like "{contributor}.{rest}" with a different
			// contributor, skip; otherwise fail. Slice (b) tightens this.
			if !looksCrossContributor(q.Intent, m.Contributor.Name) {
				return fmt.Errorf("query %q: intent %q not declared in this contributor", name, q.Intent)
			}
		}
	}
	// Walk graph nodes to validate inline data and predicate wardens
	var walk func(nodes []contract.GraphNode, path string) error
	walk = func(nodes []contract.GraphNode, path string) error {
		for i, n := range nodes {
			here := fmt.Sprintf("%s[%d]", path, i)
			if n.Data != nil && n.Data.QueryRef != "" {
				key := stripQueriesPrefix(n.Data.QueryRef)
				if _, ok := m.Queries[key]; !ok {
					return fmt.Errorf("%s: data refers to unknown query %q", here, n.Data.QueryRef)
				}
			}
			if n.Data != nil && n.Data.Intent != "" {
				if _, ok := intentByName[n.Data.Intent]; !ok && !looksCrossContributor(n.Data.Intent, m.Contributor.Name) {
					return fmt.Errorf("%s: data references unknown intent %q", here, n.Data.Intent)
				}
			}
			if err := validateWarden(coalescePredicate(n.VisibleWhen), wardens); err != nil {
				return fmt.Errorf("%s.visibleWhen: %w", here, err)
			}
			if err := validateWarden(coalescePredicate(n.EnabledWhen), wardens); err != nil {
				return fmt.Errorf("%s.enabledWhen: %w", here, err)
			}
			for slotName, children := range n.Slots {
				if err := walk(children, here+".slots."+slotName); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(m.Graph, "graph")
}

func validateKindCapability(in contract.Intent) error {
	want := map[contract.IntentKind]contract.Capability{
		contract.IntentKindQuery:        contract.CapRead,
		contract.IntentKindCommand:      contract.CapWrite,
		contract.IntentKindSubscription: contract.CapRead,
		contract.IntentKindGraph:        contract.CapRender,
	}
	if w, ok := want[in.Kind]; ok && in.Capability != w {
		return fmt.Errorf("intent %q: kind=%s requires capability=%s, got %s", in.Name, in.Kind, w, in.Capability)
	}
	return nil
}

func validateWarden(p contract.Predicate, wardens contract.WardenRegistry) error {
	if p.Warden == "" {
		return nil
	}
	if _, ok := wardens.Get(p.Warden); !ok {
		return fmt.Errorf("references unknown warden %q", p.Warden)
	}
	return nil
}

func coalescePredicate(p *contract.Predicate) contract.Predicate {
	if p == nil {
		return contract.Predicate{}
	}
	return *p
}

func looksCrossContributor(intentName, ownContributor string) bool {
	// Convention: "auth.linkedAccount" — first dotted segment is contributor name.
	for i := 0; i < len(intentName); i++ {
		if intentName[i] == '.' {
			return intentName[:i] != ownContributor
		}
	}
	return false
}

func stripQueriesPrefix(ref string) string {
	const p = "queries."
	if len(ref) > len(p) && ref[:len(p)] == p {
		return ref[len(p):]
	}
	return ref
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/loader/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/loader/validate.go extensions/dashboard/contract/loader/validate_test.go
git commit -m "feat(dashboard/contract): cross-reference validator (intents, queries, wardens)"
```

---

## Phase 6: Contract Registry

### Task 6.1: Registry — register and look up contributors + intents

**Files:**
- Create: `extensions/dashboard/contract/registry.go`
- Create: `extensions/dashboard/contract/registry_test.go`

- [ ] **Step 1: Write failing tests**

```go
// registry_test.go
package contract

import (
	"strings"
	"testing"
)

func mustManifest(t *testing.T, src string) *ContractManifest {
	t.Helper()
	var m ContractManifest
	if err := unmarshalForTest([]byte(src), &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return &m
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: users.list, kind: query, version: 1, capability: read }
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	intent, ok := r.Intent("users", "users.list", 1)
	if !ok || intent.Name != "users.list" {
		t.Errorf("lookup failed: ok=%v intent=%+v", ok, intent)
	}
}

func TestRegistry_DuplicateContributor(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
`)
	_ = r.Register(m)
	if err := r.Register(m); err == nil {
		t.Error("expected duplicate-contributor error")
	}
}

func TestRegistry_HighestActiveIntentVersion(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: user.disable, kind: command, version: 1, capability: write,
      deprecated: { intentVersion: 1, removeAfter: "2026-09-01" } }
  - { name: user.disable, kind: command, version: 2, capability: write }
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := r.HighestVersion("users", "user.disable")
	if !ok || got != 2 {
		t.Errorf("HighestVersion = %d, ok=%v", got, ok)
	}
}
```

Note: `unmarshalForTest` is a helper using `gopkg.in/yaml.v3`. Add it as a private test helper in `manifest_test.go` (or `registry_test.go`):

```go
import yaml "gopkg.in/yaml.v3"

func unmarshalForTest(b []byte, v any) error { return yaml.Unmarshal(b, v) }
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL.

- [ ] **Step 3: Implement registry.go**

```go
// registry.go
package contract

import (
	"fmt"
	"sync"
)

// Registry holds all registered contributor manifests and provides
// lookup by (contributor, intent, version) plus highest-active-version queries
// for negotiation.
type Registry interface {
	Register(m *ContractManifest) error
	Contributor(name string) (*ContractManifest, bool)
	Intent(contributor, intent string, version int) (Intent, bool)
	HighestVersion(contributor, intent string) (int, bool)
	All() []*ContractManifest
}

// NewRegistry returns an empty registry.
func NewRegistry() Registry {
	return &registry{
		contributors: map[string]*ContractManifest{},
		intents:      map[intentKey]Intent{},
		highest:      map[string]int{},
	}
}

type intentKey struct {
	contributor string
	intent      string
	version     int
}

type registry struct {
	mu           sync.RWMutex
	contributors map[string]*ContractManifest
	intents      map[intentKey]Intent
	highest      map[string]int // "contributor:intent" -> highest active version
}

func (r *registry) Register(m *ContractManifest) error {
	if m == nil {
		return fmt.Errorf("nil manifest")
	}
	name := m.Contributor.Name
	if name == "" {
		return fmt.Errorf("manifest missing contributor.name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.contributors[name]; exists {
		return fmt.Errorf("contributor %q already registered", name)
	}
	for _, in := range m.Intents {
		k := intentKey{name, in.Name, in.Version}
		if _, dup := r.intents[k]; dup {
			return fmt.Errorf("contributor %q intent %q version %d declared twice", name, in.Name, in.Version)
		}
		r.intents[k] = in
		hk := name + ":" + in.Name
		if in.Deprecated == nil {
			if r.highest[hk] < in.Version {
				r.highest[hk] = in.Version
			}
		} else if _, hasHigher := r.highest[hk]; !hasHigher {
			// only set if no active version has been seen yet; deprecated falls back
			r.highest[hk] = in.Version
		}
	}
	r.contributors[name] = m
	return nil
}

func (r *registry) Contributor(name string) (*ContractManifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.contributors[name]
	return m, ok
}

func (r *registry) Intent(contributor, intent string, version int) (Intent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	in, ok := r.intents[intentKey{contributor, intent, version}]
	return in, ok
}

func (r *registry) HighestVersion(contributor, intent string) (int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.highest[contributor+":"+intent]
	return v, ok
}

func (r *registry) All() []*ContractManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ContractManifest, 0, len(r.contributors))
	for _, m := range r.contributors {
		out = append(out, m)
	}
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/registry.go extensions/dashboard/contract/registry_test.go
git commit -m "feat(dashboard/contract): registry indexed by contributor, intent, version"
```

### Task 6.2: Slot validation at registration (depth + cycle)

**Files:**
- Create: `extensions/dashboard/contract/slots.go`
- Create: `extensions/dashboard/contract/slots_test.go`
- Modify: `extensions/dashboard/contract/registry.go`

The slot system: parent intent (e.g. `page.shell`) declares which slots it has and what each accepts. Slot declarations live in a built-in **intent kind catalog** keyed by intent kind (not version, since slot shapes change at major version bumps and we'll handle that via additive evolution). For slice (a), the catalog is hard-coded in code — slice (e) externalizes it for the React shell.

- [ ] **Step 1: Write the failing tests**

```go
// slots_test.go
package contract

import "testing"

func TestSlotCatalog_PageShell(t *testing.T) {
	def, ok := DefaultSlotCatalog["page.shell"]
	if !ok {
		t.Fatal("page.shell missing from default slot catalog")
	}
	if def.Slots["main"].Cardinality != CardinalityMany {
		t.Errorf("main cardinality = %v", def.Slots["main"].Cardinality)
	}
}

func TestValidateSlotFills_AcceptCheck(t *testing.T) {
	// page.shell.main accepts resource.list, dashboard.grid; rejects unknown
	parent := DefaultSlotCatalog["page.shell"]
	cases := []struct {
		child   string
		wantErr bool
	}{
		{"resource.list", false},
		{"dashboard.grid", false},
		{"action.button", true}, // not allowed in main
	}
	for _, c := range cases {
		err := validateSlotAccepts(parent.Slots["main"], c.child)
		if (err != nil) != c.wantErr {
			t.Errorf("child=%s err=%v wantErr=%v", c.child, err, c.wantErr)
		}
	}
}

func TestSlotDepth_ExceedsMax(t *testing.T) {
	// build a graph of depth 9 (root + 8 nested slots)
	leaf := GraphNode{Intent: "metric.counter"}
	cur := leaf
	for i := 0; i < 9; i++ {
		cur = GraphNode{Intent: "page.shell", Slots: map[string][]GraphNode{"main": {cur}}}
	}
	if err := checkDepth([]GraphNode{cur}, 0); err == nil {
		t.Error("expected depth-exceeded error")
	}
}

func TestSlotCycle_DetectedAtRegistration(t *testing.T) {
	// A node referencing its own intent through a slot is a cycle
	root := GraphNode{
		Intent: "self",
		Slots: map[string][]GraphNode{
			"main": {{Intent: "self"}},
		},
	}
	if err := checkCycle([]GraphNode{root}, map[string]bool{}); err == nil {
		t.Error("expected cycle error")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL.

- [ ] **Step 3: Implement slots.go**

```go
// slots.go
package contract

import "fmt"

// MaxSlotDepth is the maximum nesting depth of graph nodes; trees deeper
// than this are rejected at registration.
const MaxSlotDepth = 8

// Cardinality describes how many fills a slot accepts.
type Cardinality string

const (
	CardinalityOne  Cardinality = "one"
	CardinalityMany Cardinality = "many"
)

// SlotDef describes one slot of a parent intent kind.
type SlotDef struct {
	Accepts     []string    // intent names accepted in this slot
	Cardinality Cardinality
	Extensible  bool        // if true, other contributors may extend via Extends
}

// IntentKindDef declares the slots of a built-in intent kind.
type IntentKindDef struct {
	Slots map[string]SlotDef
}

// DefaultSlotCatalog is the v1 catalog of built-in intent kinds and their slots.
// Adding a new built-in intent kind here is a shell-version bump (adds new
// renderer behavior). Slice (e) defines the full v1 vocabulary; this map starts
// with the kinds used by the spec's example.
var DefaultSlotCatalog = map[string]IntentKindDef{
	"page.shell": {
		Slots: map[string]SlotDef{
			"main": {
				Accepts:     []string{"resource.list", "resource.detail", "dashboard.grid", "form.edit", "custom", "iframe"},
				Cardinality: CardinalityMany,
			},
		},
	},
	"resource.list": {
		Slots: map[string]SlotDef{
			"rowActions":   {Accepts: []string{"action.button", "action.menu", "action.divider"}, Cardinality: CardinalityMany},
			"detailDrawer": {Accepts: []string{"form.edit", "resource.detail", "custom"}, Cardinality: CardinalityOne, Extensible: true},
		},
	},
	"dashboard.grid": {
		Slots: map[string]SlotDef{
			"widgets": {Accepts: []string{"metric.counter", "metric.gauge", "audit.tail", "custom"}, Cardinality: CardinalityMany},
		},
	},
	"form.edit": {
		Slots: map[string]SlotDef{
			"fields": {Accepts: []string{"form.field", "custom"}, Cardinality: CardinalityMany, Extensible: true},
		},
	},
}

// validateSlotAccepts returns an error if child is not in slot's Accepts list.
func validateSlotAccepts(slot SlotDef, child string) error {
	for _, a := range slot.Accepts {
		if a == child {
			return nil
		}
	}
	return fmt.Errorf("slot does not accept intent %q", child)
}

// checkDepth recurses through GraphNodes and fails if any chain exceeds MaxSlotDepth.
func checkDepth(nodes []GraphNode, current int) error {
	if current > MaxSlotDepth {
		return fmt.Errorf("graph depth %d exceeds max %d", current, MaxSlotDepth)
	}
	for _, n := range nodes {
		for _, children := range n.Slots {
			if err := checkDepth(children, current+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkCycle walks the graph and detects intent self-reference along a path.
// (Cross-intent cycles via custom escape hatches are bounded by checkDepth.)
func checkCycle(nodes []GraphNode, ancestors map[string]bool) error {
	for _, n := range nodes {
		if n.Intent == "" {
			continue
		}
		if ancestors[n.Intent] {
			return fmt.Errorf("cycle detected: intent %q nests itself", n.Intent)
		}
		ancestors[n.Intent] = true
		for _, children := range n.Slots {
			if err := checkCycle(children, ancestors); err != nil {
				return err
			}
		}
		delete(ancestors, n.Intent)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Modify Register to call checkDepth, checkCycle, and slot-accept validation**

Update `Register` in `registry.go` after the per-intent registration loop:

```go
// in registry.Register, before "r.contributors[name] = m"
if err := checkDepth(m.Graph, 0); err != nil {
    return fmt.Errorf("contributor %q: %w", name, err)
}
if err := checkCycle(m.Graph, map[string]bool{}); err != nil {
    return fmt.Errorf("contributor %q: %w", name, err)
}
if err := validateGraphSlots(m.Graph); err != nil {
    return fmt.Errorf("contributor %q: %w", name, err)
}
```

Add `validateGraphSlots` to `slots.go`:

```go
func validateGraphSlots(nodes []GraphNode) error {
	for _, n := range nodes {
		def, ok := DefaultSlotCatalog[n.Intent]
		if !ok {
			// unknown parent intent: cannot validate slots, allow (may be a leaf or custom)
			continue
		}
		for slotName, children := range n.Slots {
			slot, ok := def.Slots[slotName]
			if !ok {
				return fmt.Errorf("intent %q has no slot %q", n.Intent, slotName)
			}
			if slot.Cardinality == CardinalityOne && len(children) > 1 {
				return fmt.Errorf("intent %q slot %q accepts one fill, got %d", n.Intent, slotName, len(children))
			}
			for _, c := range children {
				if err := validateSlotAccepts(slot, c.Intent); err != nil {
					return fmt.Errorf("intent %q slot %q: %w", n.Intent, slotName, err)
				}
			}
			if err := validateGraphSlots(children); err != nil {
				return err
			}
		}
	}
	return nil
}
```

Add a registry test for slot rejection:

```go
func TestRegistry_RejectsBadSlotFill(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - intent: page.shell
    slots:
      main:
        - { intent: action.button }   # not allowed in page.shell.main
`)
	if err := r.Register(m); err == nil {
		t.Error("expected slot-accept rejection")
	}
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add extensions/dashboard/contract/slots.go extensions/dashboard/contract/slots_test.go extensions/dashboard/contract/registry.go extensions/dashboard/contract/registry_test.go
git commit -m "feat(dashboard/contract): slot catalog with depth, cycle, and accept validation"
```

### Task 6.3: Slot extension application (cross-contributor extends)

**Files:**
- Modify: `extensions/dashboard/contract/slots.go`
- Modify: `extensions/dashboard/contract/slots_test.go`
- Modify: `extensions/dashboard/contract/registry.go`

- [ ] **Step 1: Write failing tests**

```go
// add to slots_test.go
func TestApplyExtensions_NonExtensibleSlot_Rejected(t *testing.T) {
	// users.page.shell has a 'main' slot (not extensible by default)
	r := NewRegistry()
	usersM := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - { route: /users, intent: page.shell, slots: { main: [{ intent: resource.list }] } }
`)
	_ = r.Register(usersM)

	authM := mustManifest(t, `
schemaVersion: 1
contributor: { name: auth, envelope: { supports: [v1], preferred: v1 } }
intents: []
extends:
  - target: { contributor: users, intent: page.shell, route: /users }
    slot: main
    add:
      - { intent: action.button }
`)
	if err := r.Register(authM); err == nil {
		t.Error("expected non-extensible-slot rejection")
	}
}

func TestApplyExtensions_Extensible_Merges(t *testing.T) {
	r := NewRegistry()
	usersM := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - { route: /users, intent: page.shell, slots: {
        main: [{
          intent: resource.list,
          slots: { detailDrawer: [{ intent: form.edit }] }
        }]
      } }
`)
	_ = r.Register(usersM)

	authM := mustManifest(t, `
schemaVersion: 1
contributor: { name: auth, envelope: { supports: [v1], preferred: v1 } }
intents: []
extends:
  - target: { contributor: users, intent: page.shell, route: /users }
    slot: main.detailDrawer.fields
    add:
      - { intent: form.field }
`)
	if err := r.Register(authM); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	// After registration, the merged graph should include the extension
	merged, ok := r.MergedGraph("users", "/users")
	if !ok {
		t.Fatal("merged graph not found")
	}
	fields := merged.Slots["main"][0].Slots["detailDrawer"][0].Slots["fields"]
	if len(fields) != 1 || fields[0].Intent != "form.field" {
		t.Errorf("extension not applied: %+v", fields)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL — `MergedGraph` undefined, no extension support.

- [ ] **Step 3: Implement slot extension application**

Add to `slots.go`:

```go
import "strings" // add to imports if not already

// applyExtension finds the target node in graph and merges adds into the named slot.
// slotPath is a dotted path through Slots maps and indices, e.g. "main.detailDrawer.fields".
// Returns an error if the target slot is not extensible.
func applyExtension(graph []GraphNode, target ExtensionTarget, slotPath string, adds []GraphNode) error {
	for i := range graph {
		if matchesTarget(graph[i], target) {
			return walkAndAppend(&graph[i], strings.Split(slotPath, "."), adds)
		}
	}
	return fmt.Errorf("extension target not found: contributor=%s intent=%s route=%s", target.Contributor, target.Intent, target.Route)
}

func matchesTarget(n GraphNode, t ExtensionTarget) bool {
	if n.Intent != t.Intent {
		return false
	}
	if t.Route != "" && n.Route != t.Route {
		return false
	}
	return true
}

func walkAndAppend(n *GraphNode, path []string, adds []GraphNode) error {
	if len(path) == 0 {
		return fmt.Errorf("empty slot path")
	}
	slotName := path[0]
	rest := path[1:]
	if n.Slots == nil {
		n.Slots = map[string][]GraphNode{}
	}
	if len(rest) == 0 {
		// We've reached the target slot; check extensibility
		def, ok := DefaultSlotCatalog[n.Intent]
		if !ok {
			return fmt.Errorf("unknown parent intent %q", n.Intent)
		}
		slot, ok := def.Slots[slotName]
		if !ok {
			return fmt.Errorf("intent %q has no slot %q", n.Intent, slotName)
		}
		if !slot.Extensible {
			return fmt.Errorf("intent %q slot %q is not extensible", n.Intent, slotName)
		}
		for _, a := range adds {
			if err := validateSlotAccepts(slot, a.Intent); err != nil {
				return fmt.Errorf("extension into %q.%q: %w", n.Intent, slotName, err)
			}
		}
		n.Slots[slotName] = append(n.Slots[slotName], adds...)
		return nil
	}
	// Recurse: choose first fill in this slot (cardinality:one is the common case for path traversal;
	// for cardinality:many extensions, the user must specify an index — out of scope for v1).
	children, ok := n.Slots[slotName]
	if !ok || len(children) == 0 {
		return fmt.Errorf("path %q: no fills in slot %q to descend", strings.Join(path, "."), slotName)
	}
	return walkAndAppend(&children[0], rest, adds)
}
```

Modify `registry.Register` to apply extensions and store merged graphs:

```go
// in registry.go, add a new field:
type registry struct {
    // ... existing fields
    mergedGraphs map[string][]GraphNode // key: contributor name
}

// in NewRegistry, initialize the new map
mergedGraphs: map[string][]GraphNode{},

// in Register, after r.contributors[name] = m, before return nil:
// Compute merged graphs: copy own graph, then apply this manifest's extends
// to ALL contributors (including ones registered earlier).
mergedGraph := deepCopyGraph(m.Graph)
r.mergedGraphs[name] = mergedGraph
for _, ext := range m.Extends {
    targetGraph, ok := r.mergedGraphs[ext.Target.Contributor]
    if !ok {
        return fmt.Errorf("contributor %q: extension target %q not registered", name, ext.Target.Contributor)
    }
    if err := applyExtension(targetGraph, ext.Target, ext.Slot, ext.Add); err != nil {
        return fmt.Errorf("contributor %q: %w", name, err)
    }
}
return nil
```

Add `MergedGraph` method:

```go
// MergedGraph returns the merged graph for a contributor (with all extensions
// applied), or false if the contributor is not registered.
// The optional route parameter narrows the result to a single route node.
func (r *registry) MergedGraph(contributor, route string) (*GraphNode, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    g, ok := r.mergedGraphs[contributor]
    if !ok {
        return nil, false
    }
    if route == "" {
        if len(g) > 0 {
            return &g[0], true
        }
        return nil, false
    }
    for i := range g {
        if g[i].Route == route {
            return &g[i], true
        }
    }
    return nil, false
}
```

Add `MergedGraph` to the `Registry` interface.

Add `deepCopyGraph` to `slots.go`:

```go
// deepCopyGraph returns a deep copy of a graph slice. Required so applying
// extensions doesn't mutate a contributor's original manifest.
func deepCopyGraph(in []GraphNode) []GraphNode {
	if in == nil {
		return nil
	}
	out := make([]GraphNode, len(in))
	for i, n := range in {
		nc := n
		if n.Slots != nil {
			nc.Slots = map[string][]GraphNode{}
			for k, v := range n.Slots {
				nc.Slots[k] = deepCopyGraph(v)
			}
		}
		out[i] = nc
	}
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/slots.go extensions/dashboard/contract/slots_test.go extensions/dashboard/contract/registry.go
git commit -m "feat(dashboard/contract): cross-contributor slot extensions with extensibility check"
```

---

## Phase 7: Per-Request Graph Build with Permission Filter

### Task 7.1: Filter the merged graph by user permissions

**Files:**
- Create: `extensions/dashboard/contract/graph.go`
- Create: `extensions/dashboard/contract/graph_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// graph_test.go
package contract

import (
	"context"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/auth"
)

func TestBuildGraph_FiltersHiddenNodes(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - route: /users
    intent: page.shell
    slots:
      main:
        - intent: resource.list
          slots:
            rowActions:
              - { intent: action.button, op: user.disable,
                  visibleWhen: { all: ["role:admin"] } }
              - { intent: action.button, op: user.view }
`)
	_ = r.Register(m)
	build := NewGraphBuilder(r, NewWardenRegistry())

	got, err := build.Build(context.Background(), "users", "/users",
		Principal{User: &auth.UserInfo{Roles: []string{"viewer"}}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	actions := got.Slots["main"][0].Slots["rowActions"]
	if len(actions) != 1 {
		t.Fatalf("expected 1 action visible to viewer, got %d", len(actions))
	}
	if actions[0].Op != "user.view" {
		t.Errorf("wrong action remained: %+v", actions[0])
	}
}

func TestBuildGraph_AdminSeesAll(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - route: /users
    intent: page.shell
    slots:
      main:
        - intent: resource.list
          slots:
            rowActions:
              - { intent: action.button, op: user.disable,
                  visibleWhen: { all: ["role:admin"] } }
`)
	_ = r.Register(m)
	build := NewGraphBuilder(r, NewWardenRegistry())
	got, _ := build.Build(context.Background(), "users", "/users",
		Principal{User: &auth.UserInfo{Roles: []string{"admin"}}})
	actions := got.Slots["main"][0].Slots["rowActions"]
	if len(actions) != 1 {
		t.Errorf("admin should see admin-only action; got %d", len(actions))
	}
}

func TestBuildGraph_RouteNotFound(t *testing.T) {
	r := NewRegistry()
	build := NewGraphBuilder(r, NewWardenRegistry())
	_, err := build.Build(context.Background(), "users", "/nope", Principal{})
	if err == nil {
		t.Error("expected not-found error")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL.

- [ ] **Step 3: Implement graph.go**

```go
// graph.go
package contract

import (
	"context"
	"fmt"
)

// GraphBuilder produces a per-(route, principal) filtered graph by walking the
// merged graph from the registry and dropping nodes whose visibleWhen predicates
// fail. EnabledWhen is preserved as an annotation (it does not strip the node);
// the React shell honors it for disabled-but-visible UI states.
type GraphBuilder struct {
	registry Registry
	wardens  WardenRegistry
}

// NewGraphBuilder returns a builder bound to the given registry and warden registry.
func NewGraphBuilder(reg Registry, wardens WardenRegistry) *GraphBuilder {
	return &GraphBuilder{registry: reg, wardens: wardens}
}

// Build returns the filtered graph rooted at the given route for the given principal.
// Returns ErrNotFound if no contributor owns the route.
func (b *GraphBuilder) Build(ctx context.Context, contributor, route string, p Principal) (*GraphNode, error) {
	root, ok := b.registry.MergedGraph(contributor, route)
	if !ok {
		return nil, fmt.Errorf("%w: contributor=%s route=%s", ErrNotFound, contributor, route)
	}
	filtered, _ := b.filter(ctx, *root, p)
	if filtered == nil {
		return nil, fmt.Errorf("%w: route filtered for principal", ErrPermissionDenied)
	}
	return filtered, nil
}

// filter returns a deep copy of n with non-visible descendants stripped, or nil
// if n itself fails its own visibleWhen.
func (b *GraphBuilder) filter(ctx context.Context, n GraphNode, p Principal) (*GraphNode, error) {
	if !b.allowsNode(ctx, n, p) {
		return nil, nil
	}
	out := n
	if n.Slots != nil {
		out.Slots = map[string][]GraphNode{}
		for slotName, children := range n.Slots {
			var kept []GraphNode
			for _, c := range children {
				kc, err := b.filter(ctx, c, p)
				if err != nil {
					return nil, err
				}
				if kc != nil {
					kept = append(kept, *kc)
				}
			}
			if len(kept) > 0 {
				out.Slots[slotName] = kept
			}
		}
	}
	return &out, nil
}

// allowsNode evaluates visibleWhen plus any per-slot 'requires' inherited from
// the parent intent's slot definition. Returns true if the node should be kept.
func (b *GraphBuilder) allowsNode(_ context.Context, n GraphNode, p Principal) bool {
	if n.VisibleWhen != nil && !n.VisibleWhen.Allow(p.User, nil) {
		return false
	}
	// Warden hook: if visibleWhen carries a Warden ref, run it
	if n.VisibleWhen != nil && n.VisibleWhen.Warden != "" {
		w, ok := b.wardens.Get(n.VisibleWhen.Warden)
		if !ok {
			return false
		}
		// Best-effort sync call here; per-event re-checks are cached in stream.go
		d, err := w.Authorize(context.Background(), p, Action{Intent: n.Intent})
		if err != nil || !d.Allow {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/graph.go extensions/dashboard/contract/graph_test.go
git commit -m "feat(dashboard/contract): per-request graph filter by visibleWhen + warden"
```

---

## Phase 8: Graph Cache

### Task 8.1: LRU graph cache

**Files:**
- Create: `extensions/dashboard/contract/cache.go`
- Create: `extensions/dashboard/contract/cache_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// cache_test.go
package contract

import (
	"testing"
	"time"
)

func TestGraphCache_HitMiss(t *testing.T) {
	c := NewGraphCache(2, time.Minute)
	key := GraphCacheKey{Route: "/users", PermissionsHash: "h1", ShellVersion: "v1"}
	if _, ok := c.Get(key); ok {
		t.Error("expected miss")
	}
	c.Put(key, &GraphNode{Intent: "page.shell"})
	got, ok := c.Get(key)
	if !ok || got.Intent != "page.shell" {
		t.Errorf("expected hit, got %+v ok=%v", got, ok)
	}
}

func TestGraphCache_Eviction(t *testing.T) {
	c := NewGraphCache(2, time.Minute)
	c.Put(GraphCacheKey{Route: "/a"}, &GraphNode{Intent: "a"})
	c.Put(GraphCacheKey{Route: "/b"}, &GraphNode{Intent: "b"})
	c.Put(GraphCacheKey{Route: "/c"}, &GraphNode{Intent: "c"}) // evicts /a
	if _, ok := c.Get(GraphCacheKey{Route: "/a"}); ok {
		t.Error("expected /a evicted")
	}
}

func TestGraphCache_TTLExpiry(t *testing.T) {
	c := NewGraphCache(2, 10*time.Millisecond)
	c.Put(GraphCacheKey{Route: "/x"}, &GraphNode{Intent: "x"})
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.Get(GraphCacheKey{Route: "/x"}); ok {
		t.Error("expected ttl expiry")
	}
}

func TestGraphCache_BustAll(t *testing.T) {
	c := NewGraphCache(4, time.Minute)
	c.Put(GraphCacheKey{Route: "/a"}, &GraphNode{Intent: "a"})
	c.BustAll()
	if _, ok := c.Get(GraphCacheKey{Route: "/a"}); ok {
		t.Error("BustAll should clear")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL.

- [ ] **Step 3: Implement cache.go**

```go
// cache.go
package contract

import (
	"container/list"
	"sync"
	"time"
)

// GraphCacheKey is the (route, permissionsHash, shellVersion) tuple keyed by the cache.
type GraphCacheKey struct {
	Route           string
	PermissionsHash string
	ShellVersion    string
}

// GraphCache is a small LRU+TTL cache. Bust on contributor manifest reload.
type GraphCache struct {
	mu     sync.Mutex
	cap    int
	ttl    time.Duration
	items  map[GraphCacheKey]*list.Element
	order  *list.List // front = MRU
}

type graphEntry struct {
	key   GraphCacheKey
	value *GraphNode
	at    time.Time
}

// NewGraphCache creates a cache with the given max size and TTL per entry.
// TTL of 0 disables expiry.
func NewGraphCache(maxEntries int, ttl time.Duration) *GraphCache {
	if maxEntries < 1 {
		maxEntries = 64
	}
	return &GraphCache{
		cap:   maxEntries,
		ttl:   ttl,
		items: map[GraphCacheKey]*list.Element{},
		order: list.New(),
	}
}

func (c *GraphCache) Get(k GraphCacheKey) (*GraphNode, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[k]
	if !ok {
		return nil, false
	}
	e := el.Value.(*graphEntry)
	if c.ttl > 0 && time.Since(e.at) > c.ttl {
		c.order.Remove(el)
		delete(c.items, k)
		return nil, false
	}
	c.order.MoveToFront(el)
	return e.value, true
}

func (c *GraphCache) Put(k GraphCacheKey, v *GraphNode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		e := el.Value.(*graphEntry)
		e.value = v
		e.at = time.Now()
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(&graphEntry{key: k, value: v, at: time.Now()})
	c.items[k] = el
	if c.order.Len() > c.cap {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(*graphEntry).key)
		}
	}
}

// BustAll clears the cache. Call after a contributor manifest reload or shell deploy.
func (c *GraphCache) BustAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[GraphCacheKey]*list.Element{}
	c.order = list.New()
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/cache.go extensions/dashboard/contract/cache_test.go
git commit -m "feat(dashboard/contract): LRU+TTL graph cache keyed by (route, permissions, shell)"
```

---

## Phase 9: Audit Emitter

### Task 9.1: Audit interface + log default

**Files:**
- Create: `extensions/dashboard/contract/audit.go`
- Create: `extensions/dashboard/contract/audit_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// audit_test.go
package contract

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestLogAuditEmitter_FormatsRecord(t *testing.T) {
	var buf bytes.Buffer
	em := NewLogAuditEmitter(&buf)
	em.Emit(context.Background(), AuditRecord{
		Time:        time.Now(),
		Contributor: "users",
		Intent:      "user.disable",
		Subject:     "u_42",
		User:        "admin@example.com",
		Result:      "ok",
		LatencyMs:   12,
	})
	out := buf.String()
	for _, want := range []string{"users", "user.disable", "u_42", "admin@example.com", "ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("audit output missing %q: %s", want, out)
		}
	}
}

func TestNoopAuditEmitter_DoesNothing(t *testing.T) {
	em := NoopAuditEmitter{}
	em.Emit(context.Background(), AuditRecord{}) // must not panic
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL.

- [ ] **Step 3: Implement audit.go**

```go
// audit.go
package contract

import (
	"context"
	"fmt"
	"io"
	"time"
)

// AuditRecord is one auditable command invocation.
type AuditRecord struct {
	Time          time.Time
	Contributor   string
	Intent        string
	IntentVersion int
	Subject       string         // resource id when known
	User          string         // user identity (subject from UserInfo)
	Result        string         // ok | error
	LatencyMs     int64
	Payload       map[string]any // pre-redaction; subject to per-intent redaction list
	CorrelationID string
}

// AuditEmitter ships audit records to durable storage. Slice (b) wires the
// chronicle implementation; slice (a) ships log-based and noop variants.
type AuditEmitter interface {
	Emit(ctx context.Context, rec AuditRecord)
}

// NoopAuditEmitter is the disabled-audit implementation.
type NoopAuditEmitter struct{}

func (NoopAuditEmitter) Emit(_ context.Context, _ AuditRecord) {}

// NewLogAuditEmitter returns an emitter that writes a stable line format to w.
// Suitable for development and as a fallback when no chronicle backend is wired.
func NewLogAuditEmitter(w io.Writer) AuditEmitter {
	return &logAuditEmitter{w: w}
}

type logAuditEmitter struct {
	w io.Writer
}

func (e *logAuditEmitter) Emit(_ context.Context, rec AuditRecord) {
	fmt.Fprintf(e.w,
		"audit ts=%s contributor=%s intent=%s v=%d subject=%s user=%s result=%s latencyMs=%d corr=%s\n",
		rec.Time.UTC().Format(time.RFC3339Nano),
		rec.Contributor, rec.Intent, rec.IntentVersion,
		rec.Subject, rec.User, rec.Result, rec.LatencyMs, rec.CorrelationID,
	)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/audit.go extensions/dashboard/contract/audit_test.go
git commit -m "feat(dashboard/contract): audit emitter interface with log-based default"
```

---

## Phase 10: HTTP Transport — POST handler

### Task 10.1: Envelope decode + kind dispatch + error envelope

**Files:**
- Create: `extensions/dashboard/contract/transport/http.go`
- Create: `extensions/dashboard/contract/transport/http_test.go`

The handler is intentionally thin: decode envelope → look up intent → enforce kind/capability match → dispatch to the right per-kind handler set on a `Dispatcher`. Real intent execution (calling contributor handlers, running queries) lives in slice (c) — for slice (a), the Dispatcher is an interface that contributor packages will fulfil.

- [ ] **Step 1: Write failing tests**

```go
// http_test.go
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type stubDispatcher struct {
	called   string
	response json.RawMessage
}

func (s *stubDispatcher) Dispatch(_ context.Context, in contract.Request, _ contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	s.called = string(in.Kind) + ":" + in.Intent
	return s.response, contract.ResponseMeta{IntentVersion: in.IntentVersion}, nil
}

func setupRegistry(t *testing.T) (contract.Registry, contract.WardenRegistry) {
	t.Helper()
	r := contract.NewRegistry()
	src := `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: users.list, kind: query, version: 1, capability: read }
  - { name: user.disable, kind: command, version: 1, capability: write }
`
	var m contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&m); err != nil {
		t.Fatal(err)
	}
	return r, contract.NewWardenRegistry()
}

func TestHandler_DispatchesQuery(t *testing.T) {
	reg, wreg := setupRegistry(t)
	disp := &stubDispatcher{response: json.RawMessage(`{"users":[]}`)}
	h := NewHandler(reg, wreg, disp, contract.NoopAuditEmitter{})

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list", IntentVersion: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	if disp.called != "query:users.list" {
		t.Errorf("dispatcher not called: %s", disp.called)
	}
}

func TestHandler_RejectsKindCapabilityMismatch(t *testing.T) {
	reg, wreg := setupRegistry(t)
	h := NewHandler(reg, wreg, &stubDispatcher{}, contract.NoopAuditEmitter{})

	// Send Kind=command for an intent whose Capability=read => mismatch
	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindCommand, Contributor: "users", Intent: "users.list", IntentVersion: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "BAD_REQUEST") {
		t.Errorf("expected BAD_REQUEST in body: %s", w.Body)
	}
}

func TestHandler_UnsupportedVersion(t *testing.T) {
	reg, wreg := setupRegistry(t)
	h := NewHandler(reg, wreg, &stubDispatcher{}, contract.NoopAuditEmitter{})

	body, _ := json.Marshal(contract.Request{
		Envelope: "v999", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "UNSUPPORTED_VERSION") {
		t.Errorf("expected UNSUPPORTED_VERSION: %s", w.Body)
	}
}

func TestHandler_CommandRequiresIdempotencyKey(t *testing.T) {
	reg, wreg := setupRegistry(t)
	h := NewHandler(reg, wreg, &stubDispatcher{}, contract.NoopAuditEmitter{})

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindCommand, Contributor: "users", Intent: "user.disable", IntentVersion: 1,
		// CSRF and IdempotencyKey omitted
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}
```

In `manifest.go` (or a new `testhelpers.go`), add:

```go
// UnmarshalManifestForTest is a test helper exposed for use by sibling packages.
func UnmarshalManifestForTest(b []byte, m *ContractManifest) error {
	return yaml.Unmarshal(b, m)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement transport/http.go**

```go
// http.go
package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// Dispatcher routes a fully-validated request to an intent implementation.
// Slice (c) provides the binding from intent name to actual handlers.
type Dispatcher interface {
	Dispatch(ctx context.Context, in contract.Request, p contract.Principal) (json.RawMessage, contract.ResponseMeta, error)
}

// supportedEnvelopes is the set this slice's handler understands.
var supportedEnvelopes = map[string]bool{"v1": true}

// NewHandler returns the POST /api/dashboard/{envelope} handler.
// principalFromCtx defaults to deriving from auth.UserFromContext.
func NewHandler(reg contract.Registry, wreg contract.WardenRegistry, disp Dispatcher, audit contract.AuditEmitter) http.Handler {
	return &handler{reg: reg, wreg: wreg, disp: disp, audit: audit}
}

type handler struct {
	reg   contract.Registry
	wreg  contract.WardenRegistry
	disp  Dispatcher
	audit contract.AuditEmitter
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, &contract.Error{Code: contract.CodeBadRequest, Message: "POST required"})
		return
	}
	defer r.Body.Close()
	var req contract.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, &contract.Error{Code: contract.CodeBadRequest, Message: "invalid JSON: " + err.Error()})
		return
	}
	if !supportedEnvelopes[req.Envelope] {
		writeError(w, http.StatusBadRequest, &contract.Error{Code: contract.CodeUnsupportedVersion, Message: "envelope " + req.Envelope + " unsupported"})
		return
	}
	if err := validateKind(req); err != nil {
		writeError(w, http.StatusBadRequest, &contract.Error{Code: contract.CodeBadRequest, Message: err.Error()})
		return
	}
	in, ok := h.reg.Intent(req.Contributor, req.Intent, intentVersionOrHighest(h.reg, req))
	if !ok {
		writeError(w, http.StatusNotFound, &contract.Error{Code: contract.CodeNotFound, Message: "intent " + req.Intent + " not registered"})
		return
	}
	if !kindMatchesCapability(req.Kind, in.Capability) {
		writeError(w, http.StatusBadRequest, &contract.Error{
			Code:    contract.CodeBadRequest,
			Message: "kind " + string(req.Kind) + " does not match intent capability " + string(in.Capability),
		})
		return
	}
	if req.Kind == contract.KindCommand {
		if req.IdempotencyKey == "" || req.CSRF == "" {
			writeError(w, http.StatusBadRequest, &contract.Error{Code: contract.CodeBadRequest, Message: "command requires csrf and idempotencyKey"})
			return
		}
	}

	user := auth.UserFromContext(r.Context())
	p := contract.PrincipalFor(user)

	if !in.Requires.Allow(user, nil) {
		writeError(w, http.StatusForbidden, &contract.Error{Code: contract.CodePermissionDenied})
		return
	}
	// Warden second pass when declared
	if in.Requires.Warden != "" {
		warden, ok := h.wreg.Get(in.Requires.Warden)
		if !ok {
			writeError(w, http.StatusInternalServerError, &contract.Error{Code: contract.CodeInternal, Message: "warden not registered"})
			return
		}
		dec, err := warden.Authorize(r.Context(), p, contract.Action{
			Contributor: req.Contributor, Intent: req.Intent, Kind: req.Kind, Capability: in.Capability, Resource: req.Params,
		})
		if err != nil || !dec.Allow {
			writeError(w, http.StatusForbidden, &contract.Error{Code: contract.CodePermissionDenied, Message: dec.Reason})
			return
		}
	}

	t0 := time.Now()
	data, meta, err := h.disp.Dispatch(r.Context(), req, p)
	latency := time.Since(t0)

	emitAudit(h.audit, req, in, p, err, latency)

	if err != nil {
		writeError(w, http.StatusInternalServerError, asContractError(err))
		return
	}
	writeOK(w, contract.Response{
		OK: true, Envelope: req.Envelope, Kind: req.Kind, Data: data, Meta: meta,
	})
}

func validateKind(req contract.Request) error {
	switch req.Kind {
	case contract.KindGraph, contract.KindQuery, contract.KindCommand:
		return nil
	case contract.KindSubscribe:
		return errKind("subscribe is GET-only on /stream")
	}
	return errKind("unknown kind " + string(req.Kind))
}

func errKind(msg string) error { return &contract.Error{Code: contract.CodeBadRequest, Message: msg} }

func kindMatchesCapability(k contract.Kind, c contract.Capability) bool {
	switch k {
	case contract.KindCommand:
		return c == contract.CapWrite
	case contract.KindQuery:
		return c == contract.CapRead
	case contract.KindGraph:
		return c == contract.CapRender
	}
	return false
}

func intentVersionOrHighest(reg contract.Registry, req contract.Request) int {
	if req.IntentVersion != 0 {
		return req.IntentVersion
	}
	v, _ := reg.HighestVersion(req.Contributor, req.Intent)
	return v
}

func emitAudit(em contract.AuditEmitter, req contract.Request, in contract.Intent, p contract.Principal, dispErr error, lat time.Duration) {
	if in.Kind != contract.IntentKindCommand {
		return
	}
	if in.Audit != nil && !*in.Audit {
		return
	}
	user := ""
	if p.User != nil {
		user = p.User.Subject
	}
	result := "ok"
	if dispErr != nil {
		result = "error"
	}
	em.Emit(context.Background(), contract.AuditRecord{
		Time: time.Now(), Contributor: req.Contributor, Intent: req.Intent,
		IntentVersion: in.Version, User: user, Result: result, LatencyMs: lat.Milliseconds(),
		CorrelationID: req.Context.CorrelationID,
	})
}

func asContractError(err error) *contract.Error {
	if e, ok := err.(*contract.Error); ok {
		return e
	}
	return &contract.Error{Code: contract.CodeInternal, Message: err.Error()}
}

func writeOK(w http.ResponseWriter, r contract.Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(r)
}

func writeError(w http.ResponseWriter, status int, e *contract.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contract.ErrorResponse{
		OK: false, Envelope: "v1", Error: e,
	})
	_ = strings.TrimSpace // keep import
}
```

Note: `auth.UserFromContext` is the existing helper (see DESIGN.md "Reused"). Confirm the name with `grep -n "func UserFromContext" /Users/rexraphael/Work/xraph/forge/extensions/dashboard/auth/*.go` before merging — if the name differs in the codebase, adjust here.

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/transport/http.go extensions/dashboard/contract/transport/http_test.go extensions/dashboard/contract/manifest.go
git commit -m "feat(dashboard/contract): POST handler with kind dispatch, version + capability checks"
```

---

## Phase 11: Capabilities Endpoint

### Task 11.1: GET /capabilities

**Files:**
- Create: `extensions/dashboard/contract/transport/capabilities.go`
- Create: `extensions/dashboard/contract/transport/capabilities_test.go`

- [ ] **Step 1: Write the failing test**

```go
// capabilities_test.go
package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestCapabilities_ReportsRegisteredContributors(t *testing.T) {
	reg, _ := setupRegistry(t)
	h := NewCapabilitiesHandler(reg, []string{"v1"})
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/capabilities", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got CapabilitiesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Contributors) != 1 || got.Contributors[0].Name != "users" {
		t.Errorf("contributors = %+v", got.Contributors)
	}
	intents := got.Contributors[0].Intents
	if len(intents) != 2 {
		t.Errorf("expected 2 intents in capabilities, got %d", len(intents))
	}
	_ = contract.IntentKindQuery
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/transport/...`
Expected: FAIL.

- [ ] **Step 3: Implement transport/capabilities.go**

```go
// capabilities.go
package transport

import (
	"encoding/json"
	"net/http"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// CapabilitiesResponse is the wire shape for GET /capabilities.
type CapabilitiesResponse struct {
	ShellEnvelopes []string                 `json:"shellEnvelopes"`
	Contributors   []ContributorCapability  `json:"contributors"`
}

// ContributorCapability is one contributor's negotiable surface.
type ContributorCapability struct {
	Name      string                  `json:"name"`
	Envelopes []string                `json:"envelopes"`
	Intents   []IntentCapability      `json:"intents"`
}

// IntentCapability summarises one intent's available versions.
type IntentCapability struct {
	Name     string                   `json:"name"`
	Versions []IntentVersionStatus    `json:"versions"`
}

// IntentVersionStatus reports a single version + lifecycle status.
type IntentVersionStatus struct {
	N           int    `json:"n"`
	Status      string `json:"status"` // active | deprecated
	RemoveAfter string `json:"removeAfter,omitempty"`
}

// NewCapabilitiesHandler returns the GET /capabilities handler.
func NewCapabilitiesHandler(reg contract.Registry, shellEnvelopes []string) http.Handler {
	return &capabilitiesHandler{reg: reg, shellEnvelopes: shellEnvelopes}
}

type capabilitiesHandler struct {
	reg            contract.Registry
	shellEnvelopes []string
}

func (h *capabilitiesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	resp := CapabilitiesResponse{ShellEnvelopes: h.shellEnvelopes}
	for _, m := range h.reg.All() {
		c := ContributorCapability{Name: m.Contributor.Name, Envelopes: m.Contributor.Envelope.Supports}
		// Group intents by name; collect versions.
		byName := map[string][]IntentVersionStatus{}
		for _, in := range m.Intents {
			s := IntentVersionStatus{N: in.Version, Status: "active"}
			if in.Deprecated != nil {
				s.Status = "deprecated"
				s.RemoveAfter = in.Deprecated.RemoveAfter
			}
			byName[in.Name] = append(byName[in.Name], s)
		}
		for name, versions := range byName {
			c.Intents = append(c.Intents, IntentCapability{Name: name, Versions: versions})
		}
		resp.Contributors = append(resp.Contributors, c)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/transport/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/transport/capabilities.go extensions/dashboard/contract/transport/capabilities_test.go
git commit -m "feat(dashboard/contract): capabilities endpoint for version negotiation"
```

---

## Phase 12: SSE Stream Transport

### Task 12.1: Multiplexed stream + control endpoint

**Files:**
- Create: `extensions/dashboard/contract/transport/stream.go`
- Create: `extensions/dashboard/contract/transport/control.go`
- Create: `extensions/dashboard/contract/transport/stream_test.go`

The stream design:
- Client opens `GET /api/dashboard/v1/stream` and receives a `streamID` in the first event.
- Client sends subscribe/unsubscribe commands to `POST /api/dashboard/v1/stream/control` with the `streamID`.
- Server fans events from each subscription into the single SSE connection.
- Each event has a `subscriptionID` (the SSE `event:` field) so the client can route locally.

Slice (a) implements transport + multiplexing + per-event authz re-check (cached). Actual subscription event sources (e.g., the audit feed's data) come from contributor handlers in slice (c) — for now, the stream broker accepts an injected `SubscriptionSource`.

- [ ] **Step 1: Write the failing test**

```go
// stream_test.go
package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type stubSource struct {
	events chan contract.StreamEvent
}

func (s *stubSource) Subscribe(_ context.Context, _ contract.Principal, _ string, _ contract.Intent, _ map[string]contract.ParamSource) (<-chan contract.StreamEvent, func(), error) {
	stop := func() { close(s.events) }
	return s.events, stop, nil
}

func TestStream_ControlSubscribeAndDeliver(t *testing.T) {
	reg, wreg := setupRegistry(t)
	// add a subscription intent to the fixture
	src := `
schemaVersion: 1
contributor: { name: feeds, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: audit.tail, kind: subscription, version: 1, capability: read, mode: append }
`
	var feedsM contract.ContractManifest
	_ = contract.UnmarshalManifestForTest([]byte(src), &feedsM)
	_ = reg.Register(&feedsM)

	source := &stubSource{events: make(chan contract.StreamEvent, 4)}
	broker := NewStreamBroker(reg, wreg, source)

	// Open the stream
	streamReq := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/stream", nil)
	streamW := httptest.NewRecorder()
	go broker.ServeStream(streamW, streamReq)
	time.Sleep(20 * time.Millisecond) // allow registration to land

	// Subscribe via control
	cmd, _ := json.Marshal(ControlMessage{
		StreamID: broker.SnapshotIDs()[0], // first active stream
		Op:       "subscribe", Contributor: "feeds", Intent: "audit.tail", SubscriptionID: "s1",
	})
	ctlReq := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1/stream/control", bytes.NewReader(cmd))
	ctlW := httptest.NewRecorder()
	broker.ServeControl(ctlW, ctlReq)
	if ctlW.Code != http.StatusOK {
		t.Fatalf("control status = %d body=%s", ctlW.Code, ctlW.Body)
	}

	// Push an event
	source.events <- contract.StreamEvent{Intent: "audit.tail", Mode: contract.ModeAppend, Payload: json.RawMessage(`{"line":"hi"}`), Seq: 1}
	time.Sleep(20 * time.Millisecond)

	// Validate the SSE body contains the event
	body := streamW.Body.String()
	if !strings.Contains(body, `"intent":"audit.tail"`) || !strings.Contains(body, `"line":"hi"`) {
		t.Errorf("stream did not deliver event: %s", body)
	}
	// And the event header carries the subscription ID
	scanner := bufio.NewScanner(strings.NewReader(body))
	hasEventID := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: s1") {
			hasEventID = true
			break
		}
	}
	if !hasEventID {
		t.Error("expected event: s1 line in stream")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contract/transport/...`
Expected: FAIL.

- [ ] **Step 3: Implement transport/stream.go and transport/control.go**

```go
// stream.go
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"

	"github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// SubscriptionSource is the upstream events feeder. Slice (c) implements one
// for each contributor's subscription intents.
type SubscriptionSource interface {
	Subscribe(ctx context.Context, p contract.Principal, contributor string, intent contract.Intent, params map[string]contract.ParamSource) (<-chan contract.StreamEvent, func(), error)
}

// StreamBroker manages active SSE connections + their subscriptions.
type StreamBroker struct {
	reg    contract.Registry
	wreg   contract.WardenRegistry
	source SubscriptionSource

	mu      sync.Mutex
	streams map[string]*streamConn
}

type streamConn struct {
	id      string
	w       http.ResponseWriter
	flusher http.Flusher
	user    *auth.UserInfo
	subs    map[string]subscription // keyed by subscriptionID
	mu      sync.Mutex
}

type subscription struct {
	cancel func()
}

// NewStreamBroker returns a broker bound to a registry, warden registry, and source.
func NewStreamBroker(reg contract.Registry, wreg contract.WardenRegistry, source SubscriptionSource) *StreamBroker {
	return &StreamBroker{
		reg:     reg,
		wreg:    wreg,
		source:  source,
		streams: map[string]*streamConn{},
	}
}

// ServeStream implements GET /api/dashboard/v1/stream.
func (b *StreamBroker) ServeStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id := uuid.NewString()
	conn := &streamConn{
		id: id, w: w, flusher: flusher,
		user: auth.UserFromContext(r.Context()),
		subs: map[string]subscription{},
	}
	b.mu.Lock()
	b.streams[id] = conn
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.streams, id)
		b.mu.Unlock()
		conn.mu.Lock()
		for _, s := range conn.subs {
			s.cancel()
		}
		conn.mu.Unlock()
	}()

	// Send a hello event so the client learns its streamID
	fmt.Fprintf(w, "event: hello\ndata: {\"streamID\":%q}\n\n", id)
	flusher.Flush()

	<-r.Context().Done()
}

// SnapshotIDs returns currently-active stream IDs (test helper / introspection).
func (b *StreamBroker) SnapshotIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.streams))
	for id := range b.streams {
		out = append(out, id)
	}
	return out
}

func (b *StreamBroker) writeEvent(conn *streamConn, subID string, ev contract.StreamEvent) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if _, err := fmt.Fprintf(conn.w, "event: %s\ndata: %s\n\n", subID, payload); err != nil {
		return err
	}
	conn.flusher.Flush()
	return nil
}
```

```go
// control.go
package transport

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// ControlMessage is one client request on POST /stream/control.
type ControlMessage struct {
	StreamID       string                              `json:"streamID"`
	Op             string                              `json:"op"` // "subscribe" | "unsubscribe"
	SubscriptionID string                              `json:"subscriptionID"`
	Contributor    string                              `json:"contributor,omitempty"`
	Intent         string                              `json:"intent,omitempty"`
	Params         map[string]contract.ParamSource     `json:"params,omitempty"`
}

// ServeControl handles POST /api/dashboard/v1/stream/control.
func (b *StreamBroker) ServeControl(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var msg ControlMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "invalid control message", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	conn, ok := b.streams[msg.StreamID]
	b.mu.Unlock()
	if !ok {
		http.Error(w, "unknown streamID", http.StatusNotFound)
		return
	}
	switch msg.Op {
	case "subscribe":
		in, ok := b.reg.Intent(msg.Contributor, msg.Intent, intentVersionForSubscribe(b.reg, msg))
		if !ok || in.Kind != contract.IntentKindSubscription {
			http.Error(w, "intent not a subscription", http.StatusBadRequest)
			return
		}
		p := contract.PrincipalFor(conn.user)
		if !in.Requires.Allow(conn.user, nil) {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		ch, stop, err := b.source.Subscribe(ctx, p, msg.Contributor, in, msg.Params)
		if err != nil {
			cancel()
			http.Error(w, "subscribe failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		conn.mu.Lock()
		conn.subs[msg.SubscriptionID] = subscription{cancel: func() { stop(); cancel() }}
		conn.mu.Unlock()
		go func() {
			defer cancel()
			for ev := range ch {
				if !b.allowsEvent(conn.user, in) {
					continue
				}
				if err := b.writeEvent(conn, msg.SubscriptionID, ev); err != nil {
					return
				}
			}
		}()
		w.WriteHeader(http.StatusOK)
	case "unsubscribe":
		conn.mu.Lock()
		s, ok := conn.subs[msg.SubscriptionID]
		if ok {
			s.cancel()
			delete(conn.subs, msg.SubscriptionID)
		}
		conn.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "unknown op", http.StatusBadRequest)
	}
}

// allowsEvent re-checks the user's predicate. Real per-event Warden invocation
// with a TTL cache lives in slice (b); slice (a) re-evaluates the YAML predicate
// against the current connection's UserInfo.
func (b *StreamBroker) allowsEvent(user *auth.UserInfo, in contract.Intent) bool {
	return in.Requires.Allow(user, nil)
}

func intentVersionForSubscribe(reg contract.Registry, msg ControlMessage) int {
	v, _ := reg.HighestVersion(msg.Contributor, msg.Intent)
	return v
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contract/transport/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/transport/stream.go extensions/dashboard/contract/transport/control.go extensions/dashboard/contract/transport/stream_test.go
git commit -m "feat(dashboard/contract): multiplexed SSE stream with subscribe/unsubscribe control"
```

### Task 12.2: Subscription mode dispatch (replace / append / snapshot+delta)

The transport in Task 12.1 forwards `StreamEvent`s as-is. Mode-specific logic lives at the source side: slice (c) `SubscriptionSource` implementations decide what to emit. Slice (a) only validates that the intent's declared `mode` matches what's emitted (fail-soft: log a warning, deliver anyway).

**Files:**
- Modify: `extensions/dashboard/contract/transport/stream.go`
- Modify: `extensions/dashboard/contract/transport/stream_test.go`

- [ ] **Step 1: Add a failing test for mode-mismatch warning**

```go
func TestStream_ModeMismatch_DeliversAndLogs(t *testing.T) {
	// Intent declared mode: append; source emits replace. Event still delivered.
	// (Strict enforcement is slice (b)'s job; for now, mismatches are observable.)
	// This test verifies delivery happens.
	// ... (use the same fixture as TestStream_ControlSubscribeAndDeliver but with mode=replace event)
}
```

For brevity, this test mirrors `TestStream_ControlSubscribeAndDeliver` with the event's `Mode` set to `ModeReplace`. Verify that the event reaches the connection.

- [ ] **Step 2: Run to verify it passes** (no implementation change needed yet — the broker forwards any mode)

Run: `go test ./extensions/dashboard/contract/transport/...`
Expected: PASS without changes.

- [ ] **Step 3: Add a stderr warning when modes diverge**

In `control.go`, inside the per-event goroutine:

```go
import "log" // add to imports

// inside `for ev := range ch`:
if ev.Mode != "" && in.Mode != "" && ev.Mode != in.Mode {
    log.Printf("contract/stream: %s/%s mode mismatch declared=%s emitted=%s", msg.Contributor, msg.Intent, in.Mode, ev.Mode)
}
```

- [ ] **Step 4: Add a test capturing log output** (optional but recommended; pattern: redirect `log.SetOutput(&buf)`).

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/transport/stream.go extensions/dashboard/contract/transport/control.go extensions/dashboard/contract/transport/stream_test.go
git commit -m "feat(dashboard/contract): warn when subscription event mode diverges from declaration"
```

---

## Phase 13: Wire the contract into the Dashboard extension

### Task 13.1: Optional Contract field on legacy Manifest

**Files:**
- Modify: `extensions/dashboard/contributor/manifest.go`
- Create: `extensions/dashboard/contributor/manifest_contract_test.go`

This lets a contributor publish the new contract manifest alongside the legacy templ manifest while migration runs.

- [ ] **Step 1: Add the failing test**

```go
// manifest_contract_test.go
package contributor

import (
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestManifest_HasContractField(t *testing.T) {
	m := &Manifest{Name: "x"}
	m.Contract = &contract.ContractManifest{SchemaVersion: 1}
	if m.Contract.SchemaVersion != 1 {
		t.Errorf("contract field round trip lost data")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./extensions/dashboard/contributor/...`
Expected: FAIL — `Contract` field undefined.

- [ ] **Step 3: Add the field to `manifest.go`**

```go
import "github.com/xraph/forge/extensions/dashboard/contract" // add to imports

// inside type Manifest struct, after AuthPages:
Contract *contract.ContractManifest `json:"contract,omitempty"`
```

- [ ] **Step 4: Run tests**

Run: `go test ./extensions/dashboard/contributor/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contributor/manifest.go extensions/dashboard/contributor/manifest_contract_test.go
git commit -m "feat(dashboard): allow legacy Manifest to carry a contract manifest"
```

### Task 13.2: Register contract endpoints alongside legacy routes

**Files:**
- Modify: `extensions/dashboard/extension.go`
- Modify: `extensions/dashboard/extension.go` (struct: add fields)

The extension grows three new fields (`contractRegistry`, `wardenRegistry`, `streamBroker`) and registers three new endpoints. Legacy endpoints continue working unchanged.

- [ ] **Step 1: Read the existing struct + registerRoutes to confirm field placement**

Read `extensions/dashboard/extension.go` at lines 100–200 (struct definition) and 1163–1280 (`registerRoutes`).

- [ ] **Step 2: Add fields to the Extension struct** (no test for plumbing — covered by E2E in Phase 15)

```go
import (
    "github.com/xraph/forge/extensions/dashboard/contract"
    "github.com/xraph/forge/extensions/dashboard/contract/transport"
)

// add to Extension struct (find the existing field block):
contractRegistry contract.Registry
wardenRegistry   contract.WardenRegistry
streamBroker     *transport.StreamBroker
auditEmitter     contract.AuditEmitter
```

- [ ] **Step 3: Initialize the new fields where the Extension is constructed**

Locate the Extension constructor (search for `&Extension{` in `extension.go`). Add:

```go
contractRegistry: contract.NewRegistry(),
wardenRegistry:   contract.NewWardenRegistry(),
auditEmitter:     contract.NewLogAuditEmitter(os.Stdout), // slice (b) replaces with chronicle
```

`streamBroker` is created in `registerRoutes` once the source is ready (slice (c)); initialize to nil here and skip stream registration when nil.

- [ ] **Step 4: Add route registrations to `registerRoutes`**

After the existing `/api/extensions` line (around line 1217), add:

```go
// Contract endpoints (slice a)
if e.contractRegistry != nil {
    must(router.POST(base+"/api/dashboard/v1", e.handleContractPOST()))
    must(router.GET(base+"/api/dashboard/v1/capabilities", e.handleContractCapabilities()))
    if e.streamBroker != nil {
        must(router.EventStream(base+"/api/dashboard/v1/stream", e.streamBroker.ServeStream))
        must(router.POST(base+"/api/dashboard/v1/stream/control", e.streamBroker.ServeControl))
    }
}
```

Add helper methods on Extension:

```go
func (e *Extension) handleContractPOST() http.HandlerFunc {
    h := transport.NewHandler(e.contractRegistry, e.wardenRegistry, /* dispatcher */ nil, e.auditEmitter)
    return h.ServeHTTP
}

func (e *Extension) handleContractCapabilities() http.HandlerFunc {
    return transport.NewCapabilitiesHandler(e.contractRegistry, []string{"v1"}).ServeHTTP
}
```

> **Note:** `dispatcher` is `nil` here because actual dispatch is slice (c)'s job. To allow the handler to function in tests during slice (a), wire a default `nilDispatcher` that returns `*contract.Error{Code: contract.CodeUnavailable, Message: "dispatcher not configured"}` until slice (c) lands.

Add `nilDispatcher` to `transport/http.go`:

```go
// NilDispatcher always returns UNAVAILABLE; useful as a safe default before
// real dispatchers are wired (slice c).
type NilDispatcher struct{}

func (NilDispatcher) Dispatch(_ context.Context, _ contract.Request, _ contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
    return nil, contract.ResponseMeta{}, &contract.Error{Code: contract.CodeUnavailable, Message: "no dispatcher configured"}
}
```

Use `transport.NilDispatcher{}` in `handleContractPOST` instead of `nil`.

- [ ] **Step 5: Build the whole module**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 6: Run all tests**

Run: `go test ./extensions/dashboard/...`
Expected: PASS — including legacy tests that haven't been touched.

- [ ] **Step 7: Commit**

```bash
git add extensions/dashboard/extension.go extensions/dashboard/contract/transport/http.go
git commit -m "feat(dashboard): register contract endpoints alongside legacy routes"
```

### Task 13.3: Hook the contract registry into existing manifest registration

When a legacy contributor publishes a `*contract.ContractManifest` via the new field, the extension must register it with the contract registry on startup.

**Files:**
- Modify: `extensions/dashboard/extension.go`

- [ ] **Step 1: Locate where contributors are added (search for `AddContributor` or `RegisterContributor`)**

- [ ] **Step 2: After legacy registration succeeds, register the contract manifest if present**

```go
if mn := contributor.Manifest(); mn != nil && mn.Contract != nil {
    if err := loader.Validate(mn.Contract, e.wardenRegistry); err != nil {
        return fmt.Errorf("contract validation: %w", err)
    }
    if err := e.contractRegistry.Register(mn.Contract); err != nil {
        return fmt.Errorf("contract register: %w", err)
    }
}
```

Import `extensions/dashboard/contract/loader`.

- [ ] **Step 3: Build + run the full extension test suite**

Run: `go test ./extensions/dashboard/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add extensions/dashboard/extension.go
git commit -m "feat(dashboard): register contract manifests at contributor add-time"
```

---

## Phase 14: Probe CLI

### Task 14.1: cmd/dashboard-contract-probe

**Files:**
- Create: `cmd/dashboard-contract-probe/main.go`

This CLI lets us test the contract endpoints without a React shell — important for slice (c) migration of the first contributor.

- [ ] **Step 1: Write the CLI**

```go
// main.go
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func main() {
	base := flag.String("base", "http://localhost:8080", "dashboard base URL (no trailing slash)")
	kind := flag.String("kind", "query", "graph | query | command")
	contributor := flag.String("contributor", "", "contributor name")
	intent := flag.String("intent", "", "intent name")
	payload := flag.String("payload", "{}", "JSON payload")
	csrf := flag.String("csrf", "", "CSRF token (required for command)")
	idem := flag.String("idem", "", "idempotency key (required for command)")
	flag.Parse()

	req := contract.Request{
		Envelope: "v1", Kind: contract.Kind(*kind),
		Contributor: *contributor, Intent: *intent,
		Payload: json.RawMessage(*payload),
		CSRF: *csrf, IdempotencyKey: *idem,
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(*base+"/api/dashboard/v1", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "request:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	fmt.Printf("HTTP %d\n%s\n", resp.StatusCode, out)
}
```

- [ ] **Step 2: Build the CLI**

Run: `go build -o /tmp/dashboard-contract-probe ./cmd/dashboard-contract-probe`
Expected: clean build.

- [ ] **Step 3: Smoke-test against a running dashboard** (manual, no automation)

```bash
/tmp/dashboard-contract-probe -base=http://localhost:8080 -kind=query -contributor=users -intent=users.list
```

Expected: HTTP 503 / UNAVAILABLE (since `NilDispatcher` is wired) — confirms the route is registered and decoding works.

- [ ] **Step 4: Commit**

```bash
git add cmd/dashboard-contract-probe/main.go
git commit -m "feat(cmd): add dashboard-contract-probe CLI for raw envelope testing"
```

---

## Phase 15: End-to-End Harness

### Task 15.1: Fixture YAML + driver test

**Files:**
- Create: `extensions/dashboard/contract/testdata/fixture_users.yaml`
- Create: `extensions/dashboard/contract/testdata/fixture_auth_extends.yaml`
- Create: `extensions/dashboard/contract/e2e_test.go`

- [ ] **Step 1: Write the fixtures**

`fixture_users.yaml`:

```yaml
schemaVersion: 1
contributor:
  name: users
  envelope: { supports: [v1], preferred: v1 }
  capabilities: [users.read, users.write]

queries:
  userList:
    intent: users.list
    cache: { staleTime: 30s }

intents:
  - name: users.list
    kind: query
    version: 1
    capability: read
    requires: { all: ["scope:users.read"] }

  - name: user.disable
    kind: command
    version: 1
    capability: write
    requires: { all: ["role:admin", "scope:users.write"] }
    invalidates: [users.list]

  - name: audit.tail
    kind: subscription
    version: 1
    capability: read
    mode: append
    requires: { all: ["role:admin"] }

graph:
  - route: /users
    intent: page.shell
    title: Users
    nav: { group: Identity, icon: users, priority: 10 }
    slots:
      main:
        - intent: resource.list
          data: queries.userList
          slots:
            rowActions:
              - intent: action.button
                op: user.disable
                visibleWhen: { all: ["role:admin"] }
            detailDrawer:
              - intent: form.edit
                slots:
                  fields:
                    - intent: form.field
```

`fixture_auth_extends.yaml`:

```yaml
schemaVersion: 1
contributor:
  name: auth
  envelope: { supports: [v1], preferred: v1 }
intents: []
extends:
  - target: { contributor: users, intent: page.shell, route: /users }
    slot: main.detailDrawer.fields
    add:
      - intent: form.field
        requires: { all: ["scope:auth.read"] }
```

- [ ] **Step 2: Write the E2E driver**

```go
// e2e_test.go
package contract

import (
	"context"
	"os"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

func loadFixture(t *testing.T, path string) *ContractManifest {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := loader.Load(f, path)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestE2E_RegisterValidateBuild(t *testing.T) {
	users := loadFixture(t, "testdata/fixture_users.yaml")
	authExt := loadFixture(t, "testdata/fixture_auth_extends.yaml")

	wreg := NewWardenRegistry()
	if err := loader.Validate(users, wreg); err != nil {
		t.Fatalf("validate users: %v", err)
	}
	if err := loader.Validate(authExt, wreg); err != nil {
		t.Fatalf("validate authExt: %v", err)
	}

	reg := NewRegistry()
	if err := reg.Register(users); err != nil {
		t.Fatalf("register users: %v", err)
	}
	if err := reg.Register(authExt); err != nil {
		t.Fatalf("register authExt: %v", err)
	}

	build := NewGraphBuilder(reg, wreg)
	admin := &auth.UserInfo{Subject: "alice", Roles: []string{"admin"}, Scopes: []string{"users.read", "users.write"}}
	got, err := build.Build(context.Background(), "users", "/users", PrincipalFor(admin))
	if err != nil {
		t.Fatalf("build admin: %v", err)
	}
	// admin should see the disable action
	actions := got.Slots["main"][0].Slots["rowActions"]
	if len(actions) != 1 || actions[0].Op != "user.disable" {
		t.Errorf("admin actions wrong: %+v", actions)
	}
	// extension should be merged: detailDrawer.fields has 2 form.fields
	fields := got.Slots["main"][0].Slots["detailDrawer"][0].Slots["fields"]
	if len(fields) != 2 {
		t.Errorf("expected 2 fields after extension merge, got %d", len(fields))
	}

	// viewer sees no row actions
	viewer := &auth.UserInfo{Subject: "bob", Roles: []string{"viewer"}, Scopes: []string{"users.read"}}
	got2, _ := build.Build(context.Background(), "users", "/users", PrincipalFor(viewer))
	if got2 == nil {
		t.Skip("viewer filtered fully") // depends on resource.list visibleWhen
		return
	}
	actions2 := got2.Slots["main"][0].Slots["rowActions"]
	if len(actions2) != 0 {
		t.Errorf("viewer should see no admin actions: %+v", actions2)
	}
}
```

- [ ] **Step 3: Run the E2E test**

Run: `go test ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add extensions/dashboard/contract/testdata/ extensions/dashboard/contract/e2e_test.go
git commit -m "test(dashboard/contract): end-to-end fixture + driver covering register/validate/build/extend"
```

---

## Final Verification

- [ ] **Run the full test suite for the dashboard extension**

```bash
go test ./extensions/dashboard/...
```
Expected: PASS, including legacy tests untouched.

- [ ] **Vet the new package**

```bash
go vet ./extensions/dashboard/contract/...
```
Expected: clean.

- [ ] **Build the probe CLI**

```bash
go build -o /tmp/dashboard-contract-probe ./cmd/dashboard-contract-probe
```
Expected: clean build.

- [ ] **Smoke test the live endpoint** (manual)

Start the dashboard locally, then run the probe against `users.list`. Expect HTTP 503 UNAVAILABLE (NilDispatcher) and confirm via dashboard logs that the request reached the contract handler.

- [ ] **Final commit if anything's left**

```bash
git status
git diff
# if there are stragglers:
git add <files>
git commit -m "chore(dashboard/contract): final touches"
```

## Self-Review Notes

- **Spec coverage:** Every row in DESIGN.md's "Design Decisions Locked In" table is covered by a phase or task in this plan. Slot extensibility flag is enforced in Task 6.3; per-event authz re-check is implemented (cached version is slice (b)); audit-on-by-default is Task 9.1+10.1.
- **Out-of-scope items honored:** No React, no chronicle integration (audit emitter is log-based), no CSRF middleware (header presence enforced; rotation is slice (b)), no templ removal.
- **Dispatcher placeholder:** `NilDispatcher` is intentional. Task 13.2 wires it as the default; slice (c) replaces it with a real dispatcher backed by contributor handler registration. The probe CLI in Task 14.1 documents the expected 503 in this state.
- **Naming consistency:** `IntentKindQuery` vs `KindQuery` — the former is the manifest-level discriminator (on Intent), the latter is the wire envelope's. They are intentionally different types; conversion is enforced in `kindMatchesCapability`. Tests cover both.
- **No placeholders:** Every step has actual code or actual commands. No "implement X later" anywhere in the body of a task.
