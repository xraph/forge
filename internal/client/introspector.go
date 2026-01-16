package client

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/xraph/forge/internal/router"
	"github.com/xraph/forge/internal/shared"
)

// Introspector extracts API specification from a Forge Router.
type Introspector struct {
	router router.Router
}

// NewIntrospector creates a new introspector for a router.
func NewIntrospector(r router.Router) *Introspector {
	return &Introspector{router: r}
}

// Introspect extracts the complete API specification from the router.
func (i *Introspector) Introspect(ctx context.Context) (*APISpec, error) {
	spec := &APISpec{
		Schemas:  make(map[string]*Schema),
		Security: []SecurityScheme{},
	}

	// Extract from OpenAPI spec if available
	openAPISpec := i.router.OpenAPISpec()
	if openAPISpec != nil {
		if err := i.extractFromOpenAPI(spec, openAPISpec); err != nil {
			return nil, fmt.Errorf("extract from OpenAPI: %w", err)
		}
	}

	// Extract from AsyncAPI spec if available
	asyncAPISpec := i.router.AsyncAPISpec()
	if asyncAPISpec != nil {
		if err := i.extractFromAsyncAPI(spec, asyncAPISpec); err != nil {
			return nil, fmt.Errorf("extract from AsyncAPI: %w", err)
		}
	}

	// Extract from raw routes if specs are not available
	if openAPISpec == nil {
		routes := i.router.Routes()
		for _, route := range routes {
			endpoint := i.routeToEndpoint(route)
			spec.Endpoints = append(spec.Endpoints, endpoint)
		}
	}

	return spec, nil
}

// extractFromOpenAPI extracts REST endpoints from OpenAPI spec.
func (i *Introspector) extractFromOpenAPI(spec *APISpec, openAPI *shared.OpenAPISpec) error {
	// Extract API info
	spec.Info = APIInfo{
		Title:       openAPI.Info.Title,
		Version:     openAPI.Info.Version,
		Description: openAPI.Info.Description,
	}

	if openAPI.Info.Contact != nil {
		spec.Info.Contact = &Contact{
			Name:  openAPI.Info.Contact.Name,
			URL:   openAPI.Info.Contact.URL,
			Email: openAPI.Info.Contact.Email,
		}
	}

	if openAPI.Info.License != nil {
		spec.Info.License = &License{
			Name: openAPI.Info.License.Name,
			URL:  openAPI.Info.License.URL,
		}
	}

	// Extract servers
	for _, srv := range openAPI.Servers {
		server := Server{
			URL:         srv.URL,
			Description: srv.Description,
			Variables:   make(map[string]ServerVariable),
		}
		for k, v := range srv.Variables {
			server.Variables[k] = ServerVariable{
				Default:     v.Default,
				Description: v.Description,
				Enum:        v.Enum,
			}
		}

		spec.Servers = append(spec.Servers, server)
	}

	// Extract security schemes
	if openAPI.Components != nil && openAPI.Components.SecuritySchemes != nil {
		for name, scheme := range openAPI.Components.SecuritySchemes {
			secScheme := SecurityScheme{
				Type:             scheme.Type,
				Name:             name,
				Description:      scheme.Description,
				In:               scheme.In,
				Scheme:           scheme.Scheme,
				BearerFormat:     scheme.BearerFormat,
				OpenIDConnectURL: scheme.OpenIdConnectUrl,
			}

			if scheme.Flows != nil {
				secScheme.Flows = &OAuthFlows{}
				if scheme.Flows.Implicit != nil {
					secScheme.Flows.Implicit = &OAuthFlow{
						AuthorizationURL: scheme.Flows.Implicit.AuthorizationURL,
						TokenURL:         scheme.Flows.Implicit.TokenURL,
						RefreshURL:       scheme.Flows.Implicit.RefreshURL,
						Scopes:           scheme.Flows.Implicit.Scopes,
					}
				}

				if scheme.Flows.Password != nil {
					secScheme.Flows.Password = &OAuthFlow{
						AuthorizationURL: scheme.Flows.Password.AuthorizationURL,
						TokenURL:         scheme.Flows.Password.TokenURL,
						RefreshURL:       scheme.Flows.Password.RefreshURL,
						Scopes:           scheme.Flows.Password.Scopes,
					}
				}

				if scheme.Flows.ClientCredentials != nil {
					secScheme.Flows.ClientCredentials = &OAuthFlow{
						AuthorizationURL: scheme.Flows.ClientCredentials.AuthorizationURL,
						TokenURL:         scheme.Flows.ClientCredentials.TokenURL,
						RefreshURL:       scheme.Flows.ClientCredentials.RefreshURL,
						Scopes:           scheme.Flows.ClientCredentials.Scopes,
					}
				}

				if scheme.Flows.AuthorizationCode != nil {
					secScheme.Flows.AuthorizationCode = &OAuthFlow{
						AuthorizationURL: scheme.Flows.AuthorizationCode.AuthorizationURL,
						TokenURL:         scheme.Flows.AuthorizationCode.TokenURL,
						RefreshURL:       scheme.Flows.AuthorizationCode.RefreshURL,
						Scopes:           scheme.Flows.AuthorizationCode.Scopes,
					}
				}
			}

			spec.Security = append(spec.Security, secScheme)
		}
	}

	// Extract schemas
	if openAPI.Components != nil && openAPI.Components.Schemas != nil {
		for name, schema := range openAPI.Components.Schemas {
			spec.Schemas[name] = i.convertSchema(schema)
		}
	}

	// Extract tags
	for _, tag := range openAPI.Tags {
		spec.Tags = append(spec.Tags, Tag{
			Name:        tag.Name,
			Description: tag.Description,
		})
	}

	// Extract endpoints from paths
	for path, pathItem := range openAPI.Paths {
		if pathItem == nil {
			continue
		}

		// Process each HTTP method
		methods := map[string]*shared.Operation{
			"GET":     pathItem.Get,
			"POST":    pathItem.Post,
			"PUT":     pathItem.Put,
			"DELETE":  pathItem.Delete,
			"PATCH":   pathItem.Patch,
			"OPTIONS": pathItem.Options,
			"HEAD":    pathItem.Head,
		}

		for method, op := range methods {
			if op == nil {
				continue
			}

			endpoint := i.operationToEndpoint(method, path, op)
			spec.Endpoints = append(spec.Endpoints, endpoint)
		}
	}

	return nil
}

