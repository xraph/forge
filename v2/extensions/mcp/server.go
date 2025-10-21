package mcp

import (
	"fmt"
	"strings"
	"sync"

	"github.com/xraph/forge/v2"
)

// Server represents an MCP server that exposes tools, resources, and prompts
type Server struct {
	config  Config
	logger  forge.Logger
	metrics forge.Metrics

	// Tools
	tools     map[string]*Tool
	toolsLock sync.RWMutex

	// Resources
	resources     map[string]*Resource
	resourcesLock sync.RWMutex

	// Prompts
	prompts     map[string]*Prompt
	promptsLock sync.RWMutex

	// Schema cache
	schemaCache     map[string]*JSONSchema
	schemaCacheLock sync.RWMutex
}

// NewServer creates a new MCP server
func NewServer(config Config, logger forge.Logger, metrics forge.Metrics) *Server {
	return &Server{
		config:      config,
		logger:      logger,
		metrics:     metrics,
		tools:       make(map[string]*Tool),
		resources:   make(map[string]*Resource),
		prompts:     make(map[string]*Prompt),
		schemaCache: make(map[string]*JSONSchema),
	}
}

// RegisterTool registers a new MCP tool
func (s *Server) RegisterTool(tool *Tool) error {
	if tool.Name == "" {
		return fmt.Errorf("mcp: tool name cannot be empty")
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

// GetTool retrieves a tool by name
func (s *Server) GetTool(name string) (*Tool, error) {
	s.toolsLock.RLock()
	defer s.toolsLock.RUnlock()

	tool, exists := s.tools[name]
	if !exists {
		return nil, fmt.Errorf("mcp: tool not found: %s", name)
	}

	return tool, nil
}

// ListTools returns all registered tools
func (s *Server) ListTools() []Tool {
	s.toolsLock.RLock()
	defer s.toolsLock.RUnlock()

	tools := make([]Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, *tool)
	}

	return tools
}

// RegisterResource registers a new MCP resource
func (s *Server) RegisterResource(resource *Resource) error {
	if resource.URI == "" {
		return fmt.Errorf("mcp: resource URI cannot be empty")
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

// GetResource retrieves a resource by URI
func (s *Server) GetResource(uri string) (*Resource, error) {
	s.resourcesLock.RLock()
	defer s.resourcesLock.RUnlock()

	resource, exists := s.resources[uri]
	if !exists {
		return nil, fmt.Errorf("mcp: resource not found: %s", uri)
	}

	return resource, nil
}

// ListResources returns all registered resources
func (s *Server) ListResources() []Resource {
	s.resourcesLock.RLock()
	defer s.resourcesLock.RUnlock()

	resources := make([]Resource, 0, len(s.resources))
	for _, resource := range s.resources {
		resources = append(resources, *resource)
	}

	return resources
}

// RegisterPrompt registers a new MCP prompt
func (s *Server) RegisterPrompt(prompt *Prompt) error {
	if prompt.Name == "" {
		return fmt.Errorf("mcp: prompt name cannot be empty")
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

// GetPrompt retrieves a prompt by name
func (s *Server) GetPrompt(name string) (*Prompt, error) {
	s.promptsLock.RLock()
	defer s.promptsLock.RUnlock()

	prompt, exists := s.prompts[name]
	if !exists {
		return nil, fmt.Errorf("mcp: prompt not found: %s", name)
	}

	return prompt, nil
}

// ListPrompts returns all registered prompts
func (s *Server) ListPrompts() []Prompt {
	s.promptsLock.RLock()
	defer s.promptsLock.RUnlock()

	prompts := make([]Prompt, 0, len(s.prompts))
	for _, prompt := range s.prompts {
		prompts = append(prompts, *prompt)
	}

	return prompts
}

// GetServerInfo returns information about the MCP server
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

// GenerateToolFromRoute generates an MCP tool from a Forge route
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
	// TODO: Generate schema from route parameters and request body
	inputSchema := &JSONSchema{
		Type:       "object",
		Properties: make(map[string]*JSONSchema),
	}

	tool := &Tool{
		Name:        toolName,
		Description: description,
		InputSchema: inputSchema,
	}

	return tool, nil
}

// generateToolName generates a tool name from HTTP method and path
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

// CacheSchema caches a generated JSON schema
func (s *Server) CacheSchema(key string, schema *JSONSchema) {
	if !s.config.SchemaCache {
		return
	}

	s.schemaCacheLock.Lock()
	defer s.schemaCacheLock.Unlock()

	s.schemaCache[key] = schema
}

// GetCachedSchema retrieves a cached schema
func (s *Server) GetCachedSchema(key string) (*JSONSchema, bool) {
	if !s.config.SchemaCache {
		return nil, false
	}

	s.schemaCacheLock.RLock()
	defer s.schemaCacheLock.RUnlock()

	schema, exists := s.schemaCache[key]
	return schema, exists
}

// Clear clears all registered tools, resources, and prompts
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

// Stats returns statistics about the MCP server
func (s *Server) Stats() map[string]interface{} {
	s.toolsLock.RLock()
	toolCount := len(s.tools)
	s.toolsLock.RUnlock()

	s.resourcesLock.RLock()
	resourceCount := len(s.resources)
	s.resourcesLock.RUnlock()

	s.promptsLock.RLock()
	promptCount := len(s.prompts)
	s.promptsLock.RUnlock()

	return map[string]interface{}{
		"tools":     toolCount,
		"resources": resourceCount,
		"prompts":   promptCount,
		"enabled":   s.config.Enabled,
	}
}

