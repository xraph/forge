package router

// WithAsyncAPI enables AsyncAPI 3.0.0 spec generation.
func WithAsyncAPI(config AsyncAPIConfig) RouterOption {
	return &asyncAPIOption{config: config}
}

type asyncAPIOption struct {
	config AsyncAPIConfig
}

func (o *asyncAPIOption) Apply(cfg *routerConfig) {
	cfg.asyncAPIConfig = &o.config
}

// setupAsyncAPI initializes AsyncAPI generation if configured.
func (r *router) setupAsyncAPI() {
	if r.asyncAPIConfig == nil {
		return
	}

	// Create generator
	generator := newAsyncAPIGenerator(*r.asyncAPIConfig, r)

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
			r.logger.Error("AsyncAPI schema generation failed: " + err.Error())
		}
		panic("AsyncAPI schema generation failed: " + err.Error())
	}

	// Store generator for access
	r.asyncAPIGenerator = generator
}

// AsyncAPISpec returns the generated AsyncAPI specification
// Returns nil if AsyncAPI is not enabled.
func (r *router) AsyncAPISpec() *AsyncAPISpec {
	if r.asyncAPIGenerator == nil {
		return nil
	}

	spec, _ := r.asyncAPIGenerator.Generate()
	return spec
}
