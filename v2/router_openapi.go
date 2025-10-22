package forge

import "github.com/xraph/forge/v2/internal/router"

// WithOpenAPI enables OpenAPI 3.1.0 spec generation
func WithOpenAPI(config OpenAPIConfig) RouterOption {
	return router.WithOpenAPI(config)
}
