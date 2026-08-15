package client

import (
	"fmt"
	"sort"
	"strings"
)

// AuthCodeGenerator generates authentication-related code.
type AuthCodeGenerator struct{}

// NewAuthCodeGenerator creates a new auth code generator.
func NewAuthCodeGenerator() *AuthCodeGenerator {
	return &AuthCodeGenerator{}
}

// DetectAuthSchemes detects authentication schemes from the API spec.
func (a *AuthCodeGenerator) DetectAuthSchemes(spec *APISpec) []DetectedAuthScheme {
	var detected []DetectedAuthScheme

	seenSchemes := make(map[string]bool)

	for _, scheme := range spec.Security {
		if seenSchemes[scheme.Key] {
			continue
		}

		seenSchemes[scheme.Key] = true

		detected = append(detected, DetectedAuthScheme{
			Key:           scheme.Key,
			ParamName:     scheme.ParamName,
			Type:          scheme.Type,
			In:            scheme.In,
			Scheme:        scheme.Scheme,
			BearerFormat:  scheme.BearerFormat,
			RequiresScope: a.requiresScopes(spec, scheme.Key),
		})
	}

	return detected
}

// requiresScopes checks if any endpoint requires scopes for this auth scheme.
func (a *AuthCodeGenerator) requiresScopes(spec *APISpec, schemeName string) bool {
	// Check REST endpoints
	for _, endpoint := range spec.Endpoints {
		for _, secReq := range endpoint.Security {
			if secReq.SchemeName == schemeName && len(secReq.Scopes) > 0 {
				return true
			}
		}
	}

	// Check WebSocket endpoints
	for _, ws := range spec.WebSockets {
		for _, secReq := range ws.Security {
			if secReq.SchemeName == schemeName && len(secReq.Scopes) > 0 {
				return true
			}
		}
	}

	// Check SSE endpoints
	for _, sse := range spec.SSEs {
		for _, secReq := range sse.Security {
			if secReq.SchemeName == schemeName && len(secReq.Scopes) > 0 {
				return true
			}
		}
	}

	return false
}

// GetAuthConfigType determines the appropriate auth config type.
func (a *AuthCodeGenerator) GetAuthConfigType(schemes []DetectedAuthScheme) string {
	if len(schemes) == 0 {
		return "none"
	}

	// Check for multiple auth types
	hasBearer := false
	hasAPIKey := false
	hasBasic := false
	hasOAuth := false

	for _, scheme := range schemes {
		switch scheme.Type {
		case "http":
			switch scheme.Scheme {
			case "bearer":
				hasBearer = true
			case "basic":
				hasBasic = true
			}
		case "apiKey":
			hasAPIKey = true
		case "oauth2":
			hasOAuth = true
		}
	}

	// Determine config type
	if hasOAuth { //nolint:gocritic // ifElseChain: auth priority logic clearer with if-else
		return "oauth"
	} else if hasBearer && !hasAPIKey && !hasBasic {
		return "bearer"
	} else if hasAPIKey && !hasBearer && !hasBasic {
		return "apikey"
	} else if hasBasic && !hasBearer && !hasAPIKey {
		return "basic"
	} else {
		return "multi" // Multiple auth types
	}
}

// GetAuthHeaderName returns the header name for an auth scheme.
func (a *AuthCodeGenerator) GetAuthHeaderName(scheme DetectedAuthScheme) string {
	switch scheme.Type {
	case "http":
		return "Authorization"
	case "apiKey":
		if scheme.In == "header" {
			return scheme.ParamName
		}

		return ""
	default:
		return ""
	}
}

// GetAuthPrefix returns the prefix for an auth value (e.g., "Bearer ").
func (a *AuthCodeGenerator) GetAuthPrefix(scheme DetectedAuthScheme) string {
	if scheme.Type == "http" {
		switch scheme.Scheme {
		case "bearer":
			return "Bearer "
		case "basic":
			return "Basic "
		}
	}

	return ""
}

// DetectedAuthScheme represents a detected authentication scheme.
type DetectedAuthScheme struct {
	Key           string // the securitySchemes map key
	ParamName     string // the wire name, apiKey only
	Type          string
	In            string
	Scheme        string
	BearerFormat  string
	RequiresScope bool
}

// AuthRequirement represents an authentication requirement for a specific endpoint.
type AuthRequirement struct {
	SchemeName string
	Required   bool
	Scopes     []string
}

