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

// WithRequestSchema sets the unified request schema for OpenAPI generation
// This is the recommended approach for new code.
//
// The struct fields are classified based on tags:
// - path:"paramName" - Path parameter
// - query:"paramName" - Query parameter
// - header:"HeaderName" - Header parameter
// - body:"" or json:"fieldName" - Request body field
//
// Example:
//
//	type CreateUserRequest struct {
//	    TenantID string `path:"tenantId" description:"Tenant ID" format:"uuid"`
//	    DryRun   bool   `query:"dryRun" description:"Preview mode"`
//	    APIKey   string `header:"X-API-Key" description:"API Key"`
//	    Name     string `json:"name" body:"" description:"User name" minLength:"1"`
//	    Email    string `json:"email" body:"" description:"Email" format:"email"`
//	}
//
// If the struct has no path/query/header tags, it's treated as body-only for
// backward compatibility with existing code.
func WithRequestSchema(schemaOrType interface{}) RouteOption {
	return &routeRequestSchema{schema: schemaOrType, unified: true}
}

// WithRequestBodySchema sets only the request body schema for OpenAPI generation
// Use this for explicit body-only schemas, or when you need separate schemas
// for different parts of the request.
// schemaOrType can be either:
// - A pointer to a struct instance for automatic schema generation
// - A *Schema for manual schema specification
func WithRequestBodySchema(schemaOrType interface{}) RouteOption {
	return &routeRequestSchema{schema: schemaOrType, unified: false}
}

type routeRequestSchema struct {
	schema  interface{}
	unified bool
}

func (o *routeRequestSchema) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	if o.unified {
		config.Metadata["openapi.requestSchema.unified"] = o.schema
	} else {
		config.Metadata["openapi.requestSchema"] = o.schema
	}
}

// WithResponseSchema sets the response schema for OpenAPI generation
// statusCode is the HTTP status code (e.g., 200, 201)
// schemaOrType can be either:
// - A pointer to a struct instance for automatic schema generation
// - A *Schema for manual schema specification
func WithResponseSchema(statusCode int, description string, schemaOrType interface{}) RouteOption {
	return &routeResponseSchema{
		statusCode:  statusCode,
		description: description,
		schema:      schemaOrType,
	}
}

type routeResponseSchema struct {
	statusCode  int
	description string
	schema      interface{}
}

func (o *routeResponseSchema) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	responses, ok := config.Metadata["openapi.responseSchemas"].(map[int]*ResponseSchemaDef)
	if !ok {
		responses = make(map[int]*ResponseSchemaDef)
		config.Metadata["openapi.responseSchemas"] = responses
	}

	responses[o.statusCode] = &ResponseSchemaDef{
		Description: o.description,
		Schema:      o.schema,
	}
}

// ResponseSchemaDef defines a response schema
type ResponseSchemaDef struct {
	Description string
	Schema      interface{}
}

// WithQuerySchema sets the query parameters schema for OpenAPI generation
// schemaType should be a struct with query tags
func WithQuerySchema(schemaType interface{}) RouteOption {
	return &routeQuerySchema{schema: schemaType}
}

type routeQuerySchema struct {
	schema interface{}
}

func (o *routeQuerySchema) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}
	config.Metadata["openapi.querySchema"] = o.schema
}

// WithHeaderSchema sets the header parameters schema for OpenAPI generation
// schemaType should be a struct with header tags
func WithHeaderSchema(schemaType interface{}) RouteOption {
	return &routeHeaderSchema{schema: schemaType}
}

type routeHeaderSchema struct {
	schema interface{}
}

func (o *routeHeaderSchema) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}
	config.Metadata["openapi.headerSchema"] = o.schema
}

// WithRequestContentTypes specifies the content types for request body
func WithRequestContentTypes(types ...string) RouteOption {
	return &routeRequestContentTypes{types: types}
}

type routeRequestContentTypes struct {
	types []string
}

func (o *routeRequestContentTypes) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}
	config.Metadata["openapi.requestContentTypes"] = o.types
}

// WithResponseContentTypes specifies the content types for response body
func WithResponseContentTypes(types ...string) RouteOption {
	return &routeResponseContentTypes{types: types}
}

type routeResponseContentTypes struct {
	types []string
}

func (o *routeResponseContentTypes) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}
	config.Metadata["openapi.responseContentTypes"] = o.types
}

// DiscriminatorConfig defines discriminator for polymorphic types
type DiscriminatorConfig struct {
	PropertyName string
	Mapping      map[string]string
}

// WithDiscriminator adds discriminator support for polymorphic schemas
func WithDiscriminator(config DiscriminatorConfig) RouteOption {
	return &discriminatorOpt{config: config}
}

type discriminatorOpt struct {
	config DiscriminatorConfig
}

func (o *discriminatorOpt) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}
	config.Metadata["openapi.discriminator"] = o.config
}

// WithRequestExample adds an example for the request body
func WithRequestExample(name string, example interface{}) RouteOption {
	return &requestExampleOpt{
		name:    name,
		example: example,
	}
}

type requestExampleOpt struct {
	name    string
	example interface{}
}

func (o *requestExampleOpt) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	examples, ok := config.Metadata["openapi.requestExamples"].(map[string]interface{})
	if !ok {
		examples = make(map[string]interface{})
		config.Metadata["openapi.requestExamples"] = examples
	}

	examples[o.name] = o.example
}

// WithResponseExample adds an example for a specific response status code
func WithResponseExample(statusCode int, name string, example interface{}) RouteOption {
	return &responseExampleOpt{
		statusCode: statusCode,
		name:       name,
		example:    example,
	}
}

type responseExampleOpt struct {
	statusCode int
	name       string
	example    interface{}
}

func (o *responseExampleOpt) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	examples, ok := config.Metadata["openapi.responseExamples"].(map[int]map[string]interface{})
	if !ok {
		examples = make(map[int]map[string]interface{})
		config.Metadata["openapi.responseExamples"] = examples
	}

	if examples[o.statusCode] == nil {
		examples[o.statusCode] = make(map[string]interface{})
	}

	examples[o.statusCode][o.name] = o.example
}

// WithSchemaRef adds a schema reference to components
func WithSchemaRef(name string, schema interface{}) RouteOption {
	return &schemaRefOpt{
		name:   name,
		schema: schema,
	}
}

type schemaRefOpt struct {
	name   string
	schema interface{}
}

func (o *schemaRefOpt) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	schemas, ok := config.Metadata["openapi.componentSchemas"].(map[string]interface{})
	if !ok {
		schemas = make(map[string]interface{})
		config.Metadata["openapi.componentSchemas"] = schemas
	}

	schemas[o.name] = o.schema
}
