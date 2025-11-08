package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

// Server represents an MCP server that exposes tools, resources, and prompts.
type Server struct {
	config  Config
	logger  forge.Logger
	metrics forge.Metrics

	// Tools
	tools      map[string]*Tool
	toolRoutes map[string]forge.RouteInfo // Maps tool name to route
	toolsLock  sync.RWMutex

	// Resources
	resources       map[string]*Resource
	resourceReaders map[string]ResourceReader // Custom readers for resources
	resourcesLock   sync.RWMutex

	// Prompts
	prompts          map[string]*Prompt
	promptGenerators map[string]PromptGenerator // Custom generators for prompts
	promptsLock      sync.RWMutex

	// Schema cache
	schemaCache     map[string]*JSONSchema
	schemaCacheLock sync.RWMutex
}

// ResourceReader is a function that reads resource content.
type ResourceReader func(ctx context.Context, resource *Resource) (Content, error)

// PromptGenerator is a function that generates prompt messages.
type PromptGenerator func(ctx context.Context, prompt *Prompt, args map[string]any) ([]PromptMessage, error)

// NewServer creates a new MCP server.
func NewServer(config Config, logger forge.Logger, metrics forge.Metrics) *Server {
	return &Server{
		config:           config,
		logger:           logger,
		metrics:          metrics,
		tools:            make(map[string]*Tool),
		toolRoutes:       make(map[string]forge.RouteInfo),
		resources:        make(map[string]*Resource),
		resourceReaders:  make(map[string]ResourceReader),
		prompts:          make(map[string]*Prompt),
		promptGenerators: make(map[string]PromptGenerator),
		schemaCache:      make(map[string]*JSONSchema),
	}
}

// RegisterTool registers a new MCP tool.
func (s *Server) RegisterTool(tool *Tool) error {
	if tool.Name == "" {
		return errors.New("mcp: tool name cannot be empty")
	}

	// Validate tool name length
	if len(tool.Name) > s.config.MaxToolNameLength {
		return fmt.Errorf("mcp: tool name exceeds maximum length (%d)", s.config.MaxToolNameLength)
	}

	s.toolsLock.Lock()
	defer s.toolsLock.Unlock()

	if _, exists := s.tools[tool.Name]; exists {
		s.logger.Warn("mcp: tool already registered, overwriting",
			forge.F("tool", tool.Name),
		)
	}

	s.tools[tool.Name] = tool
	s.logger.Debug("mcp: tool registered",
		forge.F("tool", tool.Name),
		forge.F("description", tool.Description),
	)

	if s.metrics != nil {
		s.metrics.Gauge("mcp_tools_total").Set(float64(len(s.tools)))
	}

	return nil
}

// GetTool retrieves a tool by name.
func (s *Server) GetTool(name string) (*Tool, error) {
	s.toolsLock.RLock()
	defer s.toolsLock.RUnlock()

	tool, exists := s.tools[name]
	if !exists {
		return nil, fmt.Errorf("mcp: tool not found: %s", name)
	}

	return tool, nil
}

// ListTools returns all registered tools.
func (s *Server) ListTools() []Tool {
	s.toolsLock.RLock()
	defer s.toolsLock.RUnlock()

	tools := make([]Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, *tool)
	}

	return tools
}

// RegisterResource registers a new MCP resource.
func (s *Server) RegisterResource(resource *Resource) error {
	if resource.URI == "" {
		return errors.New("mcp: resource URI cannot be empty")
	}

	s.resourcesLock.Lock()
	defer s.resourcesLock.Unlock()

	s.resources[resource.URI] = resource
	s.logger.Debug("mcp: resource registered",
		forge.F("uri", resource.URI),
		forge.F("name", resource.Name),
	)

	if s.metrics != nil {
		s.metrics.Gauge("mcp_resources_total").Set(float64(len(s.resources)))
	}

	return nil
}

// GetResource retrieves a resource by URI.
func (s *Server) GetResource(uri string) (*Resource, error) {
	s.resourcesLock.RLock()
	defer s.resourcesLock.RUnlock()

	resource, exists := s.resources[uri]
	if !exists {
		return nil, fmt.Errorf("mcp: resource not found: %s", uri)
	}

	return resource, nil
}

