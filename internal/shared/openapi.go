package shared

// OpenAPIConfig configures OpenAPI 3.1.0 generation
type OpenAPIConfig struct {
	// Basic info
	Title       string
	Description string
	Version     string

	// OpenAPI version (default: "3.1.0")
	OpenAPIVersion string

	// Server configuration
	Servers []OpenAPIServer

	// Security schemes
	Security map[string]SecurityScheme

	// Global tags
	Tags []OpenAPITag

	// External docs
	ExternalDocs *ExternalDocs

	// Contact info
	Contact *Contact
	License *License

	// UI configuration
	UIPath      string // Default: "/swagger"
	SpecPath    string // Default: "/openapi.json"
	UIEnabled   bool   // Default: true
	SpecEnabled bool   // Default: true

	// Generation options
	PrettyJSON          bool
	IncludeExamples     bool
	IncludeDescriptions bool
	ValidateResponses   bool
}

// OpenAPIServer represents a server in the OpenAPI spec
type OpenAPIServer struct {
	URL         string
	Description string
	Variables   map[string]ServerVariable
}

// ServerVariable represents a variable in a server URL
type ServerVariable struct {
	Default     string
	Enum        []string
	Description string
}

// SecurityScheme defines a security scheme
type SecurityScheme struct {
	Type             string // "apiKey", "http", "oauth2", "openIdConnect"
	Description      string
	Name             string // For apiKey
	In               string // For apiKey: "query", "header", "cookie"
	Scheme           string // For http: "bearer", "basic"
	BearerFormat     string // For http bearer
	Flows            *OAuthFlows
	OpenIdConnectUrl string
}

// OAuthFlows defines OAuth 2.0 flows
type OAuthFlows struct {
	Implicit          *OAuthFlow
	Password          *OAuthFlow
	ClientCredentials *OAuthFlow
	AuthorizationCode *OAuthFlow
}

// OAuthFlow defines a single OAuth 2.0 flow
type OAuthFlow struct {
	AuthorizationURL string
	TokenURL         string
	RefreshURL       string
	Scopes           map[string]string
}

// OpenAPITag represents a tag in the OpenAPI spec
type OpenAPITag struct {
	Name         string
	Description  string
	ExternalDocs *ExternalDocs
}

// ExternalDocs points to external documentation
type ExternalDocs struct {
	Description string
	URL         string
}

// Contact represents contact information
type Contact struct {
	Name  string
	Email string
	URL   string
}

// License represents license information
type License struct {
	Name string
	URL  string
}

// OpenAPISpec represents the complete OpenAPI 3.1.0 specification
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

// Info provides metadata about the API
type Info struct {
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	Version        string   `json:"version"`
	TermsOfService string   `json:"termsOfService,omitempty"`
	Contact        *Contact `json:"contact,omitempty"`
	License        *License `json:"license,omitempty"`
}

// PathItem describes operations available on a single path
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

// Operation describes a single API operation on a path
type Operation struct {
	Tags         []string                        `json:"tags,omitempty"`
	Summary      string                          `json:"summary,omitempty"`
	Description  string                          `json:"description,omitempty"`
	OperationID  string                          `json:"operationId,omitempty"`
	Parameters   []Parameter                     `json:"parameters,omitempty"`
	RequestBody  *RequestBody                    `json:"requestBody,omitempty"`
	Responses    map[string]*Response            `json:"responses"`
	Callbacks    map[string]map[string]*PathItem `json:"callbacks,omitempty"`
	Deprecated   bool                            `json:"deprecated,omitempty"`
	Security     []SecurityRequirement           `json:"security,omitempty"`
	ExternalDocs *ExternalDocs                   `json:"externalDocs,omitempty"`
}

// Parameter describes a single operation parameter
type Parameter struct {
	Name            string              `json:"name"`
	In              string              `json:"in"` // "query", "header", "path", "cookie"
	Description     string              `json:"description,omitempty"`
	Required        bool                `json:"required,omitempty"`
	Deprecated      bool                `json:"deprecated,omitempty"`
	AllowEmptyValue bool                `json:"allowEmptyValue,omitempty"`
	Schema          *Schema             `json:"schema,omitempty"`
	Example         interface{}         `json:"example,omitempty"`
	Examples        map[string]*Example `json:"examples,omitempty"`
}

// RequestBody describes a single request body
type RequestBody struct {
	Description string                `json:"description,omitempty"`
	Content     map[string]*MediaType `json:"content"`
	Required    bool                  `json:"required,omitempty"`
}

