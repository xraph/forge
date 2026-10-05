package middleware

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	forge "github.com/xraph/forge"
	forge_http "github.com/xraph/go-utils/http"
)

type idemAuth struct{ Subject string }

// idemCall describes one request. It runs as the "tester" principal unless
// anonymous is set; a setup that stores its own auth_context replaces it.
type idemCall struct {
	method, path, key, body string
	setup                   func(forge.Context)
	anonymous               bool
	wrap                    func(http.ResponseWriter) http.ResponseWriter
}

func runIdem(t *testing.T, mw forge.Middleware, h forge.Handler, c idemCall) (*httptest.ResponseRecorder, error) {
	t.Helper()

	return runIdemCtx(t, context.Background(), mw, h, c)
}

func runIdemCtx(t *testing.T, reqCtx context.Context, mw forge.Middleware, h forge.Handler, c idemCall) (*httptest.ResponseRecorder, error) {
	t.Helper()

	method := c.method
	if method == "" {
		method = http.MethodPost
	}

	path := c.path
	if path == "" {
		path = "/orders"
	}

	req := httptest.NewRequestWithContext(reqCtx, method, path, strings.NewReader(c.body))
	if c.key != "" {
		req.Header.Set(IdempotencyKeyHeader, c.key)
	}

	rec := httptest.NewRecorder()

	var w http.ResponseWriter = rec
	if c.wrap != nil {
		w = c.wrap(rec)
	}

	ctx := forge_http.NewContext(w, req, nil)

	if !c.anonymous {
		ctx.Set("auth_context", &idemAuth{Subject: "tester"})
	}

	if c.setup != nil {
		c.setup(ctx)
	}

	return rec, mw(h)(ctx)
}

func countingHandler(calls *atomic.Int32) forge.Handler {
	return func(ctx forge.Context) error {
		n := calls.Add(1)

		ctx.Response().Header().Set("X-Order", "7")

		return ctx.JSON(http.StatusCreated, map[string]any{"id": "7", "n": n})
	}
}

func TestIdempotencyReplaysTheStoredResponse(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())

	first, err := runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: `{"total":1}`})
	if err != nil {
		t.Fatal(err)
	}

	second, err := runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: `{"total":1}`})
	if err != nil {
		t.Fatal(err)
	}

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", calls.Load())
	}

	if second.Code != http.StatusCreated || second.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %q, want %d %q", second.Code, second.Body.String(), first.Code, first.Body.String())
	}

	if second.Header().Get("X-Order") != "7" || second.Header().Get(IdempotentReplayedHeader) != "true" {
		t.Fatalf("replay headers = %v", second.Header())
	}

	if first.Header().Get(IdempotentReplayedHeader) != "" {
		t.Fatal("the original response was marked as replayed")
	}
}

func TestIdempotencyRejectsAReusedKeyWithADifferentBody(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())

	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: `{"total":1}`})

	rec, err := runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: `{"total":2}`})
	if err != nil {
		t.Fatal(err)
	}

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", calls.Load())
	}
}

func TestIdempotencyScopesKeysByPrincipal(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())

	as := func(subject string) func(forge.Context) {
		return func(ctx forge.Context) { ctx.Set("auth_context", &idemAuth{Subject: subject}) }
	}

	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}", setup: as("alice")})
	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}", setup: as("bob")})

	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2 (bob must not get alice's response)", calls.Load())
	}
}

func TestIdempotencyReadsAnAuthSubjectString(t *testing.T) {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/orders", nil)
	ctx := forge_http.NewContext(httptest.NewRecorder(), req, nil)
	ctx.Set("auth.subject", "carol")

	if got := DefaultIdempotencyPrincipal(ctx); got != "carol" {
		t.Fatalf("principal = %q, want carol", got)
	}
}

func TestIdempotencyScopesKeysByConcretePath(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())

	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{path: "/orders/7", key: "k1", body: "{}"})
	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{path: "/orders/8", key: "k1", body: "{}"})

	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2", calls.Load())
	}
}

