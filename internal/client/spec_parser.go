package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/internal/shared"
	"gopkg.in/yaml.v3"
)

// SpecParser parses OpenAPI and AsyncAPI specification files
type SpecParser struct{}

// NewSpecParser creates a new spec parser
func NewSpecParser() *SpecParser {
	return &SpecParser{}
}

// ParseFile parses a specification file (OpenAPI or AsyncAPI)
func (p *SpecParser) ParseFile(ctx context.Context, filePath string) (*APISpec, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read spec file: %w", err)
	}

	// Determine file type by extension
	ext := strings.ToLower(filepath.Ext(filePath))
	isYAML := ext == ".yaml" || ext == ".yml"

	// Try to determine spec type by content
	specType, err := p.detectSpecType(data, isYAML)
	if err != nil {
		return nil, fmt.Errorf("detect spec type: %w", err)
	}

	switch specType {
	case "openapi":
		return p.parseOpenAPI(data, isYAML)
	case "asyncapi":
		return p.parseAsyncAPI(data, isYAML)
	default:
		return nil, fmt.Errorf("unknown spec type: %s", specType)
	}
}

// detectSpecType detects whether the spec is OpenAPI or AsyncAPI
func (p *SpecParser) detectSpecType(data []byte, isYAML bool) (string, error) {
	var raw map[string]interface{}

	if isYAML {
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return "", err
		}
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			return "", err
		}
	}

	// Check for OpenAPI version field
	if openapi, ok := raw["openapi"].(string); ok && openapi != "" {
		return "openapi", nil
	}

	// Check for AsyncAPI version field
	if asyncapi, ok := raw["asyncapi"].(string); ok && asyncapi != "" {
		return "asyncapi", nil
	}

	return "", fmt.Errorf("spec does not contain 'openapi' or 'asyncapi' version field")
}

// parseOpenAPI parses an OpenAPI specification
func (p *SpecParser) parseOpenAPI(data []byte, isYAML bool) (*APISpec, error) {
	var openAPISpec shared.OpenAPISpec

	if isYAML {
		if err := yaml.Unmarshal(data, &openAPISpec); err != nil {
			return nil, fmt.Errorf("unmarshal OpenAPI YAML: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, &openAPISpec); err != nil {
			return nil, fmt.Errorf("unmarshal OpenAPI JSON: %w", err)
		}
	}

	// Convert to IR
	spec := &APISpec{
		Schemas:  make(map[string]*Schema),
		Security: []SecurityScheme{},
	}

	// Extract info
	spec.Info = APIInfo{
		Title:       openAPISpec.Info.Title,
		Version:     openAPISpec.Info.Version,
		Description: openAPISpec.Info.Description,
	}

	if openAPISpec.Info.Contact != nil {
		spec.Info.Contact = &Contact{
			Name:  openAPISpec.Info.Contact.Name,
			URL:   openAPISpec.Info.Contact.URL,
			Email: openAPISpec.Info.Contact.Email,
		}
	}

	if openAPISpec.Info.License != nil {
		spec.Info.License = &License{
			Name: openAPISpec.Info.License.Name,
			URL:  openAPISpec.Info.License.URL,
		}
	}

	// Extract servers
	for _, srv := range openAPISpec.Servers {
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
	if openAPISpec.Components != nil && openAPISpec.Components.SecuritySchemes != nil {
		for name, scheme := range openAPISpec.Components.SecuritySchemes {
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
				secScheme.Flows = convertOAuthFlows(scheme.Flows)
			}

			spec.Security = append(spec.Security, secScheme)
		}
	}

	// Extract schemas
	if openAPISpec.Components != nil && openAPISpec.Components.Schemas != nil {
		for name, schema := range openAPISpec.Components.Schemas {
			spec.Schemas[name] = convertSchema(schema)
		}
	}

	// Extract tags
	for _, tag := range openAPISpec.Tags {
		spec.Tags = append(spec.Tags, Tag{
			Name:        tag.Name,
			Description: tag.Description,
		})
	}

	// Extract endpoints
	for path, pathItem := range openAPISpec.Paths {
		if pathItem == nil {
			continue
		}

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

			endpoint := convertOperation(method, path, op)
			spec.Endpoints = append(spec.Endpoints, endpoint)
		}
	}

	return spec, nil
}