// GetEndpointAuthRequirements returns auth requirements for an endpoint.
func (a *AuthCodeGenerator) GetEndpointAuthRequirements(endpoint Endpoint, spec *APISpec) []AuthRequirement {
	var requirements []AuthRequirement

	for _, secReq := range endpoint.Security {
		requirements = append(requirements, AuthRequirement{
			SchemeName: secReq.SchemeName,
			Required:   true,
			Scopes:     secReq.Scopes,
		})
	}

	return requirements
}

// CollectCapabilities returns every distinct scope declared anywhere in the
// spec, sorted.
//
// These are the strings a route declared through WithRequiredAuth: the
// generator turns them into a union type so a client can ask whether the
// current principal holds one. They are a UX affordance and never an
// authorization decision -- see the generated capabilities.ts header.
//
// The sort is load-bearing rather than cosmetic. Endpoint.Security is built by
// ranging a Go map (see convertOperation in spec_parser.go), Go randomises map
// iteration, and the generated capability file is byte-diffed by CI, so an
// unsorted walk would report a spurious change on every regeneration.
//
// Every endpoint kind that carries security is walked, WebTransport included --
// deliberately wider than requiresScopes, which predates WebTransport support
// and answers a different question. A scope declared on a WebTransport route is
// still a scope this API has, and omitting it would leave a capability the spec
// names outside the union that is supposed to enumerate them all.
func (a *AuthCodeGenerator) CollectCapabilities(spec *APISpec) []string {
	seen := make(map[string]bool)

	collect := func(requirements []SecurityRequirement) {
		for _, req := range requirements {
			for _, scope := range req.Scopes {
				if scope != "" {
					seen[scope] = true
				}
			}
		}
	}

	for i := range spec.Endpoints {
		collect(spec.Endpoints[i].Security)
	}

	for i := range spec.WebSockets {
		collect(spec.WebSockets[i].Security)
	}

	for i := range spec.SSEs {
		collect(spec.SSEs[i].Security)
	}

	for i := range spec.WebTransports {
		collect(spec.WebTransports[i].Security)
	}

	capabilities := make([]string, 0, len(seen))
	for scope := range seen {
		capabilities = append(capabilities, scope)
	}

	sort.Strings(capabilities)

	return capabilities
}

// EndpointCapabilities returns the scope sets, any ONE of which permits this
// endpoint. Nil means the endpoint is not scope-gated.
//
// The nesting is OpenAPI's own semantics, not an invention: security
// requirements are ORed against each other, and the scopes within one are
// ANDed. `WithRequiredAuth("jwt", "write:users", "admin")` therefore yields a
// single alternative demanding both scopes, while a route offering two
// providers yields one alternative each.
//
// Known limitation, stated here because the loss happens upstream and cannot
// be recovered at this layer: convertOperation flattens each OpenAPI security
// requirement OBJECT into one SecurityRequirement per scheme, so an
// AND-across-schemes requirement ({"jwt": [...], "apiKey": [...]} in a single
// object) arrives indistinguishable from two ORed alternatives. For specs
// Forge itself emits this is lossless -- processSecurityRequirements writes one
// scheme per requirement in both its AND and OR modes -- but for hand-written
// OpenAPI using AND, the answer below is more permissive than the server.
// Which is the safe direction for an affordance that must never be relied on
// as a boundary: it shows an action the server may still refuse, rather than
// hiding one the user actually holds.
func (a *AuthCodeGenerator) EndpointCapabilities(endpoint Endpoint) [][]string {
	return capabilityAlternatives(endpoint.Security)
}

// capabilityAlternatives normalises a security requirement list into sorted,
// deduplicated scope alternatives.
func capabilityAlternatives(requirements []SecurityRequirement) [][]string {
	if len(requirements) == 0 {
		return nil
	}

	var alternatives [][]string

	seen := make(map[string]bool, len(requirements))

	for _, req := range requirements {
		scopes := sortedUniqueScopes(req.Scopes)

		// An alternative demanding nothing is satisfied by every principal, and
		// the alternatives are ORed, so one such entry means the endpoint is not
		// scope-gated at all however many scopes its siblings demand. This is the
		// ordinary shape of `WithRequiredAuth("jwt")` -- authentication required,
		// no particular scope -- and returning its siblings instead would gate an
		// endpoint the server does not.
		if len(scopes) == 0 {
			return nil
		}

		key := strings.Join(scopes, "\x00")
		if seen[key] {
			continue
		}

		seen[key] = true

		alternatives = append(alternatives, scopes)
	}

	// Deterministic order, for the same CI byte-diff reason CollectCapabilities
	// sorts. The joined form is compared rather than the slices element by
	// element because it is already computed above and two alternatives are
	// equal exactly when their joins are.
	sort.Slice(alternatives, func(i, j int) bool {
		return strings.Join(alternatives[i], "\x00") < strings.Join(alternatives[j], "\x00")
	})

	return alternatives
}

