package router

import (
	"net/http"
	"slices"
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

	// outer holds http.Handler middleware that wraps the whole chain,
	// whatever order it was registered in. Tracing and HTTP metrics live
	// here so they see every request first, including 404s and requests an
	// extension middleware rejects. They are plain http.Handler middleware,
	// so running them here costs no forge Context; installing them through
	// PureMiddleware built one per middleware per request.
	outer []func(http.Handler) http.Handler

	// terminal is the adapter's downstream handler, captured when the adapter
	// installs the wrapper. Nil until then. container and errorHandler come
	// with it, for the Context the forge chain runs on.
	terminal     http.Handler
	container    vessel.Vessel
	errorHandler ErrorHandler

	// composed is the full chain, outer middleware included, built over
	// terminal. Requests read it with a single atomic load.
	composed atomic.Pointer[http.Handler]
}

// add appends middleware and returns true if the caller must install the
// wrapper with the adapter (the first call only).
func (gc *globalChain) add(mw []Middleware) (install bool) {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	gc.middleware = append(gc.middleware, mw...)
	gc.recomposeLocked()

	return gc.claimInstallLocked()
}

// addOuter appends http.Handler middleware to the outer slot. It returns
// true under the same rule as add.
func (gc *globalChain) addOuter(mw []func(http.Handler) http.Handler) (install bool) {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	gc.outer = append(gc.outer, mw...)
	gc.recomposeLocked()

	return gc.claimInstallLocked()
}

func (gc *globalChain) claimInstallLocked() bool {
	install := !gc.installed
	gc.installed = true

	return install
}

// recomposeLocked rebuilds the composed chain. It is a no-op until the adapter
// has handed over its downstream handler.
func (gc *globalChain) recomposeLocked() {
	if gc.terminal == nil {
		return
	}

	h := gc.terminal
	if len(gc.middleware) > 0 {
		h = gc.forgeHandlerLocked()
	}

	for _, v := range slices.Backward(gc.outer) {
		h = v(h)
	}

	gc.composed.Store(&h)
}

// forgeHandlerLocked composes the forge middleware over terminal and returns
// it as an http.Handler that builds the one Context they share. With no forge
// middleware registered the chain skips this, so an app whose only global
// middleware is in the outer slot builds no Context before routing.
func (gc *globalChain) forgeHandlerLocked() http.Handler {
	next := gc.terminal
	h := Handler(func(ctx Context) error {
		next.ServeHTTP(ctx.Response(), handOffValues(ctx))

		return nil
	})

	for _, v := range slices.Backward(gc.middleware) {
		h = v(h)
	}

	container, errorHandler := gc.container, gc.errorHandler

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := newContext(w, r, container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		if err := h(ctx); err != nil {
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

// install records the adapter's downstream handler and composes the chain
// over it. The unlock is deferred so a panic while composing cannot leave the
// chain locked, which would hang the next UseGlobal instead of failing it.
func (gc *globalChain) install(next http.Handler, container vessel.Vessel, errorHandler ErrorHandler) {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	gc.terminal = next
	gc.container = container
	gc.errorHandler = errorHandler
	gc.recomposeLocked()
}

// httpMiddleware returns the single adapter-level wrapper for the whole chain.
func (gc *globalChain) httpMiddleware(container vessel.Vessel, errorHandler ErrorHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		gc.install(next, container, errorHandler)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			composed := gc.composed.Load()
			if composed == nil {
				next.ServeHTTP(w, r)

				return
			}

			(*composed).ServeHTTP(w, r)
		})
	}
}
