package grpc

import (
	"context"
	"errors"
	"fmt"
	"math"

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

// NewExtension creates a new gRPC extension.
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

// NewExtensionWithConfig creates a new gRPC extension with a complete config.
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

	// Registrars are code, not config: no file can set them, and a failed
	// load above may have left finalConfig without the options' copy.
	finalConfig.Services = programmaticConfig.Services
	e.config = finalConfig

	// Validate config before registering constructor
	if err := finalConfig.Validate(); err != nil {
		return fmt.Errorf("grpc config validation failed: %w", err)
	}

	// Register GRPCService constructor with Vessel
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*GRPCService, error) {
		svc, err := NewGRPCService(finalConfig, logger, metrics)
		if err != nil {
			return nil, err
		}

		svc.deferServe = true

		for _, register := range finalConfig.Services {
			if register == nil {
				continue
			}

			if err := register(app, svc.Server()); err != nil {
				return nil, fmt.Errorf("grpc: failed to register service: %w", err)
			}
		}

		return svc, nil
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register grpc service: %w", err)
	}

	// Register GRPC interface backed by the same *GRPCService singleton
	if err := forge.Provide(app.Container(), func(svc *GRPCService) GRPC {
		return svc.Server()
	}); err != nil {
		return fmt.Errorf("failed to register grpc interface: %w", err)
	}

	// Serve last, after every other after-start hook, so anything registered
	// while the app starts is on the server: services from the options,
	// from extensions that registered or started after this one, and from
	// hooks up to and including PhaseAfterStart.
	if err := app.RegisterHook(forge.PhaseAfterStart, e.serve, forge.LifecycleHookOptions{
		Name:     "grpc-serve",
		Priority: math.MinInt,
	}); err != nil {
		return fmt.Errorf("grpc: failed to register serve hook: %w", err)
	}

	e.Logger().Info("grpc extension registered",
		forge.F("address", finalConfig.Address),
		forge.F("tls", finalConfig.EnableTLS),
		forge.F("registrars", len(finalConfig.Services)),
	)

	return nil
}

// Start builds the gRPC service, which runs the registrars from the options,
// and marks the extension as started. The server listens later, at the end of
// app.Start, from the hook Register adds.
func (e *Extension) Start(ctx context.Context) error {
	if _, err := forge.Inject[*GRPCService](e.App().Container()); err != nil {
		return fmt.Errorf("failed to resolve grpc service: %w", err)
	}

	e.MarkStarted()

	return nil
}

// serve starts the server. It runs as the last PhaseAfterStart hook.
func (e *Extension) serve(ctx context.Context, app forge.App) error {
	svc, err := forge.Inject[*GRPCService](app.Container())
	if err != nil {
		return fmt.Errorf("grpc: failed to resolve grpc service: %w", err)
	}

	if err := svc.serve(ctx); err != nil {
		return fmt.Errorf("grpc: %w", err)
	}

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
		return errors.New("grpc extension not started")
	}

	return nil
}