func TestIdempotencyIgnoresSafeMethodsAndMissingKeys(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())

	for range 2 {
		_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{method: http.MethodGet, key: "k1"})
		_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{body: "{}"})
	}

	if calls.Load() != 4 {
		t.Fatalf("handler ran %d times, want 4", calls.Load())
	}
}

func TestIdempotencyCanRequireTheKey(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore(), IdempotencyRequireKey())

	rec, err := runIdem(t, mw, countingHandler(&calls), idemCall{body: "{}"})
	if err != nil {
		t.Fatal(err)
	}

	if rec.Code != http.StatusBadRequest || calls.Load() != 0 {
		t.Fatalf("status = %d, calls = %d; want 400 and no call", rec.Code, calls.Load())
	}
}

func TestIdempotencyRefusesAnOverlongKey(t *testing.T) {
	var calls atomic.Int32

	rec, _ := runIdem(t, Idempotency(NewMemoryIdempotencyStore()), countingHandler(&calls), idemCall{key: strings.Repeat("k", 256), body: "{}"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestIdempotencyRefusesAnOversizedBody(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore(), IdempotencyMaxBody(8))

	rec, _ := runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: `{"total":12345}`})
	if rec.Code != http.StatusRequestEntityTooLarge || calls.Load() != 0 {
		t.Fatalf("status = %d, calls = %d; want 413 and no call", rec.Code, calls.Load())
	}
}

// signallingStore reports each InFlight result so a test can wait until a
// second request is genuinely waiting on the first.
type signallingStore struct {
	IdempotencyStore

	inFlight chan struct{}
}

func (s signallingStore) Begin(ctx context.Context, k IdempotencyKey, fp string, lease time.Duration) (IdempotencyBegun, error) {
	b, err := s.IdempotencyStore.Begin(ctx, k, fp, lease)
	if err == nil && b.State == IdempotencyInFlight {
		select {
		case s.inFlight <- struct{}{}:
		default:
		}
	}

	return b, err
}

func blockingHandler(calls *atomic.Int32, entered chan<- struct{}, release <-chan struct{}) forge.Handler {
	return func(ctx forge.Context) error {
		calls.Add(1)

		entered <- struct{}{}

		<-release

		return ctx.JSON(http.StatusCreated, map[string]string{"id": "7"})
	}
}

func TestIdempotencyMakesAConcurrentDuplicateWait(t *testing.T) {
	var calls atomic.Int32

	store := signallingStore{NewMemoryIdempotencyStore(), make(chan struct{}, 1)}
	mw := Idempotency(store)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	h := blockingHandler(&calls, entered, release)

	var wg sync.WaitGroup

	results := make([]*httptest.ResponseRecorder, 2)

	wg.Go(func() {
		results[0], _ = runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
	})
	<-entered

	wg.Go(func() {
		results[1], _ = runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
	})
	<-store.inFlight

	close(release)
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", calls.Load())
	}

	if results[1].Code != http.StatusCreated || results[1].Body.String() != results[0].Body.String() {
		t.Fatalf("waiting request got %d %q", results[1].Code, results[1].Body.String())
	}
}

func TestIdempotencyCanRejectAConcurrentDuplicate(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore(), IdempotencyOnConflict(IdempotencyReject))
	entered, release := make(chan struct{}, 1), make(chan struct{})
	h := blockingHandler(&calls, entered, release)

	done := make(chan struct{})
	go func() {
		defer close(done)

		_, _ = runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
	}()

	<-entered

	rec, _ := runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})

	close(release)
	<-done

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestIdempotencyWaitGivesUpWith409(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore(), IdempotencyWaitTimeout(30*time.Millisecond))
	entered, release := make(chan struct{}, 1), make(chan struct{})
	h := blockingHandler(&calls, entered, release)

	done := make(chan struct{})
	go func() {
		defer close(done)

		_, _ = runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
	}()

	<-entered

	rec, _ := runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})

	close(release)
	<-done

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 after the wait timed out", rec.Code)
	}
}