// extractFromAsyncAPI extracts streaming endpoints from AsyncAPI spec.
func (i *Introspector) extractFromAsyncAPI(spec *APISpec, asyncAPI *shared.AsyncAPISpec) error {
	// If we haven't set info yet, extract it from AsyncAPI
	if spec.Info.Title == "" {
		spec.Info = APIInfo{
			Title:       asyncAPI.Info.Title,
			Version:     asyncAPI.Info.Version,
			Description: asyncAPI.Info.Description,
		}

		if asyncAPI.Info.Contact != nil {
			spec.Info.Contact = &Contact{
				Name:  asyncAPI.Info.Contact.Name,
				URL:   asyncAPI.Info.Contact.URL,
				Email: asyncAPI.Info.Contact.Email,
			}
		}

		if asyncAPI.Info.License != nil {
			spec.Info.License = &License{
				Name: asyncAPI.Info.License.Name,
				URL:  asyncAPI.Info.License.URL,
			}
		}
	}

	// Extract schemas from AsyncAPI components
	if asyncAPI.Components != nil && asyncAPI.Components.Schemas != nil {
		for name, schema := range asyncAPI.Components.Schemas {
			spec.Schemas[name] = i.convertSchema(schema)
		}
	}

	// Extract streaming features from known channel patterns
	i.extractStreamingFeatures(spec, asyncAPI)

	// Extract operations and map them to channels
	for opID, operation := range asyncAPI.Operations {
		if operation == nil || operation.Channel == nil {
			continue
		}

		channelRef := operation.Channel.Ref
		if channelRef == "" {
			continue
		}

		// Resolve channel reference
		channelName := strings.TrimPrefix(channelRef, "#/channels/")

		channel := asyncAPI.Channels[channelName]
		if channel == nil {
			continue
		}

		// Skip channels that are handled by streaming features
		if i.isStreamingFeatureChannel(channelName) {
			continue
		}

		// Determine if this is WebSocket or SSE based on protocol
		isWebSocket := i.isWebSocketChannel(asyncAPI, channel)

		if isWebSocket {
			ws := i.channelToWebSocket(opID, channel, operation)
			spec.WebSockets = append(spec.WebSockets, ws)
		} else {
			// Treat as SSE
			sse := i.channelToSSE(opID, channel, operation)
			spec.SSEs = append(spec.SSEs, sse)
		}
	}

	return nil
}

