package golang

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// ErrorGenerator generates Go error types.
type ErrorGenerator struct{}

// NewErrorGenerator creates a new error generator.
func NewErrorGenerator() *ErrorGenerator {
	return &ErrorGenerator{}
}

// Generate generates error types for Go.
func (e *ErrorGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))

	buf.WriteString("import (\n")
	buf.WriteString("\t\"encoding/json\"\n")
	buf.WriteString("\t\"fmt\"\n")
	buf.WriteString(")\n\n")

	// Base APIError type
	buf.WriteString("// APIError represents a generic API error.\n")
	buf.WriteString("type APIError struct {\n")
	buf.WriteString("\tStatusCode int                    `json:\"statusCode\"`\n")
	buf.WriteString("\tMessage    string                 `json:\"message\"`\n")
	buf.WriteString("\tCode       string                 `json:\"code,omitempty\"`\n")
	buf.WriteString("\tDetails    map[string]interface{} `json:\"details,omitempty\"`\n")
	buf.WriteString("}\n\n")

	// Error method to implement error interface
	buf.WriteString("// Error implements the error interface.\n")
	buf.WriteString("func (e *APIError) Error() string {\n")
	buf.WriteString("\treturn fmt.Sprintf(\"API error [%d]: %s\", e.StatusCode, e.Message)\n")
	buf.WriteString("}\n\n")

	// MarshalJSON for clean serialization
	buf.WriteString("// MarshalJSON implements json.Marshaler.\n")
	buf.WriteString("func (e *APIError) MarshalJSON() ([]byte, error) {\n")
	buf.WriteString("\treturn json.Marshal(struct {\n")
	buf.WriteString("\t\tStatusCode int                    `json:\"statusCode\"`\n")
	buf.WriteString("\t\tMessage    string                 `json:\"message\"`\n")
	buf.WriteString("\t\tCode       string                 `json:\"code,omitempty\"`\n")
	buf.WriteString("\t\tDetails    map[string]interface{} `json:\"details,omitempty\"`\n")
	buf.WriteString("\t}{\n")
	buf.WriteString("\t\tStatusCode: e.StatusCode,\n")
	buf.WriteString("\t\tMessage:    e.Message,\n")
	buf.WriteString("\t\tCode:       e.Code,\n")
	buf.WriteString("\t\tDetails:    e.Details,\n")
	buf.WriteString("\t})\n")
	buf.WriteString("}\n\n")

	// Specific error types
	errorTypes := []struct {
		name       string
		statusCode int
		message    string
	}{
		{"ValidationError", 400, "validation failed"},
		{"UnauthorizedError", 401, "authentication required"},
		{"ForbiddenError", 403, "access forbidden"},
		{"NotFoundError", 404, "resource not found"},
		{"MethodNotAllowedError", 405, "method not allowed"},
		{"ConflictError", 409, "resource conflict"},
		{"TooManyRequestsError", 429, "too many requests"},
		{"ServerError", 500, "internal server error"},
		{"BadGatewayError", 502, "bad gateway"},
		{"ServiceUnavailableError", 503, "service unavailable"},
		{"GatewayTimeoutError", 504, "gateway timeout"},
	}

	for _, et := range errorTypes {
		buf.WriteString(fmt.Sprintf("// %s represents a %d error.\n", et.name, et.statusCode))
		buf.WriteString(fmt.Sprintf("type %s struct {\n", et.name))
		buf.WriteString("\t*APIError\n")
		buf.WriteString("}\n\n")

		buf.WriteString(fmt.Sprintf("// New%s creates a new %s.\n", et.name, et.name))
		buf.WriteString(fmt.Sprintf("func New%s(message string, code string, details map[string]interface{}) *%s {\n", et.name, et.name))
		buf.WriteString("\tif message == \"\" {\n")
		buf.WriteString(fmt.Sprintf("\t\tmessage = \"%s\"\n", et.message))
		buf.WriteString("\t}\n")
		buf.WriteString(fmt.Sprintf("\treturn &%s{\n", et.name))
		buf.WriteString("\t\tAPIError: &APIError{\n")
		buf.WriteString(fmt.Sprintf("\t\t\tStatusCode: %d,\n", et.statusCode))
		buf.WriteString("\t\t\tMessage:    message,\n")
		buf.WriteString("\t\t\tCode:       code,\n")
		buf.WriteString("\t\t\tDetails:    details,\n")
		buf.WriteString("\t\t},\n")
		buf.WriteString("\t}\n")
		buf.WriteString("}\n\n")
	}

	// Error factory function
	buf.WriteString("// NewAPIError creates an appropriate error type based on status code.\n")
	buf.WriteString("func NewAPIError(statusCode int, message, code string, details map[string]interface{}) error {\n")
	buf.WriteString("\tswitch statusCode {\n")

	for _, et := range errorTypes {
		buf.WriteString(fmt.Sprintf("\tcase %d:\n", et.statusCode))
		buf.WriteString(fmt.Sprintf("\t\treturn New%s(message, code, details)\n", et.name))
	}

	buf.WriteString("\tdefault:\n")
	buf.WriteString("\t\tif statusCode >= 400 && statusCode < 500 {\n")
	buf.WriteString("\t\t\treturn &APIError{\n")
	buf.WriteString("\t\t\t\tStatusCode: statusCode,\n")
	buf.WriteString("\t\t\t\tMessage:    message,\n")
	buf.WriteString("\t\t\t\tCode:       code,\n")
	buf.WriteString("\t\t\t\tDetails:    details,\n")
	buf.WriteString("\t\t\t}\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t\tif statusCode >= 500 {\n")
	buf.WriteString("\t\t\treturn NewServerError(message, code, details)\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t\treturn &APIError{\n")
	buf.WriteString("\t\t\tStatusCode: statusCode,\n")
	buf.WriteString("\t\t\tMessage:    message,\n")
	buf.WriteString("\t\t\tCode:       code,\n")
	buf.WriteString("\t\t\tDetails:    details,\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	// Type assertion helpers
	buf.WriteString("// IsAPIError checks if an error is an APIError.\n")
	buf.WriteString("func IsAPIError(err error) bool {\n")
	buf.WriteString("\t_, ok := err.(*APIError)\n")
	buf.WriteString("\treturn ok\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// IsClientError checks if an error is a client error (4xx).\n")
	buf.WriteString("func IsClientError(err error) bool {\n")
	buf.WriteString("\tif apiErr, ok := err.(*APIError); ok {\n")
	buf.WriteString("\t\treturn apiErr.StatusCode >= 400 && apiErr.StatusCode < 500\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\treturn false\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// IsServerError checks if an error is a server error (5xx).\n")
	buf.WriteString("func IsServerError(err error) bool {\n")
	buf.WriteString("\tif apiErr, ok := err.(*APIError); ok {\n")
	buf.WriteString("\t\treturn apiErr.StatusCode >= 500\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\treturn false\n")
	buf.WriteString("}\n")

	return buf.String()
}

