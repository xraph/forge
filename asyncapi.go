package forge

import "github.com/xraph/forge/internal/shared"

// AsyncAPIConfig configures AsyncAPI 3.0.0 generation.
type AsyncAPIConfig = shared.AsyncAPIConfig

// AsyncAPISpec represents the complete AsyncAPI 3.0.0 specification.
type AsyncAPISpec = shared.AsyncAPISpec

// AsyncAPIInfo provides metadata about the API.
type AsyncAPIInfo = shared.AsyncAPIInfo

// AsyncAPIServer represents a server in the AsyncAPI spec.
type AsyncAPIServer = shared.AsyncAPIServer

// AsyncAPIServerBindings contains protocol-specific server bindings.
type AsyncAPIServerBindings = shared.AsyncAPIServerBindings

// WebSocketServerBinding represents WebSocket-specific server configuration.
type WebSocketServerBinding = shared.WebSocketServerBinding

// HTTPServerBinding represents HTTP-specific server configuration.
type HTTPServerBinding = shared.HTTPServerBinding

// AsyncAPIChannel represents a channel in the AsyncAPI spec.
type AsyncAPIChannel = shared.AsyncAPIChannel

// AsyncAPIChannelBindings contains protocol-specific channel bindings.
type AsyncAPIChannelBindings = shared.AsyncAPIChannelBindings

// WebSocketChannelBinding represents WebSocket-specific channel configuration.
type WebSocketChannelBinding = shared.WebSocketChannelBinding

// HTTPChannelBinding represents HTTP-specific channel configuration.
type HTTPChannelBinding = shared.HTTPChannelBinding

// AsyncAPIServerReference references a server.
type AsyncAPIServerReference = shared.AsyncAPIServerReference

// AsyncAPIParameter represents a parameter in channel address.
type AsyncAPIParameter = shared.AsyncAPIParameter

// AsyncAPIOperation represents an operation in the AsyncAPI spec.
type AsyncAPIOperation = shared.AsyncAPIOperation

// AsyncAPIChannelReference references a channel.
type AsyncAPIChannelReference = shared.AsyncAPIChannelReference

// AsyncAPIMessageReference references a message.
type AsyncAPIMessageReference = shared.AsyncAPIMessageReference

// AsyncAPIOperationBindings contains protocol-specific operation bindings.
type AsyncAPIOperationBindings = shared.AsyncAPIOperationBindings

// WebSocketOperationBinding represents WebSocket-specific operation configuration.
type WebSocketOperationBinding = shared.WebSocketOperationBinding

// HTTPOperationBinding represents HTTP-specific operation configuration.
type HTTPOperationBinding = shared.HTTPOperationBinding

// AsyncAPIOperationTrait represents reusable operation characteristics.
type AsyncAPIOperationTrait = shared.AsyncAPIOperationTrait

// AsyncAPIOperationReply represents the reply configuration for an operation.
type AsyncAPIOperationReply = shared.AsyncAPIOperationReply

// AsyncAPIOperationReplyAddress represents the reply address.
type AsyncAPIOperationReplyAddress = shared.AsyncAPIOperationReplyAddress

// AsyncAPIMessage represents a message in the AsyncAPI spec.
type AsyncAPIMessage = shared.AsyncAPIMessage

// AsyncAPICorrelationID specifies a correlation ID for request-reply patterns.
type AsyncAPICorrelationID = shared.AsyncAPICorrelationID

// AsyncAPIMessageBindings contains protocol-specific message bindings.
type AsyncAPIMessageBindings = shared.AsyncAPIMessageBindings

// WebSocketMessageBinding represents WebSocket-specific message configuration.
type WebSocketMessageBinding = shared.WebSocketMessageBinding

// HTTPMessageBinding represents HTTP-specific message configuration.
type HTTPMessageBinding = shared.HTTPMessageBinding

// AsyncAPIMessageExample represents an example of a message.
type AsyncAPIMessageExample = shared.AsyncAPIMessageExample

// AsyncAPIMessageTrait represents reusable message characteristics.
type AsyncAPIMessageTrait = shared.AsyncAPIMessageTrait

// AsyncAPIComponents holds reusable objects for the API spec.
type AsyncAPIComponents = shared.AsyncAPIComponents

// AsyncAPISecurityScheme defines a security scheme.
type AsyncAPISecurityScheme = shared.AsyncAPISecurityScheme

// AsyncAPIOAuthFlows defines OAuth 2.0 flows.
type AsyncAPIOAuthFlows = shared.AsyncAPIOAuthFlows

// AsyncAPISecurityRequirement lists required security schemes.
type AsyncAPISecurityRequirement = shared.AsyncAPISecurityRequirement

// AsyncAPITag represents a tag in the AsyncAPI spec.
type AsyncAPITag = shared.AsyncAPITag
