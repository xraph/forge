package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// errorClass is one generated subclass of APIError.
type errorClass struct {
	name       string
	statusCode int
	message    string
}

// errorClasses is the taxonomy emitted into errors.ts.
//
// Package-level rather than a local in Generate so ReservedIdentifiers can read
// the same list the emitter writes from. A second copy would drift, and the way
// it would drift is silent: a class added here but missing there becomes a name
// a stripped schema is allowed to collide with.
var errorClasses = []errorClass{
	{"ValidationError", 400, "Validation failed"},
	{"UnauthorizedError", 401, "Authentication required"},
	{"ForbiddenError", 403, "Access forbidden"},
	{"NotFoundError", 404, "Resource not found"},
	{"MethodNotAllowedError", 405, "Method not allowed"},
	{"ConflictError", 409, "Resource conflict"},
	{"TooManyRequestsError", 429, "Too many requests"},
	{"ServerError", 500, "Internal server error"},
	{"BadGatewayError", 502, "Bad gateway"},
	{"ServiceUnavailableError", 503, "Service unavailable"},
	{"GatewayTimeoutError", 504, "Gateway timeout"},
}

// runtimeIdentifiers are the non-schema names the generated package exports
// outside errors.ts: the transport, the codec table and the manifest.
//
// index.ts re-exports every generated file with `export *`, so a schema whose
// name matches one of these produces an ambiguous re-export and a package that
// does not compile -- which is exactly what `TwinOS_ValidationError` did the
// first time a client was generated with strip_prefix set.
var runtimeIdentifiers = []string{
	// fetch.ts
	"HTTPClient", "HTTPError", "RequestConfig", "RetryConfig",
	// codecs.ts
	"CODECS", "Codec", "encode", "decode",
	// client.ts
	"Client",
	// ops.ts
	"OperationMeta", "EntityMeta", "ops", "entities", "streams",
}

// ReservedIdentifiers is every top-level name the generated TypeScript package
// exports that is NOT derived from a schema.
//
// Used by the prefix strip to decide which names it must leave alone: a schema
// that would strip onto one of these keeps its prefix, because the generated
// name is not negotiable and a prefixed schema name is unambiguous where a
// duplicated export is not.
func ReservedIdentifiers() map[string]bool {
	reserved := make(map[string]bool, len(errorClasses)+len(runtimeIdentifiers)+5)

	for _, ec := range errorClasses {
		reserved[ec.name] = true
	}

	for _, name := range runtimeIdentifiers {
		reserved[name] = true
	}

	// errors.ts's own base class and predicates, which are not in the table
	// above because they are written out rather than looped over.
	for _, name := range []string{"APIError", "createError", "isAPIError", "isClientError", "isServerError"} {
		reserved[name] = true
	}

	return reserved
}

// ErrorGenerator generates TypeScript error classes.
type ErrorGenerator struct{}

// NewErrorGenerator creates a new error generator.
func NewErrorGenerator() *ErrorGenerator {
	return &ErrorGenerator{}
}

// Generate generates error taxonomy code.
func (g *ErrorGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("// Generated error classes\n\n")

	// Base APIError class
	buf.WriteString("export class APIError extends Error {\n")
	buf.WriteString("  constructor(\n")
	buf.WriteString("    public statusCode: number,\n")
	buf.WriteString("    message: string,\n")
	buf.WriteString("    public code?: string,\n")
	buf.WriteString("    public details?: Record<string, any>\n")
	buf.WriteString("  ) {\n")
	buf.WriteString("    super(message);\n")
	buf.WriteString("    this.name = 'APIError';\n")
	buf.WriteString("    Object.setPrototypeOf(this, APIError.prototype);\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  toJSON() {\n")
	buf.WriteString("    return {\n")
	buf.WriteString("      name: this.name,\n")
	buf.WriteString("      statusCode: this.statusCode,\n")
	buf.WriteString("      message: this.message,\n")
	buf.WriteString("      code: this.code,\n")
	buf.WriteString("      details: this.details,\n")
	buf.WriteString("    };\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n\n")

	// Generate specific error classes, from the package-level table that
	// ReservedIdentifiers also reads.
	for _, ec := range errorClasses {
		buf.WriteString(fmt.Sprintf("export class %s extends APIError {\n", ec.name))
		buf.WriteString("  constructor(message?: string, code?: string, details?: Record<string, any>) {\n")
		buf.WriteString(fmt.Sprintf("    super(%d, message || '%s', code, details);\n", ec.statusCode, ec.message))
		buf.WriteString(fmt.Sprintf("    this.name = '%s';\n", ec.name))
		buf.WriteString(fmt.Sprintf("    Object.setPrototypeOf(this, %s.prototype);\n", ec.name))
		buf.WriteString("  }\n")
		buf.WriteString("}\n\n")
	}

	// Error factory function
	buf.WriteString("/**\n")
	buf.WriteString(" * Creates an appropriate error class based on status code\n")
	buf.WriteString(" */\n")
	buf.WriteString("export function createError(\n")
	buf.WriteString("  statusCode: number,\n")
	buf.WriteString("  message: string,\n")
	buf.WriteString("  code?: string,\n")
	buf.WriteString("  details?: Record<string, any>\n")
	buf.WriteString("): APIError {\n")
	buf.WriteString("  switch (statusCode) {\n")

	for _, ec := range errorClasses {
		buf.WriteString(fmt.Sprintf("    case %d:\n", ec.statusCode))
		buf.WriteString(fmt.Sprintf("      return new %s(message, code, details);\n", ec.name))
	}

	buf.WriteString("    default:\n")
	buf.WriteString("      if (statusCode >= 400 && statusCode < 500) {\n")
	buf.WriteString("        return new APIError(statusCode, message, code, details);\n")
	buf.WriteString("      }\n")
	buf.WriteString("      if (statusCode >= 500) {\n")
	buf.WriteString("        return new ServerError(message, code, details);\n")
	buf.WriteString("      }\n")
	buf.WriteString("      return new APIError(statusCode, message, code, details);\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n\n")

	// Type guard functions
	buf.WriteString("// Type guard functions\n")
	buf.WriteString("export function isAPIError(error: any): error is APIError {\n")
	buf.WriteString("  return error instanceof APIError;\n")
	buf.WriteString("}\n\n")

	buf.WriteString("export function isClientError(error: any): boolean {\n")
	buf.WriteString("  return isAPIError(error) && error.statusCode >= 400 && error.statusCode < 500;\n")
	buf.WriteString("}\n\n")

	buf.WriteString("export function isServerError(error: any): boolean {\n")
	buf.WriteString("  return isAPIError(error) && error.statusCode >= 500;\n")
	buf.WriteString("}\n")

	return buf.String()
}
