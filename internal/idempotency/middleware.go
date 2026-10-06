package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/router"
	"github.com/xraph/forge/internal/shared"
	forge_http "github.com/xraph/go-utils/http"
	"github.com/xraph/vessel"
)

// HeaderName is the request header that carries the client's key.
const HeaderName = "Idempotency-Key"

// ReplayedHeader marks a response written from the store rather than by the
// handler.
const ReplayedHeader = "Idempotent-Replayed"

// SkippedHeader is set on the response to a request that carried an
// Idempotency-Key the middleware did not act on. Its value says why;
// "anonymous" means the request had no principal and AllowAnonymous is off.
const SkippedHeader = "Idempotency-Skipped"

// TruncatedHeader is set to "true" on a replay whose stored response was
// larger than MaxResponse: the status and headers are the handler's, and the
// body is empty.
const TruncatedHeader = "Idempotent-Truncated"

const maxKeyLength = 255

// activeKey is the context value the middleware sets on the context it hands
// its handler. A second layer of this middleware (a group that opted in and a
// route inside it that did too) reads it and steps aside: the outer layer
// already holds the claim, so the inner one would wait on it, time out and
// answer 409 without running the handler.
const activeKey = "idempotency.active"

// pollInterval is how often a waiter asks the store again when the store
// cannot signal completion (Begun.Done is nil).
const pollInterval = 25 * time.Millisecond

// ConflictMode decides what a request does when another request with the
// same key is still running.
type ConflictMode int

const (
	// ConflictWait waits for the first request and then replays its response.
	ConflictWait ConflictMode = iota
	// ConflictReject answers 409 at once.
	ConflictReject
)

// PrincipalFunc names who made a request. Keys are scoped by it.
type PrincipalFunc func(ctx router.Context) string

type config struct {
	store       Store
	ttl         time.Duration
	lease       time.Duration
	wait        time.Duration
	conflict    ConflictMode
	principal   PrincipalFunc
	requireKey  bool
	anonymous   bool
	maxBody     int64
	maxResponse int
	now         func() time.Time
	logger      logger.Logger
}

// Option configures Middleware.
type Option func(*config)

// WithStore selects the store, overriding Middleware's store argument.
func WithStore(s Store) Option {
	return func(c *config) {
		if s != nil {
			c.store = s
		}
	}
}

// TTL is how long a stored response is replayed. Default 24h.
func TTL(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// Lease is how long a claim survives a request that never finishes, such as
// one whose process died. Default DefaultLease (1m).
func Lease(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.lease = d
		}
	}
}

// WaitTimeout bounds how long a concurrent duplicate waits before it gets
// 409. Default 10s.
func WaitTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.wait = d
		}
	}
}

// OnConflict selects waiting (the default) or an immediate 409 for a
// concurrent duplicate.
func OnConflict(m ConflictMode) Option {
	return func(c *config) { c.conflict = m }
}

// Principal replaces DefaultPrincipal.
func Principal(fn PrincipalFunc) Option {
	return func(c *config) {
		if fn != nil {
			c.principal = fn
		}
	}
}

// RequireKey answers 400 to a write that carries no Idempotency-Key. A keyed
// write with no principal still passes through undeduplicated, as it does
// without RequireKey, and its response carries Idempotency-Skipped:
// anonymous.
func RequireKey() Option {
	return func(c *config) { c.requireKey = true }
}

// AllowAnonymous deduplicates requests that have no principal too. By default
// a request whose PrincipalFunc returns "" runs as if it carried no key: every
// anonymous caller would share the "" principal, so one could be handed
// another's stored response by sending the same key. Skipping also fails safe
// when auth middleware is registered after this one and has not yet run.
//
// A skipped request is not refused, because public routes see third-party
// clients that send Idempotency-Key routinely. Its response carries
// Idempotency-Skipped: anonymous, and the first one on each route is logged
// as a warning, so a misordered auth middleware shows up.
func AllowAnonymous() Option {
	return func(c *config) { c.anonymous = true }
}

// MaxBody caps the request body read for fingerprinting. Larger bodies get
// 413. Default 1 MiB.
func MaxBody(n int64) Option {
	return func(c *config) {
		if n > 0 {
			c.maxBody = n
		}
	}
}

