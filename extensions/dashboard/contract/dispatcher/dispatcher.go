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
	// claimWait bounds how long a command waits for a concurrent dispatch
	// that holds its idempotency key. Used only with an IdempotencyClaimer.
	claimWait time.Duration

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
	h                 Handler
	secret            bool
	requiredKind      contract.Kind
	bypassIdempotency bool
	beforeDispatch    []func(context.Context, contract.Request, contract.Principal) error
	registrationErr   error
}

// RegisterOption configures one handler registration. Register, RegisterQuery and
// RegisterCommand take any number of them. Existing calls without options
// keep compiling.
type RegisterOption func(*handlerEntry)

// RequireKind restricts a local handler to query or command requests. Raw
// Register leaves kind unrestricted unless you supply this option. The last valid
// RequireKind wins, except that typed helpers always enforce their own kind.
// An invalid kind makes registration fail even if a later option overrides it.
func RequireKind(kind contract.Kind) RegisterOption {
	return func(e *handlerEntry) {
		if kind != contract.KindQuery && kind != contract.KindCommand {
			e.registrationErr = fmt.Errorf("dispatcher: invalid required kind %q: expected query or command", kind)

			return
		}

		e.requiredKind = kind
	}
}

// BypassIdempotency skips every generic cache operation for this handler,
// including lookup, claim, storage and release. Use it when your handler owns
// durable receipt semantics. It still runs admission and kind checks.
func BypassIdempotency() RegisterOption {
	return func(e *handlerEntry) { e.bypassIdempotency = true }
}

// BeforeDispatch attaches caller-specific admission to a local handler. These
// options do not authorize requests by themselves: you supply the policy.
// Callbacks run in registration order, outside dispatcher locks, with the
// original request and principal before any cache access. The first error
// stops dispatch and uses the same public error mapping as a handler error.
//
// Admission runs again after each successful Claim, including immediate claims,
// before replay or execution. Your callbacks must be safe to repeat and must
// not perform the domain mutation. A nil callback makes registration fail.
func BeforeDispatch(fn func(context.Context, contract.Request, contract.Principal) error) RegisterOption {
	return func(e *handlerEntry) {
		if fn == nil {
			e.registrationErr = errors.New("dispatcher: nil BeforeDispatch callback")

			return
		}

		e.beforeDispatch = append(e.beforeDispatch, fn)
	}
}

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

// ReasonDetail is the key, in a contract.Error's Details, under which the
// dispatcher's idempotency answers name why the command did not run. Its
// value is one of the Reason constants. Codes and messages are for people;
// a client that needs to tell these answers apart reads the reason.
const ReasonDetail = "reason"

// Reasons the dispatcher gives, under ReasonDetail, when a command with an
// idempotency key does not run. The strings are wire-stable.
const (
	// ReasonAlreadyRan is the CONFLICT a replay of a SecretResponse command
	// answers: the command ran, and its response is not kept.
	ReasonAlreadyRan = "idempotency.already_ran"
	// ReasonStillRunning is the retryable CONFLICT a command answers when
	// another dispatch with the same key and user still holds the key after
	// the wait.
	ReasonStillRunning = "idempotency.still_running"
	// ReasonClaimFailed is the retryable UNAVAILABLE a command answers when
	// its key could not be claimed for any other reason.
	ReasonClaimFailed = "idempotency.claim_failed"
)

// errSecretNotKept is what a replay of a SecretResponse command answers.
func errSecretNotKept() *contract.Error {
	return &contract.Error{
		Code:    contract.CodeConflict,
		Message: "command already ran and its response held a secret that is not kept; send a new idempotency key to run it again",
		Details: map[string]any{ReasonDetail: ReasonAlreadyRan},
	}
}

// errStillRunning is what a command answers when another dispatch with the
// same idempotency key and user still holds the key after the wait.
func errStillRunning() *contract.Error {
	return &contract.Error{
		Code:      contract.CodeConflict,
		Message:   "the same command is still running under this idempotency key; retry once it finishes",
		Details:   map[string]any{ReasonDetail: ReasonStillRunning},
		Retryable: true,
	}
}

