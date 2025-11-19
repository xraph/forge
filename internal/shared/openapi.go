package shared

// OpenAPIConfig configures OpenAPI 3.1.0 generation.
type OpenAPIConfig struct {
	// Basic info
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`
	Version     string `json:"version" yaml:"version"`

	// OpenAPI version (default: "3.1.0")
	OpenAPIVersion string `json:"openAPIVersion" yaml:"openAPIVersion"`

	// Server configuration
	Servers []OpenAPIServer `json:"servers" yaml:"servers"`

	// Security schemes
	Security map[string]SecurityScheme `json:"security" yaml:"security"`

	// Global tags
	Tags []OpenAPITag `json:"tags" yaml:"tags"`

	// External docs
	ExternalDocs *ExternalDocs `json:"externalDocs" yaml:"externalDocs"`

	// Contact info
	Contact *Contact `json:"contact" yaml:"contact"`
	License *License `json:"license" yaml:"license"`

	// UI configuration
	UIPath      string `json:"uiPath" yaml:"uiPath"`           // Default: "/swagger"
	SpecPath    string `json:"specPath" yaml:"specPath"`       // Default: "/openapi.json"
	UIEnabled   bool   `json:"uiEnabled" yaml:"uiEnabled"`     // Default: true
	SpecEnabled bool   `json:"specEnabled" yaml:"specEnabled"` // Default: true

	// Generation options
	PrettyJSON          bool `json:"prettyJSON" yaml:"prettyJSON"`
	IncludeExamples     bool `json:"includeExamples" yaml:"includeExamples"`
	IncludeDescriptions bool `json:"includeDescriptions" yaml:"includeDescriptions"`
	ValidateResponses   bool `json:"validateResponses" yaml:"validateResponses"`
}

// OpenAPIServer represents a server in the OpenAPI spec.
type OpenAPIServer struct {
	URL         string                    `json:"url" yaml:"url"`
	Description string                    `json:"description" yaml:"description"`
	Title       string                    `json:"title" yaml:"title"`
	Variables   map[string]ServerVariable `json:"variables" yaml:"variables"`
}

// ServerVariable represents a variable in a server URL.
type ServerVariable struct {
	Default     string   `json:"default" yaml:"default"`
	Enum        []string `json:"enum" yaml:"enum"`
	Description string   `json:"description" yaml:"description"`
}

// SecurityScheme defines a security scheme.
type SecurityScheme struct {
	Type             string      `json:"type"                       yaml:"type"` // "apiKey", "http", "oauth2", "openIdConnect"
	Description      string      `json:"description,omitempty"      yaml:"description,omitempty"`
	Name             string      `json:"name,omitempty"             yaml:"name,omitempty"`         // For apiKey
	In               string      `json:"in,omitempty"               yaml:"in,omitempty"`           // For apiKey: "query", "header", "cookie"
	Scheme           string      `json:"scheme,omitempty"           yaml:"scheme,omitempty"`       // For http: "bearer", "basic"
	BearerFormat     string      `json:"bearerFormat,omitempty"     yaml:"bearerFormat,omitempty"` // For http bearer
	Flows            *OAuthFlows `json:"flows,omitempty"            yaml:"flows,omitempty"`
	OpenIdConnectUrl string      `json:"openIdConnectUrl,omitempty" yaml:"openIdConnectUrl,omitempty"`
}

// OAuthFlows defines OAuth 2.0 flows.
type OAuthFlows struct {
	Implicit          *OAuthFlow
	Password          *OAuthFlow
	ClientCredentials *OAuthFlow
	AuthorizationCode *OAuthFlow
}

// OAuthFlow defines a single OAuth 2.0 flow.
type OAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl" yaml:"authorizationUrl"`
	TokenURL         string            `json:"tokenUrl" yaml:"tokenUrl"`
	RefreshURL       string            `json:"refreshUrl" yaml:"refreshUrl"`
	Scopes           map[string]string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

// OpenAPITag represents a tag in the OpenAPI spec.
type OpenAPITag struct {
	Name         string        `json:"name" yaml:"name"`
	Description  string        `json:"description" yaml:"description"`
	ExternalDocs *ExternalDocs `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
}

// ExternalDocs points to external documentation.
type ExternalDocs struct {
	Description string `json:"description" yaml:"description"`
	URL         string `json:"url" yaml:"url"`
}

// Contact represents contact information.
type Contact struct {
	Name  string `json:"name" yaml:"name"`
	Email string `json:"email" yaml:"email"`
	URL   string `json:"url" yaml:"url"`
}

// License represents license information.
type License struct {
	Name string `json:"name" yaml:"name"`
	URL  string `json:"url" yaml:"url"`
}

// OpenAPISpec represents the complete OpenAPI 3.1.0 specification.
type OpenAPISpec struct {
	OpenAPI      string                `json:"openapi"`
	Info         Info                  `json:"info"`
	Servers      []OpenAPIServer       `json:"servers,omitempty"`
	Paths        map[string]*PathItem  `json:"paths"`
	Components   *Components           `json:"components,omitempty"`
	Security     []SecurityRequirement `json:"security,omitempty"`
	Tags         []OpenAPITag          `json:"tags,omitempty"`
	ExternalDocs *ExternalDocs         `json:"externalDocs,omitempty"`
	Webhooks     map[string]*PathItem  `json:"webhooks,omitempty"`
}

// Info provides metadata about the API.
type Info struct {
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	Version        string   `json:"version"`
	TermsOfService string   `json:"termsOfService,omitempty"`
	Contact        *Contact `json:"contact,omitempty"`
	License        *License `json:"license,omitempty"`
}

// PathItem describes operations available on a single path.
type PathItem struct {
	Summary     string      `json:"summary,omitempty"`
	Description string      `json:"description,omitempty"`
	Get         *Operation  `json:"get,omitempty"`
	Put         *Operation  `json:"put,omitempty"`
	Post        *Operation  `json:"post,omitempty"`
	Delete      *Operation  `json:"delete,omitempty"`
	Options     *Operation  `json:"options,omitempty"`
	Head        *Operation  `json:"head,omitempty"`
	Patch       *Operation  `json:"patch,omitempty"`
	Parameters  []Parameter `json:"parameters,omitempty"`
}

// Operation describes a single API operation on a path.
type Operation struct {
	Tags         []string                        `json:"tags,omitempty"         yaml:"tags,omitempty"`
	Summary      string                          `json:"summary,omitempty"      yaml:"summary,omitempty"`
	Description  string                          `json:"description,omitempty"  yaml:"description,omitempty"`
	OperationID  string                          `json:"operationId,omitempty"  yaml:"operationId,omitempty"`
	Parameters   []Parameter                     `json:"parameters,omitempty"   yaml:"parameters,omitempty"`
	RequestBody  *RequestBody                    `json:"requestBody,omitempty"  yaml:"requestBody,omitempty"`
	Responses    map[string]*Response            `json:"responses"              yaml:"responses"`
	Callbacks    map[string]map[string]*PathItem `json:"callbacks,omitempty"    yaml:"callbacks,omitempty"`
	Deprecated   bool                            `json:"deprecated,omitempty"   yaml:"deprecated,omitempty"`
	Security     []SecurityRequirement           `json:"security,omitempty"     yaml:"security,omitempty"`
	ExternalDocs *ExternalDocs                   `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
}

// Parameter describes a single operation parameter.
type Parameter struct {
	Name            string              `json:"name"                      yaml:"name"`
	In              string              `json:"in"                        yaml:"in"` // "query", "header", "path", "cookie"
	Description     string              `json:"description,omitempty"     yaml:"description,omitempty"`
	Required        bool                `json:"required,omitempty"        yaml:"required,omitempty"`
	Deprecated      bool                `json:"deprecated,omitempty"      yaml:"deprecated,omitempty"`
	AllowEmptyValue bool                `json:"allowEmptyValue,omitempty" yaml:"allowEmptyValue,omitempty"`
	Schema          *Schema             `json:"schema,omitempty"          yaml:"schema,omitempty"`
	Example         any                 `json:"example,omitempty"         yaml:"example,omitempty"`
	Examples        map[string]*Example `json:"examples,omitempty"        yaml:"examples,omitempty"`
}

// RequestBody describes a single request body.
type RequestBody struct {
	Description string                `json:"description,omitempty"`
	Content     map[string]*MediaType `json:"content"`
	Required    bool                  `json:"required,omitempty"`
}

// Response describes a single response from an API operation.
type Response struct {
	Description string                `json:"description"`
	Headers     map[string]*Header    `json:"headers,omitempty"`
	Content     map[string]*MediaType `json:"content,omitempty"`
	Links       map[string]*Link      `json:"links,omitempty"`
}

// MediaType provides schema and examples for a media type.
type MediaType struct {
	Schema   *Schema              `json:"schema,omitempty"`
	Example  any                  `json:"example,omitempty"`
	Examples map[string]*Example  `json:"examples,omitempty"`
	Encoding map[string]*Encoding `json:"encoding,omitempty"`
}

// Schema represents a JSON Schema (OpenAPI 3.1.0 uses JSON Schema 2020-12).
type Schema struct {
	Type        string `json:"type,omitempty"`
	Format      string `json:"format,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Default     any    `json:"default,omitempty"`
	Nullable    bool   `json:"nullable,omitempty"`
	ReadOnly    bool   `json:"readOnly,omitempty"`
	WriteOnly   bool   `json:"writeOnly,omitempty"`
	Example     any    `json:"example,omitempty"`
	Deprecated  bool   `json:"deprecated,omitempty"`

	// Validation
	MultipleOf       float64  `json:"multipleOf,omitempty"`
	Maximum          float64  `json:"maximum,omitempty"`
	ExclusiveMaximum bool     `json:"exclusiveMaximum,omitempty"`
	Minimum          float64  `json:"minimum,omitempty"`
	ExclusiveMinimum bool     `json:"exclusiveMinimum,omitempty"`
	MaxLength        int      `json:"maxLength,omitempty"`
	MinLength        int      `json:"minLength,omitempty"`
	Pattern          string   `json:"pattern,omitempty"`
	MaxItems         int      `json:"maxItems,omitempty"`
	MinItems         int      `json:"minItems,omitempty"`
	UniqueItems      bool     `json:"uniqueItems,omitempty"`
	MaxProperties    int      `json:"maxProperties,omitempty"`
	MinProperties    int      `json:"minProperties,omitempty"`
	Required         []string `json:"required,omitempty"`
	Enum             []any    `json:"enum,omitempty"`

	// Object/Array properties
	Properties           map[string]*Schema `json:"properties,omitempty"`
	AdditionalProperties any                `json:"additionalProperties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`

	// Composition
	AllOf []Schema `json:"allOf,omitempty"`
	AnyOf []Schema `json:"anyOf,omitempty"`
	OneOf []Schema `json:"oneOf,omitempty"`
	Not   *Schema  `json:"not,omitempty"`

	// Discriminator (OpenAPI 3.1.0)
	Discriminator *Discriminator `json:"discriminator,omitempty"`

	// Reference
	Ref string `json:"$ref,omitempty"`
}

// Discriminator supports polymorphism.
type Discriminator struct {
	PropertyName string            `json:"propertyName"`
	Mapping      map[string]string `json:"mapping,omitempty"`
}

// Example provides an example value.
type Example struct {
	Summary       string `json:"summary,omitempty"`
	Description   string `json:"description,omitempty"`
	Value         any    `json:"value,omitempty"`
	ExternalValue string `json:"externalValue,omitempty"`
}

// Header describes a single header parameter.
type Header struct {
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Deprecated  bool    `json:"deprecated,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
	Example     any     `json:"example,omitempty"`
}

// Link represents a possible design-time link for a response.
type Link struct {
	OperationRef string         `json:"operationRef,omitempty"`
	OperationID  string         `json:"operationId,omitempty"`
	Parameters   map[string]any `json:"parameters,omitempty"`
	RequestBody  any            `json:"requestBody,omitempty"`
	Description  string         `json:"description,omitempty"`
	Server       *OpenAPIServer `json:"server,omitempty"`
}

// Encoding defines encoding for a property.
type Encoding struct {
	ContentType   string             `json:"contentType,omitempty"`
	Headers       map[string]*Header `json:"headers,omitempty"`
	Style         string             `json:"style,omitempty"`
	Explode       bool               `json:"explode,omitempty"`
	AllowReserved bool               `json:"allowReserved,omitempty"`
}

// Components holds reusable objects for the API spec.
type Components struct {
	Schemas         map[string]*Schema        `json:"schemas,omitempty"         yaml:"schemas,omitempty"`
	Responses       map[string]*Response      `json:"responses,omitempty"       yaml:"responses,omitempty"`
	Parameters      map[string]*Parameter     `json:"parameters,omitempty"      yaml:"parameters,omitempty"`
	Examples        map[string]*Example       `json:"examples,omitempty"        yaml:"examples,omitempty"`
	RequestBodies   map[string]*RequestBody   `json:"requestBodies,omitempty"   yaml:"requestBodies,omitempty"`
	Headers         map[string]*Header        `json:"headers,omitempty"         yaml:"headers,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	Links           map[string]*Link          `json:"links,omitempty"           yaml:"links,omitempty"`
	Callbacks       map[string]any            `json:"callbacks,omitempty"       yaml:"callbacks,omitempty"`
}

// SecurityRequirement lists required security schemes.
type SecurityRequirement map[string][]string