func TestIdempotencyReleasesTheKeyWhenTheHandlerFails(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())
	failing := func(ctx forge.Context) error {
		calls.Add(1)

		return errors.New("database unavailable")
	}

	if _, err := runIdem(t, mw, failing, idemCall{key: "k1", body: "{}"}); err == nil {
		t.Fatal("the handler's error was swallowed")
	}

	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}"})

	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2 (a failed attempt must not be replayed)", calls.Load())
	}
}

func TestIdempotencyReleasesTheKeyWhenTheHandlerPanics(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())
	panicking := func(ctx forge.Context) error {
		calls.Add(1)
		panic("boom")
	}

	func() {
		defer func() { _ = recover() }()

		_, _ = runIdem(t, mw, panicking, idemCall{key: "k1", body: "{}"})
	}()

	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}"})

	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2", calls.Load())
	}
}

func TestIdempotencyForgetsAnExpiredResponse(t *testing.T) {
	var calls atomic.Int32

	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	mw := Idempotency(NewMemoryIdempotencyStore(MemoryIdempotencyClock(clock)), IdempotencyClock(clock), IdempotencyTTL(time.Hour))

	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}"})
	now = now.Add(2 * time.Hour)
	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}"})

	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2", calls.Load())
	}
}

func TestIdempotencyDoesNotReplayHeadersSetBeforeTheHandler(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())
	withRequestID := func(id string) func(forge.Context) {
		return func(ctx forge.Context) { ctx.Response().Header().Set("X-Request-Id", id) }
	}

	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}", setup: withRequestID("req-1")})
	second, _ := runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}", setup: withRequestID("req-2")})

	if got := second.Header().Get("X-Request-Id"); got != "req-2" {
		t.Fatalf("X-Request-Id = %q, want req-2 (the first request's id leaked into the replay)", got)
	}
}

func TestIdempotencyDoesNotStoreAnOversizedResponse(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore(), IdempotencyMaxResponse(4))

	first, _ := runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}"})
	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}"})

	if first.Code != http.StatusCreated || first.Body.Len() == 0 {
		t.Fatalf("the oversized response was not sent: %d %q", first.Code, first.Body.String())
	}

	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2 (nothing was stored to replay)", calls.Load())
	}
}

func TestIdempotencyKeepsContextValuesForTheHandler(t *testing.T) {
	mw := Idempotency(NewMemoryIdempotencyStore())

	var seen any

	h := func(ctx forge.Context) error {
		seen = ctx.Get("tenant")

		return ctx.NoContent(http.StatusNoContent)
	}

	_, _ = runIdem(t, mw, h, idemCall{key: "k1", body: "{}", setup: func(ctx forge.Context) { ctx.Set("tenant", "acme") }})

	if seen != "acme" {
		t.Fatalf("handler saw tenant = %v, want acme", seen)
	}
}

func TestIdempotencyFingerprintCoversTheQueryString(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())

	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{path: "/orders", key: "k1", body: "{}"})

	rec, err := runIdem(t, mw, countingHandler(&calls), idemCall{path: "/orders?dry_run=true", key: "k1", body: "{}"})
	if err != nil {
		t.Fatal(err)
	}

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (a dry run must not replay the real order)", rec.Code)
	}

	if rec.Header().Get(IdempotentReplayedHeader) != "" || calls.Load() != 1 {
		t.Fatalf("replayed = %q, calls = %d; want no replay and one call", rec.Header().Get(IdempotentReplayedHeader), calls.Load())
	}
}

func TestIdempotencyReleasesTheKeyOnA5xx(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())
	unavailable := func(ctx forge.Context) error {
		calls.Add(1)

		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": "try later"})
	}

	first, err := runIdem(t, mw, unavailable, idemCall{key: "k1", body: "{}"})
	if err != nil || first.Code != http.StatusServiceUnavailable {
		t.Fatalf("first = %d, %v; want 503 sent to the client", first.Code, err)
	}

	second, _ := runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}"})

	if calls.Load() != 2 || second.Code != http.StatusCreated {
		t.Fatalf("calls = %d, second = %d; want the retry to run the handler and get 201", calls.Load(), second.Code)
	}
}

