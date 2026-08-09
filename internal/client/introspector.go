package client

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
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

	// Entity-to-entity field edges, once every entity is known.
	//
	// It has to be here rather than inside endpoint resolution: an edge from
	// Order.customer to Customer is only recordable after whatever endpoint or
	// stream binding registers Customer has run, and that may be an operation
	// this loop has not reached yet. SpecParser.ParseFile calls the same
	// function at the same point in its own construction.
	spec.Kind = SourceIntrospection
	resolveEntityFields(spec)

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

	// Extract endpoints from paths.
	//
	// Paths are walked in sorted order and each path item's methods in a fixed
	// order (orderedPathOps): map iteration is randomized, and endpoint order
	// is what every generator emits in, so an unsorted walk makes the whole
	// generated package churn between otherwise identical runs.
	for _, path := range sortedPathKeys(openAPI.Paths) {
		pathItem := openAPI.Paths[path]
		if pathItem == nil {
			continue
		}

		for _, mo := range orderedPathOps(pathItem) {
			endpoint := i.operationToEndpoint(spec, mo.Method, path, mo.Op)
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

	// Extract operations and map them to channels, in sorted operation-id
	// order -- this appends to spec.WebSockets/spec.SSEs, and the streaming
	// generators emit in that order.
	for _, opID := range sortedStringKeys(asyncAPI.Operations) {
		operation := asyncAPI.Operations[opID]
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
			ws := i.channelToWebSocket(spec, opID, channel, operation)
			spec.WebSockets = append(spec.WebSockets, ws)
		} else {
			// Treat as SSE
			sse := i.channelToSSE(spec, opID, channel, operation)
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
func (i *Introspector) operationToEndpoint(spec *APISpec, method, path string, op *shared.Operation) Endpoint {
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

	// Extract responses.
	//
	// Status codes arrive as strings: an exact code ("200"), the catch-all
	// ("default"), or a class wildcard ("2XX"). Parsing goes through the same
	// parseStatusCode used by spec_parser.go's convertOperation, so the two
	// introspection paths agree on what a status key means instead of each
	// carrying its own copy that can quietly drift apart.
	//
	// Exact codes are applied before wildcards, so a spec declaring both 200
	// and 2XX keeps the specific one — map iteration order is random, and a
	// single pass would let whichever happened to come last win.
	//
	// A key that is none of these (parseStatusCode's ok == false, e.g. an
	// out-of-range or non-numeric status) is skipped rather than filed under
	// DefaultError. Treating an unparseable status as the default would let a
	// typo silently become the endpoint's error shape, which is worse than
	// dropping it: the generators read DefaultError to type failures.
	type pendingResponse struct {
		code         int
		fromWildcard bool
		response     *Response
	}

	pending := make([]pendingResponse, 0, len(op.Responses))

	for statusCode, resp := range op.Responses {
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

		if statusCode == "default" {
			endpoint.DefaultError = response

			continue
		}

		code, wildcard, ok := parseStatusCode(statusCode)
		if !ok {
			continue
		}

		pending = append(pending, pendingResponse{
			code:         code,
			fromWildcard: wildcard,
			response:     response,
		})
	}

	sort.SliceStable(pending, func(a, b int) bool {
		return !pending[a].fromWildcard && pending[b].fromWildcard
	})

	for _, p := range pending {
		if p.fromWildcard {
			if _, exists := endpoint.Responses[p.code]; exists {
				continue
			}
		}

		endpoint.Responses[p.code] = p.response
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

	resolveEndpointCacheMeta(spec, &endpoint, op.Extensions)

	return endpoint
}

// channelToWebSocket converts an AsyncAPI channel to a WebSocket endpoint.
func (i *Introspector) channelToWebSocket(spec *APISpec, opID string, channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) WebSocketEndpoint {
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

	ws.StreamBindings = streamBindings(channel.Extensions)
	registerStreamBindingEntities(spec, channel.Address, ws.StreamBindings)

	return ws
}

// channelToSSE converts an AsyncAPI channel to an SSE endpoint.
func (i *Introspector) channelToSSE(spec *APISpec, opID string, channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) SSEEndpoint {
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

	sse.StreamBindings = streamBindings(channel.Extensions)
	registerStreamBindingEntities(spec, channel.Address, sse.StreamBindings)

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
		Extensions:           s.Extensions,
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

// ResolveEndpointCacheMeta resolves an endpoint's entity identity and cache
// invalidation contract from its raw x-forge-* extensions.
//
// It is a thin, exported wrapper around resolveEndpointCacheMeta so that
// tests outside this package (e.g. the typescript generator's end-to-end
// test) can drive resolution without needing an unexported symbol.
func ResolveEndpointCacheMeta(spec *APISpec, ep *Endpoint, ext map[string]any) {
	resolveEndpointCacheMeta(spec, ep, ext)
}

// resolveEndpointCacheMeta fills in an endpoint's root type, entity and cache
// tags.
//
// Explicit declarations always beat inference, and an opt-out beats both. The
// order matters: an endpoint returning a projection must not be normalized just
// because its schema happens to carry an id.
//
// This is the one place either intermediate-representation builder resolves an
// endpoint's cache metadata -- Introspector.extractFromOpenAPI for a live
// router, SpecParser for a file -- and the reason it is one place rather than
// two is that a live-versus-file divergence in exactly this metadata has been a
// recurring defect in this package.
func resolveEndpointCacheMeta(spec *APISpec, ep *Endpoint, ext map[string]any) {
	// An opt-out takes the response out of the cache entirely, RootType
	// included. Leaving the root type behind would keep the runtime walking
	// into the response and normalizing whatever entities it found nested
	// there, which is the merge into canonical records that WithoutEntity()
	// exists to prevent -- just one level down from where it was declared.
	if v, ok := ext["x-forge-no-entity"].(bool); ok && v {
		return
	}

	schema, isList := successResponseSchema(spec, ep)

	// The name of what the response actually IS, which for an envelope is not
	// the entity it carries. Recorded whether or not an entity resolves below:
	// it describes the document, and a document with nothing cacheable beneath
	// it simply gets no row in the entities table to look up.
	ep.RootType = schemaName(schema)

	entity, isList := endpointEntity(spec, ep, ext, schema, isList)
	if entity == nil {
		return
	}

	ep.Entity = entity

	base := DeriveTags(ep.Method, entity, isList)
	ep.CacheTags = ApplyTagOverrides(
		base,
		stringSlice(ext["x-forge-invalidates"]),
		stringSlice(ext["x-forge-no-invalidation"]),
	)

	if spec.Entities == nil {
		spec.Entities = make(map[string]*EntityRef)
	}

	spec.Entities[entity.Type] = entity
}

// endpointEntity resolves the entity an endpoint's success response carries,
// and reports whether that response is a collection.
//
// Three tiers, in the order this package resolves everything: the route's own
// declaration, then the response type's declaration, then inference over its
// shape. A schema-level `x-forge-envelope` therefore beats the `id`-name
// heuristic on the wrapper itself, which is the same precedence InferEntity
// applies internally between `x-forge-id` and that heuristic, and for the same
// reason -- a declaration must beat a guess, or reaching for the declaration
// does not work.
func endpointEntity(
	spec *APISpec, ep *Endpoint, ext map[string]any, schema *Schema, isList bool,
) (*EntityRef, bool) {
	if raw, ok := ext["x-forge-entity"].(map[string]any); ok {
		typ, _ := raw["type"].(string)
		idField, _ := raw["idField"].(string)

		if typ != "" && idField != "" {
			validateDeclaredIDField(spec, ep, typ, idField, schema)

			return &EntityRef{Type: typ, IDField: idField}, isList
		}
	}

	if schema == nil {
		return nil, false
	}

	name := schemaName(schema)

	// The envelope's own isList, not the response's: the response is one
	// object either way, and what makes the operation a collection read is that
	// the property inside it holds an array.
	if entity, envelopeIsList := resolveEnvelopeEntity(spec, ep, name); entity != nil {
		return entity, envelopeIsList
	}

	return InferEntity(name, spec.ResolveSchemaRef(schema.Ref)), isList
}

// validateDeclaredIDField warns when a declared entity names an id field the
// response schema does not have.
//
// EntityDef.IDField is the JSON property name, the same thing inference
// produces and the same thing the browser runtime indexes a payload by. So a
// declaration of `idField: "ID"` against a response whose key is `id` is not a
// near miss -- it is a cache key that never matches any record, which presents
// as a cache that quietly does nothing rather than as an error. Declaring
// identity is exactly the moment to say so.
//
// Only schemas whose properties are actually visible are checked: a response
// that is an unresolvable $ref, a non-object, or a schema with no declared
// properties tells us nothing, and a warning there would be noise.
func validateDeclaredIDField(spec *APISpec, ep *Endpoint, typ, idField string, schema *Schema) {
	if schema == nil {
		return
	}

	resolved := schema
	if schema.Ref != "" {
		resolved = spec.ResolveSchemaRef(schema.Ref)
	}

	if resolved == nil || resolved.Type != "object" || len(resolved.Properties) == 0 {
		return
	}

	if _, ok := resolved.Properties[idField]; ok {
		return
	}

	have := make([]string, 0, len(resolved.Properties))
	for prop := range resolved.Properties {
		have = append(have, prop)
	}

	sort.Strings(have)

	spec.Warnings = append(spec.Warnings, fmt.Sprintf(
		"client: %s %s declares entity %q with idField %q, but the response schema has no"+
			" such property (has: %s). IDField is the JSON property name; as declared, the"+
			" cache key will never match a record.",
		ep.Method, ep.Path, typ, idField, strings.Join(have, ", ")))
}

// successResponseSchema returns the lowest 2xx JSON schema and whether it is an
// array. The array's item schema is returned, since that is what carries the
// entity.
func successResponseSchema(spec *APISpec, ep *Endpoint) (*Schema, bool) {
	codes := make([]int, 0, len(ep.Responses))
	for code := range ep.Responses {
		if code >= 200 && code < 300 {
			codes = append(codes, code)
		}
	}

	if len(codes) == 0 {
		return nil, false
	}

	sort.Ints(codes)

	resp := ep.Responses[codes[0]]

	mt, ok := resp.Content["application/json"]
	if !ok || mt.Schema == nil {
		return nil, false
	}

	if mt.Schema.Type == "array" && mt.Schema.Items != nil {
		return mt.Schema.Items, true
	}

	return mt.Schema, false
}

// schemaName extracts a component name from a $ref. An inline schema has no
// name and therefore cannot be an entity: a cache key needs a stable typename,
// and an anonymous struct has none.
func schemaName(s *Schema) string {
	if s == nil || s.Ref == "" {
		return ""
	}

	if i := strings.LastIndex(s.Ref, "/"); i >= 0 {
		return s.Ref[i+1:]
	}

	return s.Ref
}

// stringSlice coerces a JSON-decoded extension value to []string. Extensions
// arrive as []string when read from a live router's in-memory spec, and as
// []any when parsed from a JSON file, so both are accepted.
func stringSlice(v any) []string {
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))

		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}

		return out
	default:
		return nil
	}
}

// streamBindings converts the x-forge-stream extension into IR StreamBindings.
//
// The extension is []map[string]any when built by a live in-memory generator
// and []any (each element a map[string]any) once it has round-tripped through
// JSON, so both shapes are accepted.
func streamBindings(ext map[string]any) []StreamBinding {
	var entries []map[string]any

	switch raw := ext["x-forge-stream"].(type) {
	case []map[string]any:
		entries = raw
	case []any:
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				entries = append(entries, m)
			}
		}
	default:
		return nil
	}

	if len(entries) == 0 {
		return nil
	}

	bindings := make([]StreamBinding, 0, len(entries))

	for _, entry := range entries {
		message, _ := entry["message"].(string)
		entityType, _ := entry["entityType"].(string)
		intent, _ := entry["intent"].(string)

		bindings = append(bindings, StreamBinding{
			Message:     message,
			EntityType:  entityType,
			Intent:      StreamIntent(intent),
			Invalidates: stringSlice(entry["invalidates"]),
		})
	}

	return bindings
}

