package shared

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// AsyncAPIConfig configures AsyncAPI 3.0.0 generation.
type AsyncAPIConfig struct {
	// Basic info
	Title       string
	Description string
	Version     string

	// AsyncAPI version (default: "3.0.0")
	AsyncAPIVersion string

	// Server configuration
	Servers map[string]*AsyncAPIServer

	// Default content type for messages
	DefaultContentType string // Default: "application/json"

	// External docs, contact, license (reuse OpenAPI types)
	ExternalDocs *ExternalDocs
	Contact      *Contact
	License      *License

	// UI configuration
	UIPath      string // Default: "/asyncapi"
	SpecPath    string // Default: "/asyncapi.json"
	UIEnabled   bool   // Default: true
	SpecEnabled bool   // Default: true

	// Generation options
	PrettyJSON          bool
	IncludeExamples     bool
	IncludeDescriptions bool
}

// AsyncAPISpec represents the complete AsyncAPI 3.0.0 specification.
type AsyncAPISpec struct {
	AsyncAPI   string                        `json:"asyncapi"` // "3.0.0"
	ID         string                        `json:"id,omitempty"`
	Info       AsyncAPIInfo                  `json:"info"`
	Servers    map[string]*AsyncAPIServer    `json:"servers,omitempty"`
	Channels   map[string]*AsyncAPIChannel   `json:"channels"`
	Operations map[string]*AsyncAPIOperation `json:"operations"`
	Components *AsyncAPIComponents           `json:"components,omitempty"`
	Tags       []AsyncAPITag                 `json:"tags,omitempty"`
	Extensions map[string]any                `json:"-"` // x-* extensions
}

// AsyncAPIInfo provides metadata about the API.
type AsyncAPIInfo struct {
	Title          string        `json:"title"`
	Description    string        `json:"description,omitempty"`
	Version        string        `json:"version"`
	TermsOfService string        `json:"termsOfService,omitempty" yaml:"termsOfService,omitempty"`
	Contact        *Contact      `json:"contact,omitempty"`
	License        *License      `json:"license,omitempty"`
	Tags           []AsyncAPITag `json:"tags,omitempty"`
	ExternalDocs   *ExternalDocs `json:"externalDocs,omitempty"   yaml:"externalDocs,omitempty"`
}

// AsyncAPIServer represents a server in the AsyncAPI spec.
type AsyncAPIServer struct {
	Host            string                        `json:"host,omitempty"`
	Protocol        string                        `json:"protocol"` // ws, wss, sse, http, https
	ProtocolVersion string                        `json:"protocolVersion,omitempty" yaml:"protocolVersion,omitempty"`
	Pathname        string                        `json:"pathname,omitempty"`
	Description     string                        `json:"description,omitempty"`
	Title           string                        `json:"title,omitempty"`
	Summary         string                        `json:"summary,omitempty"`
	Variables       map[string]*ServerVariable    `json:"variables,omitempty"`
	Security        []AsyncAPISecurityRequirement `json:"security,omitempty"`
	Tags            []AsyncAPITag                 `json:"tags,omitempty"`
	ExternalDocs    *ExternalDocs                 `json:"externalDocs,omitempty"    yaml:"externalDocs,omitempty"`
	Bindings        *AsyncAPIServerBindings       `json:"bindings,omitempty"`
}

// AsyncAPIServerBindings contains protocol-specific server bindings.
type AsyncAPIServerBindings struct {
	WS   *WebSocketServerBinding `json:"ws,omitempty"`
	HTTP *HTTPServerBinding      `json:"http,omitempty"`
}

// WebSocketServerBinding represents WebSocket-specific server configuration.
type WebSocketServerBinding struct {
	Headers        *Schema `json:"headers,omitempty"`
	Query          *Schema `json:"query,omitempty"`
	BindingVersion string  `json:"bindingVersion,omitempty" yaml:"bindingVersion,omitempty"`
}

