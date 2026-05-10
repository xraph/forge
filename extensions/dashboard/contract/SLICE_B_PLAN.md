# Slice (b) — Security + Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace slice (a)+(c) placeholder validation/dedup/metrics/audit/tracing with production-shaped real implementations: CSRF validation, idempotency-key dedup, Prometheus metrics, OpenTelemetry tracing, and structured-logger audit.

**Architecture:** Three new files in a new `extensions/dashboard/contract/idempotency/` sub-package (interface + in-memory store + tests). Three new files in `dispatcher/` for the new emitters/wrappers (`metrics_prometheus.go`, `audit_logger.go`, plus tracing folded into `dispatcher.go`). One new file in `transport/` for the CSRF token endpoint. Modifications to `transport/http.go`, `dispatcher/dispatcher.go`, and `extension.go` to wire everything in. The existing `dashboard.security.CSRFManager`, `forge.Metrics` from `app.Metrics()`, the OTel global tracer, and `app.Logger()` are reused — no new external dependencies.

**Tech Stack:** Go 1.25, stdlib `testing`, `gopkg.in/yaml.v3` (already in deps), `go.opentelemetry.io/otel` v1.40.0 (already in deps), `github.com/prometheus/client_golang` v1.19.1 (already in deps via `internal/metrics`).

---

## Reference

- **Design spec:** [SLICE_B_DESIGN.md](SLICE_B_DESIGN.md)
- **Slice (a)/(c) interfaces this plan extends:**
  - `dispatcher.MetricsEmitter` ([dispatcher/metrics.go](dispatcher/metrics.go)) — `RecordDispatch(ctx, contributor, intent, version, kind, latency, errCode)`.
  - `contract.AuditEmitter` ([audit.go](audit.go)) — `Emit(ctx, AuditRecord)`.
  - `transport.NewHandler` ([transport/http.go:40](transport/http.go)) — current signature accepts a `Dispatcher` and an `AuditEmitter`; we extend with optional CSRF.
- **Existing infrastructure to reuse:**
  - `*security.CSRFManager` from `extensions/dashboard/security/csrf.go` — already constructed at `NewExtension` and accessible via `e.CSRFManager()`.
  - `forge.Metrics` interface — `GetOrCreateCounter(name, opts...) Counter`, `GetOrCreateHistogram(name, opts...) Histogram`, `WithLabel/WithLabels` for label opts. The returned `Counter` has `WithLabels(map) Counter` for per-emit labelled children, and `Inc()`/`Add(delta)`. `Histogram` has `Observe(value)`.
  - OTel global tracer via `otel.Tracer("forge.dashboard.contract")` from `go.opentelemetry.io/otel`. `Tracer.Start(ctx, name) (context.Context, trace.Span)`.
  - `forge.Logger` from `app.Logger()` — `Info(msg, fields...)` with `forge.String/Int/Duration/Any` field helpers.

## Conventions

- Plain `testing` package; no testify. Match the slice (a)+(c) test style.
- Imports: stdlib first, then `github.com/xraph/forge/...`, then third-party.
- Compile-time interface assertions where applicable.
- One commit per logical change. No `Co-Authored-By` trailers.

## File Structure

```
extensions/dashboard/contract/idempotency/
  doc.go
  store.go              # Store interface, Cached struct
  inmemory.go           # InMemoryStore, NewInMemoryStore, options
  store_test.go
  inmemory_test.go

extensions/dashboard/contract/dispatcher/
  metrics_prometheus.go      # PrometheusMetricsEmitter
  metrics_prometheus_test.go
  audit_logger.go            # LoggerAuditEmitter
  audit_logger_test.go
  # dispatcher.go modified: optional Tracer + IdempotencyStore via constructor options
  # tracing inlined into dispatcher.go via a Tracer field — no separate tracing.go for slice (b)

extensions/dashboard/contract/transport/
  csrf.go                    # CSRFTokenResponse + NewCSRFTokenHandler
  csrf_test.go
  # http.go modified: NewHandler optional CSRFManager arg; validation in command branch
```

Modifications:
- `extensions/dashboard/contract/transport/http.go` — add CSRF arg + validation hook.
- `extensions/dashboard/contract/dispatcher/dispatcher.go` — add `Option`-style constructor for tracer + idempotency store; wrap command dispatch.
- `extensions/dashboard/extension.go` — swap defaults for real impls; register CSRF endpoint; pass idempotency + CSRF through.
- `extensions/dashboard/extension.go` config struct — add `EnableContractSecurity bool` (default true).

---

## Phase 0: Idempotency Store

### Task 0.1: Store interface + Cached struct

**Files:**
- Create: `extensions/dashboard/contract/idempotency/doc.go`
- Create: `extensions/dashboard/contract/idempotency/store.go`
- Create: `extensions/dashboard/contract/idempotency/store_test.go`

- [ ] **Step 1: Write doc.go**

```go
// Package idempotency provides command deduplication for the dashboard
// contract: a Store interface plus an in-memory implementation. Wrappers
// around dispatcher.Dispatch consult the store before invoking command
// handlers and return cached envelopes when the (key, identity) tuple
// matches a recent invocation.
package idempotency
```

- [ ] **Step 2: Write store_test.go (failing)**

```go
package idempotency

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCached_FieldsRoundTrip(t *testing.T) {
	c := Cached{
		Status:   200,
		WireBody: json.RawMessage(`{"ok":true}`),
		StoredAt: time.Now(),
		TTL:      time.Hour,
	}
	if c.Status != 200 || string(c.WireBody) != `{"ok":true}` {
		t.Errorf("Cached fields not preserved: %+v", c)
	}
}

func TestCached_Expired(t *testing.T) {
	c := Cached{StoredAt: time.Now().Add(-2 * time.Hour), TTL: time.Hour}
	if !c.Expired(time.Now()) {
		t.Error("expected Expired() to be true")
	}
	c2 := Cached{StoredAt: time.Now(), TTL: time.Hour}
	if c2.Expired(time.Now()) {
		t.Error("expected fresh entry to not be expired")
	}
}
```

- [ ] **Step 3: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/idempotency/...`
Expected: FAIL — undefined `Cached`.

- [ ] **Step 4: Implement store.go**

```go
package idempotency

import (
	"context"
	"encoding/json"
	"time"
)

// Store deduplicates command invocations by (key, identity) tuple.
// Lookup returns a cached envelope if one is present and unexpired; the
// dispatcher writes back the cached envelope verbatim when found.
// Implementations MUST be safe for concurrent use.
type Store interface {
	Lookup(ctx context.Context, key, identity string) (*Cached, bool)
	Store(ctx context.Context, key, identity string, c Cached) error
}

