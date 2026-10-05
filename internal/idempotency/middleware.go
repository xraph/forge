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
	"time"

	"github.com/xraph/forge/internal/router"
	forge_http "github.com/xraph/go-utils/http"
)

// HeaderName is the request header that carries the client's key.
const HeaderName = "Idempotency-Key"

// ReplayedHeader marks a response written from the store rather than by the
// handler.
const ReplayedHeader = "Idempotent-Replayed"

const maxKeyLength = 255

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
	maxBody     int64
	maxResponse int
	now         func() time.Time
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

// RequireKey answers 400 to a write that carries no Idempotency-Key.
func RequireKey() Option {
	return func(c *config) { c.requireKey = true }
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
// still sent, but not stored, so a retry runs the handler again. Default 1 MiB.
func MaxResponse(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxResponse = n
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
// Only a response the handler finished with a status below 500 is stored. A
// handler that returns an error, panics, answers 5xx, writes nothing or writes
// more than MaxResponse gives its key back, so the client's retry runs the
// handler again rather than replaying a failure for the whole TTL.
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
// else "" for an anonymous request. Every anonymous request shares the ""
// principal, so on routes open to anonymous callers the key itself is the
// only thing keeping one caller's response from another.
func DefaultPrincipal(ctx router.Context) string {
	if s, ok := ctx.Get("auth.subject").(string); ok && s != "" {
		return s
	}

	return subjectOf(ctx.Get("auth_context"))
}

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

	f := rv.FieldByName("Subject")
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}

	return f.String()
}

type handler struct{ cfg config }

var errBodyTooLarge = errors.New("idempotency: request body too large")

func (h *handler) wrap(next router.Handler) router.Handler {
	return func(ctx router.Context) error {
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
		key := Key{Principal: h.cfg.principal(ctx), Scope: r.Method + " " + r.URL.Path, Value: raw}

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
				return writeError(ctx, http.StatusConflict, "a request with this Idempotency-Key is still being processed")
			}

			if err := h.await(ctx.Context(), begun.Done, deadline.C); err != nil {
				if errors.Is(err, errWaitTimeout) {
					return writeError(ctx, http.StatusConflict, "a request with this Idempotency-Key is still being processed")
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

		release()

		if c, ok := inner.(forge_http.ContextWithClean); ok {
			c.Cleanup()
		}
	}()

	if err := next(inner); err != nil {
		return err
	}

	if !rec.wrote || rec.overflow || rec.status >= http.StatusInternalServerError {
		return nil
	}

	now := h.cfg.now()
	resp := Response{
		Status:      rec.status,
		Header:      addedHeaders(before, rec.Header()),
		Body:        bytes.Clone(rec.body.Bytes()),
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