// errClaimFailed is what a command answers when its idempotency key could not
// be claimed for a reason other than another dispatch holding it. The handler
// does not run, since nothing would stop a duplicate from running beside it.
func errClaimFailed() *contract.Error {
	return &contract.Error{
		Code:      contract.CodeUnavailable,
		Message:   "could not claim the idempotency key",
		Details:   map[string]any{ReasonDetail: ReasonClaimFailed},
		Retryable: true,
	}
}

// DefaultIdempotencyWait is how long a command waits, by default, for a
// concurrent dispatch that holds its idempotency key. It matches the HTTP
// idempotency middleware's default wait.
const DefaultIdempotencyWait = 10 * time.Second

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

// WithIdempotencyWait bounds how long a command waits for a concurrent
// dispatch that holds its idempotency key, when the store is an
// IdempotencyClaimer. When the wait ends with the key still held, the command
// answers CONFLICT. A value of zero or less keeps DefaultIdempotencyWait.
//
// Keep the wait below your HTTP server's WriteTimeout. A server that times the
// write out first cuts off a waiting duplicate's response, so the client sees
// a dropped connection where it should have seen the replay or CONFLICT.
func WithIdempotencyWait(wait time.Duration) Option {
	return func(d *Dispatcher) {
		if wait > 0 {
			d.claimWait = wait
		}
	}
}

// IdempotencyStore is the minimal surface the dispatcher needs from
// extensions/dashboard/contract/idempotency. Defining it here avoids an
// import cycle (the idempotency package is consumed only via this interface).
type IdempotencyStore interface {
	Lookup(ctx context.Context, key, identity string) (*IdempotencyCached, bool)
	Store(ctx context.Context, key, identity string, c IdempotencyCached) error
}

// IdempotencyClaimer is an IdempotencyStore that can also hold a key while a
// command's handler runs. The dispatcher finds it by type assertion on the
// store passed to WithIdempotencyStore. With one, a command with an
// idempotency key claims (key, identity) before its handler runs and holds
// the claim until its entry is stored or the handler fails, so two
// overlapping dispatches never both run it. Without one, the dispatcher only
// looks the key up before the handler and stores the entry after it.
//
// A Claim that fails for any reason other than the key being held (a backend
// error, say) answers a retryable UNAVAILABLE and the handler does not run,
// since nothing would stop a duplicate from running beside it.
//
// When End reports ErrIdempotencyClaimLost, the claim's lease lapsed while the
// handler ran. The dispatcher then writes the entry with Store, so a secret
// command still leaves its tombstone.
type IdempotencyClaimer interface {
	IdempotencyStore

	// Claim takes (key, identity) for the caller. While another caller holds
	// the key it waits for that claim to end, until ctx ends. Exactly one of
	// the returned claim's fields is set: Cached when an entry is stored for
	// the key, End when the caller now holds it. When ctx ends with the key
	// still held, the error wraps ErrIdempotencyClaimHeld.
	Claim(ctx context.Context, key, identity string) (IdempotencyClaim, error)
}

// IdempotencyClaim mirrors idempotency.Claim, for the same import-cycle
// reason as IdempotencyCached.
type IdempotencyClaim struct {
	// Cached is the entry already stored for the key.
	Cached *IdempotencyCached
	// End ends a claim the caller holds. Pass the entry to store under the
	// claim, or nil to store nothing and give the key back. The dispatcher
	// calls it exactly once. When the claim lapsed before End, End stores
	// nothing and its error wraps ErrIdempotencyClaimLost.
	End func(ctx context.Context, c *IdempotencyCached) error
}

// ErrIdempotencyClaimHeld is what IdempotencyClaimer.Claim's error wraps when
// its context ended while another dispatch still held the key.
var ErrIdempotencyClaimHeld = errors.New("dispatcher: idempotency key is held by a running command")

