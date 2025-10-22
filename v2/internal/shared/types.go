package shared

import (
	"context"
)

// Context wraps http.Request with convenience methods
type Context interface {
	context.Context
	Context() context.Context
	Set(key string, value any)
	Get(key string) any
}
