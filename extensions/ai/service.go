package ai

import (
	"context"

	"github.com/xraph/forge/extensions/ai/internal"
)

// service implements the shared.Service interface for AI
type service struct {
	ai internal.AI
}

// Name returns the service name
func (s *service) Name() string {
	return "ai"
}

// Start starts the AI service
// Note: AI manager is started by the extension, not the service wrapper
func (s *service) Start(ctx context.Context) error {
	// Service wrapper doesn't start the AI manager directly
	// The extension handles AI manager lifecycle
	return nil
}

// Stop stops the AI service
func (s *service) Stop(ctx context.Context) error {
	return s.ai.Stop(ctx)
}

// Health performs health check
func (s *service) Health(ctx context.Context) error {
	return s.ai.HealthCheck(ctx)
}
