package router

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/router/testtypes/billing"
	"github.com/xraph/forge/internal/router/testtypes/shipping"
	"github.com/xraph/forge/internal/router/testtypes/warehouse"
	"github.com/xraph/forge/internal/shared"
)

type raceEmptyRequest struct{}

// raceRouter builds a router with a route set big enough that spec generation
// touches the shared component map many times per call: three types competing
// for one bare name, so the naming pass has real work to do as well.
func raceRouter(t *testing.T) Router {
	t.Helper()

	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Race", Version: "1.0.0"}))

	require.NoError(t, r.GET("/billing/invoice",
		func(ctx shared.Context, req *raceEmptyRequest) (*billing.Invoice, error) {
			return &billing.Invoice{}, nil
		}))
	require.NoError(t, r.GET("/shipping/invoice",
		func(ctx shared.Context, req *raceEmptyRequest) (*shipping.Invoice, error) {
			return &shipping.Invoice{}, nil
		}))
	require.NoError(t, r.GET("/warehouse/invoice",
		func(ctx shared.Context, req *raceEmptyRequest) (*warehouse.Invoice, error) {
			return &warehouse.Invoice{}, nil
		}))
	require.NoError(t, r.GET("/warehouse/receipt",
		func(ctx shared.Context, req *raceEmptyRequest) (*warehouse.Receipt, error) {
			return &warehouse.Receipt{}, nil
		}))

	return r
}

// asyncRaceRouter is the AsyncAPI counterpart of raceRouter.
func asyncRaceRouter(t *testing.T) Router {
	t.Helper()

	r := NewRouter(WithAsyncAPI(AsyncAPIConfig{Title: "Race", Version: "1.0.0"}))

	require.NoError(t, r.WebSocket("/ws/chat", func(ctx Context, conn Connection) error { return nil },
		WithWebSocketMessages(ChatMessage{}, ChatEvent{}),
		WithName("chat")))
	require.NoError(t, r.WebSocket("/ws/notify", func(ctx Context, conn Connection) error { return nil },
		WithWebSocketMessages(NotificationEvent{}, ChatEvent{}),
		WithName("notify")))

	return r
}

// TestOpenAPISpec_ConcurrentCallers is the regression guard for the crash that
// `OpenAPISpec()` used to be: it regenerated the document on every call, and
// generation wrote into the generator's shared component map. Two callers at
// once raced on a plain Go map, which is a fatal error -- no recover, no
// middleware, the process dies. The spec document is served over HTTP, so any
// two concurrent requests to it were enough.
//
// Run under -race this fails on the data race long before the fatal error has
// a chance to fire, which is the reliable signal.
func TestOpenAPISpec_ConcurrentCallers(t *testing.T) {
	r := raceRouter(t)

	const callers = 16

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
	)

	specs := make([]*OpenAPISpec, callers)

	for i := range callers {
		wg.Go(func() {
			<-start

			specs[i] = r.OpenAPISpec()
		})
	}

	close(start)
	wg.Wait()

	for i, spec := range specs {
		require.NotNil(t, spec, "caller %d got a nil spec", i)
		require.NotNil(t, spec.Components, "caller %d got a spec with no components", i)
	}
}

// TestOpenAPISpec_RepeatedCallsEquivalent is the other half of the caching
// contract: a second call must not hand back something subtly different from
// the first. Comparing the marshalled documents checks what a consumer sees.
func TestOpenAPISpec_RepeatedCallsEquivalent(t *testing.T) {
	r := raceRouter(t)

	first, err := json.Marshal(r.OpenAPISpec())
	require.NoError(t, err)

	second, err := json.Marshal(r.OpenAPISpec())
	require.NoError(t, err)

	require.JSONEq(t, string(first), string(second))
}

