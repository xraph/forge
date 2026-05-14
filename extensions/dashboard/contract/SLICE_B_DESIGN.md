# Slice (b) — Security + Observability Bundle

> Companion design doc to [DESIGN.md](DESIGN.md). Slice (b) replaces the placeholder validation, dedup, metrics, audit, and tracing implementations in slices (a) and (c) with production-shaped real ones.

## Context

Slice (a) shipped the contract handler with `csrf` and `idempotencyKey` envelope fields *enforced by presence* but never validated. Slice (c) ships a `MetricsEmitter` interface with a noop default and an `AuditEmitter` with a stdout-log default. Slice (b) replaces the placeholders with real implementations: CSRF token validation, idempotency-key deduplication, Prometheus-backed metrics, OpenTelemetry tracing, and a structured-logger audit emitter. After slice (b) the contract is production-shaped: every command is CSRF-validated, idempotent across retries, traced end-to-end, metrics-counted by the same series the rest of forge uses, and audited via the existing zap-backed logger.

This bundle is one shippable security + observability layer. Persistent audit storage is **explicitly punted** — slice (b) emits structured audit records via `app.Logger()` and trusts deployment-side aggregation (ELK / Datadog / similar). A future slice can layer a persistent `AuditStore` interface on top without re-shaping anything we ship here.

## Scope

