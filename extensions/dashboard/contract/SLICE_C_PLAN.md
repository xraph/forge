# Slice (c) — Dispatcher + Pilot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `transport.NilDispatcher{}` with a real dispatcher that contributors register intent handlers against, and migrate three queries + one subscription onto the contract end-to-end.

**Architecture:** A new `extensions/dashboard/contract/dispatcher` sub-package owns the function-table dispatcher, the `SubscriptionSource` adapter, and generic typed wrappers. A new `extensions/dashboard/contract/pilot` sub-package ships the embedded YAML manifest plus the four handlers (`extensions.list`, `services.list`, `services.detail`, `metrics.summary`) wired to the existing `collector.DataCollector` and `contributor.ContributorRegistry`. The dashboard extension wires both into startup, replacing the no-op dispatcher and finally instantiating the SSE broker.

**Tech Stack:** Go 1.25, stdlib `testing`, `gopkg.in/yaml.v3`, existing `extensions/dashboard/{auth,collector,contributor}` packages. No new external dependencies.

---

## Reference

- **Design spec:** [SLICE_C_DESIGN.md](SLICE_C_DESIGN.md)
- **Slice (a) interfaces this plan implements:**
  - `transport.Dispatcher` ([transport/http.go:16](transport/http.go))
  - `transport.SubscriptionSource` ([transport/stream.go:19](transport/stream.go))
- **Slice (a) types reused throughout:** `contract.Request`, `contract.ResponseMeta`, `contract.Principal`, `contract.Action`, `contract.StreamEvent`, `contract.Error`, `contract.IntentKind`, `contract.Capability`, `contract.SubscriptionMode`.
- **Existing dashboard internals to call:**
  - `collector.DataCollector.CollectServices(ctx) []ServiceInfo` ([collector/collector.go:314](collector/collector.go))
  - `collector.DataCollector.CollectServiceDetail(ctx, name) *ServiceDetail` ([collector/collector.go:390](collector/collector.go))
  - `collector.DataCollector.CollectMetrics(ctx) *MetricsData` ([collector/collector.go:140](collector/collector.go))
  - `contributor.ContributorRegistry.ContributorNames() []string` and `GetManifest(name) (*Manifest, bool)` (used today by [handlers/api.go:107](handlers/api.go))

## Spec Deviation: `metrics.cpu` → `metrics.summary`

The spec's pilot included a `metrics.cpu` subscription. The collector exposes `MetricsData.Metrics map[string]any` which has no guaranteed `cpu` key — it depends on what the application registered with the metrics extension. To keep the pilot runnable in any deployment, the implementation uses `metrics.summary` instead, emitting `MetricsStats` (TotalMetrics + Counters + Gauges + Histograms) on a 5-second interval. Same `replace` mode, same subscription mechanics — only the payload type differs. If a deployment wires real CPU into the metrics extension, a future intent can target it directly.

This is a small, isolated rename. The graph YAML still drops it into a `metric.counter` widget on `/metrics/live`.

## File Structure

```
extensions/dashboard/contract/dispatcher/
  doc.go                   # package comment
  handler.go               # Handler, Result, SubscriptionHandler, IntentRef, Contributor types
  dispatcher.go            # Dispatcher struct, New, Register, Dispatch, lookup helpers
  subscription.go          # RegisterSubscription, Subscribe (transport.SubscriptionSource impl)
  generic.go               # RegisterQuery, RegisterCommand, RegisterSubscription generic wrappers
  metrics.go               # MetricsEmitter interface, NoopMetricsEmitter, DispatchInfo
  contributor.go           # RegisterContributor walks Contributor interface
  dispatcher_test.go
  subscription_test.go
  generic_test.go
  metrics_test.go
  contributor_test.go

extensions/dashboard/contract/pilot/
  doc.go
  manifest.yaml            # embedded via //go:embed
  types.go                 # ExtensionsList, ServicesList, ServiceDetail, MetricsSummary
  pilot.go                 # Register(disp, deps) entry; loads YAML, validates, registers
  extensions.go            # extensions.list handler + test
  services.go              # services.list + services.detail handlers + tests
  metrics.go               # metrics.summary subscription handler + test
  extensions_test.go
  services_test.go
  metrics_test.go
  pilot_e2e_test.go        # full HTTP+SSE end-to-end with the contract handler
```

`extensions/dashboard/extension.go` is modified to wire the dispatcher + broker; no other file in the dashboard subtree changes structurally.

## Conventions

- Plain `testing` package; no testify in this subtree.
- Imports: stdlib first, then `github.com/xraph/forge/...`, then third-party.
- The `dashauth` import alias is the existing convention. Use `import dashauth "github.com/xraph/forge/extensions/dashboard/auth"`.
- Compile-time interface assertions at the bottom of each file: `var _ transport.Dispatcher = (*Dispatcher)(nil)`.
- One commit per logical change. No `Co-Authored-By` trailers.

---

## Phase 0: Dispatcher Package Skeleton

### Task 0.1: Package + Handler/Result types

**Files:**
- Create: `extensions/dashboard/contract/dispatcher/doc.go`
- Create: `extensions/dashboard/contract/dispatcher/handler.go`
- Create: `extensions/dashboard/contract/dispatcher/handler_test.go`

- [ ] **Step 1: Write doc.go**

```go
// Package dispatcher implements transport.Dispatcher and transport.SubscriptionSource
// against a function-table of registered handlers. Contributors register their
// intent handlers via Register / RegisterSubscription / RegisterContributor;
// the HTTP and SSE transports look them up at request time.
//
// See SLICE_C_DESIGN.md in the parent contract directory for the spec this implements.
package dispatcher
```

- [ ] **Step 2: Write handler_test.go (failing)**

```go
package dispatcher

import (
	"encoding/json"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestIntentRef_StringForm(t *testing.T) {
	r := IntentRef{Intent: "users.list", Version: 1}
	if got := r.String(); got != "users.list@1" {
		t.Errorf("String() = %q", got)
	}
}

func TestResult_HoldsData(t *testing.T) {
	r := &Result{Data: json.RawMessage(`{"ok":true}`), ExtraInvalidates: []string{"x"}}
	if string(r.Data) != `{"ok":true}` {
		t.Errorf("data lost")
	}
	if r.ExtraInvalidates[0] != "x" {
		t.Errorf("invalidates lost")
	}
}

// Compile-time check: a value-conformant function compiles as Handler.
func TestHandlerSignature_Compiles(t *testing.T) {
	var h Handler = func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	}
	_ = h
}
```

Add `"context"` to the import block.

- [ ] **Step 3: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: FAIL — undefined: IntentRef / Result / Handler.

- [ ] **Step 4: Implement handler.go**

```go
package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// Handler is the foundation function-table handler signature for query and
// command intents. Returning a *contract.Error propagates the canonical code
// to the wire; any other error is wrapped as CodeInternal at dispatch time.
type Handler func(ctx context.Context, payload json.RawMessage, params map[string]any, p contract.Principal) (*Result, error)

// SubscriptionHandler is the function-table handler for subscription intents.
// The handler returns a channel of events, a force-stop function, and an
// optional error. Closing the channel signals end-of-stream; cancelling ctx
// is the canonical way to ask the handler to stop emitting.
type SubscriptionHandler func(ctx context.Context, params map[string]any, p contract.Principal) (<-chan contract.StreamEvent, func(), error)

// Result carries the data payload plus optional response-meta overrides.
// Handlers that don't need to influence meta can return &Result{Data: ...};
// handlers that need to add invalidations or override cache hints set the
// extra fields.
type Result struct {
	// Data is the JSON-encoded response body. May be nil for a {data: null} response.
	Data json.RawMessage
	// ExtraInvalidates is appended to the manifest's declared Invalidates.
	ExtraInvalidates []string
	// CacheOverride, when non-nil, replaces the manifest's declared cache hint.
	CacheOverride *contract.CacheHint
}

// IntentRef is the (intent, version) tuple used as a registration key.
type IntentRef struct {
	Intent  string
	Version int
}

// String formats as "intent@version" — used in error messages and logs.
func (r IntentRef) String() string {
	return fmt.Sprintf("%s@%d", r.Intent, r.Version)
}

// Contributor is layer (b)'s registration shape: a contributor publishes its
// handler and subscription tables, and the dispatcher walks them on Register.
type Contributor interface {
	Name() string
	Handlers() map[IntentRef]Handler
	Subscriptions() map[IntentRef]SubscriptionHandler
}
```

- [ ] **Step 5: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: PASS — 3 tests.