// MaxResponse caps the response body stored for replay. A larger response is
// still sent in full, and its status and headers are stored with an empty
// body and Idempotent-Truncated: true, so a repeat gets those instead of
// running the handler again. Default 1 MiB.
func MaxResponse(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxResponse = n
		}
	}
}

// Logger is where the middleware warns about a misconfiguration. Without it
// the middleware uses the application logger from the request's container
// (forge registers it as "forge.logger"), and logs nothing if there is none.
func Logger(l logger.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// Clock replaces time.Now for StoredAt and ExpiresAt, for tests.
func Clock(now func() time.Time) Option {
	return func(c *config) {
		if now != nil {
			c.now = now
		}
	}
}

// Middleware returns middleware that runs a handler at most once per
// (principal, METHOD /path, Idempotency-Key) and replays the stored response
// to every repeat. A nil store means Default().
//
// A response is stored unless it is one a client is expected to retry: 408,
// 429 and every 5xx give the key back, so the retry runs the handler again
// rather than replaying a transient failure for the whole TTL. Every other
// status, 409 and 422 included, is deterministic and is stored. A handler that
// returns nil without writing is stored as the empty 200 net/http sends for
// it. A handler that returns an error or panics also gives the key back, even
// when the error maps to a 4xx: the error handler writes that response outside
// this middleware, so only a 4xx the handler writes itself (ctx.JSON and the
// like) is stored. A response larger than MaxResponse is stored without its
// body and replayed with Idempotent-Truncated: true.
//
// A request whose principal is "" runs as if it carried no key unless
// AllowAnonymous is set; its response carries Idempotency-Skipped: anonymous
// and the first such request on each route logs a warning (see Logger). While
// a request holds a key, a duplicate that cannot
// wait gets 409 with Retry-After: 1, so a client can tell the conflict is
// temporary.
//
// A second layer inside the first (a group and a route that both opted in)
// steps aside, because the outer layer already holds the claim.
//
// The handler runs on a fresh context that shares the outer context's values
// and session. Anything else a forge_http.Ctx keeps privately, such as a DI
// scope opened by outer middleware, is not carried across.
func Middleware(store Store, opts ...Option) router.Middleware {
	cfg := config{
		store:       store,
		ttl:         24 * time.Hour,
		lease:       DefaultLease,
		wait:        10 * time.Second,
		principal:   DefaultPrincipal,
		maxBody:     1 << 20,
		maxResponse: 1 << 20,
		now:         time.Now,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.store == nil {
		cfg.store = Default()
	}

	h := &handler{cfg: cfg}

	return h.wrap
}

// DefaultPrincipal reads the authenticated subject: the "auth.subject"
// context value when it is a string, else the Subject field of whatever
// "auth_context" holds (the auth extension stores *auth.AuthContext there),
// prefixed with its ProviderName as "provider:subject" when that is set, else
// "" for an anonymous request. The prefix keeps one subject id issued by two
// providers from sharing keys. Every anonymous request shares the ""
// principal, so on routes open to anonymous callers (with AllowAnonymous) the
// key itself is the only thing keeping one caller's response from another.
func DefaultPrincipal(ctx router.Context) string {
	if s, ok := ctx.Get("auth.subject").(string); ok && s != "" {
		return s
	}

	return subjectOf(ctx.Get("auth_context"))
}

// subjectOf reads Subject, and ProviderName when there is one, from the
// struct v points to. A subject from a named provider is "provider:subject",
// so one subject id issued by two providers is two principals.
func subjectOf(v any) string {
	if v == nil {
		return ""
	}

	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return ""
		}

		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return ""
	}

	subject := stringField(rv, "Subject")
	if subject == "" {
		return ""
	}

	if provider := stringField(rv, "ProviderName"); provider != "" {
		return provider + ":" + subject
	}

	return subject
}

func stringField(rv reflect.Value, name string) string {
	f := rv.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}

	return f.String()
}

// maxWarnedRoutes bounds the routes a handler remembers having warned about.
// A route is named by its pattern, so the set stays small; the bound only
// matters for a router that reports no path parameters.
const maxWarnedRoutes = 1024