// ListResources returns all registered resources.
func (s *Server) ListResources() []Resource {
	s.resourcesLock.RLock()
	defer s.resourcesLock.RUnlock()

	resources := make([]Resource, 0, len(s.resources))
	for _, resource := range s.resources {
		resources = append(resources, *resource)
	}

	return resources
}

// RegisterPrompt registers a new MCP prompt.
func (s *Server) RegisterPrompt(prompt *Prompt) error {
	if prompt.Name == "" {
		return errors.New("mcp: prompt name cannot be empty")
	}

	s.promptsLock.Lock()
	defer s.promptsLock.Unlock()

	s.prompts[prompt.Name] = prompt
	s.logger.Debug("mcp: prompt registered",
		forge.F("prompt", prompt.Name),
		forge.F("description", prompt.Description),
	)

	if s.metrics != nil {
		s.metrics.Gauge("mcp_prompts_total").Set(float64(len(s.prompts)))
	}

	return nil
}

// GetPrompt retrieves a prompt by name.
func (s *Server) GetPrompt(name string) (*Prompt, error) {
	s.promptsLock.RLock()
	defer s.promptsLock.RUnlock()

	prompt, exists := s.prompts[name]
	if !exists {
		return nil, fmt.Errorf("mcp: prompt not found: %s", name)
	}

	return prompt, nil
}

// ListPrompts returns all registered prompts.
func (s *Server) ListPrompts() []Prompt {
	s.promptsLock.RLock()
	defer s.promptsLock.RUnlock()

	prompts := make([]Prompt, 0, len(s.prompts))
	for _, prompt := range s.prompts {
		prompts = append(prompts, *prompt)
	}

	return prompts
}

// GetServerInfo returns information about the MCP server.
func (s *Server) GetServerInfo() ServerInfo {
	return ServerInfo{
		Name:    s.config.ServerName,
		Version: s.config.ServerVersion,
		Capabilities: Capabilities{
			Tools: &ToolsCapability{
				ListChanged: false,
			},
			Resources: func() *ResourcesCapability {
				if s.config.EnableResources {
					return &ResourcesCapability{
						Subscribe:   false,
						ListChanged: false,
					}
				}

				return nil
			}(),
			Prompts: func() *PromptsCapability {
				if s.config.EnablePrompts {
					return &PromptsCapability{
						ListChanged: false,
					}
				}

				return nil
			}(),
		},
	}
}

// GenerateToolFromRoute generates an MCP tool from a Forge route.
func (s *Server) GenerateToolFromRoute(route forge.RouteInfo) (*Tool, error) {
	// Generate tool name from route
	toolName := s.generateToolName(route.Method, route.Path)

	// Apply prefix if configured
	if s.config.ToolPrefix != "" {
		toolName = s.config.ToolPrefix + toolName
	}

	// Generate description
	description := route.Summary
	if description == "" {
		description = fmt.Sprintf("%s %s", route.Method, route.Path)
	}

	// Generate input schema from route metadata
	inputSchema := s.generateInputSchema(route)

	tool := &Tool{
		Name:        toolName,
		Description: description,
		InputSchema: inputSchema,
	}

	// Store route mapping
	s.toolsLock.Lock()
	s.toolRoutes[toolName] = route
	s.toolsLock.Unlock()

	return tool, nil
}

// generateInputSchema generates JSON schema from route metadata.
func (s *Server) generateInputSchema(route forge.RouteInfo) *JSONSchema {
	schema := &JSONSchema{
		Type:       "object",
		Properties: make(map[string]*JSONSchema),
		Required:   []string{},
	}

	// Add path parameters
	// Extract path parameters like :id or {id}
	pathParams := extractPathParams(route.Path)
	for _, param := range pathParams {
		schema.Properties[param] = &JSONSchema{
			Type:        "string",
			Description: "Path parameter: " + param,
		}
		schema.Required = append(schema.Required, param)
	}

	// For POST/PUT/PATCH, add body schema if available
	if route.Method == http.MethodPost || route.Method == http.MethodPut || route.Method == http.MethodPatch {
		schema.Properties["body"] = &JSONSchema{
			Type:        "object",
			Description: "Request body",
		}
	}

	// Add query parameters hint
	schema.Properties["query"] = &JSONSchema{
		Type:                 "object",
		Description:          "Query parameters (optional)",
		AdditionalProperties: true,
	}

	return schema
}

