package orpc

import (
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 specification types

// Request represents a JSON-RPC 2.0 request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id,omitempty"` // string, number, or null
}

// Response represents a JSON-RPC 2.0 response
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// Error represents a JSON-RPC 2.0 error
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard JSON-RPC 2.0 error codes
const (
	ErrParseError     = -32700 // Invalid JSON
	ErrInvalidRequest = -32600 // Invalid Request object
	ErrMethodNotFound = -32601 // Method not found
	ErrInvalidParams  = -32602 // Invalid method parameters
	ErrInternalError  = -32603 // Internal JSON-RPC error
	ErrServerError    = -32000 // Server error (implementation-defined)
)

// NewErrorResponse creates an error response
func NewErrorResponse(id interface{}, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		Error: &Error{
			Code:    code,
			Message: message,
		},
		ID: id,
	}
}

// NewErrorResponseWithData creates an error response with additional data
func NewErrorResponseWithData(id interface{}, code int, message string, data interface{}) *Response {
	return &Response{
		JSONRPC: "2.0",
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
		ID: id,
	}
}

// NewSuccessResponse creates a success response
func NewSuccessResponse(id interface{}, result interface{}) *Response {
	return &Response{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
}

// IsNotification returns true if this is a notification (no ID)
func (r *Request) IsNotification() bool {
	return r.ID == nil
}

// Method represents a registered JSON-RPC method
type Method struct {
	Name        string
	Description string
	Params      *ParamsSchema
	Result      *ResultSchema
	Handler     MethodHandler
	RouteInfo   interface{} // Reference to forge.RouteInfo
	Tags        []string
	Deprecated  bool
	Metadata    map[string]interface{}
}

// MethodHandler is a function that executes a JSON-RPC method
type MethodHandler func(ctx interface{}, params interface{}) (interface{}, error)

// ParamsSchema describes the parameters schema
type ParamsSchema struct {
	Type        string                     `json:"type"`
	Properties  map[string]*PropertySchema `json:"properties,omitempty"`
	Required    []string                   `json:"required,omitempty"`
	Description string                     `json:"description,omitempty"`
}

// ResultSchema describes the result schema
type ResultSchema struct {
	Type        string                     `json:"type"`
	Properties  map[string]*PropertySchema `json:"properties,omitempty"`
	Description string                     `json:"description,omitempty"`
}

// PropertySchema describes a property in a schema
type PropertySchema struct {
	Type        string                     `json:"type"`
	Description string                     `json:"description,omitempty"`
	Properties  map[string]*PropertySchema `json:"properties,omitempty"`
	Items       *PropertySchema            `json:"items,omitempty"`
	Required    []string                   `json:"required,omitempty"`
	Example     interface{}                `json:"example,omitempty"`
}

// OpenRPC types for schema generation

// OpenRPCDocument represents an OpenRPC document
type OpenRPCDocument struct {
	OpenRPC    string                 `json:"openrpc"`
	Info       *OpenRPCInfo           `json:"info"`
	Methods    []*OpenRPCMethod       `json:"methods"`
	Components *OpenRPCComponents     `json:"components,omitempty"`
	Servers    []*OpenRPCServer       `json:"servers,omitempty"`
}

// OpenRPCInfo contains API metadata
type OpenRPCInfo struct {
	Title          string                 `json:"title"`
	Version        string                 `json:"version"`
	Description    string                 `json:"description,omitempty"`
	TermsOfService string                 `json:"termsOfService,omitempty"`
	Contact        *OpenRPCContact        `json:"contact,omitempty"`
	License        *OpenRPCLicense        `json:"license,omitempty"`
}

// OpenRPCContact contains contact information
type OpenRPCContact struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

// OpenRPCLicense contains license information
type OpenRPCLicense struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// OpenRPCMethod represents a method in OpenRPC schema
type OpenRPCMethod struct {
	Name        string                 `json:"name"`
	Summary     string                 `json:"summary,omitempty"`
	Description string                 `json:"description,omitempty"`
	Tags        []*OpenRPCTag          `json:"tags,omitempty"`
	Params      []*OpenRPCParam        `json:"params,omitempty"`
	Result      *OpenRPCResult         `json:"result,omitempty"`
	Deprecated  bool                   `json:"deprecated,omitempty"`
	Errors      []*OpenRPCError        `json:"errors,omitempty"`
}

// OpenRPCParam represents a parameter in OpenRPC schema
type OpenRPCParam struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Required    bool                   `json:"required,omitempty"`
	Schema      map[string]interface{} `json:"schema"`
}

// OpenRPCResult represents a result in OpenRPC schema
type OpenRPCResult struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Schema      map[string]interface{} `json:"schema"`
}

// OpenRPCError represents an error in OpenRPC schema
type OpenRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// OpenRPCTag represents a tag for grouping methods
type OpenRPCTag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// OpenRPCComponents contains reusable schemas
type OpenRPCComponents struct {
	Schemas map[string]interface{} `json:"schemas,omitempty"`
}

// OpenRPCServer describes a server
type OpenRPCServer struct {
	Name        string `json:"name,omitempty"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// ServerStats contains server statistics
type ServerStats struct {
	TotalMethods    int64
	TotalRequests   int64
	TotalErrors     int64
	TotalBatchReqs  int64
	ActiveRequests  int64
	AverageLatency  float64
}

// String returns a string representation of the error
func (e *Error) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("JSON-RPC error %d: %s (data: %v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

