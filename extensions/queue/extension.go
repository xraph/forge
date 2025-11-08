package queue

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

// Extension implements forge.Extension for queue functionality.
type Extension struct {
	*forge.BaseExtension

	config Config
	queue  Queue
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

	var (
		queue Queue
		err   error
	)

	switch e.config.Driver {
	case "inmemory":
		queue = NewInMemoryQueue(e.config, e.Logger(), e.Metrics())
	case "redis":
		queue, err = NewRedisQueue(e.config, e.Logger(), e.Metrics())
	case "rabbitmq":
		queue, err = NewRabbitMQQueue(e.config, e.Logger(), e.Metrics())
	case "nats":
		queue, err = NewNATSQueue(e.config, e.Logger(), e.Metrics())
	default:
		return fmt.Errorf("unknown queue driver: %s", e.config.Driver)
	}

	if err != nil {
		return fmt.Errorf("failed to create queue: %w", err)
	}

	e.queue = queue

	if err := forge.RegisterSingleton(app.Container(), "queue", func(c forge.Container) (Queue, error) {
		return e.queue, nil
	}); err != nil {
		return fmt.Errorf("failed to register queue service: %w", err)
	}

	e.Logger().Info("queue extension registered", forge.F("driver", e.config.Driver))

	return nil
}

// Start starts the queue extension.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting queue extension", forge.F("driver", e.config.Driver))

	if err := e.queue.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to queue: %w", err)
	}

	e.MarkStarted()
	e.Logger().Info("queue extension started")

	return nil
}

// Stop stops the queue extension.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping queue extension")

	if e.queue != nil {
		if err := e.queue.Disconnect(ctx); err != nil {
			e.Logger().Error("failed to disconnect queue", forge.F("error", err))
		}
	}

	e.MarkStopped()
	e.Logger().Info("queue extension stopped")

	return nil
}

// Health checks if the queue is healthy.
func (e *Extension) Health(ctx context.Context) error {
	if e.queue == nil {
		return errors.New("queue not initialized")
	}

	if err := e.queue.Ping(ctx); err != nil {
		return fmt.Errorf("queue health check failed: %w", err)
	}

	return nil
}

// Queue returns the queue instance.
func (e *Extension) Queue() Queue {
	return e.queue
}