// TestOpenAPISpec_CacheInvalidatesOnNewRoute pins the invalidation. Routes are
// registered well after the generator exists -- extensions add theirs during
// their own Register and Start phases -- so a cache that never refreshed would
// serve a document missing most of the API.
func TestOpenAPISpec_CacheInvalidatesOnNewRoute(t *testing.T) {
	r := raceRouter(t)

	before := r.OpenAPISpec()
	require.NotNil(t, before)
	require.Contains(t, before.Paths, "/billing/invoice")
	require.NotContains(t, before.Paths, "/warehouse/receipt/late")

	require.NoError(t, r.GET("/warehouse/receipt/late",
		func(ctx shared.Context, req *raceEmptyRequest) (*warehouse.Receipt, error) {
			return &warehouse.Receipt{}, nil
		}))

	after := r.OpenAPISpec()
	require.NotNil(t, after)
	require.Contains(t, after.Paths, "/warehouse/receipt/late", "a route added after the first call is missing from the spec")
	require.Contains(t, after.Paths, "/billing/invoice")

	// The document handed out before the new route must still be intact. It
	// used to point straight at the generator's component map, which the next
	// generation clears in place -- so regenerating emptied it out from under
	// whoever was still holding it.
	require.NotEmpty(t, before.Components.Schemas,
		"regenerating the spec emptied a document already returned to a caller")
	require.NotContains(t, before.Paths, "/warehouse/receipt/late")
}

// TestOpenAPISpec_CachedNamesMatchRegenerated is the collision-naming guard for
// the cache. finalizeComponentNames now runs once per route revision instead of
// once per call; the names it settles on must be the ones a fresh regeneration
// would produce, or a cached document and an uncached one would disagree.
func TestOpenAPISpec_CachedNamesMatchRegenerated(t *testing.T) {
	r := raceRouter(t)

	cached, err := json.Marshal(r.OpenAPISpec())
	require.NoError(t, err)

	gen, ok := r.(*router).openAPIGenerator.(*openAPIGenerator)
	require.True(t, ok)

	// Force the work the cache skips: three more full passes, each of which
	// re-runs beginSpec and finalizeComponentNames over the same route set.
	var regenerated []byte

	for range 3 {
		spec, genErr := gen.generate()
		require.NoError(t, genErr)

		regenerated, err = json.Marshal(spec)
		require.NoError(t, err)
	}

	require.JSONEq(t, string(cached), string(regenerated),
		"component names differ between a cached document and a regenerated one")
}

// TestAsyncAPISpec_ConcurrentCallers is the same guard for the AsyncAPI half,
// which had the identical shape: AsyncAPISpec() regenerated per call and the
// generator shares one schema generator and one component map.
func TestAsyncAPISpec_ConcurrentCallers(t *testing.T) {
	r := asyncRaceRouter(t)

	const callers = 16

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
	)

	specs := make([]*AsyncAPISpec, callers)

	for i := range callers {
		wg.Go(func() {
			<-start

			specs[i] = r.AsyncAPISpec()
		})
	}

	close(start)
	wg.Wait()

	for i, spec := range specs {
		require.NotNil(t, spec, "caller %d got a nil spec", i)
		require.NotNil(t, spec.Components, "caller %d got a spec with no components", i)
	}
}

// TestAsyncAPISpec_RepeatedCallsAndInvalidation mirrors the OpenAPI cache
// contract on the AsyncAPI side: repeated calls agree, and a channel added
// later shows up.
func TestAsyncAPISpec_RepeatedCallsAndInvalidation(t *testing.T) {
	r := asyncRaceRouter(t)

	first, err := json.Marshal(r.AsyncAPISpec())
	require.NoError(t, err)

	second, err := json.Marshal(r.AsyncAPISpec())
	require.NoError(t, err)

	require.JSONEq(t, string(first), string(second))

	before := r.AsyncAPISpec()
	require.NotNil(t, before)

	channelsBefore := len(before.Channels)
	require.Positive(t, channelsBefore)

	require.NoError(t, r.WebSocket("/ws/late", func(ctx Context, conn Connection) error { return nil },
		WithWebSocketMessages(ChatMessage{}, NotificationEvent{}),
		WithName("late")))

	after := r.AsyncAPISpec()
	require.NotNil(t, after)
	require.Greater(t, len(after.Channels), channelsBefore,
		"a WebSocket route added after the first call is missing from the spec")

	require.Len(t, before.Channels, channelsBefore,
		"regenerating mutated a document already returned to a caller")
}

