// Package mcp provides Model Context Protocol (MCP) integration for the AI SDK.
// MCP enables AI systems to securely connect to external data sources and tools.
package mcp

import (
	"encoding/json"
	"time"
)

// Protocol version.
const ProtocolVersion = "2024-11-05"

// MessageType represents the type of MCP message.
type MessageType string

const (
	MessageTypeRequest      MessageType = "request"
	MessageTypeResponse     MessageType = "response"
	MessageTypeNotification MessageType = "notification"
	MessageTypeError        MessageType = "error"
)

// Method represents an MCP method.
type Method string

const (
	// Initialization methods.
	MethodInitialize Method = "initialize"
	MethodShutdown   Method = "shutdown"

	// Resource methods.
	MethodListResources       Method = "resources/list"
	MethodReadResource        Method = "resources/read"
	MethodSubscribeResource   Method = "resources/subscribe"
	MethodUnsubscribeResource Method = "resources/unsubscribe"

	// Tool methods.
	MethodListTools Method = "tools/list"
	MethodCallTool  Method = "tools/call"

	// Prompt methods.
	MethodListPrompts Method = "prompts/list"
	MethodGetPrompt   Method = "prompts/get"

	// Logging methods.
	MethodSetLogLevel Method = "logging/setLevel"

	// Notification methods.
	MethodResourceUpdated     Method = "notifications/resources/updated"
	MethodResourceListChanged Method = "notifications/resources/list_changed"
	MethodToolListChanged     Method = "notifications/tools/list_changed"
	MethodPromptListChanged   Method = "notifications/prompts/list_changed"
	MethodProgress            Method = "notifications/progress"
	MethodLog                 Method = "notifications/message"
)

// Message is the base MCP message structure.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  Method          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorResponse  `json:"error,omitempty"`
}

// ErrorResponse represents an MCP error.
type ErrorResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Standard error codes.
const (
	ErrorCodeParse            = -32700
	ErrorCodeInvalidRequest   = -32600
	ErrorCodeMethodNotFound   = -32601
	ErrorCodeInvalidParams    = -32602
	ErrorCodeInternal         = -32603
	ErrorCodeResourceNotFound = -32002
	ErrorCodeToolNotFound     = -32003
)

// NewMessage creates a new MCP message.
func NewMessage(method Method, id any, params any) (*Message, error) {
	msg := &Message{
		JSONRPC: "2.0",
		Method:  method,
	}

	if id != nil {
		idBytes, err := json.Marshal(id)
		if err != nil {
			return nil, err
		}

		msg.ID = idBytes
	}

	if params != nil {
		paramsBytes, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}

		msg.Params = paramsBytes
	}

	return msg, nil
}

// NewResponse creates a new MCP response message.
func NewResponse(id json.RawMessage, result any) (*Message, error) {
	msg := &Message{
		JSONRPC: "2.0",
		ID:      id,
	}

	if result != nil {
		resultBytes, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}

		msg.Result = resultBytes
	}

	return msg, nil
}

// NewErrorResponse creates a new MCP error response.
func NewErrorResponse(id json.RawMessage, code int, message string) *Message {
	return &Message{
		JSONRPC: "2.0",
		ID:      id,
		Error: &ErrorResponse{
			Code:    code,
			Message: message,
		},
	}
}

// Capability represents server or client capabilities.
type Capability struct {
	Experimental map[string]any       `json:"experimental,omitempty"`
	Logging      *LoggingCapability   `json:"logging,omitempty"`
	Prompts      *PromptsCapability   `json:"prompts,omitempty"`
	Resources    *ResourcesCapability `json:"resources,omitempty"`
	Tools        *ToolsCapability     `json:"tools,omitempty"`
}

// LoggingCapability represents logging capabilities.
type LoggingCapability struct{}

// PromptsCapability represents prompts capabilities.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability represents resources capabilities.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// ToolsCapability represents tools capabilities.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// Implementation describes the server or client implementation.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeRequest is the initialize request parameters.
type InitializeRequest struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    Capability     `json:"capabilities"`
	ClientInfo      Implementation `json:"clientInfo"`
}

// InitializeResponse is the initialize response.
type InitializeResponse struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    Capability     `json:"capabilities"`
	ServerInfo      Implementation `json:"serverInfo"`
	Instructions    string         `json:"instructions,omitempty"`
}