// HTTPServerBinding represents HTTP-specific server configuration.
type HTTPServerBinding struct {
	BindingVersion string `json:"bindingVersion,omitempty" yaml:"bindingVersion,omitempty"`
}

// AsyncAPIChannel represents a channel in the AsyncAPI spec.
type AsyncAPIChannel struct {
	Address      string                        `json:"address,omitempty"` // Channel address (can include params)
	Messages     map[string]*AsyncAPIMessage   `json:"messages,omitempty"`
	Title        string                        `json:"title,omitempty"`
	Summary      string                        `json:"summary,omitempty"`
	Description  string                        `json:"description,omitempty"`
	Servers      []AsyncAPIServerReference     `json:"servers,omitempty"`
	Parameters   map[string]*AsyncAPIParameter `json:"parameters,omitempty"`
	Tags         []AsyncAPITag                 `json:"tags,omitempty"`
	ExternalDocs *ExternalDocs                 `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	Bindings     *AsyncAPIChannelBindings      `json:"bindings,omitempty"`

	// Extensions carries x-* specification extensions. They are hoisted to the
	// top level of this object on marshal, per the AsyncAPI specification, rather
	// than nesting under a literal "Extensions" key.
	Extensions map[string]any `json:"-" yaml:"-"`
}

// MarshalJSON writes the channel with its x-* extensions hoisted to the top level.
//
// The local alias sheds this method, so json.Marshal below does not recurse into
// MarshalJSON forever. Marshalling through the alias rather than enumerating fields by
// hand means a field added to AsyncAPIChannel later is carried automatically; a
// hand-written marshaller would drop it silently.
func (c AsyncAPIChannel) MarshalJSON() ([]byte, error) {
	type alias AsyncAPIChannel

	base, err := json.Marshal(alias(c))
	if err != nil {
		return nil, err
	}

	// No extensions: return the ordinary encoding untouched, so extension-free
	// documents are byte-identical to what this type produced before.
	if len(c.Extensions) == 0 {
		return base, nil
	}

	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}

	for key, value := range c.Extensions {
		// Only x- keys are hoisted. Without this guard a caller could put "address"
		// in the map and overwrite a real channel field.
		if !strings.HasPrefix(key, "x-") {
			continue
		}

		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}

		merged[key] = raw
	}

	return json.Marshal(merged)
}

// UnmarshalJSON reads x-* keys back out of the top level into Extensions.
func (c *AsyncAPIChannel) UnmarshalJSON(data []byte) error {
	type alias AsyncAPIChannel

	var base alias
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}

	*c = AsyncAPIChannel(base)

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}

	for key, raw := range all {
		if !strings.HasPrefix(key, "x-") {
			continue
		}

		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}

		if c.Extensions == nil {
			c.Extensions = make(map[string]any)
		}

		c.Extensions[key] = value
	}

	return nil
}

// MarshalYAML writes the channel with its x-* extensions hoisted to the top level.
//
// yaml.v3 does not consult MarshalJSON, so this is the YAML counterpart of the
// method above and not a duplicate of it. The local alias sheds this method, so
// encoding it does not recurse into MarshalYAML forever.
func (c AsyncAPIChannel) MarshalYAML() (any, error) {
	type alias AsyncAPIChannel

	return marshalYAMLWithExtensions(alias(c), c.Extensions)
}

// UnmarshalYAML reads x-* keys back out of the top level into Extensions.
func (c *AsyncAPIChannel) UnmarshalYAML(value *yaml.Node) error {
	type alias AsyncAPIChannel

	var base alias
	if err := value.Decode(&base); err != nil {
		return err
	}

	*c = AsyncAPIChannel(base)

	extensions, err := unmarshalYAMLExtensions(value)
	if err != nil {
		return err
	}

	c.Extensions = extensions

	return nil
}

// AsyncAPIChannelBindings contains protocol-specific channel bindings.
type AsyncAPIChannelBindings struct {
	WS   *WebSocketChannelBinding `json:"ws,omitempty"`
	HTTP *HTTPChannelBinding      `json:"http,omitempty"`
}

// WebSocketChannelBinding represents WebSocket-specific channel configuration.
type WebSocketChannelBinding struct {
	Method         string  `json:"method,omitempty"` // GET, POST
	Query          *Schema `json:"query,omitempty"`
	Headers        *Schema `json:"headers,omitempty"`
	BindingVersion string  `json:"bindingVersion,omitempty" yaml:"bindingVersion,omitempty"`
}

// HTTPChannelBinding represents HTTP-specific channel configuration.
type HTTPChannelBinding struct {
	Method         string `json:"method,omitempty"` // GET, POST, etc.
	BindingVersion string `json:"bindingVersion,omitempty" yaml:"bindingVersion,omitempty"`
}

// AsyncAPIServerReference references a server.
type AsyncAPIServerReference struct {
	Ref string `json:"$ref" yaml:"$ref"` // #/servers/serverName
}

// AsyncAPIParameter represents a parameter in channel address.
type AsyncAPIParameter struct {
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     string   `json:"default,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	Location    string   `json:"location,omitempty"` // $message.header, $message.payload
	Schema      *Schema  `json:"schema,omitempty"`
}

