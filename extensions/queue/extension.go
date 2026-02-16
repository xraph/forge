package queue

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/database"
	"github.com/xraph/vessel"
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

	container := app.Container()

	// Fail-fast: verify database extension is available when using database redis connection
	if cfg.Driver == "redis" && cfg.DatabaseRedisConnection != "" {
		if _, err := forge.InjectType[*database.DatabaseManager](container); err != nil {
			return fmt.Errorf("database extension not available for redis connection '%s': %w",
				cfg.DatabaseRedisConnection, err)
		}
	}

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
				dbManager, err := forge.InjectType[*database.DatabaseManager](container)
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
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register queue service: %w", err)
	}

	// Register Queue interface backed by the same *QueueService singleton
	if err := forge.Provide(container, func(svc *QueueService) Queue {
		return svc
	}); err != nil {
		return fmt.Errorf("failed to register queue interface: %w", err)
	}

	e.Logger().Info("queue extension registered", forge.F("driver", cfg.Driver))

	return nil
}

// Start resolves and starts the queue service, then marks the extension as started.
func (e *Extension) Start(ctx context.Context) error {
	svc, err := forge.Inject[*QueueService](e.App().Container())
	if err != nil {
		return fmt.Errorf("failed to resolve queue service: %w", err)
	}

	if err := svc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start queue service: %w", err)
	}

	e.MarkStarted()
	return nil
}

// Stop stops the queue service and marks the extension as stopped.
func (e *Extension) Stop(ctx context.Context) error {
	svc, err := forge.Inject[*QueueService](e.App().Container())
	if err == nil {
		if stopErr := svc.Stop(ctx); stopErr != nil {
			e.Logger().Error("failed to stop queue service", forge.F("error", stopErr))
		}
	}

	e.MarkStopped()
	return nil
}

// Health checks the extension health.
func (e *Extension) Health(ctx context.Context) error {
	if !e.IsStarted() {
		return fmt.Errorf("queue extension not started")
	}

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