// Resource represents an MCP resource.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceContent represents the content of a resource.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"` // Base64 encoded
}

// ListResourcesResponse is the response for listing resources.
type ListResourcesResponse struct {
	Resources  []Resource `json:"resources"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

// ReadResourceRequest is the request for reading a resource.
type ReadResourceRequest struct {
	URI string `json:"uri"`
}

// ReadResourceResponse is the response for reading a resource.
type ReadResourceResponse struct {
	Contents []ResourceContent `json:"contents"`
}

// ResourceTemplate represents a resource template.
type ResourceTemplate struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Tool represents an MCP tool.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ListToolsResponse is the response for listing tools.
type ListToolsResponse struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// CallToolRequest is the request for calling a tool.
type CallToolRequest struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CallToolResponse is the response for calling a tool.
type CallToolResponse struct {
	Content []ToolResultContent `json:"content"`
	IsError bool                `json:"isError,omitempty"`
}

// ToolResultContent represents content in a tool result.
type ToolResultContent struct {
	Type     string           `json:"type"` // "text", "image", "resource"
	Text     string           `json:"text,omitempty"`
	MimeType string           `json:"mimeType,omitempty"`
	Data     string           `json:"data,omitempty"` // Base64 for images
	Resource *ResourceContent `json:"resource,omitempty"`
}

// Prompt represents an MCP prompt.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument represents a prompt argument.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// ListPromptsResponse is the response for listing prompts.
type ListPromptsResponse struct {
	Prompts    []Prompt `json:"prompts"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

// GetPromptRequest is the request for getting a prompt.
type GetPromptRequest struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// GetPromptResponse is the response for getting a prompt.
type GetPromptResponse struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// PromptMessage represents a message in a prompt.
type PromptMessage struct {
	Role    string               `json:"role"`
	Content PromptMessageContent `json:"content"`
}

// PromptMessageContent represents content in a prompt message.
type PromptMessageContent struct {
	Type     string           `json:"type"` // "text", "image", "resource"
	Text     string           `json:"text,omitempty"`
	MimeType string           `json:"mimeType,omitempty"`
	Data     string           `json:"data,omitempty"`
	Resource *ResourceContent `json:"resource,omitempty"`
}

// LogLevel represents a log level.
type LogLevel string

const (
	LogLevelDebug     LogLevel = "debug"
	LogLevelInfo      LogLevel = "info"
	LogLevelNotice    LogLevel = "notice"
	LogLevelWarning   LogLevel = "warning"
	LogLevelError     LogLevel = "error"
	LogLevelCritical  LogLevel = "critical"
	LogLevelAlert     LogLevel = "alert"
	LogLevelEmergency LogLevel = "emergency"
)

// SetLogLevelRequest is the request for setting log level.
type SetLogLevelRequest struct {
	Level LogLevel `json:"level"`
}

// LogNotification is a log notification.
type LogNotification struct {
	Level  LogLevel `json:"level"`
	Logger string   `json:"logger,omitempty"`
	Data   any      `json:"data"`
}

// ProgressNotification is a progress notification.
type ProgressNotification struct {
	ProgressToken string  `json:"progressToken"`
	Progress      float64 `json:"progress"`
	Total         float64 `json:"total,omitempty"`
}

// ResourceUpdatedNotification is a resource updated notification.
type ResourceUpdatedNotification struct {
	URI string `json:"uri"`
}

// SubscribeRequest is a subscribe request.
type SubscribeRequest struct {
	URI string `json:"uri"`
}

// UnsubscribeRequest is an unsubscribe request.
type UnsubscribeRequest struct {
	URI string `json:"uri"`
}

// ListRequest is a generic list request with optional cursor.
type ListRequest struct {
	Cursor string `json:"cursor,omitempty"`
}

// ConnectionState represents the state of an MCP connection.
type ConnectionState string

const (
	ConnectionStateDisconnected ConnectionState = "disconnected"
	ConnectionStateConnecting   ConnectionState = "connecting"
	ConnectionStateConnected    ConnectionState = "connected"
	ConnectionStateError        ConnectionState = "error"
)

// ConnectionInfo contains information about an MCP connection.
type ConnectionInfo struct {
	State        ConnectionState
	ServerInfo   *Implementation
	Capabilities *Capability
	ConnectedAt  time.Time
	LastError    error
}