// AsyncAPIOperation represents an operation in the AsyncAPI spec.
type AsyncAPIOperation struct {
	Action       string                        `json:"action"` // send, receive
	Channel      *AsyncAPIChannelReference     `json:"channel"`
	Title        string                        `json:"title,omitempty"`
	Summary      string                        `json:"summary,omitempty"`
	Description  string                        `json:"description,omitempty"`
	Security     []AsyncAPISecurityRequirement `json:"security,omitempty"`
	Tags         []AsyncAPITag                 `json:"tags,omitempty"`
	ExternalDocs *ExternalDocs                 `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	Bindings     *AsyncAPIOperationBindings    `json:"bindings,omitempty"`
	Traits       []AsyncAPIOperationTrait      `json:"traits,omitempty"`
	Messages     []AsyncAPIMessageReference    `json:"messages,omitempty"`
	Reply        *AsyncAPIOperationReply       `json:"reply,omitempty"`
}

// AsyncAPIChannelReference references a channel.
type AsyncAPIChannelReference struct {
	Ref string `json:"$ref" yaml:"$ref"` // #/channels/channelName
}

// AsyncAPIMessageReference references a message.
type AsyncAPIMessageReference struct {
	Ref string `json:"$ref" yaml:"$ref"` // #/components/messages/messageName or #/channels/channelName/messages/messageName
}

// AsyncAPIOperationBindings contains protocol-specific operation bindings.
type AsyncAPIOperationBindings struct {
	WS   *WebSocketOperationBinding `json:"ws,omitempty"`
	HTTP *HTTPOperationBinding      `json:"http,omitempty"`
}

// WebSocketOperationBinding represents WebSocket-specific operation configuration.
type WebSocketOperationBinding struct {
	BindingVersion string `json:"bindingVersion,omitempty" yaml:"bindingVersion,omitempty"`
}

// HTTPOperationBinding represents HTTP-specific operation configuration.
type HTTPOperationBinding struct {
	Method         string  `json:"method,omitempty"`
	Query          *Schema `json:"query,omitempty"`
	BindingVersion string  `json:"bindingVersion,omitempty" yaml:"bindingVersion,omitempty"`
}

// AsyncAPIOperationTrait represents reusable operation characteristics.
type AsyncAPIOperationTrait struct {
	Title        string                        `json:"title,omitempty"`
	Summary      string                        `json:"summary,omitempty"`
	Description  string                        `json:"description,omitempty"`
	Security     []AsyncAPISecurityRequirement `json:"security,omitempty"`
	Tags         []AsyncAPITag                 `json:"tags,omitempty"`
	ExternalDocs *ExternalDocs                 `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	Bindings     *AsyncAPIOperationBindings    `json:"bindings,omitempty"`
}

