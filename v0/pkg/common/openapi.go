package common

import (
	"reflect"
)

// =============================================================================
// OPENAPI TYPES
// =============================================================================

// OpenAPIConfig contains configuration for OpenAPI generation
type OpenAPIConfig struct {
	Title          string
	Description    string
	Version        string
	TermsOfService string
	Contact        *ContactObject
	License        *LicenseObject
	Servers        []*ServerObject
	AutoUpdate     bool
	EnableUI       bool
	UIPath         string
	SpecPath       string
	Security       []SecurityRequirement
	Tags           []*TagObject
}

// ContactObject represents contact information
type ContactObject struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

// LicenseObject represents license information
type LicenseObject struct {
	Name       string `json:"name"`
	Identifier string `json:"identifier,omitempty"`
	URL        string `json:"url,omitempty"`
}

// ServerObject represents a server
type ServerObject struct {
	URL         string                     `json:"url"`
	Description string                     `json:"description,omitempty"`
	Variables   map[string]*ServerVariable `json:"variables,omitempty"`
}

// ServerVariable represents a server variable
type ServerVariable struct {
	Enum        []string `json:"enum,omitempty"`
	Default     string   `json:"default"`
	Description string   `json:"description,omitempty"`
}

// TagObject represents an OpenAPI tag
type TagObject struct {
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	ExternalDocs *ExternalDocsObject `json:"externalDocs,omitempty"`
}

// ExternalDocsObject represents external documentation
type ExternalDocsObject struct {
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
}

// SecurityRequirement represents a security requirement
type SecurityRequirement map[string][]string

// OpenAPISpec represents the OpenAPI specification
type OpenAPISpec struct {
	OpenAPI      string                `json:"openapi"`
	Info         *InfoObject           `json:"info"`
	Servers      []*ServerObject       `json:"servers,omitempty"`
	Paths        map[string]*PathItem  `json:"paths"`
	Components   *ComponentsObject     `json:"components,omitempty"`
	Security     []SecurityRequirement `json:"security,omitempty"`
	Tags         []*TagObject          `json:"tags,omitempty"`
	ExternalDocs *ExternalDocsObject   `json:"externalDocs,omitempty"`
}

// InfoObject represents the OpenAPI info object
type InfoObject struct {
	Title          string         `json:"title"`
	Summary        string         `json:"summary,omitempty"`
	Description    string         `json:"description,omitempty"`
	TermsOfService string         `json:"termsOfService,omitempty"`
	Contact        *ContactObject `json:"contact,omitempty"`
	License        *LicenseObject `json:"license,omitempty"`
	Version        string         `json:"version"`
}

