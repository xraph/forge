package client

import (
	"fmt"
	"strings"
)

// AuthCodeGenerator generates authentication-related code
type AuthCodeGenerator struct{}

// NewAuthCodeGenerator creates a new auth code generator
func NewAuthCodeGenerator() *AuthCodeGenerator {
	return &AuthCodeGenerator{}
}

// DetectAuthSchemes detects authentication schemes from the API spec
func (a *AuthCodeGenerator) DetectAuthSchemes(spec *APISpec) []DetectedAuthScheme {
	var detected []DetectedAuthScheme
	seenSchemes := make(map[string]bool)

	for _, scheme := range spec.Security {
		if seenSchemes[scheme.Name] {
			continue
		}
		seenSchemes[scheme.Name] = true

		detected = append(detected, DetectedAuthScheme{
			Name:          scheme.Name,
			Type:          scheme.Type,
			In:            scheme.In,
			Scheme:        scheme.Scheme,
			BearerFormat:  scheme.BearerFormat,
			RequiresScope: a.requiresScopes(spec, scheme.Name),
		})
	}

	return detected
}

// requiresScopes checks if any endpoint requires scopes for this auth scheme
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

// GetAuthConfigType determines the appropriate auth config type
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
			if scheme.Scheme == "bearer" {
				hasBearer = true
			} else if scheme.Scheme == "basic" {
				hasBasic = true
			}
		case "apiKey":
			hasAPIKey = true
		case "oauth2":
			hasOAuth = true
		}
	}

	// Determine config type
	if hasOAuth {
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

// GetAuthHeaderName returns the header name for an auth scheme
func (a *AuthCodeGenerator) GetAuthHeaderName(scheme DetectedAuthScheme) string {
	switch scheme.Type {
	case "http":
		return "Authorization"
	case "apiKey":
		if scheme.In == "header" {
			return scheme.Name
		}
		return ""
	default:
		return ""
	}
}

// GetAuthPrefix returns the prefix for an auth value (e.g., "Bearer ")
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

// DetectedAuthScheme represents a detected authentication scheme
type DetectedAuthScheme struct {
	Name          string
	Type          string
	In            string
	Scheme        string
	BearerFormat  string
	RequiresScope bool
}

// AuthRequirement represents an authentication requirement for a specific endpoint
type AuthRequirement struct {
	SchemeName string
	Required   bool
	Scopes     []string
}

// GetEndpointAuthRequirements returns auth requirements for an endpoint
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

// GenerateAuthDocumentation generates documentation for authentication
func (a *AuthCodeGenerator) GenerateAuthDocumentation(schemes []DetectedAuthScheme) string {
	if len(schemes) == 0 {
		return "No authentication required."
	}

	var doc strings.Builder
	doc.WriteString("## Authentication\n\n")
	doc.WriteString("This API supports the following authentication methods:\n\n")

	for _, scheme := range schemes {
		doc.WriteString(fmt.Sprintf("### %s\n\n", scheme.Name))
		doc.WriteString(fmt.Sprintf("- **Type**: %s\n", scheme.Type))

		switch scheme.Type {
		case "http":
			doc.WriteString(fmt.Sprintf("- **Scheme**: %s\n", scheme.Scheme))
			if scheme.BearerFormat != "" {
				doc.WriteString(fmt.Sprintf("- **Bearer Format**: %s\n", scheme.BearerFormat))
			}
			if scheme.Scheme == "bearer" {
				doc.WriteString("- **Usage**: Pass the token in the `Authorization` header as `Bearer <token>`\n")
			} else if scheme.Scheme == "basic" {
				doc.WriteString("- **Usage**: Pass credentials in the `Authorization` header as `Basic <base64-encoded-credentials>`\n")
			}

		case "apiKey":
			doc.WriteString(fmt.Sprintf("- **Location**: %s\n", scheme.In))
			if scheme.In == "header" {
				doc.WriteString(fmt.Sprintf("- **Header Name**: %s\n", scheme.Name))
			} else if scheme.In == "query" {
				doc.WriteString(fmt.Sprintf("- **Query Parameter**: %s\n", scheme.Name))
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

// NeedsAuthConfig determines if any endpoints need authentication
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

// MergeAuthSchemes merges authentication schemes, removing duplicates
func MergeAuthSchemes(schemes []DetectedAuthScheme) []DetectedAuthScheme {
	seen := make(map[string]bool)
	var result []DetectedAuthScheme

	for _, scheme := range schemes {
		key := fmt.Sprintf("%s:%s", scheme.Type, scheme.Name)
		if !seen[key] {
			seen[key] = true
			result = append(result, scheme)
		}
	}

	return result
}
