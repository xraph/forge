package forgemux

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	forge_http "github.com/xraph/go-utils/http"

	"github.com/xraph/forge/internal/shared"
)

func TestMux_SatisfiesExtendedAdapter(t *testing.T) {
	var adapter shared.ExtendedAdapter = New()

	require.NotNil(t, adapter)

	caps := adapter.Capabilities()
	assert.True(t, caps.MethodNotAllowed)
	assert.True(t, caps.Constraints)
	assert.True(t, caps.ConflictDetection)
	assert.True(t, caps.TypedParams)
	assert.True(t, caps.AnyMethod)
}

func TestMux_ServesAMatchedRouteWithParams(t *testing.T) {
	m := New()

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method:  http.MethodGet,
		Pattern: mustParse(t, "/users/{id:int}"),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rp, ok := r.Context().Value(forge_http.RouteParamsKey).(*forge_http.RouteParams)
			require.True(t, ok, "the typed carrier must be published")

			v, found := rp.Get("id")
			require.True(t, found)

			_, _ = w.Write([]byte(v))
		}),
	}))

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "42", rec.Body.String())
}

func TestMux_ConstraintMissFallsThrough(t *testing.T) {
	m := New()

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/users/{id:int}"),
		Handler: namedHandler("byID"),
	}))
	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/users/me"),
		Handler: namedHandler("me"),
	}))

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/me", nil))
	assert.Equal(t, "me", rec.Body.String())

	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	assert.Equal(t, "byID", rec.Body.String())
}

func TestMux_MethodNotAllowedSetsAllow(t *testing.T) {
	m := New()

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		require.NoError(t, m.HandleRoute(shared.RouteSpec{
			Method: method, Pattern: mustParse(t, "/users"), Handler: namedHandler(method),
		}))
	}

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/users", nil))

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET, POST", rec.Header().Get("Allow"))
}

func TestMux_NotFound(t *testing.T) {
	m := New()

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// A long-lived route must not have its carrier recycled: the handler may hold
// it for the life of the connection.
func TestMux_DoesNotPoolParamsForALongLivedRoute(t *testing.T) {
	m := New()

	var held *forge_http.RouteParams

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method:  http.MethodGet,
		Pattern: mustParse(t, "/events/{room}"),
		Kind:    shared.KindSSE,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			held, _ = r.Context().Value(forge_http.RouteParamsKey).(*forge_http.RouteParams)
			w.WriteHeader(http.StatusOK)
		}),
	}))

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/events/lobby", nil))

	require.NotNil(t, held)

	// Drive an unrelated request through the pool. If the SSE carrier had been
	// released, this would recycle it and the value below would be gone.
	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/other/{x}"), Handler: namedHandler("other"),
	}))

	m.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/other/zzz", nil))

	v, ok := held.Get("room")
	assert.True(t, ok, "a streaming handler's parameters must survive later requests")
	assert.Equal(t, "lobby", v)
}

func TestMux_ConfigureRejectsAChangeAfterRegistration(t *testing.T) {
	m := New()

	require.NoError(t, m.Configure(shared.MatcherConfig{NotFound: http.NotFoundHandler()}))
	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/a"), Handler: namedHandler("a"),
	}))

	err := m.Configure(shared.MatcherConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after")
}

func TestMux_BothSpellingsReachTheSameHandler(t *testing.T) {
	m := New()
	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/dashboard"), Handler: namedHandler("dash"),
	}))

	for _, path := range []string{"/dashboard", "/dashboard/"} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		assert.Equalf(t, http.StatusOK, rec.Code, "path %s", path)
		assert.Equal(t, "dash", rec.Body.String())
	}
}

// r.URL.Path must never be rewritten. The old adapter mutated it, so handlers
// could not see the path the client actually sent.
func TestMux_LenientDoesNotMutateTheRequestPath(t *testing.T) {
	m := New()

	var seen string

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/dashboard"),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = r.URL.Path
		}),
	}))

	m.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/dashboard/", nil))

	assert.Equal(t, "/dashboard/", seen, "the handler must see the path the client sent")
}