// extractPathParams extracts parameter names from path.
func extractPathParams(path string) []string {
	var params []string

	parts := strings.SplitSeq(path, "/")
	for part := range parts {
		// Handle :param style
		if after, ok := strings.CutPrefix(part, ":"); ok {
			params = append(params, after)
		}
		// Handle {param} style
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			param := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			params = append(params, param)
		}
	}

	return params
}

// generateToolName generates a tool name from HTTP method and path.
func (s *Server) generateToolName(method, path string) string {
	// Remove leading slash and convert to snake_case
	path = strings.TrimPrefix(path, "/")

	// Replace path separators and parameters
	path = strings.ReplaceAll(path, "/", "_")
	path = strings.ReplaceAll(path, ":", "")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")

	// Add method prefix
	var prefix string

	switch method {
	case "POST":
		prefix = "create"
	case "GET":
		prefix = "get"
	case "PUT":
		prefix = "update"
	case "PATCH":
		prefix = "patch"
	case "DELETE":
		prefix = "delete"
	default:
		prefix = strings.ToLower(method)
	}

	// Combine prefix and path
	if path == "" {
		return prefix
	}

	return prefix + "_" + path
}

// CacheSchema caches a generated JSON schema.
func (s *Server) CacheSchema(key string, schema *JSONSchema) {
	if !s.config.SchemaCache {
		return
	}

	s.schemaCacheLock.Lock()
	defer s.schemaCacheLock.Unlock()

	s.schemaCache[key] = schema
}

// GetCachedSchema retrieves a cached schema.
func (s *Server) GetCachedSchema(key string) (*JSONSchema, bool) {
	if !s.config.SchemaCache {
		return nil, false
	}

	s.schemaCacheLock.RLock()
	defer s.schemaCacheLock.RUnlock()

	schema, exists := s.schemaCache[key]

	return schema, exists
}

// Clear clears all registered tools, resources, and prompts.
func (s *Server) Clear() {
	s.toolsLock.Lock()
	s.tools = make(map[string]*Tool)
	s.toolsLock.Unlock()

	s.resourcesLock.Lock()
	s.resources = make(map[string]*Resource)
	s.resourcesLock.Unlock()

	s.promptsLock.Lock()
	s.prompts = make(map[string]*Prompt)
	s.promptsLock.Unlock()

	s.schemaCacheLock.Lock()
	s.schemaCache = make(map[string]*JSONSchema)
	s.schemaCacheLock.Unlock()

	s.logger.Info("mcp: server cleared")
}

// Stats returns statistics about the MCP server.
func (s *Server) Stats() map[string]any {
	s.toolsLock.RLock()
	toolCount := len(s.tools)
	s.toolsLock.RUnlock()

	s.resourcesLock.RLock()
	resourceCount := len(s.resources)
	s.resourcesLock.RUnlock()

	s.promptsLock.RLock()
	promptCount := len(s.prompts)
	s.promptsLock.RUnlock()

	return map[string]any{
		"tools":     toolCount,
		"resources": resourceCount,
		"prompts":   promptCount,
		"enabled":   s.config.Enabled,
	}
}

