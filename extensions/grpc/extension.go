package grpc

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// Extension implements forge.Extension for gRPC functionality
type Extension struct {
	*forge.BaseExtension
	config Config
	server GRPC
}

// NewExtension creates a new gRPC extension
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("grpc", "2.0.0", "gRPC server with interceptors and streaming")
	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new gRPC extension with a complete config
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the gRPC extension with the app
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	programmaticConfig := e.config
	finalConfig := DefaultConfig()
	if err := e.LoadConfig("grpc", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("grpc: failed to load required config: %w", err)
		}
		e.Logger().Warn("grpc: using default/programmatic config", forge.F("error", err.Error()))
	}
	e.config = finalConfig

	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("grpc config validation failed: %w", err)
	}

	server := NewGRPCServer(e.config, e.Logger(), e.Metrics())
	e.server = server

	if err := forge.RegisterSingleton(app.Container(), "grpc", func(c forge.Container) (GRPC, error) {
		return e.server, nil
	}); err != nil {
		return fmt.Errorf("failed to register grpc service: %w", err)
	}

	e.Logger().Info("grpc extension registered",
		forge.F("address", e.config.Address),
		forge.F("tls", e.config.EnableTLS),
	)

	return nil
}

// Start starts the gRPC extension
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting grpc extension", forge.F("address", e.config.Address))

	if err := e.server.Start(ctx, e.config.Address); err != nil {
		return fmt.Errorf("failed to start grpc server: %w", err)
	}

	e.MarkStarted()
	e.Logger().Info("grpc extension started")
	return nil
}

// Stop stops the gRPC extension
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping grpc extension")

	if e.server != nil {
		if err := e.server.GracefulStop(ctx); err != nil {
			e.Logger().Error("failed to stop grpc server gracefully", forge.F("error", err))
			// Force stop
			if err := e.server.Stop(ctx); err != nil {
				e.Logger().Error("failed to force stop grpc server", forge.F("error", err))
			}
		}
	}

	e.MarkStopped()
	e.Logger().Info("grpc extension stopped")
	return nil
}

// Health checks if the gRPC server is healthy
func (e *Extension) Health(ctx context.Context) error {
	if e.server == nil {
		return fmt.Errorf("grpc server not initialized")
	}

	if err := e.server.Ping(ctx); err != nil {
		return fmt.Errorf("grpc health check failed: %w", err)
	}

	return nil
}

// GRPC returns the gRPC server instance
func (e *Extension) GRPC() GRPC {
	return e.server
}
