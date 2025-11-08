package router

// WithOpenAPI enables OpenAPI 3.1.0 spec generation.
func WithOpenAPI(config OpenAPIConfig) RouterOption {
	return &openAPIOption{config: config}
}

type openAPIOption struct {
	config OpenAPIConfig
}

func (o *openAPIOption) Apply(cfg *routerConfig) {
	cfg.openAPIConfig = &o.config
}

// setupOpenAPI initializes OpenAPI generation if configured.
func (r *router) setupOpenAPI() {
	if r.openAPIConfig == nil {
		return
	}

	// Create generator with container access for auth registry
	generator := newOpenAPIGenerator(*r.openAPIConfig, r, r.container)

	// Register endpoints
	generator.RegisterEndpoints()

	// Store generator for access
	r.openAPIGenerator = generator
}

// OpenAPISpec returns the generated OpenAPI specification
// Returns nil if OpenAPI is not enabled.
func (r *router) OpenAPISpec() *OpenAPISpec {
	if r.openAPIGenerator == nil {
		return nil
	}

	return r.openAPIGenerator.Generate()
}
