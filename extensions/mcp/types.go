package mcp

// MCP (Model Context Protocol) types based on Anthropic's specification

// Tool represents an MCP tool that can be called by AI assistants.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema *JSONSchema `json:"inputSchema"`
}

// ToolCall represents a request to execute a tool.
type ToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content represents a piece of content in MCP.
type Content struct {
	Type     string `json:"type"` // "text", "image", "resource"
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// Resource represents an MCP resource (data access).
type Resource struct {
	URI         string      `json:"uri"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	MimeType    string      `json:"mimeType,omitempty"`
	Schema      *JSONSchema `json:"schema,omitempty"`
}

// ResourceContents represents the contents of a resource.
type ResourceContents struct {
	URI      string    `json:"uri"`
	Contents []Content `json:"contents"`
}

// Prompt represents an MCP prompt template.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument represents an argument for a prompt.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptMessage represents a message in a prompt result.
type PromptMessage struct {
	Role    string    `json:"role"` // "user" or "assistant"
	Content []Content `json:"content"`
}

// JSONSchema represents a JSON Schema for input/output validation.
type JSONSchema struct {
	Type                 string                 `json:"type,omitempty"`
	Properties           map[string]*JSONSchema `json:"properties,omitempty"`
	Items                *JSONSchema            `json:"items,omitempty"`
	Required             []string               `json:"required,omitempty"`
	Description          string                 `json:"description,omitempty"`
	Enum                 []any                  `json:"enum,omitempty"`
	Format               string                 `json:"format,omitempty"`
	Pattern              string                 `json:"pattern,omitempty"`
	MinLength            *int                   `json:"minLength,omitempty"`
	MaxLength            *int                   `json:"maxLength,omitempty"`
	Minimum              *float64               `json:"minimum,omitempty"`
	Maximum              *float64               `json:"maximum,omitempty"`
	AdditionalProperties any                    `json:"additionalProperties,omitempty"`
}

// ServerInfo represents MCP server information.
type ServerInfo struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Capabilities Capabilities `json:"capabilities"`
}

// Capabilities represents MCP server capabilities.
type Capabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// ToolsCapability represents tools capability.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability represents resources capability.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability represents prompts capability.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ListToolsResponse represents the response for listing tools.
type ListToolsResponse struct {
	Tools []Tool `json:"tools"`
}

// CallToolRequest represents a request to call a tool.
type CallToolRequest struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CallToolResponse represents the response from calling a tool.
type CallToolResponse struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// ListResourcesResponse represents the response for listing resources.
type ListResourcesResponse struct {
	Resources []Resource `json:"resources"`
}

// ReadResourceRequest represents a request to read a resource.
type ReadResourceRequest struct {
	URI string `json:"uri"`
}

// ReadResourceResponse represents the response from reading a resource.
type ReadResourceResponse struct {
	Contents []Content `json:"contents"`
}

// ListPromptsResponse represents the response for listing prompts.
type ListPromptsResponse struct {
	Prompts []Prompt `json:"prompts"`
}

// GetPromptRequest represents a request to get a prompt.
type GetPromptRequest struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// GetPromptResponse represents the response from getting a prompt.
type GetPromptResponse struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}
