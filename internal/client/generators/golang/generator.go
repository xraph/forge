package golang

import (
	"context"
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators"
)

// Generator generates Go clients
type Generator struct {
	typesGen     *TypesGenerator
	restGen      *RESTGenerator
	websocketGen *WebSocketGenerator
	sseGen       *SSEGenerator
}

// NewGenerator creates a new Go generator
func NewGenerator() generators.LanguageGenerator {
	return &Generator{
		typesGen:     NewTypesGenerator(),
		restGen:      NewRESTGenerator(),
		websocketGen: NewWebSocketGenerator(),
		sseGen:       NewSSEGenerator(),
	}
}

// Name returns the generator name
func (g *Generator) Name() string {
	return "go"
}

// SupportedFeatures returns supported features
func (g *Generator) SupportedFeatures() []string {
	return []string{
		generators.FeatureREST,
		generators.FeatureWebSocket,
		generators.FeatureSSE,
		generators.FeatureWebTransport,
		generators.FeatureAuth,
		generators.FeatureReconnection,
		generators.FeatureHeartbeat,
		generators.FeatureStateManagement,
		generators.FeatureTypedErrors,
		generators.FeatureRequestRetry,
		generators.FeatureTimeout,
		generators.FeaturePolymorphicTypes,
	}
}

// Validate validates the spec for Go generation
func (g *Generator) Validate(specIface generators.APISpec) error {
	spec, ok := specIface.(*client.APISpec)
	if !ok || spec == nil {
		return fmt.Errorf("spec is nil or invalid type")
	}

	if spec.Info.Title == "" {
		return fmt.Errorf("API title is required")
	}

	return nil
}

// Generate generates the Go client
func (g *Generator) Generate(ctx context.Context, specIface generators.APISpec, configIface generators.GeneratorConfig) (*generators.GeneratedClient, error) {
	spec, ok := specIface.(*client.APISpec)
	if !ok || spec == nil {
		return nil, fmt.Errorf("spec is nil or invalid type")
	}

	config, ok := configIface.(client.GeneratorConfig)
	if !ok {
		return nil, fmt.Errorf("config is invalid type")
	}
	genClient := &generators.GeneratedClient{
		Files:        make(map[string]string),
		Language:     "go",
		Version:      config.Version,
		Dependencies: g.getDependencies(config),
	}

	// Generate client.go (main client with auth config)
	clientCode := g.generateClientFile(spec, config)
	genClient.Files["client.go"] = clientCode

	// Generate types.go
	typesCode := g.typesGen.Generate(spec, config)
	genClient.Files["types.go"] = typesCode

	// Generate errors.go
	errorsCode := g.generateErrorsFile(spec, config)
	genClient.Files["errors.go"] = errorsCode

	// Generate REST endpoints if any
	if len(spec.Endpoints) > 0 {
		restCode := g.restGen.Generate(spec, config)
		genClient.Files["rest.go"] = restCode
	}

	// Generate WebSocket clients if any
	if len(spec.WebSockets) > 0 && config.IncludeStreaming {
		wsCode := g.websocketGen.Generate(spec, config)
		genClient.Files["websocket.go"] = wsCode
	}

	// Generate SSE clients if any
	if len(spec.SSEs) > 0 && config.IncludeStreaming {
		sseCode := g.sseGen.Generate(spec, config)
		genClient.Files["sse.go"] = sseCode
	}

	// Generate WebTransport clients if any
	if len(spec.WebTransports) > 0 && config.IncludeStreaming {
		wtGen := NewWebTransportGenerator()
		wtCode := wtGen.Generate(spec, config)
		genClient.Files["webtransport.go"] = wtCode
	}

	// Generate go.mod
	if config.Module != "" {
		goModCode := g.generateGoMod(config)
		genClient.Files["go.mod"] = goModCode
	}

	// Generate instructions
	genClient.Instructions = g.generateInstructions(spec, config)

	return genClient, nil
}

