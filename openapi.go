package forge

import "github.com/xraph/forge/internal/shared"

// OpenAPIConfig configures OpenAPI 3.1.0 generation
type OpenAPIConfig = shared.OpenAPIConfig

// OpenAPIServer represents a server in the OpenAPI spec
type OpenAPIServer = shared.OpenAPIServer

// SecurityScheme defines a security scheme
type SecurityScheme = shared.SecurityScheme

// OAuthFlows defines OAuth 2.0 flows
type OAuthFlows = shared.OAuthFlows

// OAuthFlow defines a single OAuth 2.0 flow
type OAuthFlow = shared.OAuthFlow

// OpenAPITag represents a tag in the OpenAPI spec
type OpenAPITag = shared.OpenAPITag

// ExternalDocs points to external documentation
type ExternalDocs = shared.ExternalDocs

// Contact represents contact information
type Contact = shared.Contact

// License represents license information
type License = shared.License

// OpenAPISpec represents the complete OpenAPI 3.1.0 specification
type OpenAPISpec = shared.OpenAPISpec

// Info provides metadata about the API
type Info = shared.Info

// PathItem describes operations available on a single path
type PathItem = shared.PathItem

// Operation describes a single API operation on a path
type Operation = shared.Operation

// Parameter describes a single operation parameter
type Parameter = shared.Parameter

// RequestBody describes a single request body
type RequestBody = shared.RequestBody

// Response describes a single response from an API operation
type Response = shared.Response

// MediaType provides schema and examples for a media type
type MediaType = shared.MediaType

// Schema represents a JSON Schema (OpenAPI 3.1.0 uses JSON Schema 2020-12)
type Schema = shared.Schema

// Discriminator supports polymorphism
type Discriminator = shared.Discriminator

// Example provides an example value
type Example = shared.Example

// Header describes a single header parameter
type Header = shared.Header

// Link represents a possible design-time link for a response
type Link = shared.Link

// Encoding defines encoding for a property
type Encoding = shared.Encoding

// Components holds reusable objects for the API spec
type Components = shared.Components

// SecurityRequirement lists required security schemes
type SecurityRequirement = shared.SecurityRequirement
