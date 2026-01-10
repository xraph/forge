package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ResourceHandler handles resource read requests.
type ResourceHandler func(ctx context.Context, uri string) (*ResourceContent, error)

// ToolHandler handles tool call requests.
type ToolHandler func(ctx context.Context, args map[string]any) ([]ToolResultContent, error)

// PromptHandler handles prompt get requests.
type PromptHandler func(ctx context.Context, args map[string]any) (*GetPromptResponse, error)

// ResourceRegistry manages registered resources.
type ResourceRegistry struct {
	resources     map[string]Resource
	handlers      map[string]ResourceHandler
	subscriptions map[string]bool
	mu            sync.RWMutex
}

// NewResourceRegistry creates a new resource registry.
func NewResourceRegistry() *ResourceRegistry {
	return &ResourceRegistry{
		resources:     make(map[string]Resource),
		handlers:      make(map[string]ResourceHandler),
		subscriptions: make(map[string]bool),
	}
}

// Register registers a resource with its handler.
func (r *ResourceRegistry) Register(resource Resource, handler ResourceHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.resources[resource.URI] = resource
	r.handlers[resource.URI] = handler
}

// Unregister removes a resource.
func (r *ResourceRegistry) Unregister(uri string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.resources, uri)
	delete(r.handlers, uri)
	delete(r.subscriptions, uri)
}

// List returns all registered resources.
func (r *ResourceRegistry) List() []Resource {
	r.mu.RLock()
	defer r.mu.RUnlock()

	resources := make([]Resource, 0, len(r.resources))
	for _, res := range r.resources {
		resources = append(resources, res)
	}

	return resources
}

// Get returns a resource by URI.
func (r *ResourceRegistry) Get(uri string) (Resource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	res, ok := r.resources[uri]

	return res, ok
}

// Read reads a resource by URI.
func (r *ResourceRegistry) Read(ctx context.Context, uri string) (*ResourceContent, error) {
	r.mu.RLock()
	handler, ok := r.handlers[uri]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("resource not found: %s", uri)
	}

	return handler(ctx, uri)
}

// Subscribe marks a resource as subscribed.
func (r *ResourceRegistry) Subscribe(uri string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.resources[uri]; !ok {
		return fmt.Errorf("resource not found: %s", uri)
	}

	r.subscriptions[uri] = true

	return nil
}

// Unsubscribe removes a subscription.
func (r *ResourceRegistry) Unsubscribe(uri string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.subscriptions, uri)
}

// IsSubscribed checks if a resource is subscribed.
func (r *ResourceRegistry) IsSubscribed(uri string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.subscriptions[uri]
}

// ToolRegistry manages registered tools.
type ToolRegistry struct {
	tools    map[string]Tool
	handlers map[string]ToolHandler
	mu       sync.RWMutex
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:    make(map[string]Tool),
		handlers: make(map[string]ToolHandler),
	}
}

// Register registers a tool with its handler.
func (r *ToolRegistry) Register(tool Tool, handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools[tool.Name] = tool
	r.handlers[tool.Name] = handler
}

// Unregister removes a tool.
func (r *ToolRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.tools, name)
	delete(r.handlers, name)
}

// List returns all registered tools.
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}

	return tools
}

// Get returns a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[name]

	return tool, ok
}

// Call calls a tool by name with the given arguments.
func (r *ToolRegistry) Call(ctx context.Context, name string, args map[string]any) ([]ToolResultContent, error) {
	r.mu.RLock()
	handler, ok := r.handlers[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}

	return handler(ctx, args)
}

// PromptRegistry manages registered prompts.
type PromptRegistry struct {
	prompts  map[string]Prompt
	handlers map[string]PromptHandler
	mu       sync.RWMutex
}

// NewPromptRegistry creates a new prompt registry.
func NewPromptRegistry() *PromptRegistry {
	return &PromptRegistry{
		prompts:  make(map[string]Prompt),
		handlers: make(map[string]PromptHandler),
	}
}

// Register registers a prompt with its handler.
func (r *PromptRegistry) Register(prompt Prompt, handler PromptHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.prompts[prompt.Name] = prompt
	r.handlers[prompt.Name] = handler
}

// Unregister removes a prompt.
func (r *PromptRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.prompts, name)
	delete(r.handlers, name)
}

// List returns all registered prompts.
func (r *PromptRegistry) List() []Prompt {
	r.mu.RLock()
	defer r.mu.RUnlock()

	prompts := make([]Prompt, 0, len(r.prompts))
	for _, prompt := range r.prompts {
		prompts = append(prompts, prompt)
	}

	return prompts
}

// Get retrieves a prompt by name with the given arguments.
func (r *PromptRegistry) Get(ctx context.Context, name string, args map[string]any) (*GetPromptResponse, error) {
	r.mu.RLock()
	handler, ok := r.handlers[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("prompt not found: %s", name)
	}

	return handler(ctx, args)
}

// TextResourceHandler creates a handler for a static text resource.
func TextResourceHandler(text, mimeType string) ResourceHandler {
	return func(ctx context.Context, uri string) (*ResourceContent, error) {
		return &ResourceContent{
			URI:      uri,
			MimeType: mimeType,
			Text:     text,
		}, nil
	}
}

// FileResourceHandler creates a handler that reads from a file provider.
type FileResourceProvider interface {
	ReadFile(path string) ([]byte, error)
}