// TestOpenAPISpec_ConfigCollectionsDetached is the sibling of the Schemas
// detachment above, one field over. Components.SecuritySchemes used to be
// g.config.Security itself, and addAuthSecuritySchemes maps.Copy's into that
// map on every generation -- so generation wrote into the caller's config, and
// every document ever returned shared the one map. Tags aliased the config
// slice the same way.
//
// Writing through the config the caller still holds is the direct probe: if the
// document were still aliasing it, the write would show up in the document.
func TestOpenAPISpec_ConfigCollectionsDetached(t *testing.T) {
	cfg := OpenAPIConfig{
		Title:    "Detach",
		Version:  "1.0.0",
		Security: map[string]SecurityScheme{"bearer": {Type: "http", Scheme: "bearer"}},
		Tags:     []OpenAPITag{{Name: "original"}},
	}

	r := NewRouter(WithOpenAPI(cfg))

	require.NoError(t, r.GET("/billing/invoice",
		func(ctx shared.Context, req *raceEmptyRequest) (*billing.Invoice, error) {
			return &billing.Invoice{}, nil
		}))

	spec := r.OpenAPISpec()
	require.NotNil(t, spec)
	require.Contains(t, spec.Components.SecuritySchemes, "bearer")
	require.Equal(t, "original", spec.Tags[0].Name)

	// The caller still owns cfg's map and slice; the generator copied the
	// headers but not the backing storage, so these reach g.config directly.
	cfg.Security["injected"] = SecurityScheme{Type: "apiKey"}
	cfg.Tags[0].Name = "mutated"

	require.NotContains(t, spec.Components.SecuritySchemes, "injected",
		"the returned document still aliases the config's security scheme map")
	require.Equal(t, "original", spec.Tags[0].Name,
		"the returned document still aliases the config's tag slice")

	// And a regeneration must leave the already-returned document alone, the
	// same guarantee the Schemas clone gives.
	require.NoError(t, r.GET("/shipping/invoice",
		func(ctx shared.Context, req *raceEmptyRequest) (*shipping.Invoice, error) {
			return &shipping.Invoice{}, nil
		}))

	next := r.OpenAPISpec()
	require.NotNil(t, next)
	require.Contains(t, next.Paths, "/shipping/invoice")

	require.Contains(t, spec.Components.SecuritySchemes, "bearer",
		"regenerating disturbed a document already returned to a caller")
	require.Len(t, spec.Components.SecuritySchemes, 1)
	require.Equal(t, "original", spec.Tags[0].Name)

	// The two documents must not share the maps either.
	next.Components.SecuritySchemes["only-in-next"] = SecurityScheme{Type: "apiKey"}
	require.NotContains(t, spec.Components.SecuritySchemes, "only-in-next",
		"two returned documents share one security scheme map")
}

// TestAsyncAPISpec_ConfigServersDetached is the AsyncAPI equivalent. Servers is
// the only mutable collection AsyncAPIConfig hands straight to the document.
func TestAsyncAPISpec_ConfigServersDetached(t *testing.T) {
	cfg := AsyncAPIConfig{
		Title:   "Detach",
		Version: "1.0.0",
		Servers: map[string]*AsyncAPIServer{
			"production": {Host: "api.example.com", Protocol: "wss"},
		},
	}

	r := NewRouter(WithAsyncAPI(cfg))

	require.NoError(t, r.WebSocket("/ws/chat", func(ctx Context, conn Connection) error { return nil },
		WithWebSocketMessages(ChatMessage{}, ChatEvent{}),
		WithName("chat")))

	spec := r.AsyncAPISpec()
	require.NotNil(t, spec)
	require.Contains(t, spec.Servers, "production")

	cfg.Servers["injected"] = &AsyncAPIServer{Host: "evil.example.com", Protocol: "wss"}

	require.NotContains(t, spec.Servers, "injected",
		"the returned document still aliases the config's server map")

	require.NoError(t, r.WebSocket("/ws/late", func(ctx Context, conn Connection) error { return nil },
		WithWebSocketMessages(NotificationEvent{}, ChatEvent{}),
		WithName("late")))

	next := r.AsyncAPISpec()
	require.NotNil(t, next)

	require.Len(t, spec.Servers, 1,
		"regenerating disturbed a document already returned to a caller")

	next.Servers["only-in-next"] = &AsyncAPIServer{Host: "next.example.com", Protocol: "wss"}
	require.NotContains(t, spec.Servers, "only-in-next",
		"two returned documents share one server map")
}
