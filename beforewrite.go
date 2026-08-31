package forge

import (
	"net/http"

	"github.com/xraph/forge/internal/shared"
)

// BeforeWriter is a response writer that can run callbacks at the moment the
// response headers are committed. Every request forge routes is wrapped in one,
// so ctx.Response() satisfies it.
//
// Prefer the BeforeWrite helper below over asserting to this directly.
type BeforeWriter = shared.BeforeWriter

// BeforeWrite registers fn to run immediately before the response headers are
// committed, and reports whether the registration took.
//
// This is the answer to a sharp edge in writing middleware: forge streams
// responses, so a handler's first write puts the status line on the connection.
// A header set after next() returns has already missed the wire — and an
// httptest.ResponseRecorder will not tell you, because WriteHeader snapshots
// the header map while Header() keeps handing back the live one. Middleware
// that needs to set a header derived from the handler's outcome therefore had
// two bad options: do the work before the handler and guess at the outcome, or
// set the header afterwards and lose it silently.
//
// With a callback, the middleware keeps its natural shape:
//
//	func Middleware() forge.Middleware {
//	    return func(next forge.Handler) forge.Handler {
//	        return func(ctx forge.Context) error {
//	            forge.BeforeWrite(ctx, func() {
//	                // Runs just before headers go out, so it sees whatever the
//	                // handler decided — and is still early enough to deliver.
//	                ctx.Response().Header().Set("X-Trace", traceOf(ctx))
//	            })
//	            return next(ctx)
//	        }
//	    }
//	}
//
// Callbacks run once, in registration order, and may mutate Header(). Writing
// the body or the status from inside one is allowed but pointless — the headers
// are already going out by then.
//
// Reports false when the headers are already committed (the caller is too late,
// and fn will not run) or when the response writer is not a BeforeWriter, which
// in practice means a hand-rolled test double rather than a routed request.
// Treat false as "this header cannot be delivered" rather than ignoring it.
//
// A hijacked connection — a WebSocket upgrade, say — drops pending callbacks,
// since there is no longer an HTTP response of ours for them to affect.
func BeforeWrite(ctx Context, fn func()) bool {
	if ctx == nil || fn == nil {
		return false
	}
	return shared.BeforeWrite(ctx.Response(), fn)
}

// ResponseWritten reports whether the response headers have been committed to
// the client. Useful to a recovery or error-mapping middleware deciding whether
// it can still replace the response or has to leave a partial one alone.
//
// Reports false for a response writer that is not a BeforeWriter.
func ResponseWritten(ctx Context) bool {
	if ctx == nil {
		return false
	}
	bw, ok := ctx.Response().(BeforeWriter)
	if !ok {
		return false
	}
	return bw.Written()
}

// WrapBeforeWrite returns w as a BeforeWriter, wrapping it if it is not one
// already. Idempotent.
//
// Routed requests are wrapped by forge itself; this is for code standing up its
// own http.Handler chain outside the router and wanting the same facility.
func WrapBeforeWrite(w http.ResponseWriter) http.ResponseWriter {
	return shared.WrapBeforeWrite(w)
}
