package grpc

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
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

	// Register GRPCService constructor with Vessel
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*GRPCService, error) {
		return NewGRPCService(finalConfig, logger, metrics)
	}); err != nil {
		return fmt.Errorf("failed to register grpc service: %w", err)
	}

	// Register backward-compatible string key
	if err := forge.RegisterSingleton(app.Container(), "grpc", func(c forge.Container) (GRPC, error) {
		return forge.InjectType[*GRPCService](c)
	}); err != nil {
		return fmt.Errorf("failed to register grpc interface: %w", err)
	}

	e.Logger().Info("grpc extension registered",
		forge.F("address", finalConfig.Address),
		forge.F("tls", finalConfig.EnableTLS),
	)

	return nil
}

// Start marks the extension as started.
// The actual server is started by Vessel calling GRPCService.Start().
func (e *Extension) Start(ctx context.Context) error {
	e.MarkStarted()
	return nil
}

// Stop marks the extension as stopped.
// The actual server is stopped by Vessel calling GRPCService.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

// Health checks the extension health.
// Service health is managed by Vessel through GRPCService.Health().
func (e *Extension) Health(ctx context.Context) error {
	return nil
}