func TestIdempotencyStoresADeterministic4xx(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusConflict, http.StatusUnprocessableEntity} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls atomic.Int32

			mw := Idempotency(NewMemoryIdempotencyStore())
			refuse := func(ctx forge.Context) error {
				calls.Add(1)

				return ctx.JSON(status, map[string]string{"error": "refused"})
			}

			_, _ = runIdem(t, mw, refuse, idemCall{key: "k1", body: "{}"})
			second, _ := runIdem(t, mw, refuse, idemCall{key: "k1", body: "{}"})

			if calls.Load() != 1 || second.Code != status || second.Header().Get(IdempotentReplayedHeader) != "true" {
				t.Fatalf("calls = %d, second = %d; want the %d replayed", calls.Load(), second.Code, status)
			}
		})
	}
}

func TestIdempotencyReleasesTheKeyOnARetryable4xx(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls atomic.Int32

			mw := Idempotency(NewMemoryIdempotencyStore())
			busy := func(ctx forge.Context) error {
				calls.Add(1)

				return ctx.JSON(status, map[string]string{"error": "try later"})
			}

			first, _ := runIdem(t, mw, busy, idemCall{key: "k1", body: "{}"})
			second, _ := runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}"})

			if first.Code != status || calls.Load() != 2 || second.Code != http.StatusCreated || second.Header().Get(IdempotentReplayedHeader) != "" {
				t.Fatalf("first = %d, calls = %d, second = %d; want the retry to run the handler and get 201", first.Code, calls.Load(), second.Code)
			}
		})
	}
}

// A 4xx returned as an error is written by the error handler outside the
// middleware, so nothing is stored and the retry runs again. Only a 4xx the
// handler writes itself is replayed.
func TestIdempotencyReleasesA4xxReturnedAsAnError(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())
	refuse := func(ctx forge.Context) error {
		calls.Add(1)

		return forge.BadRequest("total is required")
	}

	if _, err := runIdem(t, mw, refuse, idemCall{key: "k1", body: "{}"}); err == nil {
		t.Fatal("the handler's error was swallowed")
	}

	_, _ = runIdem(t, mw, refuse, idemCall{key: "k1", body: "{}"})

	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2", calls.Load())
	}
}

func TestIdempotencyBusyConflictCarriesRetryAfter(t *testing.T) {
	cases := map[string]IdempotencyOption{
		"reject":       IdempotencyOnConflict(IdempotencyReject),
		"wait timeout": IdempotencyWaitTimeout(30 * time.Millisecond),
	}

	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32

			mw := Idempotency(NewMemoryIdempotencyStore(), opt)
			entered, release := make(chan struct{}, 1), make(chan struct{})
			h := blockingHandler(&calls, entered, release)

			done := make(chan struct{})
			go func() {
				defer close(done)

				_, _ = runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
			}()

			<-entered

			rec, _ := runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})

			close(release)
			<-done

			if rec.Code != http.StatusConflict || rec.Header().Get("Retry-After") != "1" {
				t.Fatalf("status = %d, Retry-After = %q; want 409 with Retry-After: 1", rec.Code, rec.Header().Get("Retry-After"))
			}
		})
	}
}

func TestIdempotencySkipsAnonymousRequests(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())

	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}", anonymous: true})
	second, _ := runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}", anonymous: true})

	if calls.Load() != 2 || second.Header().Get(IdempotentReplayedHeader) != "" {
		t.Fatalf("calls = %d, replayed = %q; want two runs and no replay", calls.Load(), second.Header().Get(IdempotentReplayedHeader))
	}
}

