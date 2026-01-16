package features

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for feature flags.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension

	config Config
	// No longer storing provider or service - Vessel manages it
}

// NewExtension creates a new feature flags extension.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("features", "1.0.0", "Feature Flags & A/B Testing")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new extension with complete config.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the extension with the app.
// This method now only loads configuration and registers service constructors.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	if !e.config.Enabled {
		e.Logger().Info("features extension disabled")
		return nil
	}

	cfg := e.config

	// Create provider based on configuration
	provider, err := e.createProvider()
	if err != nil {
		return fmt.Errorf("failed to create feature flags provider: %w", err)
	}

	// Register Service constructor with Vessel using vessel.WithAliases for backward compatibility
	// Config and provider captured in closure
	if err := e.RegisterConstructor(func(logger forge.Logger) (*Service, error) {
		return NewService(cfg, provider, logger), nil
	}, vessel.WithAliases(ServiceKey, ServiceKeyLegacy)); err != nil {
		return fmt.Errorf("failed to register features service: %w", err)
	}

	e.Logger().Info("features extension registered",
		forge.F("provider", cfg.Provider),
		forge.F("refresh_interval", cfg.RefreshInterval),
	)

	return nil
}

// Start marks the extension as started.
// The actual service is started by Vessel calling Service.Start().
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.MarkStarted()
	return nil
}

// Stop marks the extension as stopped.
// The actual service is stopped by Vessel calling Service.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.MarkStopped()
	return nil
}

// Health checks the extension health.
// Service health is managed by Vessel through Service.Health().
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	// Health is now managed by Vessel through Service.Health()
	return nil
}

// Dependencies returns extension dependencies.
func (e *Extension) Dependencies() []string {
	return []string{} // No dependencies
}

// createProvider creates the appropriate provider based on configuration.
func (e *Extension) createProvider() (Provider, error) {
	switch e.config.Provider {
	case "local":
		return NewLocalProvider(e.config.Local, e.config.DefaultFlags), nil
	case "launchdarkly":
		return NewLaunchDarklyProvider(e.config.LaunchDarkly, e.config.DefaultFlags)
	case "unleash":
		return NewUnleashProvider(e.config.Unleash, e.config.DefaultFlags)
	case "flagsmith":
		return NewFlagsmithProvider(e.config.Flagsmith, e.config.DefaultFlags)
	case "posthog":
		return NewPostHogProvider(e.config.PostHog, e.config.DefaultFlags)
	default:
		return nil, fmt.Errorf("unknown feature flags provider: %s", e.config.Provider)
	}
}
