package streaming

import (
	"context"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// IStreamingService defines the interface for the DefaultStreamingService.
type StreamingService interface {
	// Name returns the service name.
	Name() string

	// Dependencies returns the list of service dependencies.
	Dependencies() []string

	// OnStart starts the streaming service.
	OnStart(ctx context.Context) error

	// OnStop stops the streaming service.
	OnStop(ctx context.Context) error

	// OnHealthCheck performs a health check on the streaming service.
	OnHealthCheck(ctx context.Context) error

	// GetManager retrieves the StreamingManager associated with the service.
	GetManager() StreamingManager
}

// DefaultStreamingService implements the common.Service interface for DI integration
type DefaultStreamingService struct {
	manager StreamingManager
	config  StreamingConfig
	logger  common.Logger
	metrics common.Metrics
}

// NewStreamingService creates a new streaming service
func NewStreamingService(l common.Logger, metrics common.Metrics, configManager common.ConfigManager) StreamingService {
	// Load configuration
	config := DefaultStreamingConfig()
	if configManager != nil {
		if err := configManager.Bind("streaming", &config); err != nil {
			if l != nil {
				l.Warn("failed to bind streaming configuration, using defaults",
					logger.Error(err),
				)
			}
		}
	}

	// Create streaming manager
	manager := NewStreamingManager(config, l, metrics)

	return &DefaultStreamingService{
		manager: manager,
		config:  config,
		logger:  l,
		metrics: metrics,
	}
}

// NewStreamingServiceWithOpts creates a new streaming service
func NewStreamingServiceWithOpts(l common.Logger, metrics common.Metrics, configManager common.ConfigManager, opts StreamingConfig) StreamingService {
	// Load configuration
	config := opts
	if configManager != nil {
		if err := configManager.Bind("streaming", &config); err != nil {
			if l != nil {
				l.Warn("failed to bind streaming configuration, using defaults",
					logger.Error(err),
				)
			}
		}
	}

	// Create streaming manager
	manager := NewStreamingManager(config, l, metrics)

	return &DefaultStreamingService{
		manager: manager,
		config:  config,
		logger:  l,
		metrics: metrics,
	}
}

// Name returns the service name
func (s *DefaultStreamingService) Name() string {
	return common.StreamingServiceKey
}

// Dependencies returns the service dependencies
func (s *DefaultStreamingService) Dependencies() []string {
	deps := []string{common.LoggerKey, common.MetricsKey, common.ConfigKey}

	if s.config.EnablePersistence {
		deps = append(deps, common.DatabaseManagerKey)
	}

	if s.config.EnableRedisScaling {
		deps = append(deps, common.DatabaseRedisClientKey)
	}

	return deps
}

// OnStart starts the streaming service
func (s *DefaultStreamingService) OnStart(ctx context.Context) error {
	return s.manager.OnStart(ctx)
}

// OnStop stops the streaming service
func (s *DefaultStreamingService) OnStop(ctx context.Context) error {
	return s.manager.OnStop(ctx)
}

// OnHealthCheck performs health check
func (s *DefaultStreamingService) OnHealthCheck(ctx context.Context) error {
	return s.manager.OnHealthCheck(ctx)
}

// GetManager returns the streaming manager
func (s *DefaultStreamingService) GetManager() StreamingManager {
	return s.manager
}

// NewStreamingServiceFactory creates a streaming service instance
func NewStreamingServiceFactory(opts *StreamingConfig) func(logger common.Logger, metrics common.Metrics, configManager common.ConfigManager) StreamingService {
	streamConf := DefaultStreamingConfig()
	if opts != nil {
		streamConf = *opts
	}
	return func(logger common.Logger, metrics common.Metrics, configManager common.ConfigManager) StreamingService {
		return NewStreamingServiceWithOpts(logger, metrics, configManager, streamConf)
	}
}

// RegisterStreamingService registers the streaming service with a DI container
func RegisterStreamingService(container common.Container, opts *StreamingConfig) error {
	return container.Register(common.ServiceDefinition{
		Name:         common.StreamingServiceKey,
		Type:         (*StreamingService)(nil),
		Constructor:  NewStreamingServiceFactory(opts),
		Singleton:    true,
		Dependencies: []string{common.LoggerKey, common.MetricsKey, common.ConfigKey},
	})
}

// StreamingManagerFactory creates a streaming manager instance for direct injection
func StreamingManagerFactory(l common.Logger, metrics common.Metrics, configManager common.ConfigManager) StreamingManager {
	// Load configuration
	config := DefaultStreamingConfig()
	if configManager != nil {
		if err := configManager.Bind("streaming", &config); err != nil {
			if l != nil {
				l.Warn("failed to bind streaming configuration, using defaults",
					logger.Error(err),
				)
			}
		}
	}

	return NewStreamingManager(config, l, metrics)
}

// RegisterStreamingManager registers the streaming manager directly with a DI container
func RegisterStreamingManager(container common.Container) error {
	return container.Register(common.ServiceDefinition{
		Name:         common.StreamManagerKey,
		Type:         (*StreamingManager)(nil),
		Constructor:  StreamingManagerFactory,
		Singleton:    true,
		Dependencies: []string{common.LoggerKey, common.MetricsKey, common.ConfigKey},
	})
}