// parseAsyncAPI parses an AsyncAPI specification
func (p *SpecParser) parseAsyncAPI(data []byte, isYAML bool) (*APISpec, error) {
	var asyncAPISpec shared.AsyncAPISpec

	if isYAML {
		if err := yaml.Unmarshal(data, &asyncAPISpec); err != nil {
			return nil, fmt.Errorf("unmarshal AsyncAPI YAML: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, &asyncAPISpec); err != nil {
			return nil, fmt.Errorf("unmarshal AsyncAPI JSON: %w", err)
		}
	}

	// Convert to IR
	spec := &APISpec{
		Schemas:  make(map[string]*Schema),
		Security: []SecurityScheme{},
	}

	// Extract info
	spec.Info = APIInfo{
		Title:       asyncAPISpec.Info.Title,
		Version:     asyncAPISpec.Info.Version,
		Description: asyncAPISpec.Info.Description,
	}

	if asyncAPISpec.Info.Contact != nil {
		spec.Info.Contact = &Contact{
			Name:  asyncAPISpec.Info.Contact.Name,
			URL:   asyncAPISpec.Info.Contact.URL,
			Email: asyncAPISpec.Info.Contact.Email,
		}
	}

	if asyncAPISpec.Info.License != nil {
		spec.Info.License = &License{
			Name: asyncAPISpec.Info.License.Name,
			URL:  asyncAPISpec.Info.License.URL,
		}
	}

	// Extract schemas from components
	if asyncAPISpec.Components != nil && asyncAPISpec.Components.Schemas != nil {
		for name, schema := range asyncAPISpec.Components.Schemas {
			spec.Schemas[name] = convertSchema(schema)
		}
	}

	// Extract servers
	for _, srv := range asyncAPISpec.Servers {
		server := Server{
			URL:         fmt.Sprintf("%s://%s%s", srv.Protocol, srv.Host, srv.Pathname),
			Description: srv.Description,
			Variables:   make(map[string]ServerVariable),
		}
		spec.Servers = append(spec.Servers, server)
	}

	// Extract operations and channels
	wsEndpoints := make(map[string]*WebSocketEndpoint)
	sseEndpoints := make(map[string]*SSEEndpoint)
	
	for opID, operation := range asyncAPISpec.Operations {
		if operation == nil || operation.Channel == nil {
			continue
		}

		channelRef := operation.Channel.Ref
		if channelRef == "" {
			continue
		}

		channelName := strings.TrimPrefix(channelRef, "#/channels/")
		channel := asyncAPISpec.Channels[channelName]
		if channel == nil {
			continue
		}

		// Determine if WebSocket or SSE
		isWebSocket := detectWebSocketChannel(&asyncAPISpec, channel)

		if isWebSocket {
			// Use channel name as key to merge operations on same channel
			if wsEndpoints[channelName] == nil {
				ws := convertWebSocketChannel(opID, channel, operation)
				wsEndpoints[channelName] = &ws
			} else {
				// Merge with existing endpoint
				existing := wsEndpoints[channelName]
				if operation.Action == "send" && existing.SendSchema == nil {
					existing.SendSchema = convertSchemaFromChannel(channel, operation)
				} else if operation.Action == "receive" && existing.ReceiveSchema == nil {
					existing.ReceiveSchema = convertSchemaFromChannel(channel, operation)
				}
			}
		} else {
			// Use channel name as key to merge operations on same channel
			if sseEndpoints[channelName] == nil {
				sse := convertSSEChannel(opID, channel, operation)
				sseEndpoints[channelName] = &sse
			} else {
				// Merge event schemas
				existing := sseEndpoints[channelName]
				for msgName, msg := range channel.Messages {
					if msg.Payload != nil {
						existing.EventSchemas[msgName] = convertSchema(msg.Payload)
					}
				}
			}
		}
	}
	
	// Add merged endpoints to spec
	for _, ws := range wsEndpoints {
		spec.WebSockets = append(spec.WebSockets, *ws)
	}
	for _, sse := range sseEndpoints {
		spec.SSEs = append(spec.SSEs, *sse)
	}

	return spec, nil
}

// Helper conversion functions

func convertSchemaFromChannel(channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) *Schema {
	for _, msg := range channel.Messages {
		if msg.Payload != nil {
			return convertSchema(msg.Payload)
		}
	}
	return nil
}

func convertOperation(method, path string, op *shared.Operation) Endpoint {
	endpoint := Endpoint{
		Method:      method,
		Path:        path,
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
		OperationID: op.OperationID,
		Deprecated:  op.Deprecated,
		Responses:   make(map[int]*Response),
		Metadata:    make(map[string]interface{}),
	}

	// Extract parameters
	for _, param := range op.Parameters {
		p := Parameter{
			Name:        param.Name,
			In:          param.In,
			Description: param.Description,
			Required:    param.Required,
			Deprecated:  param.Deprecated,
			Schema:      convertSchema(param.Schema),
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
				Schema:   convertSchema(media.Schema),
				Example:  media.Example,
				Examples: convertExamples(media.Examples),
			}
		}
	}

	// Extract responses
	for statusCode, resp := range op.Responses {
		code := 0
		if statusCode != "default" {
			fmt.Sscanf(statusCode, "%d", &code)
		}

		response := &Response{
			Description: resp.Description,
			Content:     make(map[string]*MediaType),
			Headers:     make(map[string]*Parameter),
		}

		for contentType, media := range resp.Content {
			response.Content[contentType] = &MediaType{
				Schema:   convertSchema(media.Schema),
				Example:  media.Example,
				Examples: convertExamples(media.Examples),
			}
		}

		for headerName, header := range resp.Headers {
			response.Headers[headerName] = &Parameter{
				Name:        headerName,
				In:          "header",
				Description: header.Description,
				Required:    header.Required,
				Schema:      convertSchema(header.Schema),
			}
		}

		if code == 0 {
			endpoint.DefaultError = response
		} else {
			endpoint.Responses[code] = response
		}
	}

	// Extract security
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

func convertSchema(s *shared.Schema) *Schema {
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
		min := s.Minimum
		schema.Minimum = &min
	}
	if s.Maximum != 0 {
		max := s.Maximum
		schema.Maximum = &max
	}

	if len(s.Properties) > 0 {
		schema.Properties = make(map[string]*Schema)
		for k, v := range s.Properties {
			schema.Properties[k] = convertSchema(v)
		}
	}

	if s.Items != nil {
		schema.Items = convertSchema(s.Items)
	}

	if len(s.OneOf) > 0 {
		for idx := range s.OneOf {
			schema.OneOf = append(schema.OneOf, convertSchema(&s.OneOf[idx]))
		}
	}
	if len(s.AnyOf) > 0 {
		for idx := range s.AnyOf {
			schema.AnyOf = append(schema.AnyOf, convertSchema(&s.AnyOf[idx]))
		}
	}
	if len(s.AllOf) > 0 {
		for idx := range s.AllOf {
			schema.AllOf = append(schema.AllOf, convertSchema(&s.AllOf[idx]))
		}
	}

	if s.Discriminator != nil {
		schema.Discriminator = &Discriminator{
			PropertyName: s.Discriminator.PropertyName,
			Mapping:      s.Discriminator.Mapping,
		}
	}

	return schema
}

