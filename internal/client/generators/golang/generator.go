package golang

import (
	"context"
	"fmt"
	"strings"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators"
)

// Generator generates Go clients.
type Generator struct {
	typesGen     *TypesGenerator
	restGen      *RESTGenerator
	websocketGen *WebSocketGenerator
	sseGen       *SSEGenerator
}

// NewGenerator creates a new Go generator.
func NewGenerator() generators.LanguageGenerator {
	return &Generator{
		typesGen:     NewTypesGenerator(),
		restGen:      NewRESTGenerator(),
		websocketGen: NewWebSocketGenerator(),
		sseGen:       NewSSEGenerator(),
	}
}

// Name returns the generator name.
func (g *Generator) Name() string {
	return "go"
}

// SupportedFeatures returns supported features.
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

// Validate validates the spec for Go generation.
func (g *Generator) Validate(specIface generators.APISpec) error {
	spec, ok := specIface.(*client.APISpec)
	if !ok || spec == nil {
		return errors.New("spec is nil or invalid type")
	}

	if spec.Info.Title == "" {
		return errors.New("API title is required")
	}

	return nil
}

// needsAuthConfig reports whether client.go will declare the AuthConfig
// type for this spec/config pair. websocket.go and webtransport.go both
// reference *AuthConfig and c.auth/ws.client.auth, so each calls this before
// emitting any of that -- referencing a type client.go never declares does
// not compile, which is exactly what a no-auth spec used to produce.
func needsAuthConfig(spec *client.APISpec, config client.GeneratorConfig) bool {
	return config.IncludeAuth && client.NeedsAuthConfig(spec)
}

// needsSharedStreaming reports whether streaming.go has a consumer in this
// spec/config pair. WebTransport is deliberately absent: webtransport.go
// carries its own state handling and refers to nothing streaming.go declares,
// so listing it here would emit a file of dead declarations for a spec whose
// only streaming transport is WebTransport.
func needsSharedStreaming(spec *client.APISpec, config client.GeneratorConfig) bool {
	if !config.IncludeStreaming {
		return false
	}

	return len(spec.WebSockets) > 0 || len(spec.SSEs) > 0
}