func TestIdempotencyCanDeduplicateAnonymousRequests(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore(), IdempotencyAllowAnonymous())

	_, _ = runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}", anonymous: true})
	second, _ := runIdem(t, mw, countingHandler(&calls), idemCall{key: "k1", body: "{}", anonymous: true})

	if calls.Load() != 1 || second.Header().Get(IdempotentReplayedHeader) != "true" {
		t.Fatalf("calls = %d, replayed = %q; want one run and a replay", calls.Load(), second.Header().Get(IdempotentReplayedHeader))
	}
}

func TestIdempotencyStoresTheImplicit200OfASilentHandler(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())
	silent := func(ctx forge.Context) error {
		calls.Add(1)
		ctx.Response().Header().Set("X-Order", "7")

		return nil
	}

	_, _ = runIdem(t, mw, silent, idemCall{key: "k1", body: "{}"})
	second, _ := runIdem(t, mw, silent, idemCall{key: "k1", body: "{}"})

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1 (its side effect already happened)", calls.Load())
	}

	if second.Code != http.StatusOK || second.Body.Len() != 0 || second.Header().Get("X-Order") != "7" || second.Header().Get(IdempotentReplayedHeader) != "true" {
		t.Fatalf("replay = %d %q %v, want an empty 200 with the handler's headers", second.Code, second.Body.String(), second.Header())
	}
}

// hintsWriter records informational statuses, which httptest.ResponseRecorder
// would otherwise take as the final one.
type hintsWriter struct {
	*httptest.ResponseRecorder

	informational []int
}

func (w *hintsWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 {
		w.informational = append(w.informational, code)

		return
	}

	w.ResponseRecorder.WriteHeader(code)
}

func TestIdempotencyPassesEarlyHintsThrough(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())
	hinting := func(ctx forge.Context) error {
		ctx.Response().Header().Set("Link", "</app.css>; rel=preload")
		ctx.Response().WriteHeader(http.StatusEarlyHints)

		return countingHandler(&calls)(ctx)
	}

	var hints *hintsWriter

	first, _ := runIdem(t, mw, hinting, idemCall{key: "k1", body: "{}", wrap: func(rec http.ResponseWriter) http.ResponseWriter {
		hints = &hintsWriter{ResponseRecorder: rec.(*httptest.ResponseRecorder)}

		return hints
	}})

	if len(hints.informational) != 1 || hints.informational[0] != http.StatusEarlyHints || first.Code != http.StatusCreated {
		t.Fatalf("informational = %v, final = %d; want [103] then 201", hints.informational, first.Code)
	}

	second, _ := runIdem(t, mw, hinting, idemCall{key: "k1", body: "{}"})
	if calls.Load() != 1 || second.Code != http.StatusCreated || second.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %q after %d calls, want the stored 201", second.Code, second.Body.String(), calls.Load())
	}
}

func TestIdempotencyReplayIsByteIdentical(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore())
	payload := []byte("\x00\xffbinary\r\nbody\x7f")

	h := func(ctx forge.Context) error {
		calls.Add(1)

		hd := ctx.Response().Header()
		hd.Set("Content-Type", "application/octet-stream")
		hd.Add("Link", "</a>; rel=a")
		hd.Add("Link", "</b>; rel=b")
		hd.Set("Content-Length", strconv.Itoa(len(payload)))
		hd.Set("Etag", `"v1"`)
		ctx.Response().WriteHeader(http.StatusAccepted)

		// The handler writes in pieces and then reuses its buffer, so the
		// stored body must be a copy, not the handler's slice.
		buf := bytes.Clone(payload)
		_, _ = ctx.Response().Write(buf[:3])
		_, _ = ctx.Response().Write(buf[3:])

		for i := range buf {
			buf[i] = 'X'
		}

		return nil
	}

	first, _ := runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})

	check := func(name string, replay *httptest.ResponseRecorder) {
		t.Helper()

		if replay.Code != first.Code {
			t.Fatalf("%s status = %d, want %d", name, replay.Code, first.Code)
		}

		if !bytes.Equal(replay.Body.Bytes(), payload) || !bytes.Equal(first.Body.Bytes(), payload) {
			t.Fatalf("%s bodies: original %q, replay %q, want %q", name, first.Body.Bytes(), replay.Body.Bytes(), payload)
		}

		got := replay.Header().Clone()
		if got.Get(IdempotentReplayedHeader) != "true" {
			t.Fatalf("%s is not marked: %v", name, got)
		}

		got.Del(IdempotentReplayedHeader)

		if len(got) != len(first.Header()) {
			t.Fatalf("%s headers = %v, want %v", name, got, first.Header())
		}

		for k, vs := range first.Header() {
			if strings.Join(got[k], "\x00") != strings.Join(vs, "\x00") {
				t.Fatalf("%s header %s = %q, want %q", name, k, got[k], vs)
			}
		}
	}

	second, _ := runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
	check("second", second)

	// A caller that scribbles over one replay must not change the next.
	second.Header()["Link"][0] = "scribbled"
	second.Body.Bytes()[0] = 'X'

	third, _ := runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
	check("third", third)

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", calls.Load())
	}
}