// FunctionToolHandler creates a handler from a function.
func FunctionToolHandler(fn func(ctx context.Context, args map[string]any) (string, error)) ToolHandler {
	return func(ctx context.Context, args map[string]any) ([]ToolResultContent, error) {
		result, err := fn(ctx, args)
		if err != nil {
			return nil, err
		}

		return []ToolResultContent{{
			Type: "text",
			Text: result,
		}}, nil
	}
}

// SimplePromptHandler creates a handler for a simple template prompt.
func SimplePromptHandler(template string, description string) PromptHandler {
	return func(ctx context.Context, args map[string]any) (*GetPromptResponse, error) {
		// Simple template substitution
		text := template

		for key, value := range args {
			placeholder := fmt.Sprintf("{{%s}}", key)
			text = replaceAll(text, placeholder, fmt.Sprint(value))
		}

		return &GetPromptResponse{
			Description: description,
			Messages: []PromptMessage{{
				Role: "user",
				Content: PromptMessageContent{
					Type: "text",
					Text: text,
				},
			}},
		}, nil
	}
}

func replaceAll(s, old, new string) string {
	for {
		idx := indexOf(s, old)
		if idx == -1 {
			break
		}

		s = s[:idx] + new + s[idx+len(old):]
	}

	return s
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}

	return -1
}

// ToolBuilder helps build MCP tools.
type ToolBuilder struct {
	name        string
	description string
	properties  map[string]PropertySchema
	required    []string
}

// PropertySchema describes a tool parameter.
type PropertySchema struct {
	Type        string      `json:"type"`
	Description string      `json:"description,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Default     interface{} `json:"default,omitempty"`
}

// NewToolBuilder creates a new tool builder.
func NewToolBuilder(name string) *ToolBuilder {
	return &ToolBuilder{
		name:       name,
		properties: make(map[string]PropertySchema),
	}
}

// Description sets the tool description.
func (b *ToolBuilder) Description(desc string) *ToolBuilder {
	b.description = desc

	return b
}

// StringParam adds a string parameter.
func (b *ToolBuilder) StringParam(name, description string, required bool) *ToolBuilder {
	b.properties[name] = PropertySchema{
		Type:        "string",
		Description: description,
	}
	if required {
		b.required = append(b.required, name)
	}

	return b
}

// IntParam adds an integer parameter.
func (b *ToolBuilder) IntParam(name, description string, required bool) *ToolBuilder {
	b.properties[name] = PropertySchema{
		Type:        "integer",
		Description: description,
	}
	if required {
		b.required = append(b.required, name)
	}

	return b
}

// BoolParam adds a boolean parameter.
func (b *ToolBuilder) BoolParam(name, description string, required bool) *ToolBuilder {
	b.properties[name] = PropertySchema{
		Type:        "boolean",
		Description: description,
	}
	if required {
		b.required = append(b.required, name)
	}

	return b
}

// EnumParam adds an enum parameter.
func (b *ToolBuilder) EnumParam(name, description string, values []string, required bool) *ToolBuilder {
	b.properties[name] = PropertySchema{
		Type:        "string",
		Description: description,
		Enum:        values,
	}
	if required {
		b.required = append(b.required, name)
	}

	return b
}

// Build creates the tool.
func (b *ToolBuilder) Build() Tool {
	schema := map[string]any{
		"type":       "object",
		"properties": b.properties,
	}
	if len(b.required) > 0 {
		schema["required"] = b.required
	}

	schemaBytes, _ := json.Marshal(schema)

	return Tool{
		Name:        b.name,
		Description: b.description,
		InputSchema: schemaBytes,
	}
}

// MCPResourceAdapter adapts MCP resources for SDK use.
type MCPResourceAdapter struct {
	client *Client
	uri    string
}

// NewMCPResourceAdapter creates a new resource adapter.
func NewMCPResourceAdapter(client *Client, uri string) *MCPResourceAdapter {
	return &MCPResourceAdapter{
		client: client,
		uri:    uri,
	}
}

// Read reads the resource content.
func (a *MCPResourceAdapter) Read(ctx context.Context) (string, error) {
	contents, err := a.client.ReadResource(ctx, a.uri)
	if err != nil {
		return "", err
	}

	if len(contents) == 0 {
		return "", nil
	}

	// Return the first text content
	for _, content := range contents {
		if content.Text != "" {
			return content.Text, nil
		}
	}

	return "", nil
}

// MCPPromptAdapter adapts MCP prompts for SDK use.
type MCPPromptAdapter struct {
	client     *Client
	promptName string
}

// NewMCPPromptAdapter creates a new prompt adapter.
func NewMCPPromptAdapter(client *Client, promptName string) *MCPPromptAdapter {
	return &MCPPromptAdapter{
		client:     client,
		promptName: promptName,
	}
}

// Get retrieves the prompt with the given arguments.
func (a *MCPPromptAdapter) Get(ctx context.Context, args map[string]any) ([]PromptMessage, error) {
	resp, err := a.client.GetPrompt(ctx, a.promptName, args)
	if err != nil {
		return nil, err
	}

	return resp.Messages, nil
}

// ToUserMessage converts the prompt to a simple user message.
func (a *MCPPromptAdapter) ToUserMessage(ctx context.Context, args map[string]any) (string, error) {
	messages, err := a.Get(ctx, args)
	if err != nil {
		return "", err
	}

	var result string
	var resultSb471 strings.Builder

	for _, msg := range messages {
		if msg.Role == "user" && msg.Content.Type == "text" {
			resultSb471.WriteString(msg.Content.Text)
		}
	}
	result += resultSb471.String()

	return result, nil
}
