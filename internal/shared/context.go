package shared

import (
	"github.com/xraph/go-utils/http"
)

// Session represents a user session (mirrors security.Session).
type Session = http.Session

// ResponseBuilder provides fluent response building.
type ResponseBuilder = http.ResponseBuilder

// Context wraps http.Request with convenience methods.
type Context = http.Context
