package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	handlers      map[handlerKey]handlerEntry
	subscriptions map[handlerKey]SubscriptionHandler
	// remote is consulted by Dispatch when no local handler exists. Slice (m)
	// added this so contributors hosted in other services can serve queries
	// and commands over HTTP without the host knowing they're remote at the
	// transport layer.
	remote RemoteDispatcher
}

// RemoteDispatcher is the fallback Dispatcher consults when no local handler
// is registered for an envelope. The dispatcher passes the verbatim request
// through; implementations typically POST it to a peer service. Slice (m)
// added this so dashboards can aggregate contributors from multiple
// upstreams without baking forwarding into the dispatcher itself.
type RemoteDispatcher interface {
	Dispatch(ctx context.Context, req contract.Request, p contract.Principal) (json.RawMessage, contract.ResponseMeta, error)
}

type handlerKey struct {
	Contributor string
	Intent      string
	Version     int
}

// handlerEntry is one registered query or command handler with the options
// it was registered under.
type handlerEntry struct {
	h      Handler
	secret bool
}

// RegisterOption configures one handler registration. Register and
// RegisterCommand take any number of them, so a call without options keeps
// compiling and behaving as before.
type RegisterOption func(*handlerEntry)

// SecretResponse marks a command whose response carries a secret the caller
// sees once, such as a freshly minted API key. The dispatcher never keeps
// that response for idempotent replay. A successful dispatch with an
// idempotency key stores a tombstone (TombstoneStatus, no body), and a later
// dispatch with the same key and user answers CONFLICT without running the
// handler again, because running it again would mint a second secret.
func SecretResponse() RegisterOption {
	return func(e *handlerEntry) { e.secret = true }
}

// TombstoneStatus is the Status of the idempotency entry a SecretResponse
// command leaves behind. The entry has no WireBody. Any entry with this
// status answers CONFLICT on lookup, whatever the handler's current
// registration says, so a tombstone never falls through to a fresh dispatch.
const TombstoneStatus = http.StatusConflict

// idempotencyTTL is how long a command's idempotency entry, tombstone or
// response, stays in the store.
const idempotencyTTL = 24 * time.Hour

// errSecretNotKept is what a replay of a SecretResponse command answers.
func errSecretNotKept() *contract.Error {
	return &contract.Error{
		Code:    contract.CodeConflict,
		Message: "command already ran and its response held a secret that is not kept; send a new idempotency key to run it again",
	}
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
		handlers:      map[handlerKey]handlerEntry{},
		subscriptions: map[handlerKey]SubscriptionHandler{},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// SetRemoteDispatcher installs (or clears, with nil) the fallback consulted
// when no local handler matches a request. Idempotent — the dispatcher's
// forwarding plumbing typically calls this once during wire-up.
func (d *Dispatcher) SetRemoteDispatcher(rd RemoteDispatcher) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.remote = rd
}

// Register binds a query/command handler to a (contributor, intent, version)
// key. Returns an error on duplicate registration. Pass SecretResponse for a
// command whose response must never be kept for replay.
func (d *Dispatcher) Register(contributor, intent string, version int, h Handler, opts ...RegisterOption) error {
	if h == nil {
		return fmt.Errorf("dispatcher: nil handler for %s/%s@%d", contributor, intent, version)
	}

	entry := handlerEntry{h: h}
	for _, opt := range opts {
		opt(&entry)
	}

	k := handlerKey{contributor, intent, version}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.handlers[k]; exists {
		return fmt.Errorf("dispatcher: handler %s/%s@%d already registered", contributor, intent, version)
	}

	d.handlers[k] = entry

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
	k := handlerKey{req.Contributor, req.Intent, req.IntentVersion}

	d.mu.RLock()
	entry, ok := d.handlers[k]
	remote := d.remote
	d.mu.RUnlock()

	// Idempotency wrap (commands only, requires store + key).
	if req.Kind == contract.KindCommand && d.store != nil && req.IdempotencyKey != "" {
		identity := principalIdentity(p, req.Intent)
		if cached, hit := d.store.Lookup(ctx, req.IdempotencyKey, identity); hit {
			// A tombstone, or any entry at all for a secret command, means
			// the command already ran and its answer is gone. Running it
			// again would mint a second secret, so refuse before the
			// fallthrough below can.
			if entry.secret || cached.Status == TombstoneStatus {
				return nil, contract.ResponseMeta{}, errSecretNotKept()
			}
			// Decode the cached envelope back into (data, meta).
			var resp contract.Response
			if err := json.Unmarshal(cached.WireBody, &resp); err == nil && resp.OK {
				return resp.Data, resp.Meta, nil
			}
			// Cached but undecodable; fall through to fresh dispatch.
		}
	}

	if !ok {
		// Slice (m): no local handler — fall through to the remote dispatcher
		// if wired. Forwarding inherits the dispatcher's metrics + dedup
		// pipeline; latency is measured around the remote call too so the
		// host sees the round-trip cost.
		if remote != nil {
			t0 := time.Now()
			data, meta, rErr := remote.Dispatch(ctx, req, p)
			latency := time.Since(t0)
			errCode := contract.ErrorCode("")
			if rErr != nil {
				var ce *contract.Error
				if errors.As(rErr, &ce) {
					errCode = ce.Code
				}
				d.metrics.RecordDispatch(ctx, req.Contributor, req.Intent, req.IntentVersion, req.Kind, latency, errCode)
				return nil, contract.ResponseMeta{}, rErr
			}
			d.metrics.RecordDispatch(ctx, req.Contributor, req.Intent, req.IntentVersion, req.Kind, latency, errCode)
			return data, meta, nil
		}
		err := &contract.Error{Code: contract.CodeNotFound, Message: fmt.Sprintf("handler %s/%s@%d not registered", req.Contributor, req.Intent, req.IntentVersion)}
		d.metrics.RecordDispatch(ctx, req.Contributor, req.Intent, req.IntentVersion, req.Kind, 0, err.Code)
		return nil, contract.ResponseMeta{}, err
	}

	t0 := time.Now()
	res, handlerErr := entry.h(ctx, req.Payload, req.Params, p)
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
		d.remember(ctx, req, p, entry.secret, data, meta)
	}

	return data, meta, nil
}

// remember stores the idempotency entry for a command that succeeded. A
// secret command leaves a tombstone: TombstoneStatus and no body, so the
// secret its response carries never reaches the store. Any other command
// leaves its full success envelope for replay.
func (d *Dispatcher) remember(ctx context.Context, req contract.Request, p contract.Principal, secret bool, data json.RawMessage, meta contract.ResponseMeta) {
	// TTL: 24h hardcoded. Phase 6 will surface this via Extension config.
	cached := IdempotencyCached{StoredAt: time.Now(), TTL: idempotencyTTL}

	if secret {
		cached.Status = TombstoneStatus
	} else {
		body, err := json.Marshal(contract.Response{OK: true, Envelope: req.Envelope, Kind: req.Kind, Data: data, Meta: meta})
		if err != nil {
			// Nothing worth replaying. The old code stored the empty body,
			// which a replay could not decode and ran afresh anyway.
			return
		}

		cached.Status = http.StatusOK
		cached.WireBody = body
	}

	_ = d.store.Store(ctx, req.IdempotencyKey, principalIdentity(p, req.Intent), cached)
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
