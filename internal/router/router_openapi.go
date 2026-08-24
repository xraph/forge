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

	// Create generator with container access for auth registry and HTTP address
	generator := newOpenAPIGenerator(*r.openAPIConfig, r, r.container, r.httpAddress)

	// Set logger if available
	if r.logger != nil {
		generator.schemas.setLogger(r.logger)
	}

	// Register endpoints
	generator.RegisterEndpoints()

	// Validate schema generation - this will detect and report all collisions
	_, err := generator.Generate()
	if err != nil {
		// Log the error and panic to crash the server
		if r.logger != nil {
			r.logger.Error("OpenAPI schema generation failed: " + err.Error())
		}

		panic("OpenAPI schema generation failed: " + err.Error())
	}

	// Store generator for access
	r.openAPIGenerator = generator
}

// OpenAPISpec returns the generated OpenAPI specification
// Returns nil if OpenAPI is not enabled.
func (r *router) OpenAPISpec() *OpenAPISpec {
	if r.openAPIGenerator == nil {
		return nil
	}

	spec, err := r.openAPIGenerator.Generate()
	if err != nil {
		// The nil this returns says only that there is no spec, and callers
		// have had to guess why -- the usual guess being that WithOpenAPI was
		// never configured, which is the one cause it cannot be by this point.
		// Generate already knows the real reason, so say it out loud.
		if r.logger != nil {
			r.logger.Error("OpenAPI spec generation failed: " + err.Error())
		}

		return nil
	}

	return spec
}