type handler struct {
	cfg config

	warnMu sync.Mutex
	warned map[string]struct{}
}

// warnSkipped logs, once per route, that a keyed request ran without
// deduplication because it had no principal.
func (h *handler) warnSkipped(ctx router.Context) {
	route := ctx.Request().Method + " " + routeOf(ctx)

	h.warnMu.Lock()
	_, seen := h.warned[route]

	if !seen && len(h.warned) < maxWarnedRoutes {
		if h.warned == nil {
			h.warned = map[string]struct{}{}
		}

		h.warned[route] = struct{}{}
	} else {
		seen = true
	}
	h.warnMu.Unlock()

	if seen {
		return
	}

	l := h.cfg.logger
	if l == nil {
		l = containerLogger(ctx)
	}

	if l == nil {
		return
	}

	l.Warn("idempotency: a request carried an Idempotency-Key but had no principal, so it was not deduplicated "+
		"(register auth before the idempotency middleware, or pass AllowAnonymous on a public route)",
		logger.String("route", route))
}

// routeOf names the matched route: the path with every segment that holds a
// path parameter's value replaced by {name}, so /orders/7 and /orders/8 are
// one route.
func routeOf(ctx router.Context) string {
	path := ctx.Request().URL.Path

	params := ctx.Params()
	if len(params) == 0 {
		return path
	}

	byValue := make(map[string]string, len(params))
	for name, value := range params {
		if value != "" {
			byValue[value] = name
		}
	}

	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if name, ok := byValue[seg]; ok {
			segments[i] = "{" + name + "}"
		}
	}

	return strings.Join(segments, "/")
}

// containerLogger is the application logger registered in the request's
// container, or nil. Forge registers it by type, which is where this looks
// first, then under the name "forge.logger".
func containerLogger(ctx router.Context) logger.Logger {
	c := ctx.Container()
	if c == nil {
		return nil
	}

	if l, err := vessel.Inject[logger.Logger](c); err == nil && l != nil {
		return l
	}

	v, err := c.Resolve(shared.LoggerKey)
	if err != nil {
		return nil
	}

	l, _ := v.(logger.Logger)

	return l
}

var errBodyTooLarge = errors.New("idempotency: request body too large")

func (h *handler) wrap(next router.Handler) router.Handler {
	return func(ctx router.Context) error {
		if active, _ := ctx.Get(activeKey).(bool); active {
			return next(ctx)
		}

		r := ctx.Request()
		if !mutating(r.Method) {
			return next(ctx)
		}

		raw := strings.TrimSpace(r.Header.Get(HeaderName))
		if raw == "" {
			if h.cfg.requireKey {
				return writeError(ctx, http.StatusBadRequest, "the Idempotency-Key header is required on this route")
			}

			return next(ctx)
		}

		principal := h.cfg.principal(ctx)
		if principal == "" && !h.cfg.anonymous {
			// Not deduplicated, and said so: on the response, for a client
			// that relies on the key, and once per route in the log, because
			// the usual cause is auth registered after this middleware.
			ctx.Response().Header().Set(SkippedHeader, "anonymous")
			h.warnSkipped(ctx)

			return next(ctx)
		}

		if len(raw) > maxKeyLength {
			return writeError(ctx, http.StatusBadRequest, "the Idempotency-Key header is longer than 255 characters")
		}

		body, err := readBody(r, h.cfg.maxBody)
		if errors.Is(err, errBodyTooLarge) {
			return writeError(ctx, http.StatusRequestEntityTooLarge, "the request body is too large to be made idempotent")
		}

		if err != nil {
			return err
		}

		fp := fingerprint(r.Method, r.URL.Path, r.URL.RawQuery, body)
		key := Key{Principal: principal, Scope: r.Method + " " + r.URL.Path, Value: raw}

		return h.serve(ctx, next, key, fp)
	}
}

