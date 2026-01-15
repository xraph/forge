package queue

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/database"
)

// Extension implements forge.Extension for queue functionality.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension

	config Config
	// No longer storing queue - Vessel manages it
}

// NewExtension creates a new queue extension with functional options.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("queue", "2.0.0", "Message queue with Redis/RabbitMQ/NATS")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new queue extension with a complete config.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the queue extension with the app.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	programmaticConfig := e.config

	finalConfig := DefaultConfig()
	if err := e.LoadConfig("queue", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("queue: failed to load required config: %w", err)
		}

		e.Logger().Warn("queue: using default/programmatic config", forge.F("error", err.Error()))
	}

	e.config = finalConfig

	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("queue config validation failed: %w", err)
	}

	cfg := finalConfig

	// Register QueueService constructor with Vessel
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*QueueService, error) {
		var (
			queue Queue
			err   error
		)

		switch cfg.Driver {
		case "inmemory":
			queue = NewInMemoryQueue(cfg, logger, metrics)
		case "redis":
			// Check if using database Redis connection
			if cfg.DatabaseRedisConnection != "" {
				dbManager, err := forge.InjectType[*database.DatabaseManager](app.Container())
				if err != nil {
					return nil, fmt.Errorf("database extension not available for redis connection '%s': %w",
						cfg.DatabaseRedisConnection, err)
				}

				// Get Redis client from database manager
				redisClient, err := dbManager.Redis(cfg.DatabaseRedisConnection)
				if err != nil {
					return nil, fmt.Errorf("failed to get redis connection '%s' from database: %w",
						cfg.DatabaseRedisConnection, err)
				}

				// Create queue with external client
				queue, err = NewRedisQueueWithClient(cfg, logger, metrics, redisClient)
			} else {
				// Create queue with own connection
				queue, err = NewRedisQueue(cfg, logger, metrics)
			}
		case "rabbitmq":
			queue, err = NewRabbitMQQueue(cfg, logger, metrics)
		case "nats":
			queue, err = NewNATSQueue(cfg, logger, metrics)
		default:
			return nil, fmt.Errorf("unknown queue driver: %s", cfg.Driver)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to create queue: %w", err)
		}

		return NewQueueService(cfg, queue, logger, metrics), nil
	}); err != nil {
		return fmt.Errorf("failed to register queue service: %w", err)
	}

	// Register backward-compatible string key
	if err := forge.RegisterSingleton(app.Container(), "queue", func(c forge.Container) (Queue, error) {
		return forge.InjectType[*QueueService](c)
	}); err != nil {
		return fmt.Errorf("failed to register queue interface: %w", err)
	}

	e.Logger().Info("queue extension registered", forge.F("driver", cfg.Driver))

	return nil
}

// Start marks the extension as started.
// Queue service is started by Vessel calling QueueService.Start().
func (e *Extension) Start(ctx context.Context) error {
	e.MarkStarted()
	return nil
}

// Stop marks the extension as stopped.
// Queue service is stopped by Vessel calling QueueService.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

// Health checks the extension health.
// Service health is managed by Vessel through QueueService.Health().
func (e *Extension) Health(ctx context.Context) error {
	return nil
}

// Dependencies returns the names of extensions this extension depends on.
// When using a database Redis connection, the queue depends on the database extension.
func (e *Extension) Dependencies() []string {
	if e.config.DatabaseRedisConnection != "" {
		return []string{"database"}
	}

	return nil
}
