package middleware

import (
	"fmt"
	"net/http"

	forge "github.com/xraph/forge"
)

// Recovery middleware recovers from panics and logs them
// Returns http.StatusInternalServerError on panic
func Recovery(logger forge.Logger) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			defer func() {
				if err := recover(); err != nil {
					// Log the panic with stack trace
					logger.Error(fmt.Sprintf("panic recovered: %v", err))

					// Return http.StatusInternalServerError error
					_ = ctx.String(http.StatusInternalServerError, "Internal Server Error")
				}
			}()

			return next(ctx)
		}
	}
}
