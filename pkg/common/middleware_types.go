package common

import "net/http"

// =============================================================================
// MIDDLEWARE DEFINITIONS
// =============================================================================

// MiddlewareDefinition defines a middleware component
type MiddlewareDefinition struct {
	Name         string                               `json:"name"`
	Priority     int                                  `json:"priority"`
	Handler      func(next http.Handler) http.Handler `json:"-"`
	Dependencies []string                             `json:"dependencies"`
	Config       interface{}                          `json:"config"`
}