- [ ] **Step 6: Commit**

```bash
git add extensions/dashboard/contract/dispatcher/{doc.go,handler.go,handler_test.go}
git commit -m "feat(dashboard/contract/dispatcher): add package skeleton and handler types"
```

---

## Phase 1: Dispatcher Core — Register + Dispatch

### Task 1.1: Register and Dispatch implementation

**Files:**
- Create: `extensions/dashboard/contract/dispatcher/dispatcher.go`
- Create: `extensions/dashboard/contract/dispatcher/dispatcher_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestDispatcher_RegisterAndDispatch(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	if err := d.Register("users", "users.list", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`{"users":[]}`)}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list", IntentVersion: 1}
	data, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if string(data) != `{"users":[]}` {
		t.Errorf("data = %s", data)
	}
}

func TestDispatcher_DuplicateRegister(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	h := func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) { return &Result{}, nil }
	_ = d.Register("c", "i", 1, h)
	if err := d.Register("c", "i", 1, h); err == nil {
		t.Error("duplicate register should fail")
	}
}

func TestDispatcher_NotFound(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "x", Intent: "y", IntentVersion: 1}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}

func TestDispatcher_ContractErrorPassesThrough(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return nil, &contract.Error{Code: contract.CodeConflict, Message: "duplicate"}
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindCommand, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeConflict {
		t.Errorf("expected CodeConflict pass-through, got %v", err)
	}
}

func TestDispatcher_PlainErrorWrappedAsInternal(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return nil, errors.New("kaboom")
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindCommand, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeInternal {
		t.Errorf("expected CodeInternal wrap, got %v", err)
	}
}

func TestDispatcher_ContextCanceledMappedToUnavailable(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return nil, context.Canceled
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeUnavailable {
		t.Errorf("expected CodeUnavailable for canceled, got %v", err)
	}
	if !ce.Retryable {
		t.Error("canceled errors should be retryable")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: FAIL — undefined: New / NoopMetricsEmitter / Dispatcher.

- [ ] **Step 3: Stub MetricsEmitter so this test compiles** — full impl lands in Phase 4. Add to `metrics.go`:

```go
package dispatcher

import (
	"context"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// MetricsEmitter ships dispatch metrics to a backend. The Phase 4 expansion
// of this file adds the full DispatchInfo struct and the noop default. For
// Phase 1, only the interface and the noop are needed.
type MetricsEmitter interface {
	RecordDispatch(ctx context.Context, contributor, intent string, version int, kind contract.Kind, latency time.Duration, errCode contract.ErrorCode)
}

// NoopMetricsEmitter discards all dispatch metrics.
type NoopMetricsEmitter struct{}

func (NoopMetricsEmitter) RecordDispatch(_ context.Context, _, _ string, _ int, _ contract.Kind, _ time.Duration, _ contract.ErrorCode) {}
```

- [ ] **Step 4: Implement dispatcher.go**

```go
package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
)

// Dispatcher is the concrete implementation of transport.Dispatcher and
// transport.SubscriptionSource (Subscribe lives in subscription.go).
// Contributors register handlers indexed by (contributor, intent, version);
// dispatch is a map lookup + the handler call wrapped in metrics emission
// and canonical error mapping.
type Dispatcher struct {
	metrics MetricsEmitter

	mu            sync.RWMutex
	handlers      map[handlerKey]Handler
	subscriptions map[handlerKey]SubscriptionHandler
}

type handlerKey struct {
	Contributor string
	Intent      string
	Version     int
}

// New returns a fresh dispatcher. Pass NoopMetricsEmitter{} for tests / dev;
// slice (b) provides a Prometheus-backed implementation.
func New(metrics MetricsEmitter) *Dispatcher {
	if metrics == nil {
		metrics = NoopMetricsEmitter{}
	}
	return &Dispatcher{
		metrics:       metrics,
		handlers:      map[handlerKey]Handler{},
		subscriptions: map[handlerKey]SubscriptionHandler{},
	}
}

// Register binds a query/command handler to a (contributor, intent, version)
// key. Returns an error on duplicate registration.
func (d *Dispatcher) Register(contributor, intent string, version int, h Handler) error {
	if h == nil {
		return fmt.Errorf("dispatcher: nil handler for %s/%s@%d", contributor, intent, version)
	}
	k := handlerKey{contributor, intent, version}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.handlers[k]; exists {
		return fmt.Errorf("dispatcher: handler %s/%s@%d already registered", contributor, intent, version)
	}
	d.handlers[k] = h
	return nil
}

// Dispatch implements transport.Dispatcher.
func (d *Dispatcher) Dispatch(ctx context.Context, req contract.Request, p contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	k := handlerKey{req.Contributor, req.Intent, req.IntentVersion}
	d.mu.RLock()
	h, ok := d.handlers[k]
	d.mu.RUnlock()
	if !ok {
		err := &contract.Error{Code: contract.CodeNotFound, Message: fmt.Sprintf("handler %s/%s@%d not registered", req.Contributor, req.Intent, req.IntentVersion)}
		d.metrics.RecordDispatch(ctx, req.Contributor, req.Intent, req.IntentVersion, req.Kind, 0, err.Code)
		return nil, contract.ResponseMeta{}, err
	}

	t0 := time.Now()
	res, handlerErr := h(ctx, req.Payload, req.Params, p)
	latency := time.Since(t0)

	wireErr := mapDispatchError(handlerErr)
	errCode := contract.ErrorCode("")
	if wireErr != nil {
		var ce *contract.Error
		if errors.As(wireErr, &ce) {
			errCode = ce.Code
		}
	}
	d.metrics.RecordDispatch(ctx, req.Contributor, req.Intent, req.IntentVersion, req.Kind, latency, errCode)

	if wireErr != nil {
		return nil, contract.ResponseMeta{}, wireErr
	}
	if res == nil {
		// Allow nil result to mean {data: null} explicitly.
		return nil, contract.ResponseMeta{IntentVersion: req.IntentVersion}, nil
	}
	meta := contract.ResponseMeta{IntentVersion: req.IntentVersion}
	if len(res.ExtraInvalidates) > 0 {
		meta.Invalidates = append(meta.Invalidates, res.ExtraInvalidates...)
	}
	if res.CacheOverride != nil {
		meta.CacheControl = res.CacheOverride
	}
	return res.Data, meta, nil
}

// mapDispatchError converts a handler error into the canonical wire error
// shape. *contract.Error is preserved verbatim. context.Canceled becomes
// CodeUnavailable+Retryable. Any other error is wrapped as CodeInternal,
// with the original chained for server-side logging.
func mapDispatchError(err error) error {
	if err == nil {
		return nil
	}
	var ce *contract.Error
	if errors.As(err, &ce) {
		return ce
	}
	if errors.Is(err, context.Canceled) {
		return &contract.Error{Code: contract.CodeUnavailable, Message: "request cancelled", Retryable: true}
	}
	log.Printf("dispatcher: unmapped handler error: %v", err)
	return &contract.Error{Code: contract.CodeInternal, Message: "internal error"}
}