// generateClientFile generates the main client.go file
func (g *Generator) generateClientFile(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Package declaration
	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))

	// Imports
	buf.WriteString("import (\n")
	buf.WriteString("\t\"context\"\n")
	buf.WriteString("\t\"fmt\"\n")
	buf.WriteString("\t\"net/http\"\n")
	buf.WriteString("\t\"time\"\n")

	if config.Features.Logging {
		buf.WriteString("\t\"log\"\n")
	}

	buf.WriteString(")\n\n")

	// Client struct
	buf.WriteString("// Client is the main API client\n")
	buf.WriteString("type Client struct {\n")
	buf.WriteString("\thttpClient *http.Client\n")
	buf.WriteString("\tbaseURL    string\n")

	if config.IncludeAuth && client.NeedsAuthConfig(spec) {
		buf.WriteString("\tauth       *AuthConfig\n")
	}

	if config.Features.Logging {
		buf.WriteString("\tlogger     Logger\n")
	}

	buf.WriteString("}\n\n")

	// AuthConfig struct if needed
	if config.IncludeAuth && client.NeedsAuthConfig(spec) {
		buf.WriteString(g.generateAuthConfig(spec))
	}

	// ClientOption type
	buf.WriteString("// ClientOption configures the client\n")
	buf.WriteString("type ClientOption func(*Client)\n\n")

	// NewClient function
	buf.WriteString("// NewClient creates a new API client\n")
	buf.WriteString("func NewClient(opts ...ClientOption) *Client {\n")
	buf.WriteString("\tc := &Client{\n")
	buf.WriteString("\t\thttpClient: &http.Client{\n")
	buf.WriteString("\t\t\tTimeout: 30 * time.Second,\n")
	buf.WriteString("\t\t},\n")

	if config.BaseURL != "" {
		buf.WriteString(fmt.Sprintf("\t\tbaseURL: \"%s\",\n", config.BaseURL))
	}

	buf.WriteString("\t}\n\n")
	buf.WriteString("\tfor _, opt := range opts {\n")
	buf.WriteString("\t\topt(c)\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\treturn c\n")
	buf.WriteString("}\n\n")

	// Client options
	buf.WriteString("// WithBaseURL sets the base URL\n")
	buf.WriteString("func WithBaseURL(url string) ClientOption {\n")
	buf.WriteString("\treturn func(c *Client) {\n")
	buf.WriteString("\t\tc.baseURL = url\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// WithHTTPClient sets a custom HTTP client\n")
	buf.WriteString("func WithHTTPClient(client *http.Client) ClientOption {\n")
	buf.WriteString("\treturn func(c *Client) {\n")
	buf.WriteString("\t\tc.httpClient = client\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	if config.Features.Timeout {
		buf.WriteString("// WithTimeout sets the request timeout\n")
		buf.WriteString("func WithTimeout(timeout time.Duration) ClientOption {\n")
		buf.WriteString("\treturn func(c *Client) {\n")
		buf.WriteString("\t\tc.httpClient.Timeout = timeout\n")
		buf.WriteString("\t}\n")
		buf.WriteString("}\n\n")
	}

	if config.IncludeAuth && client.NeedsAuthConfig(spec) {
		buf.WriteString("// WithAuth sets the authentication configuration\n")
		buf.WriteString("func WithAuth(auth AuthConfig) ClientOption {\n")
		buf.WriteString("\treturn func(c *Client) {\n")
		buf.WriteString("\t\tc.auth = &auth\n")
		buf.WriteString("\t}\n")
		buf.WriteString("}\n\n")
	}

	// Helper methods
	buf.WriteString(g.generateHelperMethods(config))

	return buf.String()
}

