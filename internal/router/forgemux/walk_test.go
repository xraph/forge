package forgemux

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/shared"
)

// buildTree registers "METHOD /path" keys so tests read as route tables.
func buildTree(t *testing.T, routes map[string]string) *tree {
	t.Helper()

	tr := newTree()

	for spec, name := range routes {
		method, path := splitSpec(spec)

		require.NoErrorf(t, tr.insert(method, mustParse(t, path), namedHandler(name), shared.KindHTTP),
			"inserting %q", spec)
	}

	return tr
}

func splitSpec(spec string) (method, path string) {
	for i := range len(spec) {
		if spec[i] == ' ' {
			return spec[:i], spec[i+1:]
		}
	}

	return http.MethodGet, spec
}

// namedHandler writes its name, so a test can assert which route was reached.
func namedHandler(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(name))
	})
}

// bind runs a lookup and returns the matched route's name plus its params.
func bind(t *testing.T, tr *tree, method, path string) (string, map[string]string, result, []string) {
	t.Helper()

	var c capture

	ref, res, allowed := tr.lookup(method, path, &c)
	if res != resultMatched {
		return "", nil, res, allowed
	}

	params := map[string]string{}

	for i, name := range ref.pattern.Params {
		if i >= c.len() {
			break
		}

		start, end := c.at(i)
		params[name] = path[start:end]
	}

	rec := &recordingWriter{}
	ref.handler.ServeHTTP(rec, nil)

	return rec.body, params, resultMatched, nil
}

type recordingWriter struct {
	body string
	hdr  http.Header
}

func (w *recordingWriter) Header() http.Header {
	if w.hdr == nil {
		w.hdr = http.Header{}
	}

	return w.hdr
}

func (w *recordingWriter) Write(b []byte) (int, error) { w.body += string(b); return len(b), nil }
func (w *recordingWriter) WriteHeader(int)             {}

func TestLookup_StaticBeatsParam(t *testing.T) {
	tr := buildTree(t, map[string]string{
		"GET /users/me":   "me",
		"GET /users/{id}": "byID",
	})

	name, _, res, _ := bind(t, tr, http.MethodGet, "/users/me")
	require.Equal(t, resultMatched, res)
	assert.Equal(t, "me", name)

	name, params, res, _ := bind(t, tr, http.MethodGet, "/users/42")
	require.Equal(t, resultMatched, res)
	assert.Equal(t, "byID", name)
	assert.Equal(t, "42", params["id"])
}

// The static branch is entered and must be abandoned. Without backtracking
// this returns 404.
func TestLookup_BacktracksOutOfAStaticDeadEnd(t *testing.T) {
	tr := buildTree(t, map[string]string{
		"GET /users/me/posts":      "mePosts",
		"GET /users/{id}/comments": "idComments",
	})

	name, params, res, _ := bind(t, tr, http.MethodGet, "/users/me/comments")
	require.Equal(t, resultMatched, res)
	assert.Equal(t, "idComments", name)
	assert.Equal(t, "me", params["id"], "the abandoned static segment must be captured as the parameter")
}

func TestLookup_TriesConstraintsInRankOrder(t *testing.T) {
	tr := buildTree(t, map[string]string{
		"GET /x/{n:int}":   "int",
		"GET /x/{s:alpha}": "alpha",
		"GET /x/{a}":       "any",
	})

	for path, want := range map[string]string{
		"/x/42":   "int",
		"/x/abc":  "alpha",
		"/x/a1-b": "any",
	} {
		name, _, res, _ := bind(t, tr, http.MethodGet, path)
		require.Equalf(t, resultMatched, res, "path %s", path)
		assert.Equalf(t, want, name, "path %s", path)
	}
}

// The design's central claim. Two routes share a parameter node but disagree
// about the name; binding at the leaf makes that a non-issue.
func TestLookup_BindsNamesAtTheLeaf(t *testing.T) {
	tr := buildTree(t, map[string]string{
		"GET /users/{id}/posts":     "posts",
		"GET /users/{uid}/comments": "comments",
	})

	name, params, res, _ := bind(t, tr, http.MethodGet, "/users/42/posts")
	require.Equal(t, resultMatched, res)
	assert.Equal(t, "posts", name)
	assert.Equal(t, map[string]string{"id": "42"}, params)

	name, params, res, _ = bind(t, tr, http.MethodGet, "/users/42/comments")
	require.Equal(t, resultMatched, res)
	assert.Equal(t, "comments", name)
	assert.Equal(t, map[string]string{"uid": "42"}, params)
}

func TestLookup_WildcardCapturesTheRemainder(t *testing.T) {
	tr := buildTree(t, map[string]string{"GET /files/*": "files"})

	name, params, res, _ := bind(t, tr, http.MethodGet, "/files/a/b/c.txt")
	require.Equal(t, resultMatched, res)
	assert.Equal(t, "files", name)
	assert.Equal(t, "a/b/c.txt", params["filepath"])
}

func TestLookup_MethodNotAllowedCarriesASortedAllowList(t *testing.T) {
	tr := buildTree(t, map[string]string{
		"GET /users":  "get",
		"POST /users": "post",
	})

	_, _, res, allowed := bind(t, tr, http.MethodDelete, "/users")
	require.Equal(t, resultMethodNotAllowed, res)
	assert.Equal(t, []string{http.MethodGet, http.MethodPost}, allowed)

	_, _, res, _ = bind(t, tr, http.MethodGet, "/nope")
	assert.Equal(t, resultNotFound, res)
}

func TestLookup_AnyMethodMatchesCustomVerbs(t *testing.T) {
	tr := newTree()
	require.NoError(t, tr.insert("", mustParse(t, "/mounted/*"), namedHandler("mount"), shared.KindHTTP))

	for _, method := range []string{http.MethodGet, http.MethodPost, "PROPFIND", "CONNECT"} {
		name, _, res, _ := bind(t, tr, method, "/mounted/x")
		require.Equalf(t, resultMatched, res, "method %s", method)
		assert.Equal(t, "mount", name)
	}
}

func TestLookup_CapturesSpillPastTheInlineLimit(t *testing.T) {
	tr := buildTree(t, map[string]string{
		"GET /{a}/{b}/{c}/{d}/{e}/{f}/{g}/{h}/{i}/{j}": "ten",
	})

	name, params, res, _ := bind(t, tr, http.MethodGet, "/1/2/3/4/5/6/7/8/9/10")
	require.Equal(t, resultMatched, res)
	assert.Equal(t, "ten", name)
	assert.Equal(t, map[string]string{
		"a": "1", "b": "2", "c": "3", "d": "4", "e": "5",
		"f": "6", "g": "7", "h": "8", "i": "9", "j": "10",
	}, params)
}
