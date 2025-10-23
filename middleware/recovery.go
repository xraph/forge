package middleware

import (
	"fmt"
	"net/http"

	forge "github.com/xraph/forge"
)

// Recovery middleware recovers from panics and logs them
// Returns 500 Internal Server Error on panic
func Recovery(logger forge.Logger) forge.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log the panic with stack trace
					logger.Error(fmt.Sprintf("panic recovered: %v", err))

					// Return 500 error
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