// isStreamingFeatureChannel checks if a channel is a streaming extension feature channel.
func (i *Introspector) isStreamingFeatureChannel(channelName string) bool {
	streamingChannels := []string{"rooms", "channels", "presence", "typing"}

	return slices.Contains(streamingChannels, channelName)
}

// extractStreamingFeatures extracts streaming extension features from AsyncAPI channels.
func (i *Introspector) extractStreamingFeatures(spec *APISpec, asyncAPI *shared.AsyncAPISpec) {
	// Initialize streaming spec
	spec.Streaming = &StreamingSpec{}

	// Check for room channel
	if roomChannel, ok := asyncAPI.Channels["rooms"]; ok {
		spec.Streaming.EnableRooms = true
		spec.Streaming.Rooms = i.extractRoomOperations(roomChannel, asyncAPI)
	}

	// Check for presence channel
	if presenceChannel, ok := asyncAPI.Channels["presence"]; ok {
		spec.Streaming.EnablePresence = true
		spec.Streaming.Presence = i.extractPresenceOperations(presenceChannel, asyncAPI)
	}

	// Check for typing channel
	if typingChannel, ok := asyncAPI.Channels["typing"]; ok {
		spec.Streaming.EnableTyping = true
		spec.Streaming.Typing = i.extractTypingOperations(typingChannel, asyncAPI)
	}

	// Check for pub/sub channels
	if channelsChannel, ok := asyncAPI.Channels["channels"]; ok {
		spec.Streaming.EnableChannels = true
		spec.Streaming.Channels = i.extractChannelOperations(channelsChannel, asyncAPI)
	}

	// Check if history is enabled (look for history-related operations)
	for opID := range asyncAPI.Operations {
		if strings.Contains(strings.ToLower(opID), "history") {
			spec.Streaming.EnableHistory = true

			break
		}
	}

	// If no streaming features found, set spec.Streaming to nil
	if !spec.Streaming.EnableRooms &&
		!spec.Streaming.EnablePresence &&
		!spec.Streaming.EnableTyping &&
		!spec.Streaming.EnableChannels {
		spec.Streaming = nil
	}
}

// extractRoomOperations extracts room-related operations from the rooms channel.
func (i *Introspector) extractRoomOperations(channel *shared.AsyncAPIChannel, asyncAPI *shared.AsyncAPISpec) *RoomOperations {
	ops := &RoomOperations{
		Path:           channel.Address,
		Parameters:     i.extractChannelParameters(channel),
		HistoryEnabled: false,
	}

	// Extract message schemas from the channel
	for msgName, msg := range channel.Messages {
		if msg.Payload == nil {
			continue
		}

		schema := i.convertSchema(msg.Payload)
		msgNameLower := strings.ToLower(msgName)

		switch {
		case strings.Contains(msgNameLower, "join"):
			ops.JoinSchema = schema
		case strings.Contains(msgNameLower, "leave"):
			ops.LeaveSchema = schema
		case strings.Contains(msgNameLower, "send"):
			ops.SendSchema = schema
		case strings.Contains(msgNameLower, "receive"):
			ops.ReceiveSchema = schema
		case strings.Contains(msgNameLower, "memberjoin"):
			ops.MemberJoinSchema = schema
		case strings.Contains(msgNameLower, "memberleave"):
			ops.MemberLeaveSchema = schema
		}
	}

	// Check for history in operations
	for opID := range asyncAPI.Operations {
		if strings.Contains(strings.ToLower(opID), "history") &&
			strings.Contains(strings.ToLower(opID), "room") {
			ops.HistoryEnabled = true

			break
		}
	}

	return ops
}