func (h *handler) serve(ctx router.Context, next router.Handler, key Key, fp string) error {
	deadline := time.NewTimer(h.cfg.wait)
	defer deadline.Stop()

	for {
		begun, err := h.cfg.store.Begin(ctx.Context(), key, fp, h.cfg.lease)
		if err != nil {
			return err
		}

		switch begun.State {
		case Acquired:
			return h.run(ctx, next, key, begun.Token, fp)
		case Replay:
			if begun.Response == nil {
				return errors.New("idempotency: store returned Replay without a response")
			}

			if begun.Fingerprint != fp {
				return writeError(ctx, http.StatusUnprocessableEntity, "this Idempotency-Key was already used with a different request")
			}

			return replay(ctx, begun.Response)
		case InFlight:
			if begun.Fingerprint != fp {
				return writeError(ctx, http.StatusUnprocessableEntity, "this Idempotency-Key was already used with a different request")
			}

			if h.cfg.conflict == ConflictReject {
				return writeBusy(ctx)
			}

			if err := h.await(ctx.Context(), begun.Done, deadline.C); err != nil {
				if errors.Is(err, errWaitTimeout) {
					return writeBusy(ctx)
				}

				return err
			}
		default:
			return fmt.Errorf("idempotency: store returned unknown state %d", begun.State)
		}
	}
}

var errWaitTimeout = errors.New("idempotency: wait timed out")

// await blocks until it is worth calling Begin again: the holder finished
// (done closed), the poll interval passed for a store that cannot signal, or
// one lease passed. The lease bound matters because a store need not close
// done the instant a holder's lease lapses, only at its next sweep, which the
// next Begin triggers. It returns errWaitTimeout once deadline fires and the
// request's own error when its context ends.
func (h *handler) await(ctx context.Context, done <-chan struct{}, deadline <-chan time.Time) error {
	interval := h.cfg.lease
	if done == nil {
		interval = min(interval, pollInterval)
	}

	retry := time.NewTimer(interval)
	defer retry.Stop()

	select {
	case <-done:
	case <-retry.C:
	case <-deadline:
		return errWaitTimeout
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (h *handler) run(ctx router.Context, next router.Handler, key Key, token Token, fp string) error {
	background := context.WithoutCancel(ctx.Context())
	completed := false

	// Runs on return and during a panic, so a failed attempt never leaves the
	// key claimed until its lease lapses. ErrNotHolder here means the lease
	// already lapsed and the key moved on; there is nothing left to give back.
	defer func() {
		if !completed {
			_ = h.cfg.store.Release(background, key, token)
		}
	}()

	before := ctx.Response().Header().Clone()
	rec := &recorder{ResponseWriter: ctx.Response(), limit: h.cfg.maxResponse}

	inner := forge_http.NewContext(rec, ctx.Request(), ctx.Container())
	release := shareValues(ctx, inner)

	// The values map is shared with the outer context, so the marker has to be
	// cleared before release() hands the map back: it describes this handler
	// call, not the request.
	inner.Set(activeKey, true)

	// NewContext starts with no session. Hand the outer one in, and hand back
	// whatever the handler left (a rotated or destroyed session) so outer
	// middleware that saves the session sees the handler's change.
	outerSession, _ := ctx.Session()
	if outerSession != nil {
		inner.SetSession(outerSession)
	}

	defer func() {
		if innerSession, _ := inner.Session(); outerSession != nil || innerSession != nil {
			ctx.SetSession(innerSession)
		}

		inner.Set(activeKey, false)
		release()

		if c, ok := inner.(forge_http.ContextWithClean); ok {
			c.Cleanup()
		}
	}()

	if err := next(inner); err != nil {
		return err
	}

	if !rec.wrote {
		// net/http answers a handler that wrote nothing with an empty 200,
		// and the handler's side effect has happened, so that is the outcome.
		rec.status = http.StatusOK
	}

	if retryable(rec.status) {
		return nil
	}

	header := addedHeaders(before, rec.Header())
	body := bytes.Clone(rec.body.Bytes())

	if rec.overflow {
		// Too large to keep, but the handler's side effect has happened, so
		// the key stays taken: a repeat gets the status and headers, an empty
		// body and a marker saying the body was dropped, never a second run.
		header.Del("Content-Length")
		header.Set(TruncatedHeader, "true")

		body = nil
	}

	now := h.cfg.now()
	resp := Response{
		Status:      rec.status,
		Header:      header,
		Body:        body,
		Fingerprint: fp,
		StoredAt:    now,
		ExpiresAt:   now.Add(h.cfg.ttl),
	}

	// The response already reached the client whatever happens here. On
	// ErrNotHolder the lease lapsed while the handler ran and another request
	// may hold the key now; the token keeps this late Complete from
	// overwriting that holder's outcome, and the deferred Release from
	// dropping its claim. On any other error the deferred Release gives the
	// key back, so a retry runs again rather than hanging on a claim.
	if err := h.cfg.store.Complete(background, key, token, resp); err == nil {
		completed = true
	}

	return nil
}

func replay(ctx router.Context, resp *Response) error {
	header := ctx.Response().Header()
	for k, vs := range resp.Header {
		header[k] = slices.Clone(vs)
	}

	header.Set(ReplayedHeader, "true")
	ctx.Response().WriteHeader(resp.Status)

	_, err := ctx.Response().Write(resp.Body)

	return err
}

// retryable reports a status a client retries with the same key, which must
// therefore run the handler again rather than replay.
func retryable(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

// writeBusy answers a duplicate of a request that is still running. The
// Retry-After header marks the 409 as temporary, unlike a 409 the handler
// itself returns.
func writeBusy(ctx router.Context) error {
	ctx.Response().Header().Set("Retry-After", "1")

	return writeError(ctx, http.StatusConflict, "a request with this Idempotency-Key is still being processed")
}

func writeError(ctx router.Context, status int, message string) error {
	return ctx.JSON(status, map[string]string{"error": message})
}

func mutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}

	return false
}

func readBody(r *http.Request, limit int64) ([]byte, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}

	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	_ = r.Body.Close()

	if err != nil {
		return nil, err
	}

	if int64(len(data)) > limit {
		return nil, errBodyTooLarge
	}

	r.Body = io.NopCloser(bytes.NewReader(data))

	return data, nil
}