**In scope:**
- CSRF token validation in the command handler path (slice (a)'s presence-only check stays as a cheap pre-flight).
- Token issuance endpoint at `GET /api/dashboard/v1/csrf`.
- Idempotency-key deduplication store (interface + in-memory impl) wrapped around command dispatch.
- Prometheus-backed `MetricsEmitter` implementation using `app.Metrics()`.
- OpenTelemetry tracing wrapper around `Dispatcher.Dispatch` and subscription connections.
- Structured-logger `AuditEmitter` using `app.Logger()`.
- Wire-up in `extension.go` replacing the noop/log defaults.
- Configuration toggle (`EnableContractSecurity`) so deployments can opt out during rollout.

**Out of scope (future slices):**
- Persistent audit storage (`AuditStore` interface + SQL/Redis backend) — explicitly punted; structured logging covers near-term needs.
- Redis-backed idempotency store for multi-instance deployments — interface is shaped to accept it later; in-memory ships first.
- Stateful CSRF (session-bound) via `extensions/security/csrf.go` — current dashboard `CSRFManager` is stateless HMAC and sufficient.
- Per-event subscription tracing (cardinality concerns).
- React shell rendering engine (slice d), built-in intent vocabulary (slice e), templ retirement (slice f).

## Design Decisions

| Decision | Choice |
|---|---|
| CSRF backend | Reuse `extensions/dashboard/security.CSRFManager` (stateless HMAC, already wired on `Extension`). No new manager. |
| CSRF wire location | Inside `transport.handler.ServeHTTP`'s command branch, after the presence check. Mismatch returns `CodeUnauthenticated`. |
| Token issuance | New `GET /api/dashboard/v1/csrf` endpoint returning `{token, expiresAt}` (12h TTL). Same v1 versioning as the rest of the contract. |
| Idempotency store | Build from scratch: `Store` interface + in-memory implementation with TTL eviction and sharded concurrent map. Future Redis impl plugs in via the same interface. |
| Idempotency identity key | `Principal.User.Subject + ":" + intent` — same key from same user against same intent dedupes; same key from different users is independent. |
| Idempotency wrap location | Around `Dispatcher.Dispatch` for commands only. Lookup → return cached envelope verbatim; miss → dispatch, capture, store. |
| Idempotency TTL | 24 hours default, configurable. |
| Failure mode for idempotency | Read errors fail open (treat as miss); write errors log and proceed (best-effort caching). |
| Metrics backend | `forge.Metrics` from `app.Metrics()` (Prometheus-backed). Lazy collector creation on first emit. |
| Metrics series | `forge_dashboard_dispatch_total{contributor,intent,version,kind,error_code}` (counter); `forge_dashboard_dispatch_duration_seconds{contributor,intent,version,kind}` (histogram). |
| Histogram buckets | 1ms, 5ms, 10ms, 50ms, 100ms, 500ms, 1s, 5s. |
| Tracing backend | `internal/observability/otel_tracer.go` v1.40.0. Global tracer via `otel.Tracer("forge.dashboard.contract")`. |
| Tracing scope | One span per dispatch (query/command/graph); one long-lived span per subscription connection (events as span events, not child spans). |
| Tracing decorator | `WithTracing(MetricsEmitter) MetricsEmitter` — composes with the Prometheus emitter; tracing inherits the same call surface. |
| Audit backend | Structured logger via `app.Logger()` with stable field set (`audit=true, contributor, intent, version, subject, user, result, latency_ms, correlation_id`). |
| Audit persistence | Out of scope. Deployment-side aggregation handles long-term storage. |
| Rollout toggle | `EnableContractSecurity` config flag (default `true`). When `false`, falls back to the slice-(a)/(c) noop/log defaults — useful for first-run shakeout in a new environment. |

## Components

### CSRF (`transport/csrf.go` + edit to `transport/http.go`)

Token endpoint:
```go
// transport/csrf.go
type CSRFTokenResponse struct {
    Token     string    `json:"token"`
    ExpiresAt time.Time `json:"expiresAt"`
}

func NewCSRFTokenHandler(mgr *security.CSRFManager) http.Handler { /* GET-only; returns CSRFTokenResponse */ }
```

Validation in the existing handler:
```go
// transport/http.go — inside ServeHTTP, after the presence check for kind=command
if h.csrfMgr != nil && !h.csrfMgr.ValidateToken(req.CSRF) {
    writeError(w, http.StatusForbidden, &contract.Error{Code: contract.CodeUnauthenticated, Message: "csrf token invalid"})
    return
}
```

`NewHandler` gains an optional `*security.CSRFManager` parameter (or via a setter to keep the existing signature stable). When nil, no validation runs — preserves the slice-(a) test behavior.

### Idempotency store (`idempotency/`)

```go
// idempotency/store.go
type Store interface {
    Lookup(ctx context.Context, key, identity string) (*Cached, bool)
    Store(ctx context.Context, key, identity string, c Cached) error
}

type Cached struct {
    Status   int             // HTTP status that was returned
    WireBody json.RawMessage // the cached envelope, ready to write back verbatim
    StoredAt time.Time
    TTL      time.Duration
}

// idempotency/inmemory.go
type InMemoryStore struct { /* sharded sync.Map + LRU + TTL eviction */ }
func NewInMemoryStore(opts ...Option) *InMemoryStore
```

Wrap location: `Dispatcher.Dispatch` checks the store before invoking the handler when `kind=command`. The HTTP transport is responsible for serializing the cached `WireBody` back to the client. This keeps the dispatcher's contract clean (handler is the only side-effect surface) while ensuring dedup is consistent across all entry points.

### Prometheus emitter (`dispatcher/metrics_prometheus.go`)

```go
type PrometheusMetricsEmitter struct { /* lazy counter + histogram */ }

func NewPrometheusMetricsEmitter(m forge.Metrics) *PrometheusMetricsEmitter

// Implements dispatcher.MetricsEmitter:
func (e *PrometheusMetricsEmitter) RecordDispatch(ctx context.Context, contributor, intent string, version int, kind contract.Kind, latency time.Duration, errCode contract.ErrorCode)
```

Lazy creation on first emission keeps startup fast and avoids registering collectors for intents never actually called. Labels keyed at registration; values applied per emit via the existing `WithLabel`/`WithLabels` helpers in `internal/metrics`.

### Tracing wrapper (`dispatcher/tracing.go`)

```go
func WithTracing(inner MetricsEmitter) MetricsEmitter

// Internally:
type tracingEmitter struct { inner MetricsEmitter; tracer trace.Tracer }
```

The wrapper opens a span when `RecordDispatch` is called (... actually, since `RecordDispatch` is called *after* the handler returns, the span needs to start earlier. So `WithTracing` is not just an emitter wrapper — it needs a hook at the start of `Dispatch` too).

**Revised approach:** `Dispatcher.Dispatch` itself opens a span before invoking the handler and closes it on return. The span lives in a context value carried through to `RecordDispatch`, which adds the `error_code` attribute and sets status. This keeps tracing as a first-class concern of the dispatcher rather than a metrics decorator.

Implementation: `dispatcher.go` gains a `tracer trace.Tracer` field set at construction; `Dispatch` opens a span; `RecordDispatch` (or its internal counterpart) sets attributes. No separate decorator needed.

### Audit logger emitter (`dispatcher/audit_logger.go`)

```go
func NewLoggerAuditEmitter(logger forge.Logger) contract.AuditEmitter

// Implements contract.AuditEmitter:
func (e *loggerAuditEmitter) Emit(ctx context.Context, rec contract.AuditRecord)
```

Emits at info level with structured fields. Replaces `contract.NewLogAuditEmitter(os.Stdout)` in the wire-up.

## Files Affected

### New
```
extensions/dashboard/contract/idempotency/
  doc.go
  store.go              # Store interface + Cached struct
  inmemory.go           # InMemoryStore with TTL+LRU
  store_test.go
  inmemory_test.go

extensions/dashboard/contract/dispatcher/
  metrics_prometheus.go
  tracing.go            # tracer field + Dispatch span lifecycle
  audit_logger.go
  metrics_prometheus_test.go
  tracing_test.go
  audit_logger_test.go

extensions/dashboard/contract/transport/
  csrf.go               # CSRFTokenResponse + NewCSRFTokenHandler
  csrf_test.go

extensions/dashboard/contract/SLICE_B_DESIGN.md   # this file
extensions/dashboard/contract/SLICE_B_PLAN.md     # produced via writing-plans skill
```

### Modified
- `extensions/dashboard/contract/transport/http.go` — add CSRF validation in command branch; constructor accepts optional `*security.CSRFManager`.
- `extensions/dashboard/contract/dispatcher/dispatcher.go` — accept idempotency store via constructor option; accept optional `trace.Tracer`; wrap command dispatches with store lookup + span.
- `extensions/dashboard/extension.go` — swap noop/log defaults for real impls; register CSRF token endpoint; pass idempotency store + CSRF manager through to handler.
- `extensions/dashboard/contract/dispatcher/dispatcher_test.go` — add tests for the new optional constructor inputs (idempotency, tracer).
- `extensions/dashboard/extension.go` config struct — add `EnableContractSecurity bool` (defaulting true).

### Reused (do not duplicate)
- `extensions/dashboard/security.CSRFManager` — already constructed at `NewExtension`. Inject directly into the transport handler.
- `internal/metrics.Registry` via `app.Metrics()` — counters/histograms.
- `internal/observability.OTelTracer` — global tracer extracted via `otel.Tracer(...)`.
- `forge.Logger` via `app.Logger()` — structured audit lines.

## Verification

1. **Unit tests** for each new component:
   - Idempotency store: hit, miss, TTL expiry, LRU eviction, concurrent access (`-race`).
   - Prometheus emitter: counter increment per call, histogram observation per call, label correctness.
   - Tracing in dispatcher: span opens, attributes set, status set on error, span closes on handler return.
   - Audit logger emitter: log line shape verified via captured logger output.
   - CSRF token handler: returns `{token, expiresAt}` with valid HMAC token.

2. **Integration tests**:
   - Send a `kind=command` envelope with missing CSRF → `UNAUTHENTICATED`.
   - Send same `kind=command` envelope twice with same idempotency key → byte-equal responses, dispatcher counter incremented once.
   - Issue token via `GET /csrf`, use it on a command → success.

3. **Observability spot-checks** (manual):
   - Hit several intents via the probe CLI, scrape `app.Metrics()` exporter, verify series + labels.
   - Run with Jaeger/OTLP collector, verify spans land with the expected attributes.

4. **No regressions**: `go test -count=1 ./extensions/dashboard/...` and `-race` clean. The 181 tests from slices (a) + (c) all stay green.

## Out of Scope — Future Slices

- **Persistent audit storage** — `AuditStore` interface + SQL/Redis backend. Explicitly punted; structured logging covers near-term needs.
- **Redis-backed idempotency** — for multi-instance deployments. Store interface accepts it; in-memory ships first.
- **Stateful CSRF** — `extensions/security/csrf.go` already exists for this if HMAC rotation becomes operationally hard.
- **React shell** (slice d), **built-in intent vocabulary** (slice e), **templ retirement** (slice f) — independent slices.