// extractPresenceOperations extracts presence-related operations.
func (i *Introspector) extractPresenceOperations(channel *shared.AsyncAPIChannel, _ *shared.AsyncAPISpec) *PresenceOperations {
	ops := &PresenceOperations{
		Path:     channel.Address,
		Statuses: []string{"online", "away", "busy", "offline"}, // Default statuses
	}

	// Extract message schemas from the channel
	for msgName, msg := range channel.Messages {
		if msg.Payload == nil {
			continue
		}

		schema := i.convertSchema(msg.Payload)
		msgNameLower := strings.ToLower(msgName)

		if strings.Contains(msgNameLower, "update") {
			ops.UpdateSchema = schema
		}

		// Event schema is typically the same for presence updates
		ops.EventSchema = schema

		// Try to extract statuses from enum if available
		if schema.Properties != nil {
			if statusProp, ok := schema.Properties["status"]; ok {
				if len(statusProp.Enum) > 0 {
					ops.Statuses = make([]string, 0, len(statusProp.Enum))
					for _, e := range statusProp.Enum {
						if s, ok := e.(string); ok {
							ops.Statuses = append(ops.Statuses, s)
						}
					}
				}
			}
		}
	}

	return ops
}

// extractTypingOperations extracts typing indicator operations.
func (i *Introspector) extractTypingOperations(channel *shared.AsyncAPIChannel, _ *shared.AsyncAPISpec) *TypingOperations {
	ops := &TypingOperations{
		Path:       channel.Address,
		Parameters: i.extractChannelParameters(channel),
		TimeoutMs:  3000, // Default timeout
	}

	// Extract message schemas from the channel
	for msgName, msg := range channel.Messages {
		if msg.Payload == nil {
			continue
		}

		schema := i.convertSchema(msg.Payload)
		msgNameLower := strings.ToLower(msgName)

		switch {
		case strings.Contains(msgNameLower, "start"):
			ops.StartSchema = schema
		case strings.Contains(msgNameLower, "stop"):
			ops.StopSchema = schema
		}
	}

	return ops
}

// extractChannelOperations extracts pub/sub channel operations.
func (i *Introspector) extractChannelOperations(channel *shared.AsyncAPIChannel, _ *shared.AsyncAPISpec) *ChannelOperations {
	ops := &ChannelOperations{
		Path:       channel.Address,
		Parameters: i.extractChannelParameters(channel),
	}

	// Extract message schemas from the channel
	for msgName, msg := range channel.Messages {
		if msg.Payload == nil {
			continue
		}

		schema := i.convertSchema(msg.Payload)
		msgNameLower := strings.ToLower(msgName)

		switch {
		case strings.Contains(msgNameLower, "subscribe"):
			ops.SubscribeSchema = schema
		case strings.Contains(msgNameLower, "unsubscribe"):
			ops.UnsubscribeSchema = schema
		case strings.Contains(msgNameLower, "publish"):
			ops.PublishSchema = schema
		case strings.Contains(msgNameLower, "message"):
			ops.MessageSchema = schema
		}
	}

	return ops
}

// extractChannelParameters extracts parameters from an AsyncAPI channel.
func (i *Introspector) extractChannelParameters(channel *shared.AsyncAPIChannel) []Parameter {
	if channel.Parameters == nil {
		return nil
	}

	params := make([]Parameter, 0, len(channel.Parameters))
	for name, param := range channel.Parameters {
		p := Parameter{
			Name:        name,
			In:          "path",
			Description: param.Description,
			Required:    true, // Path parameters are always required
		}

		if param.Schema != nil {
			p.Schema = i.convertSchema(param.Schema)
		}

		params = append(params, p)
	}

	return params
}