// Registering the trailing-slash spelling must not create a second route.
// Parse normalizes it, so both registrations describe the same pattern and the
// second is a genuine conflict.
func TestMux_BothSpellingsAreTheSameRoute(t *testing.T) {
	m := New()
	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/dashboard/"), Handler: namedHandler("dash"),
	}))

	for _, path := range []string{"/dashboard", "/dashboard/"} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		assert.Equalf(t, http.StatusOK, rec.Code, "path %s", path)
	}

	err := m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/dashboard"), Handler: namedHandler("other"),
	})
	require.Error(t, err, "the two spellings are one route and must not both register")
}

// No response is ever a redirect. The old adapter's 301 let clients rewrite
// POST as GET, which silently drops the body.
func TestMux_NeverRedirectsAPost(t *testing.T) {
	m := New()

	var sawMethod string

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodPost, Pattern: mustParse(t, "/submit"),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sawMethod = r.Method
			w.WriteHeader(http.StatusOK)
		}),
	}))

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/submit/", nil))

	assert.Equal(t, http.StatusOK, rec.Code, "a trailing slash must not produce a redirect")
	assert.Equal(t, http.MethodPost, sawMethod, "the method must survive intact")
}

// An upgrade route is reached directly. A redirect in front of a handshake
// breaks it, which is one reason no redirect mode exists.
func TestMux_UpgradeRouteIsReachedOnBothSpellings(t *testing.T) {
	m := New()
	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/ws"),
		Kind: shared.KindWebSocket, Handler: namedHandler("ws"),
	}))

	for _, path := range []string{"/ws", "/ws/"} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		assert.Equalf(t, http.StatusOK, rec.Code, "path %s must not be redirected", path)
		assert.Equal(t, "ws", rec.Body.String())
	}
}

// Hammer distinct parameter values through one matcher and assert every
// handler observed its own. A carrier released too early, or reused without a
// reset, shows up here and essentially nowhere else.
func TestMux_NoCrossRequestParamBleedUnderConcurrency(t *testing.T) {
	m := New()

	var mismatches atomic.Int64

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method:  http.MethodGet,
		Pattern: mustParse(t, "/tenants/{tenant}/users/{id}"),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rp, ok := r.Context().Value(forge_http.RouteParamsKey).(*forge_http.RouteParams)
			if !ok {
				mismatches.Add(1)

				return
			}

			tenant, _ := rp.Get("tenant")
			id, _ := rp.Get("id")

			// The request is built so the two values always agree.
			if tenant != id {
				mismatches.Add(1)
			}

			w.WriteHeader(http.StatusOK)
		}),
	}))

	const workers, iterations = 16, 200

	var wg sync.WaitGroup

	for w := range workers {
		wg.Add(1)

		go func(worker int) {
			defer wg.Done()

			for i := range iterations {
				value := strconv.Itoa(worker*iterations + i)
				path := "/tenants/" + value + "/users/" + value

				m.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
			}
		}(w)
	}

	wg.Wait()

	assert.Zero(t, mismatches.Load(), "a handler saw parameters belonging to another request")
}

// A streaming handler holds its carrier past the response. Ordinary traffic
// running concurrently must not recycle it out from under them.
func TestMux_StreamingCarrierSurvivesConcurrentTraffic(t *testing.T) {
	m := New()

	held := make(chan *forge_http.RouteParams, 1)

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method:  http.MethodGet,
		Pattern: mustParse(t, "/events/{room}"),
		Kind:    shared.KindSSE,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rp, _ := r.Context().Value(forge_http.RouteParamsKey).(*forge_http.RouteParams)
			held <- rp
		}),
	}))

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/plain/{x}"), Handler: namedHandler("plain"),
	}))

	m.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/events/lobby", nil))

	carrier := <-held
	require.NotNil(t, carrier)

	var wg sync.WaitGroup

	for w := range 8 {
		wg.Add(1)

		go func(worker int) {
			defer wg.Done()

			for i := range 200 {
				path := "/plain/" + strconv.Itoa(worker*1000+i)
				m.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
			}
		}(w)
	}

	wg.Wait()

	room, ok := carrier.Get("room")
	assert.True(t, ok, "the streaming handler's carrier was recycled")
	assert.Equal(t, "lobby", room)
}

