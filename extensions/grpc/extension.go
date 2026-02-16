package grpc

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for gRPC functionality.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension
	config Config
	// No longer storing server - Vessel manages it
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

// Register registers the gRPC extension with the app.
// This method now only loads configuration and registers service constructors.
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

	// Validate config before registering constructor
	if err := finalConfig.Validate(); err != nil {
		return fmt.Errorf("grpc config validation failed: %w", err)
	}

	// Register GRPCService constructor with Vessel
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*GRPCService, error) {
		return NewGRPCService(finalConfig, logger, metrics)
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register grpc service: %w", err)
	}

	// Register GRPC interface backed by the same *GRPCService singleton
	if err := forge.Provide(app.Container(), func(svc *GRPCService) GRPC {
		return svc.Server()
	}); err != nil {
		return fmt.Errorf("failed to register grpc interface: %w", err)
	}

	e.Logger().Info("grpc extension registered",
		forge.F("address", finalConfig.Address),
		forge.F("tls", finalConfig.EnableTLS),
	)

	return nil
}

// Start resolves and starts the gRPC service, then marks the extension as started.
func (e *Extension) Start(ctx context.Context) error {
	svc, err := forge.Inject[*GRPCService](e.App().Container())
	if err != nil {
		return fmt.Errorf("failed to resolve grpc service: %w", err)
	}

	if err := svc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start grpc service: %w", err)
	}

	e.MarkStarted()
	return nil
}

// Stop stops the gRPC service and marks the extension as stopped.
func (e *Extension) Stop(ctx context.Context) error {
	svc, err := forge.Inject[*GRPCService](e.App().Container())
	if err == nil {
		if stopErr := svc.Stop(ctx); stopErr != nil {
			e.Logger().Error("failed to stop grpc service", forge.F("error", stopErr))
		}
	}

	e.MarkStopped()
	return nil
}

// Health checks the extension health.
func (e *Extension) Health(ctx context.Context) error {
	if !e.IsStarted() {
		return fmt.Errorf("grpc extension not started")
	}

	return nil
}
