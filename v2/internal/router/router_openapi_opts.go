package router

// OpenAPI route options (additional to those in router.go)

// WithSecurity sets security requirements for a route
func WithSecurity(schemes ...string) RouteOption {
	return &routeSecurity{schemes: schemes}
}

type routeSecurity struct {
	schemes []string
}

func (o *routeSecurity) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}
	config.Metadata["security"] = o.schemes
}

// WithResponse adds a response definition to the route
func WithResponse(code int, description string, example interface{}) RouteOption {
	return &routeResponse{
		code:        code,
		description: description,
		example:     example,
	}
}

type routeResponse struct {
	code        int
	description string
	example     interface{}
}

func (o *routeResponse) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	responses, ok := config.Metadata["responses"].(map[int]*ResponseDef)
	if !ok {
		responses = make(map[int]*ResponseDef)
		config.Metadata["responses"] = responses
	}

	responses[o.code] = &ResponseDef{
		Description: o.description,
		Example:     o.example,
	}
}

// ResponseDef defines a response
type ResponseDef struct {
	Description string
	Example     interface{}
}

// WithRequestBody adds request body documentation
func WithRequestBody(description string, required bool, example interface{}) RouteOption {
	return &routeRequestBody{
		description: description,
		required:    required,
		example:     example,
	}
}

type routeRequestBody struct {
	description string
	required    bool
	example     interface{}
}

func (o *routeRequestBody) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	config.Metadata["requestBody"] = &RequestBodyDef{
		Description: o.description,
		Required:    o.required,
		Example:     o.example,
	}
}

// RequestBodyDef defines a request body
type RequestBodyDef struct {
	Description string
	Required    bool
	Example     interface{}
}

// WithParameter adds a parameter definition
func WithParameter(name, in, description string, required bool, example interface{}) RouteOption {
	return &routeParameter{
		name:        name,
		in:          in,
		description: description,
		required:    required,
		example:     example,
	}
}

type routeParameter struct {
	name        string
	in          string
	description string
	required    bool
	example     interface{}
}

func (o *routeParameter) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	params, ok := config.Metadata["parameters"].([]ParameterDef)
	if !ok {
		params = []ParameterDef{}
	}

	params = append(params, ParameterDef{
		Name:        o.name,
		In:          o.in,
		Description: o.description,
		Required:    o.required,
		Example:     o.example,
	})

	config.Metadata["parameters"] = params
}

// ParameterDef defines a parameter
type ParameterDef struct {
	Name        string
	In          string
	Description string
	Required    bool
	Example     interface{}
}

// WithExternalDocs adds external documentation link
func WithExternalDocs(description, url string) RouteOption {
	return &routeExternalDocs{
		description: description,
		url:         url,
	}
}

type routeExternalDocs struct {
	description string
	url         string
}

func (o *routeExternalDocs) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	config.Metadata["externalDocs"] = &ExternalDocsDef{
		Description: o.description,
		URL:         o.url,
	}
}

// ExternalDocsDef defines external documentation
type ExternalDocsDef struct {
	Description string
	URL         string
}
