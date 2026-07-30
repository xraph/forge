package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"

	forge "github.com/xraph/forge"
)

// Recovery middleware recovers from panics and logs them with a stack trace.
// Returns http.StatusInternalServerError on panic.
//
// http.ErrAbortHandler is deliberately re-panicked: net/http uses it as the
// signal to drop the connection without logging, and swallowing it here would
// turn an intentional abort into a bogus 500.
func Recovery(logger forge.Logger) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}

				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(rec)
				}

				// Guard the logger: a nil logger here would panic inside the
				// deferred recover, killing the connection instead of returning
				// the 500 this middleware exists to produce. Other middleware in
				// this package already treat the logger as optional.
				if logger != nil {
					logger.Error(fmt.Sprintf("panic recovered: %v\n%s", rec, debug.Stack()))
				}

				// If the handler already wrote a response before panicking, the
				// status line is spent; writing another emits a superfluous
				// header and appends error text to a partial body. Leave it
				// alone — the log carries the detail.
				if responseAlreadyWritten(ctx) {
					return
				}

				_ = ctx.String(http.StatusInternalServerError, "Internal Server Error")
			}()

			return next(ctx)
		}
	}
}

// responseAlreadyWritten reports whether the response writer has already
// committed a status code.
//
// It relies on the writer exposing Written(); when the writer is not wrapped
// (so there is nothing to ask) it reports false and Recovery falls back to
// writing the 500, matching the previous behaviour.
func responseAlreadyWritten(ctx forge.Context) bool {
	type wroteReporter interface{ Written() bool }

	if w, ok := ctx.Response().(wroteReporter); ok {
		return w.Written()
	}

	return false
}