// ExecuteTool executes a tool by calling its underlying route.
func (s *Server) ExecuteTool(ctx context.Context, tool *Tool, arguments map[string]any) (string, error) {
	s.toolsLock.RLock()
	route, exists := s.toolRoutes[tool.Name]
	s.toolsLock.RUnlock()

	if !exists {
		return "", fmt.Errorf("mcp: no route found for tool %s", tool.Name)
	}

	// Build the request path with parameters
	path := route.Path

	pathParams := extractPathParams(route.Path)
	for _, param := range pathParams {
		if val, ok := arguments[param]; ok {
			placeholder := ":" + param
			if !strings.Contains(path, placeholder) {
				placeholder = "{" + param + "}"
			}

			path = strings.ReplaceAll(path, placeholder, fmt.Sprintf("%v", val))
		}
	}

	// Build query string
	query := ""

	if queryArgs, ok := arguments["query"].(map[string]any); ok {
		var queryParts []string
		for k, v := range queryArgs {
			queryParts = append(queryParts, fmt.Sprintf("%s=%v", k, v))
		}

		if len(queryParts) > 0 {
			query = "?" + strings.Join(queryParts, "&")
		}
	}

	// Extract body
	var bodyData []byte

	if body, ok := arguments["body"]; ok {
		var err error

		bodyData, err = json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("mcp: failed to marshal request body: %w", err)
		}
	}

	s.logger.Debug("mcp: executing tool",
		forge.F("tool", tool.Name),
		forge.F("method", route.Method),
		forge.F("path", path+query),
	)

	// Create internal HTTP request
	req, err := http.NewRequestWithContext(ctx, route.Method, path+query, bytes.NewReader(bodyData))
	if err != nil {
		return "", fmt.Errorf("mcp: failed to create request: %w", err)
	}

	if len(bodyData) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	// Execute via internal transport (simulation)
	// Note: This is a simplified implementation
	// In production, you'd want to use the actual router's handler
	result := fmt.Sprintf("Tool executed: %s %s", route.Method, path+query)
	if len(bodyData) > 0 {
		result += fmt.Sprintf(" (body: %s)", string(bodyData))
	}

	return result, nil
}

// RegisterResourceReader registers a custom reader for a resource.
func (s *Server) RegisterResourceReader(uri string, reader ResourceReader) error {
	s.resourcesLock.Lock()
	defer s.resourcesLock.Unlock()

	s.resourceReaders[uri] = reader
	s.logger.Debug("mcp: resource reader registered", forge.F("uri", uri))

	return nil
}

// ReadResource reads resource content using registered reader or default.
func (s *Server) ReadResource(ctx context.Context, resource *Resource) (Content, error) {
	s.resourcesLock.RLock()
	reader, hasReader := s.resourceReaders[resource.URI]
	s.resourcesLock.RUnlock()

	// Use custom reader if available
	if hasReader {
		return reader(ctx, resource)
	}

	// Default implementation: return basic text content
	return Content{
		Type: "text",
		Text: fmt.Sprintf("Resource: %s\nURI: %s\nDescription: %s",
			resource.Name, resource.URI, resource.Description),
	}, nil
}

// RegisterPromptGenerator registers a custom generator for a prompt.
func (s *Server) RegisterPromptGenerator(name string, generator PromptGenerator) error {
	s.promptsLock.Lock()
	defer s.promptsLock.Unlock()

	s.promptGenerators[name] = generator
	s.logger.Debug("mcp: prompt generator registered", forge.F("prompt", name))

	return nil
}

// GeneratePrompt generates prompt messages using registered generator or default.
func (s *Server) GeneratePrompt(ctx context.Context, prompt *Prompt, args map[string]any) ([]PromptMessage, error) {
	s.promptsLock.RLock()
	generator, hasGenerator := s.promptGenerators[prompt.Name]
	s.promptsLock.RUnlock()

	// Use custom generator if available
	if hasGenerator {
		return generator(ctx, prompt, args)
	}

	// Default implementation: generate simple message with arguments
	var argText string

	if len(args) > 0 {
		argsParts := []string{}
		for k, v := range args {
			argsParts = append(argsParts, fmt.Sprintf("%s=%v", k, v))
		}

		argText = " with arguments: " + strings.Join(argsParts, ", ")
	}

	return []PromptMessage{
		{
			Role: "user",
			Content: []Content{
				{
					Type: "text",
					Text: fmt.Sprintf("Prompt: %s%s\n%s", prompt.Name, argText, prompt.Description),
				},
			},
		},
	}, nil
}
