package mcp

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// MCPService wraps an MCP Server implementation and provides lifecycle management.
// It implements vessel's di.Service interface so Vessel can manage its lifecycle.
type MCPService struct {
	config  Config
	server  *Server
	logger  forge.Logger
	metrics forge.Metrics
}

// NewMCPService creates a new MCP service with the given configuration.
// This is the constructor that will be registered with the DI container.
func NewMCPService(config Config, logger forge.Logger, metrics forge.Metrics) (*MCPService, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid mcp config: %w", err)
	}

	// Create MCP server
	server := NewServer(config, logger, metrics)

	return &MCPService{
		config:  config,
		server:  server,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Name returns the service name for Vessel's lifecycle management.
func (s *MCPService) Name() string {
	return "mcp-service"
}

// Start starts the MCP service.
// This is called automatically by Vessel during container.Start().
func (s *MCPService) Start(ctx context.Context) error {
	s.logger.Info("starting mcp service",
		forge.F("base_path", s.config.BasePath),
	)

	// MCP server doesn't have explicit Start, it's always ready after construction
	s.logger.Info("mcp service started")
	return nil
}

// Stop stops the MCP service.
// This is called automatically by Vessel during container.Stop().
func (s *MCPService) Stop(ctx context.Context) error {
	s.logger.Info("stopping mcp service")
	// MCP server cleanup happens automatically
	s.logger.Info("mcp service stopped")
	return nil
}

// Health checks if the MCP service is healthy.
func (s *MCPService) Health(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("mcp server not initialized")
	}
	// MCP doesn't have explicit health check, check if server exists
	return nil
}

// Server returns the underlying MCP server implementation.
func (s *MCPService) Server() *Server {
	return s.server
}