// Generate generates the Go client.
func (g *Generator) Generate(ctx context.Context, specIface generators.APISpec, configIface generators.GeneratorConfig) (*generators.GeneratedClient, error) {
	spec, ok := specIface.(*client.APISpec)
	if !ok || spec == nil {
		return nil, errors.New("spec is nil or invalid type")
	}

	config, ok := configIface.(client.GeneratorConfig)
	if !ok {
		return nil, errors.New("config is invalid type")
	}

	genClient := &generators.GeneratedClient{
		Files: make(map[string]string),
		// Warnings raised while the specification was being built -- a merge
		// that dropped a duplicate route, an entity whose declared id field is
		// absent from its response schema -- are carried through, exactly as
		// the TypeScript generator does. Go is the default language, so
		// dropping them here made every collision silent for anyone who did
		// not pass --language typescript.
		Warnings:     append([]string(nil), spec.Warnings...),
		Language:     "go",
		Version:      config.Version,
		Dependencies: g.getDependencies(config),
	}

	// Generate client.go (main client with auth config)
	clientCode, authWarnings := g.generateClientFile(spec, config)
	genClient.Files["client.go"] = clientCode
	genClient.Warnings = append(genClient.Warnings, authWarnings...)

	// Endpoint.CookieParams is populated by both IR builders (spec_parser.go
	// and introspector.go), but rest.go has never read it -- a non-auth
	// `in: cookie` parameter still vanishes with no trace, just one layer
	// later than before the field was added. The generator has no request
	// object to attach a cookie parameter to at call time (doRequest builds
	// one from a URL and a body, not a caller-supplied cookie jar), so this
	// warns rather than silently building a client that drops it.
	for _, ep := range spec.Endpoints {
		if len(ep.CookieParams) == 0 {
			continue
		}

		names := make([]string, len(ep.CookieParams))
		for i, p := range ep.CookieParams {
			names[i] = p.Name
		}

		genClient.Warnings = append(genClient.Warnings, fmt.Sprintf(
			"%s %s declares cookie parameter(s) %s that the generator does not emit",
			ep.Method, ep.Path, strings.Join(names, ", ")))
	}

	// Generate types.go
	typesCode := g.typesGen.Generate(spec, config)
	genClient.Files["types.go"] = typesCode

	// Generate errors.go
	errGen := NewErrorGenerator()
	errorsCode := errGen.Generate(spec, config)
	genClient.Files["errors.go"] = errorsCode

	// Generate REST endpoints if any
	if len(spec.Endpoints) > 0 {
		restCode := g.restGen.Generate(spec, config)
		genClient.Files["rest.go"] = restCode
	}

	// Generate pagination helpers if enabled
	if config.Pagination && len(spec.Endpoints) > 0 {
		paginationGen := NewPaginationGenerator()
		paginationCode := paginationGen.Generate(spec, config)
		genClient.Files["pagination.go"] = paginationCode
	}

	// Generate the declarations shared between streaming transports.
	//
	// Emitted before either consumer below and gated on the union of them,
	// not on one transport: websocket.go used to declare ConnectionState and
	// the reconnect helpers that sse.go refers to, so an SSE-only spec --
	// which skips the WebSocket branch below -- generated a client that named
	// identifiers nothing had declared.
	if needsSharedStreaming(spec, config) {
		streamingGen := NewStreamingGenerator()
		genClient.Files["streaming.go"] = streamingGen.Generate(config)
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

// generateClientFile generates the main client.go file, returning any
// warnings raised while resolving auth schemes into fields.
func (g *Generator) generateClientFile(spec *client.APISpec, config client.GeneratorConfig) (string, []string) {
	needsAuth := needsAuthConfig(spec, config)

	var (
		detected       []client.DetectedAuthScheme
		authConfigCode string
		authApplyCode  string
		authWarnings   []string
	)

	if needsAuth {
		authGen := client.NewAuthCodeGenerator()
		detected = authGen.DetectAuthSchemes(spec)

		authConfigCode, authWarnings = generateAuthConfig(detected)
		authApplyCode = generateAuthApply(detected)
	}

	// apply() always declares a *url.URL parameter (a WebSocket handshake has
	// no *http.Request to pull one from), and only reaches for base64 when a
	// basic scheme is actually present -- importing it unconditionally would
	// leave it unused whenever no scheme needs it.
	needsBase64 := strings.Contains(authApplyCode, "base64.")

	// The body is built before the import block so context/fmt can be gated
	// on whether anything in it actually ends up using them, the same way
	// needsBase64 above is decided from authApplyCode's content rather than
	// from a flag. client.go used to import both unconditionally from a time
	// when doRequest lived here; it moved to rest.go and left two dead
	// imports behind, which is a compile error the moment no other part of
	// this file happens to need them.
	body := g.generateClientBody(spec, config, needsAuth, authConfigCode, authApplyCode)

	needsContext := strings.Contains(body, "context.")
	needsFmt := strings.Contains(body, "fmt.")

	var buf strings.Builder

	// Package declaration
	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))

	// Imports
	buf.WriteString("import (\n")

	if needsContext {
		buf.WriteString("\t\"context\"\n")
	}

	if needsBase64 {
		buf.WriteString("\t\"encoding/base64\"\n")
	}

	if needsFmt {
		buf.WriteString("\t\"fmt\"\n")
	}

	buf.WriteString("\t\"net/http\"\n")
	// WithSessionJar uses this unconditionally: the login endpoint that sets
	// a session cookie is frequently absent from securitySchemes entirely,
	// so this option can't be gated on a declared cookie scheme the way
	// net/url below is gated on needsAuth.
	buf.WriteString("\t\"net/http/cookiejar\"\n")

	if needsAuth {
		buf.WriteString("\t\"net/url\"\n")
	}

	buf.WriteString("\t\"time\"\n")

	if config.Features.Logging {
		buf.WriteString("\t\"log\"\n")
	}

	buf.WriteString(")\n\n")

	buf.WriteString(body)

	return buf.String(), authWarnings
}

