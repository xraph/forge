package router

// WithAsyncAPI enables AsyncAPI 3.0.0 spec generation
func WithAsyncAPI(config AsyncAPIConfig) RouterOption {
	return &asyncAPIOption{config: config}
}

type asyncAPIOption struct {
	config AsyncAPIConfig
}

func (o *asyncAPIOption) Apply(cfg *routerConfig) {
	cfg.asyncAPIConfig = &o.config
}

// setupAsyncAPI initializes AsyncAPI generation if configured
func (r *router) setupAsyncAPI() {
	if r.asyncAPIConfig == nil {
		return
	}

	// Create generator
	generator := newAsyncAPIGenerator(*r.asyncAPIConfig, r)

	// Register endpoints
	generator.RegisterEndpoints()

	// Store generator for access
	r.asyncAPIGenerator = generator
}

// AsyncAPISpec returns the generated AsyncAPI specification
// Returns nil if AsyncAPI is not enabled
func (r *router) AsyncAPISpec() *AsyncAPISpec {
	if r.asyncAPIGenerator == nil {
		return nil
	}

	return r.asyncAPIGenerator.Generate()
}