func TestIdempotencyRunsOneHandlerForManyConcurrentDuplicates(t *testing.T) {
	const n = 20

	var calls atomic.Int32

	store := signallingStore{NewMemoryIdempotencyStore(), make(chan struct{}, n)}
	mw := Idempotency(store)
	entered, release := make(chan struct{}, n), make(chan struct{})
	h := blockingHandler(&calls, entered, release)

	var wg sync.WaitGroup

	results := make([]*httptest.ResponseRecorder, n)

	for i := range n {
		wg.Go(func() {
			results[i], _ = runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
		})
	}

	<-entered

	for range n - 1 {
		<-store.inFlight
	}

	close(release)
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", calls.Load())
	}

	replays := 0

	for i, rec := range results {
		if rec.Code != http.StatusCreated || rec.Body.String() != results[0].Body.String() {
			t.Fatalf("request %d got %d %q", i, rec.Code, rec.Body.String())
		}

		if rec.Header().Get(IdempotentReplayedHeader) == "true" {
			replays++
		}
	}

	if replays != n-1 {
		t.Fatalf("%d replays, want %d", replays, n-1)
	}
}

// A holder whose lease lapses, such as one stuck behind a dead dependency,
// must neither strand a waiter until WaitTimeout nor overwrite the response
// of the request that took the key over.
func TestIdempotencyWaiterRetakesALapsedClaimAndTheLateHolderIsFenced(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(NewMemoryIdempotencyStore(), IdempotencyLease(30*time.Millisecond), IdempotencyWaitTimeout(5*time.Second))
	entered, release := make(chan struct{}, 1), make(chan struct{})

	h := func(ctx forge.Context) error {
		n := calls.Add(1)
		if n == 1 {
			entered <- struct{}{}

			<-release
		}

		return ctx.JSON(http.StatusCreated, map[string]any{"n": n})
	}

	stuck := make(chan *httptest.ResponseRecorder, 1)

	go func() {
		rec, _ := runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
		stuck <- rec
	}()

	<-entered

	start := time.Now()
	taken, _ := runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})

	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("the waiter waited %v; a lapsed lease must end the wait", waited)
	}

	if taken.Code != http.StatusCreated || !strings.Contains(taken.Body.String(), `"n":2`) {
		t.Fatalf("waiter got %d %q, want 201 from the second run", taken.Code, taken.Body.String())
	}

	close(release)
	<-stuck

	replay, _ := runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
	if replay.Body.String() != taken.Body.String() || replay.Header().Get(IdempotentReplayedHeader) != "true" {
		t.Fatalf("replay = %q, want the taker's %q (the late holder must not overwrite it)", replay.Body.String(), taken.Body.String())
	}

	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2", calls.Load())
	}
}