// The bunrouter adapter maps a param named "filepath" onto "*", and existing
// forge routes read ctx.Param("*"). The matcher owes them both names.
func TestMux_WildcardIsAlsoReachableAsStar(t *testing.T) {
	m := New()

	var star, named string

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method:  http.MethodGet,
		Pattern: mustParse(t, "/static/*"),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rp, _ := r.Context().Value(forge_http.RouteParamsKey).(*forge_http.RouteParams)
			star, _ = rp.Get("*")
			named, _ = rp.Get("filepath")
			w.WriteHeader(http.StatusOK)
		}),
	}))

	m.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil))

	assert.Equal(t, "css/app.css", star, `the wildcard must be reachable as "*"`)
	assert.Equal(t, "css/app.css", named, "and under its own name")
}

func TestMux_NamedWildcardIsAlsoReachableAsStar(t *testing.T) {
	m := New()

	var star, named string

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method:  http.MethodGet,
		Pattern: mustParse(t, "/static/*assetPath"),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rp, _ := r.Context().Value(forge_http.RouteParamsKey).(*forge_http.RouteParams)
			star, _ = rp.Get("*")
			named, _ = rp.Get("assetPath")
			w.WriteHeader(http.StatusOK)
		}),
	}))

	m.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/static/js/app.js", nil))

	assert.Equal(t, "js/app.js", star)
	assert.Equal(t, "js/app.js", named)
}

// The root path has no segments, so the walk must treat the root node as the
// terminal rather than looking for an empty segment beneath it.
func TestMux_RootPathMatches(t *testing.T) {
	m := New()

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/"), Handler: namedHandler("root"),
	}))

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "root", rec.Body.String())
}

// A direct lookup with a trailing slash must resolve too, for callers driving
// the tree without going through dispatch's normalization.
func TestLookup_TrailingSlashResolvesWithoutDispatch(t *testing.T) {
	tr := buildTree(t, map[string]string{"GET /users": "users"})

	name, _, res, _ := bind(t, tr, http.MethodGet, "/users/")
	require.Equal(t, resultMatched, res)
	assert.Equal(t, "users", name)
}

// A repeated slash is an empty segment. It collapses, so "/users//42" reaches
// the same handler as "/users/42".
//
// The BunRouter adapter cleans these too, but for an interior double slash it
// does so with a 301, which is the redirect this design removed: a 301 permits
// a client to rewrite POST as GET and drop the body. Collapsing during the
// walk reaches the same handler without any redirect at all.
func TestMux_CollapsesRepeatedSlashes(t *testing.T) {
	m := New()

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/users/{id}"), Handler: namedHandler("byID"),
	}))
	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method: http.MethodGet, Pattern: mustParse(t, "/users/{id}/posts"), Handler: namedHandler("posts"),
	}))

	for path, want := range map[string]string{
		"/users//42":         "byID",
		"//users/42":         "byID",
		"/users/42//posts":   "posts",
		"//users//42//posts": "posts",
	} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		assert.Equalf(t, http.StatusOK, rec.Code, "path %s", path)
		assert.Equalf(t, want, rec.Body.String(), "path %s", path)
	}
}

// Collapsing must not swallow a real segment into a captured parameter.
func TestMux_CollapsingKeepsParamValuesIntact(t *testing.T) {
	m := New()

	var got string

	require.NoError(t, m.HandleRoute(shared.RouteSpec{
		Method:  http.MethodGet,
		Pattern: mustParse(t, "/users/{id}"),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rp, _ := r.Context().Value(forge_http.RouteParamsKey).(*forge_http.RouteParams)
			got, _ = rp.Get("id")
			w.WriteHeader(http.StatusOK)
		}),
	}))

	m.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users//42", nil))

	assert.Equal(t, "42", got, "the collapsed slash must not leak into the captured value")
}