// registerStreamBindingEntities registers the entity type named by each
// stream binding into spec.Entities, so the browser runtime knows which JSON
// property identifies a record it receives over that channel.
//
// This is the other half of endpoint entity resolution (resolveEndpointCacheMeta
// writes the HTTP side of spec.Entities): a stream binding names its entity
// only by type name -- Emits[Order] records "Order" -- and that name is
// resolved against spec.Schemas, which is keyed by the same Go type name
// (component schemas and Emits[T] both use it). Identity is then inferred
// exactly as it would be for an HTTP response, via InferEntity, so a `forge:"id"`
// tag or ForgeEntity declaration on the type is honored the same way either
// path reaches it.
//
// Without an entities row the browser runtime has no idea which property is
// the identity, so a streams[] entry naming that entity cannot normalize --
// it is inert. Two failure modes are handled without inventing an entry or
// aborting generation: the named type may not appear in spec.Schemas at all
// (e.g. it only ever flows over this channel, and was never registered as a
// component), or InferEntity may refuse (ambiguous identity, or none). Both
// append a warning naming the channel and the entity type rather than
// failing silently -- a stream binding that quietly never normalizes is
// worse than a loud warning, which is the whole reason spec.Warnings exists.
//
// An entity already present in spec.Entities is left untouched: if it got
// there from an HTTP endpoint's response schema, that resolution is
// authoritative and must not be second-guessed by a stream binding naming the
// same type.
//
// A binding with an empty EntityType is its own failure mode, distinct from
// the two below: router.Emits[T] names the entity via reflection
// (reflect.TypeOf((*T)(nil)).Elem().Name()), and Name() returns "" for a type
// argument that has no name of its own -- an anonymous struct, or a slice,
// map, or pointer type. The resulting StreamBinding is well-formed Go but
// names no entity, so it would previously fall through the loop with no
// record of why: nothing else here reaches this point (streamBindings only
// ever populates EntityType from the same extension field), so this is the
// one place that can catch it. This is detected here rather than inside
// Emits[T] itself because that constructor runs in the router package, which
// has no spec and therefore nowhere to put a warning; this function already
// carries the channel address and is where its two sibling failure modes are
// reported, so an unnamed type argument is reported the same way.
func registerStreamBindingEntities(spec *APISpec, channelAddress string, bindings []StreamBinding) {
	for _, b := range bindings {
		if b.EntityType == "" {
			spec.Warnings = append(spec.Warnings, fmt.Sprintf(
				"channel %q: stream binding for message %q has no entity type -- Emits[T] was likely "+
					"called with an unnamed type argument (an anonymous struct, or a slice, map, or "+
					"pointer type); this binding will not normalize",
				channelAddress, b.Message))

			continue
		}

		if _, ok := spec.Entities[b.EntityType]; ok {
			continue
		}

		schema, ok := spec.Schemas[b.EntityType]
		if !ok {
			spec.Warnings = append(spec.Warnings, fmt.Sprintf(
				"channel %q: stream binding names entity type %q, which has no matching schema component; this binding will not normalize",
				channelAddress, b.EntityType))

			continue
		}

		entity := InferEntity(b.EntityType, schema)
		if entity == nil {
			spec.Warnings = append(spec.Warnings, fmt.Sprintf(
				"channel %q: stream binding names entity type %q, but its identity could not be inferred (ambiguous or no identity-shaped field); this binding will not normalize",
				channelAddress, b.EntityType))

			continue
		}

		if spec.Entities == nil {
			spec.Entities = make(map[string]*EntityRef)
		}

		spec.Entities[b.EntityType] = entity
	}
}