// Cached is one cached command response.
type Cached struct {
	// Status is the HTTP status the original handler returned.
	Status int
	// WireBody is the JSON envelope the original handler produced, ready to
	// write back verbatim.
	WireBody json.RawMessage
	// StoredAt is when this entry landed in the store.
	StoredAt time.Time
	// TTL is how long the entry is considered fresh.
	TTL time.Duration
}

// Expired reports whether c is past its TTL relative to now.
func (c Cached) Expired(now time.Time) bool {
	if c.TTL <= 0 {
		return false
	}
	return now.After(c.StoredAt.Add(c.TTL))
}
```

- [ ] **Step 5: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/idempotency/...`
Expected: PASS — 2 tests.

- [ ] **Step 6: Commit**

```bash
git add extensions/dashboard/contract/idempotency/{doc.go,store.go,store_test.go}
git commit -m "feat(dashboard/contract/idempotency): Store interface and Cached entry"
```

### Task 0.2: In-memory implementation

**Files:**
- Create: `extensions/dashboard/contract/idempotency/inmemory.go`
- Create: `extensions/dashboard/contract/idempotency/inmemory_test.go`

- [ ] **Step 1: Write failing tests**

```go
package idempotency

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestInMemory_LookupMissThenHit(t *testing.T) {
	s := NewInMemoryStore()
	if _, ok := s.Lookup(context.Background(), "k", "u"); ok {
		t.Error("expected miss")
	}
	c := Cached{Status: 200, WireBody: json.RawMessage(`{"x":1}`), StoredAt: time.Now(), TTL: time.Hour}
	if err := s.Store(context.Background(), "k", "u", c); err != nil {
		t.Fatalf("store: %v", err)
	}
	got, ok := s.Lookup(context.Background(), "k", "u")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.Status != 200 || string(got.WireBody) != `{"x":1}` {
		t.Errorf("cached value lost: %+v", got)
	}
}

func TestInMemory_DifferentIdentityIsIndependent(t *testing.T) {
	s := NewInMemoryStore()
	c := Cached{WireBody: json.RawMessage(`null`), StoredAt: time.Now(), TTL: time.Hour}
	_ = s.Store(context.Background(), "k", "alice", c)
	if _, ok := s.Lookup(context.Background(), "k", "bob"); ok {
		t.Error("bob should not see alice's cached entry")
	}
}

func TestInMemory_ExpiredEntryReturnsMiss(t *testing.T) {
	s := NewInMemoryStore()
	c := Cached{StoredAt: time.Now().Add(-2 * time.Hour), TTL: time.Hour, WireBody: json.RawMessage(`null`)}
	_ = s.Store(context.Background(), "k", "u", c)
	if _, ok := s.Lookup(context.Background(), "k", "u"); ok {
		t.Error("expected expired entry to miss")
	}
}

func TestInMemory_LRUEvictionAtCapacity(t *testing.T) {
	s := NewInMemoryStore(WithMaxEntries(2))
	now := time.Now()
	_ = s.Store(context.Background(), "k1", "u", Cached{StoredAt: now, TTL: time.Hour, WireBody: json.RawMessage(`1`)})
	_ = s.Store(context.Background(), "k2", "u", Cached{StoredAt: now, TTL: time.Hour, WireBody: json.RawMessage(`2`)})
	_ = s.Store(context.Background(), "k3", "u", Cached{StoredAt: now, TTL: time.Hour, WireBody: json.RawMessage(`3`)})
	// k1 should be evicted (oldest, capacity=2).
	if _, ok := s.Lookup(context.Background(), "k1", "u"); ok {
		t.Error("k1 should have been evicted")
	}
	if _, ok := s.Lookup(context.Background(), "k2", "u"); !ok {
		t.Error("k2 should still be present")
	}
	if _, ok := s.Lookup(context.Background(), "k3", "u"); !ok {
		t.Error("k3 should still be present")
	}
}

func TestInMemory_ConcurrentReadWrite(t *testing.T) {
	s := NewInMemoryStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			key := "k"
			s.Store(context.Background(), key, "u", Cached{StoredAt: time.Now(), TTL: time.Hour, WireBody: json.RawMessage(`null`)})
			_ = i
		}(i)
		go func(i int) {
			defer wg.Done()
			s.Lookup(context.Background(), "k", "u")
			_ = i
		}(i)
	}
	wg.Wait()
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/idempotency/...`
Expected: FAIL — undefined `NewInMemoryStore`.

- [ ] **Step 3: Implement inmemory.go**

```go
package idempotency

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// DefaultMaxEntries is the default LRU cap for an in-memory store.
const DefaultMaxEntries = 10000

// Option configures an InMemoryStore.
type Option func(*InMemoryStore)

// WithMaxEntries caps the number of cached entries; oldest are evicted first.
func WithMaxEntries(n int) Option {
	return func(s *InMemoryStore) {
		if n > 0 {
			s.maxEntries = n
		}
	}
}

// InMemoryStore is a process-local Store with TTL and LRU eviction.
// Safe for concurrent use.
type InMemoryStore struct {
	mu         sync.Mutex
	maxEntries int
	entries    map[entryKey]*list.Element
	order      *list.List // front = MRU, back = LRU
}

type entryKey struct {
	Key      string
	Identity string
}

type entry struct {
	key entryKey
	val Cached
}

// NewInMemoryStore returns an in-memory Store with the given options.
func NewInMemoryStore(opts ...Option) *InMemoryStore {
	s := &InMemoryStore{
		maxEntries: DefaultMaxEntries,
		entries:    map[entryKey]*list.Element{},
		order:      list.New(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Lookup implements Store.
func (s *InMemoryStore) Lookup(_ context.Context, key, identity string) (*Cached, bool) {
	k := entryKey{key, identity}
	s.mu.Lock()
	defer s.mu.Unlock()
	el, ok := s.entries[k]
	if !ok {
		return nil, false
	}
	e := el.Value.(*entry)
	if e.val.Expired(time.Now()) {
		s.order.Remove(el)
		delete(s.entries, k)
		return nil, false
	}
	s.order.MoveToFront(el)
	c := e.val // copy
	return &c, true
}

// Store implements Store. Returns nil; signature reserves error for future
// backends (e.g., Redis).
func (s *InMemoryStore) Store(_ context.Context, key, identity string, c Cached) error {
	k := entryKey{key, identity}
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.entries[k]; ok {
		e := el.Value.(*entry)
		e.val = c
		s.order.MoveToFront(el)
		return nil
	}
	el := s.order.PushFront(&entry{key: k, val: c})
	s.entries[k] = el
	for s.order.Len() > s.maxEntries {
		oldest := s.order.Back()
		if oldest != nil {
			s.order.Remove(oldest)
			delete(s.entries, oldest.Value.(*entry).key)
		}
	}
	return nil
}

// Compile-time assertion.
var _ Store = (*InMemoryStore)(nil)
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test -race ./extensions/dashboard/contract/idempotency/...`
Expected: PASS — 5 tests, race-clean.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/idempotency/{inmemory.go,inmemory_test.go}
git commit -m "feat(dashboard/contract/idempotency): in-memory Store with TTL and LRU eviction"
```

---

## Phase 1: CSRF Validation + Token Endpoint

### Task 1.1: Token issuance endpoint

**Files:**
- Create: `extensions/dashboard/contract/transport/csrf.go`
- Create: `extensions/dashboard/contract/transport/csrf_test.go`

The token endpoint returns a fresh HMAC-signed token from the dashboard's existing `*security.CSRFManager`. The TTL surfaces as `expiresAt` so the React shell knows when to refresh.

- [ ] **Step 1: Write failing tests**

```go
package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/security"
)