// operationToEndpoint converts an OpenAPI operation to an IR endpoint.
func (i *Introspector) operationToEndpoint(method, path string, op *shared.Operation) Endpoint {
	endpoint := Endpoint{
		Method:      method,
		Path:        path,
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
		OperationID: op.OperationID,
		Deprecated:  op.Deprecated,
		Responses:   make(map[int]*Response),
		Metadata:    make(map[string]any),
	}

	// Extract parameters
	for _, param := range op.Parameters {
		p := Parameter{
			Name:        param.Name,
			In:          param.In,
			Description: param.Description,
			Required:    param.Required,
			Deprecated:  param.Deprecated,
			Schema:      i.convertSchema(param.Schema),
			Example:     param.Example,
		}

		switch param.In {
		case "path":
			endpoint.PathParams = append(endpoint.PathParams, p)
		case "query":
			endpoint.QueryParams = append(endpoint.QueryParams, p)
		case "header":
			endpoint.HeaderParams = append(endpoint.HeaderParams, p)
		}
	}

	// Extract request body
	if op.RequestBody != nil {
		endpoint.RequestBody = &RequestBody{
			Description: op.RequestBody.Description,
			Required:    op.RequestBody.Required,
			Content:     make(map[string]*MediaType),
		}

		for contentType, media := range op.RequestBody.Content {
			endpoint.RequestBody.Content[contentType] = &MediaType{
				Schema:   i.convertSchema(media.Schema),
				Example:  media.Example,
				Examples: i.convertExamples(media.Examples),
			}
		}
	}

	// Extract responses
	for statusCode, resp := range op.Responses {
		code := 0

		if statusCode != "default" {
		}

		response := &Response{
			Description: resp.Description,
			Content:     make(map[string]*MediaType),
			Headers:     make(map[string]*Parameter),
		}

		for contentType, media := range resp.Content {
			response.Content[contentType] = &MediaType{
				Schema:   i.convertSchema(media.Schema),
				Example:  media.Example,
				Examples: i.convertExamples(media.Examples),
			}
		}

		for headerName, header := range resp.Headers {
			response.Headers[headerName] = &Parameter{
				Name:        headerName,
				In:          "header",
				Description: header.Description,
				Required:    header.Required,
				Schema:      i.convertSchema(header.Schema),
			}
		}

		if code == 0 {
			endpoint.DefaultError = response
		} else {
			endpoint.Responses[code] = response
		}
	}

	// Extract security requirements
	for _, secReq := range op.Security {
		for name, scopes := range secReq {
			endpoint.Security = append(endpoint.Security, SecurityRequirement{
				SchemeName: name,
				Scopes:     scopes,
			})
		}
	}

	return endpoint
}

// channelToWebSocket converts an AsyncAPI channel to a WebSocket endpoint.
func (i *Introspector) channelToWebSocket(opID string, channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) WebSocketEndpoint {
	ws := WebSocketEndpoint{
		ID:           opID,
		Path:         channel.Address,
		Summary:      channel.Summary,
		Description:  channel.Description,
		Tags:         i.extractTagNames(channel.Tags),
		Parameters:   i.extractChannelParameters(channel),
		MessageTypes: make(map[string]*Schema),
		Metadata:     make(map[string]any),
	}

	// Extract send/receive schemas from messages
	for msgName, msg := range channel.Messages {
		if msg.Payload != nil {
			schema := i.convertSchema(msg.Payload)

			// Store all message types
			ws.MessageTypes[msgName] = schema

			// Determine direction based on operation action
			switch operation.Action {
			case "send":
				ws.SendSchema = schema
			case "receive":
				ws.ReceiveSchema = schema
			}

			// Store message name in metadata
			if ws.Metadata["messages"] == nil {
				ws.Metadata["messages"] = make(map[string]string)
			}

			ws.Metadata["messages"].(map[string]string)[msgName] = operation.Action
		}
	}

	return ws
}

// channelToSSE converts an AsyncAPI channel to an SSE endpoint.
func (i *Introspector) channelToSSE(opID string, channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) SSEEndpoint {
	sse := SSEEndpoint{
		ID:           opID,
		Path:         channel.Address,
		Summary:      channel.Summary,
		Description:  channel.Description,
		Tags:         i.extractTagNames(channel.Tags),
		EventSchemas: make(map[string]*Schema),
		Metadata:     make(map[string]any),
	}

	// Extract event schemas from messages
	for msgName, msg := range channel.Messages {
		if msg.Payload != nil {
			sse.EventSchemas[msgName] = i.convertSchema(msg.Payload)
		}
	}

	return sse
}