// sortedUniqueScopes returns scopes sorted, deduplicated, and with empty
// entries dropped.
//
// Sorting is safe because an alternative is a SET whose members are all
// required together, so order carries no meaning -- and it is what makes the
// deduplication in capabilityAlternatives see two spellings of the same
// requirement as one.
func sortedUniqueScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(scopes))
	out := make([]string, 0, len(scopes))

	for _, scope := range scopes {
		if scope == "" || seen[scope] {
			continue
		}

		seen[scope] = true

		out = append(out, scope)
	}

	if len(out) == 0 {
		return nil
	}

	sort.Strings(out)

	return out
}

// GenerateAuthDocumentation generates documentation for authentication.
func (a *AuthCodeGenerator) GenerateAuthDocumentation(schemes []DetectedAuthScheme) string {
	if len(schemes) == 0 {
		return "No authentication required."
	}

	var doc strings.Builder
	doc.WriteString("## Authentication\n\n")
	doc.WriteString("This API supports the following authentication methods:\n\n")

	for _, scheme := range schemes {
		doc.WriteString(fmt.Sprintf("### %s\n\n", scheme.Key))
		doc.WriteString(fmt.Sprintf("- **Type**: %s\n", scheme.Type))

		switch scheme.Type {
		case "http":
			doc.WriteString(fmt.Sprintf("- **Scheme**: %s\n", scheme.Scheme))

			if scheme.BearerFormat != "" {
				doc.WriteString(fmt.Sprintf("- **Bearer Format**: %s\n", scheme.BearerFormat))
			}

			switch scheme.Scheme {
			case "bearer":
				doc.WriteString("- **Usage**: Pass the token in the `Authorization` header as `Bearer <token>`\n")
			case "basic":
				doc.WriteString("- **Usage**: Pass credentials in the `Authorization` header as `Basic <base64-encoded-credentials>`\n")
			}

		case "apiKey":
			doc.WriteString(fmt.Sprintf("- **Location**: %s\n", scheme.In))

			switch scheme.In {
			case "header":
				doc.WriteString(fmt.Sprintf("- **Header Name**: %s\n", scheme.Key))
			case "query":
				doc.WriteString(fmt.Sprintf("- **Query Parameter**: %s\n", scheme.Key))
			}

		case "oauth2":
			doc.WriteString("- **OAuth 2.0 Flow**: See API documentation for OAuth configuration\n")
		}

		if scheme.RequiresScope {
			doc.WriteString("- **Scopes**: Some endpoints require specific scopes\n")
		}

		doc.WriteString("\n")
	}

	return doc.String()
}

// NeedsAuthConfig determines if any endpoints need authentication.
func NeedsAuthConfig(spec *APISpec) bool {
	// Check if there are any security schemes
	if len(spec.Security) > 0 {
		return true
	}

	// Check if any endpoint has security requirements
	for _, endpoint := range spec.Endpoints {
		if len(endpoint.Security) > 0 {
			return true
		}
	}

	for _, ws := range spec.WebSockets {
		if len(ws.Security) > 0 {
			return true
		}
	}

	for _, sse := range spec.SSEs {
		if len(sse.Security) > 0 {
			return true
		}
	}

	return false
}

// MergeAuthSchemes merges authentication schemes, removing duplicates.
func MergeAuthSchemes(schemes []DetectedAuthScheme) []DetectedAuthScheme {
	seen := make(map[string]bool)

	var result []DetectedAuthScheme

	for _, scheme := range schemes {
		key := fmt.Sprintf("%s:%s", scheme.Type, scheme.Key)
		if !seen[key] {
			seen[key] = true

			result = append(result, scheme)
		}
	}

	return result
}
