package forge

import "github.com/xraph/forge/v2/internal/router"

// OpenAPI route options (additional to those in router.go)

// WithSecurity sets security requirements for a route
func WithSecurity(schemes ...string) RouteOption {
	return router.WithSecurity(schemes...)
}

// WithResponse adds a response definition to the route
func WithResponse(code int, description string, example interface{}) RouteOption {
	return router.WithResponse(code, description, example)
}

// ResponseDef defines a response
type ResponseDef = router.ResponseDef

// WithRequestBody adds request body documentation
func WithRequestBody(description string, required bool, example interface{}) RouteOption {
	return router.WithRequestBody(description, required, example)
}

// RequestBodyDef defines a request body
type RequestBodyDef = router.RequestBodyDef

// WithParameter adds a parameter definition
func WithParameter(name, in, description string, required bool, example interface{}) RouteOption {
	return router.WithParameter(name, in, description, required, example)
}

// ParameterDef defines a parameter
type ParameterDef = router.ParameterDef

// WithExternalDocs adds external documentation link
func WithExternalDocs(description, url string) RouteOption {
	return router.WithExternalDocs(description, url)
}

// ExternalDocsDef defines external documentation
type ExternalDocsDef = router.ExternalDocsDef
