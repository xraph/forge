package router

import (
	"context"
	"net/http"
	"reflect"

	forge_http "github.com/xraph/go-utils/http"
	"github.com/xraph/vessel"
)

// contextHandler is the http.Handler a forge handler signature converts to.
//
// The route chain calls serve with the Context its middleware ran on, so the
// middleware, the interceptors and the handler share one Context: what one
// stores with ctx.Set, the next reads with ctx.Get, and nothing is copied or
// allocated to get it there. ServeHTTP is for callers that hold only (w, r),
// and builds a Context of its own.
type contextHandler struct {
	container vessel.Vessel
	serve     func(ctx Context)
}

func (h *contextHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(w, r, h.container)
	defer ctx.(forge_http.ContextWithClean).Cleanup()

	h.serve(ctx)
}

// terminal returns the innermost step of a forge chain over h. A forge
// handler runs on the chain's own Context. Anything else gets the request
// handed off with the chain's values, so a streaming route or a mounted
// handler that builds its own Context still sees them.
func terminal(h http.Handler) func(ctx Context) {
	if ch, ok := h.(*contextHandler); ok {
		return ch.serve
	}

	return func(ctx Context) {
		h.ServeHTTP(ctx.Response(), handOffValues(ctx))
	}
}

// requestValuesKey carries, on the request context, the values map of the
// forge Context that is handing the request on to the next layer.
//
// The global chain and the route chain each build their own pooled Context,
// and so does a streaming route's handler. They meet only through
// http.Handler, so without this a value one layer stored with ctx.Set never
// reached the next. Middleware that authenticated a request could publish
// who the caller was, and the handler would read nil.
type requestValuesKey struct{}

// handOffValues returns ctx's request carrying ctx's values map, for a layer
// about to call an http.Handler that will build a Context of its own.
//
// The map is the live one, not a copy, so a value stored downstream is
// visible to this layer's middleware after next returns, the same as on a
// single Context.
//
// The map belongs to a pooled Context and is cleared when that Context is
// reused, so it is only valid until the layer that handed it off returns.
// newContext reads it synchronously while that layer is still on the stack;
// nothing else should.
func handOffValues(ctx Context) *http.Request {
	r := ctx.Request()

	v, ok := ctx.(contextValuer)
	if !ok {
		return r
	}

	values := v.Values()

	// A Context that borrowed its map from an outer layer hands off the same
	// map, and the request already carries it. Skip the allocation.
	if carried, ok := r.Context().Value(requestValuesKey{}).(map[string]any); ok && sameMap(carried, values) {
		return r
	}

	return r.WithContext(context.WithValue(r.Context(), requestValuesKey{}, values))
}

// newContext builds a forge Context for r. When an outer layer handed r off
// with handOffValues, the new Context borrows that values map, so ctx.Get
// sees what outer middleware stored and ctx.Set writes where outer middleware
// will look.
//
// Cleanup releases the borrow before the Context goes back to the pool (see
// forge_http.Ctx.ShareValues), so callers clean up exactly as they would a
// Context from forge_http.NewContext.
func newContext(w http.ResponseWriter, r *http.Request, container vessel.Vessel) Context {
	ctx := forge_http.NewContext(w, r, container)

	values, ok := r.Context().Value(requestValuesKey{}).(map[string]any)
	if !ok || values == nil {
		return ctx
	}

	if s, ok := ctx.(contextSharer); ok {
		s.ShareValues(values)
	}

	return ctx
}

// sameMap reports whether a and b are the same map, not merely equal ones.
func sameMap(a, b map[string]any) bool {
	return reflect.ValueOf(a).UnsafePointer() == reflect.ValueOf(b).UnsafePointer()
}