func TestIdempotencyWaiterStopsWhenItsRequestIsCancelled(t *testing.T) {
	var calls atomic.Int32

	store := signallingStore{NewMemoryIdempotencyStore(), make(chan struct{}, 1)}
	mw := Idempotency(store)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	h := blockingHandler(&calls, entered, release)

	done := make(chan struct{})
	go func() {
		defer close(done)

		_, _ = runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
	}()

	<-entered

	reqCtx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)

	go func() {
		_, err := runIdemCtx(t, reqCtx, mw, h, idemCall{key: "k1", body: "{}"})
		errc <- err
	}()

	<-store.inFlight
	cancel()

	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	close(release)
	<-done
}

type idemSession struct{ id string }

func (s *idemSession) GetID() string                { return s.id }
func (s *idemSession) GetUserID() string            { return "" }
func (s *idemSession) GetData(string) (any, bool)   { return nil, false }
func (s *idemSession) SetData(string, any)          {}
func (s *idemSession) DeleteData(string)            {}
func (s *idemSession) IsExpired() bool              { return false }
func (s *idemSession) IsValid() bool                { return true }
func (s *idemSession) Touch()                       {}
func (s *idemSession) GetCreatedAt() time.Time      { return time.Time{} }
func (s *idemSession) GetExpiresAt() time.Time      { return time.Time{} }
func (s *idemSession) GetLastAccessedAt() time.Time { return time.Time{} }

func TestIdempotencyCarriesTheSessionThroughTheHandler(t *testing.T) {
	mw := Idempotency(NewMemoryIdempotencyStore())

	var seen string

	h := func(ctx forge.Context) error {
		if s, err := ctx.Session(); err == nil {
			seen = s.GetID()
		}

		ctx.SetSession(&idemSession{id: "rotated"})

		return ctx.NoContent(http.StatusNoContent)
	}

	var outer forge.Context

	_, _ = runIdem(t, mw, h, idemCall{key: "k1", body: "{}", setup: func(ctx forge.Context) {
		outer = ctx
		ctx.SetSession(&idemSession{id: "s1"})
	}})

	if seen != "s1" {
		t.Fatalf("handler saw session %q, want s1", seen)
	}

	if s, err := outer.Session(); err != nil || s.GetID() != "rotated" {
		t.Fatalf("outer session = %v, %v; want the handler's rotated session", s, err)
	}
}

// rawStore keeps the Response it is given and hands that same value back on
// every Begin, as a store without defensive copies would. It also proves an
// IdempotencyStore can be written outside forge with the exported names.
type rawStore struct {
	mu   sync.Mutex
	resp *IdempotentResponse
}

func (s *rawStore) Begin(context.Context, IdempotencyKey, string, time.Duration) (IdempotencyBegun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.resp != nil {
		return IdempotencyBegun{State: IdempotencyReplay, Fingerprint: s.resp.Fingerprint, Response: s.resp}, nil
	}

	return IdempotencyBegun{State: IdempotencyAcquired, Token: 1}, nil
}

func (s *rawStore) Complete(_ context.Context, _ IdempotencyKey, token IdempotencyToken, resp IdempotentResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if token != 1 {
		return ErrIdempotencyNotHolder
	}

	s.resp = &resp

	return nil
}

func (s *rawStore) Release(context.Context, IdempotencyKey, IdempotencyToken) error { return nil }

func TestIdempotencyReplayDoesNotAliasTheStoredResponse(t *testing.T) {
	var calls atomic.Int32

	mw := Idempotency(&rawStore{})

	h := func(ctx forge.Context) error {
		calls.Add(1)
		ctx.Response().Header().Add("Link", "</a>; rel=a")

		return ctx.NoContent(http.StatusAccepted)
	}

	_, _ = runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})
	second, _ := runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})

	// Outer middleware appending to or editing a replayed header must not
	// reach into the store.
	second.Header()["Link"][0] = "scribbled"

	third, _ := runIdem(t, mw, h, idemCall{key: "k1", body: "{}"})

	if got := third.Header().Get("Link"); got != "</a>; rel=a" || calls.Load() != 1 {
		t.Fatalf("third Link = %q, calls = %d; want the stored value and one call", got, calls.Load())
	}
}