// AsyncAPIOperationReply represents the reply configuration for an operation.
type AsyncAPIOperationReply struct {
	Address  *AsyncAPIOperationReplyAddress `json:"address,omitempty"`
	Channel  *AsyncAPIChannelReference      `json:"channel,omitempty"`
	Messages []AsyncAPIMessageReference     `json:"messages,omitempty"`
}

// AsyncAPIOperationReplyAddress represents the reply address.
type AsyncAPIOperationReplyAddress struct {
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"` // Runtime expression like $message.header#/replyTo
}

// AsyncAPIMessage represents a message in the AsyncAPI spec.
type AsyncAPIMessage struct {
	MessageID     string                   `json:"messageId,omitempty"     yaml:"messageId,omitempty"`
	Headers       *Schema                  `json:"headers,omitempty"`
	Payload       *Schema                  `json:"payload,omitempty"`
	CorrelationID *AsyncAPICorrelationID   `json:"correlationId,omitempty" yaml:"correlationId,omitempty"`
	ContentType   string                   `json:"contentType,omitempty"   yaml:"contentType,omitempty"`
	Name          string                   `json:"name,omitempty"`
	Title         string                   `json:"title,omitempty"`
	Summary       string                   `json:"summary,omitempty"`
	Description   string                   `json:"description,omitempty"`
	Tags          []AsyncAPITag            `json:"tags,omitempty"`
	ExternalDocs  *ExternalDocs            `json:"externalDocs,omitempty"  yaml:"externalDocs,omitempty"`
	Bindings      *AsyncAPIMessageBindings `json:"bindings,omitempty"`
	Examples      []AsyncAPIMessageExample `json:"examples,omitempty"`
	Traits        []AsyncAPIMessageTrait   `json:"traits,omitempty"`
}

// AsyncAPICorrelationID specifies a correlation ID for request-reply patterns.
type AsyncAPICorrelationID struct {
	Description string `json:"description,omitempty"`
	Location    string `json:"location"` // Runtime expression like $message.header#/correlationId
}

// AsyncAPIMessageBindings contains protocol-specific message bindings.
type AsyncAPIMessageBindings struct {
	WS   *WebSocketMessageBinding `json:"ws,omitempty"`
	HTTP *HTTPMessageBinding      `json:"http,omitempty"`
}

// WebSocketMessageBinding represents WebSocket-specific message configuration.
type WebSocketMessageBinding struct {
	BindingVersion string `json:"bindingVersion,omitempty" yaml:"bindingVersion,omitempty"`
}

// HTTPMessageBinding represents HTTP-specific message configuration.
type HTTPMessageBinding struct {
	Headers        *Schema `json:"headers,omitempty"`
	StatusCode     int     `json:"statusCode,omitempty"     yaml:"statusCode,omitempty"`
	BindingVersion string  `json:"bindingVersion,omitempty" yaml:"bindingVersion,omitempty"`
}