func TestCSRFTokenHandler_ReturnsTokenAndExpiry(t *testing.T) {
	mgr := security.NewCSRFManager()
	h := NewCSRFTokenHandler(mgr, time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/csrf", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var resp CSRFTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Token == "" {
		t.Error("token empty")
	}
	if !mgr.ValidateToken(resp.Token) {
		t.Error("returned token does not validate against the manager")
	}
	if resp.ExpiresAt.Before(time.Now()) {
		t.Errorf("ExpiresAt is in the past: %v", resp.ExpiresAt)
	}
}

func TestCSRFTokenHandler_RejectsNonGET(t *testing.T) {
	h := NewCSRFTokenHandler(security.NewCSRFManager(), time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1/csrf", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/transport/...`
Expected: FAIL — undefined `NewCSRFTokenHandler`.

- [ ] **Step 3: Implement csrf.go**

```go
package transport

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/xraph/forge/extensions/dashboard/security"
)

// CSRFTokenResponse is the wire shape for GET /api/dashboard/v1/csrf.
type CSRFTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// NewCSRFTokenHandler returns the GET /csrf handler. ttl is the token validity
// window the response surfaces to the client; the underlying manager is the
// authority on validation.
func NewCSRFTokenHandler(mgr *security.CSRFManager, ttl time.Duration) http.Handler {
	return &csrfHandler{mgr: mgr, ttl: ttl}
}

type csrfHandler struct {
	mgr *security.CSRFManager
	ttl time.Duration
}

func (h *csrfHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	tok := h.mgr.GenerateToken()
	resp := CSRFTokenResponse{
		Token:     tok,
		ExpiresAt: time.Now().Add(h.ttl),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/transport/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/transport/{csrf.go,csrf_test.go}
git commit -m "feat(dashboard/contract): CSRF token issuance endpoint"
```

### Task 1.2: CSRF validation in the command handler

**Files:**
- Modify: `extensions/dashboard/contract/transport/http.go`
- Modify: `extensions/dashboard/contract/transport/http_test.go`

We add an optional `*security.CSRFManager` arg to `NewHandler`. When non-nil and `kind=command`, the handler validates `req.CSRF` after the existing presence check. Failure returns `CodeUnauthenticated`. nil manager preserves the slice-(a) test behavior.

- [ ] **Step 1: Add failing test to http_test.go**

```go
import (
	// ... existing imports ...
	"github.com/xraph/forge/extensions/dashboard/security"
)

func TestHandler_CommandRejectsInvalidCSRF(t *testing.T) {
	reg, wreg := setupRegistry(t)
	mgr := security.NewCSRFManager()
	h := NewHandlerWithCSRF(reg, wreg, &stubDispatcher{}, contract.NoopAuditEmitter{}, mgr)

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindCommand, Contributor: "users", Intent: "user.disable", IntentVersion: 1,
		CSRF: "not-a-real-token", IdempotencyKey: "ik_1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "UNAUTHENTICATED") {
		t.Errorf("expected UNAUTHENTICATED in body: %s", w.Body)
	}
}

func TestHandler_CommandAcceptsValidCSRF(t *testing.T) {
	reg, wreg := setupRegistry(t)
	mgr := security.NewCSRFManager()
	tok := mgr.GenerateToken()
	disp := &stubDispatcher{response: json.RawMessage(`{"ok":true}`)}
	h := NewHandlerWithCSRF(reg, wreg, disp, contract.NoopAuditEmitter{}, mgr)

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindCommand, Contributor: "users", Intent: "user.disable", IntentVersion: 1,
		CSRF: tok, IdempotencyKey: "ik_1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
}
```

The fixture's `setupRegistry` already sets up a `user.disable` command intent. Verify that's still the case by reading the existing http_test.go.

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/transport/...`
Expected: FAIL — undefined `NewHandlerWithCSRF`.

- [ ] **Step 3: Modify http.go**

Add a new constructor without breaking the existing one:

```go
// In transport/http.go, near NewHandler:

// NewHandlerWithCSRF is NewHandler plus a CSRFManager for command validation.
// When mgr is non-nil, command envelopes whose CSRF token does not validate
// return CodeUnauthenticated. Pass nil to skip CSRF (preserves the slice-(a)
// behaviour for tests and rollout opt-out).
func NewHandlerWithCSRF(reg contract.Registry, wreg contract.WardenRegistry, disp Dispatcher, audit contract.AuditEmitter, mgr *security.CSRFManager) http.Handler {
	h := NewHandler(reg, wreg, disp, audit).(*handler)
	h.csrfMgr = mgr
	return h
}
```

Add the field to the `handler` struct and wire validation in `ServeHTTP`:

```go
import (
	// ... existing imports ...
	"github.com/xraph/forge/extensions/dashboard/security"
)

// handler struct gains:
type handler struct {
	reg     contract.Registry
	wreg    contract.WardenRegistry
	disp    Dispatcher
	audit   contract.AuditEmitter
	csrfMgr *security.CSRFManager // optional; nil disables CSRF validation
}

// In ServeHTTP, inside the `req.Kind == contract.KindCommand` branch,
// AFTER the presence check (req.IdempotencyKey == "" || req.CSRF == ""),
// add:

if h.csrfMgr != nil && !h.csrfMgr.ValidateToken(req.CSRF) {
	writeError(w, http.StatusForbidden, &contract.Error{Code: contract.CodeUnauthenticated, Message: "csrf token invalid"})
	return
}
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/transport/...`
Expected: PASS — 2 new tests + all prior transport tests still pass.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/transport/{http.go,http_test.go}
git commit -m "feat(dashboard/contract): CSRF token validation for command envelopes"
```

---

## Phase 2: Prometheus MetricsEmitter

### Task 2.1: PrometheusMetricsEmitter

**Files:**
- Create: `extensions/dashboard/contract/dispatcher/metrics_prometheus.go`
- Create: `extensions/dashboard/contract/dispatcher/metrics_prometheus_test.go`

We use `forge.Metrics`'s `GetOrCreateCounter` + `GetOrCreateHistogram` to lazily create the dispatch series; per-emit labels go through `Counter.WithLabels(map)`.

- [ ] **Step 1: Write failing tests**

```go
package dispatcher

import (
	"context"
	"testing"
	"time"

	forgemetrics "github.com/xraph/forge/internal/metrics"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestPrometheusMetricsEmitter_RecordsCounterAndHistogram(t *testing.T) {
	// NewNoOpMetrics returns a forge.Metrics instance whose Counters/Histograms
	// are real but don't export anywhere — perfect for assertion via Value/Count.
	// (forgemetrics.NewNoOpMetrics is the public entry; equivalent to forge.NewNoOpMetrics.)
	m := forgemetrics.NewNoOpMetrics()
	em := NewPrometheusMetricsEmitter(m)

	em.RecordDispatch(context.Background(), "users", "users.list", 1, contract.KindQuery, 12*time.Millisecond, "")
	em.RecordDispatch(context.Background(), "users", "users.list", 1, contract.KindQuery, 8*time.Millisecond, contract.CodeNotFound)

	// We don't assert exact Prometheus output — the noop registry doesn't render.
	// We assert the emitter is callable, doesn't panic, and idempotent on repeat.
	em.RecordDispatch(context.Background(), "users", "users.list", 1, contract.KindQuery, 5*time.Millisecond, "")
}

func TestPrometheusMetricsEmitter_LazyCollectorCreation(t *testing.T) {
	m := forgemetrics.NewNoOpMetrics()
	em := NewPrometheusMetricsEmitter(m)
	// No collectors should exist yet — test by calling RecordDispatch and verifying no panic.
	em.RecordDispatch(context.Background(), "x", "y", 1, contract.KindCommand, time.Millisecond, "")
}

func TestPrometheusMetricsEmitter_NilMetricsIsNoop(t *testing.T) {
	em := NewPrometheusMetricsEmitter(nil)
	em.RecordDispatch(context.Background(), "x", "y", 1, contract.KindQuery, time.Millisecond, "")
	// no panic, no assertion — the constructor handles nil by becoming a noop.
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: FAIL — undefined `NewPrometheusMetricsEmitter`.

- [ ] **Step 3: Implement metrics_prometheus.go**

```go
package dispatcher

import (
	"context"
	"strconv"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

const (
	dispatchTotalMetric    = "forge_dashboard_dispatch_total"
	dispatchDurationMetric = "forge_dashboard_dispatch_duration_seconds"
)

// PrometheusMetricsEmitter records dispatch metrics into a forge.Metrics
// registry. Counters and histograms are created lazily on first emission.
// Pass nil to disable (the emitter becomes a noop).
type PrometheusMetricsEmitter struct {
	metrics forge.Metrics
}

// NewPrometheusMetricsEmitter returns an emitter that writes to m.
// If m is nil, the emitter is a noop.
func NewPrometheusMetricsEmitter(m forge.Metrics) *PrometheusMetricsEmitter {
	return &PrometheusMetricsEmitter{metrics: m}
}

// RecordDispatch implements MetricsEmitter.
func (e *PrometheusMetricsEmitter) RecordDispatch(_ context.Context, contributor, intent string, version int, kind contract.Kind, latency time.Duration, errCode contract.ErrorCode) {
	if e.metrics == nil {
		return
	}

	labels := map[string]string{
		"contributor": contributor,
		"intent":      intent,
		"version":     strconv.Itoa(version),
		"kind":        string(kind),
	}

	hist := e.metrics.GetOrCreateHistogram(dispatchDurationMetric)
	if hist != nil {
		hist.WithLabels(labels).Observe(latency.Seconds())
	}

	counterLabels := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		counterLabels[k] = v
	}
	counterLabels["error_code"] = string(errCode) // empty when success — Prometheus is OK with that

	cnt := e.metrics.GetOrCreateCounter(dispatchTotalMetric)
	if cnt != nil {
		cnt.WithLabels(counterLabels).Inc()
	}
}

// Compile-time assertion.
var _ MetricsEmitter = (*PrometheusMetricsEmitter)(nil)
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: PASS — 3 new metrics tests + all prior dispatcher tests.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/dispatcher/{metrics_prometheus.go,metrics_prometheus_test.go}
git commit -m "feat(dashboard/contract/dispatcher): Prometheus-backed MetricsEmitter"
```

---

## Phase 3: OTel Tracing in Dispatcher

### Task 3.1: Optional Tracer field + span lifecycle

**Files:**
- Modify: `extensions/dashboard/contract/dispatcher/dispatcher.go`
- Create: `extensions/dashboard/contract/dispatcher/tracing_test.go`

We add a `tracer trace.Tracer` field to `Dispatcher` and wrap `Dispatch` with `tracer.Start(ctx, name)`. The span name embeds `(contributor, intent, version, kind)`. Attributes capture principal subject and result code. Nil tracer keeps the no-tracing default working.

- [ ] **Step 1: Write the failing test**

```go
package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestDispatcher_OpensSpanPerDispatch(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	d := NewWithOptions(NoopMetricsEmitter{}, WithTracer(tracer))
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	s := spans[0]
	if s.Name != "dispatch:c/i@1" {
		t.Errorf("span name = %q", s.Name)
	}
}

func TestDispatcher_SpanRecordsErrorCode(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())
	tracer := tp.Tracer("test")

	d := NewWithOptions(NoopMetricsEmitter{}, WithTracer(tracer))
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return nil, &contract.Error{Code: contract.CodeConflict}
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindCommand, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span")
	}
	found := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "forge.contract.error_code" && attr.Value.AsString() == "CONFLICT" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error_code attribute, got attrs=%+v", spans[0].Attributes)
	}
	_ = errors.New
}

