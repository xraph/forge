package forgemux

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/pathspec"
	"github.com/xraph/forge/internal/shared"
)

func mustParse(t *testing.T, raw string) pathspec.Pattern {
	t.Helper()

	p, err := pathspec.Parse(raw)
	require.NoError(t, err)

	return p
}

func noopHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
}

func TestTree_InsertAcceptsDistinctRoutes(t *testing.T) {
	tr := newTree()

	for _, raw := range []string{"/", "/users", "/users/me", "/users/{id}", "/files/*"} {
		require.NoErrorf(t, tr.insert(http.MethodGet, mustParse(t, raw), noopHandler(), shared.KindHTTP),
			"inserting %q", raw)
	}
}

// Same shape, same method, differing only by parameter name is ambiguous.
func TestTree_InsertRejectsAnIdenticalShape(t *testing.T) {
	tr := newTree()

	require.NoError(t, tr.insert(http.MethodGet, mustParse(t, "/users/{id}"), noopHandler(), shared.KindHTTP))

	err := tr.insert(http.MethodGet, mustParse(t, "/users/{name}"), noopHandler(), shared.KindHTTP)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
	assert.Contains(t, err.Error(), "/users/{name}")
	assert.Contains(t, err.Error(), "/users/{id}")
}

// A constrained parameter is a different shape from a bare one, so these
// coexist. This is what makes /users/{id:int} plus /users/{slug:alpha} legal.
func TestTree_InsertAllowsDifferentConstraintsOnOneSegment(t *testing.T) {
	tr := newTree()

	require.NoError(t, tr.insert(http.MethodGet, mustParse(t, "/users/{id:int}"), noopHandler(), shared.KindHTTP))
	require.NoError(t, tr.insert(http.MethodGet, mustParse(t, "/users/{slug:alpha}"), noopHandler(), shared.KindHTTP))
	require.NoError(t, tr.insert(http.MethodGet, mustParse(t, "/users/{any}"), noopHandler(), shared.KindHTTP))
}

func TestTree_InsertAllowsTheSameShapeOnDifferentMethods(t *testing.T) {
	tr := newTree()

	require.NoError(t, tr.insert(http.MethodGet, mustParse(t, "/users/{id}"), noopHandler(), shared.KindHTTP))
	require.NoError(t, tr.insert(http.MethodPost, mustParse(t, "/users/{id}"), noopHandler(), shared.KindHTTP))
}

func TestTree_InsertRejectsTwoAnyMethodRoutesOnOneNode(t *testing.T) {
	tr := newTree()

	require.NoError(t, tr.insert("", mustParse(t, "/mounted/*"), noopHandler(), shared.KindHTTP))

	err := tr.insert("", mustParse(t, "/mounted/*"), noopHandler(), shared.KindHTTP)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "every method")
}

// Without a cap, a deeply nested path exhausts the stack during the walk.
func TestTree_InsertRejectsTooManySegments(t *testing.T) {
	raw := ""
	for range maxSegments + 1 {
		raw += "/a"
	}

	err := newTree().insert(http.MethodGet, mustParse(t, raw), noopHandler(), shared.KindHTTP)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "segments")
}

func TestShapeOf_DiscardsNamesButKeepsConstraints(t *testing.T) {
	assert.Equal(t,
		shapeOf(mustParse(t, "/users/{id}")),
		shapeOf(mustParse(t, "/users/{name}")),
		"names must not affect shape")

	assert.NotEqual(t,
		shapeOf(mustParse(t, "/users/{id:int}")),
		shapeOf(mustParse(t, "/users/{id}")),
		"a constraint must affect shape")

	assert.NotEqual(t,
		shapeOf(mustParse(t, "/users/{id}")),
		shapeOf(mustParse(t, "/users/me")),
		"a static segment must not share a shape with a parameter")
}
