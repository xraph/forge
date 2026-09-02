package router

import (
	"net/http"
	"sync"
	"sync/atomic"

	forge_http "github.com/xraph/go-utils/http"
	"github.com/xraph/vessel"
)

// globalChain holds the forge middleware registered through UseGlobal.
//
// Every extension that contributes middleware goes through this path (see
// app.applyExtensionMiddlewares), so its cost is paid on every request and
// multiplied by the number of extensions installed. Registering each
// middleware as its own adapter-level wrapper made that cost linear: each
// wrapper built its own forge Context, so ten extension middlewares meant ten
// context checkouts, ten closure allocations and ten cleanups per request
// before the route handler ran.
//
// Instead the whole set is installed as a single adapter wrapper. One Context
// is built per request and the forge chain is composed once at registration,
// so adding an extension no longer adds per-request work. It also means a
// value written with ctx.Set in one global middleware is visible to the next
// one, which was not true when each ran against its own Context.
//
// A router shares this with every group derived from it, because UseGlobal is
// global by contract regardless of which group it was called on.
type globalChain struct {
	mu         sync.Mutex
	middleware []Middleware
	installed  bool

	// terminal is the adapter's downstream handler, captured when the adapter
	// installs the wrapper. Nil until then.
	terminal http.Handler

	// composed is the forge handler chain built over terminal. Requests read
	// it with a single atomic load.
	composed atomic.Pointer[Handler]
}

// add appends middleware and returns true if the caller must install the
// wrapper with the adapter (the first call only).
func (gc *globalChain) add(mw []Middleware) (install bool) {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	gc.middleware = append(gc.middleware, mw...)
	gc.recomposeLocked()

	install = !gc.installed
	gc.installed = true

	return install
}

// recomposeLocked rebuilds the composed chain. It is a no-op until the adapter
// has handed over its downstream handler.
func (gc *globalChain) recomposeLocked() {
	if gc.terminal == nil {
		return
	}

	next := gc.terminal
	h := Handler(func(ctx Context) error {
		next.ServeHTTP(ctx.Response(), ctx.Request())

		return nil
	})

	for i := len(gc.middleware) - 1; i >= 0; i-- {
		h = gc.middleware[i](h)
	}

	gc.composed.Store(&h)
}

// httpMiddleware returns the single adapter-level wrapper for the whole chain.
func (gc *globalChain) httpMiddleware(container vessel.Vessel, errorHandler ErrorHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		gc.mu.Lock()
		gc.terminal = next
		gc.recomposeLocked()
		gc.mu.Unlock()

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			composed := gc.composed.Load()
			if composed == nil {
				next.ServeHTTP(w, r)

				return
			}

			ctx := forge_http.NewContext(w, r, container)
			defer ctx.(forge_http.ContextWithClean).Cleanup()

			if err := (*composed)(ctx); err != nil {
				if errorHandler != nil {
					_ = errorHandler.HandleError(ctx.Context(), err)
				} else {
					// See applyMiddleware. Global middleware maps
					// errors the same way route middleware does.
					handleError(ctx, err)
				}
			}
		})
	}
}