// ErrIdempotencyClaimLost is what IdempotencyClaim.End's error wraps when the
// claim's lease lapsed before End, so End stored nothing.
var ErrIdempotencyClaimLost = errors.New("dispatcher: idempotency claim lapsed before it ended")

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
		claimWait:     DefaultIdempotencyWait,
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
		if opt == nil {
			return errors.New("dispatcher: nil registration option")
		}

		opt(&entry)
	}

	if entry.registrationErr != nil {
		return entry.registrationErr
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

	if !ok {
		// No local handler: forward the original request to the remote dispatcher
		// if wired. Forwarding inherits the dispatcher's metrics;
		// latency is measured around the remote call too so the
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

	if err := d.admit(ctx, req, p, entry); err != nil {
		return nil, contract.ResponseMeta{}, err
	}

	useIdempotency := !entry.bypassIdempotency && req.Kind == contract.KindCommand && d.store != nil && req.IdempotencyKey != ""

	// end, when set, ends the claim this dispatch holds on its idempotency
	// key. remember stores the entry through it; until then the deferred call
	// below gives the key back, on a failed handler and on a panic alike, so
	// the next dispatch with the key runs the handler again.
	var end func(context.Context, *IdempotencyCached) error

	defer func() {
		if end != nil {
			_ = end(context.WithoutCancel(ctx), nil)
		}
	}()

	// Idempotency wrap (commands only, requires store + key).
	if useIdempotency {
		identity := principalIdentity(p, req.Intent)

		claimer, canClaim := d.store.(IdempotencyClaimer)
		if canClaim {
			claim, err := d.claim(ctx, claimer, req.IdempotencyKey, identity)
			if err != nil {
				return nil, contract.ResponseMeta{}, err
			}

			end = claim.End

			if err := d.admit(ctx, req, p, entry); err != nil {
				return nil, contract.ResponseMeta{}, err
			}

			if claim.Cached != nil {
				if data, meta, answered, err := answerCached(entry, claim.Cached); answered {
					return data, meta, err
				}
				// Cached but undecodable. Run afresh with no claim, as the
				// Lookup path below does, and overwrite the entry with Store.
			}
		} else if cached, hit := d.store.Lookup(ctx, req.IdempotencyKey, identity); hit {
			if data, meta, answered, err := answerCached(entry, cached); answered {
				return data, meta, err
			}
			// Cached but undecodable; fall through to fresh dispatch.
		}
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
	if useIdempotency {
		d.remember(ctx, req, p, entry.secret, data, meta, end)
		end = nil // remember ended the claim
	}

	return data, meta, nil
}

// admit checks local registration policy before cache access or execution.
func (d *Dispatcher) admit(ctx context.Context, req contract.Request, p contract.Principal, entry handlerEntry) error {
	started := time.Now()

	var err error
	if entry.requiredKind != "" && req.Kind != entry.requiredKind {
		err = &contract.Error{Code: contract.CodeBadRequest, Message: fmt.Sprintf("handler requires kind %q", entry.requiredKind)}
	} else {
		for _, before := range entry.beforeDispatch {
			if err = before(ctx, req, p); err != nil {
				break
			}
		}
	}

	wireErr := mapDispatchError(err)
	if wireErr != nil {
		var ce *contract.Error
		if errors.As(wireErr, &ce) {
			d.metrics.RecordDispatch(ctx, req.Contributor, req.Intent, req.IntentVersion, req.Kind, time.Since(started), ce.Code)
		}
	}

	return wireErr
}

// claim takes the command's idempotency key through claimer, waiting at most
// d.claimWait for a concurrent dispatch that holds it. A key still held when
// the wait or ctx ends answers CONFLICT, and any other failure UNAVAILABLE;
// neither runs the handler.
func (d *Dispatcher) claim(ctx context.Context, claimer IdempotencyClaimer, key, identity string) (IdempotencyClaim, error) {
	waitCtx, cancel := context.WithTimeout(ctx, d.claimWait)
	defer cancel()

	claim, err := claimer.Claim(waitCtx, key, identity)

	switch {
	case errors.Is(err, ErrIdempotencyClaimHeld):
		return IdempotencyClaim{}, errStillRunning()
	case err != nil:
		log.Printf("dispatcher: claiming idempotency key: %v", err)

		return IdempotencyClaim{}, errClaimFailed()
	case claim.Cached == nil && claim.End == nil:
		log.Printf("dispatcher: idempotency claimer returned neither an entry nor a claim")

		return IdempotencyClaim{}, errClaimFailed()
	}

	return claim, nil
}

// answerCached answers a command from the idempotency entry stored for its
// key. answered is false when the entry is a non-secret one that does not
// decode, and then the caller runs the handler afresh.
func answerCached(entry handlerEntry, cached *IdempotencyCached) (json.RawMessage, contract.ResponseMeta, bool, error) {
	// A tombstone, or any entry at all for a secret command, means the
	// command already ran and its answer is gone. Running it again would mint
	// a second secret, so refuse before the caller's fallthrough can.
	if entry.secret || cached.Status == TombstoneStatus {
		return nil, contract.ResponseMeta{}, true, errSecretNotKept()
	}

	// Decode the cached envelope back into (data, meta).
	var resp contract.Response
	if err := json.Unmarshal(cached.WireBody, &resp); err == nil && resp.OK {
		return resp.Data, resp.Meta, true, nil
	}

	return nil, contract.ResponseMeta{}, false, nil
}

// remember stores the idempotency entry for a command that succeeded. A
// secret command leaves a tombstone: TombstoneStatus and no body, so the
// secret its response carries never reaches the store. Any other command
// leaves its full success envelope for replay.
//
// With end set, the dispatch holds a claim on the key: the entry is stored
// through end, which ends the claim, and end runs even when there is no entry
// to store, so the key is given back. If the claim lapsed while the handler
// ran, end stores nothing, so the entry goes through Store as it does without
// a claim. A tombstone is always safe to write that way, and so is a response:
// Store never overwrites a live claim, and a duplicate that took the key
// writes its own entry when it finishes.
func (d *Dispatcher) remember(ctx context.Context, req contract.Request, p contract.Principal, secret bool, data json.RawMessage, meta contract.ResponseMeta, end func(context.Context, *IdempotencyCached) error) {
	// TTL: 24h hardcoded. Phase 6 will surface this via Extension config.
	cached := &IdempotencyCached{StoredAt: time.Now(), TTL: idempotencyTTL}

	if secret {
		cached.Status = TombstoneStatus
	} else {
		body, err := json.Marshal(contract.Response{OK: true, Envelope: req.Envelope, Kind: req.Kind, Data: data, Meta: meta})
		if err != nil {
			// Nothing worth replaying. The old code stored the empty body,
			// which a replay could not decode and ran afresh anyway.
			cached = nil
		} else {
			cached.Status = http.StatusOK
			cached.WireBody = body
		}
	}

	identity := principalIdentity(p, req.Intent)

	if end != nil {
		// The response is already on its way, so a client gone by now must
		// not cost the entry, nor leave the key held.
		ctx = context.WithoutCancel(ctx)

		err := end(ctx, cached)
		if err == nil {
			return
		}

		if cached == nil || !errors.Is(err, ErrIdempotencyClaimLost) {
			log.Printf("dispatcher: ending the idempotency claim for %s/%s@%d: %v", req.Contributor, req.Intent, req.IntentVersion, err)

			return
		}

		log.Printf("dispatcher: idempotency claim for %s/%s@%d lapsed while the handler ran; storing its entry without the claim", req.Contributor, req.Intent, req.IntentVersion)
	}

	if cached == nil {
		return
	}

	if err := d.store.Store(ctx, req.IdempotencyKey, identity, *cached); err != nil && end != nil {
		log.Printf("dispatcher: storing the idempotency entry for %s/%s@%d after its claim lapsed: %v", req.Contributor, req.Intent, req.IntentVersion, err)
	}
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