// Response describes a single response from an API operation
type Response struct {
	Description string                `json:"description"`
	Headers     map[string]*Header    `json:"headers,omitempty"`
	Content     map[string]*MediaType `json:"content,omitempty"`
	Links       map[string]*Link      `json:"links,omitempty"`
}

// MediaType provides schema and examples for a media type
type MediaType struct {
	Schema   *Schema              `json:"schema,omitempty"`
	Example  interface{}          `json:"example,omitempty"`
	Examples map[string]*Example  `json:"examples,omitempty"`
	Encoding map[string]*Encoding `json:"encoding,omitempty"`
}

// Schema represents a JSON Schema (OpenAPI 3.1.0 uses JSON Schema 2020-12)
type Schema struct {
	Type        string      `json:"type,omitempty"`
	Format      string      `json:"format,omitempty"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Nullable    bool        `json:"nullable,omitempty"`
	ReadOnly    bool        `json:"readOnly,omitempty"`
	WriteOnly   bool        `json:"writeOnly,omitempty"`
	Example     interface{} `json:"example,omitempty"`
	Deprecated  bool        `json:"deprecated,omitempty"`

	// Validation
	MultipleOf       float64       `json:"multipleOf,omitempty"`
	Maximum          float64       `json:"maximum,omitempty"`
	ExclusiveMaximum bool          `json:"exclusiveMaximum,omitempty"`
	Minimum          float64       `json:"minimum,omitempty"`
	ExclusiveMinimum bool          `json:"exclusiveMinimum,omitempty"`
	MaxLength        int           `json:"maxLength,omitempty"`
	MinLength        int           `json:"minLength,omitempty"`
	Pattern          string        `json:"pattern,omitempty"`
	MaxItems         int           `json:"maxItems,omitempty"`
	MinItems         int           `json:"minItems,omitempty"`
	UniqueItems      bool          `json:"uniqueItems,omitempty"`
	MaxProperties    int           `json:"maxProperties,omitempty"`
	MinProperties    int           `json:"minProperties,omitempty"`
	Required         []string      `json:"required,omitempty"`
	Enum             []interface{} `json:"enum,omitempty"`

	// Object/Array properties
	Properties           map[string]*Schema `json:"properties,omitempty"`
	AdditionalProperties interface{}        `json:"additionalProperties,omitempty"`
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

// Discriminator supports polymorphism
type Discriminator struct {
	PropertyName string            `json:"propertyName"`
	Mapping      map[string]string `json:"mapping,omitempty"`
}

// Example provides an example value
type Example struct {
	Summary       string      `json:"summary,omitempty"`
	Description   string      `json:"description,omitempty"`
	Value         interface{} `json:"value,omitempty"`
	ExternalValue string      `json:"externalValue,omitempty"`
}

// Header describes a single header parameter
type Header struct {
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Deprecated  bool        `json:"deprecated,omitempty"`
	Schema      *Schema     `json:"schema,omitempty"`
	Example     interface{} `json:"example,omitempty"`
}

// Link represents a possible design-time link for a response
type Link struct {
	OperationRef string                 `json:"operationRef,omitempty"`
	OperationID  string                 `json:"operationId,omitempty"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	RequestBody  interface{}            `json:"requestBody,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Server       *OpenAPIServer         `json:"server,omitempty"`
}

// Encoding defines encoding for a property
type Encoding struct {
	ContentType   string             `json:"contentType,omitempty"`
	Headers       map[string]*Header `json:"headers,omitempty"`
	Style         string             `json:"style,omitempty"`
	Explode       bool               `json:"explode,omitempty"`
	AllowReserved bool               `json:"allowReserved,omitempty"`
}

// Components holds reusable objects for the API spec
type Components struct {
	Schemas         map[string]*Schema        `json:"schemas,omitempty"`
	Responses       map[string]*Response      `json:"responses,omitempty"`
	Parameters      map[string]*Parameter     `json:"parameters,omitempty"`
	Examples        map[string]*Example       `json:"examples,omitempty"`
	RequestBodies   map[string]*RequestBody   `json:"requestBodies,omitempty"`
	Headers         map[string]*Header        `json:"headers,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	Links           map[string]*Link          `json:"links,omitempty"`
	Callbacks       map[string]interface{}    `json:"callbacks,omitempty"`
}

// SecurityRequirement lists required security schemes
type SecurityRequirement map[string][]string