// PathItem represents a path item in the OpenAPI spec
type PathItem struct {
	Ref         string          `json:"$ref,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Description string          `json:"description,omitempty"`
	Get         *Operation      `json:"get,omitempty"`
	Put         *Operation      `json:"put,omitempty"`
	Post        *Operation      `json:"post,omitempty"`
	Delete      *Operation      `json:"delete,omitempty"`
	Options     *Operation      `json:"options,omitempty"`
	Head        *Operation      `json:"head,omitempty"`
	Patch       *Operation      `json:"patch,omitempty"`
	Trace       *Operation      `json:"trace,omitempty"`
	Servers     []*ServerObject `json:"servers,omitempty"`
	Parameters  []*Parameter    `json:"parameters,omitempty"`
}

// Operation represents an OpenAPI operation
type Operation struct {
	Tags         []string              `json:"tags,omitempty"`
	Summary      string                `json:"summary,omitempty"`
	Description  string                `json:"description,omitempty"`
	ExternalDocs *ExternalDocsObject   `json:"externalDocs,omitempty"`
	OperationID  string                `json:"operationId,omitempty"`
	Parameters   []*Parameter          `json:"parameters,omitempty"`
	RequestBody  *RequestBody          `json:"requestBody,omitempty"`
	Responses    map[string]*Response  `json:"responses"`
	Callbacks    map[string]*Callback  `json:"callbacks,omitempty"`
	Deprecated   bool                  `json:"deprecated,omitempty"`
	Security     []SecurityRequirement `json:"security,omitempty"`
	Servers      []*ServerObject       `json:"servers,omitempty"`
}

// Parameter represents an OpenAPI parameter
type Parameter struct {
	Name            string                `json:"name"`
	In              string                `json:"in"`
	Description     string                `json:"description,omitempty"`
	Required        bool                  `json:"required,omitempty"`
	Deprecated      bool                  `json:"deprecated,omitempty"`
	AllowEmptyValue bool                  `json:"allowEmptyValue,omitempty"`
	Style           string                `json:"style,omitempty"`
	Explode         *bool                 `json:"explode,omitempty"`
	AllowReserved   bool                  `json:"allowReserved,omitempty"`
	Schema          *Schema               `json:"schema,omitempty"`
	Example         interface{}           `json:"example,omitempty"`
	Examples        map[string]*Example   `json:"examples,omitempty"`
	Content         map[string]*MediaType `json:"content,omitempty"`
}

// RequestBody represents an OpenAPI request body
type RequestBody struct {
	Description string                `json:"description,omitempty"`
	Content     map[string]*MediaType `json:"content"`
	Required    bool                  `json:"required,omitempty"`
}

// Response represents an OpenAPI response
type Response struct {
	Description string                `json:"description"`
	Headers     map[string]*Header    `json:"headers,omitempty"`
	Content     map[string]*MediaType `json:"content,omitempty"`
	Links       map[string]*Link      `json:"links,omitempty"`
}

// MediaType represents a media type
type MediaType struct {
	Schema   *Schema              `json:"schema,omitempty"`
	Example  interface{}          `json:"example,omitempty"`
	Examples map[string]*Example  `json:"examples,omitempty"`
	Encoding map[string]*Encoding `json:"encoding,omitempty"`
}

// Schema represents an OpenAPI schema
type Schema struct {
	// Core schema properties
	Type        string        `json:"type,omitempty"`
	Title       string        `json:"title,omitempty"`
	Description string        `json:"description,omitempty"`
	Format      string        `json:"format,omitempty"`
	Default     interface{}   `json:"default,omitempty"`
	Example     interface{}   `json:"example,omitempty"`
	Examples    []interface{} `json:"examples,omitempty"`

	// Validation properties
	MultipleOf       *float64      `json:"multipleOf,omitempty"`
	Maximum          *float64      `json:"maximum,omitempty"`
	ExclusiveMaximum *float64      `json:"exclusiveMaximum,omitempty"`
	Minimum          *float64      `json:"minimum,omitempty"`
	ExclusiveMinimum *float64      `json:"exclusiveMinimum,omitempty"`
	MaxLength        *int          `json:"maxLength,omitempty"`
	MinLength        *int          `json:"minLength,omitempty"`
	Pattern          string        `json:"pattern,omitempty"`
	MaxItems         *int          `json:"maxItems,omitempty"`
	MinItems         *int          `json:"minItems,omitempty"`
	UniqueItems      bool          `json:"uniqueItems,omitempty"`
	MaxProperties    *int          `json:"maxProperties,omitempty"`
	MinProperties    *int          `json:"minProperties,omitempty"`
	Required         []string      `json:"required,omitempty"`
	Enum             []interface{} `json:"enum,omitempty"`

	// Object properties
	Properties           map[string]*Schema `json:"properties,omitempty"`
	AdditionalProperties interface{}        `json:"additionalProperties,omitempty"`
	PropertyNames        *Schema            `json:"propertyNames,omitempty"`

	// Array properties
	Items    *Schema `json:"items,omitempty"`
	Contains *Schema `json:"contains,omitempty"`

	// Composition
	AllOf []*Schema `json:"allOf,omitempty"`
	OneOf []*Schema `json:"oneOf,omitempty"`
	AnyOf []*Schema `json:"anyOf,omitempty"`
	Not   *Schema   `json:"not,omitempty"`

	// References
	Ref string `json:"$ref,omitempty"`

	// OpenAPI specific
	Discriminator *Discriminator         `json:"discriminator,omitempty"`
	ReadOnly      bool                   `json:"readOnly,omitempty"`
	WriteOnly     bool                   `json:"writeOnly,omitempty"`
	Deprecated    bool                   `json:"deprecated,omitempty"`
	XML           *XMLObject             `json:"xml,omitempty"`
	Extensions    map[string]interface{} `json:"extensions,omitempty"`
	ExternalDocs  *ExternalDocsObject    `json:"externalDocs,omitempty"`
}

type Discriminator struct {
	PropertyName string            `json:"propertyName"`
	Mapping      map[string]string `json:"mapping,omitempty"`
}

type XMLObject struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	Attribute bool   `json:"attribute,omitempty"`
	Wrapped   bool   `json:"wrapped,omitempty"`
}

// Supporting types
type Example struct {
	Summary       string      `json:"summary,omitempty"`
	Description   string      `json:"description,omitempty"`
	Value         interface{} `json:"value,omitempty"`
	ExternalValue string      `json:"externalValue,omitempty"`
}

type Header struct {
	Description     string                `json:"description,omitempty"`
	Required        bool                  `json:"required,omitempty"`
	Deprecated      bool                  `json:"deprecated,omitempty"`
	AllowEmptyValue bool                  `json:"allowEmptyValue,omitempty"`
	Style           string                `json:"style,omitempty"`
	Explode         *bool                 `json:"explode,omitempty"`
	AllowReserved   bool                  `json:"allowReserved,omitempty"`
	Schema          *Schema               `json:"schema,omitempty"`
	Example         interface{}           `json:"example,omitempty"`
	Examples        map[string]*Example   `json:"examples,omitempty"`
	Content         map[string]*MediaType `json:"content,omitempty"`
}

type Link struct {
	OperationRef string                 `json:"operationRef,omitempty"`
	OperationID  string                 `json:"operationId,omitempty"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	RequestBody  interface{}            `json:"requestBody,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Server       *ServerObject          `json:"server,omitempty"`
}

type Encoding struct {
	ContentType   string             `json:"contentType,omitempty"`
	Headers       map[string]*Header `json:"headers,omitempty"`
	Style         string             `json:"style,omitempty"`
	Explode       *bool              `json:"explode,omitempty"`
	AllowReserved bool               `json:"allowReserved,omitempty"`
}

type Callback struct {
	Extensions map[string]interface{} `json:"-"`
}

type ComponentsObject struct {
	Schemas         map[string]*Schema         `json:"schemas,omitempty"`
	Responses       map[string]*Response       `json:"responses,omitempty"`
	Parameters      map[string]*Parameter      `json:"parameters,omitempty"`
	Examples        map[string]*Example        `json:"examples,omitempty"`
	RequestBodies   map[string]*RequestBody    `json:"requestBodies,omitempty"`
	Headers         map[string]*Header         `json:"headers,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes,omitempty"`
	Links           map[string]*Link           `json:"links,omitempty"`
	Callbacks       map[string]*Callback       `json:"callbacks,omitempty"`
	PathItems       map[string]*PathItem       `json:"pathItems,omitempty"`
}

type SecurityScheme struct {
	Type             string      `json:"type"`
	Description      string      `json:"description,omitempty"`
	Name             string      `json:"name,omitempty"`
	In               string      `json:"in,omitempty"`
	Scheme           string      `json:"scheme,omitempty"`
	BearerFormat     string      `json:"bearerFormat,omitempty"`
	Flows            *OAuthFlows `json:"flows,omitempty"`
	OpenIDConnectURL string      `json:"openIdConnectUrl,omitempty"`
}

type OAuthFlows struct {
	Implicit          *OAuthFlow `json:"implicit,omitempty"`
	Password          *OAuthFlow `json:"password,omitempty"`
	ClientCredentials *OAuthFlow `json:"clientCredentials,omitempty"`
	AuthorizationCode *OAuthFlow `json:"authorizationCode,omitempty"`
}

type OAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"`
	RefreshURL       string            `json:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes"`
}

// SchemaDefinition represents a reusable schema definition
type SchemaDefinition struct {
	Name     string
	Schema   *Schema
	Type     reflect.Type
	Examples []interface{}
}

// FieldTagInfo contains all parsed tag information for a field
type FieldTagInfo struct {
	// Parameter location tags
	Path      string
	Query     string
	Header    string
	Cookie    string
	Body      bool
	JSON      string
	XML       string
	Form      string
	Multipart string

	// Validation tags
	Required   bool
	Min        *float64
	Max        *float64
	MinLength  *int
	MaxLength  *int
	Pattern    string
	Enum       []interface{}
	MultipleOf *float64

	// Documentation tags
	Description string
	Example     interface{}
	Deprecated  bool
	Title       string

	// Format and type tags
	Format     string
	Type       string
	Default    interface{}
	AllowEmpty bool

	// Custom OpenAPI extensions
	Extensions map[string]interface{}
}
