package graphql

import (
	"github.com/99designs/gqlgen/graphql/handler"
)

// enableFederation configures Apollo Federation support for the GraphQL server
// Federation allows multiple GraphQL services to be composed into a single graph
func enableFederation(srv *handler.Server, config Config) {
	// Federation is configured in gqlgen.yml via the federation section
	// The generated code includes federation support automatically when enabled
	// Additional runtime configuration can be added here if needed

	// Note: Federation v2 is configured in gqlgen.yml with:
	// federation:
	//   filename: generated/federation.go
	//   package: generated
	//   version: 2

	// No additional runtime configuration required for basic federation
	// Advanced features like custom entity resolvers can be added here
}

// FederationConfig holds federation-specific configuration
type FederationConfig struct {
	// Enable federation support
	Enabled bool
	// Version of Apollo Federation (1 or 2)
	Version int
}

// DefaultFederationConfig returns default federation configuration
func DefaultFederationConfig() FederationConfig {
	return FederationConfig{
		Enabled: false,
		Version: 2,
	}
}