// fingerprint hashes what makes two requests the same operation. The query
// string is included because it can change what a write does (dry_run=true),
// while the key's scope stays the path alone, so reusing a key with another
// query is refused as a mismatch rather than run as a new operation.
func fingerprint(method, path, rawQuery string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{'\n'})
	h.Write([]byte(path))
	h.Write([]byte{'\n'})
	h.Write([]byte(rawQuery))
	h.Write([]byte{'\n'})
	h.Write(body)

	return hex.EncodeToString(h.Sum(nil))
}

// addedHeaders keeps only what the handler set or changed. Headers outer
// middleware set before the handler, such as a request id, belong to the
// request that is running, not to the one being replayed. Set-Cookie is never
// replayed (a cookie minted for one request must not be handed out again) and
// Date always describes the response being sent now.
func addedHeaders(before, after http.Header) http.Header {
	out := http.Header{}

	for k, vs := range after {
		switch http.CanonicalHeaderKey(k) {
		case "Set-Cookie", "Date", ReplayedHeader:
			continue
		}

		if !slices.Equal(before[k], vs) {
			out[k] = slices.Clone(vs)
		}
	}

	return out
}

func shareValues(src, dst router.Context) func() {
	valuer, ok := src.(interface{ Values() map[string]any })
	if !ok {
		return func() {}
	}

	sharer, ok := dst.(interface{ ShareValues(values map[string]any) })
	if !ok {
		return func() {}
	}

	sharer.ShareValues(valuer.Values())

	if r, ok := dst.(interface{ ReleaseSharedValues() }); ok {
		return r.ReleaseSharedValues
	}

	return func() { sharer.ShareValues(make(map[string]any)) }
}

// recorder writes through to the client and keeps a copy of the body up to
// limit bytes.
type recorder struct {
	http.ResponseWriter

	status   int
	wrote    bool
	body     bytes.Buffer
	limit    int
	overflow bool
}

func (w *recorder) WriteHeader(code int) {
	if w.wrote {
		return
	}

	// An informational status such as 103 Early Hints is not the response;
	// pass it on and keep waiting for the final status. 101 Switching
	// Protocols is final.
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code)

		return
	}

	w.wrote = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *recorder) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}

	if !w.overflow {
		if w.body.Len()+len(p) > w.limit {
			w.overflow = true
			w.body.Reset()
		} else {
			w.body.Write(p)
		}
	}

	return w.ResponseWriter.Write(p)
}

func (w *recorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *recorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }
