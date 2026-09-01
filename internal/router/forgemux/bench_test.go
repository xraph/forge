package forgemux

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/pathspec"
	"github.com/xraph/forge/internal/shared"
)

func benchTree(tb testing.TB) *tree {
	tb.Helper()

	tr := newTree()

	routes := []struct{ method, path string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/users"},
		{http.MethodGet, "/users/me"},
		{http.MethodGet, "/users/{id:int}"},
		{http.MethodGet, "/users/{id:int}/posts/{postId:int}"},
		{http.MethodPost, "/users"},
		{http.MethodGet, "/orders/{id:uuid}"},
		{http.MethodGet, "/files/*"},
	}

	for _, r := range routes {
		p, err := pathspec.Parse(r.path)
		require.NoError(tb, err)
		require.NoError(tb, tr.insert(r.method, p, noopHandler(), shared.KindHTTP))
	}

	return tr
}

func benchLookup(tr *tree, method, path string) result {
	var c capture

	_, res, _ := tr.lookup(method, path, &c)

	return res
}

// The walk must not allocate. Everything it records is a byte offset into the
// request path, and nothing becomes a string until a route is confirmed.
func TestWalk_DoesNotAllocate(t *testing.T) {
	tr := benchTree(t)

	for _, path := range []string{"/users/me", "/users/42", "/users/42/posts/7", "/files/a/b/c.txt"} {
		allocs := testing.AllocsPerRun(500, func() {
			benchLookup(tr, http.MethodGet, path)
		})

		assert.LessOrEqualf(t, allocs, 0.0, "walking %q allocated %.1f times, want 0", path, allocs)
	}
}

func BenchmarkWalk_Static(b *testing.B) {
	tr := benchTree(b)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if benchLookup(tr, http.MethodGet, "/users/me") != resultMatched {
			b.Fatal("miss")
		}
	}
}

func BenchmarkWalk_TwoParams(b *testing.B) {
	tr := benchTree(b)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if benchLookup(tr, http.MethodGet, "/users/42/posts/7") != resultMatched {
			b.Fatal("miss")
		}
	}
}

// Enters /users/me statically, dead-ends, falls back to {id:int}.
func BenchmarkWalk_Backtrack(b *testing.B) {
	tr := benchTree(b)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if benchLookup(tr, http.MethodGet, "/users/42") != resultMatched {
			b.Fatal("miss")
		}
	}
}

func BenchmarkWalk_Wildcard(b *testing.B) {
	tr := benchTree(b)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if benchLookup(tr, http.MethodGet, "/files/a/b/c.txt") != resultMatched {
			b.Fatal("miss")
		}
	}
}
