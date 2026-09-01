package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/pathspec"
	"github.com/xraph/forge/internal/router/forgemux"
)

func newForgeMuxForTest() RouterAdapter { return forgemux.New() }

func parsePatternForTest(raw string) (pathspec.Pattern, error) { return pathspec.Parse(raw) }

// On an adapter without Constraints, these two routes render to the same path
// and half the traffic silently reaches the wrong handler. Forge can detect
// this because it holds both parsed Patterns; the backend cannot, because by
// the time the path arrives the constraint is gone.
func TestRouter_ErrorsWhenConstraintErasureCollides(t *testing.T) {
	r := NewRouter(WithAdapter(NewBunRouterAdapter()))

	require.NoError(t, r.GET("/users/{id:int}", func(ctx Context) error { return nil }))

	err := r.GET("/users/{slug:alpha}", func(ctx Context) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collide")
	assert.Contains(t, err.Error(), "{id:int}")
	assert.Contains(t, err.Error(), "{slug:alpha}")
}

// One constrained route on its own erases harmlessly. Only a collision is an
// error; degradation alone is a warning.
func TestRouter_AllowsASingleConstrainedRouteOnANarrowAdapter(t *testing.T) {
	r := NewRouter(WithAdapter(NewBunRouterAdapter()))

	require.NoError(t, r.GET("/users/{id:int}", func(ctx Context) error { return nil }))
	require.NoError(t, r.GET("/users/me", func(ctx Context) error { return nil }))
}

// A backend that honors constraints keeps both routes distinct, so there is
// nothing to erase and nothing to report.
func TestRouter_NoCollisionOnAWideAdapter(t *testing.T) {
	r := NewRouter(WithAdapter(newForgeMuxForTest()))

	require.NoError(t, r.GET("/users/{id:int}", func(ctx Context) error { return nil }))
	require.NoError(t, r.GET("/users/{slug:alpha}", func(ctx Context) error { return nil }))
}

// Different methods on the same erased shape are not a collision.
func TestRouter_ErasureCollisionIsPerMethod(t *testing.T) {
	r := NewRouter(WithAdapter(NewBunRouterAdapter()))

	require.NoError(t, r.GET("/users/{id:int}", func(ctx Context) error { return nil }))
	require.NoError(t, r.POST("/users/{slug:alpha}", func(ctx Context) error { return nil }))
}

// Groups share the parent's detection state, so a collision across a group
// boundary is still caught.
func TestRouter_ErasureCollisionAcrossAGroup(t *testing.T) {
	r := NewRouter(WithAdapter(NewBunRouterAdapter()))

	require.NoError(t, r.GET("/api/users/{id:int}", func(ctx Context) error { return nil }))

	group := r.Group("/api")

	err := group.GET("/users/{slug:alpha}", func(ctx Context) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collide")
}

// Registering the identical route twice is a duplicate, not an erasure
// collision, and the old behavior for that is unchanged.
func TestRouter_UnconstrainedDuplicatesAreNotErasureCollisions(t *testing.T) {
	r := NewRouter(WithAdapter(NewBunRouterAdapter()))

	require.NoError(t, r.GET("/users/{id}", func(ctx Context) error { return nil }))
	require.NoError(t, r.GET("/posts/{id}", func(ctx Context) error { return nil }))
}

func TestErasedShape(t *testing.T) {
	tests := []struct {
		a, b string
		same bool
	}{
		{"/users/{id:int}", "/users/{slug:alpha}", true},
		{"/users/{id}", "/users/{name}", true},
		{"/users/{id:int}", "/users/me", false},
		{"/users/{id:int}", "/orders/{id:int}", false},
		{"/files/*", "/files/{x}", false},
		{"/", "/", true},
	}

	for _, tt := range tests {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			a, err := parsePatternForTest(tt.a)
			require.NoError(t, err)

			b, err := parsePatternForTest(tt.b)
			require.NoError(t, err)

			assert.Equal(t, tt.same, erasedShape(a) == erasedShape(b))
		})
	}
}

// What forge's check actually prevents, verified per adapter.
//
// On chi the two erased routes are both accepted and the second silently
// shadows the first, so the constrained route becomes unreachable. On
// bunrouter and httprouter the backend panics at registration with a message
// naming the ERASED paths ("/users/:id" and "/users/:slug"), which gives no
// hint that constraints were the thing distinguishing them.
//
// Forge catches it first either way, and its error names the paths as the
// author wrote them.
func TestRouter_ErasureCollisionIsCaughtBeforeTheBackendSeesIt(t *testing.T) {
	r := NewRouter(WithAdapter(NewBunRouterAdapter()))

	require.NoError(t, r.GET("/users/{id:int}", func(ctx Context) error { return nil }))

	// Without the check this reaches bunrouter and panics. It must return an
	// error instead, and must not panic.
	var err error

	require.NotPanics(t, func() {
		err = r.GET("/users/{slug:alpha}", func(ctx Context) error { return nil })
	}, "forge must catch the collision before the backend can panic on it")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "{id:int}", "the error names the path as written, not the erased form")
	assert.Contains(t, err.Error(), "{slug:alpha}")
	assert.Contains(t, err.Error(), "constraints")
}

func mustParseForTest(t *testing.T, raw string) pathspec.Pattern {
	t.Helper()

	p, err := pathspec.Parse(raw)
	require.NoError(t, err)

	return p
}

var _ = mustParseForTest