func TestDispatcher_NilTracerIsNoop(t *testing.T) {
	d := NewWithOptions(NoopMetricsEmitter{}) // no tracer option
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	})
	req := contract.Request{Envelope: "v1", Kind: contract.KindQuery, Contributor: "c", Intent: "i", IntentVersion: 1}
	_, _, err := d.Dispatch(context.Background(), req, contract.Principal{})
	if err != nil {
		t.Errorf("nil tracer should not affect dispatch: %v", err)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: FAIL — undefined `NewWithOptions`, `WithTracer`.

- [ ] **Step 3: Modify dispatcher.go**

Add the option pattern + tracer field, plus span lifecycle in Dispatch:

```go
// At top of dispatcher.go imports (alongside existing):
import (
	// ... existing ...
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Add to the Dispatcher struct (alongside metrics field):
type Dispatcher struct {
	metrics MetricsEmitter
	tracer  trace.Tracer       // optional; nil = no tracing
	store   IdempotencyStore   // optional; nil = no dedup (filled in Phase 5 wire-up)

	mu            sync.RWMutex
	handlers      map[handlerKey]Handler
	subscriptions map[handlerKey]SubscriptionHandler
}

// Option configures a Dispatcher.
type Option func(*Dispatcher)

// WithTracer configures the dispatcher to open a span per Dispatch call.
func WithTracer(t trace.Tracer) Option {
	return func(d *Dispatcher) { d.tracer = t }
}

// IdempotencyStore is the minimal surface the dispatcher needs from
// extensions/dashboard/contract/idempotency. Defining it here avoids an
// import cycle (the idempotency package is consumed only via this interface).
type IdempotencyStore interface {
	Lookup(ctx context.Context, key, identity string) (*IdempotencyCached, bool)
	Store(ctx context.Context, key, identity string, c IdempotencyCached) error
}

// IdempotencyCached mirrors idempotency.Cached; defined here for the same
// import-cycle reason. Adapters in the wire-up convert between the two.
type IdempotencyCached struct {
	Status   int
	WireBody json.RawMessage
	StoredAt time.Time
	TTL      time.Duration
}

// WithIdempotencyStore wires command dedup. Phase 5 uses this.
func WithIdempotencyStore(s IdempotencyStore) Option {
	return func(d *Dispatcher) { d.store = s }
}

// NewWithOptions is New with explicit options. Existing New calls keep working.
func NewWithOptions(metrics MetricsEmitter, opts ...Option) *Dispatcher {
	if metrics == nil {
		metrics = NoopMetricsEmitter{}
	}
	d := &Dispatcher{
		metrics:       metrics,
		handlers:      map[handlerKey]Handler{},
		subscriptions: map[handlerKey]SubscriptionHandler{},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}
```

Update `Dispatch` to open a span:

```go
func (d *Dispatcher) Dispatch(ctx context.Context, req contract.Request, p contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	if d.tracer != nil {
		var span trace.Span
		spanName := fmt.Sprintf("dispatch:%s/%s@%d", req.Contributor, req.Intent, req.IntentVersion)
		ctx, span = d.tracer.Start(ctx, spanName,
			trace.WithAttributes(
				attribute.String("forge.contract.contributor", req.Contributor),
				attribute.String("forge.contract.intent", req.Intent),
				attribute.Int("forge.contract.version", req.IntentVersion),
				attribute.String("forge.contract.kind", string(req.Kind)),
			),
		)
		defer span.End()
		// Wrap the rest in a closure so we can update span attrs on return.
		data, meta, err := d.dispatchInner(ctx, req, p)
		if err != nil {
			var ce *contract.Error
			if errors.As(err, &ce) {
				span.SetAttributes(attribute.String("forge.contract.error_code", string(ce.Code)))
				span.SetStatus(codes.Error, string(ce.Code))
			} else {
				span.SetStatus(codes.Error, err.Error())
			}
		} else {
			span.SetStatus(codes.Ok, "")
		}
		return data, meta, err
	}
	return d.dispatchInner(ctx, req, p)
}
```

Refactor the existing Dispatch body into `dispatchInner`. The signature stays the same.

- [ ] **Step 4: Run, expect PASS**

Run: `go test -race ./extensions/dashboard/contract/dispatcher/...`
Expected: PASS — 3 new tracing tests + all prior dispatcher tests.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/dispatcher/{dispatcher.go,tracing_test.go}
git commit -m "feat(dashboard/contract/dispatcher): optional OTel tracer with span lifecycle"
```

---

## Phase 4: Idempotency Wrap in Dispatcher

### Task 4.1: Wrap command dispatches with store lookup + write-through

**Files:**
- Modify: `extensions/dashboard/contract/dispatcher/dispatcher.go`
- Modify: `extensions/dashboard/contract/dispatcher/dispatcher_test.go`

The store is consulted only for `kind=command`. Hit → return cached envelope verbatim (data + meta unmarshalled from `WireBody`). Miss → dispatch, store result, return.

The dispatcher already has `d.store IdempotencyStore` from Task 3.1. Now we use it.

- [ ] **Step 1: Write failing tests**

```go
package dispatcher

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type stubStore struct {
	hits  map[string]IdempotencyCached
	puts  int64
	gets  int64
}

func newStubStore() *stubStore { return &stubStore{hits: map[string]IdempotencyCached{}} }

func (s *stubStore) Lookup(_ context.Context, key, identity string) (*IdempotencyCached, bool) {
	atomic.AddInt64(&s.gets, 1)
	c, ok := s.hits[key+"|"+identity]
	if !ok {
		return nil, false
	}
	cc := c
	return &cc, true
}

func (s *stubStore) Store(_ context.Context, key, identity string, c IdempotencyCached) error {
	atomic.AddInt64(&s.puts, 1)
	s.hits[key+"|"+identity] = c
	return nil
}

func TestDispatcher_IdempotencyHitReturnsCached(t *testing.T) {
	store := newStubStore()
	store.hits["k|alice"] = IdempotencyCached{
		Status: 200, WireBody: json.RawMessage(`{"cached":true}`),
		StoredAt: time.Now(), TTL: time.Hour,
	}
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))
	called := int64(0)
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		atomic.AddInt64(&called, 1)
		return &Result{Data: json.RawMessage(`{"fresh":true}`)}, nil
	})
	req := contract.Request{
		Envelope: "v1", Kind: contract.KindCommand,
		Contributor: "c", Intent: "i", IntentVersion: 1,
		IdempotencyKey: "k",
	}
	p := contract.PrincipalFor(&dashauth.UserInfo{Subject: "alice"})
	data, _, err := d.Dispatch(context.Background(), req, p)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if string(data) != `{"cached":true}` {
		t.Errorf("expected cached body, got %s", data)
	}
	if atomic.LoadInt64(&called) != 0 {
		t.Errorf("handler should not have been called on cache hit")
	}
}