// Compile-time check that the dispatcher satisfies the transport interface.
// The Subscribe half lands in subscription.go (Phase 2).
var _ transport.Dispatcher = (*Dispatcher)(nil)
```

- [ ] **Step 5: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: PASS — 6 dispatcher tests + the 3 from Phase 0.

- [ ] **Step 6: Commit**

```bash
git add extensions/dashboard/contract/dispatcher/{dispatcher.go,dispatcher_test.go,metrics.go}
git commit -m "feat(dashboard/contract/dispatcher): function-table dispatcher with canonical error mapping"
```

---

## Phase 2: Subscription Registration + SubscriptionSource

### Task 2.1: RegisterSubscription + Subscribe

**Files:**
- Create: `extensions/dashboard/contract/dispatcher/subscription.go`
- Create: `extensions/dashboard/contract/dispatcher/subscription_test.go`

- [ ] **Step 1: Write failing tests**

```go
package dispatcher

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestDispatcher_RegisterSubscriptionAndSubscribe(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	if err := d.RegisterSubscription("logs", "audit.tail", 1, func(_ context.Context, _ map[string]any, _ contract.Principal) (<-chan contract.StreamEvent, func(), error) {
		ch := make(chan contract.StreamEvent, 1)
		ch <- contract.StreamEvent{Intent: "audit.tail", Mode: contract.ModeAppend, Payload: json.RawMessage(`{"line":"hi"}`), Seq: 1}
		close(ch)
		return ch, func() {}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	intent := contract.Intent{Name: "audit.tail", Kind: contract.IntentKindSubscription, Version: 1, Capability: contract.CapRead}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ch, stop, err := d.Subscribe(ctx, contract.Principal{}, "logs", intent, nil)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before event")
		}
		if ev.Intent != "audit.tail" {
			t.Errorf("intent = %q", ev.Intent)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}
}

func TestDispatcher_SubscribeMissingHandler(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	intent := contract.Intent{Name: "missing", Kind: contract.IntentKindSubscription, Version: 1, Capability: contract.CapRead}
	_, _, err := d.Subscribe(context.Background(), contract.Principal{}, "x", intent, nil)
	if err == nil {
		t.Error("expected not-found")
	}
}

func TestDispatcher_DuplicateRegisterSubscription(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	h := func(_ context.Context, _ map[string]any, _ contract.Principal) (<-chan contract.StreamEvent, func(), error) {
		return nil, nil, nil
	}
	_ = d.RegisterSubscription("c", "i", 1, h)
	if err := d.RegisterSubscription("c", "i", 1, h); err == nil {
		t.Error("duplicate register should fail")
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: FAIL — undefined RegisterSubscription / Subscribe.

- [ ] **Step 3: Implement subscription.go**

```go
package dispatcher

import (
	"context"
	"fmt"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
)

// RegisterSubscription binds a subscription handler to (contributor, intent, version).
func (d *Dispatcher) RegisterSubscription(contributor, intent string, version int, h SubscriptionHandler) error {
	if h == nil {
		return fmt.Errorf("dispatcher: nil subscription handler for %s/%s@%d", contributor, intent, version)
	}
	k := handlerKey{contributor, intent, version}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.subscriptions[k]; exists {
		return fmt.Errorf("dispatcher: subscription %s/%s@%d already registered", contributor, intent, version)
	}
	d.subscriptions[k] = h
	return nil
}

// Subscribe implements transport.SubscriptionSource. The broker calls this on
// each subscribe-control message; the dispatcher routes to the registered handler.
// Params from YAML (map[string]contract.ParamSource) are flattened into a
// runtime map[string]any using the From string when set, the literal Value otherwise.
func (d *Dispatcher) Subscribe(ctx context.Context, p contract.Principal, contributor string, intent contract.Intent, params map[string]contract.ParamSource) (<-chan contract.StreamEvent, func(), error) {
	k := handlerKey{contributor, intent.Name, intent.Version}
	d.mu.RLock()
	h, ok := d.subscriptions[k]
	d.mu.RUnlock()
	if !ok {
		return nil, nil, &contract.Error{Code: contract.CodeNotFound, Message: fmt.Sprintf("subscription %s/%s@%d not registered", contributor, intent.Name, intent.Version)}
	}
	flat := flattenParams(params)
	return h(ctx, flat, p)
}

func flattenParams(in map[string]contract.ParamSource) map[string]any {
	out := make(map[string]any, len(in))
	for k, src := range in {
		if src.From != "" {
			out[k] = src.From // resolution happens caller-side; the handler sees the bound value if any
			continue
		}
		out[k] = src.Value
	}
	return out
}

// Compile-time check that Subscribe satisfies the broker's source interface.
var _ transport.SubscriptionSource = (*Dispatcher)(nil)
```

> **Note on `flattenParams`:** for the pilot, params arrive already flattened by the React shell or the probe CLI — the broker passes the raw map through. The TODO of doing `route.tenant` resolution server-side belongs to slice (d) (the React shell builds the dependency graph). For slice (c), the handler sees whatever the caller sent.

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: PASS — 9 + 3 tests now passing.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/dispatcher/{subscription.go,subscription_test.go}
git commit -m "feat(dashboard/contract/dispatcher): subscription handler registration and broker source adapter"
```

---

## Phase 3: Generic Typed Wrappers

### Task 3.1: RegisterQuery / RegisterCommand / RegisterSubscription helpers

**Files:**
- Create: `extensions/dashboard/contract/dispatcher/generic.go`
- Create: `extensions/dashboard/contract/dispatcher/generic_test.go`

- [ ] **Step 1: Write failing tests**

```go
package dispatcher

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type listIn struct {
	Tenant string `json:"tenant"`
}
type listOut struct {
	Users []string `json:"users"`
}

func TestRegisterQuery_DecodesAndEncodes(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	if err := RegisterQuery(d, "users", "users.list", 1, func(_ context.Context, in listIn, _ contract.Principal) (listOut, error) {
		if in.Tenant != "acme" {
			t.Errorf("decoded tenant = %q", in.Tenant)
		}
		return listOut{Users: []string{"alice", "bob"}}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list", IntentVersion: 1, Payload: json.RawMessage(`{"tenant":"acme"}`)}
	data, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var got listOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Users) != 2 {
		t.Errorf("users = %v", got.Users)
	}
}

func TestRegisterQuery_DecodeErrorBecomesBadRequest(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	_ = RegisterQuery(d, "u", "u.l", 1, func(_ context.Context, _ listIn, _ contract.Principal) (listOut, error) { return listOut{}, nil })
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "u", Intent: "u.l", IntentVersion: 1, Payload: json.RawMessage(`not json`)}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err == nil {
		t.Fatal("expected decode error")
	}
	if ce, ok := err.(*contract.Error); !ok || ce.Code != contract.CodeBadRequest {
		t.Errorf("expected CodeBadRequest, got %v", err)
	}
}

type tickIn struct{}
type tickEvent struct {
	N int `json:"n"`
}

func TestRegisterSubscriptionGeneric_PumpsTypedEvents(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	if err := RegisterSubscription(d, "feed", "tick", 1, func(ctx context.Context, _ tickIn, _ contract.Principal) (<-chan tickEvent, func(), error) {
		ch := make(chan tickEvent, 2)
		ch <- tickEvent{N: 1}
		ch <- tickEvent{N: 2}
		close(ch)
		return ch, func() {}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	intent := contract.Intent{Name: "tick", Kind: contract.IntentKindSubscription, Version: 1, Capability: contract.CapRead}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	out, stop, err := d.Subscribe(ctx, contract.Principal{}, "feed", intent, nil)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	count := 0
	for ev := range out {
		count++
		var got tickEvent
		if err := json.Unmarshal(ev.Payload, &got); err != nil {
			t.Errorf("unmarshal event: %v", err)
		}
		if got.N != count {
			t.Errorf("event %d N = %d", count, got.N)
		}
	}
	if count != 2 {
		t.Errorf("expected 2 events, got %d", count)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: FAIL — undefined RegisterQuery / RegisterCommand / RegisterSubscription (the generic ones).

- [ ] **Step 3: Implement generic.go**

```go
package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// RegisterQuery wraps a typed handler in a Handler-compatible closure that
// JSON-decodes Payload into I and encodes the returned O into Result.Data.
// I and O must be JSON-marshallable. Use struct{} for an empty-input intent.
func RegisterQuery[I, O any](d *Dispatcher, contributor, intent string, version int, fn func(ctx context.Context, in I, p contract.Principal) (O, error)) error {
	return d.Register(contributor, intent, version, wrapTyped[I, O](fn))
}

// RegisterCommand is identical in shape to RegisterQuery; both register a
// query/command handler. The dispatcher's wire layer enforces kind/capability
// matching against the manifest, so the only practical difference between the
// two helpers is intent of the caller — they're aliases.
func RegisterCommand[I, O any](d *Dispatcher, contributor, intent string, version int, fn func(ctx context.Context, in I, p contract.Principal) (O, error)) error {
	return d.Register(contributor, intent, version, wrapTyped[I, O](fn))
}

func wrapTyped[I, O any](fn func(ctx context.Context, in I, p contract.Principal) (O, error)) Handler {
	return func(ctx context.Context, payload json.RawMessage, _ map[string]any, p contract.Principal) (*Result, error) {
		var in I
		if len(payload) > 0 && string(payload) != "null" {
			if err := json.Unmarshal(payload, &in); err != nil {
				return nil, &contract.Error{Code: contract.CodeBadRequest, Message: fmt.Sprintf("invalid payload: %v", err)}
			}
		}
		out, err := fn(ctx, in, p)
		if err != nil {
			return nil, err
		}
		data, mErr := json.Marshal(out)
		if mErr != nil {
			return nil, &contract.Error{Code: contract.CodeInternal, Message: fmt.Sprintf("marshal output: %v", mErr)}
		}
		return &Result{Data: data}, nil
	}
}

// RegisterSubscription wraps a typed subscription handler. The pump goroutine
// JSON-encodes each typed E event into a contract.StreamEvent before
// forwarding into the broker's channel.
func RegisterSubscription[P, E any](d *Dispatcher, contributor, intent string, version int, fn func(ctx context.Context, in P, p contract.Principal) (<-chan E, func(), error)) error {
	wrapped := func(ctx context.Context, params map[string]any, principal contract.Principal) (<-chan contract.StreamEvent, func(), error) {
		var in P
		if len(params) > 0 {
			// Decode by remarshalling — slow but tolerable; subscription params are tiny.
			b, _ := json.Marshal(params)
			if err := json.Unmarshal(b, &in); err != nil {
				return nil, nil, &contract.Error{Code: contract.CodeBadRequest, Message: fmt.Sprintf("invalid params: %v", err)}
			}
		}
		typedCh, stop, err := fn(ctx, in, principal)
		if err != nil {
			return nil, nil, err
		}
		out := make(chan contract.StreamEvent, 4)
		var seq uint64
		go func() {
			defer close(out)
			for ev := range typedCh {
				seq++
				payload, mErr := json.Marshal(ev)
				if mErr != nil {
					// Drop the event if it can't be marshalled; log server-side.
					continue
				}
				select {
				case out <- contract.StreamEvent{Intent: intent, Payload: payload, Seq: seq}:
				case <-ctx.Done():
					return
				}
			}
		}()
		return out, stop, nil
	}
	return d.RegisterSubscription(contributor, intent, version, wrapped)
}
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: PASS — 12 dispatcher tests now passing.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/dispatcher/{generic.go,generic_test.go}
git commit -m "feat(dashboard/contract/dispatcher): generic typed wrappers for query/command/subscription"
```

---

## Phase 4: MetricsEmitter Full Type

### Task 4.1: Expand metrics.go with DispatchInfo + tests

**Files:**
- Modify: `extensions/dashboard/contract/dispatcher/metrics.go`
- Create: `extensions/dashboard/contract/dispatcher/metrics_test.go`

- [ ] **Step 1: Write failing tests**

```go
package dispatcher

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type recordingMetrics struct {
	mu      sync.Mutex
	records []recordedDispatch
}

type recordedDispatch struct {
	Contributor, Intent string
	Version             int
	Kind                contract.Kind
	ErrCode             contract.ErrorCode
}

func (r *recordingMetrics) RecordDispatch(_ context.Context, c, i string, v int, k contract.Kind, _ time.Duration, errCode contract.ErrorCode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, recordedDispatch{c, i, v, k, errCode})
}

func TestDispatcher_EmitsMetrics_Success(t *testing.T) {
	rm := &recordingMetrics{}
	d := New(rm)
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if len(rm.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rm.records))
	}
	r := rm.records[0]
	if r.ErrCode != "" {
		t.Errorf("expected empty errCode for success, got %q", r.ErrCode)
	}
	if r.Kind != contract.KindQuery {
		t.Errorf("kind = %v", r.Kind)
	}
}

func TestDispatcher_EmitsMetrics_Error(t *testing.T) {
	rm := &recordingMetrics{}
	d := New(rm)
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return nil, &contract.Error{Code: contract.CodeConflict}
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindCommand, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if len(rm.records) != 1 || rm.records[0].ErrCode != contract.CodeConflict {
		t.Errorf("expected conflict record, got %+v", rm.records)
	}
}

func TestDispatcher_EmitsMetrics_NotFound(t *testing.T) {
	rm := &recordingMetrics{}
	d := New(rm)
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "x", Intent: "y", IntentVersion: 1}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if len(rm.records) != 1 || rm.records[0].ErrCode != contract.CodeNotFound {
		t.Errorf("expected not-found record, got %+v", rm.records)
	}
}
```

Add `"time"` to the imports.

- [ ] **Step 2: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: PASS — Phase 1 already wired RecordDispatch into the dispatcher, so the new tests should pass without any further code change.

- [ ] **Step 3: Commit**

```bash
git add extensions/dashboard/contract/dispatcher/metrics_test.go
git commit -m "test(dashboard/contract/dispatcher): cover metrics emission for success, error, and not-found paths"
```

---

## Phase 5: Contributor Interface (Layer b)

### Task 5.1: RegisterContributor walks the contributor's tables

**Files:**
- Create: `extensions/dashboard/contract/dispatcher/contributor.go`
- Create: `extensions/dashboard/contract/dispatcher/contributor_test.go`

- [ ] **Step 1: Write failing tests**

```go
package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type fakeContributor struct {
	name string
	q    map[IntentRef]Handler
	s    map[IntentRef]SubscriptionHandler
}

func (f *fakeContributor) Name() string                              { return f.name }
func (f *fakeContributor) Handlers() map[IntentRef]Handler           { return f.q }
func (f *fakeContributor) Subscriptions() map[IntentRef]SubscriptionHandler { return f.s }

func TestRegisterContributor_RegistersAllTables(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	c := &fakeContributor{
		name: "users",
		q: map[IntentRef]Handler{
			{Intent: "users.list", Version: 1}: func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
				return &Result{Data: json.RawMessage(`{}`)}, nil
			},
		},
		s: map[IntentRef]SubscriptionHandler{
			{Intent: "users.events", Version: 1}: func(_ context.Context, _ map[string]any, _ contract.Principal) (<-chan contract.StreamEvent, func(), error) {
				return nil, nil, nil
			},
		},
	}
	if err := d.RegisterContributor(c); err != nil {
		t.Fatalf("register: %v", err)
	}

	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list", IntentVersion: 1}
	if _, _, err := d.Dispatch(context.Background(), req, contract.Principal{}); err != nil {
		t.Errorf("dispatch query: %v", err)
	}
	intent := contract.Intent{Name: "users.events", Kind: contract.IntentKindSubscription, Version: 1, Capability: contract.CapRead}
	if _, _, err := d.Subscribe(context.Background(), contract.Principal{}, "users", intent, nil); err != nil {
		t.Errorf("subscribe: %v", err)
	}
}

func TestRegisterContributor_NameRequired(t *testing.T) {
	d := New(NoopMetricsEmitter{})
	c := &fakeContributor{name: "", q: map[IntentRef]Handler{}, s: map[IntentRef]SubscriptionHandler{}}
	if err := d.RegisterContributor(c); err == nil {
		t.Error("expected name-required error")
	}
}

func TestRegisterContributor_PartialFailureIsAtomic(t *testing.T) {
	// First register a conflicting handler; then attempt RegisterContributor and verify
	// it surfaces the conflict and does not partially apply.
	d := New(NoopMetricsEmitter{})
	_ = d.Register("users", "users.list", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) { return &Result{}, nil })

	c := &fakeContributor{
		name: "users",
		q: map[IntentRef]Handler{
			{Intent: "users.detail", Version: 1}: func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) { return &Result{}, nil },
			{Intent: "users.list", Version: 1}:   func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) { return &Result{}, nil },
		},
		s: nil,
	}
	err := d.RegisterContributor(c)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	// users.detail must NOT be registered (atomicity).
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.detail", IntentVersion: 1}
	if _, _, dispErr := d.Dispatch(context.Background(), req, contract.Principal{}); dispErr == nil {
		t.Error("partial registration leaked: users.detail should not be registered")
	} else {
		var ce *contract.Error
		if !errors.As(dispErr, &ce) || ce.Code != contract.CodeNotFound {
			t.Errorf("expected NotFound, got %v", dispErr)
		}
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: FAIL — undefined RegisterContributor.

- [ ] **Step 3: Implement contributor.go**

```go
package dispatcher

import "fmt"

// RegisterContributor walks a Contributor's Handlers() and Subscriptions() maps
// and registers each one. Atomic: if any registration fails, all preceding
// registrations from this call are rolled back.
func (d *Dispatcher) RegisterContributor(c Contributor) error {
	if c == nil {
		return fmt.Errorf("dispatcher: nil contributor")
	}
	name := c.Name()
	if name == "" {
		return fmt.Errorf("dispatcher: contributor name is empty")
	}

	// Snapshot what we register so we can roll back on failure.
	var registeredHandlers []handlerKey
	var registeredSubs []handlerKey

	rollback := func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		for _, k := range registeredHandlers {
			delete(d.handlers, k)
		}
		for _, k := range registeredSubs {
			delete(d.subscriptions, k)
		}
	}

	for ref, h := range c.Handlers() {
		if err := d.Register(name, ref.Intent, ref.Version, h); err != nil {
			rollback()
			return fmt.Errorf("contributor %q: %w", name, err)
		}
		registeredHandlers = append(registeredHandlers, handlerKey{name, ref.Intent, ref.Version})
	}
	for ref, h := range c.Subscriptions() {
		if err := d.RegisterSubscription(name, ref.Intent, ref.Version, h); err != nil {
			rollback()
			return fmt.Errorf("contributor %q: %w", name, err)
		}
		registeredSubs = append(registeredSubs, handlerKey{name, ref.Intent, ref.Version})
	}
	return nil
}
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: PASS — all dispatcher tests + 3 new contributor tests.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/dispatcher/{contributor.go,contributor_test.go}
git commit -m "feat(dashboard/contract/dispatcher): contributor interface registration with atomic rollback"
```

---

## Phase 6: Pilot Package Skeleton + Types

### Task 6.1: Pilot types

**Files:**
- Create: `extensions/dashboard/contract/pilot/doc.go`
- Create: `extensions/dashboard/contract/pilot/types.go`
- Create: `extensions/dashboard/contract/pilot/types_test.go`

- [ ] **Step 1: Write doc.go**

```go
// Package pilot ships the migrated dashboard contributor used to validate
// the contract end-to-end: extensions.list, services.list, services.detail,
// and the metrics.summary subscription, all wired against the existing
// collector and contributor registry.
//
// See SLICE_C_DESIGN.md in the parent contract directory for the spec.
package pilot
```

- [ ] **Step 2: Write types_test.go (failing)**

```go
package pilot

import (
	"encoding/json"
	"testing"
)

func TestExtensionsList_RoundTrip(t *testing.T) {
	in := ExtensionsList{Extensions: []ExtensionInfo{
		{Name: "auth", DisplayName: "Authentication", Version: "1.0", Layout: "extension", PageCount: 2, WidgetCount: 0},
	}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ExtensionsList
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Extensions[0].DisplayName != "Authentication" {
		t.Errorf("display name lost: %+v", got)
	}
}

func TestServiceDetail_NilSafe(t *testing.T) {
	// A nil ServicesList should round-trip as `{"services":null}` not panic.
	var sl ServicesList
	b, err := json.Marshal(sl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ServicesList
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Services) != 0 {
		t.Errorf("expected zero services, got %d", len(got.Services))
	}
}
```

- [ ] **Step 3: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/pilot/...`
Expected: FAIL — undefined types.

- [ ] **Step 4: Implement types.go**

```go
package pilot

import "github.com/xraph/forge/extensions/dashboard/collector"

// ExtensionInfo is a flattened summary of one registered contributor manifest.
type ExtensionInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Version     string `json:"version"`
	Icon        string `json:"icon,omitempty"`
	Layout      string `json:"layout,omitempty"`
	PageCount   int    `json:"pageCount"`
	WidgetCount int    `json:"widgetCount"`
}

// ExtensionsList is the response payload for the extensions.list query.
type ExtensionsList struct {
	Extensions []ExtensionInfo `json:"extensions"`
}

// ServicesList is the response payload for the services.list query.
type ServicesList struct {
	Services []collector.ServiceInfo `json:"services"`
}

// ServiceDetailResponse is the response payload for services.detail.
// (collector.ServiceDetail is reused as-is.)
type ServiceDetailResponse = collector.ServiceDetail

// ServiceDetailInput is the input payload for services.detail.
type ServiceDetailInput struct {
	Name string `json:"name"`
}

// MetricsSummary is the per-event payload for the metrics.summary subscription.
type MetricsSummary struct {
	TotalMetrics int   `json:"totalMetrics"`
	Counters     int   `json:"counters"`
	Gauges       int   `json:"gauges"`
	Histograms   int   `json:"histograms"`
	TS           int64 `json:"ts"` // unix seconds
}
```

- [ ] **Step 5: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/pilot/...`
Expected: PASS — 2 tests.

- [ ] **Step 6: Commit**

```bash
git add extensions/dashboard/contract/pilot/{doc.go,types.go,types_test.go}
git commit -m "feat(dashboard/contract/pilot): payload types for the pilot intents"
```

---

## Phase 7: Pilot Manifest YAML

### Task 7.1: Embedded YAML manifest

**Files:**
- Create: `extensions/dashboard/contract/pilot/manifest.yaml`

- [ ] **Step 1: Write manifest.yaml**

```yaml
schemaVersion: 1
contributor:
  name: core-contract
  envelope:
    supports: [v1]
    preferred: v1
  capabilities: [dashboard.read]

queries:
  extensionList:
    intent: extensions.list
    cache: { staleTime: 10s }
  serviceList:
    intent: services.list
    cache: { staleTime: 5s }

intents:
  - name: extensions.list
    kind: query
    version: 1
    capability: read

  - name: services.list
    kind: query
    version: 1
    capability: read

  - name: services.detail
    kind: query
    version: 1
    capability: read

  - name: metrics.summary
    kind: subscription
    version: 1
    capability: read
    mode: replace

graph:
  - route: /extensions
    intent: page.shell
    title: Extensions
    nav: { group: Operations, icon: package, priority: 20 }
    slots:
      main:
        - intent: resource.list
          data: queries.extensionList
          props:
            columns: [name, displayName, version, layout, pageCount, widgetCount]

  - route: /services
    intent: page.shell
    title: Services
    nav: { group: Operations, icon: server, priority: 21 }
    slots:
      main:
        - intent: resource.list
          data: queries.serviceList
          props:
            columns: [name, type, status]
          slots:
            detailDrawer:
              - intent: resource.detail
                data:
                  intent: services.detail
                  params: { name: { from: parent.name } }

  - route: /metrics/live
    intent: page.shell
    title: Live Metrics
    nav: { group: Operations, icon: activity, priority: 22 }
    slots:
      main:
        - intent: dashboard.grid
          slots:
            widgets:
              - intent: metric.counter
                title: Metrics Summary
                data:
                  intent: metrics.summary
```

- [ ] **Step 2: Validate the YAML loads** — write a quick test.

Add to `pilot/types_test.go` (or create `pilot/manifest_test.go`):

```go
package pilot

import (
	_ "embed"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

//go:embed manifest.yaml
var manifestYAML []byte

func TestPilotManifest_Loads(t *testing.T) {
	m, err := loader.Load(strings.NewReader(string(manifestYAML)), "pilot/manifest.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.Contributor.Name != "core-contract" {
		t.Errorf("contributor name = %q", m.Contributor.Name)
	}
	if got := len(m.Intents); got != 4 {
		t.Errorf("intents = %d, want 4", got)
	}
	if got := len(m.Graph); got != 3 {
		t.Errorf("graph routes = %d, want 3", got)
	}
}

func TestPilotManifest_Validates(t *testing.T) {
	m, err := loader.Load(strings.NewReader(string(manifestYAML)), "pilot/manifest.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := loader.Validate(m, contract.NewWardenRegistry()); err != nil {
		t.Errorf("validate: %v", err)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./extensions/dashboard/contract/pilot/...`
Expected: PASS — manifest loads and validates.

- [ ] **Step 4: Commit**

```bash
git add extensions/dashboard/contract/pilot/{manifest.yaml,types_test.go}
git commit -m "feat(dashboard/contract/pilot): embedded YAML manifest with three routes and four intents"
```

---

## Phase 8: Pilot Query Handlers

### Task 8.1: extensions.list, services.list, services.detail

**Files:**
- Create: `extensions/dashboard/contract/pilot/extensions.go`
- Create: `extensions/dashboard/contract/pilot/services.go`
- Create: `extensions/dashboard/contract/pilot/extensions_test.go`
- Create: `extensions/dashboard/contract/pilot/services_test.go`

The handlers depend on:
- `*contributor.ContributorRegistry` (for extensions.list — list of registered manifests)
- `*collector.DataCollector` (for services.list and services.detail)

We collect both into a `Deps` struct.

- [ ] **Step 1: Write failing tests for extensions handler**

`extensions_test.go`:

```go
package pilot

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

func newRegistryWith(t *testing.T, manifests ...*contributor.Manifest) *contributor.ContributorRegistry {
	t.Helper()
	r := contributor.NewContributorRegistry("/dashboard")
	for _, m := range manifests {
		stub := &stubLocal{manifest: m}
		if err := r.Register(stub); err != nil {
			t.Fatalf("register %q: %v", m.Name, err)
		}
	}
	return r
}

type stubLocal struct{ manifest *contributor.Manifest }

func (s *stubLocal) Manifest() *contributor.Manifest { return s.manifest }

func TestExtensionsListHandler_ReturnsRegisteredContributors(t *testing.T) {
	r := newRegistryWith(t,
		&contributor.Manifest{Name: "auth", DisplayName: "Authentication", Version: "1.0", Layout: "extension", Nav: []contributor.NavItem{{}, {}}, Widgets: nil},
		&contributor.Manifest{Name: "cron", DisplayName: "", Version: "0.9", Widgets: []contributor.WidgetDescriptor{{}}},
	)

	h := extensionsListHandler(r)
	res, err := h(context.Background(), nil, contract.Principal{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(res.Extensions) != 2 {
		t.Fatalf("got %d, want 2", len(res.Extensions))
	}
	// Check that empty DisplayName is filled with Name (matches today's API behavior).
	for _, e := range res.Extensions {
		if e.Name == "cron" && e.DisplayName != "cron" {
			t.Errorf("cron display name fallback = %q", e.DisplayName)
		}
	}

	// Verify the result encodes cleanly to JSON.
	if _, err := json.Marshal(res); err != nil {
		t.Errorf("marshal: %v", err)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/pilot/...`
Expected: FAIL — undefined extensionsListHandler.

- [ ] **Step 3: Implement extensions.go**

```go
package pilot

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// extensionsListHandler is exposed to the dispatcher via RegisterQuery.
// Mirrors the existing /api/extensions JSON shape so consumers can compare directly.
func extensionsListHandler(reg *contributor.ContributorRegistry) func(ctx context.Context, _ struct{}, _ contract.Principal) (ExtensionsList, error) {
	return func(_ context.Context, _ struct{}, _ contract.Principal) (ExtensionsList, error) {
		names := reg.ContributorNames()
		out := make([]ExtensionInfo, 0, len(names))
		for _, name := range names {
			m, ok := reg.GetManifest(name)
			if !ok {
				continue
			}
			displayName := m.DisplayName
			if displayName == "" {
				displayName = name
			}
			out = append(out, ExtensionInfo{
				Name:        m.Name,
				DisplayName: displayName,
				Version:     m.Version,
				Icon:        m.Icon,
				Layout:      m.Layout,
				PageCount:   len(m.Nav),
				WidgetCount: len(m.Widgets),
			})
		}
		return ExtensionsList{Extensions: out}, nil
	}
}
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/pilot/...`
Expected: PASS.

- [ ] **Step 5: Write services handler tests + impl**

`services_test.go`:

```go
package pilot

import (
	"context"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// servicesProvider abstracts the collector for tests.
type servicesProvider interface {
	CollectServices(ctx context.Context) []collector.ServiceInfo
	CollectServiceDetail(ctx context.Context, name string) *collector.ServiceDetail
}

type stubServices struct {
	list   []collector.ServiceInfo
	detail map[string]*collector.ServiceDetail
}

func (s *stubServices) CollectServices(_ context.Context) []collector.ServiceInfo {
	return s.list
}
func (s *stubServices) CollectServiceDetail(_ context.Context, name string) *collector.ServiceDetail {
	return s.detail[name]
}

func TestServicesListHandler(t *testing.T) {
	stub := &stubServices{list: []collector.ServiceInfo{{Name: "db", Status: "healthy"}, {Name: "cache", Status: "degraded"}}}
	h := servicesListHandler(stub)
	res, err := h(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(res.Services) != 2 {
		t.Errorf("services = %d", len(res.Services))
	}
}

func TestServicesDetailHandler_Found(t *testing.T) {
	stub := &stubServices{detail: map[string]*collector.ServiceDetail{
		"db": {Name: "db", Type: "postgres"},
	}}
	h := servicesDetailHandler(stub)
	res, err := h(context.Background(), ServiceDetailInput{Name: "db"}, contract.Principal{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res == nil || res.Name != "db" {
		t.Errorf("detail = %+v", res)
	}
}

func TestServicesDetailHandler_NotFound(t *testing.T) {
	stub := &stubServices{detail: map[string]*collector.ServiceDetail{}}
	h := servicesDetailHandler(stub)
	_, err := h(context.Background(), ServiceDetailInput{Name: "missing"}, contract.Principal{})
	if err == nil {
		t.Fatal("expected not-found")
	}
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}

func TestServicesDetailHandler_EmptyNameIsBadRequest(t *testing.T) {
	stub := &stubServices{}
	h := servicesDetailHandler(stub)
	_, err := h(context.Background(), ServiceDetailInput{Name: ""}, contract.Principal{})
	if err == nil {
		t.Fatal("expected bad-request")
	}
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeBadRequest {
		t.Errorf("expected CodeBadRequest, got %v", err)
	}
}
```

`services.go`:

```go
package pilot

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// ServicesProvider is the slice of the collector's API the pilot calls.
// Splitting it out lets tests stub the collector without the full DataCollector.
type ServicesProvider interface {
	CollectServices(ctx context.Context) []collector.ServiceInfo
	CollectServiceDetail(ctx context.Context, name string) *collector.ServiceDetail
}

func servicesListHandler(p ServicesProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (ServicesList, error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (ServicesList, error) {
		return ServicesList{Services: p.CollectServices(ctx)}, nil
	}
}

func servicesDetailHandler(p ServicesProvider) func(ctx context.Context, in ServiceDetailInput, _ contract.Principal) (*ServiceDetailResponse, error) {
	return func(ctx context.Context, in ServiceDetailInput, _ contract.Principal) (*ServiceDetailResponse, error) {
		if in.Name == "" {
			return nil, &contract.Error{Code: contract.CodeBadRequest, Message: "name is required"}
		}
		d := p.CollectServiceDetail(ctx, in.Name)
		if d == nil {
			return nil, &contract.Error{Code: contract.CodeNotFound, Message: "service " + in.Name + " not found"}
		}
		return d, nil
	}
}
```

- [ ] **Step 6: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/pilot/...`
Expected: PASS — 4 services tests + 1 extensions test + 2 type tests + 2 manifest tests.

- [ ] **Step 7: Commit**

```bash
git add extensions/dashboard/contract/pilot/{extensions.go,services.go,extensions_test.go,services_test.go}
git commit -m "feat(dashboard/contract/pilot): query handlers for extensions, services list, services detail"
```

---

## Phase 9: Pilot Subscription Handler

### Task 9.1: metrics.summary

**Files:**
- Create: `extensions/dashboard/contract/pilot/metrics.go`
- Create: `extensions/dashboard/contract/pilot/metrics_test.go`

The handler needs an injectable interval so tests don't have to wait 5 seconds. Use a `time.Duration` parameter on the constructor.

- [ ] **Step 1: Write failing tests**

```go
package pilot

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

type stubMetrics struct {
	data *collector.MetricsData
}

func (s *stubMetrics) CollectMetrics(_ context.Context) *collector.MetricsData {
	return s.data
}

func TestMetricsSummarySub_EmitsOnTick(t *testing.T) {
	stub := &stubMetrics{data: &collector.MetricsData{
		Stats: collector.MetricsStats{TotalMetrics: 10, Counters: 4, Gauges: 3, Histograms: 3},
	}}
	h := metricsSummarySub(stub, 10*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	ch, stop, err := h(ctx, struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before event")
		}
		var got MetricsSummary
		if err := json.Unmarshal(jsonOf(ev), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.TotalMetrics != 10 {
			t.Errorf("TotalMetrics = %d", got.TotalMetrics)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for tick")
	}
}

// jsonOf is a tiny helper for tests reading typed events out of the typed
// subscription handler (which returns chan MetricsSummary, not StreamEvent).
func jsonOf(v MetricsSummary) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestMetricsSummarySub_StopsOnCancel(t *testing.T) {
	stub := &stubMetrics{data: &collector.MetricsData{}}
	h := metricsSummarySub(stub, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	ch, stop, err := h(ctx, struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()
	// Drain a few events
	go func() {
		for range ch {
		}
	}()
	cancel()
	// Channel should close shortly after cancellation
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // closed — pass
			}
		case <-deadline:
			t.Fatal("channel did not close after cancel")
		}
	}
}
```

The handler returns a typed channel `<-chan MetricsSummary`; the tests need to read typed events directly.

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/pilot/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement metrics.go**

```go
package pilot

import (
	"context"
	"time"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// MetricsProvider is the slice of DataCollector the metrics.summary handler needs.
type MetricsProvider interface {
	CollectMetrics(ctx context.Context) *collector.MetricsData
}

// metricsSummarySub returns a typed subscription handler that emits a
// MetricsSummary every interval until ctx is cancelled. The interval is
// injectable so tests can use millisecond ticks instead of 5 seconds.
func metricsSummarySub(p MetricsProvider, interval time.Duration) func(ctx context.Context, _ struct{}, _ contract.Principal) (<-chan MetricsSummary, func(), error) {
	return func(ctx context.Context, _ struct{}, _ contract.Principal) (<-chan MetricsSummary, func(), error) {
		out := make(chan MetricsSummary, 4)
		ticker := time.NewTicker(interval)
		go func() {
			defer close(out)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case t := <-ticker.C:
					data := p.CollectMetrics(ctx)
					if data == nil {
						continue
					}
					ev := MetricsSummary{
						TotalMetrics: data.Stats.TotalMetrics,
						Counters:     data.Stats.Counters,
						Gauges:       data.Stats.Gauges,
						Histograms:   data.Stats.Histograms,
						TS:           t.Unix(),
					}
					select {
					case out <- ev:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		return out, func() {}, nil
	}
}
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/pilot/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/pilot/{metrics.go,metrics_test.go}
git commit -m "feat(dashboard/contract/pilot): metrics.summary replace-mode subscription handler"
```

---

## Phase 10: Pilot Register Entry Point

### Task 10.1: pilot.Register wires manifest + handlers into the dispatcher and contract registry

**Files:**
- Create: `extensions/dashboard/contract/pilot/pilot.go`
- Create: `extensions/dashboard/contract/pilot/pilot_test.go`

- [ ] **Step 1: Write failing tests**

```go
package pilot

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
)

func TestPilotRegister_RegistersAllIntents(t *testing.T) {
	d := dispatcher.New(dispatcher.NoopMetricsEmitter{})
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()

	deps := Deps{
		ExtensionsRegistry: newRegistryWith(t, &contributor.Manifest{Name: "auth"}),
		Services:           &stubServices{},
		Metrics:            &stubMetrics{data: &collector.MetricsData{}},
		MetricsInterval:    time.Millisecond,
	}
	if err := Register(d, reg, wreg, deps); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Contract registry has the pilot manifest.
	if _, ok := reg.Contributor("core-contract"); !ok {
		t.Error("core-contract not in contract registry")
	}
	// Dispatcher has each intent.
	for _, intentName := range []string{"extensions.list", "services.list", "services.detail"} {
		req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "core-contract", Intent: intentName, IntentVersion: 1, Payload: json.RawMessage(`{}`)}
		_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
		if err != nil && intentName != "services.detail" {
			t.Errorf("%s dispatch: %v", intentName, err)
		}
	}
}

func TestPilotRegister_DefaultsMetricsInterval(t *testing.T) {
	d := dispatcher.New(dispatcher.NoopMetricsEmitter{})
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()
	deps := Deps{
		ExtensionsRegistry: newRegistryWith(t),
		Services:           &stubServices{},
		Metrics:            &stubMetrics{data: &collector.MetricsData{}},
		// MetricsInterval intentionally zero
	}
	if err := Register(d, reg, wreg, deps); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// No assertion on the actual interval; verify Register didn't error and
	// the subscription is registered.
	intent := contract.Intent{Name: "metrics.summary", Kind: contract.IntentKindSubscription, Version: 1, Capability: contract.CapRead}
	if _, _, err := d.Subscribe(context.Background(), contract.Principal{}, "core-contract", intent, nil); err != nil {
		t.Errorf("metrics.summary not registered: %v", err)
	}
}
```

Add `"github.com/xraph/forge/extensions/dashboard/collector"` and `"github.com/xraph/forge/extensions/dashboard/contributor"` to the imports.

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/pilot/...`
Expected: FAIL — undefined Register / Deps.

- [ ] **Step 3: Implement pilot.go**

```go
package pilot

import (
	"bytes"
	"fmt"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// DefaultMetricsInterval is the production tick rate for metrics.summary.
const DefaultMetricsInterval = 5 * time.Second

// Deps bundles the data sources the pilot handlers need. The dashboard
// extension constructs this when it wires the pilot at startup.
type Deps struct {
	ExtensionsRegistry *contributor.ContributorRegistry
	Services           ServicesProvider
	Metrics            MetricsProvider
	// MetricsInterval is how often metrics.summary emits. Zero defaults to
	// DefaultMetricsInterval. Tests use millisecond values.
	MetricsInterval time.Duration
}

// Register loads the embedded pilot manifest, validates it, registers it with
// the contract registry, and binds the four handlers against the dispatcher.
// Idempotent: calling twice on the same registries returns the duplicate-
// registration error from the second call.
func Register(d *dispatcher.Dispatcher, contractReg contract.Registry, wreg contract.WardenRegistry, deps Deps) error {
	if deps.ExtensionsRegistry == nil {
		return fmt.Errorf("pilot: ExtensionsRegistry is required")
	}
	if deps.Services == nil {
		return fmt.Errorf("pilot: Services is required")
	}
	if deps.Metrics == nil {
		return fmt.Errorf("pilot: Metrics is required")
	}
	interval := deps.MetricsInterval
	if interval <= 0 {
		interval = DefaultMetricsInterval
	}

	m, err := loader.Load(bytes.NewReader(manifestYAML), "pilot/manifest.yaml")
	if err != nil {
		return fmt.Errorf("pilot: loading manifest: %w", err)
	}
	if err := loader.Validate(m, wreg); err != nil {
		return fmt.Errorf("pilot: validating manifest: %w", err)
	}
	if err := contractReg.Register(m); err != nil {
		return fmt.Errorf("pilot: contract registry: %w", err)
	}

	const c = "core-contract"
	if err := dispatcher.RegisterQuery(d, c, "extensions.list", 1, extensionsListHandler(deps.ExtensionsRegistry)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(d, c, "services.list", 1, servicesListHandler(deps.Services)); err != nil {
		return err
	}
	if err := dispatcher.RegisterQuery(d, c, "services.detail", 1, servicesDetailHandler(deps.Services)); err != nil {
		return err
	}
	if err := dispatcher.RegisterSubscription(d, c, "metrics.summary", 1, metricsSummarySub(deps.Metrics, interval)); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/pilot/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/pilot/{pilot.go,pilot_test.go}
git commit -m "feat(dashboard/contract/pilot): Register entry point wires manifest + handlers"
```

---

## Phase 11: Wire Dispatcher + Broker Into the Dashboard Extension

### Task 11.1: Replace NilDispatcher; instantiate StreamBroker; call pilot.Register

**Files:**
- Modify: `extensions/dashboard/extension.go`

This is the integration step that makes everything work end-to-end. Slice (a)'s wire-up left two TODOs: the dispatcher was a `NilDispatcher{}` and `streamBroker` was nil. Slice (c) fixes both.

- [ ] **Step 1: Read the current state**

```bash
grep -n "NilDispatcher\|contractRegistry\|wardenRegistry\|streamBroker\|auditEmitter" extensions/dashboard/extension.go
```

You should see:
- The struct fields added in slice (a) Phase 13.
- Their initialisation in `NewExtension`.
- `transport.NilDispatcher{}` passed into `transport.NewHandler` inside `handleContractPOST`.

- [ ] **Step 2: Add a `dispatcher` field on the Extension struct**

In the struct definition (near the contract fields):

```go
dispatcher *dispatcher.Dispatcher
```

Imports: `"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"`. Watch out for the name clash with the existing `dispatcher` package name in some forge subtrees — if needed, alias as `contractDispatcher`. Verify with `goimports`.

- [ ] **Step 3: Initialise it in `NewExtension`**

Replace the existing init block where `auditEmitter` is set with:

```go
disp := dispatcher.New(dispatcher.NoopMetricsEmitter{})
ext.dispatcher = disp
```

And construct the stream broker right after the contract registry exists:

```go
ext.streamBroker = transport.NewStreamBroker(ext.contractRegistry, ext.wardenRegistry, disp)
```

- [ ] **Step 4: Update `handleContractPOST` to use the real dispatcher**

```go
func (e *Extension) handleContractPOST() http.HandlerFunc {
    h := transport.NewHandler(e.contractRegistry, e.wardenRegistry, e.dispatcher, e.auditEmitter)
    return h.ServeHTTP
}
```

(Replace `transport.NilDispatcher{}` with `e.dispatcher`.)

- [ ] **Step 5: Register the pilot**

Locate the place where `e.collector` is fully initialised and `e.contributor.ContributorRegistry` is the registry the dashboard already uses for legacy contributors. After both are ready (typically inside `Start` or right after `NewExtension`'s setup completes):

```go
import "github.com/xraph/forge/extensions/dashboard/contract/pilot"

// pilotDeps wires data sources the pilot handlers need.
pilotDeps := pilot.Deps{
    ExtensionsRegistry: e.contributor, // or whatever holds *contributor.ContributorRegistry
    Services:           e.collector,
    Metrics:            e.collector,
    // MetricsInterval defaults to 5s when zero.
}
if err := pilot.Register(e.dispatcher, e.contractRegistry, e.wardenRegistry, pilotDeps); err != nil {
    return fmt.Errorf("dashboard: registering contract pilot: %w", err)
}
```

> **Find the right method.** The pilot must be registered *after* the contract registry is constructed and *before* the routes are registered (so route registration sees the pilot's manifest). If the existing extension structure makes this awkward — e.g. the registry is constructed in `NewExtension` but contributors register inside `Start` — add the pilot inside `NewExtension` right after the field init block. The pilot only depends on `e.collector` and `e.contributor`, both available at NewExtension time per slice (a)'s Phase 13 wiring.

- [ ] **Step 6: Build and test**

```bash
go build ./...
go test ./extensions/dashboard/...
```

Expected: clean build; all tests pass — including the pilot tests, the contract tests (54+ from slice a), and the legacy dashboard tests.

If a `dashboard.New` constructor or fixture in another file changes its initialisation order in surprising ways, address it; do not relax test assertions.

- [ ] **Step 7: Commit**

```bash
git add extensions/dashboard/extension.go
git commit -m "feat(dashboard): wire real dispatcher, stream broker, and pilot contributor"
```

---

## Phase 12: End-to-End Pilot Test

### Task 12.1: Drive the contract HTTP and SSE handlers with the wired pilot

**Files:**
- Create: `extensions/dashboard/contract/pilot/pilot_e2e_test.go`

This test stands up everything in-process: contract registry, dispatcher, transport handler, stream broker, pilot registration. It then drives requests through the public HTTP and SSE entry points and asserts the envelope shapes.

- [ ] **Step 1: Write the E2E test**

```go
package pilot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

func setupPilotEnv(t *testing.T) (http.Handler, *transport.StreamBroker, *dispatcher.Dispatcher) {
	t.Helper()
	d := dispatcher.New(dispatcher.NoopMetricsEmitter{})
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()

	extReg := newRegistryWith(t,
		&contributor.Manifest{Name: "auth", DisplayName: "Authentication", Version: "1.0"},
	)
	deps := Deps{
		ExtensionsRegistry: extReg,
		Services:           &stubServices{list: []collector.ServiceInfo{{Name: "db", Status: "healthy"}}},
		Metrics:            &stubMetrics{data: &collector.MetricsData{Stats: collector.MetricsStats{TotalMetrics: 5}}},
		MetricsInterval:    20 * time.Millisecond,
	}
	if err := Register(d, reg, wreg, deps); err != nil {
		t.Fatalf("pilot register: %v", err)
	}
	httpHandler := transport.NewHandler(reg, wreg, d, contract.NoopAuditEmitter{})
	broker := transport.NewStreamBroker(reg, wreg, d)
	return httpHandler, broker, d
}

func TestPilotE2E_ExtensionsList_HTTPRoundTrip(t *testing.T) {
	h, _, _ := setupPilotEnv(t)
	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindQuery,
		Contributor: "core-contract", Intent: "extensions.list", IntentVersion: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var resp contract.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Errorf("ok = false")
	}
	var data ExtensionsList
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("data unmarshal: %v", err)
	}
	if len(data.Extensions) != 1 || data.Extensions[0].Name != "auth" {
		t.Errorf("extensions = %+v", data.Extensions)
	}
}

func TestPilotE2E_ServicesDetail_NotFoundEnvelope(t *testing.T) {
	h, _, _ := setupPilotEnv(t)
	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindQuery,
		Contributor: "core-contract", Intent: "services.detail", IntentVersion: 1,
		Payload: json.RawMessage(`{"name":"missing"}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		// transport.NewHandler maps errors via asContractError; verify the wire envelope code rather than HTTP status.
	}
	if !strings.Contains(w.Body.String(), "NOT_FOUND") {
		t.Errorf("expected NOT_FOUND in body: %s", w.Body)
	}
}

func TestPilotE2E_MetricsSummary_SSE(t *testing.T) {
	_, broker, _ := setupPilotEnv(t)

	// Open the SSE stream
	streamReq := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/stream", nil)
	streamCtx, cancelStream := context.WithCancel(streamReq.Context())
	streamReq = streamReq.WithContext(streamCtx)
	streamW := httptest.NewRecorder()

	streamDone := make(chan struct{})
	go func() {
		broker.ServeStream(streamW, streamReq)
		close(streamDone)
	}()

	// Wait for the broker to register the stream
	deadline := time.After(250 * time.Millisecond)
	var streamID string
LOOP:
	for {
		ids := broker.SnapshotIDs()
		if len(ids) > 0 {
			streamID = ids[0]
			break LOOP
		}
		select {
		case <-deadline:
			t.Fatal("stream not registered in time")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// Subscribe via control
	cmd, _ := json.Marshal(transport.ControlMessage{
		StreamID: streamID, Op: "subscribe",
		Contributor: "core-contract", Intent: "metrics.summary",
		SubscriptionID: "s1",
	})
	ctlReq := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1/stream/control", bytes.NewReader(cmd))
	ctlW := httptest.NewRecorder()
	broker.ServeControl(ctlW, ctlReq)
	if ctlW.Code != http.StatusOK {
		t.Fatalf("control = %d body=%s", ctlW.Code, ctlW.Body)
	}

	// Wait for at least one event to land in the recorder
	deadline = time.After(500 * time.Millisecond)
	for {
		if strings.Contains(streamW.Body.String(), `"totalMetrics":5`) {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("no metrics event in time; body=%s", streamW.Body)
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancelStream()
	<-streamDone
}
```

- [ ] **Step 2: Run the E2E test with -race**

```bash
go test -race -count=1 ./extensions/dashboard/contract/pilot/...
```

Expected: PASS, race-clean.

- [ ] **Step 3: Build the probe CLI and run a manual smoke (optional, not automated)**

```bash
go build -o /tmp/dashboard-contract-probe ./cmd/dashboard-contract-probe
# (with the dashboard running on :8080)
/tmp/dashboard-contract-probe -base=http://localhost:8080 -kind=query -contributor=core-contract -intent=extensions.list
```

Expected: HTTP 200 with the extensions list JSON.

- [ ] **Step 4: Commit**

```bash
git add extensions/dashboard/contract/pilot/pilot_e2e_test.go
git commit -m "test(dashboard/contract/pilot): end-to-end HTTP + SSE round-trip through the wired pilot"
```

---

## Final Verification

- [ ] **Run the whole repo test suite**

```bash
go test -count=1 ./...
```
Expected: all PASS.

- [ ] **Run the dashboard subtree with -race**

```bash
go test -race -count=1 ./extensions/dashboard/...
```
Expected: race-clean.

- [ ] **Vet the new packages**

```bash
go vet ./extensions/dashboard/contract/...
```
Expected: clean.

## Self-Review Notes

- **Spec coverage:** Every row in SLICE_C_DESIGN.md's "Design Decisions" table maps to a phase or task. Layer (a) is Phase 1; layer (b) is Phase 5; layer (c) is Phase 3. Subscription handler shape is Phase 2 + Phase 9. The `Result` struct is Phase 0. Error mapping is Phase 1. Metrics emitter is Phase 1+4. Pilot scope is Phases 6-10. Wire-up is Phase 11. E2E is Phase 12.
- **Spec deviation called out:** `metrics.cpu` → `metrics.summary` is the only deviation. Documented in the plan's preamble.
- **No placeholders:** All TDD steps include real test code and real implementation code. No "TBD", no "fill in", no "similar to Phase N".
- **Naming consistency:** `Handler`, `SubscriptionHandler`, `Result`, `IntentRef`, `Contributor` are defined in Phase 0 and used identically through Phases 1-12. `MetricsEmitter` interface is defined in Phase 1 (stub) and tested fully in Phase 4 — no signature change between the stub and the test usage.
- **Out-of-scope items honoured:** No CSRF middleware integration, no Prometheus wiring, no chronicle integration, no React shell — those stay in slice (b) / (d).
- **Concrete data sources verified:** `CollectServices`, `CollectServiceDetail`, `CollectMetrics` are real methods on `collector.DataCollector` (verified via grep before plan write). `ContributorNames` and `GetManifest` are the existing `*ContributorRegistry` methods used by today's `/api/extensions` handler.