func convertOAuthFlows(flows *shared.OAuthFlows) *OAuthFlows {
	if flows == nil {
		return nil
	}

	result := &OAuthFlows{}

	if flows.Implicit != nil {
		result.Implicit = &OAuthFlow{
			AuthorizationURL: flows.Implicit.AuthorizationURL,
			TokenURL:         flows.Implicit.TokenURL,
			RefreshURL:       flows.Implicit.RefreshURL,
			Scopes:           flows.Implicit.Scopes,
		}
	}
	if flows.Password != nil {
		result.Password = &OAuthFlow{
			AuthorizationURL: flows.Password.AuthorizationURL,
			TokenURL:         flows.Password.TokenURL,
			RefreshURL:       flows.Password.RefreshURL,
			Scopes:           flows.Password.Scopes,
		}
	}
	if flows.ClientCredentials != nil {
		result.ClientCredentials = &OAuthFlow{
			AuthorizationURL: flows.ClientCredentials.AuthorizationURL,
			TokenURL:         flows.ClientCredentials.TokenURL,
			RefreshURL:       flows.ClientCredentials.RefreshURL,
			Scopes:           flows.ClientCredentials.Scopes,
		}
	}
	if flows.AuthorizationCode != nil {
		result.AuthorizationCode = &OAuthFlow{
			AuthorizationURL: flows.AuthorizationCode.AuthorizationURL,
			TokenURL:         flows.AuthorizationCode.TokenURL,
			RefreshURL:       flows.AuthorizationCode.RefreshURL,
			Scopes:           flows.AuthorizationCode.Scopes,
		}
	}

	return result
}

func convertExamples(examples map[string]*shared.Example) map[string]*Example {
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

func convertWebSocketChannel(opID string, channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) WebSocketEndpoint {
	ws := WebSocketEndpoint{
		ID:          opID,
		Path:        channel.Address,
		Summary:     channel.Summary,
		Description: channel.Description,
		Tags:        extractAsyncTagNames(channel.Tags),
		Metadata:    make(map[string]interface{}),
	}

	for msgName, msg := range channel.Messages {
		if msg.Payload != nil {
			schema := convertSchema(msg.Payload)

			if operation.Action == "send" {
				ws.SendSchema = schema
			} else if operation.Action == "receive" {
				ws.ReceiveSchema = schema
			}

			if ws.Metadata["messages"] == nil {
				ws.Metadata["messages"] = make(map[string]string)
			}
			ws.Metadata["messages"].(map[string]string)[msgName] = operation.Action
		}
	}

	return ws
}

func convertSSEChannel(opID string, channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) SSEEndpoint {
	sse := SSEEndpoint{
		ID:           opID,
		Path:         channel.Address,
		Summary:      channel.Summary,
		Description:  channel.Description,
		Tags:         extractAsyncTagNames(channel.Tags),
		EventSchemas: make(map[string]*Schema),
		Metadata:     make(map[string]interface{}),
	}

	for msgName, msg := range channel.Messages {
		if msg.Payload != nil {
			sse.EventSchemas[msgName] = convertSchema(msg.Payload)
		}
	}

	return sse
}

func detectWebSocketChannel(asyncAPI *shared.AsyncAPISpec, channel *shared.AsyncAPIChannel) bool {
	for _, serverRef := range channel.Servers {
		serverName := strings.TrimPrefix(serverRef.Ref, "#/servers/")
		if server, ok := asyncAPI.Servers[serverName]; ok {
			protocol := strings.ToLower(server.Protocol)
			if protocol == "ws" || protocol == "wss" {
				return true
			}
		}
	}
	return len(channel.Messages) > 0
}

func extractAsyncTagNames(tags []shared.AsyncAPITag) []string {
	names := make([]string, len(tags))
	for i, tag := range tags {
		names[i] = tag.Name
	}
	return names
}