// routeToEndpoint converts a raw route to an endpoint (fallback when no OpenAPI).
func (i *Introspector) routeToEndpoint(route router.RouteInfo) Endpoint {
	endpoint := Endpoint{
		Method:      route.Method,
		Path:        route.Path,
		Summary:     route.Summary,
		Description: route.Description,
		Tags:        route.Tags,
		Responses:   make(map[int]*Response),
		Metadata:    make(map[string]any),
	}

	// Extract auth requirements from metadata
	if authProviders, ok := route.Metadata["auth"].([]string); ok {
		for _, provider := range authProviders {
			endpoint.Security = append(endpoint.Security, SecurityRequirement{
				SchemeName: provider,
			})
		}
	}

	// Copy metadata
	maps.Copy(endpoint.Metadata, route.Metadata)

	return endpoint
}

// convertSchema converts a shared.Schema to an IR Schema.
func (i *Introspector) convertSchema(s *shared.Schema) *Schema {
	if s == nil {
		return nil
	}

	schema := &Schema{
		Type:                 s.Type,
		Format:               s.Format,
		Description:          s.Description,
		Required:             s.Required,
		Enum:                 s.Enum,
		Default:              s.Default,
		Example:              s.Example,
		Nullable:             s.Nullable,
		ReadOnly:             s.ReadOnly,
		WriteOnly:            s.WriteOnly,
		Pattern:              s.Pattern,
		Ref:                  s.Ref,
		AdditionalProperties: s.AdditionalProperties,
	}

	if s.MinLength > 0 {
		minLen := s.MinLength
		schema.MinLength = &minLen
	}

	if s.MaxLength > 0 {
		maxLen := s.MaxLength
		schema.MaxLength = &maxLen
	}

	if s.Minimum != 0 {
		minVal := s.Minimum
		schema.Minimum = &minVal
	}

	if s.Maximum != 0 {
		maxVal := s.Maximum
		schema.Maximum = &maxVal
	}

	// Convert properties
	if len(s.Properties) > 0 {
		schema.Properties = make(map[string]*Schema)
		for k, v := range s.Properties {
			schema.Properties[k] = i.convertSchema(v)
		}
	}

	// Convert items
	if s.Items != nil {
		schema.Items = i.convertSchema(s.Items)
	}

	// Convert polymorphic schemas
	if len(s.OneOf) > 0 {
		for idx := range s.OneOf {
			schema.OneOf = append(schema.OneOf, i.convertSchema(&s.OneOf[idx]))
		}
	}

	if len(s.AnyOf) > 0 {
		for idx := range s.AnyOf {
			schema.AnyOf = append(schema.AnyOf, i.convertSchema(&s.AnyOf[idx]))
		}
	}

	if len(s.AllOf) > 0 {
		for idx := range s.AllOf {
			schema.AllOf = append(schema.AllOf, i.convertSchema(&s.AllOf[idx]))
		}
	}

	// Convert discriminator
	if s.Discriminator != nil {
		schema.Discriminator = &Discriminator{
			PropertyName: s.Discriminator.PropertyName,
			Mapping:      s.Discriminator.Mapping,
		}
	}

	return schema
}

// convertExamples converts examples.
func (i *Introspector) convertExamples(examples map[string]*shared.Example) map[string]*Example {
	if examples == nil {
		return nil
	}

	result := make(map[string]*Example)
	for k, v := range examples {
		result[k] = &Example{
			Summary:     v.Summary,
			Description: v.Description,
			Value:       v.Value,
		}
	}

	return result
}

// isWebSocketChannel determines if a channel is WebSocket based on protocol.
func (i *Introspector) isWebSocketChannel(asyncAPI *shared.AsyncAPISpec, channel *shared.AsyncAPIChannel) bool {
	// Check channel servers
	for _, serverRef := range channel.Servers {
		serverName := strings.TrimPrefix(serverRef.Ref, "#/servers/")
		if server, ok := asyncAPI.Servers[serverName]; ok {
			protocol := strings.ToLower(server.Protocol)
			if protocol == "ws" || protocol == "wss" {
				return true
			}
		}
	}

	// Default to checking if there are bidirectional messages
	return len(channel.Messages) > 0
}

// extractTagNames extracts tag names from AsyncAPI tags.
func (i *Introspector) extractTagNames(tags []shared.AsyncAPITag) []string {
	names := make([]string, len(tags))
	for i, tag := range tags {
		names[i] = tag.Name
	}

	return names
}
