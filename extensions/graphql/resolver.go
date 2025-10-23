package graphql

import (
	"github.com/xraph/forge"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

// Resolver is the root resolver struct for GraphQL operations
type Resolver struct {
	container forge.Container
	logger    forge.Logger
	metrics   forge.Metrics
	config    Config
}

// NewResolver creates a new resolver with dependencies
func NewResolver(container forge.Container, logger forge.Logger, metrics forge.Metrics, config Config) *Resolver {
	return &Resolver{
		container: container,
		logger:    logger,
		metrics:   metrics,
		config:    config,
	}
}