// generateAuthConfig generates the auth configuration struct
func (g *Generator) generateAuthConfig(spec *client.APISpec) string {
	var buf strings.Builder

	authGen := client.NewAuthCodeGenerator()
	schemes := authGen.DetectAuthSchemes(spec)

	buf.WriteString("// AuthConfig holds authentication configuration\n")
	buf.WriteString("type AuthConfig struct {\n")

	for _, scheme := range schemes {
		switch scheme.Type {
		case "http":
			if scheme.Scheme == "bearer" {
				buf.WriteString("\tBearerToken string\n")
			} else if scheme.Scheme == "basic" {
				buf.WriteString("\tBasicUsername string\n")
				buf.WriteString("\tBasicPassword string\n")
			}
		case "apiKey":
			buf.WriteString(fmt.Sprintf("\tAPIKey string // %s in %s\n", scheme.Name, scheme.In))
		}
	}

	buf.WriteString("\tCustomHeaders map[string]string\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateHelperMethods generates helper methods for the client
func (g *Generator) generateHelperMethods(config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("// buildURL builds a full URL from a path\n")
	buf.WriteString("func (c *Client) buildURL(path string) string {\n")
	buf.WriteString("\treturn c.baseURL + path\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// addAuth adds authentication to the request\n")
	buf.WriteString("func (c *Client) addAuth(req *http.Request) {\n")
	buf.WriteString("\tif c.auth == nil {\n")
	buf.WriteString("\t\treturn\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\tif c.auth.BearerToken != \"\" {\n")
	buf.WriteString("\t\treq.Header.Set(\"Authorization\", \"Bearer \"+c.auth.BearerToken)\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\tif c.auth.APIKey != \"\" {\n")
	buf.WriteString("\t\treq.Header.Set(\"X-API-Key\", c.auth.APIKey)\n")
	buf.WriteString("\t}\n\n")
	buf.WriteString("\tfor key, value := range c.auth.CustomHeaders {\n")
	buf.WriteString("\t\treq.Header.Set(key, value)\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateErrorsFile generates the errors.go file
func (g *Generator) generateErrorsFile(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))

	buf.WriteString("import (\n")
	buf.WriteString("\t\"fmt\"\n")
	buf.WriteString(")\n\n")

	buf.WriteString("// APIError represents an API error\n")
	buf.WriteString("type APIError struct {\n")
	buf.WriteString("\tStatusCode int\n")
	buf.WriteString("\tMessage    string\n")
	buf.WriteString("\tDetails    map[string]interface{}\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// Error implements the error interface\n")
	buf.WriteString("func (e *APIError) Error() string {\n")
	buf.WriteString("\treturn fmt.Sprintf(\"API error %d: %s\", e.StatusCode, e.Message)\n")
	buf.WriteString("}\n\n")

	if config.Features.TypedErrors {
		buf.WriteString("// Common error types\n")
		buf.WriteString("var (\n")
		buf.WriteString("\tErrBadRequest          = &APIError{StatusCode: 400, Message: \"Bad Request\"}\n")
		buf.WriteString("\tErrUnauthorized        = &APIError{StatusCode: 401, Message: \"Unauthorized\"}\n")
		buf.WriteString("\tErrForbidden           = &APIError{StatusCode: 403, Message: \"Forbidden\"}\n")
		buf.WriteString("\tErrNotFound            = &APIError{StatusCode: 404, Message: \"Not Found\"}\n")
		buf.WriteString("\tErrInternalServerError = &APIError{StatusCode: 500, Message: \"Internal Server Error\"}\n")
		buf.WriteString(")\n\n")
	}

	return buf.String()
}

// generateGoMod generates the go.mod file
func (g *Generator) generateGoMod(config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("module %s\n\n", config.Module))
	buf.WriteString("go 1.24.0\n\n")

	if config.IncludeStreaming {
		buf.WriteString("require (\n")
		buf.WriteString("\tgithub.com/gorilla/websocket v1.5.0\n")
		buf.WriteString(")\n")
	}

	return buf.String()
}

// getDependencies returns the list of dependencies
func (g *Generator) getDependencies(config client.GeneratorConfig) []generators.Dependency {
	deps := []generators.Dependency{}

	if config.IncludeStreaming {
		deps = append(deps, generators.Dependency{
			Name:    "github.com/gorilla/websocket",
			Version: "v1.5.0",
			Type:    "direct",
		})
		deps = append(deps, generators.Dependency{
			Name:    "github.com/quic-go/webtransport-go",
			Version: "v0.6.0",
			Type:    "direct",
		})
	}

	return deps
}

// generateInstructions generates setup instructions
func (g *Generator) generateInstructions(spec *client.APISpec, config client.GeneratorConfig) string {
	outputMgr := client.NewOutputManager()
	authGen := client.NewAuthCodeGenerator()

	authDocs := ""
	if config.IncludeAuth {
		schemes := authGen.DetectAuthSchemes(spec)
		authDocs = authGen.GenerateAuthDocumentation(schemes)
	}

	return outputMgr.GenerateREADME(config, spec, authDocs)
}