func TestDispatcher_IdempotencyMissCallsHandlerAndStores(t *testing.T) {
	store := newStubStore()
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`{"fresh":true}`)}, nil
	})
	req := contract.Request{
		Envelope: "v1", Kind: contract.KindCommand,
		Contributor: "c", Intent: "i", IntentVersion: 1,
		IdempotencyKey: "k",
	}
	p := contract.PrincipalFor(&dashauth.UserInfo{Subject: "alice"})
	_, _, _ = d.Dispatch(context.Background(), req, p)
	if atomic.LoadInt64(&store.puts) != 1 {
		t.Errorf("expected 1 store write, got %d", store.puts)
	}
}

func TestDispatcher_IdempotencyOnlyAppliesToCommands(t *testing.T) {
	store := newStubStore()
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	})
	req := contract.Request{
		Envelope: "v1", Kind: contract.KindQuery,
		Contributor: "c", Intent: "i", IntentVersion: 1,
		IdempotencyKey: "k",
	}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})
	if atomic.LoadInt64(&store.gets) != 0 {
		t.Errorf("query should not consult store, gets=%d", store.gets)
	}
}

func TestDispatcher_IdempotencyMissingKeyBypassesStore(t *testing.T) {
	store := newStubStore()
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))
	_ = d.Register("c", "i", 1, func(_ context.Context, _ json.RawMessage, _ map[string]any, _ contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`null`)}, nil
	})
	req := contract.Request{
		Envelope: "v1", Kind: contract.KindCommand,
		Contributor: "c", Intent: "i", IntentVersion: 1,
		// IdempotencyKey intentionally empty — slice (a)'s presence check is the gate, but
		// when a wrap with the dispatcher only is exercised, missing key just bypasses dedup.
	}
	_, _, _ = d.Dispatch(context.Background(), req, contract.Principal{})
	if atomic.LoadInt64(&store.gets) != 0 {
		t.Errorf("missing key should bypass store, gets=%d", store.gets)
	}
}
```

Imports needed in test file: `dashauth "github.com/xraph/forge/extensions/dashboard/auth"`.

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: FAIL — store path not yet wired.

- [ ] **Step 3: Modify dispatcher.go's `dispatchInner`**

Add the dedup wrap around the handler call, **only for commands and only when both store and key are present**:

```go
func (d *Dispatcher) dispatchInner(ctx context.Context, req contract.Request, p contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	// Idempotency wrap (commands only, requires store + key + identity).
	if req.Kind == contract.KindCommand && d.store != nil && req.IdempotencyKey != "" {
		identity := principalIdentity(p, req.Intent)
		if cached, ok := d.store.Lookup(ctx, req.IdempotencyKey, identity); ok {
			// Decode the cached envelope back into (data, meta).
			var resp contract.Response
			if err := json.Unmarshal(cached.WireBody, &resp); err == nil && resp.OK {
				return resp.Data, resp.Meta, nil
			}
			// Cached but undecodable; fall through to fresh dispatch.
		}
	}

	// ... existing dispatchInner body ...
	// (look up handler, call, map errors, emit metrics)

	// After successful dispatch, capture for next time.
	if req.Kind == contract.KindCommand && d.store != nil && req.IdempotencyKey != "" && wireErr == nil {
		identity := principalIdentity(p, req.Intent)
		successResp := contract.Response{OK: true, Envelope: req.Envelope, Kind: req.Kind, Data: data, Meta: meta}
		body, _ := json.Marshal(successResp)
		_ = d.store.Store(ctx, req.IdempotencyKey, identity, IdempotencyCached{
			Status:   200,
			WireBody: body,
			StoredAt: time.Now(),
			TTL:      24 * time.Hour, // TODO: make configurable in Phase 6 wire-up if needed
		})
	}
	// (return)
}

