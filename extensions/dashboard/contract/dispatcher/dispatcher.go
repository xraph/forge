package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

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
	tracer  trace.Tracer     // optional; nil = no tracing
	store   IdempotencyStore // optional; nil = no command dedup

	mu            sync.RWMutex
	handlers      map[handlerKey]Handler
	subscriptions map[handlerKey]SubscriptionHandler
}

type handlerKey struct {
	Contributor string
	Intent      string
	Version     int
}

// Option configures a Dispatcher.
type Option func(*Dispatcher)

// WithTracer configures the dispatcher to open a span per Dispatch call.
// Passing a nil tracer is equivalent to not supplying the option at all.
func WithTracer(t trace.Tracer) Option {
	return func(d *Dispatcher) { d.tracer = t }
}

// WithIdempotencyStore wires command dedup. When set, commands carrying a
// non-empty IdempotencyKey are deduped per-user via the store.
func WithIdempotencyStore(s IdempotencyStore) Option {
	return func(d *Dispatcher) { d.store = s }
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

// New returns a fresh dispatcher. Pass NoopMetricsEmitter{} for tests / dev;
// slice (b) provides a Prometheus-backed implementation.
func New(metrics MetricsEmitter) *Dispatcher {
	return NewWithOptions(metrics)
}

// NewWithOptions returns a dispatcher configured with the supplied options.
// The existing New(metrics) constructor is preserved as a thin wrapper.
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

// Dispatch implements transport.Dispatcher. When a tracer is configured, a
// span wraps the dispatch with attributes capturing (contributor, intent,
// version, kind) and a status reflecting the outcome.
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

// dispatchInner performs the handler lookup + invocation + metrics + error
// mapping, plus optional idempotency dedup for commands. Dispatch is a thin
// wrapper that adds optional span instrumentation.
func (d *Dispatcher) dispatchInner(ctx context.Context, req contract.Request, p contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	// Idempotency wrap (commands only, requires store + key).
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

	var (
		data json.RawMessage
		meta contract.ResponseMeta
	)
	if res == nil {
		// Allow nil result to mean {data: null} explicitly.
		meta = contract.ResponseMeta{IntentVersion: req.IntentVersion}
	} else {
		meta = contract.ResponseMeta{IntentVersion: req.IntentVersion}
		if len(res.ExtraInvalidates) > 0 {
			meta.Invalidates = append(meta.Invalidates, res.ExtraInvalidates...)
		}
		if res.CacheOverride != nil {
			meta.CacheControl = res.CacheOverride
		}
		data = res.Data
	}

	// Capture for next time on successful command dispatch.
	if req.Kind == contract.KindCommand && d.store != nil && req.IdempotencyKey != "" {
		identity := principalIdentity(p, req.Intent)
		successResp := contract.Response{OK: true, Envelope: req.Envelope, Kind: req.Kind, Data: data, Meta: meta}
		body, _ := json.Marshal(successResp)
		// TTL: 24h hardcoded. Phase 6 will surface this via Extension config.
		_ = d.store.Store(ctx, req.IdempotencyKey, identity, IdempotencyCached{
			Status:   200,
			WireBody: body,
			StoredAt: time.Now(),
			TTL:      24 * time.Hour,
		})
	}

	return data, meta, nil
}

// principalIdentity is the per-user dedup key suffix. Empty user is allowed
// (anonymous principals dedup against the empty subject). The intent is
// folded in so the same idempotency key for two different intents does not
// collide.
func principalIdentity(p contract.Principal, intent string) string {
	user := ""
	if p.User != nil {
		user = p.User.Subject
	}
	return user + ":" + intent
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