// AsyncAPIMessageExample represents an example of a message.
type AsyncAPIMessageExample struct {
	Name    string         `json:"name,omitempty"`
	Summary string         `json:"summary,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
	Payload any            `json:"payload,omitempty"`
}

// AsyncAPIMessageTrait represents reusable message characteristics.
type AsyncAPIMessageTrait struct {
	MessageID     string                   `json:"messageId,omitempty"     yaml:"messageId,omitempty"`
	Headers       *Schema                  `json:"headers,omitempty"`
	CorrelationID *AsyncAPICorrelationID   `json:"correlationId,omitempty" yaml:"correlationId,omitempty"`
	ContentType   string                   `json:"contentType,omitempty"   yaml:"contentType,omitempty"`
	Name          string                   `json:"name,omitempty"`
	Title         string                   `json:"title,omitempty"`
	Summary       string                   `json:"summary,omitempty"`
	Description   string                   `json:"description,omitempty"`
	Tags          []AsyncAPITag            `json:"tags,omitempty"`
	ExternalDocs  *ExternalDocs            `json:"externalDocs,omitempty"  yaml:"externalDocs,omitempty"`
	Bindings      *AsyncAPIMessageBindings `json:"bindings,omitempty"`
	Examples      []AsyncAPIMessageExample `json:"examples,omitempty"`
}

// AsyncAPIComponents holds reusable objects for the API spec.
type AsyncAPIComponents struct {
	Schemas           map[string]*Schema                    `json:"schemas,omitempty"`
	Servers           map[string]*AsyncAPIServer            `json:"servers,omitempty"`
	Channels          map[string]*AsyncAPIChannel           `json:"channels,omitempty"`
	Operations        map[string]*AsyncAPIOperation         `json:"operations,omitempty"`
	Messages          map[string]*AsyncAPIMessage           `json:"messages,omitempty"`
	SecuritySchemes   map[string]*AsyncAPISecurityScheme    `json:"securitySchemes,omitempty"   yaml:"securitySchemes,omitempty"`
	Parameters        map[string]*AsyncAPIParameter         `json:"parameters,omitempty"`
	CorrelationIDs    map[string]*AsyncAPICorrelationID     `json:"correlationIds,omitempty"    yaml:"correlationIds,omitempty"`
	OperationTraits   map[string]*AsyncAPIOperationTrait    `json:"operationTraits,omitempty"   yaml:"operationTraits,omitempty"`
	MessageTraits     map[string]*AsyncAPIMessageTrait      `json:"messageTraits,omitempty"     yaml:"messageTraits,omitempty"`
	ServerBindings    map[string]*AsyncAPIServerBindings    `json:"serverBindings,omitempty"    yaml:"serverBindings,omitempty"`
	ChannelBindings   map[string]*AsyncAPIChannelBindings   `json:"channelBindings,omitempty"   yaml:"channelBindings,omitempty"`
	OperationBindings map[string]*AsyncAPIOperationBindings `json:"operationBindings,omitempty" yaml:"operationBindings,omitempty"`
	MessageBindings   map[string]*AsyncAPIMessageBindings   `json:"messageBindings,omitempty"   yaml:"messageBindings,omitempty"`
}

// AsyncAPISecurityScheme defines a security scheme.
type AsyncAPISecurityScheme struct {
	Type             string              `json:"type"` // userPassword, apiKey, X509, symmetricEncryption, asymmetricEncryption, httpApiKey, http, oauth2, openIdConnect
	Description      string              `json:"description,omitempty"`
	Name             string              `json:"name,omitempty"`                                               // For apiKey and httpApiKey
	In               string              `json:"in,omitempty"`                                                 // For apiKey and httpApiKey: user, password, query, header, cookie
	Scheme           string              `json:"scheme,omitempty"`                                             // For http: bearer, basic, etc.
	BearerFormat     string              `json:"bearerFormat,omitempty"     yaml:"bearerFormat,omitempty"`     // For http bearer
	Flows            *AsyncAPIOAuthFlows `json:"flows,omitempty"`                                              // For oauth2
	OpenIdConnectUrl string              `json:"openIdConnectUrl,omitempty" yaml:"openIdConnectUrl,omitempty"` // For openIdConnect
	Scopes           []string            `json:"scopes,omitempty"`
}

// AsyncAPIOAuthFlows defines OAuth 2.0 flows (compatible with OpenAPI OAuthFlows).
type AsyncAPIOAuthFlows struct {
	Implicit          *OAuthFlow `json:"implicit,omitempty"          yaml:"implicit,omitempty"`
	Password          *OAuthFlow `json:"password,omitempty"          yaml:"password,omitempty"`
	ClientCredentials *OAuthFlow `json:"clientCredentials,omitempty" yaml:"clientCredentials,omitempty"`
	AuthorizationCode *OAuthFlow `json:"authorizationCode,omitempty" yaml:"authorizationCode,omitempty"`
}

// AsyncAPISecurityRequirement lists required security schemes.
type AsyncAPISecurityRequirement map[string][]string

// AsyncAPITag represents a tag in the AsyncAPI spec.
type AsyncAPITag struct {
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	ExternalDocs *ExternalDocs `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
}