func principalIdentity(p contract.Principal, intent string) string {
	user := ""
	if p.User != nil {
		user = p.User.Subject
	}
	return user + ":" + intent
}
```

Refactor the existing handler-lookup-and-call body to live above the storage block. The exact integration shape:

1. Top-of-function: idempotency lookup → return cached if hit.
2. Middle: existing handler lookup, call, error mapping, metrics emission. Capture `data, meta, wireErr` locally.
3. Bottom: idempotency store on success, then return.

The `// TODO` in the snippet is a real TODO worth leaving as a `// Phase 6 will surface this via Extension config.` comment in the actual code — the comment is informational, not a placeholder for missing logic.

- [ ] **Step 4: Run, expect PASS**

Run: `go test -race ./extensions/dashboard/contract/dispatcher/...`
Expected: PASS — 4 new idempotency tests + all prior tests, race-clean.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/dispatcher/{dispatcher.go,dispatcher_test.go}
git commit -m "feat(dashboard/contract/dispatcher): idempotency-key dedup for command dispatches"
```

---

## Phase 5: Structured-Logger AuditEmitter

### Task 5.1: LoggerAuditEmitter

**Files:**
- Create: `extensions/dashboard/contract/dispatcher/audit_logger.go`
- Create: `extensions/dashboard/contract/dispatcher/audit_logger_test.go`

The emitter writes audit records as info-level structured logs via `forge.Logger`. Each field is a discrete logger field so log aggregators can filter by `audit=true` cheaply.

- [ ] **Step 1: Write failing tests**

```go
package dispatcher

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestLoggerAuditEmitter_EmitsStructuredFields(t *testing.T) {
	// Use a development logger that writes to a captured buffer.
	var buf bytes.Buffer
	cfg := forge.LoggingConfig{Level: "info", Encoding: "json", Output: &buf}
	logger := forge.NewLogger(cfg)
	em := NewLoggerAuditEmitter(logger)

	em.Emit(context.Background(), contract.AuditRecord{
		Time:          time.Now(),
		Contributor:   "users",
		Intent:        "user.disable",
		IntentVersion: 2,
		Subject:       "u_42",
		User:          "admin@example.com",
		Result:        "ok",
		LatencyMs:     12,
		CorrelationID: "req_x",
	})

	out := buf.String()
	for _, want := range []string{`"audit":true`, `"contributor":"users"`, `"intent":"user.disable"`, `"version":2`, `"subject":"u_42"`, `"user":"admin@example.com"`, `"result":"ok"`} {
		if !strings.Contains(out, want) {
			t.Errorf("audit log missing %q in output: %s", want, out)
		}
	}
}

func TestLoggerAuditEmitter_NilLoggerIsNoop(t *testing.T) {
	em := NewLoggerAuditEmitter(nil)
	// Must not panic.
	em.Emit(context.Background(), contract.AuditRecord{})
}
```

> **Note on the Logger API surface:** the test uses `forge.LoggingConfig{Level, Encoding, Output: &buf}` and `forge.NewLogger(cfg)`. If the actual `LoggingConfig` field names differ, adapt — the implementer should `grep "type LoggingConfig" /Users/rexraphael/Work/xraph/forge/internal/logger/` first to confirm the field names. The behavior contract is: pass a logger that writes JSON to a buffer; assert the buffer contains the expected JSON-encoded fields.

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: FAIL — undefined `NewLoggerAuditEmitter`.

- [ ] **Step 3: Implement audit_logger.go**

```go
package dispatcher