// generateClientBody generates everything in client.go after the import
// block: the struct, auth plumbing, constructor, options, and helpers. Split
// out from generateClientFile so the import block can be decided from this
// text's actual content (see needsContext/needsFmt there) instead of from a
// hand-maintained flag that can drift from what the body below really uses.
func (g *Generator) generateClientBody(spec *client.APISpec, config client.GeneratorConfig, needsAuth bool, authConfigCode, authApplyCode string) string {
	var buf strings.Builder

	// Client struct
	buf.WriteString("// Client is the main API client\n")
	buf.WriteString("type Client struct {\n")
	buf.WriteString("\thttpClient *http.Client\n")
	buf.WriteString("\tbaseURL    string\n")

	if needsAuth {
		buf.WriteString("\tauth       *AuthConfig\n")
	}

	if config.Features.Logging {
		buf.WriteString("\tlogger     Logger\n")
	}

	buf.WriteString("}\n\n")

	// AuthConfig struct and its apply method, if needed.
	if needsAuth {
		buf.WriteString(authConfigCode)
		buf.WriteString(authApplyCode)
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

	// Emitted unconditionally, not gated on a declared cookie scheme: the
	// login endpoint that sets the session cookie is frequently absent from
	// securitySchemes entirely, so gating this on a detected scheme would
	// withhold the option from exactly the case that needs it.
	buf.WriteString("// WithCookieJar makes the client store and resend cookies.\n")
	buf.WriteString("//\n")
	buf.WriteString("// A client holding a jar carries session state and must not be shared\n")
	buf.WriteString("// between users: one caller's session would ride along on another's\n")
	buf.WriteString("// requests. Pass a per-user jar, or use WithSessionJar for a process-local one.\n")
	buf.WriteString("func WithCookieJar(jar http.CookieJar) ClientOption {\n")
	buf.WriteString("\treturn func(c *Client) {\n")
	buf.WriteString("\t\tc.httpClient.Jar = jar\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// WithSessionJar installs an in-memory cookie jar, so a login response's\n")
	buf.WriteString("// Set-Cookie is replayed on later calls. See WithCookieJar on sharing.\n")
	buf.WriteString("func WithSessionJar() ClientOption {\n")
	buf.WriteString("\treturn func(c *Client) {\n")
	buf.WriteString("\t\t// cookiejar.New only errors on bad options, and there are none here.\n")
	buf.WriteString("\t\tjar, _ := cookiejar.New(nil)\n")
	buf.WriteString("\t\tc.httpClient.Jar = jar\n")
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

	if needsAuth {
		buf.WriteString("// WithAuth sets the authentication configuration\n")
		buf.WriteString("func WithAuth(auth AuthConfig) ClientOption {\n")
		buf.WriteString("\treturn func(c *Client) {\n")
		buf.WriteString("\t\tc.auth = &auth\n")
		buf.WriteString("\t}\n")
		buf.WriteString("}\n\n")
	}

	// Helper methods
	buf.WriteString(g.generateHelperMethods(needsAuth))

	return buf.String()
}

// generateHelperMethods generates helper methods for the client.
func (g *Generator) generateHelperMethods(needsAuth bool) string {
	var buf strings.Builder

	buf.WriteString("// buildURL builds a full URL from a path\n")
	buf.WriteString("func (c *Client) buildURL(path string) string {\n")
	buf.WriteString("\treturn c.baseURL + path\n")
	buf.WriteString("}\n\n")

	buf.WriteString("// addAuth adds authentication to the request\n")
	buf.WriteString("func (c *Client) addAuth(req *http.Request) {\n")

	if needsAuth {
		// c.auth.apply is nil-receiver-safe, so this needs no nil check of
		// its own; every scheme's credential goes to its own declared
		// location instead of the single hardcoded X-API-Key header this
		// used to emit.
		buf.WriteString("\tc.auth.apply(req.Header, req.URL)\n")
	}

	// When no scheme is declared, Client carries no auth field at all (see
	// the struct above), so there is nothing to apply -- addAuth is kept
	// as a no-op rather than removed, so rest.go's unconditional c.addAuth(req)
	// call needs no gating of its own.
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateErrorsFile generates the errors.go file.
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

// generateGoMod generates the go.mod file.
func (g *Generator) generateGoMod(config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("module %s\n\n", config.Module))
	buf.WriteString("go 1.24.0\n\n")

	// Derived from getDependencies rather than listed again here. The two
	// used to be maintained separately and had drifted: the reported
	// dependency list named webtransport-go, this require block did not, so
	// a generated client that imports it could not resolve the module at
	// all. Sharing the one list is what stops that from recurring.
	deps := g.getDependencies(config)
	if len(deps) > 0 {
		buf.WriteString("require (\n")

		for _, dep := range deps {
			buf.WriteString(fmt.Sprintf("\t%s %s\n", dep.Name, dep.Version))
		}

		buf.WriteString(")\n")
	}

	return buf.String()
}

// getDependencies returns the list of dependencies. It is the single source
// of truth for what the generated client needs: generateGoMod renders its
// require block from this list, and the result's own Dependencies metadata
// is this list verbatim.
func (g *Generator) getDependencies(config client.GeneratorConfig) []generators.Dependency {
	deps := []generators.Dependency{}

	if config.IncludeStreaming {
		deps = append(deps, generators.Dependency{
			Name:    "github.com/gorilla/websocket",
			Version: "v1.5.0",
			Type:    "direct",
		})
		// webtransport.go emits against the Transport type, which the
		// package introduced in v0.12.0 when it renamed Dialer. This pin
		// and that emission have to name the same release, and both match
		// what forge's own go.mod pins.
		deps = append(deps, generators.Dependency{
			Name:    "github.com/quic-go/webtransport-go",
			Version: "v0.12.0",
			Type:    "direct",
		})
	}

	return deps
}

// generateInstructions generates setup instructions.
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