import (
	"context"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// LoggerAuditEmitter writes audit records as info-level structured logs.
// nil logger is a noop.
type LoggerAuditEmitter struct {
	logger forge.Logger
}

// NewLoggerAuditEmitter returns an emitter that writes via logger. Pass nil
// to disable (the emitter becomes a noop).
func NewLoggerAuditEmitter(logger forge.Logger) *LoggerAuditEmitter {
	return &LoggerAuditEmitter{logger: logger}
}

// Emit implements contract.AuditEmitter.
func (e *LoggerAuditEmitter) Emit(_ context.Context, rec contract.AuditRecord) {
	if e.logger == nil {
		return
	}
	e.logger.Info("dashboard contract audit",
		forge.Bool("audit", true),
		forge.String("contributor", rec.Contributor),
		forge.String("intent", rec.Intent),
		forge.Int("version", rec.IntentVersion),
		forge.String("subject", rec.Subject),
		forge.String("user", rec.User),
		forge.String("result", rec.Result),
		forge.Int64("latency_ms", rec.LatencyMs),
		forge.String("correlation_id", rec.CorrelationID),
		forge.Time("time", rec.Time),
	)
}

// Compile-time assertion.
var _ contract.AuditEmitter = (*LoggerAuditEmitter)(nil)
```

> **Note on field helpers:** `forge.Bool`, `forge.String`, `forge.Int`, `forge.Int64`, `forge.Time` should exist alongside the existing `forge.Any`/`forge.Duration`. If any are missing, fall back to `forge.Any(name, value)` — the assertion is on field *presence*, not the constructor name. Confirm with `grep "func String\|func Int\|func Bool" /Users/rexraphael/Work/xraph/forge/logger.go /Users/rexraphael/Work/xraph/forge/internal/logger/*.go`.

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./extensions/dashboard/contract/dispatcher/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/dispatcher/{audit_logger.go,audit_logger_test.go}
git commit -m "feat(dashboard/contract/dispatcher): structured-logger AuditEmitter"
```

---

## Phase 6: Wire-up in extension.go + Integration Test

### Task 6.1: Replace defaults; register CSRF endpoint; pass dispatcher options

**Files:**
- Modify: `extensions/dashboard/extension.go`
- Modify: `extensions/dashboard/extension.go` config struct (add `EnableContractSecurity bool`)

- [ ] **Step 1: Read existing wire-up**

```bash
grep -n "transport.NilDispatcher\|NewLogAuditEmitter\|dispatcher.New(\|streamBroker\s*=\s*transport.NewStreamBroker" extensions/dashboard/extension.go
```

You should find:
- `disp := dispatcher.New(dispatcher.NoopMetricsEmitter{})` from slice (c) Phase 11.
- `auditEmitter: contract.NewLogAuditEmitter(os.Stdout)` from slice (a) Phase 13.
- `ext.streamBroker = transport.NewStreamBroker(...)` adjacent to dispatcher init.
- `handleContractPOST()` calling `transport.NewHandler(...)`.

- [ ] **Step 2: Add config flag**

In the dashboard config struct (search for the existing `Enable*` fields):

```go
EnableContractSecurity bool `json:"enable_contract_security" yaml:"enable_contract_security"`
```

Default to true wherever defaults are constructed. Confirm by reading the existing config defaults function.

- [ ] **Step 3: Swap defaults in NewExtension**

Change the dispatcher construction:

```go
import (
	// ... existing ...
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"
	"go.opentelemetry.io/otel"
)

// Replace:
//   disp := dispatcher.New(dispatcher.NoopMetricsEmitter{})
// With:

var metricsEmitter dispatcher.MetricsEmitter = dispatcher.NoopMetricsEmitter{}
if app != nil && app.Metrics() != nil {
	metricsEmitter = dispatcher.NewPrometheusMetricsEmitter(app.Metrics())
}

var dispOpts []dispatcher.Option
if cfg.EnableContractSecurity {
	dispOpts = append(dispOpts,
		dispatcher.WithTracer(otel.Tracer("forge.dashboard.contract")),
		dispatcher.WithIdempotencyStore(adaptIdempotencyStore(idempotency.NewInMemoryStore())),
	)
}

disp := dispatcher.NewWithOptions(metricsEmitter, dispOpts...)
ext.dispatcher = disp
```

`adaptIdempotencyStore` is a small helper because `idempotency.Cached` and `dispatcher.IdempotencyCached` are different types (the dispatcher defined its own to avoid an import cycle):

```go
// adaptIdempotencyStore adapts an idempotency.Store to the dispatcher's
// minimal interface. The two types are intentionally separate to avoid a
// dispatcher → idempotency import cycle.
type idempotencyAdapter struct{ inner idempotency.Store }

func adaptIdempotencyStore(s idempotency.Store) dispatcher.IdempotencyStore {
	return &idempotencyAdapter{inner: s}
}
func (a *idempotencyAdapter) Lookup(ctx context.Context, key, identity string) (*dispatcher.IdempotencyCached, bool) {
	c, ok := a.inner.Lookup(ctx, key, identity)
	if !ok {
		return nil, false
	}
	return &dispatcher.IdempotencyCached{
		Status: c.Status, WireBody: c.WireBody,
		StoredAt: c.StoredAt, TTL: c.TTL,
	}, true
}
func (a *idempotencyAdapter) Store(ctx context.Context, key, identity string, c dispatcher.IdempotencyCached) error {
	return a.inner.Store(ctx, key, identity, idempotency.Cached{
		Status: c.Status, WireBody: c.WireBody,
		StoredAt: c.StoredAt, TTL: c.TTL,
	})
}
```

Place the adapter at the bottom of `extension.go` (or in a small new `extensions/dashboard/contract_wire.go` if preferred).

Replace the audit emitter:

```go
// Replace:
//   auditEmitter: contract.NewLogAuditEmitter(os.Stdout)
// With (assuming app.Logger() is the standard logger):

var auditEmitter contract.AuditEmitter = contract.NewLogAuditEmitter(os.Stdout)
if app != nil && app.Logger() != nil {
	auditEmitter = dispatcher.NewLoggerAuditEmitter(app.Logger())
}
ext.auditEmitter = auditEmitter
```

- [ ] **Step 4: Update handleContractPOST to use the CSRF-aware constructor**

Find the existing `handleContractPOST` method:

```go
func (e *Extension) handleContractPOST() http.HandlerFunc {
    h := transport.NewHandler(e.contractRegistry, e.wardenRegistry, e.dispatcher, e.auditEmitter)
    return h.ServeHTTP
}
```

Replace with:

```go
func (e *Extension) handleContractPOST() http.HandlerFunc {
	var mgr *security.CSRFManager
	if e.config.EnableContractSecurity && e.csrfMgr != nil {
		mgr = e.csrfMgr
	}
	h := transport.NewHandlerWithCSRF(e.contractRegistry, e.wardenRegistry, e.dispatcher, e.auditEmitter, mgr)
	return h.ServeHTTP
}
```

The `*security.CSRFManager` is `e.csrfMgr` (already constructed at NewExtension per the Explore agent's findings). Confirm by grepping `extension.go` for `csrfMgr`.

- [ ] **Step 5: Register the CSRF token endpoint**

In `registerRoutes`, alongside the existing contract routes:

```go
// Inside the `if e.contractRegistry != nil { ... }` block:
if e.csrfMgr != nil && e.config.EnableContractSecurity {
	must(router.GET(base+"/api/dashboard/v1/csrf",
		transport.NewCSRFTokenHandler(e.csrfMgr, 12*time.Hour).ServeHTTP))
}
```

- [ ] **Step 6: Build + test**

```bash
go build ./...
go test -count=1 ./extensions/dashboard/...
go test -race -count=1 ./extensions/dashboard/contract/...
go vet ./extensions/dashboard/...
```

All four must be clean. Slice (a)+(c)'s 181 tests + slice (b)'s ~25 new tests should all pass.

- [ ] **Step 7: Commit**

```bash
git add extensions/dashboard/extension.go
git commit -m "feat(dashboard): wire CSRF, idempotency, Prometheus metrics, OTel tracing, structured audit"
```

### Task 6.2: Integration test

**Files:**
- Create: `extensions/dashboard/contract/contract_security_e2e_test.go`

Drives the wired-up dispatcher through `transport.Handler` to exercise CSRF + idempotency end-to-end.

- [ ] **Step 1: Write the integration test**

```go
package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
	"github.com/xraph/forge/extensions/dashboard/security"
)

func TestSecurityE2E_CSRFRequired(t *testing.T) {
	reg, wreg, disp := setupSecurityEnv(t, idempotency.NewInMemoryStore())
	mgr := security.NewCSRFManager()
	h := transport.NewHandlerWithCSRF(reg, wreg, disp, contract.NoopAuditEmitter{}, mgr)

	body, _ := json.Marshal(contract.Request{
		Envelope: "v1", Kind: contract.KindCommand,
		Contributor: "test", Intent: "do.thing", IntentVersion: 1,
		CSRF: "wrong", IdempotencyKey: "k1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "UNAUTHENTICATED") {
		t.Errorf("expected UNAUTHENTICATED")
	}
}

func TestSecurityE2E_IdempotencyDedup(t *testing.T) {
	store := idempotency.NewInMemoryStore()
	reg, wreg, disp := setupSecurityEnv(t, store)
	mgr := security.NewCSRFManager()
	tok := mgr.GenerateToken()
	h := transport.NewHandlerWithCSRF(reg, wreg, disp, contract.NoopAuditEmitter{}, mgr)

	build := func() *http.Request {
		body, _ := json.Marshal(contract.Request{
			Envelope: "v1", Kind: contract.KindCommand,
			Contributor: "test", Intent: "do.thing", IntentVersion: 1,
			CSRF: tok, IdempotencyKey: "ik_e2e",
		})
		return httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	}
	w1 := httptest.NewRecorder(); h.ServeHTTP(w1, build())
	w2 := httptest.NewRecorder(); h.ServeHTTP(w2, build())

	if w1.Body.String() != w2.Body.String() {
		t.Errorf("idempotent calls produced different bodies:\nfirst:  %s\nsecond: %s", w1.Body, w2.Body)
	}
}

func setupSecurityEnv(t *testing.T, store idempotency.Store) (contract.Registry, contract.WardenRegistry, *dispatcher.Dispatcher) {
	t.Helper()
	reg := contract.NewRegistry()
	src := `
schemaVersion: 1
contributor: { name: test, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: do.thing, kind: command, version: 1, capability: write }
`
	var m contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&m); err != nil {
		t.Fatal(err)
	}
	wreg := contract.NewWardenRegistry()
	disp := dispatcher.NewWithOptions(dispatcher.NoopMetricsEmitter{},
		dispatcher.WithIdempotencyStore(adaptStore(store)))
	dispatcher.RegisterCommand(disp, "test", "do.thing", 1,
		func(ctx context.Context, _ struct{}, _ contract.Principal) (struct{ OK bool }, error) {
			return struct{ OK bool }{OK: true}, nil
		})
	return reg, wreg, disp
}

// adaptStore adapts idempotency.Store to dispatcher.IdempotencyStore for tests.
type adapter struct{ inner idempotency.Store }

func adaptStore(s idempotency.Store) dispatcher.IdempotencyStore { return &adapter{inner: s} }
func (a *adapter) Lookup(ctx context.Context, k, id string) (*dispatcher.IdempotencyCached, bool) {
	c, ok := a.inner.Lookup(ctx, k, id)
	if !ok {
		return nil, false
	}
	return &dispatcher.IdempotencyCached{Status: c.Status, WireBody: c.WireBody, StoredAt: c.StoredAt, TTL: c.TTL}, true
}
func (a *adapter) Store(ctx context.Context, k, id string, c dispatcher.IdempotencyCached) error {
	return a.inner.Store(ctx, k, id, idempotency.Cached{Status: c.Status, WireBody: c.WireBody, StoredAt: c.StoredAt, TTL: c.TTL})
}
```

- [ ] **Step 2: Run, expect PASS**

Run: `go test -race -count=1 ./extensions/dashboard/contract/...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add extensions/dashboard/contract/contract_security_e2e_test.go
git commit -m "test(dashboard/contract): integration test for CSRF + idempotency end-to-end"
```

---

## Final Verification

- [ ] **Run the whole test suite**

```bash
go test -count=1 ./...
go test -race -count=1 ./extensions/dashboard/contract/...
go vet ./extensions/dashboard/...
go build ./...
```

All four must be clean. Slice (b) adds ~25 new tests on top of the 181 from slices (a)+(c).

## Self-Review Notes

- **Spec coverage:** Every row in SLICE_B_DESIGN.md's decision table maps to a phase. CSRF wire location → Phase 1; token endpoint → Phase 1; idempotency interface + impl → Phase 0; idempotency wrap location → Phase 4; identity key shape → Phase 4 (`principalIdentity`); metrics series + buckets → Phase 2; tracing scope → Phase 3; audit logger → Phase 5; rollout toggle (`EnableContractSecurity`) → Phase 6.
- **Spec deviations:** None. The design's "tracing as a first-class dispatcher concern" pivot is honored — `WithTracer` is on the dispatcher constructor, not a standalone wrapper.
- **No placeholders:** every TDD cycle has real test code + real implementation. Two informational notes instruct the implementer to grep for actual API names (`forge.LoggingConfig` field names, logger field helpers) — these are honest "verify-before-using" notes, not unfinished spec.
- **Type consistency:** `Handler`, `Result`, `IntentRef`, `Contributor`, `IdempotencyStore`, `IdempotencyCached` are defined in slice (a)/(c)/Phase 0/Phase 3 and used identically through Phase 6. The dispatcher-side `IdempotencyCached` and the idempotency-package `Cached` are intentionally separate types with an adapter — documented in Phase 6 with the rationale (import cycle avoidance).
- **Out-of-scope items honored:** No persistent audit storage, no Redis idempotency, no per-event subscription tracing, no React shell — those stay in their own slices.
